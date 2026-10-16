package web

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"orbit/nodes/web/inboxui"
)

// InboxHandler is the Web Node entry point. No display or host actions are mounted.
func InboxHandler(cfg AuthConfig, staticDir string) (http.Handler, error) {
	if strings.TrimSpace(cfg.Password) == "" || cfg.SessionTTL <= 0 || cfg.InboxURL == "" || cfg.InboxToken == "" {
		return nil, fmt.Errorf("web inbox requires an upstream URL, device token, password and positive session TTL")
	}
	upstream, err := url.Parse(cfg.InboxURL)
	if err != nil || upstream.Host == "" || upstream.User != nil || (upstream.Scheme != "https" && upstream.Scheme != "http") {
		return nil, fmt.Errorf("invalid web inbox upstream URL")
	}
	mux := http.NewServeMux()
	auth := newAuthManager(cfg)
	mountAuth(mux, auth)
	mountInbox(mux, auth, cfg, true)
	if staticDir != "" {
		handler, err := inboxui.DevelopmentHandler(staticDir)
		if err != nil {
			return nil, err
		}
		// The longer pattern overrides the embedded asset handler.
		mux.Handle("GET /inbox/{asset}", handler)
		mux.Handle("GET /inbox/{$}", handler)
	}
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		http.Redirect(w, r, "/inbox/", http.StatusSeeOther)
	})
	return securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !sameOrigin(r) && strings.HasPrefix(r.URL.Path, "/api/") {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		mux.ServeHTTP(w, r)
	})), nil
}

func sameOrigin(r *http.Request) bool {
	if site := r.Header.Get("Sec-Fetch-Site"); site == "cross-site" || site == "same-site" {
		return false
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		scheme := "http"
		if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
			scheme = "https"
		}
		return err == nil && u.Scheme == scheme && u.Host == r.Host && u.User == nil && u.RawQuery == "" && u.Fragment == "" && u.Path == ""
	}
	return true
}
