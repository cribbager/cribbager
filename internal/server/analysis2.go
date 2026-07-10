package server

import (
	"net/http"
	"sync"

	"github.com/cribbager/cribbager/internal/bot"
	"github.com/cribbager/cribbager/internal/bot/eval"
	"github.com/cribbager/cribbager/internal/cribbage"
	"github.com/cribbager/cribbager/internal/game"
)

// Multi-engine post-game analysis (v2) — GET /games/{id}/analysis.
//
// This is the API behind the unified replay/analyze page: ONE payload carries
// EVERY engine's verdict on every decision the analyzed seat made, so the UI
// can switch engines client-side with no refetch. It is strictly post-game
// (finished games only) and additive — the v1 discard-only endpoint at
// GET /users/me/games/{id}/analysis is untouched.
//
// # Access
//
// Any PARTICIPANT of the finished game may call it, including guests:
//
//   - Player token (Authorization: Bearer <player_token>): the same per-game
//     credential that authorized the participant's moves. Works for guests and
//     registered players alike, for as long as the game's session is live in
//     the registry (finished sessions persist until reaped) — which covers the
//     game-over "Evaluate Game" moment. A fully-guest game never gets a stored
//     Result, so this is the ONLY path that can serve it.
//   - Login session (cookie): a registered user may analyze any finished game
//     in their history (the stored Result names their seat), forever — same as
//     v1 and replay.
//
// The analyzed seat is always the requester's own seat. Status codes:
//
//   - 401: no credential at all (no bearer token, no login session).
//   - 404: a credential was presented but grants no seat in this game —
//     unknown id, a spectator/non-participant, or a stale/foreign token. Like
//     replay, a game the caller can't see is indistinguishable from one that
//     doesn't exist.
//   - 409: the caller IS a participant (valid player token) but the game is
//     not finished — analysis is post-game only, deliberately (no in-game
//     coaching). Via the login path an unfinished game has no stored Result
//     yet and so 404s, mirroring v1's convention.
//
// # Response contract
//
// All card values are two-character strings ("5H", "TD"); seats are 0 or 1;
// units is "points" (expected points per deal) or "winprob" (probability of
// winning the game, 0..1). Point values are rounded to 4 decimals, win
// probabilities to 6; optimality is decided on the raw values BEFORE
// rounding, with ties (delta <= 1e-9) counting as optimal. Summary deltas are
// sums of the per-decision rounded deltas.
//
//	{
//	  "game_id": "…",
//	  "seat": 0,                       // the analyzed seat = the requester's seat
//	  "engines": [                     // fixed order; all per-decision engine arrays align with it
//	    {"name": "ml",       "version": "2"},   // the production ml bot's own values
//	    {"name": "champion", "version": "3"},   // win objective (RankDiscardsWin / RankPlaysWin)
//	    {"name": "exact-ev", "version": "1"}    // pure point EV for both phases
//	  ],
//	  "deals": [                       // one per deal the analyzed seat discarded in
//	    {
//	      "deal": 0,                   // 0-based deal index within the game
//	      "dealer": 1,                 // the seat that dealt (owns the crib)
//	      "start_scores": [0, 0],      // seat-indexed scores at the deal's start
//	      "discard": {
//	        "hand":  ["5H","5S","5C","5D","JH","KH"],   // the six dealt cards
//	        "throw": ["JH","KH"],                        // what the player threw
//	        "keep":  ["5H","5S","5C","5D"],              // the four kept
//	        "engines": [               // aligned with engines[]
//	          {"best_throw": ["JH","KH"], "best_keep": ["5H","5S","5C","5D"],
//	           "chosen_value": 20.4142, "best_value": 20.4142,
//	           "delta": 0, "optimal": true, "units": "points"},
//	          …
//	        ],
//	        "agree": true              // all engines' best throws are the same unordered pair
//	      },
//	      "plays": [                   // the seat's NON-FORCED pegging decisions, in order;
//	        {                          // a decision needs >= 2 distinct legal RANKS (suits
//	          "count": 10,             //   are pegging-equivalent, so [5H 5S] is forced)
//	          "pile": ["TD"],          // the current count series before the play
//	          "hand": ["5H","5S","5C","5D"],  // cards still held before the play
//	          "played": "5H",
//	          "legal": ["5H","5S","5C","5D"],
//	          "engines": [             // aligned with engines[]
//	            {"best": "5H", "chosen_value": 1.9931, "best_value": 1.9931,
//	             "delta": 0, "optimal": true, "units": "points"},
//	            …
//	          ],
//	          "agree": true            // all engines' best plays share one RANK
//	        }
//	      ],
//	      "rollup": [                  // aligned with engines[]; feeds the per-deal ✓/✗ rail
//	        {"discard_optimal": true, "pegging_optimal": true},  // pegging_optimal is
//	        …                                                    // vacuously true with no decisions
//	      ]
//	    }
//	  ],
//	  "summary": [                     // aligned with engines[]
//	    {"hands": 9, "optimal_discards": 7,
//	     "discard_delta_points": 1.8342, "discard_delta_winprob": 0.031,
//	     "play_decisions": 24, "optimal_plays": 21,
//	     "play_delta_points": 2.1, "play_delta_winprob": 0.0007}
//	  ]
//	}
//
// # Engines, values, and the units subtlety
//
// Every per-decision value is THE exact quantity whose argmax is that
// engine's move, so a game played by the corresponding bot always grades
// optimal (delta 0) on its own engine, and delta is >= 0 by construction:
//
//   - "ml": points, both phases. Discard = exact expected points of the split
//     plus the discard net's predicted pegging differential for the keep;
//     play = the pegging Q-net's predicted pegging-point differential for the
//     card's rank. Exactly the production bot's numbers (bot.MLAnalyzer).
//   - "champion": far from the target, points (identical objective to
//     exact-ev — the objectives provably agree there); once either player is
//     in reach of 121 (eval.InReach at the decision's scores), win
//     probability. In-reach discard values are P(win) with the hold;
//     in-reach play values are P(win) after the play plus the evaluator's
//     deterministic tie-break term (<= ~1e-4, folded in so the value ranks
//     exactly as the bot decides). Each verdict's units field says which
//     scale that decision used.
//   - "exact-ev": points, both phases. Discard = exact expected show + signed
//     crib points; play = one-ply net expected points against the calibrated
//     opponent model (the same ranking champion uses far from the end).
//
// Play-phase point values (champion far-from-end and exact-ev) include the
// evaluator's low-card tie-break (<= 0.013 points), which only separates
// otherwise-equal plays.
//
// Summary deltas are kept in SEPARATE per-unit sums (…_delta_points and
// …_delta_winprob) — a points-loss and a winprob-loss are not addable, and a
// single game can contain both as it approaches the endgame.
//
// # Limitations
//
// Verdicts assume the standard 121-point target (the evaluators' win tables
// are built for it); a non-default-target game still analyzes but champion's
// in-reach switch and win probabilities treat 121 as the goal — the same
// limitation replay has for its Target field.

