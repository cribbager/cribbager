# Game display & interaction variations

A catalog of the ways the in-game experience *could* be presented and driven, so
that design decisions are made against a mapped space instead of ad hoc. It has
three jobs:

1. **Guide design** — a menu of options to choose from (and to steal from
   competitors) when we redesign the board, especially for tablet/mobile.
2. **Feed a user-options system** — many of these are legitimately player
   preferences, not one-true-answers. Each variation has a **stable id** so it
   can become a settings key without renaming later.
3. **Spec a gameplay builder** — a screen that lets us (and eventually players)
   pick variations and render the game that way, to *test* combinations. The id
   scheme below is that builder's config schema in embryo.

Nothing here is a commitment. It is the design space; picking points in it is the
next conversation. Related: `../research/apps-and-venues.md` (competitors),
`../research/newuser-journey-review.md` (H3/U3 mobile findings), backlog U3.

## How to read an entry

Each **dimension** is one independent choice. Format:

- **`dimension.id`** — one-line description.
  - `option-id` — what it is. *(mobile: good/ok/poor — why.)*
  - **Today:** the current cribbager default.
  - *Depends on / conflicts with:* cross-dimension interactions.

"Mobile" ratings assume a ~375px-wide phone held portrait; "tablet" is treated as
a small desktop unless noted. The hardest constraint everywhere is **horizontal
width** — anything that grows sideways (per-player columns, side rails, wide card
rows) is the first thing to break on a phone.

---

## A. Messages & narration

The running cribbage "voice": "fifteen for 2", "his heels for 2", "go", show
counts. These are *ephemeral* today, which is the root reason the game needs
pauses (a message shown then overwritten must be caught in a time window). How
messages are displayed and advanced is therefore coupled to the whole
pause/snappiness question — see "Cross-cutting: pacing" below.

- **`messages.display`** — where a narration line appears.
  - `per-player` — a speech area anchored to each player. *(mobile: ok — two zones fit vertically.)*
  - `single` — one shared message area (e.g. under the board), speaker named or implied. *(mobile: good — one zone, no width cost.)*
  - `popup` — a transient toast/callout with player name + message. *(mobile: good; risk of covering cards.)*
  - **Today:** `per-player` (`state.speech[player]`, one bubble per seat).

- **`messages.advance`** — how a message yields to the next.
  - `timed` — disappears/overwrites after a pause (current model; needs the pause).
  - `click` — the player clicks "OK/next" to advance when a message *would* be lost (opt-in for users who never want to miss a call). *(mobile: good — tap is natural.)*
  - `persist-log` — messages don't need catching because a per-hand log keeps them; the live line can then advance instantly. *(mobile: log can be a collapsible drawer.)*
  - **Today:** `timed`.
  - *Note:* `persist-log` and `click` both largely remove the *need* for reading-window pauses (see pacing). A player option across these three is plausible.

- **`messages.log`** — is there a reviewable history?
  - `none` | `per-hand` (clears each deal) | `full-game`.
  - **Today:** `none` (except the show breakdown via Explain).

---

## B. Pegging area

- **`pegging.layout`** — where played cards accumulate during the play.
  - `per-player` — each player's plays sit in their own zone (current). *(mobile: two zones stack ok; each zone still a horizontal row that can overflow.)*
  - `unified` — one shared pile/track, cards staggered in play order (closer to a real table). *(mobile: good — a single row/stagger uses space best; ownership shown by offset/tint.)*
  - **Today:** `per-player` (`.pegged` per seat).
  - *Depends on:* `cards.layout` (overlap vs side-by-side), `pegging.round-sep`.

- **`pegging.round-sep`** — are successive count-to-31 "rounds" visually separated?
  - `spaced` — a gap/divider starts each new round after a go/31. *(clarifies the count history.)*
  - `continuous` — no separation.
  - **Today:** effectively `continuous` (pile resets; no lingering round boundary).
  - *Pairs well with:* `unified` layout, where a stagger + gap reads as rounds.

---

## C. Cards

