package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClientIP(t *testing.T) {
	req := func(remote string, headers map[string]string) *http.Request {
		r := httptest.NewRequest("POST", "/", nil)
		r.RemoteAddr = remote
		for k, v := range headers {
			r.Header.Set(k, v)
		}
		return r
	}
	cases := []struct {
		name    string
		remote  string
		headers map[string]string
		want    string
	}{
		{"tunnel (loopback) trusts CF-Connecting-IP",
			"127.0.0.1:5555", map[string]string{"CF-Connecting-IP": "203.0.113.7"}, "203.0.113.7"},
		{"tunnel over IPv6 loopback trusts CF-Connecting-IP",
			"[::1]:5555", map[string]string{"CF-Connecting-IP": "2001:db8::1"}, "2001:db8::1"},
		{"loopback without the header falls back to the peer",
			"127.0.0.1:5555", nil, "127.0.0.1"},
		{"loopback with a malformed header falls back to the peer",
			"127.0.0.1:5555", map[string]string{"CF-Connecting-IP": "not-an-ip"}, "127.0.0.1"},
		// A client that reaches the app directly (not via the tunnel) can set any
		// header it likes, so CF-Connecting-IP is ignored unless the peer is loopback.
		{"non-loopback peer cannot forge CF-Connecting-IP",
			"10.0.0.1:5555", map[string]string{"CF-Connecting-IP": "203.0.113.7"}, "10.0.0.1"},
		// Fly-Client-IP was only safe behind fly's proxy. Cloudflare passes unknown
		// headers through, so it is now client-controlled and must be ignored.
		{"Fly-Client-IP is ignored even from loopback",
			"127.0.0.1:5555", map[string]string{"Fly-Client-IP": "1.2.3.4"}, "127.0.0.1"},
		{"Fly-Client-IP is ignored from other peers",
			"10.0.0.1:5555", map[string]string{"Fly-Client-IP": "1.2.3.4"}, "10.0.0.1"},
		// X-Forwarded-For is client-supplied and unauthenticated; never trusted.
		{"X-Forwarded-For is ignored",
			"127.0.0.1:5555", map[string]string{"X-Forwarded-For": "9.9.9.9"}, "127.0.0.1"},
		{"X-Forwarded-For is ignored alongside CF-Connecting-IP",
			"127.0.0.1:5555", map[string]string{"X-Forwarded-For": "9.9.9.9", "CF-Connecting-IP": "203.0.113.7"}, "203.0.113.7"},
		{"peer without a port still resolves", "10.0.0.1", nil, "10.0.0.1"},
	}
	for _, c := range cases {
		if got := clientIP(req(c.remote, c.headers)); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
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
