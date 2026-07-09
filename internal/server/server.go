// Package server exposes the cribbage engine over HTTP: capability-token auth,
// a session registry, per-seat visibility-filtered snapshots, and a per-seat
// semantic delta stream over SSE. The engine never learns about tokens or
// transport — the server only ever serializes View(seat) and projected events.
package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cribbager/cribbager/internal/bot"
	"github.com/cribbager/cribbager/internal/cribbage"
	"github.com/cribbager/cribbager/internal/game"
)

// maxBodyBytes caps request bodies; the JSON DTOs here are tiny.
const maxBodyBytes = 1 << 16 // 64 KiB

// Server is the HTTP front end for the engine.
type Server struct {
	reg    *registry
	now    func() time.Time
	static string // optional directory of static web files to serve

	// subscribers is the current number of live SSE stream subscribers across all
	// sessions. subscribe increments it, unsubscribe decrements it; each session
	// holds a pointer to it (set in handleCreate) so the count survives the
	// session and is observable via Stats / GET /stats.
	subscribers atomic.Int64

	// shutdown is closed once by Close() to signal every live stream handler to
	// return promptly (e.g. on a deploy/restart), instead of being hard-killed.
	shutdown  chan struct{}
	closeOnce sync.Once

	// store is the durable backing for sessions; the registry is the hot cache.
	// Defaults to NoopStore (no durability); SetStore injects a real one.
	store Store

	// results is the permanent record of finished games (per-player history).
	// Defaults to NoopResultStore; SetResultStore injects a real one.
	results ResultStore

	// auth is the durable backing for accounts and login sessions. Defaults to an
	// in-memory MemAuthStore; SetAuthStore injects a real one (e.g. Postgres).
	auth AuthStore

	// secureCookies gates the Secure flag on the session cookie. It MUST be false
	// for plain http://localhost (a Secure cookie is never sent over http) and
	// true behind https in production. SetSecureCookies sets it.
	secureCookies bool

	// emailer sends transactional email (currently just password-reset links).
	// Defaults to LogEmailer (logs instead of sends) so dev works with no mail
	// server; SetEmailer injects a real SMTPEmailer.
	emailer Emailer

	// baseURL is the external origin used to build absolute links in emails (e.g.
	// the reset link). Empty means "derive scheme://host from the request"
	// (honoring X-Forwarded-Proto); SetBaseURL pins it explicitly.
	baseURL string

	// Per-IP rate limiters for the sensitive auth endpoints: loginLimiter guards
	// /auth/login (brute force), authLimiter guards signup + reset-request (abuse,
	// reset-email bombing). Their windows are swept periodically by Reap.
	loginLimiter *rateLimiter
	authLimiter  *rateLimiter

	// statsToken, when non-empty, gates GET /stats behind a bearer token so live
	// occupancy isn't exposed publicly. Empty (default) leaves /stats open for dev.
	// The same token also gates GET /metrics.
	statsToken string

	// startTime is when the server was constructed; GET /metrics reports process
	// uptime as now - startTime.
	startTime time.Time

	// HTTP request counters by response status class, incremented in logRequests
	// for every completed request and exposed via GET /metrics. Atomic because the
	// server serves concurrent requests.
	reqs2xx   atomic.Int64
	reqs3xx   atomic.Int64
	reqs4xx   atomic.Int64
	reqs5xx   atomic.Int64
	reqsOther atomic.Int64
}

// New builds a server with an empty session registry and no durable store.
func New() *Server {
	s := &Server{
		reg:       newRegistry(),
		now:       time.Now,
		shutdown:  make(chan struct{}),
		store:     NoopStore{},
		results:   NewMemResultStore(),
		auth:      NewMemAuthStore(),
		emailer:   LogEmailer{},
		startTime: time.Now(),
	}
	// Limiters read the clock through s.now so tests can drive time.
	nowFn := func() time.Time { return s.now() }
	s.loginLimiter = newRateLimiter(loginRateMax, loginRateWindow, nowFn)
	s.authLimiter = newRateLimiter(authRateMax, authRateWindow, nowFn)
	return s
}

