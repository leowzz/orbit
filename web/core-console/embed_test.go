package coreconsole

import (
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
	for _, path := range []string{"/agents", "/nodes", "/routes", "/system"} {
		w := request(path)
		if w.Code != 200 || w.Body.String() != root.Body.String() {
			t.Fatalf("SPA deep link %s failed", path)
		}
	}
	scripts := regexp.MustCompile(`src="([^"]+\.js)"`).FindAllStringSubmatch(root.Body.String(), -1)
	if len(scripts) == 0 {
		t.Fatal("index has no built JS bundle")
	}
	for _, script := range scripts {
		w := request(script[1])
		if w.Code != 200 || strings.Contains(w.Header().Get("Content-Type"), "text/html") {
			t.Fatalf("embedded script missing: %s", script[1])
		}
	}
	for _, path := range []string{"/assets/old-hash.js", "/api/unknown", "/.gitkeep"} {
		if w := request(path); w.Code != 404 {
			t.Fatalf("missing resource %s was replaced by HTML", path)
		}
	}
}
