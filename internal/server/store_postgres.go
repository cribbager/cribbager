package server

import (
	"bytes"
	"database/sql"
	"encoding/gob"
)

// PgStore is a Postgres-backed Store. A session's full Record is gob-encoded into
// a single row's BYTEA column — the engine is event-sourced and the log is small,
// so a full upsert per change is cheap and keeps restore trivial (decode one row).
// It uses the shared pool opened by OpenPg (not its own), so connection count stays
// bounded across all the stores.
type PgStore struct {
	db *sql.DB
}

// NewPgStore ensures the schema exists on the shared pool and returns the store.
func NewPgStore(db *sql.DB) (*PgStore, error) {
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS games (
			id         TEXT PRIMARY KEY,
			state      BYTEA NOT NULL,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`); err != nil {
		return nil, err
	}
	return &PgStore{db: db}, nil
}

// Save upserts the record's full state (write-through on every change).
func (p *PgStore) Save(r Record) error {
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(r); err != nil {
		return err
	}
	ctx, cancel := dbCtx()
	defer cancel()
	_, err := p.db.ExecContext(ctx, `
		INSERT INTO games (id, state, updated_at) VALUES ($1, $2, now())
		ON CONFLICT (id) DO UPDATE SET state = EXCLUDED.state, updated_at = now()`,
		r.ID, buf.Bytes())
	return err
}

// LoadAll reads every stored game back into Records (called once at boot).
func (p *PgStore) LoadAll() ([]Record, error) {
	ctx, cancel := dbCtx()
	defer cancel()
	rows, err := p.db.QueryContext(ctx, `SELECT state FROM games`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Record
	for rows.Next() {
		var blob []byte
		if err := rows.Scan(&blob); err != nil {
			return nil, err
		}
		var r Record
		if err := gob.NewDecoder(bytes.NewReader(blob)).Decode(&r); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Delete removes a game (on reap).
func (p *PgStore) Delete(id string) error {
	ctx, cancel := dbCtx()
	defer cancel()
	_, err := p.db.ExecContext(ctx, `DELETE FROM games WHERE id = $1`, id)
	return err
}

// Ping verifies the database is reachable, backing the /readyz check. It satisfies
// the optional pinger interface the server type-asserts.
func (p *PgStore) Ping() error {
	ctx, cancel := dbCtx()
	defer cancel()
	return p.db.PingContext(ctx)
}