// SetStore swaps in a durable Store (e.g. Postgres). Call before serving.
func (s *Server) SetStore(st Store) { s.store = st }

// SetResultStore swaps in a durable ResultStore (e.g. Postgres). Call before serving.
func (s *Server) SetResultStore(rs ResultStore) { s.results = rs }

// SetEmailer swaps in a real Emailer (e.g. SMTPEmailer). Call before serving.
func (s *Server) SetEmailer(e Emailer) { s.emailer = e }

// SetBaseURL pins the external origin used to build links in emails. Call before
// serving; leave unset to derive scheme://host from each request.
func (s *Server) SetBaseURL(u string) { s.baseURL = strings.TrimRight(u, "/") }

// SetAuthStore swaps in a durable AuthStore (e.g. Postgres). Call before serving.
func (s *Server) SetAuthStore(a AuthStore) { s.auth = a }

// SetSecureCookies toggles the Secure flag on the session cookie. Call before
// serving: true behind https, false for local http.
func (s *Server) SetSecureCookies(v bool) { s.secureCookies = v }

// SetStatsToken gates GET /stats behind a bearer token. Empty leaves it open.
func (s *Server) SetStatsToken(t string) { s.statsToken = t }

// Restore loads any persisted sessions from the store into the registry. Call
// once at boot, before serving, so in-progress games survive a restart.
func (s *Server) Restore() error {
	records, err := s.store.LoadAll()
	if err != nil {
		return err
	}
	dropped := 0
	for _, rec := range records {
		if !s.reg.add(sessionFromRecord(rec, &s.subscribers)) {
			dropped++ // over the session cap; left in the store, just not served
		}
	}
	if len(records) > 0 {
		log.Printf("restored %d games from store", len(records)-dropped)
	}
	if dropped > 0 {
		log.Printf("WARNING: %d restored games exceeded the %d-session cap and were not loaded (still in the DB)", dropped, maxSessions)
	}
	return nil
}

// persist writes the session's current state through to the store, logging (not
// failing) on error — the in-memory state is authoritative and the next change
// re-saves the full record. Must be called under sess.mu.
func (s *Server) persist(sess *session) {
	if err := s.store.Save(sess.record()); err != nil {
		log.Printf("persist %s: %v", sess.id, err)
	}
}

// Stats returns the current count of live sessions and live SSE subscribers. It
// is cheap and lock-light (a map-length read plus an atomic load), suitable for
// a periodic log or the GET /stats endpoint.
func (s *Server) Stats() (games, subscribers int) {
	return s.reg.count(), int(s.subscribers.Load())
}

// Close signals every live stream handler to return promptly. It is safe to call
// more than once (the channel is closed exactly once). Wire it via
// http.Server.RegisterOnShutdown so a graceful Shutdown unblocks the SSE loops.
func (s *Server) Close() {
	s.closeOnce.Do(func() { close(s.shutdown) })
}

// ServeStatic serves the given directory for any non-API GET, so the web client
// and the API share one origin.
func (s *Server) ServeStatic(dir string) { s.static = dir }

// pinger is the optional readiness interface a Store may implement (PgStore does).
// Stores without a backing dependency (Noop, Mem) don't, and are treated as ready.
type pinger interface{ Ping() error }

// handleReady is the readiness check: unlike /healthz (liveness — "the process is
// up"), it confirms the durable store is reachable, so a post-deploy smoke check
// can catch a build that boots but can't talk to its database. It deliberately is
// NOT the fly liveness check, so a transient DB blip doesn't trigger a restart.
func (s *Server) handleReady(w http.ResponseWriter, _ *http.Request) {
	if p, ok := s.store.(pinger); ok {
		if err := p.Ping(); err != nil {
			writeErr(w, http.StatusServiceUnavailable, "not ready: store unreachable")
			return
		}
	}
	w.Write([]byte("ready"))
}