// analysisEpsilon is the optimality slack: the evaluators are deterministic,
// so a delta this small is a genuine tie (equal-value alternatives), not a
// worse move. Same convention as v1's evEpsilon.
const analysisEpsilon = 1e-9

// Units for per-decision values.
const (
	unitsPoints  = "points"  // expected points per deal
	unitsWinprob = "winprob" // probability of winning the game (0..1)
)

// exactEVVersion versions the "exact-ev" engine: its discard side is the
// exact point-EV table ranking (eval.RankDiscards) and its play side is the
// one-ply net-EV ranking against the calibrated opponent model
// (eval.RankPlays). Bump when either ranking's semantics change.
const exactEVVersion = "1"

// --- response DTOs -------------------------------------------------------------

// engineInfo identifies one analysis engine; the deals' per-decision engine
// arrays and the summary array align with the response's engines slice.
type engineInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// engineDiscardVerdict is one engine's judgment of one discard.
type engineDiscardVerdict struct {
	BestThrow   [2]cribbage.Card `json:"best_throw"` // the engine's optimal throw
	BestKeep    [4]cribbage.Card `json:"best_keep"`  // the four it would keep
	ChosenValue float64          `json:"chosen_value"`
	BestValue   float64          `json:"best_value"`
	Delta       float64          `json:"delta"`   // best - chosen, >= 0
	Optimal     bool             `json:"optimal"` // delta <= 1e-9 (ties are optimal)
	Units       string           `json:"units"`   // "points" | "winprob"
}

