package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"testing"
)

// TestLoggedInGameCarriesPlayerID: a game created while logged in records the
// account's id and display name on the creator's seat, overriding any typed name.
// (Guest creation — no cookie — keeps the typed name and an empty player id, as
// the other create tests cover.)
func TestLoggedInGameCarriesPlayerID(t *testing.T) {
	store := NewMemStore()
	srv := New()
	srv.SetStore(store)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}

	post := func(path string, body any) *http.Response {
		t.Helper()
		b, _ := json.Marshal(body)
		r, err := client.Post(ts.URL+path, "application/json", bytes.NewReader(b))
		if err != nil {
			t.Fatal(err)
		}
		return r
	}

	// Sign up — the cookie jar captures the session cookie for later requests.
	r := post("/auth/signup", map[string]string{
		"username": "alice", "email": "alice@example.com",
		"password": "password123", "display_name": "Alice A",
	})
	if r.StatusCode != http.StatusCreated {
		t.Fatalf("signup status %d", r.StatusCode)
	}
	var u userResponse
	json.NewDecoder(r.Body).Decode(&u)
	r.Body.Close()

	// Create a game while logged in, passing a DIFFERENT typed name; the account wins.
	r = post("/games", map[string]any{"mode": "open", "name": "ignored-guest-name"})
	if r.StatusCode != http.StatusCreated {
		t.Fatalf("create status %d", r.StatusCode)
	}
	var created createResponse
	json.NewDecoder(r.Body).Decode(&created)
	r.Body.Close()

	recs, err := store.LoadAll()
	if err != nil {
		t.Fatal(err)
	}
	var rec *Record
	for i := range recs {
		if recs[i].ID == created.GameID {
			rec = &recs[i]
		}
	}
	if rec == nil {
		t.Fatal("created game was not persisted")
	}
	if rec.PlayerIDs[0] != u.ID || u.ID == "" {
		t.Fatalf("seat 0 player id = %q, want the account id %q", rec.PlayerIDs[0], u.ID)
	}
	if rec.Names[0] != "Alice A" {
		t.Fatalf("seat 0 name = %q, want the account display name", rec.Names[0])
	}
}