// Handler returns the HTTP routes.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok")) })
	mux.HandleFunc("GET /readyz", s.handleReady)
	mux.HandleFunc("GET /stats", s.handleStats)
	mux.HandleFunc("GET /lobby", s.handleLobby)
	mux.HandleFunc("GET /bots", s.handleBots)
	mux.HandleFunc("GET /metrics", s.handleMetrics)
	mux.HandleFunc("POST /auth/signup", s.handleSignup)
	mux.HandleFunc("POST /auth/login", s.handleLogin)
	mux.HandleFunc("POST /auth/logout", s.handleLogout)
	mux.HandleFunc("GET /auth/me", s.handleMe)
	mux.HandleFunc("POST /auth/password/reset-request", s.handlePasswordResetRequest)
	mux.HandleFunc("POST /auth/password/reset", s.handlePasswordReset)
	mux.HandleFunc("GET /users/me/games", s.handleUserGames)
	mux.HandleFunc("GET /users/me/stats", s.handleUserStats)
	mux.HandleFunc("GET /users/me/games/{id}/analysis", s.handleGameAnalysis)
	mux.HandleFunc("GET /users/me/games/{id}/replay", s.handleGameReplay)
	mux.HandleFunc("POST /tools/discard-eval", s.handleDiscardEval)
	mux.HandleFunc("POST /tools/score-hand", s.handleScoreHand)
	mux.HandleFunc("POST /games", s.handleCreate)
	mux.HandleFunc("POST /games/{id}/join", s.handleJoin)
	mux.HandleFunc("GET /games/{id}", s.handleSnapshot)
	mux.HandleFunc("POST /games/{id}/actions", s.handleAction)
	mux.HandleFunc("POST /games/{id}/abandon", s.handleAbandon)
	mux.HandleFunc("GET /games/{id}/stream", s.handleStream)
	if s.static != "" {
		mux.Handle("GET /", noCache(http.FileServer(http.Dir(s.static))))
	}
	// logRequests is the outermost wrapper so it observes the final status (after
	// recoverPanics may have written a 500) and the full request lifetime.
	return s.logRequests(recoverPanics(mux))
}

// statusRecorder wraps an http.ResponseWriter to capture the status code for
// request logging. WriteHeader may never be called (net/http defaults to 200),
// so status starts at 200. It delegates Flush to the underlying writer so the
// SSE stream keeps working — handleStream type-asserts http.Flusher, and hiding
// Flush here would break streaming.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// Flush delegates to the underlying writer's Flusher so SSE streaming works
// through the wrapper. net/http's response writer implements http.Flusher.
func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// logRequests logs one line per completed request: method, path, status, and
// duration in ms. The long-lived SSE request logs once when it closes, with its
// lifetime as the duration — that's expected. It also tallies the response status
// into the per-class counters exposed via GET /metrics.
func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		s.countStatus(rec.status)
		log.Printf("%s %s %d %dms", r.Method, r.URL.Path, rec.status, time.Since(start).Milliseconds())
	})
}

// countStatus increments the request counter for the response's status class.
// Anything outside 2xx–5xx (e.g. a 1xx) lands in the "other" bucket so the total
// across all buckets always equals the number of requests handled.
func (s *Server) countStatus(code int) {
	switch {
	case code >= 200 && code < 300:
		s.reqs2xx.Add(1)
	case code >= 300 && code < 400:
		s.reqs3xx.Add(1)
	case code >= 400 && code < 500:
		s.reqs4xx.Add(1)
	case code >= 500:
		s.reqs5xx.Add(1)
	default:
		s.reqsOther.Add(1)
	}
}