- **`cards.layout`** — how multiple cards in a row relate.
  - `overlap` — fanned/overlapping to save width (current, −28px). *(mobile: good for width; can hide rank pips.)*
  - `side-by-side` — full separation. *(mobile: poor — widest option.)*
  - **Today:** `overlap` for played cards; hand cards spaced.

- **`cards.design`** — the face style.
  - `traditional` (current) | future skins (minimalist, large-rank, high-contrast, four-color deck). Already pluggable (`cardFace.js`, board-/card-designer tools exist).
  - **Today:** `traditional`.
  - *Accessibility:* a four-color / high-contrast deck is an a11y option, not just taste.

---

## D. Board (the pegging track)

- **`board.style`** — how score is shown.
  - `numeric` — just the numbers (compact; no board metaphor). *(mobile: excellent — tiny footprint.)*
  - `long` / straight board (current). *(mobile: poor as a wide horizontal; ok if oriented vertical or scaled down.)*
  - `classic` — the folded 3-street board. *(mobile: poor — large.)*
  - **Today:** `long` (straightBoard).
  - *Note:* on mobile, `numeric` (or a very small board) may be the only viable default; the board is the biggest single space consumer.

- **`board.location`** — where the board sits relative to the players.
  - `vertical-top` | `vertical-bottom` | `vertical-middle` (current "board in the middle") | `left-rail` | `right-rail`.
  - **Today:** `vertical-middle` (opponent top, board middle, you bottom).
  - *Depends on:* `board.style` (a rail wants a vertical/numeric board), screen size. This dimension is the backbone of the whole layout — most other placements are relative to it.

---

## E. The show

- **`show.sequence`** — reveal order at the count.
  - `all-at-once` (current) | `one-at-a-time` — pone hand → dealer hand → crib, each acknowledged (more like a real count; more teaching-friendly). *(mobile: `one-at-a-time` fits better — one hand on screen at a time.)*
  - **Today:** `all-at-once`.
  - *Depends on:* `messages.advance` (a stepped show implies click-to-advance).

- **`show.scoring`** — how much scoring detail.
  - `total-only` | `explain-always` (breakdown inline) | `explain-on-click` (current — total + Explain affordance).
  - **Today:** `explain-on-click`.
  - *Depends on:* assist-mode settings; the Explain overlay already exists.

---

## F. Opponent hand

The opponent's hand is *never required* on screen — during pegging you know their
remaining **count**, and their cards are hidden until the show anyway. This is
one of the biggest width/space levers, especially on mobile.

- **`opponent.hand`** — show a representation of the opponent's held cards?
  - `show` — render card backs / placeholders (current). *(mobile: costs a row.)*
  - `count-only` — just "3 cards left", no card row. *(mobile: excellent — reclaims a whole zone.)*
  - `hide` — nothing until the show.
  - **Today:** `show` (count tracked as `botHandCount`; a card representation is drawn).

- **`opponent.hand-size`** — if shown, at what size.
  - `full` | `small/mini`.
  - **Today:** `full`.
  - *Mobile:* `count-only` or `small` are the realistic phone choices.

---

## G. Player / competitor info

- **`info.location`** — where names/stats live.
  - `right-rail` (current) | `left-rail` | `top-bar` | `bottom-bar` | `by-board`.
  - **Today:** `right-rail` (the versus/results rail, outside the felt).
  - *Mobile:* rails are width-hostile; `top-bar`/`bottom-bar` (thin, full-width) are the mobile-friendly forms.

- **`info.content`** — what's shown per player.
  - `name` | `+record` (W–L) | `+head-to-head` (record vs this opponent) | `+rating`.
  - **Today:** `name` (record/H2H depend on stats work — backlog E1/E2).
  - *Depends on:* the stats/leaderboard backlog; head-to-head needs opponent identity + history.

---

## H. Dealer / crib indicator

