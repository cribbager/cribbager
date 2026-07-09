package server

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"math/big"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cribbager/cribbager/internal/bot"
	"github.com/cribbager/cribbager/internal/cribbage"
	"github.com/cribbager/cribbager/internal/game"
)

// session is one live game plus everything the transport needs: per-seat tokens,
// any bot seats, the SSE subscribers, and a mutex that serializes all access
// (the engine is not concurrency-safe). A session is self-contained and
// addressed by its id, so the future scale path is sharding by id.
type session struct {
	id   string
	mu   sync.Mutex
	game *game.Game

	tokens    [2]string // player_token per seat ("" if unclaimed)
	bots      [2]bot.Bot
	names     [2]string // display name per seat (account name if logged in, else the typed name)
	playerIDs [2]string // account id per seat ("" for a guest seat)

	// public marks an open game as listable in the lobby (GET /lobby). It is
	// opt-in at create time: "Create a game" sets it true; "Challenge a friend"
	// (a private, link-only open game) and bot games leave it false. Only a public
	// open game still waiting for an opponent is ever listed.
	public    bool
	createdAt time.Time // when the session was created (lobby "age")

	lastSeen time.Time

	subs      map[int]chan struct{}
	nextID    int
	subCount  [2]int  // live stream subscribers per seat (presence)
	left      [2]bool // seat deliberately abandoned the game (via /abandon)
	rosterVer int     // bumped on any name change, join, presence, or abandon change

	// subCnt is the server-wide live-subscriber gauge, shared by every session
	// (set in handleCreate). subscribe increments it and unsubscribe decrements
	// it, so GET /stats can report the current SSE subscriber count.
	subCnt *atomic.Int64
}

// seatFor returns the seat a player token authorizes, or false. The comparison
// is constant-time and checks both seats unconditionally (no early return), so
// it leaks nothing about the secret through timing.
func (s *session) seatFor(token string) (game.Seat, bool) {
	if token == "" {
		return 0, false
	}
	match, found := game.Seat(0), false
	for seat := game.Seat(0); seat < 2; seat++ {
		if s.tokens[seat] != "" && subtle.ConstantTimeCompare([]byte(s.tokens[seat]), []byte(token)) == 1 {
			match, found = seat, true
		}
	}
	return match, found
}

// driveBots applies moves for any bot seat until it is a human's turn or the
// game is over. Called under s.mu after every state change.
func (s *session) driveBots() {
	for {
		if _, ok := s.game.Winner(); ok {
			return
		}
		v := s.game.View(game.Seat0)
		switch v.Phase {
		case game.PhaseDiscard:
			acted := false
			for seat := game.Seat(0); seat < 2; seat++ {
				if s.bots[seat] == nil {
					continue
				}
				vs := s.game.View(seat)
				if len(vs.YourHand) == 6 {
					_, _ = s.game.Apply(seat, game.Discard{Cards: s.bots[seat].Discard(vs)})
					acted = true
				}
			}
			if !acted {
				return // a human still owes a discard
			}
		case game.PhasePlay:
			seat := *v.ToPlay
			if s.bots[seat] == nil {
				return // human's turn
			}
			_, _ = s.game.Apply(seat, game.Play{Card: s.bots[seat].Play(s.game.View(seat))})
		default:
			return
		}
	}
}

// notify wakes every SSE subscriber after new events.
func (s *session) notify() {
	for _, ch := range s.subs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// subscribe registers a stream subscriber for seat and returns its id and wake
// channel. On a seat's subscriber count going 0->1 it bumps the roster version
// and notifies every subscriber, so all streams push an updated "players" delta
// reflecting the seat coming online.
func (s *session) subscribe(seat game.Seat) (int, chan struct{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.nextID
	s.nextID++
	ch := make(chan struct{}, 1)
	if s.subs == nil {
		s.subs = map[int]chan struct{}{}
	}
	s.subs[id] = ch
	if s.subCnt != nil {
		s.subCnt.Add(1)
	}
	if seat < 2 {
		s.subCount[seat]++
		if s.subCount[seat] == 1 {
			s.rosterVer++
			s.notify()
		}
	}
	return id, ch
}

// unsubscribe removes a stream subscriber. On a seat's subscriber count going
// 1->0 it bumps the roster version and notifies, so remaining streams push an
// updated "players" delta reflecting the seat going offline.
func (s *session) unsubscribe(id int, seat game.Seat) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.subs, id)
	if s.subCnt != nil {
		s.subCnt.Add(-1)
	}
	if seat < 2 && s.subCount[seat] > 0 {
		s.subCount[seat]--
		if s.subCount[seat] == 0 {
			s.rosterVer++
			s.notify()
		}
	}
}

// roster builds the full current "players" delta: both seats, their names, and
// presence. A bot seat is always reported connected. Must be called under s.mu.
func (s *session) roster() Delta {
	players := make([]PlayerInfo, 2)
	for seat := game.Seat(0); seat < 2; seat++ {
		connected := s.subCount[seat] > 0 || s.bots[seat] != nil
		name := s.names[seat]
		if name == "" && (s.tokens[seat] != "" || s.bots[seat] != nil) {
			name = "Anonymous" // a seated guest with no display name
		}
		players[seat] = PlayerInfo{Seat: seat, Name: name, Connected: connected, Left: s.left[seat]}
	}
	return Delta{Type: "players", Players: players}
}

// hasSubscribers reports whether any seat currently has a live stream
// subscriber. Must be called under s.mu.
func (s *session) hasSubscribers() bool { return len(s.subs) > 0 }

