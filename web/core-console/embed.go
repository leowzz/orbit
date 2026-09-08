// Package coreconsole embeds the independently built React application.
package coreconsole

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

// Build with make build-core (or pnpm build in this directory) before go build.
//
//go:embed all:dist
var assets embed.FS

func Handler() http.Handler {
	root, _ := fs.Sub(assets, "dist")
	files := http.FileServer(http.FS(root))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}
		name := strings.TrimPrefix(r.URL.Path, "/")
		if strings.HasPrefix(name, ".") || strings.Contains(name, "/.") {
			http.NotFound(w, r)
			return
		}
		if name != "" {
			if info, err := fs.Stat(root, name); err == nil && !info.IsDir() {
				files.ServeHTTP(w, r)
				return
			}
		}
		if strings.HasPrefix(name, "assets/") || strings.Contains(name, ".") {
			http.NotFound(w, r)
			return
		}
		html, err := fs.ReadFile(root, "index.html")
		if err != nil {
			http.Error(w, "Core console assets are missing. Run make build-console before building Core.", 503)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(html)
	})
}
