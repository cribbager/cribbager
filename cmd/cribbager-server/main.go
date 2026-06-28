// Command cribbager-server runs the cribbage HTTP server.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/cribbager/cribbager/internal/server"
)

func main() {
	addr := ":8080"
	if v := os.Getenv("ADDR"); v != "" {
		addr = v
	}

	srv := server.New()
	// Durable persistence: with DATABASE_URL set, in-progress games are written
	// through to Postgres and reloaded at boot, so a deploy/restart doesn't drop
	// them. Without it, games live only in memory (fine for local/dev). Postgres
	// is portable (run it anywhere or in Docker) — no provider lock-in.
	if dsn := os.Getenv("DATABASE_URL"); dsn != "" {
		// One shared, connection-limited pool backs all three stores (games, auth,
		// results) so total connection count stays bounded.
		db, err := server.OpenPg(dsn)
		if err != nil {
			log.Fatalf("connect to database: %v", err)
		}
		pg, err := server.NewPgStore(db)
		if err != nil {
			log.Fatalf("init games store: %v", err)
		}
		srv.SetStore(pg)
		if err := srv.Restore(); err != nil {
			log.Fatalf("restore games from database: %v", err)
		}
		// Durable accounts/sessions and finished-game history share the same pool.
		pgAuth, err := server.NewPgAuthStore(db)
		if err != nil {
			log.Fatalf("init auth store: %v", err)
		}
		srv.SetAuthStore(pgAuth)
		pgResults, err := server.NewPgResultStore(db)
		if err != nil {
			log.Fatalf("init results store: %v", err)
		}
		srv.SetResultStore(pgResults)
		log.Print("persistence: Postgres (DATABASE_URL)")
	} else {
		log.Print("persistence: none (in-memory only; set DATABASE_URL to enable)")
	}

	// Session cookies must carry the Secure flag in production (https) but not over
	// plain http://localhost. Set SECURE_COOKIES=1 when deployed behind https.
	if os.Getenv("SECURE_COOKIES") != "" {
		srv.SetSecureCookies(true)
	}

	// STATS_TOKEN, when set, gates GET /stats behind that bearer token so live
	// occupancy isn't exposed publicly. Left unset, /stats stays open (dev).
	if t := os.Getenv("STATS_TOKEN"); t != "" {
		srv.SetStatsToken(t)
	}

	// Outgoing email (password-reset links). With SMTP_HOST set, send real mail via
	// SMTP; otherwise keep the default LogEmailer, which logs the reset link instead
	// of sending — so dev works with no mail server (run Mailpit via docker-compose
	// to catch resets locally). SMTP_USER is optional (Mailpit needs no auth).
	if host := os.Getenv("SMTP_HOST"); host != "" {
		port := os.Getenv("SMTP_PORT")
		if port == "" {
			port = "587"
		}
		from := os.Getenv("SMTP_FROM")
		if from == "" {
			from = "cribbager@localhost"
		}
		srv.SetEmailer(server.NewSMTPEmailer(host, port, os.Getenv("SMTP_USER"), os.Getenv("SMTP_PASS"), from))
		log.Printf("email: SMTP via %s:%s", host, port)
	} else {
		log.Print("email: SMTP unconfigured; reset links are logged only (set SMTP_HOST to send mail)")
	}

	// BASE_URL is the external origin used to build absolute reset links (e.g.
	// https://cribbager.fly.dev). When unset, links are derived per-request from the
	// Host header (honoring X-Forwarded-Proto).
	if base := os.Getenv("BASE_URL"); base != "" {
		srv.SetBaseURL(base)
		log.Printf("base URL for email links: %s", base)
	}

	// The web client is plain ES modules served raw (no build step). Default to
	// "web/public" — the directory that holds exactly the served content — so
	// `go run ./cmd/cribbager-server` just works; set WEB to override.
	dir := os.Getenv("WEB")
	if dir == "" {
		dir = "web/public"
	}
	if _, err := os.Stat(dir); err == nil {
		srv.ServeStatic(dir)
		log.Printf("serving web client from %s", dir)
	} else {
		log.Printf("web dir %q not found; serving API only", dir)
	}

	// Evict idle games periodically so the in-memory registry doesn't grow, and
	// log the active counts each cycle for basic visibility into load.
	go func() {
		for range time.Tick(5 * time.Minute) {
			if n := srv.Reap(2 * time.Hour); n > 0 {
				log.Printf("reaped %d idle games", n)
			}
			games, subscribers := srv.Stats()
			log.Printf("active games: %d, subscribers: %d", games, subscribers)
		}
	}()

	httpSrv := &http.Server{
		Addr:    addr,
		Handler: srv.Handler(),
		// Bound slow-client attacks. No WriteTimeout: the SSE stream is
		// long-lived, and a write deadline would sever it.
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	// On SIGINT/SIGTERM (e.g. a fly deploy/restart), shut down gracefully instead
	// of hard-killing in-flight games and SSE streams. RegisterOnShutdown wires
	// Shutdown to srv.Close, which signals every stream handler to return so the
	// clients' EventSources reconnect to the replacement instance.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	httpSrv.RegisterOnShutdown(srv.Close)

	serveErr := make(chan error, 1)
	go func() { serveErr <- httpSrv.ListenAndServe() }()

	log.Printf("cribbager-server listening on %s", addr)

	select {
	case err := <-serveErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	case <-ctx.Done():
		log.Print("shutdown signal received; draining connections")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpSrv.Shutdown(shutdownCtx); err != nil {
			log.Printf("graceful shutdown error: %v", err)
		}
	}
}