// handleStats reports counts only — the number of live games and live SSE
// subscribers — with no game data, so it is safe to expose unauthenticated.
func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	if s.statsToken != "" && r.Header.Get("Authorization") != "Bearer "+s.statsToken {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	games, subscribers := s.Stats()
	writeJSON(w, http.StatusOK, statsResponse{Games: games, Subscribers: subscribers})
}

// handleLobby lists the joinable public open games: those a host created as
// public ("Create a game") that are still waiting for an opponent. Private
// ("Challenge a friend") games, bot games, and games that have filled, finished,
// or been abandoned are excluded — the filter is by each session's current state,
// so the list always reflects reality (reaping removes stale sessions entirely).
// No auth: guests can browse the lobby before joining or signing in.
func (s *Server) handleLobby(w http.ResponseWriter, _ *http.Request) {
	games := []lobbyGame{}
	for _, sess := range s.reg.snapshot() {
		sess.mu.Lock()
		if sess.joinableLobby() {
			games = append(games, lobbyGame{
				GameID:    sess.id,
				HostName:  sess.names[game.Seat0],
				CreatedAt: sess.createdAt,
				OpenSeat:  game.Seat1,
			})
		}
		sess.mu.Unlock()
	}
	// Newest first, tie-broken by id, so the order is stable and deterministic.
	sort.Slice(games, func(i, j int) bool {
		if !games[i].CreatedAt.Equal(games[j].CreatedAt) {
			return games[i].CreatedAt.After(games[j].CreatedAt)
		}
		return games[i].GameID < games[j].GameID
	})
	writeJSON(w, http.StatusOK, lobbyResponse{Games: games})
}

// handleBots lists the production bots a "bot" game can be created against, and
// the default used when the create request omits a name. It reads a static
// in-process table, so it needs no auth and no locking — a client (CLI or web)
// can call it to populate a bot picker or validate a --bot flag.
func (s *Server) handleBots(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, botsResponse{Bots: bot.Names(), Default: bot.DefaultName})
}

// noCache makes the browser revalidate static assets on every load. The web
// client has no build step, so without this the browser keeps serving a stale
// cached ES module after an edit — and "edit a file and just refresh" silently
// wouldn't pick up the change. "no-cache" still allows efficient 304s via the
// FileServer's Last-Modified/ETag; it only forbids using a cached copy unchecked.
func noCache(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		next.ServeHTTP(w, r)
	})
}

// recoverPanics turns an unexpected panic into a 500 instead of a dropped
// connection, and logs it. http.ErrAbortHandler is re-raised so net/http can
// handle its own intentional aborts.
func recoverPanics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				if rec == http.ErrAbortHandler {
					panic(rec)
				}
				log.Printf("panic serving %s %s: %v\n%s", r.Method, r.URL.Path, rec, debug.Stack())
				w.WriteHeader(http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// decodeJSON reads a size-limited JSON body into v, writing a 400 and returning
// false on failure.
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeErr(w, http.StatusBadRequest, "bad request body")
		return false
	}
	return true
}

// seatIdentity fills a seat's display name from the request, preferring a
// logged-in user's account (its id + display name) over a typed guest name.
// Guests keep "" for the player id. Call before the session is shared (create) or
// under sess.mu (join).
func (s *Server) seatIdentity(sess *session, seat game.Seat, r *http.Request, typedName string) {
	if u, ok := s.currentUser(r); ok {
		sess.playerIDs[seat] = u.ID
		sess.names[seat] = u.DisplayName
	} else {
		sess.names[seat] = cleanName(typedName)
	}
}