// joinableLobby reports whether this session belongs in the public lobby: a
// public open game still waiting for an opponent. It excludes private (link-only)
// games, bot games, games whose open seat is already claimed (full/started),
// abandoned games, and finished games. Freshness is by current state, so a game
// drops off as soon as it fills, ends, or is abandoned (and reaping removes it
// from the registry entirely). Must be called under s.mu.
func (s *session) joinableLobby() bool {
	if !s.public {
		return false // private "challenge a friend" game (link-only)
	}
	if s.bots[game.Seat0] != nil || s.bots[game.Seat1] != nil {
		return false // vs-bot games are never listed
	}
	if s.tokens[game.Seat1] != "" {
		return false // opponent already joined: full/started
	}
	if s.left[game.Seat0] || s.left[game.Seat1] {
		return false // host (or anyone) abandoned the game
	}
	if _, over := s.game.Winner(); over {
		return false // finished
	}
	return true
}

// record captures the session's durable state for persistence. Must be called
// under s.mu. Transient transport state (subscribers, presence) is excluded — it
// is rebuilt when clients reconnect.
func (s *session) record() Record {
	return Record{
		ID:        s.id,
		Game:      s.game.Snapshot(),
		Tokens:    s.tokens,
		Names:     s.names,
		PlayerIDs: s.playerIDs,
		Left:      s.left,
		Bots:      [2]bool{s.bots[game.Seat0] != nil, s.bots[game.Seat1] != nil},
		BotNames:  [2]string{botName(s.bots[game.Seat0]), botName(s.bots[game.Seat1])},
		Public:    s.public,
		CreatedAt: s.createdAt,
		LastSeen:  s.lastSeen,
	}
}

// sessionFromRecord rebuilds a session from a persisted record. Subscribers start
// empty (clients reconnect and presence is rebuilt) and a bot seat gets a fresh
// instance of the SAME production bot it was seated with (by recorded name; a
// legacy row with no name, or an unknown name, falls back to the default so the
// game still plays). subCnt is the server-wide subscriber gauge to attach.
func sessionFromRecord(r Record, subCnt *atomic.Int64) *session {
	s := &session{
		id:        r.ID,
		game:      game.Restore(r.Game, cryptoDeck{}),
		tokens:    r.Tokens,
		names:     r.Names,
		playerIDs: r.PlayerIDs,
		left:      r.Left,
		public:    r.Public,
		createdAt: r.CreatedAt,
		lastSeen:  r.LastSeen,
		subCnt:    subCnt,
	}
	for seat := game.Seat(0); seat < 2; seat++ {
		if r.Bots[seat] {
			name := r.BotNames[seat]
			if name == "" {
				name = bot.DefaultName // legacy row written before bot names were stored
			}
			s.bots[seat] = newBot(name)
		}
	}
	return s
}

// botName returns a seated bot's production name, or "" for an empty (human)
// seat, so a session's bot identity can be persisted and re-seated on restore.
func botName(b bot.Bot) string {
	if b == nil {
		return ""
	}
	return b.Name()
}

// --- registry ----------------------------------------------------------------

// maxSessions caps the number of concurrently registered sessions. POST /games
// is unauthenticated, so without a ceiling a burst of creates could exhaust the
// small machine's memory. 5000 is a tunable bound: each session is tiny, so this
// is conservative for a 256 MB host. Raise it if the machine grows.
const maxSessions = 5000

// registry holds all active sessions, addressed by id.
type registry struct {
	mu       sync.Mutex
	sessions map[string]*session
}

func newRegistry() *registry { return &registry{sessions: map[string]*session{}} }

// count returns the number of live sessions currently registered.
func (r *registry) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.sessions)
}

// snapshot returns the currently registered sessions. The registry lock is held
// only while copying the map's pointers, so callers can then lock each session
// individually (e.g. to build the lobby list) without holding the registry lock.
func (r *registry) snapshot() []*session {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*session, 0, len(r.sessions))
	for _, s := range r.sessions {
		out = append(out, s)
	}
	return out
}

func (r *registry) get(id string) (*session, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.sessions[id]
	return s, ok
}

// add registers a session, returning false (without inserting) if the registry
// is already at maxSessions capacity. The check and insert happen atomically
// under r.mu so concurrent creates can't overshoot the cap.
func (r *registry) add(s *session) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.sessions) >= maxSessions {
		return false
	}
	r.sessions[s.id] = s
	return true
}

// reap evicts sessions untouched for longer than ttl, returning the ids it
// evicted so the caller can drop them from the durable store too.
func (r *registry) reap(ttl time.Duration, now time.Time) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var reaped []string
	for id, s := range r.sessions {
		s.mu.Lock()
		// A game with any live stream subscriber is never reaped, so an
		// in-progress game survives an idle-but-connected pair.
		stale := !s.hasSubscribers() && now.Sub(s.lastSeen) > ttl
		s.mu.Unlock()
		if stale {
			delete(r.sessions, id)
			reaped = append(reaped, id)
		}
	}
	return reaped
}

// --- tokens & decks ----------------------------------------------------------

// newToken returns a 256-bit URL-safe random token.
func newToken() string {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Sprintf("server: out of randomness: %v", err))
	}
	return base64.RawURLEncoding.EncodeToString(b[:])
}

// cryptoDeck is a game.DeckSource that shuffles with cryptographic randomness,
// so the deck order is unpredictable (it underlies hidden-hand integrity).
type cryptoDeck struct{}

func (cryptoDeck) Shuffle() []cribbage.Card {
	d := cribbage.Deck()
	for i := len(d) - 1; i > 0; i-- {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(i+1)))
		if err != nil {
			panic(fmt.Sprintf("server: out of randomness: %v", err))
		}
		j := int(n.Int64())
		d[i], d[j] = d[j], d[i]
	}
	return d
}
