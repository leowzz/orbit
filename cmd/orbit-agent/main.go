// Command orbit-agent runs the trusted-host Orbit Agent.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"orbit/internal/agent"
	codexcap "orbit/internal/capabilities/codex"
	appclock "orbit/internal/clock"
	"orbit/internal/config"
	"orbit/internal/logging"
	"orbit/internal/mqtt"
	codexsource "orbit/internal/sources/codex"
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
	agentID, err := agent.ResolveID(cfg.Agent.ID)
	if err != nil {
		return err
	}

	var sources agent.Sources
	runnerConfig := agent.Config{
		AgentID:      agentID,
		AgentEpoch:   agent.NewEpoch(),
		AgentVersion: version,
		HostLabel:    cfg.Agent.HostLabel,
	}
	if cfg.Sources.Sub2API.Enabled {
		location, locationErr := time.LoadLocation(cfg.Sources.Sub2API.Timezone)
		if locationErr != nil {
			return fmt.Errorf("load Sub2API timezone: %w", locationErr)
		}
		source, sourceErr := sub2api.New(sub2api.Config{
			LoginEndpoint:   cfg.Sources.Sub2API.LoginEndpoint,
			RefreshEndpoint: cfg.Sources.Sub2API.RefreshEndpoint,
			UsageEndpoint:   cfg.Sources.Sub2API.UsageEndpoint,
			Email:           cfg.Sources.Sub2API.Credentials.Email,
			Password:        cfg.Sources.Sub2API.Credentials.Password,
			Timeout:         cfg.Sources.Sub2API.Timeout.Duration,
		})
		if sourceErr != nil {
			return sourceErr
		}
		sources.Usage = source
		runnerConfig.CurrencyCode = cfg.Sources.Sub2API.CurrencyCode
		runnerConfig.Location = location
		runnerConfig.PollInterval = cfg.Sources.Sub2API.PollInterval.Duration
		runnerConfig.ObservationTTL = cfg.Sources.Sub2API.ObservationTTL.Duration
	}
	if cfg.Sources.Codex.Enabled {
		source, sourceErr := codexsource.New(codexsource.Config{
			Home:            cfg.Sources.Codex.CodexHome,
			Limit:           cfg.Sources.Codex.SessionLimit,
			IncludeArchived: cfg.Sources.Codex.IncludeArchived,
			IgnoreCWD:       cfg.Sources.Codex.Ignore.CWD,
			IgnoreSource:    cfg.Sources.Codex.Ignore.Source,
		})
		if sourceErr != nil {
			return sourceErr
		}
		sources.Codex = source
		runnerConfig.CodexPollInterval = cfg.Sources.Codex.PollInterval.Duration
		runnerConfig.CodexObservationTTL = cfg.Sources.Codex.ObservationTTL.Duration
		runnerConfig.CodexDisplayName = cfg.Sources.Codex.Privacy.IncludeDisplayName
		runnerConfig.CodexProjectName = cfg.Sources.Codex.Privacy.IncludeProjectName
	}
	var capabilities agent.Capabilities
	if cfg.Capabilities.OpenCodexSession.Enabled {
		capabilities.OpenCodexSession = codexcap.NewOpener()
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
	runnerConfig.Now = synchronizedClock.Now
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

	runner, err := agent.New(runnerConfig, sources, capabilities, client, logger)
	if err != nil {
		return err
	}
	logger.Info("orbit agent started",
		zap.String("agent_id", agentID),
		zap.String("agent_epoch", runnerConfig.AgentEpoch),
		zap.String("agent_version", runnerConfig.AgentVersion),
		zap.String("host_label", runnerConfig.HostLabel),
		zap.Bool("usage_enabled", sources.Usage != nil),
		zap.Bool("codex_enabled", sources.Codex != nil),
		zap.Bool("open_codex_session_enabled", capabilities.OpenCodexSession != nil),
	)
	runnerErr := make(chan error, 1)
	go func() { runnerErr <- runner.Run(ctx) }()
	select {
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