func (s *Server) handleCreate(w http.ResponseWriter, r *http.Request) {
	var req createRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	target := req.Target
	if target <= 0 {
		target = 121
	}

	sess := &session{
		id:        newToken()[:16],
		game:      game.New(game.Options{Deck: cryptoDeck{}, TargetScore: target}),
		createdAt: s.now(),
		lastSeen:  s.now(),
		subCnt:    &s.subscribers,
	}
	sess.tokens[game.Seat0] = newToken()
	s.seatIdentity(sess, game.Seat0, r, req.Name)

	resp := createResponse{GameID: sess.id, PlayerToken: sess.tokens[game.Seat0], Seat: game.Seat0}

	switch req.Mode {
	case "bot":
		// The opponent is a named production bot (GET /bots lists them); an empty
		// name defaults to the champion. An unknown name is a clean 400 — lab
		// challengers are never seatable here. Validate before registering the
		// session so a bad request costs nothing.
		name := req.Bot
		if name == "" {
			name = bot.DefaultName
		}
		if !bot.Valid(name) {
			writeErr(w, http.StatusBadRequest, fmt.Sprintf("unknown bot %q; valid bots: %v", name, bot.Names()))
			return
		}
		sess.bots[game.Seat1] = newBot(name)
		sess.mu.Lock()
		sess.driveBots() // let the bot take any opening action (e.g. its discard)
		sess.mu.Unlock()
	case "open":
		// An open game claims seat 0 and leaves seat 1 unclaimed. The game id is
		// the join credential — anyone with it can take the open seat (single-join
		// is enforced by seat occupancy, not a separate token). public is opt-in:
		// set, the game is listed in the lobby ("Create a game"); unset (the
		// default), it stays private/link-only ("Challenge a friend").
		sess.public = req.Public
	default:
		writeErr(w, http.StatusBadRequest, `mode must be "bot" or "open"`)
		return
	}

	if !s.reg.add(sess) {
		// At capacity: don't register the session we just built (a tiny bit of
		// work like game.New already ran, but it's discarded here, not stored).
		writeErr(w, http.StatusServiceUnavailable, "server at capacity, try again later")
		return
	}
	sess.mu.Lock()
	s.persist(sess)
	sess.mu.Unlock()
	writeJSON(w, http.StatusCreated, resp)
}

func (s *Server) handleJoin(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.reg.get(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, "game not found")
		return
	}
	var req joinRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	sess.mu.Lock()
	defer sess.mu.Unlock()
	// The game id alone is the join credential; single-join is enforced by seat
	// occupancy. Seat 1 is taken if it holds a player token (a human already
	// joined) or a bot (a vs-bot game), so a join then yields 409.
	if sess.tokens[game.Seat1] != "" || sess.bots[game.Seat1] != nil {
		writeErr(w, http.StatusConflict, "seat is taken")
		return
	}
	sess.tokens[game.Seat1] = newToken()
	s.seatIdentity(sess, game.Seat1, r, req.Name)
	sess.lastSeen = s.now()
	// The opponent joined: bump the roster and wake the host's stream so it
	// learns the new player (name + presence).
	sess.rosterVer++
	sess.notify()
	s.persist(sess)
	writeJSON(w, http.StatusOK, joinResponse{PlayerToken: sess.tokens[game.Seat1], Seat: game.Seat1})
}

func (s *Server) handleSnapshot(w http.ResponseWriter, r *http.Request) {
	sess, seat, ok := s.authed(w, r)
	if !ok {
		return
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	sess.lastSeen = s.now()
	writeJSON(w, http.StatusOK, sess.game.View(seat))
}

func (s *Server) handleAction(w http.ResponseWriter, r *http.Request) {
	sess, seat, ok := s.authed(w, r)
	if !ok {
		return
	}
	var req actionRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	sess.mu.Lock()
	defer sess.mu.Unlock()

	if req.ExpectedVersion != nil && *req.ExpectedVersion != sess.game.Version() {
		writeErr(w, http.StatusConflict, "version conflict")
		return
	}

	cmd, err := command(req)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}

	baseSeq := sess.game.Version()
	if _, err := sess.game.Apply(seat, cmd); err != nil {
		writeErr(w, statusForGameErr(err), err.Error())
		return
	}
	sess.driveBots()
	sess.lastSeen = s.now()
	sess.notify()
	s.persist(sess) // write-through: the move (and any bot replies) are now durable
	if _, over := sess.game.Winner(); over {
		s.recordResult(sess) // permanent history on game-over (idempotent in the store)
	}

	deltas := projectEvents(seat, sess.game.Events()[baseSeq:], baseSeq)
	writeJSON(w, http.StatusOK, actionResponse{Version: sess.game.Version(), Deltas: deltas})
}

