// Package inboxui contains the personal inbox shared by Core and Web Node.
package inboxui

import (
	"embed"
	"fmt"
	"io/fs"
	"net/http"
	"os"
)

//go:embed static/*
var assets embed.FS

func Handler() http.Handler {
	root, _ := fs.Sub(assets, "static")
	return handler(root)
}

func DevelopmentHandler(directory string) (http.Handler, error) {
	root := os.DirFS(directory)
	if _, err := fs.Stat(root, "index.html"); err != nil {
		return nil, fmt.Errorf("open inbox assets: %w", err)
	}
	return handler(root), nil
}

func handler(root fs.FS) http.Handler {
	files := http.StripPrefix("/inbox/", http.FileServer(http.FS(root)))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; connect-src 'self'; img-src 'self' blob:; style-src 'self'; script-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		files.ServeHTTP(w, r)
	})
}