// enginePlayVerdict is one engine's judgment of one pegging play.
type enginePlayVerdict struct {
	Best        cribbage.Card `json:"best"` // the engine's optimal card (suits are pegging-equivalent)
	ChosenValue float64       `json:"chosen_value"`
	BestValue   float64       `json:"best_value"`
	Delta       float64       `json:"delta"`   // best - chosen, >= 0
	Optimal     bool          `json:"optimal"` // delta <= 1e-9 (ties are optimal)
	Units       string        `json:"units"`   // "points" | "winprob"
}

// discardDecision is the analyzed seat's discard in one deal, with every
// engine's verdict.
type discardDecision struct {
	Hand    []cribbage.Card        `json:"hand"`  // the six cards dealt
	Throw   [2]cribbage.Card       `json:"throw"` // the two thrown to the crib
	Keep    [4]cribbage.Card       `json:"keep"`  // the four kept
	Engines []engineDiscardVerdict `json:"engines"`
	Agree   bool                   `json:"agree"` // all engines' best throws are the same unordered pair
}

// playDecision is one NON-FORCED pegging choice (>= 2 distinct legal ranks),
// with every engine's verdict. Forced moves are not decisions and are omitted
// — the same rule the pegging training environment uses.
type playDecision struct {
	Count   int                 `json:"count"` // the count before the play
	Pile    []cribbage.Card     `json:"pile"`  // the current series before the play
	Hand    []cribbage.Card     `json:"hand"`  // cards still held before the play
	Played  cribbage.Card       `json:"played"`
	Legal   []cribbage.Card     `json:"legal"`
	Engines []enginePlayVerdict `json:"engines"`
	Agree   bool                `json:"agree"` // all engines' best plays share one rank
}

// engineRollup is one engine's per-deal ✓/✗: was the discard optimal, and was
// every pegging decision optimal (vacuously true with no decisions).
type engineRollup struct {
	DiscardOptimal bool `json:"discard_optimal"`
	PeggingOptimal bool `json:"pegging_optimal"`
}

// dealAnalysis is one deal's decisions from the analyzed seat's perspective.
type dealAnalysis struct {
	Deal        int             `json:"deal"`   // 0-based deal index within the game
	Dealer      int             `json:"dealer"` // the seat that dealt (owns the crib)
	StartScores [2]int          `json:"start_scores"`
	Discard     discardDecision `json:"discard"`
	Plays       []playDecision  `json:"plays"`
	Rollup      []engineRollup  `json:"rollup"`
}

// engineSummary aggregates one engine's verdicts over the whole game. The
// delta sums are split by units because a points-loss and a winprob-loss are
// not addable; each is the sum of that unit's per-decision (rounded) deltas.
type engineSummary struct {
	Hands               int     `json:"hands"`
	OptimalDiscards     int     `json:"optimal_discards"`
	DiscardDeltaPoints  float64 `json:"discard_delta_points"`
	DiscardDeltaWinprob float64 `json:"discard_delta_winprob"`
	PlayDecisions       int     `json:"play_decisions"`
	OptimalPlays        int     `json:"optimal_plays"`
	PlayDeltaPoints     float64 `json:"play_delta_points"`
	PlayDeltaWinprob    float64 `json:"play_delta_winprob"`
}

// gameAnalysisV2Response is the JSON returned by GET /games/{id}/analysis.
// See the contract at the top of this file.
type gameAnalysisV2Response struct {
	GameID  string          `json:"game_id"`
	Seat    int             `json:"seat"`
	Engines []engineInfo    `json:"engines"`
	Deals   []dealAnalysis  `json:"deals"`
	Summary []engineSummary `json:"summary"`
}

// --- engines -------------------------------------------------------------------

