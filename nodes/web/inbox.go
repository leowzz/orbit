package web

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"time"

	"orbit/nodes/web/inboxui"
)

func mountInbox(mux *http.ServeMux, auth *authManager, cfg AuthConfig, inboxOnly bool) {
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
		Transport: transport,
		ModifyResponse: func(response *http.Response) error {
			if !inboxOnly || response.Request.URL.Path != "/api/v1/status" || response.StatusCode != http.StatusOK {
				return nil
			}
			// The inbox needs device identity, never a display/session projection.
			defer response.Body.Close()
			var identity struct {
				NodeID string `json:"node_id"`
				Label  string `json:"label"`
			}
			if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&identity); err != nil {
				return err
			}
			data, err := json.Marshal(identity)
			if err != nil {
				return err
			}
			response.Body = io.NopCloser(bytes.NewReader(data))
			response.ContentLength = int64(len(data))
			response.Header.Set("Content-Length", strconv.Itoa(len(data)))
			response.Header.Set("Content-Type", "application/json")
			return nil
		},
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
			if !sameOrigin(r) {
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
