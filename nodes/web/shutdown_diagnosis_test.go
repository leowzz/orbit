package web

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestDiagnosisWebServerShutdown(t *testing.T) {
	t.Run("without SSE", func(t *testing.T) {
		server, _, _ := startDiagnosisServer(t)
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		defer cancel()

		started := time.Now()
		err := server.Shutdown(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if elapsed := time.Since(started); elapsed >= 100*time.Millisecond {
			t.Fatalf("shutdown took %s without an SSE client", elapsed)
		}
	})

	t.Run("with SSE", func(t *testing.T) {
		server, store, address := startDiagnosisServer(t)
		responseDone := make(chan struct{})
		go func() {
			response, _ := http.Get("http://" + address + "/api/events")
			if response != nil {
				_ = response.Body.Close()
			}
			close(responseDone)
		}()
		waitForDiagnosisSubscriber(t, store)

		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		defer cancel()
		started := time.Now()
		err := server.Shutdown(ctx)
		elapsed := time.Since(started)
		_ = server.Close()
		<-responseDone

		if errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("shutdown waited %s for the SSE handler and exhausted its deadline", elapsed)
		}
		if err != nil {
			t.Fatal(err)
		}
		if elapsed >= 100*time.Millisecond {
			t.Fatalf("shutdown took %s with an SSE client", elapsed)
		}
	})
}

func startDiagnosisServer(t *testing.T) (*http.Server, *Store, string) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	store := NewStore()
	server := &http.Server{Handler: Handler(store, nil)}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })
	return server, store, listener.Addr().String()
}

func waitForDiagnosisSubscriber(t *testing.T, store *Store) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		store.mu.RLock()
		count := len(store.subscribers)
		store.mu.RUnlock()
		if count == 1 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("SSE handler did not subscribe")
}