// analysisEngine is one way of valuing decisions. The invariant every
// implementation must keep: the returned values are exactly the quantity
// whose argmax is the engine's own move, so delta >= 0 always and the
// corresponding bot's actual choices grade optimal. ok is false only when the
// recorded choice isn't among the candidates, which a legal event log makes
// impossible; it is handled defensively rather than fabricating a verdict.
type analysisEngine interface {
	info() engineInfo
	rateDiscard(hand [6]cribbage.Card, myCrib bool, myScore, oppScore int, throw [2]cribbage.Card) (engineDiscardVerdict, bool)
	ratePlay(v game.PlayerView, played cribbage.Card) (enginePlayVerdict, bool)
}

// analysisEngines builds the engine list ONCE (the ml analyzer parses the
// embedded network weights), in the response's fixed order: ml, champion,
// exact-ev.
var analysisEngines = sync.OnceValue(func() []analysisEngine {
	return []analysisEngine{
		mlEngine{a: bot.NewMLAnalyzer()},
		championEngine{version: bot.Champion().Version()},
		exactEVEngine{},
	}
})

// discardVerdict assembles a discard verdict from raw values, rounding for
// the wire (points to 4 decimals, win probabilities to 6) while deciding
// delta/optimality on the raw numbers. A chosen value that beats "best" by a
// tie-break-sized hair clamps to a delta of zero — it's a tie, never negative.
func discardVerdict(bestThrow [2]cribbage.Card, bestKeep [4]cribbage.Card, best, chosen float64, units string) engineDiscardVerdict {
	delta := best - chosen
	if delta < 0 {
		delta = 0
	}
	round := round4
	if units == unitsWinprob {
		round = round6
	}
	return engineDiscardVerdict{
		BestThrow:   bestThrow,
		BestKeep:    bestKeep,
		ChosenValue: round(chosen),
		BestValue:   round(best),
		Delta:       round(delta),
		Optimal:     delta <= analysisEpsilon,
		Units:       units,
	}
}

// playVerdict is discardVerdict's play-phase counterpart.
func playVerdict(best cribbage.Card, bestVal, chosen float64, units string) enginePlayVerdict {
	delta := bestVal - chosen
	if delta < 0 {
		delta = 0
	}
	round := round4
	if units == unitsWinprob {
		round = round6
	}
	return enginePlayVerdict{
		Best:        best,
		ChosenValue: round(chosen),
		BestValue:   round(bestVal),
		Delta:       round(delta),
		Optimal:     delta <= analysisEpsilon,
		Units:       units,
	}
}

// mlEngine reproduces the production ml bot's own values via bot.MLAnalyzer —
// the same embedded networks and code paths the live bot decides with. Units
// are always points (the discard value is exact points EV plus a predicted
// pegging differential; the play value is a predicted pegging-point swing).
// Scores are ignored: the ml bot is deliberately score-blind.
type mlEngine struct{ a bot.MLAnalyzer }

func (e mlEngine) info() engineInfo { return engineInfo{Name: e.a.Name(), Version: e.a.Version()} }

func (e mlEngine) rateDiscard(hand [6]cribbage.Card, myCrib bool, _, _ int, throw [2]cribbage.Card) (engineDiscardVerdict, bool) {
	vals := e.a.DiscardValues(hand, myCrib)
	best := vals[0]
	var chosen *bot.MLDiscardValue
	for i, dv := range vals {
		if dv.Value > best.Value { // strict >: first maximum, the bot's own pick
			best = dv
		}
		if samePair(dv.Discard, throw) {
			chosen = &vals[i]
		}
	}
	if chosen == nil {
		return engineDiscardVerdict{}, false
	}
	return discardVerdict(best.Discard, best.Keep, best.Value, chosen.Value, unitsPoints), true
}

