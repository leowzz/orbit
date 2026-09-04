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
	mux.HandleFunc("GET /favicon.ico", func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /api/auth/config", func(response http.ResponseWriter, _ *http.Request) {
		writeJSON(response, map[string]bool{"required": auth.required()})
	})
	mux.HandleFunc("POST /api/auth/login", func(response http.ResponseWriter, request *http.Request) {
		if !auth.required() {
			http.Error(response, "web authentication is not configured", http.StatusNotFound)
			return
		}
		mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
		if err != nil || mediaType != "application/json" {
			http.Error(response, "content type must be application/json", http.StatusUnsupportedMediaType)
			return
		}
		var input struct {
			Password string `json:"password"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(response, request.Body, 1024))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&input); err != nil {
			http.Error(response, "invalid credentials", http.StatusUnauthorized)
			return
		}
		if err := decoder.Decode(&struct{}{}); err != io.EOF {
			http.Error(response, "invalid credentials", http.StatusUnauthorized)
			return
		}
		if !auth.passwordMatches(input.Password) {
			http.Error(response, "invalid credentials", http.StatusUnauthorized)
			return
		}
		token, expiresAt, err := auth.issue()
		if err != nil {
			http.Error(response, "authentication unavailable", http.StatusInternalServerError)
			return
		}
		maxAge := int(auth.ttl / time.Second)
		if maxAge < 1 {
			maxAge = 1
		}
		http.SetCookie(response, &http.Cookie{
			Name: authCookieName, Value: token, Path: "/", MaxAge: maxAge,
			Expires: expiresAt, HttpOnly: true, SameSite: http.SameSiteLaxMode,
		})
		writeJSON(response, map[string]string{"token": token, "expires_at": expiresAt.UTC().Format(time.RFC3339Nano)})
	})
	mux.HandleFunc("POST /api/auth/logout", func(response http.ResponseWriter, _ *http.Request) {
		http.SetCookie(response, &http.Cookie{Name: authCookieName, Value: "", Path: "/", MaxAge: -1, Expires: time.Unix(1, 0), HttpOnly: true, SameSite: http.SameSiteLaxMode})
		response.WriteHeader(http.StatusNoContent)
	})
	mux.Handle("POST /api/auth/session", auth.protect(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if !auth.required() {
			response.WriteHeader(http.StatusNoContent)
			return
		}
		token := authToken(request)
		expiresAt, ok := auth.tokenExpires(token)
		if !ok {
			http.Error(response, "authentication required", http.StatusUnauthorized)
			return
		}
		maxAge := int(time.Until(expiresAt) / time.Second)
		if maxAge < 1 {
			maxAge = 1
		}
		http.SetCookie(response, &http.Cookie{
			Name: authCookieName, Value: token, Path: "/", MaxAge: maxAge,
			Expires: expiresAt, HttpOnly: true, SameSite: http.SameSiteLaxMode,
		})
		response.WriteHeader(http.StatusNoContent)
	})))
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
		if auth.valid(authToken(request)) {
			next.ServeHTTP(response, request)
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
		response.Header().Set("Content-Security-Policy", "default-src 'self'; connect-src 'self'; img-src 'self'; style-src 'self'; script-src 'self'")
		response.Header().Set("Referrer-Policy", "no-referrer")
		response.Header().Set("X-Content-Type-Options", "nosniff")
		next.ServeHTTP(response, request)
	})
}
