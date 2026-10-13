package server

import (
	"net"
	"net/http"
	"strings"
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

// clientIP is the originating client address for rate-limiting.
//
// In production the app listens on loopback only and is reached through a
// Cloudflare Tunnel, so every connection's peer is cloudflared on 127.0.0.1 and
// the real client is in CF-Connecting-IP, which Cloudflare always sets. That
// header is honored ONLY when the peer is loopback: a client that reached the app
// any other way could set it to anything, and trusting it would let them present a
// fresh value per request and bypass every limiter.
//
// Fly-Client-IP is deliberately not honored: it was only unforgeable behind fly's
// proxy, whereas Cloudflare forwards unknown request headers untouched. Nor is
// X-Forwarded-For, which is client-supplied and unauthenticated.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		if v := strings.TrimSpace(r.Header.Get("CF-Connecting-IP")); net.ParseIP(v) != nil {
			return v
		}
	}
	return host
}
