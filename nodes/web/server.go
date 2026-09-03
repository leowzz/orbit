package web

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"time"
)

//go:embed static/*
var staticFiles embed.FS

type SessionIntentPublisher interface {
	OpenCodexSession(context.Context, string, uint64) (string, error)
}

func Handler(store *Store, intents SessionIntentPublisher) http.Handler {
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
	mux.HandleFunc("POST /api/sessions/{session_id}/open", func(response http.ResponseWriter, request *http.Request) {
		if intents == nil {
			http.Error(response, "session actions unavailable", http.StatusServiceUnavailable)
			return
		}
		mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
		if err != nil || mediaType != "application/json" {
			http.Error(response, "content type must be application/json", http.StatusUnsupportedMediaType)
			return
		}
		var input struct {
			ViewRevision uint64 `json:"view_revision"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(response, request.Body, 1024))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&input); err != nil {
			http.Error(response, "invalid request", http.StatusBadRequest)
			return
		}
		if err := decoder.Decode(&struct{}{}); err != io.EOF {
			http.Error(response, "invalid request", http.StatusBadRequest)
			return
		}
		intentID, err := intents.OpenCodexSession(request.Context(), request.PathValue("session_id"), input.ViewRevision)
		if err != nil {
			http.Error(response, "session unavailable", http.StatusConflict)
			return
		}
		response.Header().Set("Content-Type", "application/json; charset=utf-8")
		response.Header().Set("Cache-Control", "no-store")
		response.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(response).Encode(map[string]string{"intent_id": intentID, "status": "accepted"})
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
