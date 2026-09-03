// Command orbit-agent runs the trusted-host Orbit Agent.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"orbit/internal/agent"
	"orbit/internal/config"
	"orbit/internal/logging"
	"orbit/internal/mqtt"
	"orbit/internal/sources/sub2api"

	"go.uber.org/zap"
)

const version = "0.1.0"

func main() {
	configPath := flag.String("config", "configs/agent.local.yaml", "path to the Agent YAML configuration")
	flag.Parse()
	cfg, runErr := config.LoadAgent(*configPath)
	level := "info"
	if runErr == nil {
		level = cfg.Logging.Level
	}
	logger := zap.Must(logging.New(level))
	if runErr == nil {
		runErr = run(cfg, logger)
	}
	if runErr != nil {
		logger.Error("orbit agent stopped", zap.Error(runErr))
	}
	_ = logger.Sync()
	if runErr != nil {
		os.Exit(1)
	}
}

func run(cfg *config.AgentConfig, logger *zap.Logger) error {
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
	defer disconnect(client, logger)

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
	logger.Info("orbit agent started", zap.String("agent_id", agentID))
	return runner.Run(ctx)
}

func disconnect(client *mqtt.Client, logger *zap.Logger) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Disconnect(ctx); err != nil {
		logger.Warn("disconnect mqtt", zap.Error(err))
	}
}
