// Command orbit-web runs the Orbit browser display node.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	appclock "orbit/internal/clock"
	"orbit/internal/config"
	"orbit/internal/logging"
	"orbit/internal/mqtt"
	webnode "orbit/nodes/web"

	"go.uber.org/zap"
)

const version = "0.1.0"

func main() {
	configPath := flag.String("config", "configs/web.local.yaml", "path to the Web Node YAML configuration")
	staticDir := flag.String("static-dir", "", "development static asset directory with automatic browser reload")
	flag.Parse()
	cfg, runErr := config.LoadWebNode(*configPath)
	level := "info"
	if runErr == nil {
		level = cfg.Logging.Level
	}
	logger := zap.Must(logging.New(level))
	if runErr == nil {
		runErr = run(cfg, logger, *staticDir)
	}
	if runErr != nil {
		logger.Error("orbit web node stopped", zap.Error(runErr))
	}
	_ = logger.Sync()
	if runErr != nil {
		os.Exit(1)
	}
}

func run(cfg *config.WebNodeConfig, logger *zap.Logger, staticDir string) error {
	signalContext, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	ctx, cancel := context.WithCancel(signalContext)
	defer cancel()
	if cfg.MQTT.URL == "" && cfg.Web.Inbox.URL != "" {
		return runInbox(ctx, cfg, logger, staticDir)
	}
	synchronizedClock, err := appclock.New(appclock.Config{
		Server:       cfg.NTP.Server,
		SyncInterval: cfg.NTP.SyncInterval.Duration,
		Timeout:      cfg.NTP.Timeout.Duration,
	}, logger)
	if err != nil {
		return err
	}
	synchronizedClock.Start(ctx)

	client, err := mqtt.Connect(ctx, mqtt.Config{
		URL:      cfg.MQTT.URL,
		ClientID: "orbit-web-" + cfg.Node.ID,
		Username: cfg.MQTT.Credentials.Username,
		Password: cfg.MQTT.Credentials.Password,
		TLS: mqtt.TLSConfig{
			Enabled: cfg.MQTT.TLS.Enabled, CAFile: cfg.MQTT.TLS.CAFile,
			CertFile: cfg.MQTT.TLS.CertFile, KeyFile: cfg.MQTT.TLS.KeyFile,
		},
	}, logger)
	if err != nil {
		return err
	}
	defer disconnect(client, logger)

	store := webnode.NewStore()
	nodeEpoch := webnode.NewEpoch()
	runner, err := webnode.NewRunner(webnode.RunnerConfig{
		NodeID: cfg.Node.ID, NodeEpoch: nodeEpoch, FirmwareVersion: version, Now: synchronizedClock.Now,
	}, client, store, logger)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", cfg.Web.Listen)
	if err != nil {
		return err
	}
	authConfig := webnode.AuthConfig{
		Password: cfg.Web.Auth.Password, SessionTTL: cfg.Web.Auth.SessionTTL.Duration,
		InboxURL: cfg.Web.Inbox.URL, InboxToken: cfg.Web.Inbox.Token,
	}
	var handler http.Handler
	if staticDir == "" {
		handler = webnode.HandlerWithAuth(store, runner, authConfig)
	} else {
		handler, err = webnode.DevelopmentHandler(store, runner, authConfig, staticDir)
		if err != nil {
			return err
		}
	}
	server := newHTTPServer(ctx, handler)
	errCh := make(chan error, 2)
	go func() { errCh <- runner.Run(ctx) }()
	go func() { errCh <- server.Serve(listener) }()
	logger.Info("orbit web node started",
		zap.String("node_id", cfg.Node.ID),
		zap.String("node_epoch", nodeEpoch),
		zap.String("firmware_version", version),
		zap.String("url", "http://"+cfg.Web.Listen),
	)

	var runErr error
	select {
	case <-signalContext.Done():
	case runErr = <-client.TerminalErrors():
		runErr = fmt.Errorf("mqtt connection terminated: %w", runErr)
	case runErr = <-errCh:
		if errors.Is(runErr, http.ErrServerClosed) {
			runErr = nil
		}
	}
	cancel()
	shutdownContext, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	shutdownErr := server.Shutdown(shutdownContext)
	return errors.Join(runErr, shutdownErr)
}

func newHTTPServer(ctx context.Context, handler http.Handler) *http.Server {
	return &http.Server{
		Handler:           handler,
		BaseContext:       func(net.Listener) context.Context { return ctx },
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
}

func disconnect(client *mqtt.Client, logger *zap.Logger) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Disconnect(ctx); err != nil {
		logger.Warn("disconnect mqtt", zap.Error(err))
	}
}

// A message-only Web Node needs one Core connection, no MQTT identity or route.
func runInbox(ctx context.Context, cfg *config.WebNodeConfig, logger *zap.Logger, staticDir string) error {
	auth := webnode.AuthConfig{Password: cfg.Web.Auth.Password, SessionTTL: cfg.Web.Auth.SessionTTL.Duration, InboxURL: cfg.Web.Inbox.URL, InboxToken: cfg.Web.Inbox.Token}
	var handler http.Handler = webnode.HandlerWithAuth(webnode.NewStore(), nil, auth)
	if staticDir != "" {
		var err error
		handler, err = webnode.DevelopmentHandler(webnode.NewStore(), nil, auth, staticDir)
		if err != nil {
			return err
		}
	}
	original := handler
	handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" || r.URL.Path == "/index.html" {
			http.Redirect(w, r, "/inbox/", http.StatusSeeOther)
			return
		}
		original.ServeHTTP(w, r)
	})
	listener, err := net.Listen("tcp", cfg.Web.Listen)
	if err != nil {
		return err
	}
	server := newHTTPServer(ctx, handler)
	stopped := make(chan error, 1)
	go func() { stopped <- server.Serve(listener) }()
	logger.Info("orbit web inbox started", zap.String("url", "http://"+cfg.Web.Listen+"/inbox/"))
	select {
	case err = <-stopped:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return server.Shutdown(shutdown)
}
