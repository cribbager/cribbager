package server

import (
	"fmt"
	"net/http"
	"runtime"
	"strings"
)

// handleMetrics serves Prometheus text-format metrics for scraping by Fly
// metrics / Grafana / an external Prometheus.
//
// It is gated by the same bearer token as GET /stats (statsToken): when that's
// configured, runtime internals and live occupancy aren't exposed publicly.
// Empty (dev) leaves it open, exactly like /stats.
//
// The exposition is hand-written rather than pulling in prometheus/client_golang:
// the format is trivial (# HELP / # TYPE / name{labels} value lines) and keeping
// the dependency footprint minimal is a deliberate project value.
func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if s.statsToken != "" && r.Header.Get("Authorization") != "Bearer "+s.statsToken {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	games, subscribers := s.Stats()

	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	uptime := s.now().Sub(s.startTime).Seconds()

	r2 := s.reqs2xx.Load()
	r3 := s.reqs3xx.Load()
	r4 := s.reqs4xx.Load()
	r5 := s.reqs5xx.Load()
	rOther := s.reqsOther.Load()

	var b strings.Builder

	gauge := func(name, help string, value float64) {
		fmt.Fprintf(&b, "# HELP %s %s\n", name, help)
		fmt.Fprintf(&b, "# TYPE %s gauge\n", name)
		fmt.Fprintf(&b, "%s %g\n", name, value)
	}
	counter := func(name, help string, value int64) {
		fmt.Fprintf(&b, "# HELP %s %s\n", name, help)
		fmt.Fprintf(&b, "# TYPE %s counter\n", name)
		fmt.Fprintf(&b, "%s %d\n", name, value)
	}

	// HTTP requests, one counter with a status-class label (low cardinality: no
	// raw paths, no game ids). Sum across classes == total requests handled.
	b.WriteString("# HELP cribbager_http_requests_total Total HTTP requests handled, by response status class.\n")
	b.WriteString("# TYPE cribbager_http_requests_total counter\n")
	fmt.Fprintf(&b, "cribbager_http_requests_total{class=\"2xx\"} %d\n", r2)
	fmt.Fprintf(&b, "cribbager_http_requests_total{class=\"3xx\"} %d\n", r3)
	fmt.Fprintf(&b, "cribbager_http_requests_total{class=\"4xx\"} %d\n", r4)
	fmt.Fprintf(&b, "cribbager_http_requests_total{class=\"5xx\"} %d\n", r5)
	fmt.Fprintf(&b, "cribbager_http_requests_total{class=\"other\"} %d\n", rOther)

	// Application state (reuses the same counts as GET /stats).
	gauge("cribbager_games_live", "Number of live game sessions currently in memory.", float64(games))
	gauge("cribbager_sse_subscribers", "Number of live SSE stream subscribers across all games.", float64(subscribers))
	gauge("cribbager_uptime_seconds", "Process uptime in seconds.", uptime)

	// Go runtime.
	gauge("go_goroutines", "Number of goroutines that currently exist.", float64(runtime.NumGoroutine()))
	gauge("go_memstats_heap_alloc_bytes", "Bytes of allocated heap objects.", float64(mem.HeapAlloc))
	gauge("go_memstats_sys_bytes", "Bytes of memory obtained from the OS.", float64(mem.Sys))
	counter("go_memstats_gc_total", "Number of completed GC cycles.", int64(mem.NumGC))

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.Write([]byte(b.String()))
}
