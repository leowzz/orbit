package web

import (
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func mountAuth(mux *http.ServeMux, auth *authManager) {
	mux.HandleFunc("GET /api/auth/config", func(response http.ResponseWriter, _ *http.Request) {
		writeJSON(response, map[string]bool{"required": auth.required()})
	})
	mux.HandleFunc("POST /api/auth/login", func(response http.ResponseWriter, request *http.Request) {
		if !sameOrigin(request) {
			http.Error(response, "forbidden", http.StatusForbidden)
			return
		}
		if !auth.required() {
			http.Error(response, "web authentication is not configured", http.StatusNotFound)
			return
		}
		if retry := auth.loginRetry(); retry > 0 {
			response.Header().Set("Retry-After", strconv.Itoa(int((retry+time.Second-1)/time.Second)))
			http.Error(response, "too many login attempts", http.StatusTooManyRequests)
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
			Expires: expiresAt, HttpOnly: true, Secure: request.TLS != nil || strings.EqualFold(request.Header.Get("X-Forwarded-Proto"), "https"), SameSite: http.SameSiteStrictMode,
		})
		writeJSON(response, map[string]string{"token": token, "expires_at": expiresAt.UTC().Format(time.RFC3339Nano)})
	})
	mux.HandleFunc("POST /api/auth/logout", func(response http.ResponseWriter, request *http.Request) {
		if !sameOrigin(request) {
			http.Error(response, "forbidden", http.StatusForbidden)
			return
		}
		auth.revoke(authToken(request))
		http.SetCookie(response, &http.Cookie{Name: authCookieName, Value: "", Path: "/", MaxAge: -1, Expires: time.Unix(1, 0), HttpOnly: true, Secure: request.TLS != nil || strings.EqualFold(request.Header.Get("X-Forwarded-Proto"), "https"), SameSite: http.SameSiteStrictMode})
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
			Expires: expiresAt, HttpOnly: true, Secure: request.TLS != nil || strings.EqualFold(request.Header.Get("X-Forwarded-Proto"), "https"), SameSite: http.SameSiteStrictMode,
		})
		response.WriteHeader(http.StatusNoContent)
	})))
}
