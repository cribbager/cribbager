package server

import (
	"bytes"
	"database/sql"
	"encoding/gob"
)

// PgResultStore is the Postgres-backed ResultStore using the shared pool (OpenPg):
// one row per finished game, with the event log gob-encoded into a BYTEA column for
// future replay, and per-player indexes for history queries.
type PgResultStore struct {
	db *sql.DB
}

// resultMeta is the small bundle of code-version metadata gob-encoded into the
// results.meta column: the engine version and per-seat bot info. Keeping it in one
// blob (rather than a column per field) makes adding future metadata additive.
type resultMeta struct {
	EngineVersion string
	Bots          [2]BotInfo
}

// NewPgResultStore ensures the results table and its indexes exist on the shared
// pool and returns the store.
func NewPgResultStore(db *sql.DB) (*PgResultStore, error) {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS results (
			id          TEXT PRIMARY KEY,
			player_id_0 TEXT,
			player_id_1 TEXT,
			name_0      TEXT NOT NULL,
			name_1      TEXT NOT NULL,
			score_0     INTEGER NOT NULL,
			score_1     INTEGER NOT NULL,
			winner      INTEGER NOT NULL,
			events      BYTEA,
			ended_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
			meta        BYTEA
		)`,
		// Additive migration for tables created before the meta column existed.
		`ALTER TABLE results ADD COLUMN IF NOT EXISTS meta BYTEA`,
		`CREATE INDEX IF NOT EXISTS results_player0 ON results (player_id_0, ended_at DESC)`,
		`CREATE INDEX IF NOT EXISTS results_player1 ON results (player_id_1, ended_at DESC)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			return nil, err
		}
	}
	return &PgResultStore{db: db}, nil
}

func (p *PgResultStore) SaveResult(r Result) error {
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(r.Events); err != nil {
		return err
	}
	var metaBuf bytes.Buffer
	if err := gob.NewEncoder(&metaBuf).Encode(resultMeta{EngineVersion: r.EngineVersion, Bots: r.Bots}); err != nil {
		return err
	}
	ctx, cancel := dbCtx()
	defer cancel()
	_, err := p.db.ExecContext(ctx, `
		INSERT INTO results
			(id, player_id_0, player_id_1, name_0, name_1, score_0, score_1, winner, events, ended_at, meta)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		ON CONFLICT (id) DO NOTHING`,
		r.ID, nullStr(r.PlayerIDs[0]), nullStr(r.PlayerIDs[1]),
		r.Names[0], r.Names[1], r.Scores[0], r.Scores[1], r.Winner, buf.Bytes(), r.EndedAt, metaBuf.Bytes())
	return err
}

func (p *PgResultStore) ResultsForPlayer(playerID string, limit int) ([]Result, error) {
	ctx, cancel := dbCtx()
	defer cancel()
	rows, err := p.db.QueryContext(ctx, `
		SELECT id, player_id_0, player_id_1, name_0, name_1, score_0, score_1, winner, ended_at, meta
		FROM results
		WHERE player_id_0 = $1 OR player_id_1 = $1
		ORDER BY ended_at DESC
		LIMIT $2`, playerID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Result
	for rows.Next() {
		var r Result
		var p0, p1 sql.NullString
		var meta []byte
		if err := rows.Scan(&r.ID, &p0, &p1, &r.Names[0], &r.Names[1],
			&r.Scores[0], &r.Scores[1], &r.Winner, &r.EndedAt, &meta); err != nil {
			return nil, err
		}
		r.PlayerIDs[0], r.PlayerIDs[1] = p0.String, p1.String
		if len(meta) > 0 { // NULL for rows written before the meta column existed
			var m resultMeta
			if err := gob.NewDecoder(bytes.NewReader(meta)).Decode(&m); err != nil {
				return nil, err
			}
			r.EngineVersion, r.Bots = m.EngineVersion, m.Bots
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ResultsForPlayerWithEvents loads all of a player's finished games with their
// gob-encoded event logs decoded, so stats can replay the scoring events. No
// limit (callers aggregate the whole set); version metadata is not needed here.
func (p *PgResultStore) ResultsForPlayerWithEvents(playerID string) ([]Result, error) {
	ctx, cancel := dbCtx()
	defer cancel()
	rows, err := p.db.QueryContext(ctx, `
		SELECT id, player_id_0, player_id_1, name_0, name_1, score_0, score_1, winner, events, ended_at
		FROM results
		WHERE player_id_0 = $1 OR player_id_1 = $1`, playerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Result
	for rows.Next() {
		var r Result
		var p0, p1 sql.NullString
		var events []byte
		if err := rows.Scan(&r.ID, &p0, &p1, &r.Names[0], &r.Names[1],
			&r.Scores[0], &r.Scores[1], &r.Winner, &events, &r.EndedAt); err != nil {
			return nil, err
		}
		r.PlayerIDs[0], r.PlayerIDs[1] = p0.String, p1.String
		if len(events) > 0 {
			if err := gob.NewDecoder(bytes.NewReader(events)).Decode(&r.Events); err != nil {
				return nil, err
			}
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ResultByID loads one finished game by id, decoding the gob-encoded event log
// (and version metadata) so the post-game analysis can replay it. ok=false maps
// the no-rows case to a clean miss rather than an error.
func (p *PgResultStore) ResultByID(id string) (Result, bool, error) {
	ctx, cancel := dbCtx()
	defer cancel()
	var r Result
	var p0, p1 sql.NullString
	var events, meta []byte
	err := p.db.QueryRowContext(ctx, `
		SELECT id, player_id_0, player_id_1, name_0, name_1, score_0, score_1, winner, events, ended_at, meta
		FROM results
		WHERE id = $1`, id).Scan(&r.ID, &p0, &p1, &r.Names[0], &r.Names[1],
		&r.Scores[0], &r.Scores[1], &r.Winner, &events, &r.EndedAt, &meta)
	if err == sql.ErrNoRows {
		return Result{}, false, nil
	}
	if err != nil {
		return Result{}, false, err
	}
	r.PlayerIDs[0], r.PlayerIDs[1] = p0.String, p1.String
	if len(events) > 0 {
		if err := gob.NewDecoder(bytes.NewReader(events)).Decode(&r.Events); err != nil {
			return Result{}, false, err
		}
	}
	if len(meta) > 0 { // NULL for rows written before the meta column existed
		var m resultMeta
		if err := gob.NewDecoder(bytes.NewReader(meta)).Decode(&m); err != nil {
			return Result{}, false, err
		}
		r.EngineVersion, r.Bots = m.EngineVersion, m.Bots
	}
	return r, true, nil
}

func (p *PgResultStore) PlayerStats(playerID string) (total, wins int, err error) {
	ctx, cancel := dbCtx()
	defer cancel()
	err = p.db.QueryRowContext(ctx, `
		SELECT COUNT(*),
		       COUNT(*) FILTER (WHERE (player_id_0 = $1 AND winner = 0)
		                           OR (player_id_1 = $1 AND winner = 1))
		FROM results
		WHERE player_id_0 = $1 OR player_id_1 = $1`, playerID).Scan(&total, &wins)
	return total, wins, err
}

// nullStr maps a guest seat's empty id to SQL NULL so the per-player WHERE clauses
// never match it (and guests don't all collide on "").
func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}
