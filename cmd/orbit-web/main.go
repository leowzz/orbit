// Command orbit-web serves the personal Orbit inbox.
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
	webnode "orbit/nodes/web"

	"go.uber.org/zap"
)

func main() {
	configPath := flag.String("config", "configs/web.local.yaml", "path to the Web Node YAML configuration")
	staticDir := flag.String("static-dir", "", "development inbox static asset directory")
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
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	auth := webnode.AuthConfig{Password: cfg.Web.Auth.Password, SessionTTL: cfg.Web.Auth.SessionTTL.Duration, InboxURL: cfg.Web.Inbox.URL, InboxToken: cfg.Web.Inbox.Token}
	handler, err := webnode.InboxHandler(auth, staticDir)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", cfg.Web.Listen)
	if err != nil {
		return err
	}
	server := newHTTPServer(ctx, handler)
	stopped := make(chan error, 1)
	go func() { stopped <- server.Serve(listener) }()
	logger.Info("orbit inbox web started", zap.String("url", "http://"+cfg.Web.Listen))
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

func newHTTPServer(ctx context.Context, handler http.Handler) *http.Server {
	return &http.Server{
		Handler:           handler,
		BaseContext:       func(net.Listener) context.Context { return ctx },
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		MaxHeaderBytes:    16 << 10,
		IdleTimeout:       60 * time.Second,
	}
}