func (e mlEngine) ratePlay(v game.PlayerView, played cribbage.Card) (enginePlayVerdict, bool) {
	vals := e.a.PlayValues(v)
	if len(vals) == 0 {
		return enginePlayVerdict{}, false
	}
	best := vals[0]
	var chosen *bot.MLPlayValue
	for i, pv := range vals {
		if pv.Value > best.Value { // strict >: first maximum, the bot's own pick
			best = pv
		}
		if pv.Card == played {
			chosen = &vals[i]
		}
	}
	if chosen == nil {
		return enginePlayVerdict{}, false
	}
	return playVerdict(best.Card, best.Value, chosen.Value, unitsPoints), true
}

// championEngine is the win objective — the champion bot's own rankings.
// Far from the target the objective provably reduces to point EV, so values
// are points; once either player is in reach of 121 at the decision's scores
// (eval.InReach) values switch to win probability. Discards use the hold's
// P(win) (RankDiscardsWin's primary sort key); plays use RankedPlay.Score,
// which in reach is P(win) plus the evaluator's <=1e-4 deterministic
// tie-break — the exact quantity BestPlayWin argmaxes, so the champion's own
// plays always grade optimal.
type championEngine struct{ version string }

func (e championEngine) info() engineInfo {
	return engineInfo{Name: bot.ChampionName, Version: e.version}
}

func (e championEngine) rateDiscard(hand [6]cribbage.Card, myCrib bool, myScore, oppScore int, throw [2]cribbage.Card) (engineDiscardVerdict, bool) {
	ranked := eval.RankDiscardsWin(hand, myCrib, myScore, oppScore)
	value := func(rd eval.RankedDiscard) float64 { return rd.Score }
	units := unitsPoints
	if eval.InReach(myScore, oppScore, myCrib) {
		value = func(rd eval.RankedDiscard) float64 { return rd.Win }
		units = unitsWinprob
	}
	for _, rd := range ranked {
		if samePair(rd.Discard, throw) {
			return discardVerdict(ranked[0].Discard, ranked[0].Keep, value(ranked[0]), value(rd), units), true
		}
	}
	return engineDiscardVerdict{}, false
}

func (e championEngine) ratePlay(v game.PlayerView, played cribbage.Card) (enginePlayVerdict, bool) {
	units := unitsPoints
	if eval.InReach(v.Scores[v.You], v.Scores[1-v.You], v.Dealer == v.You) {
		units = unitsWinprob
	}
	return ratedPlay(eval.RankPlaysWin(v), played, units)
}

// exactEVEngine is pure point expectation for both phases: the exact
// crib-aware discard tables (eval.RankDiscards — the same values v1 serves)
// and the one-ply net-EV play ranking against the calibrated opponent model
// (eval.RankPlays — champion's far-from-end path). Always points; play values
// include the evaluator's <=0.013-point low-card tie-break.
type exactEVEngine struct{}

func (exactEVEngine) info() engineInfo { return engineInfo{Name: "exact-ev", Version: exactEVVersion} }

func (exactEVEngine) rateDiscard(hand [6]cribbage.Card, myCrib bool, _, _ int, throw [2]cribbage.Card) (engineDiscardVerdict, bool) {
	ranked := eval.RankDiscards(hand, myCrib)
	for _, rd := range ranked {
		if samePair(rd.Discard, throw) {
			return discardVerdict(ranked[0].Discard, ranked[0].Keep, ranked[0].Score, rd.Score, unitsPoints), true
		}
	}
	return engineDiscardVerdict{}, false
}

func (exactEVEngine) ratePlay(v game.PlayerView, played cribbage.Card) (enginePlayVerdict, bool) {
	return ratedPlay(eval.RankPlays(v), played, unitsPoints)
}

// ratedPlay converts an eval play ranking (best-first by Score) into a
// verdict for the played card. Score is the ranking's own sort key, so
// ranked[0] is the engine's move and deltas are >= 0 by construction.
func ratedPlay(ranked []eval.RankedPlay, played cribbage.Card, units string) (enginePlayVerdict, bool) {
	for _, rp := range ranked {
		if rp.Card == played {
			return playVerdict(ranked[0].Card, ranked[0].Score, rp.Score, units), true
		}
	}
	return enginePlayVerdict{}, false
}

// --- analysis core ---------------------------------------------------------------

