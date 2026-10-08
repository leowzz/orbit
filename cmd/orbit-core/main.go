// Command orbit-core runs the Orbit Core service.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"orbit/internal/appapi"
	appclock "orbit/internal/clock"
	"orbit/internal/config"
	"orbit/internal/core"
	"orbit/internal/inbox"
	"orbit/internal/logging"
	"orbit/internal/mqtt"

	"go.uber.org/zap"
	"google.golang.org/protobuf/encoding/protojson"
)

func main() {
	configPath := flag.String("config", "configs/core.local.yaml", "path to the Core YAML configuration")
	seed := flag.Bool("seed-inbox", false, "insert public sample items, then exit")
	reset := flag.Bool("reset-sync-generation", false, "invalidate sync cursors after restoring a backup, then exit")
	flag.Parse()
	cfg, runErr := config.LoadCore(*configPath)
	level := "info"
	if runErr == nil {
		level = cfg.Logging.Level
	}
	logger := zap.Must(logging.New(level))
	if runErr == nil {
		if *seed || *reset {
			if cfg.App.Listen == "" {
				runErr = errors.New("app must be enabled")
			} else {
				var store *inbox.Store
				store, runErr = inbox.Open(cfg.App.DataDir)
				if runErr == nil {
					if *reset {
						runErr = store.ResetGeneration(context.Background())
					}
					if runErr == nil && *seed {
						runErr = store.Seed(context.Background())
					}
					_ = store.Close()
				}
			}
		} else {
			runErr = run(cfg, logger)
		}
	}
	if runErr != nil {
		logger.Error("orbit core stopped", zap.Error(runErr))
	}
	_ = logger.Sync()
	if runErr != nil {
		os.Exit(1)
	}
}

func run(cfg *config.CoreConfig, logger *zap.Logger) error {
	store, err := core.OpenRouteStore(cfg.Console.Database, cfg.ProjectionRoutes)
	if err != nil {
		return fmt.Errorf("open core database: %w", err)
	}
	defer store.Close()
	document, err := store.Load()
	if err != nil {
		return err
	}
	if err := cfg.ValidateRoutes(document.Routes); err != nil {
		return fmt.Errorf("stored routes: %w", err)
	}
	routes := core.RoutesFromConfig(document.Routes)
	nodeIDs := make([]string, 0, len(routes))
	for _, route := range routes {
		nodeIDs = append(nodeIDs, route.NodeID)
	}
	usagePolicy := cfg.ObservationPolicies["usage"]
	codexPolicy := cfg.ObservationPolicies["codex"]
	coreEpoch := core.NewEpoch()
	engine, err := core.New(core.Config{
		CoreID:    cfg.Core.ID,
		CoreEpoch: coreEpoch,
		Routes:    routes,
		UsagePolicy: core.UsagePolicy{
			MaxTTL:        usagePolicy.MaxTTL.Duration,
			MaxFutureSkew: usagePolicy.MaxFutureSkew.Duration,
		},
		CodexPolicy: core.CodexPolicy{
			MaxTTL:        codexPolicy.MaxTTL.Duration,
			MaxFutureSkew: codexPolicy.MaxFutureSkew.Duration,
		},
		RetainFor: 24 * time.Hour,
	})
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	synchronizedClock, err := appclock.New(appclock.Config{
		Server:       cfg.NTP.Server,
		SyncInterval: cfg.NTP.SyncInterval.Duration,
		Timeout:      cfg.NTP.Timeout.Duration,
	}, logger)
	if err != nil {
		return err
	}
	synchronizedClock.Start(ctx)

	if cfg.App.Listen == "" {
		return runMQTT(ctx, cfg, engine, store, logger, synchronizedClock.Now)
	}
	inboxStore, err := inbox.Open(cfg.App.DataDir)
	if err != nil {
		return err
	}
	defer inboxStore.Close()
	api := appapi.New(inboxStore, cfg.App, func(node string) json.RawMessage {
		view := engine.AppView(synchronizedClock.Now(), node)
		if view == nil {
			return json.RawMessage(`null`)
		}
		raw, _ := protojson.Marshal(view)
		return raw
	})
	listener, err := net.Listen("tcp", cfg.App.Listen)
	if err != nil {
		return err
	}
	server := &http.Server{Handler: api.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, BaseContext: func(net.Listener) context.Context { return ctx }}
	serverErr := make(chan error, 1)
	go func() { serverErr <- server.Serve(listener) }()
	go api.Sweep(ctx)
	go func() {
		if err := runMQTT(ctx, cfg, engine, store, logger, synchronizedClock.Now); err != nil && ctx.Err() == nil {
			logger.Error("MQTT stopped; App inbox remains available", zap.Error(err))
		}
	}()
	logger.Info("app api started", zap.String("listen", cfg.App.Listen), zap.Strings("node_ids", nodeIDs))
	select {
	case <-ctx.Done():
	case err = <-serverErr:
	}
	stop()
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdown)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func runMQTT(ctx context.Context, cfg *config.CoreConfig, engine *core.Engine, store *core.RouteStore, logger *zap.Logger, now func() time.Time) error {
	client, err := mqtt.Connect(ctx, mqtt.Config{
		URL:      cfg.MQTT.URL,
		ClientID: "orbit-core-" + cfg.Core.ID,
		Username: cfg.MQTT.Credentials.Username,
		Password: cfg.MQTT.Credentials.Password,
		TLS: mqtt.TLSConfig{
			Enabled:  cfg.MQTT.TLS.Enabled,
			CAFile:   cfg.MQTT.TLS.CAFile,
			CertFile: cfg.MQTT.TLS.CertFile,
			KeyFile:  cfg.MQTT.TLS.KeyFile,
		},
	}, logger)
	if err != nil {
		return err
	}
	defer disconnect(client, logger)
	runner, err := core.NewRunner(engine, client, logger, now)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", cfg.Console.Listen)
	if err != nil {
		return fmt.Errorf("listen core console: %w", err)
	}
	server := &http.Server{Handler: core.ConsoleHandler(runner, store, cfg), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second}
	defer func() {
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	serverErr := make(chan error, 1)
	go func() { serverErr <- server.Serve(listener) }()
	logger.Info("core console listening", zap.String("listen", cfg.Console.Listen))
	logger.Info("orbit core started",
		zap.String("core_id", cfg.Core.ID),
	)
	runnerErr := make(chan error, 1)
	go func() { runnerErr <- runner.Run(ctx) }()
	select {
	case <-ctx.Done():
		return nil
	case err := <-serverErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case err := <-runnerErr:
		return err
	case err := <-client.TerminalErrors():
		return fmt.Errorf("mqtt connection terminated: %w", err)
	}
}

func disconnect(client *mqtt.Client, logger *zap.Logger) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Disconnect(ctx); err != nil {
		logger.Warn("disconnect mqtt", zap.Error(err))
	}
}
