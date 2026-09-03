// Command orbit-core runs the Orbit Core service.
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"orbit/internal/config"
	"orbit/internal/core"
	"orbit/internal/mqtt"
)

func main() {
	configPath := flag.String("config", "configs/core.local.yaml", "path to the Core YAML configuration")
	flag.Parse()
	if err := run(*configPath); err != nil {
		slog.Error("orbit core stopped", "error", err)
		os.Exit(1)
	}
}

func run(configPath string) error {
	cfg, err := config.LoadCore(configPath)
	if err != nil {
		return err
	}
	logger := newLogger(cfg.Logging.Level)
	routes := make([]core.Route, 0, len(cfg.ProjectionRoutes))
	for nodeID, route := range cfg.ProjectionRoutes {
		if len(route.Inputs) != 1 || route.Inputs[0].ObservationType != "usage" {
			return errors.New("V1 projection routes require exactly one usage input")
		}
		routes = append(routes, core.Route{
			NodeID:  nodeID,
			AgentID: route.Inputs[0].AgentID,
			Profile: route.Profile,
		})
	}
	usagePolicy := cfg.ObservationPolicies["usage"]
	engine, err := core.New(core.Config{
		CoreID:    cfg.Core.ID,
		CoreEpoch: core.NewEpoch(),
		Routes:    routes,
		UsagePolicy: core.UsagePolicy{
			MaxTTL:        usagePolicy.MaxTTL.Duration,
			MaxFutureSkew: usagePolicy.MaxFutureSkew.Duration,
		},
		RetainFor: 24 * time.Hour,
	})
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
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
	defer disconnect(client)
	runner, err := core.NewRunner(engine, client, logger)
	if err != nil {
		return err
	}
	logger.Info("orbit core started", "core_id", cfg.Core.ID)
	return runner.Run(ctx)
}

func newLogger(level string) *slog.Logger {
	var slogLevel slog.Level
	switch level {
	case "debug":
		slogLevel = slog.LevelDebug
	case "warn":
		slogLevel = slog.LevelWarn
	case "error":
		slogLevel = slog.LevelError
	default:
		slogLevel = slog.LevelInfo
	}
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slogLevel}))
}

func disconnect(client *mqtt.Client) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Disconnect(ctx); err != nil {
		slog.Warn("disconnect mqtt", "error", err)
	}
}
