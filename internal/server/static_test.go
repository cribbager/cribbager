package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// staticServer builds a server serving a temp static dir that contains a marker
// game.html, and returns the httptest server.
func staticServer(t *testing.T) *httptest.Server {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "game.html"), []byte("<title>Cribbager</title>GAME_PAGE"), 0o600); err != nil {
		t.Fatal(err)
	}
	srv := New()
	srv.ServeStatic(dir)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts
}

// TestGamePageRouteServesGameHTML checks the clean per-game URL /game/{id} serves
// the game client (game.html) so a hard refresh at that path loads the app instead
// of 404ing — the resume-on-refresh fix depends on it.
func TestGamePageRouteServesGameHTML(t *testing.T) {
	ts := staticServer(t)

	resp, err := ts.Client().Get(ts.URL + "/game/deadbeefdeadbeef")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /game/<id>: status %d, want 200", resp.StatusCode)
	}
	body := make([]byte, 4096)
	n, _ := resp.Body.Read(body)
	if !strings.Contains(string(body[:n]), "GAME_PAGE") {
		t.Fatalf("GET /game/<id> did not serve game.html; body: %q", body[:n])
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "no-cache" {
		t.Fatalf("Cache-Control = %q, want no-cache", cc)
	}
}

// TestGamePageRouteWithoutStatic confirms the route 404s (rather than panicking)
// when no static dir is configured — the route is only registered with static, but
// this guards the handler's own nil-static guard.
func TestGamePageRouteWithoutStatic(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/game/abc", nil)
	New().handleGamePage(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("handleGamePage with no static: status %d, want 404", rec.Code)
	}
}
