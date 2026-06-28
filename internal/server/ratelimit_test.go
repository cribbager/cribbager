package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClientIP(t *testing.T) {
	// Fly-Client-IP (set by the trusted proxy) wins over everything.
	r := httptest.NewRequest("POST", "/", nil)
	r.Header.Set("Fly-Client-IP", "1.2.3.4")
	r.Header.Set("X-Forwarded-For", "9.9.9.9")
	r.RemoteAddr = "10.0.0.1:5555"
	if got := clientIP(r); got != "1.2.3.4" {
		t.Errorf("with Fly-Client-IP: got %q, want 1.2.3.4", got)
	}
	// Without it, client-supplied X-Forwarded-For must be IGNORED (anti-spoof) and
	// we fall back to RemoteAddr's host.
	r2 := httptest.NewRequest("POST", "/", nil)
	r2.Header.Set("X-Forwarded-For", "9.9.9.9")
	r2.RemoteAddr = "10.0.0.1:5555"
	if got := clientIP(r2); got != "10.0.0.1" {
		t.Errorf("X-Forwarded-For must not be trusted: got %q, want 10.0.0.1", got)
	}
}

func TestRateLimiter(t *testing.T) {
	now := time.Now()
	rl := newRateLimiter(3, time.Minute, func() time.Time { return now })

	for i := 1; i <= 3; i++ {
		if !rl.allow("ip1") {
			t.Fatalf("attempt %d should be allowed", i)
		}
	}
	if rl.allow("ip1") {
		t.Fatal("4th attempt should be denied")
	}
	if !rl.allow("ip2") {
		t.Fatal("a different key must have its own budget")
	}

	// After the window elapses, the key resets.
	now = now.Add(time.Minute + time.Second)
	if !rl.allow("ip1") {
		t.Fatal("after the window, ip1 should be allowed again")
	}

	// sweep drops fully-elapsed windows.
	now = now.Add(2 * time.Minute)
	rl.sweep()
	rl.mu.Lock()
	n := len(rl.hits)
	rl.mu.Unlock()
	if n != 0 {
		t.Fatalf("after sweep, hits = %d, want 0", n)
	}
}

// TestLoginRateLimited: a burst of login attempts from one client is throttled —
// after loginRateMax attempts the next returns 429 (before any password work).
func TestLoginRateLimited(t *testing.T) {
	c := newTestClient(t)
	var last int
	for i := 0; i <= loginRateMax; i++ {
		resp, _ := c.do("POST", "/auth/login", "", loginRequest{Username: "ghost", Password: "whatever123"})
		last = resp.StatusCode
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("after %d attempts, got %d, want 429", loginRateMax+1, last)
	}
}

// TestResetConsumeRateLimited: brute-forcing reset tokens at /auth/password/reset
// is throttled the same way — after authRateMax bad attempts the next is 429.
func TestResetConsumeRateLimited(t *testing.T) {
	c := newTestClient(t)
	var last int
	for i := 0; i <= authRateMax; i++ {
		resp, _ := c.do("POST", "/auth/password/reset", "", resetRequest{Token: "nope", Password: "newpassword456"})
		last = resp.StatusCode
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("after %d attempts, got %d, want 429", authRateMax+1, last)
	}
}
