package appapi

import (
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"orbit/internal/inbox"
	"orbit/nodes/web/inboxui"
)

const browserCookie = "orbit_inbox"

// Cookie authorization is restricted to same-origin browser requests. Native
// clients keep using Bearer tokens; no CORS or query-string credentials exist.
func browserOriginAllowed(r *http.Request) bool {
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		return false
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host == r.Host
	}
	return true
}

func (s *Server) browserRoutes(mux *http.ServeMux) {
	mux.Handle("GET /inbox/", inboxui.Handler())
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/inbox/", http.StatusSeeOther) })
	mux.HandleFunc("GET /api/v1/browser", func(w http.ResponseWriter, r *http.Request) {
		write(w, 200, map[string]string{"mode": "device"})
	})
	mux.HandleFunc("POST /api/v1/session", func(w http.ResponseWriter, r *http.Request) {
		if !browserOriginAllowed(r) {
			failure(w, &inbox.Fault{Code: "forbidden"})
			return
		}
		var input struct {
			Token string `json:"token"`
		}
		if !decodeAdmin(w, r, &input) {
			return
		}
		// Never let an existing cookie rescue an invalid replacement token.
		r.Header.Set("Authorization", "Bearer "+strings.TrimSpace(input.Token))
		s.auth(func(w http.ResponseWriter, r *http.Request, node string) {
			setBrowserCookie(w, r, strings.TrimSpace(input.Token), 30*24*60*60)
			write(w, 200, map[string]string{"node_id": node})
		})(w, r)
	})
	mux.HandleFunc("DELETE /api/v1/session", func(w http.ResponseWriter, r *http.Request) {
		if !browserOriginAllowed(r) {
			failure(w, &inbox.Fault{Code: "forbidden"})
			return
		}
		setBrowserCookie(w, r, "", -1)
		w.WriteHeader(http.StatusNoContent)
	})
}

func setBrowserCookie(w http.ResponseWriter, r *http.Request, token string, maxAge int) {
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	local := host == "localhost" || (net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback())
	http.SetCookie(w, &http.Cookie{Name: browserCookie, Value: token, Path: "/api/v1", HttpOnly: true, Secure: !local || r.TLS != nil, SameSite: http.SameSiteStrictMode, MaxAge: maxAge, Expires: time.Now().Add(time.Duration(maxAge) * time.Second)})
}
