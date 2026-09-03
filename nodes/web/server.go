package web

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"time"
)

//go:embed static/*
var staticFiles embed.FS

func Handler(store *Store) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /favicon.ico", func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /api/state", func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "application/json; charset=utf-8")
		response.Header().Set("Cache-Control", "no-store")
		snapshot, err := store.Snapshot()
		if err != nil {
			http.Error(response, "state unavailable", http.StatusInternalServerError)
			return
		}
		if snapshot == nil {
			response.WriteHeader(http.StatusNoContent)
			return
		}
		_ = json.NewEncoder(response).Encode(snapshot)
	})
	mux.HandleFunc("GET /api/events", func(response http.ResponseWriter, request *http.Request) {
		flusher, ok := response.(http.Flusher)
		if !ok {
			http.Error(response, "streaming unavailable", http.StatusInternalServerError)
			return
		}
		response.Header().Set("Content-Type", "text/event-stream")
		response.Header().Set("Cache-Control", "no-cache")
		response.Header().Set("Connection", "keep-alive")
		updates, unsubscribe := store.Subscribe()
		defer unsubscribe()
		keepAlive := time.NewTicker(15 * time.Second)
		defer keepAlive.Stop()
		for {
			select {
			case <-request.Context().Done():
				return
			case payload := <-updates:
				fmt.Fprintf(response, "data: %s\n\n", payload)
				flusher.Flush()
			case <-keepAlive.C:
				fmt.Fprint(response, ": keepalive\n\n")
				flusher.Flush()
			}
		}
	})

	assets, err := fs.Sub(staticFiles, "static")
	if err != nil {
		panic(err)
	}
	mux.Handle("GET /", securityHeaders(http.FileServer(http.FS(assets))))
	return mux
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Security-Policy", "default-src 'self'; connect-src 'self'; img-src 'self'; style-src 'self'; script-src 'self'")
		response.Header().Set("Referrer-Policy", "no-referrer")
		response.Header().Set("X-Content-Type-Options", "nosniff")
		next.ServeHTTP(response, request)
	})
}