// handleAbandon marks the caller's seat as having deliberately left the game —
// distinct from a transient disconnect. It does NOT mutate game state or declare
// a winner: abandoning isn't a win. The other player's stream is woken so it
// receives an updated roster showing the seat as Left. Idempotent: abandoning an
// already-left seat is a no-op that still returns 204.
func (s *Server) handleAbandon(w http.ResponseWriter, r *http.Request) {
	sess, seat, ok := s.authed(w, r)
	if !ok {
		return
	}
	sess.mu.Lock()
	sess.left[seat] = true
	sess.rosterVer++
	sess.lastSeen = s.now()
	sess.notify()
	s.persist(sess)
	sess.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

// handleStream is the SSE delta stream. The client may resume from a sequence
// via the Last-Event-ID header or ?since=N; the server replays from there, then
// pushes new deltas as they occur, with periodic heartbeats.
func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	sess, seat, ok := s.authedStream(w, r)
	if !ok {
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}

	last := resumeFrom(r)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher.Flush()

	id, ch := sess.subscribe(seat)
	defer sess.unsubscribe(id, seat)

	heartbeat := time.NewTicker(25 * time.Second)
	defer heartbeat.Stop()

	// lastRoster is the roster version this connection last sent; -1 forces the
	// players delta to be sent once on connect.
	lastRoster := -1

	// send drains any pending roster change and game events to this stream.
	// The "players" delta is current-state, so it is written WITHOUT an id:
	// line (it must never disturb the client's Last-Event-ID resume cursor);
	// only game-event deltas carry id: <seq>.
	// send returns false if a write to the client failed, so the caller can return
	// and shed the dead connection promptly (rather than waiting on the context).
	send := func() bool {
		sess.mu.Lock()
		sess.lastSeen = s.now()
		var roster *Delta
		if sess.rosterVer != lastRoster {
			r := sess.roster()
			roster = &r
			lastRoster = sess.rosterVer
		}
		events := sess.game.Events()
		// Clamp a cursor that's ahead of the log (a bogus/stale ?since= or
		// Last-Event-ID): treat it as caught up and resume from the current end,
		// rather than silently stranding the client receiving nothing.
		if last > len(events) {
			last = len(events)
		}
		var deltas []Delta
		if last < len(events) {
			deltas = projectEvents(seat, events[last:], last)
			last = len(events)
		}
		sess.mu.Unlock()

		wrote := false
		if roster != nil {
			b, _ := json.Marshal(roster)
			if _, err := fmt.Fprintf(w, "data: %s\n\n", b); err != nil {
				return false
			}
			wrote = true
		}
		for _, d := range deltas {
			b, _ := json.Marshal(d)
			if _, err := fmt.Fprintf(w, "id: %d\ndata: %s\n\n", d.Seq, b); err != nil {
				return false
			}
			wrote = true
		}
		if wrote {
			flusher.Flush()
		}
		return true
	}

	if !send() { // deliver the current roster + catch up on connect
		return
	}
	for {
		select {
		case <-r.Context().Done():
			return
		case <-s.shutdown:
			// The server is shutting down; return so the client's EventSource
			// reconnects to the replacement instance. The deferred
			// sess.unsubscribe still runs.
			return
		case <-ch:
			if !send() {
				return
			}
		case <-heartbeat.C:
			sess.mu.Lock()
			sess.lastSeen = s.now()
			sess.mu.Unlock()
			// A failed heartbeat write means the client is gone; shed it.
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// --- helpers ------------------------------------------------------------------

func (s *Server) authed(w http.ResponseWriter, r *http.Request) (*session, game.Seat, bool) {
	sess, ok := s.reg.get(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, "game not found")
		return nil, 0, false
	}
	seat, ok := sess.seatFor(bearer(r))
	if !ok {
		writeErr(w, http.StatusUnauthorized, "missing or invalid token")
		return nil, 0, false
	}
	return sess, seat, true
}

// authedStream authorizes the SSE stream. Browsers' EventSource cannot set the
// Authorization header, so the player token may also arrive as ?token=<token>;
// the Bearer header takes precedence when present. seatFor keeps the comparison
// constant-time.
func (s *Server) authedStream(w http.ResponseWriter, r *http.Request) (*session, game.Seat, bool) {
	sess, ok := s.reg.get(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, "game not found")
		return nil, 0, false
	}
	token := bearer(r)
	if token == "" {
		token = r.URL.Query().Get("token")
	}
	seat, ok := sess.seatFor(token)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "missing or invalid token")
		return nil, 0, false
	}
	return sess, seat, true
}

func bearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if t, ok := strings.CutPrefix(h, "Bearer "); ok {
		return t
	}
	return ""
}

// cleanName trims a display name and caps it at 24 runes; empty is allowed.
// Truncating by runes (not bytes) avoids splitting a multi-byte UTF-8 sequence,
// which would otherwise ship mojibake.
func cleanName(name string) string {
	name = strings.TrimSpace(name)
	if r := []rune(name); len(r) > 24 {
		name = string(r[:24])
	}
	return name
}

// resumeFrom reads the resume point from the Last-Event-ID header or ?since=N.
// The result is clamped to >= 0 so a negative value can never index events[last:]
// out of bounds in the stream loop.
func resumeFrom(r *http.Request) int {
	since := 0
	if v := r.Header.Get("Last-Event-ID"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			since = n
		}
	} else if n, err := strconv.Atoi(r.URL.Query().Get("since")); err == nil {
		since = n
	}
	if since < 0 {
		since = 0
	}
	return since
}

func command(req actionRequest) (game.Command, error) {
	switch req.Type {
	case "discard":
		if len(req.Cards) != 2 {
			return nil, errors.New("discard needs exactly two cards")
		}
		return game.Discard{Cards: [2]cribbage.Card{req.Cards[0], req.Cards[1]}}, nil
	case "play":
		if req.Card == nil {
			return nil, errors.New("play needs a card")
		}
		return game.Play{Card: *req.Card}, nil
	default:
		return nil, fmt.Errorf("unknown action type %q", req.Type)
	}
}

// statusForGameErr maps engine errors to HTTP status codes: turn/phase/version
// conflicts are 409; an otherwise illegal move is 422.
func statusForGameErr(err error) int {
	switch {
	case errors.Is(err, game.ErrNotYourTurn),
		errors.Is(err, game.ErrWrongPhase),
		errors.Is(err, game.ErrAlreadyDiscarded),
		errors.Is(err, game.ErrGameOver):
		return http.StatusConflict
	default:
		return http.StatusUnprocessableEntity
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// Reap evicts sessions idle longer than ttl. Call periodically.
func (s *Server) Reap(ttl time.Duration) int {
	reaped := s.reg.reap(ttl, s.now())
	for _, id := range reaped {
		if err := s.store.Delete(id); err != nil {
			log.Printf("store delete %s: %v", id, err)
		}
	}
	// Piggyback the periodic maintenance: drop elapsed rate-limit windows and
	// expired session / reset-token rows so those tables stay bounded.
	s.loginLimiter.sweep()
	s.authLimiter.sweep()
	if err := s.auth.ReapExpired(s.now()); err != nil {
		log.Printf("reap expired auth rows: %v", err)
	}
	return len(reaped)
}
