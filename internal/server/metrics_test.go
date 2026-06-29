package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestMetricsExposition hits GET /metrics and asserts the Prometheus text format
// is well-formed and that the request counters increment as requests are served.
func TestMetricsExposition(t *testing.T) {
	srv := New()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Generate a 2xx (healthz) and a 4xx (unknown game) so both class counters move.
	http.Get(ts.URL + "/healthz")
	http.Get(ts.URL + "/games/nope")

	resp, err := http.Get(ts.URL + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/metrics: got %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("content-type = %q, want text/plain...", ct)
	}

	// Exposition structure: HELP + TYPE lines and the metric families we publish.
	for _, want := range []string{
		"# HELP cribbager_http_requests_total",
		"# TYPE cribbager_http_requests_total counter",
		`cribbager_http_requests_total{class="2xx"}`,
		`cribbager_http_requests_total{class="4xx"}`,
		`cribbager_http_requests_total{class="5xx"}`,
		"cribbager_games_live ",
		"cribbager_sse_subscribers ",
		"cribbager_uptime_seconds ",
		"go_goroutines ",
		"go_memstats_heap_alloc_bytes ",
		"go_memstats_sys_bytes ",
		"go_memstats_gc_total ",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics body missing %q\n---\n%s", want, body)
		}
	}

	// The 2xx and 4xx we issued must be counted (value > 0, not the bare "0").
	if strings.Contains(body, `cribbager_http_requests_total{class="2xx"} 0`) {
		t.Error("expected a non-zero 2xx request count")
	}
	if strings.Contains(body, `cribbager_http_requests_total{class="4xx"} 0`) {
		t.Error("expected a non-zero 4xx request count")
	}
}

// TestMetricsTokenGate confirms GET /metrics honors the same bearer token as
// /stats: 401 without it when configured, 200 with it.
func TestMetricsTokenGate(t *testing.T) {
	srv := New()
	srv.SetStatsToken("secret")
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("/metrics without token: got %d, want 401", resp.StatusCode)
	}

	req, _ := http.NewRequest("GET", ts.URL+"/metrics", nil)
	req.Header.Set("Authorization", "Bearer secret")
	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Errorf("/metrics with token: got %d, want 200", resp2.StatusCode)
	}
}

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	var sb strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(buf)
		sb.Write(buf[:n])
		if err != nil {
			break
		}
	}
	return sb.String()
}
