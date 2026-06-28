package server

import (
	"context"
	"database/sql"
	"time"

	_ "github.com/lib/pq" // registers the database/sql "postgres" driver
)

// dbTimeout bounds a single database call. The hot-path write (persist) runs under
// the session mutex, so an unbounded query against a stalled DB would wedge that
// game and its SSE stream indefinitely; this degrades it to a logged failure that
// the next change re-saves.
const dbTimeout = 5 * time.Second

// dbCtx returns a context bounding one database call to dbTimeout. Callers must
// defer the cancel.
func dbCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), dbTimeout)
}

// OpenPg opens the single shared connection pool used by all the Postgres stores
// (PgStore, PgAuthStore, PgResultStore). One pool — not three — keeps total
// connection count bounded and predictable, and the limits below stop a request
// burst from exhausting Postgres's max_connections or the app machine's memory.
// Caller owns the returned *sql.DB and should Close it on shutdown.
func OpenPg(dsn string) (*sql.DB, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)
	db.SetConnMaxIdleTime(5 * time.Minute)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}
