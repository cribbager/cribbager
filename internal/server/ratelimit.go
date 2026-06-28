package server

import (
	"net"
	"net/http"
	"sync"
	"time"
)

// Rate-limit budgets for the sensitive auth endpoints (per client IP). Login is
// tighter (brute-force defense); signup and reset-request are looser but still
// bounded (account-creation abuse, reset-email bombing).
const (
	loginRateMax    = 15
	loginRateWindow = 10 * time.Minute
	authRateMax     = 10
	authRateWindow  = time.Hour
)

// rateLimiter is a simple fixed-window per-key limiter: at most max events per
// window per key. In-memory and mutex-guarded; sweep() drops elapsed windows so
// the map stays bounded.
type rateLimiter struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	hits   map[string]*hitWindow
	now    func() time.Time
}

type hitWindow struct {
	start time.Time
	count int
}

func newRateLimiter(max int, window time.Duration, now func() time.Time) *rateLimiter {
	return &rateLimiter{max: max, window: window, hits: map[string]*hitWindow{}, now: now}
}

// allow records an event for key and reports whether it is within the limit.
func (r *rateLimiter) allow(key string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	t := r.now()
	w := r.hits[key]
	if w == nil || t.Sub(w.start) >= r.window {
		r.hits[key] = &hitWindow{start: t, count: 1}
		return true
	}
	w.count++
	return w.count <= r.max
}

// sweep removes windows whose period has fully elapsed, bounding memory. Called
// periodically by the server's maintenance loop.
func (r *rateLimiter) sweep() {
	r.mu.Lock()
	defer r.mu.Unlock()
	t := r.now()
	for k, w := range r.hits {
		if t.Sub(w.start) >= r.window {
			delete(r.hits, k)
		}
	}
}

// clientIP is the originating client address for rate-limiting. It trusts only
// Fly-Client-IP, which the fly proxy sets and overwrites (a client can't forge it
// end-to-end), then falls back to RemoteAddr. It deliberately does NOT honor
// X-Forwarded-For: that header is client-supplied and unauthenticated, so trusting
// it would let anyone present a fresh value per request and bypass every limiter.
func clientIP(r *http.Request) string {
	if v := r.Header.Get("Fly-Client-IP"); v != "" {
		return v
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}