- **`dealer.indicator`** — how "who deals / whose crib" is signaled.
  - `crib-space` — a labeled crib slot on the dealer's side of the board (current-ish). | `name-badge` — a flag/chip by the dealer's name. | `both`.
  - **Today:** the crib appears in the dealer's pegging area during the show; a persistent explicit dealer flag is weak.
  - *Note:* a clear always-on dealer indicator is a known newcomer gap (they don't track whose crib it is). Cheap, high-clarity win independent of the big redesign.

---

## I. Starter card

- **`starter.location`** — where the cut card sits.
  - `left-of-board` | `right-of-board` | `above-board` | `below-board` | `by-pile` | `inline-with-hand-at-show`.
  - **Today:** shown with the hand+starter grouping at the show; during play it sits with the board/cut area.
  - *(This is the direct subject of BUG-fix work — starter spacing at the show — now resolved.)*

- **`starter.label`** — labeled ("starter/cut") or bare.
  - `labeled` | `unlabeled`.
  - **Today:** `unlabeled`.
  - *Depends on:* beginner mode — a label is a teaching aid (glossary/NU work).

---

## Cross-cutting concerns

### Pacing (pauses) — coupled to A
Whether the game needs pauses at all is not an independent dimension; it *falls
out of* `messages.display` + `messages.advance` + `show.sequence`. Ephemeral,
timed messages *require* reading-window pauses; a persistent log or click-advance
*removes* the requirement. So "should we shorten/remove pauses" is downstream of
choosing message variations — which is exactly why we're not tuning timers before
mapping this space. (See the pause audit in the session history / backlog PERF1.)

### Mobile / tablet viability (the width budget)
The recurring constraint. Phone-hostile choices: side rails (`info.location`
left/right), `side-by-side` cards, the `long`/`classic` board horizontal,
`opponent.hand = show/full`, `pegging.layout = per-player` (two horizontal rows).
Phone-friendly defaults likely cluster as: `single`/`popup` messages,
`numeric`-or-tiny board, `top/bottom-bar` info, `count-only` opponent hand,
`unified` pegging, `overlap` cards, `one-at-a-time` show. That cluster is
effectively a **Mobile Compact preset** (below).

### Presets / profiles
Users won't tune 15 knobs. The realistic product is a few named presets that
bundle choices, with advanced override. Candidate presets to design toward:
- **Desktop Classic** — today's board-in-the-middle, per-player everything, long board.
- **Mobile Compact** — the phone-friendly cluster above.
- **Beginner / Coached** — labeled starter, always-on dealer indicator,
  one-at-a-time show, explain-always, click-advance messages, persistent log.
- **Minimalist / Fast** — numeric board, single message area, count-only
  opponent, instant advance.
A preset is just a named set of dimension→option bindings — i.e. a saved builder
config.

### Accessibility
`cards.design` (four-color/high-contrast), message persistence (don't force
time-limited reading), click-advance (motor/cognitive pacing control), and font
scaling cut across every preset and should be first-class, not afterthoughts.

### The gameplay builder (aspiration)
Because every dimension above has an id and an option set, a builder is a form
over this schema: pick options → serialize to a config object → the game renderer
reads the config and lays out accordingly. Value: test combinations quickly, find
good presets empirically, and (later) expose a subset as player settings. This
implies a refactor where the game view reads layout from a config rather than
hard-coding it — a large effort, noted as the north star, not a near-term task.

---

## Competitor scan (to fill in)

Placeholder to record what shipping cribbage apps/venues actually do per
dimension — the owner may survey these. Seed list in
`../research/apps-and-venues.md`. For each competitor, note its choices for:
board style/location, opponent-hand visibility, message model, show sequence,
and mobile layout. Divergences from our defaults are candidate options worth
stealing.

| App | Board | Opp hand | Messages | Show | Mobile layout |
|-----|-------|----------|----------|------|---------------|
| _(tbd)_ | | | | | |

---

## Open questions

- Which dimensions are genuinely **user options** vs. single design decisions we
  just make? (Over-optioning is its own cost.)
- Do we design **mobile-first** (pick the Mobile Compact cluster, let desktop be
  the enhanced version) or keep desktop as the reference and derive mobile?
- Is the **builder** worth building, or do we hand-pick 2–3 presets and skip the
  generality?
- Minimum set for a **first mobile-playable** board (backlog U3): almost
  certainly numeric/tiny board + count-only opponent + single/popup messages +
  unified or single-column pegging. Confirm before U3 design.
