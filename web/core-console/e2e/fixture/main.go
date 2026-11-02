// Local-only browser fixture. Every run creates a new temporary inbox and never reads deployment configuration.
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"orbit/internal/appapi"
	"orbit/internal/config"
	"orbit/internal/core"
	"orbit/internal/inbox"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

func main() {
	dir, err := os.MkdirTemp("", "orbit-workbench-check-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)
	routes, err := core.OpenRouteStore(filepath.Join(dir, "core.sqlite"), nil)
	if err != nil {
		panic(err)
	}
	defer routes.Close()
	store, err := inbox.Open(dir)
	if err != nil {
		panic(err)
	}
	defer store.Close()
	cfg := &config.CoreConfig{Core: config.CoreIdentity{ID: "local-check"}, Console: config.ConsoleConfig{Password: "local-check-only", SessionHours: 1}, App: config.AppConfig{DataDir: dir}}
	engine, err := core.New(core.Config{CoreID: "local-check", CoreEpoch: "local-check"})
	if err != nil {
		panic(err)
	}
	personal, err := appapi.New(store, cfg.App, nil)
	if err != nil {
		panic(err)
	}
	_, err = store.Apply(context.Background(), "@console", inbox.Operation{ID: "00000000-0000-4000-8000-000000000001", ItemID: "00000000-0000-4000-8000-000000000002", Type: "create", Kind: "text", Body: "欢迎使用工作台 https://example.com"})
	if err != nil {
		panic(err)
	}
	fmt.Println("Local fixture: http://127.0.0.1:17622")
	server := &http.Server{Addr: "127.0.0.1:17622", Handler: core.InboxConsoleHandler(engine, routes, cfg, personal.AdminHandler(), personal.Handler()), ReadHeaderTimeout: 5 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() { <-ctx.Done(); _ = server.Close() }()
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		panic(err)
	}
}
