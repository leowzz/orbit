// Package coreconsole embeds the independently built React application.
package coreconsole

import (
	"bytes"
	"embed"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"
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
		if strings.HasPrefix(name, ".") || strings.Contains(name, "/.") || strings.HasSuffix(name, ".gz") {
			http.NotFound(w, r)
			return
		}
		if name != "" {
			if info, err := fs.Stat(root, name); err == nil && !info.IsDir() {
				if strings.HasPrefix(name, "assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				} else {
					w.Header().Set("Cache-Control", "no-cache")
				}
				w.Header().Add("Vary", "Accept-Encoding")
				if acceptsGzip(r.Header.Get("Accept-Encoding")) {
					if compressed, err := fs.ReadFile(root, name+".gz"); err == nil {
						w.Header().Set("Content-Encoding", "gzip")
						w.Header().Set("Content-Type", mime.TypeByExtension(path.Ext(name)))
						http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(compressed))
						return
					}
				}
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
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(html)
	})
}

func acceptsGzip(header string) bool {
	wildcard := false
	for _, part := range strings.Split(header, ",") {
		fields := strings.Split(strings.TrimSpace(part), ";")
		encoding := strings.TrimSpace(fields[0])
		quality := 1.0
		for _, parameter := range fields[1:] {
			name, value, ok := strings.Cut(strings.TrimSpace(parameter), "=")
			if ok && name == "q" {
				parsed, err := strconv.ParseFloat(value, 64)
				if err != nil || parsed < 0 || parsed > 1 {
					quality = 0
				} else {
					quality = parsed
				}
			}
		}
		if strings.EqualFold(encoding, "gzip") {
			return quality > 0
		}
		if encoding == "*" {
			wildcard = quality > 0
		}
	}
	return wildcard
}
