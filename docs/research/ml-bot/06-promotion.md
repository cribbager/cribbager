# Chapter 6 — Phase 3: assembly, a dead safety net, and promotion

*2026-07-09. Code: `internal/bot/mlbot.go` (+ embedded weights),
`internal/bot/peg/` (moved out of lab), `SEED` on the fixtures gate.
Result: the production bot **`ml`** ships — champion discards + learned
pegging — selectable via `POST /games {"bot":"ml"}`.*

## The assembly question

The promotion candidate composes the program's winners: the champion's
win-probability discards (chapters 2–3 proved imitation could only tie the
exact evaluator, so it stays) and the learned pegging (chapter 5 proved it
beats the champion's). One design question remained: the net's training
reward was score-blind point differential — it does not know a point can be
worth a game. Should it hand off to the champion's win-aware pegging once
someone is in reach of the target (`eval.InReach`, the same boundary the
champion's own objectives switch on)?

Theory said yes. We built both and let the instruments decide.

## The instruments answer

**Fixtures, pure net** (score-blind everywhere; 8 positions × 1500 pairs,
seeds disjoint from training): pooled windiff **+0.002 [−0.004, +0.008]**,
every per-fixture CI spanning zero. The feared endgame regression does not
exist at measurable size. In hindsight the mechanism is intuitive: in a
pegging race near 121, scoring points *now* and winning are nearly the same
objective, and the net simply pegs better.

**Full-game gate, hybrid vs pure** (10,000 pairs, virgin seeds):

| candidate | margin vs champion | windiff |
|---|---|---|
| pure net pegging (`ml-peg`) | **+0.70 [+0.46, +0.95]** | +0.007 [−0.000, +0.015] |
| handoff hybrid (`ml-hybrid`) | +0.28 [+0.10, +0.47] | +0.001 [−0.005, +0.008] |

The safety net wasn't just unnecessary — it was expensive. The handoff
surrenders the net's pegging for roughly the half of the game where
`InReach` holds, and that costs 0.42 pts/pair of real margin while buying
zero measurable win-safety. The hybrid was deleted per lab convention (this
chapter and git history are its record). The lesson runs in both directions:
we didn't ship the score-blind net on vibes — we tested the fear; and we
didn't keep the safety net on vibes either — we tested the cost.

## The promotion

`internal/bot/mlbot.go`: production bot **`ml`**, version 1. Champion
discards (`eval.BestDiscardWin`), greedy Q-network pegging (`peg.Net`).
Deterministic. The weights are **embedded in the binary** (`go:embed`) — a
shipped bot cannot depend on a file lying around at runtime; corrupt
embedded weights panic at construction because they are a build artifact,
not an input. Registry line added; the server's `GET /bots` lists it and
`POST /games {"mode":"bot","bot":"ml"}` seats it. **Champion remains the
default opponent** — whether `ml` should unseat it is a product decision,
deliberately not made in this chapter.

Plumbing that moved: `peg` relocated from `internal/bot/lab/peg` to
`internal/bot/peg` (production code may not depend on the lab), dropping its
`internal/bot` import (a `Discarder` interface replaces `bot.Champion()` in
`Generate`; the DealStats cross-check became an external test package —
`bot` now imports `peg`, so the cycle had to break somewhere visible). The
fixtures gate gained the same `SEED` override as the main gate, same
rationale: a trained challenger must be examined on deals it never studied.

## Where the program stands

- Hypothesis 1 (series-level pegging tactics): **confirmed and shipped.**
- Hypothesis 2 (pegging-aware keeps) and 3 (early-game positional
  discarding): open — both need outcome-learned discard (Phase 4), for which
  the self-play machinery now exists.
- Also available: targeted vs-champion training (`pegdata -opponent
  champion`) if more pegging margin is ever wanted; win-aware pegging
  (score-conditioned reward) as a later refinement.

## Reproduce

```
CHALLENGE=ml-peg PAIRS=1500 SEED=3000000 go test ./internal/bot/lab -run ChallengerFixtures -v
CHALLENGE=ml-hybrid PAIRS=10000 SEED=1000000 go test ./internal/bot/lab -run ChallengerVsChampion -v  # (deleted; git history)
go test ./internal/bot -run MLBot -v
```