// analyzeGameV2 walks a finished game's event log and grades every decision
// the analyzed seat made — its discard and its non-forced pegging plays, deal
// by deal — under every engine. Deal start scores are reconstructed by
// folding the log's scoring events (the same accumulation the engine's own
// reduce performs, including a Handicap head start); pegging decisions come
// from game.ReconstructPlays, which replays the log through the engine's own
// fold so each play's view is exactly what the seat saw.
func analyzeGameV2(id string, seat game.Seat, events []game.Event, engines []analysisEngine) (gameAnalysisV2Response, error) {
	resp := gameAnalysisV2Response{
		GameID:  id,
		Seat:    int(seat),
		Engines: make([]engineInfo, len(engines)),
		Deals:   []dealAnalysis{},
		Summary: make([]engineSummary, len(engines)),
	}
	for i, e := range engines {
		resp.Engines[i] = e.info()
	}

	decisions, err := game.ReconstructPlays(events)
	if err != nil {
		return resp, err
	}
	// The analyzed seat's non-forced pegging decisions, grouped by deal.
	playsByDeal := map[int][]game.PlayDecision{}
	for _, d := range decisions {
		if d.Seat == seat && distinctRanks(d.View.LegalPlays) >= 2 {
			playsByDeal[d.Deal] = append(playsByDeal[d.Deal], d)
		}
	}

	// Winprob-unit summary deltas need finer rounding than the points sums, so
	// they accumulate separately and are rounded once at the end.
	discardWinprobSum := make([]float64, len(engines))
	playWinprobSum := make([]float64, len(engines))

	var scores [2]int
	deal := -1
	var dealer game.Seat
	var hand [6]cribbage.Card
	var startScores [2]int
	haveHand := false

	for _, ev := range events {
		switch e := ev.(type) {
		case game.Handicap:
			scores = e.Scores
		case game.HandDealt:
			deal++
			dealer = e.Dealer
			startScores = scores
			if len(e.Hands[seat]) == 6 {
				copy(hand[:], e.Hands[seat])
				haveHand = true
			} else {
				haveHand = false // defensive: a malformed deal is skipped, not analyzed
			}
		case game.Discarded:
			if e.Seat != seat || !haveHand {
				continue
			}
			haveHand = false // one discard per seat per deal
			da, ok := analyzeDealV2(engines, deal, dealer, seat, startScores, hand, e.Cards, playsByDeal[deal])
			if !ok {
				continue
			}
			resp.Deals = append(resp.Deals, da)
			for i := range engines {
				s := &resp.Summary[i]
				dv := da.Discard.Engines[i]
				s.Hands++
				if dv.Optimal {
					s.OptimalDiscards++
				}
				if dv.Units == unitsWinprob {
					discardWinprobSum[i] += dv.Delta
				} else {
					s.DiscardDeltaPoints += dv.Delta
				}
				for _, p := range da.Plays {
					pv := p.Engines[i]
					s.PlayDecisions++
					if pv.Optimal {
						s.OptimalPlays++
					}
					if pv.Units == unitsWinprob {
						playWinprobSum[i] += pv.Delta
					} else {
						s.PlayDeltaPoints += pv.Delta
					}
				}
			}
		// Score accumulation mirrors the engine's reduce, so start_scores are
		// exactly the engine's scores at each HandDealt.
		case game.StarterCut:
			scores[dealer] += e.Heels
		case game.CardPlayed:
			scores[e.Seat] += e.Score.Total
		case game.GoAwarded:
			scores[e.Seat] += e.Points
		case game.HandShown:
			scores[e.Seat] += e.Score.Total
		case game.CribShown:
			scores[dealer] += e.Score.Total
		}
	}

	for i := range resp.Summary {
		s := &resp.Summary[i]
		s.DiscardDeltaPoints = round4(s.DiscardDeltaPoints)
		s.DiscardDeltaWinprob = round6(discardWinprobSum[i])
		s.PlayDeltaPoints = round4(s.PlayDeltaPoints)
		s.PlayDeltaWinprob = round6(playWinprobSum[i])
	}
	return resp, nil
}

