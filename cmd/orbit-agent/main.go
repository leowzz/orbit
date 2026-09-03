// Command orbit-agent runs the trusted-host Orbit Agent.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"orbit/internal/agent"
	"orbit/internal/config"
	"orbit/internal/mqtt"
	"orbit/internal/sources/sub2api"
)

const version = "0.1.0"

func main() {
	configPath := flag.String("config", "configs/agent.local.yaml", "path to the Agent YAML configuration")
	flag.Parse()
	if err := run(*configPath); err != nil {
		slog.Error("orbit agent stopped", "error", err)
		os.Exit(1)
	}
}

func run(configPath string) error {
	cfg, err := config.LoadAgent(configPath)
	if err != nil {
		return err
	}
	if !cfg.Sources.Sub2API.Enabled {
		return errors.New("sources.sub2api.enabled must be true for the V1 Agent")
	}
	agentID, err := agent.ResolveID(cfg.Agent.ID)
	if err != nil {
		return err
	}
	location, err := time.LoadLocation(cfg.Sources.Sub2API.Timezone)
	if err != nil {
		return fmt.Errorf("load Sub2API timezone: %w", err)
	}
	logger := newLogger(cfg.Logging.Level)
	source, err := sub2api.New(sub2api.Config{
		LoginEndpoint:   cfg.Sources.Sub2API.LoginEndpoint,
		RefreshEndpoint: cfg.Sources.Sub2API.RefreshEndpoint,
		UsageEndpoint:   cfg.Sources.Sub2API.UsageEndpoint,
		Email:           cfg.Sources.Sub2API.Credentials.Email,
		Password:        cfg.Sources.Sub2API.Credentials.Password,
		Timeout:         cfg.Sources.Sub2API.Timeout.Duration,
	})
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	client, err := mqtt.Connect(ctx, mqtt.Config{
		URL:      cfg.MQTT.URL,
		ClientID: "orbit-agent-" + agentID,
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

	runner, err := agent.New(agent.Config{
		AgentID:        agentID,
		AgentEpoch:     agent.NewEpoch(),
		AgentVersion:   version,
		HostLabel:      cfg.Agent.HostLabel,
		CurrencyCode:   cfg.Sources.Sub2API.CurrencyCode,
		Location:       location,
		PollInterval:   cfg.Sources.Sub2API.PollInterval.Duration,
		ObservationTTL: cfg.Sources.Sub2API.ObservationTTL.Duration,
	}, source, client, logger)
	if err != nil {
		return err
	}
	logger.Info("orbit agent started", "agent_id", agentID)
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
