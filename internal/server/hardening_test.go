package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestValidEmail(t *testing.T) {
	valid := []string{"a@b.com", "user.name@example.co", "x@y.io", "a@b.c"}
	invalid := []string{"a@b.", "a@.com", "nope", "a@b", "@b.com", "a b@c.com", "a@b.c\n"}
	for _, e := range valid {
		if !validEmail(e) {
			t.Errorf("validEmail(%q) = false, want true", e)
		}
	}
	for _, e := range invalid {
		if validEmail(e) {
			t.Errorf("validEmail(%q) = true, want false", e)
		}
	}
}

func TestMemAuthReapExpired(t *testing.T) {
	m := NewMemAuthStore()
	now := time.Now()
	m.CreateSession(Session{ID: "live", UserID: "u", ExpiresAt: now.Add(time.Hour)})
	m.CreateSession(Session{ID: "dead", UserID: "u", ExpiresAt: now.Add(-time.Hour)})
	m.CreateResetToken(ResetToken{ID: "rlive", UserID: "u", ExpiresAt: now.Add(time.Hour)})
	m.CreateResetToken(ResetToken{ID: "rdead", UserID: "u", ExpiresAt: now.Add(-time.Hour)})

	if err := m.ReapExpired(now); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := m.SessionByID("dead"); ok {
		t.Error("expired session not reaped")
	}
	if _, ok, _ := m.SessionByID("live"); !ok {
		t.Error("live session wrongly reaped")
	}
	if _, ok, _ := m.ResetTokenByID("rdead"); ok {
		t.Error("expired reset token not reaped")
	}
	if _, ok, _ := m.ResetTokenByID("rlive"); !ok {
		t.Error("live reset token wrongly reaped")
	}
}

func TestStatsTokenGate(t *testing.T) {
	srv := New()
	srv.SetStatsToken("secret")
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/stats")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("/stats without token: got %d, want 401", resp.StatusCode)
	}

	req, _ := http.NewRequest("GET", ts.URL+"/stats", nil)
	req.Header.Set("Authorization", "Bearer secret")
	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Errorf("/stats with token: got %d, want 200", resp2.StatusCode)
	}
}
