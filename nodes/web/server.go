package web

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"strings"
	"time"
)

//go:embed static/*
var staticFiles embed.FS

const developmentReloadInterval = 250 * time.Millisecond

type SessionIntentPublisher interface {
	OpenCodexSession(context.Context, string, uint64) (string, error)
}

func Handler(store *Store, intents SessionIntentPublisher) http.Handler {
	return HandlerWithAuth(store, intents, AuthConfig{})
}

func HandlerWithAuth(store *Store, intents SessionIntentPublisher, authConfig AuthConfig) http.Handler {
	assets, err := fs.Sub(staticFiles, "static")
	if err != nil {
		panic(err)
	}
	return handlerWithAssets(store, intents, authConfig, assets, nil)
}

func DevelopmentHandler(store *Store, intents SessionIntentPublisher, authConfig AuthConfig, staticDir string) (http.Handler, error) {
	if staticDir == "" {
		return nil, fmt.Errorf("development static directory is required")
	}
	assets := os.DirFS(staticDir)
	if _, err := fs.Stat(assets, "index.html"); err != nil {
		return nil, fmt.Errorf("open development static directory %q: %w", staticDir, err)
	}
	return handlerWithAssets(store, intents, authConfig, assets, func() (string, error) {
		return staticAssetVersion(assets)
	}), nil
}

func handlerWithAssets(store *Store, intents SessionIntentPublisher, authConfig AuthConfig, assets fs.FS, assetVersion func() (string, error)) http.Handler {
	auth := newAuthManager(authConfig)
	mux := http.NewServeMux()
	mountInbox(mux, auth, authConfig, false)
	mux.HandleFunc("GET /favicon.ico", func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusNoContent)
	})
	mountAuth(mux, auth)
	mux.Handle("GET /api/state", auth.protect(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
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
	})))
	mux.Handle("GET /api/events", auth.protect(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
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
		var reloadTicks <-chan time.Time
		lastAssetVersion := ""
		if assetVersion != nil {
			lastAssetVersion, _ = assetVersion()
			reloadTicker := time.NewTicker(developmentReloadInterval)
			defer reloadTicker.Stop()
			reloadTicks = reloadTicker.C
		}
		fmt.Fprint(response, ": connected\n\n")
		flusher.Flush()
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
			case <-reloadTicks:
				version, err := assetVersion()
				if err != nil || version == lastAssetVersion {
					continue
				}
				lastAssetVersion = version
				fmt.Fprintf(response, "event: reload\ndata: %s\n\n", version)
				flusher.Flush()
			}
		}
	})))
	mux.Handle("POST /api/sessions/{session_id}/open", auth.protect(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
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
	})))

	fileServer := http.FileServer(http.FS(assets))
	var staticHandler http.Handler = fileServer
	if assetVersion != nil {
		staticHandler = http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			response.Header().Set("Cache-Control", "no-store")
			fileServer.ServeHTTP(response, request)
		})
	}
	mux.Handle("GET /", staticHandler)
	return securityHeaders(mux)
}

func staticAssetVersion(assets fs.FS) (string, error) {
	hash := sha256.New()
	err := fs.WalkDir(assets, ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		contents, err := fs.ReadFile(assets, path)
		if err != nil {
			return err
		}
		_, _ = io.WriteString(hash, path)
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write(contents)
		_, _ = hash.Write([]byte{0})
		return nil
	})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

func (auth *authManager) protect(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if !sameOrigin(request) {
			http.Error(response, "forbidden", http.StatusForbidden)
			return
		}
		if !auth.required() {
			next.ServeHTTP(response, request)
			return
		}
		if session := auth.session(authToken(request)); session != nil {
			ctx, cancel := context.WithDeadline(request.Context(), session.expires)
			stop := context.AfterFunc(session.ctx, cancel)
			defer stop()
			defer cancel()
			next.ServeHTTP(response, request.WithContext(ctx))
			return
		}
		response.Header().Set("WWW-Authenticate", `Bearer realm="orbit-web"`)
		response.Header().Set("Cache-Control", "no-store")
		http.Error(response, "authentication required", http.StatusUnauthorized)
	})
}

func authToken(request *http.Request) string {
	if value := strings.TrimSpace(request.Header.Get("Authorization")); strings.HasPrefix(value, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(value, "Bearer "))
	}
	if cookie, err := request.Cookie(authCookieName); err == nil {
		return cookie.Value
	}
	return ""
}

func writeJSON(response http.ResponseWriter, value any) {
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(response).Encode(value)
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Security-Policy", "default-src 'self'; connect-src 'self'; img-src 'self' blob:; style-src 'self'; script-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		response.Header().Set("X-Frame-Options", "DENY")
		response.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		if request.TLS != nil || strings.EqualFold(request.Header.Get("X-Forwarded-Proto"), "https") {
			response.Header().Set("Strict-Transport-Security", "max-age=31536000")
		}
		response.Header().Set("Referrer-Policy", "no-referrer")
		response.Header().Set("X-Content-Type-Options", "nosniff")
		next.ServeHTTP(response, request)
	})
}
