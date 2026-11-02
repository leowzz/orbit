package coreconsole

import (
	"compress/gzip"
	"io"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

func TestEmbeddedSPADeepLinksAndAssets(t *testing.T) {
	handler := Handler()
	request := func(path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		return w
	}
	root := request("/")
	if root.Code != 200 {
		t.Fatalf("build console before Go tests: %s", root.Body.String())
	}
	for _, path := range []string{"/box", "/box/", "/agents", "/nodes", "/routes", "/system"} {
		w := request(path)
		if w.Code != 200 || w.Body.String() != root.Body.String() {
			t.Fatalf("SPA deep link %s failed", path)
		}
	}
	scripts := regexp.MustCompile(`src="([^"]+\.js)"`).FindAllStringSubmatch(root.Body.String(), -1)
	if len(scripts) == 0 {
		t.Fatal("index has no built JS bundle")
	}
	if root.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("HTML must revalidate after deployment")
	}
	for _, script := range scripts {
		w := request(script[1])
		if w.Code != 200 || strings.Contains(w.Header().Get("Content-Type"), "text/html") {
			t.Fatalf("embedded script missing: %s", script[1])
		}
		if w.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" {
			t.Fatal("hashed asset is not cached")
		}
		request := httptest.NewRequest("GET", script[1], nil)
		request.Header.Set("Accept-Encoding", "br, gzip")
		compressed := httptest.NewRecorder()
		handler.ServeHTTP(compressed, request)
		if compressed.Header().Get("Content-Encoding") != "gzip" {
			t.Fatal("missing precompressed bundle")
		}
		reader, err := gzip.NewReader(compressed.Body)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(reader)
		reader.Close()
		if err != nil || string(body) != w.Body.String() {
			t.Fatal("compressed bundle differs")
		}
		request.Header.Set("Accept-Encoding", "gzip;q=0, *;q=1")
		disabled := httptest.NewRecorder()
		handler.ServeHTTP(disabled, request)
		if disabled.Header().Get("Content-Encoding") != "" {
			t.Fatal("gzip opt-out ignored")
		}
	}
	for _, path := range []string{"/assets/old-hash.js", "/api/unknown", "/.gitkeep", "/.vite/manifest.json", scripts[0][1] + ".gz"} {
		if w := request(path); w.Code != 404 {
			t.Fatalf("missing resource %s was replaced by HTML", path)
		}
	}
}
