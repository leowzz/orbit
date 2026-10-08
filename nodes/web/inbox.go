package web

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"orbit/nodes/web/inboxui"
)

func mountInbox(mux *http.ServeMux, auth *authManager, cfg AuthConfig) {
	mux.Handle("GET /inbox/", inboxui.Handler())
	mux.HandleFunc("GET /api/v1/browser", func(w http.ResponseWriter, r *http.Request) {
		mode := "unavailable"
		if cfg.InboxURL != "" && cfg.InboxToken != "" && auth.required() {
			mode = "gateway"
		}
		writeJSON(w, map[string]string{"mode": mode})
	})
	if cfg.InboxURL == "" || cfg.InboxToken == "" || !auth.required() {
		return
	}
	upstream, err := url.Parse(cfg.InboxURL)
	if err != nil {
		return
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = 15 * time.Second
	proxy := &httputil.ReverseProxy{
		Rewrite: func(p *httputil.ProxyRequest) {
			p.SetURL(upstream)
			p.Out.Header.Del("Cookie")
			p.Out.Header.Del("Origin")
			p.Out.Header.Del("Referer")
			p.Out.Header.Set("Authorization", "Bearer "+cfg.InboxToken)
		},
		Transport:     transport,
		FlushInterval: -1,
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(`{"code":"unavailable"}`))
		},
	}
	// Only the personal API is forwarded. In particular, this cannot reach Core
	// administration or exchange browser credentials with the upstream service.
	for _, pattern := range []string{"GET /api/v1/status", "GET /api/v1/sync/snapshot", "GET /api/v1/changes", "GET /api/v1/events", "POST /api/v1/operations", "POST /api/v1/attachments", "GET /api/v1/attachments/{id}"} {
		mux.Handle(pattern, auth.protect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			u, err := url.Parse(origin)
			if r.Header.Get("Sec-Fetch-Site") == "cross-site" || (origin != "" && (err != nil || u.Host != r.Host || (u.Scheme != "http" && u.Scheme != "https"))) {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			if strings.HasPrefix(r.URL.Path, "/api/v1/attachments") {
				r.Body = http.MaxBytesReader(w, r.Body, 11<<20)
			} else {
				r.Body = http.MaxBytesReader(w, r.Body, 32<<10)
			}
			w.Header().Set("Cache-Control", "no-store")
			proxy.ServeHTTP(w, r)
		})))
	}
}
