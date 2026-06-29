package server

import (
	"time"

	"github.com/cribbager/cribbager/internal/cribbage"
	"github.com/cribbager/cribbager/internal/game"
	"github.com/cribbager/cribbager/internal/scoring/hand"
	"github.com/cribbager/cribbager/internal/scoring/pegging"
)

// The wire protocol: clients GET a snapshot (the visibility-filtered
// game.PlayerView, which already carries a Version) on load/reconnect, then
// receive a stream of per-seat semantic deltas, each tagged with the sequence
// number it advances the client to. A gap in seq means "resync via snapshot".
//
// Encoding is JSON, kept deliberately encoding-agnostic in shape so a later
// swap to a binary codec is localized. Cards serialize as "5H" strings.

// Delta is one projected event for one seat. Hidden information (the opponent's
// dealt hand, the opponent's discards) is redacted here, so the stream a player
// receives can never leak it. Type discriminates.
//
// The presence-optional fields (seat, card, cards, ...) are omitempty. The
// numeric *score* fields deliberately are NOT: a legitimate zero (a scoreless
// play, a non-jack cut) must travel as `0`, not be dropped, so the client can
// read them unconditionally instead of inferring "absent means zero" — that
// inference is what produced an earlier NaN bug client-side.
type Delta struct {
	Seq  int    `json:"seq"`
	Type string `json:"type"`

	Seat   *game.Seat      `json:"seat,omitempty"`
	Dealer *game.Seat      `json:"dealer,omitempty"`
	Card   *cribbage.Card  `json:"card,omitempty"`
	Cards  []cribbage.Card `json:"cards,omitempty"`
	Hand   []cribbage.Card `json:"hand,omitempty"`
	Cut    []cribbage.Card `json:"cut,omitempty"` // slice, not array: omitempty works

	// Hands carries BOTH seats' full dealt hands on a hand_dealt delta, indexed by
	// seat ([]{seat0Hand, seat1Hand}). It is set ONLY by the full-visibility replay
	// projection (projectReplayEvent), never by the live per-seat projection, which
	// redacts the opponent's hand.
	//
	// Deliberately a slice, not [2][]cribbage.Card: encoding/json's omitempty is a
	// no-op on a fixed-size array (it is never "empty"), so an array would emit
	// "hands":[null,null] on EVERY live delta and break the byte-for-byte identity
	// of the live wire format. A nil slice omits cleanly. The replay JSON shape is
	// unchanged either way: "hands":[[...6],[...6]].
	Hands [][]cribbage.Card `json:"hands,omitempty"`

	OpponentCards int          `json:"opponentCards"`
	Points        int          `json:"points"`
	Count         int          `json:"count"`
	Total         int          `json:"total"`
	Scores        *[2]int      `json:"scores,omitempty"`
	Combos        []ScoreCombo `json:"combos,omitempty"`

	// Players carries the full current roster on a "players" delta: both seats,
	// their display names, and whether each seat currently has a live stream
	// subscriber. It is current-state, not a sequenced game event, so a "players"
	// delta is emitted with Seq 0 and written to the stream without an id: line.
	Players []PlayerInfo `json:"players,omitempty"`
}

// PlayerInfo is one seat's roster entry on a "players" delta. Left reports a
// deliberate departure (via /abandon), distinct from a transient disconnect
// (Connected): the opponent has left for good and won't return to this game.
type PlayerInfo struct {
	Seat      game.Seat `json:"seat"`
	Name      string    `json:"name"`
	Connected bool      `json:"connected"`
	Left      bool      `json:"left"`
}

// ScoreCombo is one scoring element, for the client to narrate ("run of three
// for 3", "his nobs for 1"). Length is set only for runs.
type ScoreCombo struct {
	Kind   string `json:"kind"`
	Points int    `json:"points"`
	Length int    `json:"length,omitempty"`
}

func pegCombos(r pegging.Result) []ScoreCombo {
	var out []ScoreCombo
	for _, c := range r.Combos() {
		out = append(out, ScoreCombo{Kind: c.Kind.String(), Points: c.Points, Length: c.RunLength})
	}
	return out
}

func handCombos(r hand.Result) []ScoreCombo {
	var out []ScoreCombo
	for _, c := range r.Combos() {
		out = append(out, ScoreCombo{Kind: c.Kind.String(), Points: c.Points, Length: c.RunLength})
	}
	return out
}

// projectEvents redacts and tags a run of engine events for one seat. baseSeq is
// the sequence number of the event just before events[0], so events[i] gets seq
// baseSeq+1+i. Clients keep the running score from the snapshot plus each
// delta's point increment.
func projectEvents(viewer game.Seat, events []game.Event, baseSeq int) []Delta {
	out := make([]Delta, 0, len(events))
	for i, e := range events {
		out = append(out, projectEvent(viewer, e, baseSeq+1+i))
	}
	return out
}

