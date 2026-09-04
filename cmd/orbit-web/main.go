// Command orbit-web runs the Orbit browser display node.
package main

import (
	"context"
	"errors"
	"flag"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"orbit/internal/config"
	"orbit/internal/logging"
	"orbit/internal/mqtt"
	webnode "orbit/nodes/web"

	"go.uber.org/zap"
)

const version = "0.1.0"

func main() {
	configPath := flag.String("config", "configs/web.local.yaml", "path to the Web Node YAML configuration")
	flag.Parse()
	cfg, runErr := config.LoadWebNode(*configPath)
	level := "info"
	if runErr == nil {
		level = cfg.Logging.Level
	}
	logger := zap.Must(logging.New(level))
	if runErr == nil {
		runErr = run(cfg, logger)
	}
	if runErr != nil {
		logger.Error("orbit web node stopped", zap.Error(runErr))
	}
	_ = logger.Sync()
	if runErr != nil {
		os.Exit(1)
	}
}

func run(cfg *config.WebNodeConfig, logger *zap.Logger) error {
	signalContext, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	ctx, cancel := context.WithCancel(signalContext)
	defer cancel()

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
	runner, err := webnode.NewRunner(webnode.RunnerConfig{
		NodeID: cfg.Node.ID, NodeEpoch: webnode.NewEpoch(), FirmwareVersion: version,
	}, client, store, logger)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", cfg.Web.Listen)
	if err != nil {
		return err
	}
	server := &http.Server{
		Handler: webnode.HandlerWithAuth(store, runner, webnode.AuthConfig{
			Password: cfg.Web.Auth.Password, SessionTTL: cfg.Web.Auth.SessionTTL.Duration,
		}), ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout: 60 * time.Second,
	}
	errCh := make(chan error, 2)
	go func() { errCh <- runner.Run(ctx) }()
	go func() { errCh <- server.Serve(listener) }()
	logger.Info("orbit web node started", zap.String("node_id", cfg.Node.ID), zap.String("url", "http://"+cfg.Web.Listen))

	var runErr error
	select {
	case <-signalContext.Done():
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

func disconnect(client *mqtt.Client, logger *zap.Logger) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Disconnect(ctx); err != nil {
		logger.Warn("disconnect mqtt", zap.Error(err))
	}
}