// analyzeDealV2 grades one deal: the analyzed seat's discard and its
// non-forced pegging decisions, under every engine, plus the per-deal
// agreement flags and rollup. ok is false only if some engine couldn't match
// the recorded choice to a candidate, which a legal log makes impossible.
func analyzeDealV2(engines []analysisEngine, deal int, dealer, seat game.Seat, startScores [2]int, hand [6]cribbage.Card, throw [2]cribbage.Card, plays []game.PlayDecision) (dealAnalysis, bool) {
	myCrib := dealer == seat
	my, opp := startScores[seat], startScores[1-seat]

	da := dealAnalysis{
		Deal:        deal,
		Dealer:      int(dealer),
		StartScores: startScores,
		Discard: discardDecision{
			Hand:  append([]cribbage.Card(nil), hand[:]...),
			Throw: throw,
			Keep:  keepOf(hand, throw),
		},
		Plays:  []playDecision{},
		Rollup: make([]engineRollup, len(engines)),
	}

	for i, e := range engines {
		v, ok := e.rateDiscard(hand, myCrib, my, opp, throw)
		if !ok {
			return dealAnalysis{}, false
		}
		da.Discard.Engines = append(da.Discard.Engines, v)
		da.Rollup[i] = engineRollup{DiscardOptimal: v.Optimal, PeggingOptimal: true}
	}
	da.Discard.Agree = throwsAgree(da.Discard.Engines)

	for _, pd := range plays {
		p := playDecision{
			Count: pd.View.Count,
			// The pile is empty (not absent) on a lead: [] on the wire, never null.
			Pile:   orEmpty(pd.View.Pile),
			Hand:   pd.View.YourHand,
			Played: pd.Played,
			Legal:  pd.View.LegalPlays,
		}
		for i, e := range engines {
			v, ok := e.ratePlay(pd.View, pd.Played)
			if !ok {
				return dealAnalysis{}, false
			}
			p.Engines = append(p.Engines, v)
			if !v.Optimal {
				da.Rollup[i].PeggingOptimal = false
			}
		}
		p.Agree = playsAgree(p.Engines)
		da.Plays = append(da.Plays, p)
	}
	return da, true
}

// orEmpty turns a nil card slice into an empty one, so optional-but-present
// arrays encode as [] rather than null.
func orEmpty(cs []cribbage.Card) []cribbage.Card {
	if cs == nil {
		return []cribbage.Card{}
	}
	return cs
}

// keepOf is the four cards of hand not thrown.
func keepOf(hand [6]cribbage.Card, throw [2]cribbage.Card) [4]cribbage.Card {
	var keep [4]cribbage.Card
	n := 0
	for _, c := range hand {
		if c != throw[0] && c != throw[1] && n < 4 {
			keep[n] = c
			n++
		}
	}
	return keep
}

// throwsAgree reports whether every engine's best throw is the same unordered
// pair of cards.
func throwsAgree(vs []engineDiscardVerdict) bool {
	for _, v := range vs[1:] {
		if !samePair(v.BestThrow, vs[0].BestThrow) {
			return false
		}
	}
	return true
}

// playsAgree reports whether every engine's best play is the same RANK —
// suits never score during the play, so same-rank picks are the same move.
func playsAgree(vs []enginePlayVerdict) bool {
	for _, v := range vs[1:] {
		if v.Best.Rank != vs[0].Best.Rank {
			return false
		}
	}
	return true
}

// distinctRanks counts the genuinely different pegging moves available —
// suits are pegging-equivalent, so [5H 5S] is one move. Fewer than two means
// the play was forced, not a decision (the same rule the pegging training
// environment applies).
func distinctRanks(cards []cribbage.Card) int {
	var seen [14]bool // ranks are 1..13
	n := 0
	for _, c := range cards {
		if !seen[c.Rank] {
			seen[c.Rank] = true
			n++
		}
	}
	return n
}

// --- HTTP ------------------------------------------------------------------------

