package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeDist creates a fake frontend build like the one Vite produces.
func writeDist(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"index.html":           "<!doctype html><title>poker</title>",
		"assets/index-abc.js":  "console.log('app')",
		"assets/index-abc.css": "body{}",
		"img/logo.svg":         "<svg/>",
	}
	for name, body := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func get(t *testing.T, url string) (int, http.Header, string) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, resp.Header, string(body)
}

func TestServesFrontend(t *testing.T) {
	srv := httptest.NewServer(New(Options{StaticDir: writeDist(t)}))
	defer srv.Close()

	tests := []struct {
		path      string
		status    int
		bodyHas   string
		cacheHas  string
		typePrefx string
	}{
		{"/", http.StatusOK, "<title>poker</title>", "no-cache", "text/html"},
		{"/?room=K7MQ2X", http.StatusOK, "<title>poker</title>", "no-cache", "text/html"},
		{"/index.html", http.StatusMovedPermanently, "", "", ""},
		{"/assets/index-abc.js", http.StatusOK, "console.log", "immutable", "text/javascript"},
		{"/assets/index-abc.css", http.StatusOK, "body{}", "immutable", "text/css"},
		{"/assets/", http.StatusNotFound, "", "", ""},
		{"/assets/missing.js", http.StatusNotFound, "", "", ""},
		{"/img/logo.svg", http.StatusOK, "<svg/>", "no-cache", "image/svg+xml"},
		{"/img/", http.StatusNotFound, "", "", ""},
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	for _, tt := range tests {
		resp, err := client.Get(srv.URL + tt.path)
		if err != nil {
			t.Fatalf("GET %s: %v", tt.path, err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != tt.status {
			t.Errorf("GET %s: status %d, want %d", tt.path, resp.StatusCode, tt.status)
			continue
		}
		if !strings.Contains(string(body), tt.bodyHas) {
			t.Errorf("GET %s: body %q, want it to contain %q", tt.path, body, tt.bodyHas)
		}
		if got := resp.Header.Get("Cache-Control"); !strings.Contains(got, tt.cacheHas) {
			t.Errorf("GET %s: Cache-Control %q, want %q", tt.path, got, tt.cacheHas)
		}
		if got := resp.Header.Get("Content-Type"); !strings.HasPrefix(got, tt.typePrefx) {
			t.Errorf("GET %s: Content-Type %q, want %q", tt.path, got, tt.typePrefx)
		}
	}
	if cc := cacheControlOf(t, srv.URL+"/assets/missing.js"); cc != "" {
		t.Errorf("404 for a missing asset must not be cached forever, Cache-Control %q", cc)
	}
}

func cacheControlOf(t *testing.T, url string) string {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	resp.Body.Close()
	return resp.Header.Get("Cache-Control")
}

// The mux cleans "/../x" and redirects to "/x", so files outside the
// frontend directory can never be reached.
func TestStaticCannotEscapeDir(t *testing.T) {
	srv := httptest.NewServer(New(Options{StaticDir: writeDist(t)}))
	defer srv.Close()
	for _, p := range []string{"/../go.mod", "/assets/../../go.mod", "/%2e%2e/go.mod"} {
		status, _, body := get(t, srv.URL+p)
		if status != http.StatusNotFound || strings.Contains(body, "module") {
			t.Errorf("GET %s = %d %q, want 404 without file contents", p, status, body)
		}
	}
}

func TestAPIStillWorksWithFrontend(t *testing.T) {
	srv := httptest.NewServer(New(Options{StaticDir: writeDist(t), NextHandDelay: 0}))
	defer srv.Close()
	code := createRoom(t, srv)
	token := joinRoom(t, srv, code, "Ann")
	c := connect(t, srv, code, token)
	c.waitState("state over /ws", func(s *stateView) bool { return s.Players[0].Name == "Ann" })
}

func TestWithoutFrontendIndexIsPlainText(t *testing.T) {
	srv := httptest.NewServer(New(Options{}))
	defer srv.Close()
	status, _, body := get(t, srv.URL+"/")
	if status != http.StatusOK || body != "poker server\n" {
		t.Errorf("GET / = %d %q, want 200 \"poker server\\n\"", status, body)
	}
}
