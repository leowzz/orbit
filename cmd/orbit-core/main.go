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
	"sort"
	"syscall"
	"time"

	orbitv1 "orbit/gen/go/orbit/v1"
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
	routes := make([]core.Route, 0, len(cfg.ProjectionRoutes))
	nodeIDs := make([]string, 0, len(cfg.ProjectionRoutes))
	for nodeID, route := range cfg.ProjectionRoutes {
		nodeIDs = append(nodeIDs, nodeID)
		inputs := make([]core.RouteInput, 0, len(route.Inputs))
		for _, input := range route.Inputs {
			observationType, err := parseObservationType(input.ObservationType)
			if err != nil {
				return err
			}
			inputs = append(inputs, core.RouteInput{AgentID: input.AgentID, ObservationType: observationType})
		}
		routes = append(routes, core.Route{
			NodeID: nodeID, Profile: route.Profile, Inputs: inputs,
		})
	}
	sort.Strings(nodeIDs)
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
		return runMQTT(ctx, cfg, engine, logger, synchronizedClock.Now)
	}
	store, err := inbox.Open(cfg.App.DataDir)
	if err != nil {
		return err
	}
	defer store.Close()
	api := appapi.New(store, cfg.App, func(node string) json.RawMessage {
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
		if err := runMQTT(ctx, cfg, engine, logger, synchronizedClock.Now); err != nil && ctx.Err() == nil {
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

func runMQTT(ctx context.Context, cfg *config.CoreConfig, engine *core.Engine, logger *zap.Logger, now func() time.Time) error {
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
	logger.Info("orbit core started",
		zap.String("core_id", cfg.Core.ID),
	)
	runnerErr := make(chan error, 1)
	go func() { runnerErr <- runner.Run(ctx) }()
	select {
	case <-ctx.Done():
		return nil
	case err := <-runnerErr:
		return err
	case err := <-client.TerminalErrors():
		return fmt.Errorf("mqtt connection terminated: %w", err)
	}
}

func parseObservationType(value string) (orbitv1.ObservationType, error) {
	switch value {
	case "usage":
		return orbitv1.ObservationType_OBSERVATION_TYPE_USAGE, nil
	case "codex":
		return orbitv1.ObservationType_OBSERVATION_TYPE_CODEX, nil
	default:
		return orbitv1.ObservationType_OBSERVATION_TYPE_UNSPECIFIED, fmt.Errorf("unsupported observation type %q", value)
	}
}

func disconnect(client *mqtt.Client, logger *zap.Logger) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Disconnect(ctx); err != nil {
		logger.Warn("disconnect mqtt", zap.Error(err))
	}
}