// handleGameAnalysisV2 serves the multi-engine post-game analysis for one
// finished game, from the requester's own seat. See the contract and access
// rules at the top of this file. Path: GET /games/{id}/analysis.
func (s *Server) handleGameAnalysisV2(w http.ResponseWriter, r *http.Request) {
	pg, ok := s.finishedGameSubject(w, r)
	if !ok {
		return // finishedGameSubject wrote the error
	}
	resp, err := analyzeGameV2(r.PathValue("id"), pg.seat, pg.events, analysisEngines())
	if err != nil {
		// A stored log that fails reconstruction is corrupt — a server-side
		// problem, never the caller's.
		writeErr(w, http.StatusInternalServerError, "could not analyze game")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// participantGame is one finished game resolved through a participant
// credential (finishedGameSubject): its full event log, the requester's seat,
// and the roster facts the replay response needs. Whichever credential
// resolved it — the live session or the stored Result — the fields carry the
// same values, so the two paths serve identical responses.
type participantGame struct {
	events []game.Event
	seat   game.Seat // the requester's seat
	names  [2]string // display names ("" for an unnamed guest / bot seat)
	bots   [2]bool   // true where a bot held the seat
	winner int       // the winning seat
}

// participantGameFromResult projects a stored Result for the given requester
// seat.
func participantGameFromResult(res Result, seat game.Seat) participantGame {
	return participantGame{
		events: res.Events,
		seat:   seat,
		names:  res.Names,
		bots:   [2]bool{res.Bots[0].Name != "", res.Bots[1].Name != ""},
		winner: res.Winner,
	}
}

// finishedGameSubject authenticates the requester as a participant of the
// finished game and returns it (event log, requester seat, roster), writing
// the error response itself otherwise. It backs both post-game endpoints that
// admit guests — GET /games/{id}/analysis and GET /games/{id}/replay — so
// their access semantics can never drift apart. Two credentials are accepted,
// tried in order:
//
//  1. The per-game player token (Bearer) against the live session registry —
//     the credential the participant played with, so it works for guests too.
//     A valid token on an unfinished game is a 409 (analysis/replay are
//     post-game only); the token proves participation, so unlike the paths
//     below this is allowed to acknowledge the game exists.
//  2. The login session (cookie) against the stored Result's player ids —
//     the durable path for registered users, exactly like v1 and replay.
//
// A request with a credential that grants no seat here gets a 404 — like
// replay, the endpoint never reveals that a game the caller can't see exists
// (this includes spectators and non-participants). A request with no
// credential at all gets a 401.
func (s *Server) finishedGameSubject(w http.ResponseWriter, r *http.Request) (participantGame, bool) {
	id := r.PathValue("id")

	token := bearer(r)
	if token != "" {
		if sess, ok := s.reg.get(id); ok {
			if seat, ok := sess.seatFor(token); ok {
				sess.mu.Lock()
				winner, over := sess.game.Winner()
				var pg participantGame
				if over {
					pg = participantGame{
						events: sess.game.Events(), // Events copies the log
						seat:   seat,
						names:  sess.names,
						bots:   [2]bool{sess.bots[0] != nil, sess.bots[1] != nil},
						winner: int(winner),
					}
				}
				sess.mu.Unlock()
				if !over {
					writeErr(w, http.StatusConflict, "game not finished")
					return participantGame{}, false
				}
				return pg, true
			}
		}
	}

	if u, ok := s.currentUser(r); ok {
		res, found, err := s.results.ResultByID(id)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "could not load game")
			return participantGame{}, false
		}
		if found && involves(res, u.ID) {
			seat := game.Seat0
			if res.PlayerIDs[1] == u.ID {
				seat = game.Seat1
			}
			return participantGameFromResult(res, seat), true
		}
		// Logged in, but this game isn't theirs (or doesn't exist): 404 either
		// way, so the endpoint never reveals that a game it can't show exists.
		writeErr(w, http.StatusNotFound, "game not found")
		return participantGame{}, false
	}

	if token != "" {
		// A token was presented but it opens no seat in this game (stale,
		// foreign, or the game is gone): same non-revealing 404.
		writeErr(w, http.StatusNotFound, "game not found")
		return participantGame{}, false
	}
	writeErr(w, http.StatusUnauthorized, "not authenticated")
	return participantGame{}, false
}