func projectEvent(viewer game.Seat, e game.Event, seq int) Delta {
	d := Delta{Seq: seq}
	switch e := e.(type) {
	case game.CutForDeal:
		dealer := e.Dealer
		d.Type, d.Cut, d.Dealer = "cut_for_deal", e.Cuts[:], &dealer

	case game.HandDealt:
		dealer := e.Dealer
		opp := other(viewer)
		d.Type, d.Dealer = "hand_dealt", &dealer
		d.Hand = e.Hands[viewer]            // your cards only
		d.OpponentCards = len(e.Hands[opp]) // opponent's are hidden

	case game.Discarded:
		seat := e.Seat
		d.Type, d.Seat = "discarded", &seat
		if e.Seat == viewer {
			d.Cards = e.Cards[:] // you see your own discards
		}
		// opponent's discards are redacted

	case game.StarterCut:
		card := e.Card
		d.Type, d.Card, d.Points = "starter_cut", &card, e.Heels

	case game.CardPlayed:
		seat := e.Seat
		card := e.Card
		d.Type, d.Seat, d.Card = "card_played", &seat, &card
		d.Points, d.Count, d.Combos = e.Score.Total, e.Score.Count, pegCombos(e.Score)

	case game.Pass:
		seat := e.Seat
		d.Type, d.Seat = "pass", &seat

	case game.GoAwarded:
		seat := e.Seat
		d.Type, d.Seat, d.Points = "go", &seat, e.Points

	case game.SeriesReset:
		d.Type = "series_reset"

	case game.HandShown:
		seat := e.Seat
		d.Type, d.Seat, d.Cards, d.Total, d.Combos = "hand_shown", &seat, e.Cards, e.Score.Total, handCombos(e.Score)

	case game.CribShown:
		d.Type, d.Cards, d.Total, d.Combos = "crib_shown", e.Cards, e.Score.Total, handCombos(e.Score)

	case game.GameWon:
		seat := e.Seat
		d.Type, d.Seat = "game_won", &seat
	}
	return d
}

// projectReplayEvents is the full-visibility counterpart of projectEvents, used
// ONLY for post-game replay of a finished, stored game by one of its
// participants. It tags each event with its sequence number (events[i] gets seq
// baseSeq+1+i), exactly like projectEvents.
func projectReplayEvents(events []game.Event, baseSeq int) []Delta {
	out := make([]Delta, 0, len(events))
	for i, e := range events {
		out = append(out, projectReplayEvent(e, baseSeq+1+i))
	}
	return out
}

// projectReplayEvent flattens one engine event into a Delta with FULL
// visibility, for post-game replay. It is identical to projectEvent (same type
// discriminators, same field names, so the client's existing delta vocabulary is
// reused unchanged) EXCEPT for the two events that the live projection redacts:
//
//   - hand_dealt: emits BOTH seats' full dealt hands (Hands), not just the
//     viewer's hand + an opponent count.
//   - discarded: always emits the actual Cards, for either seat.
//
// Every other event type carries no hidden information and is delegated to
// projectEvent verbatim, so it stays byte-identical to the live delta. There is
// no viewer here: a replay shows the whole game.
func projectReplayEvent(e game.Event, seq int) Delta {
	switch e := e.(type) {
	case game.HandDealt:
		dealer := e.Dealer
		return Delta{
			Seq:    seq,
			Type:   "hand_dealt",
			Dealer: &dealer,
			Hands:  [][]cribbage.Card{e.Hands[game.Seat0], e.Hands[game.Seat1]},
		}
	case game.Discarded:
		seat := e.Seat
		return Delta{
			Seq:   seq,
			Type:  "discarded",
			Seat:  &seat,
			Cards: e.Cards[:], // both seats' discards are revealed in replay
		}
	default:
		// All other events carry no hidden info; reuse the live projection so the
		// wire shape matches the deltas the client already parses. The viewer seat is
		// irrelevant for these cases (it only affects hand_dealt/discarded above).
		return projectEvent(game.Seat0, e, seq)
	}
}

func other(s game.Seat) game.Seat { return s ^ 1 }

// --- request / response DTOs --------------------------------------------------

type createRequest struct {
	Mode   string `json:"mode"` // "bot" or "open"
	Target int    `json:"target"`
	Name   string `json:"name,omitempty"`
	// Public, for an open game, lists it in the lobby (GET /lobby) for anyone to
	// join ("Create a game"). It defaults to false, so an open game with no flag
	// stays private/link-only ("Challenge a friend") — back-compatible. Ignored for
	// bot games (never listed).
	Public bool `json:"public,omitempty"`
}

type createResponse struct {
	GameID      string    `json:"game_id"`
	PlayerToken string    `json:"player_token"`
	Seat        game.Seat `json:"seat"`
}

type joinRequest struct {
	Name string `json:"name,omitempty"`
}

type joinResponse struct {
	PlayerToken string    `json:"player_token"`
	Seat        game.Seat `json:"seat"`
}

type actionRequest struct {
	Type            string          `json:"type"` // "discard" or "play"
	Cards           []cribbage.Card `json:"cards"`
	Card            *cribbage.Card  `json:"card"`
	ExpectedVersion *int            `json:"expected_version"`
}

type actionResponse struct {
	Version int     `json:"version"`
	Deltas  []Delta `json:"deltas"`
}

// statsResponse is the GET /stats payload: counts only (live games and live SSE
// subscribers), no game data, so it is safe to expose unauthenticated.
type statsResponse struct {
	Games       int `json:"games"`
	Subscribers int `json:"subscribers"`
}

// lobbyGame is one joinable public open game in the GET /lobby listing: enough to
// show it and join it (the game id is the join credential), and nothing private.
type lobbyGame struct {
	GameID    string    `json:"game_id"`
	HostName  string    `json:"host_name"`
	CreatedAt time.Time `json:"created_at"`
	OpenSeat  game.Seat `json:"open_seat"` // the unclaimed seat a joiner takes
}

// lobbyResponse is the GET /lobby payload: the public open games still waiting for
// an opponent. Games is always a (possibly empty) array, never null.
type lobbyResponse struct {
	Games []lobbyGame `json:"games"`
}
