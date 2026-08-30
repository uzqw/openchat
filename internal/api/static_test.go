package api_test

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"openchat/internal/api"
)

// withWebDir points the API at a temp built-frontend directory.
func withWebDir(t *testing.T, files map[string]string) envOpt {
	return func(c *api.Config) {
		dir := t.TempDir()
		for name, content := range files {
			p := filepath.Join(dir, name)
			if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
				t.Fatalf("web dir: %v", err)
			}
			if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
				t.Fatalf("web file: %v", err)
			}
		}
		c.WebDir = dir
	}
}

// TestStaticServing pins the P4 integration: the built frontend is served
// under the same global Basic Auth as the API, unknown client routes fall
// back to the SPA shell, /api* never reaches the shell (JSON 404), and
// /api/health stays public.
func TestStaticServing(t *testing.T) {
	webFiles := map[string]string{
		"index.html":    "<html><body>app shell</body></html>",
		"assets/app.js": "console.log('asset ok')",
	}
	e := newEnv(t, withWebDir(t, webFiles), withAuth("u", "p"))

	// 1. static files require auth like every other route
	status, body, err := e.do("GET", "/", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusUnauthorized {
		t.Fatalf("GET / without auth: got %d, want 401", status)
	}
	if !strings.Contains(string(body), "authentication required") {
		t.Fatalf("401 body not the JSON envelope: %s", body)
	}

	// 2. authed requests serve real assets verbatim
	status, body, err = e.do("GET", "/index.html", nil, basic("u", "p"))
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusOK || !strings.Contains(string(body), "app shell") {
		t.Fatalf("GET /index.html: got %d %q", status, body)
	}
	status, body, err = e.do("GET", "/assets/app.js", nil, basic("u", "p"))
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusOK || !strings.Contains(string(body), "asset ok") {
		t.Fatalf("GET /assets/app.js: got %d %q", status, body)
	}

	// 3. SPA fallback: client-only routes served with the app shell
	for _, path := range []string{"/history", "/settings", "/history/c1"} {
		status, body, err = e.do("GET", path, nil, basic("u", "p"))
		if err != nil {
			t.Fatal(err)
		}
		if status != http.StatusOK || !strings.Contains(string(body), "app shell") {
			t.Fatalf("GET %s (SPA fallback): got %d %q", path, status, body)
		}
	}

	// 4. unknown API paths are a JSON 404, never the SPA shell
	status, body, err = e.do("GET", "/api/does-not-exist", nil, basic("u", "p"))
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusNotFound || strings.Contains(string(body), "app shell") {
		t.Fatalf("GET /api/does-not-exist: got %d %q", status, body)
	}
	var env struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &env); err != nil || env.Error.Code != "not_found" {
		t.Fatalf("unknown API path not a JSON envelope: %d %s", status, body)
	}

	// 5. health remains the only public route
	status, _, err = e.do("GET", "/api/health", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusOK {
		t.Fatalf("GET /api/health (public): got %d, want 200", status)
	}
}

// TestStaticServingRequiresWebDir keeps the pre-P4 behavior when no web
// dir is configured: unknown paths are plain 404s and the shell is not
// served.
func TestStaticServingRequiresWebDir(t *testing.T) {
	e := newEnv(t, withAuth("u", "p"), withExecPath(fakePath))
	status, body, err := e.do("GET", "/", nil, basic("u", "p"))
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusNotFound || strings.Contains(string(body), "app shell") {
		t.Fatalf("GET / without WebDir: got %d %q", status, body)
	}
}

// TestStaticServingTraversalSafe probes that a path attempting to escape
// the web root never leaks files outside it (the SPA shell or a 404 is
// all a client can ever see).
func TestStaticServingTraversalSafe(t *testing.T) {
	secret := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(secret, []byte("top secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	e := newEnv(t, withWebDir(t, map[string]string{"index.html": "<html>shell</html>"}), withAuth("u", "p"))

	for _, path := range []string{"/../secret.txt", "/%2e%2e/secret.txt", "/..%2fsecret.txt"} {
		status, body, err := e.do("GET", path, nil, basic("u", "p"))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(body), "top secret") {
			t.Fatalf("path traversal leaked %q: %d %s", path, status, body)
		}
	}
}
