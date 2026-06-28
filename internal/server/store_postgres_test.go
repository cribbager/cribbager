package server

import (
	"database/sql"
	"os"
	"reflect"
	"testing"

	"github.com/cribbager/cribbager/internal/game"
)

// openTestDB opens the shared pool against the integration DSN and closes it when
// the test ends. The gated Postgres tests use it instead of each owning a pool.
func openTestDB(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	db, err := OpenPg(dsn)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// TestPgStoreRoundTrip is an integration test gated on TEST_DATABASE_URL (a
// Postgres DSN, e.g. from docker-compose or CI). It exercises the real
// save / upsert / load / restore / delete cycle against the database.
func TestPgStoreRoundTrip(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL to run the Postgres integration test")
	}
	pg, err := NewPgStore(openTestDB(t, dsn))
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	if _, err := pg.db.Exec("DELETE FROM games"); err != nil {
		t.Fatalf("clean table: %v", err)
	}

	g := game.New(game.Options{Deck: game.NewSeededDeck(3)})
	rec := Record{
		ID:     "pg-test-game",
		Game:   g.Snapshot(),
		Tokens: [2]string{"tokA", "tokB"},
		Names:  [2]string{"Alice", "Bob"},
		Bots:   [2]bool{false, true},
	}
	if err := pg.Save(rec); err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := pg.Save(rec); err != nil { // upsert path must not error/duplicate
		t.Fatalf("re-save: %v", err)
	}

	recs, err := pg.LoadAll()
	if err != nil {
		t.Fatalf("loadall: %v", err)
	}
	var got *Record
	for i := range recs {
		if recs[i].ID == rec.ID {
			got = &recs[i]
		}
	}
	if got == nil {
		t.Fatalf("game not found after save (got %d records)", len(recs))
	}
	if got.Tokens != rec.Tokens || got.Names != rec.Names || got.Bots != rec.Bots {
		t.Fatalf("metadata mismatch after round-trip: %+v", got)
	}

	// The persisted game must fold back to identical state.
	a := game.Restore(rec.Game, game.NewSeededDeck(1))
	b := game.Restore(got.Game, game.NewSeededDeck(2))
	if a.Version() != b.Version() || a.Scores() != b.Scores() {
		t.Fatalf("restored game differs after round-trip")
	}
	if !reflect.DeepEqual(a.View(game.Seat0), b.View(game.Seat0)) {
		t.Fatal("restored view mismatch after round-trip")
	}

	if err := pg.Delete(rec.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	recs2, _ := pg.LoadAll()
	for _, r := range recs2 {
		if r.ID == rec.ID {
			t.Fatal("game still present after delete")
		}
	}
}
