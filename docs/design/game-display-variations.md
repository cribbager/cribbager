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

> **Review note (2026-07-10):** the "Today:" lines were spot-checked against
> `web/public/src/ui/main.js` + `styles.css`. Most are accurate; corrections are
> inlined below where a claim was incomplete or stale (`messages.advance`,
> `dealer.indicator`, and the mobile-width baseline — see Review additions).

"Mobile" ratings assume a ~375px-wide phone held portrait; "tablet" is treated as
a small desktop unless noted. The hardest constraint everywhere is **horizontal
width** — anything that grows sideways (per-player columns, side rails, wide card
rows) is the first thing to break on a phone.

---

## The master axis: instructional ↔ efficient (owner directive, 2026-07-10)

Before the individual dimensions, the lens that explains *why the current design
is what it is* and gives the presets a principled ordering.

**Today's design deliberately mimics over-the-board, in-person play — as a
teaching aid.** It is optimized to help someone learn *physical* cribbage, not to
be the most screen-efficient. The clearest tell is `pegging.layout = per-player`:
each player's played cards sit in their own zone because, at a real table, you
would **never** merge both players' cards into one staggered pile — you'd lose
track of whose cards are whose when gathering them at the show. So the current
per-player layout is *faithful to the physical game*, and a `unified` staggered
pile — while more screen-efficient — is a screen-native convenience that has no
over-the-table equivalent. Same logic runs through the whole current UI: it
teaches the real game's shape.

This defines a spectrum every layout choice sits on:

- **Instructional / faithful** — mirrors the physical table, makes every element
  explicit, teaches over-the-board play. Spends screen space to do it. (Today's
  desktop design.)
- **Efficient / minimal** — screen-native, shows only what's needed, lets the
  skilled player infer the rest. Wins on mobile. (Where a phone layout must go.)

**Reframe the presets against this axis** (cleaner than the ad-hoc names in the
Presets section): *Faithful/Coached* (instructional end) → *Standard* → *Compact*
→ *Minimal* (efficient end). "Mobile-first vs desktop-derived" is really "which
end of this axis is canonical" — **DECIDED (owner, 2026-07-10): mobile-first.**
The efficient/minimal end is canonical and will emerge from the focused push to
build a decent mobile interface (backlog U3); the instructional/faithful
presentation becomes the enhanced desktop preset. The instructional value is real
and worth preserving as an **option/preset**, not the only mode — the same game
can present faithfully for a learner and minimally for a phone veteran.

## Information model: what's needed, when, and how obvious (owner directive, 2026-07-10)

A method for deciding each element's treatment instead of choosing display
options blind. For every piece of game information, ask three things:

1. **Necessary?** Must the player have it to make the current decision?
2. **Deducible?** Can a skilled player *infer* it from what's already on screen —
   so showing it explicitly is a teaching aid, not a requirement?
3. **Obvious ↔ inferable presentation?** Beginners want it explicit and
   labeled; advanced players are fine with subtle (or nothing). This maps
   directly onto the instructional↔efficient axis and onto presets.

Worked examples (the guidance the owner asked for):

| Information | Necessary? | Deducible from… | Beginner (obvious) | Advanced (infer/subtle) |
|---|---|---|---|---|
| Opponent's hand | No | count of cards already played (13 − played − starter) | show a mini fanned back cluster | hide entirely — just a status line, or nothing |
| Opponent card *count* | **No (owner, r2)** | plays so far (you can count what's been played) | explicit "3 left" chip | infer from the pile / not shown |
| Whose deal / crib | Yes, but | who dealt last / crib position | labeled crib slot + dealer flag by name | **conveyed at discard time by the action copy** ("Discard to *your* / *opponent's* crib" — already Today); a subtle dot otherwise |
| Running count to 31 | Yes | sum of the pile (hard mid-play) | persistent "Count: 22" chip | **spoken in the call only (today — the owner keeps this: it mirrors an in-person game)** |
| Whose turn | Yes | which cards are clickable + **the opponent's spoken call** (today) | banner / seat highlight | **inferred from the opponent's speech (today — owner: this is by design, not a bug)** |
| Starter | Yes | — (must be shown) | labeled "starter" | unlabeled, positioned by convention |
| Scoring (why N points) | No (the total is) | the cards themselves | explain-always breakdown | total only, tap to explain |

The pattern: **necessary + not-deducible → always show; necessary + deducible →
show for beginners, allow inference for advanced; not-necessary → default off,
offer as a teaching toggle.** This turns "how obvious should X be" from taste into
a rule, and it's exactly what distinguishes a *Coached* preset from a *Minimal*
one — same information model, different obviousness thresholds.

**A third channel (owner, round 2 — the key refinement):** information doesn't
have to live in a *persistent visual element* or be left to *inference*. Much of
it can ride the **action / status / narration channel** — the button you're about
to press, a one-line status message, or the spoken call:

- *Whose crib* → the discard button already says "Discard to your / opponent's
  crib." No separate persistent indicator strictly required.
- *Opponent has discarded* → showing their card backs is one signal, but a
  **"waiting for opponent to discard"** status line serves the same purpose and
  costs no space (see the new `opponent.status` dimension).
- *Whose turn* → the opponent's spoken count ("…for 2") is itself the hand-off
  cue (owner: intentional, mimics the table).
- *Running count* → embedded in the call, as at a real table (owner: keep).

This channel is the backbone of the **efficient/minimal (mobile-first)** end of
the axis: it lets the phone layout drop whole persistent elements without losing
the information, because the information moves into copy the player already reads.

## Owner-added dimensions (2026-07-10)

New dimensions/options from the owner's design pass, beyond the draft and the
independent review:

- **`hints` (assist / tips display)** — *predictive* guidance surfaced during a
  decision, distinct from the post-hoc show breakdown. Options:
  `none` (today, official play — see the analysis-out-of-live-play rule) |
  `on-select-preview` — as you *tentatively* pick a card/discard, show its
  consequence (the pegging points it would score, or the resulting hand/crib EV
  for a discard) before you commit | `post-action` — reveal what a play/discard
  scored or gave up *after* it happens | `full-coach` — ongoing recommended move.
  *Depends on:* `confirm-move` (a select-then-confirm model is what makes an
  on-select preview possible — you need a "selected but not played" state to
  preview into). *Gated by:* assist-mode rules (never in rated games; backlog
  AM1–AM3). This is the display-timing side of assist mode.

- **`scoring.breakdown` (verbosity of a score explanation)** — how a score is
  *decomposed* when shown, orthogonal to `show.detail`/`show.explain-trigger`
  (which decide *whether/when* to explain). Options: `verbose` — enumerate every
  atom ("fifteen 2, fifteen 4, pair 6, run 9…") | `shorthand` — bundle combos
  ("double run of 3 for 8") | `expandable` — shorthand that a beginner can tap to
  expand into its atoms. Teaching value lives in `verbose`/`expandable`; experts
  prefer `shorthand`. (Ties to the deferred hand-scorer verbose-run-breakdown
  work — this is its display dimension.)

- **`interaction.commit` (clarifying `confirm-move`)** — the owner frames this as
  a first-class choice for BOTH play and discard: `single-click` — one tap plays
  the card / throws the selected discards immediately (fast, fat-finger risk) |
  `select-then-confirm` — tap to select (a reversible "armed" state), then a
  Confirm button commits. The `select-then-confirm` state is the natural host for
  an `on-select` hint preview (above) and for undo. (This restates the review's
  `confirm-move` dimension with the owner's explicit play-and-discard framing and
  the select-preview linkage; keep one id.)

### Round 2 (owner, 2026-07-10)

- **`opponent.status` (NEW dimension)** — a status line for what the opponent is
  *doing right now*: `waiting-for-discard` | `waiting-for-play` |
  `reviewing-score` | `generic` ("waiting for opponent…") | `off`. This is the
  space-free alternative to `opponent.hand=show` for the one thing showing their
  cards actually signals in a human game — *that they've discarded / are still
  thinking*. Pairs with `opponent.hand=count-only`/`hide` on mobile: drop the card
  row, keep the knowledge, in one text line. *(Bot games don't need it much — the
  bot is instant; it earns its keep in human-vs-human.)*

- **`opponent.hand` — refined role.** Showing the opponent's card backs currently
  doubles as the "they have discarded" signal; `opponent.status` decouples that,
  so `count-only`/`hide` becomes viable without losing the cue. And per the
  information model above, the *count* itself is deducible — so `count-only` is a
  convenience, not a necessity.

- **`dealer.indicator` — refined.** Crib ownership is *already* conveyed by the
  discard action copy ("Discard to your / opponent's crib"), so a persistent
  crib/dealer indicator is **optional, not required** — a subtle name-badge for
  beginners at most, nothing for veterans. Downgrades this from "gap to fill" to
  "nice-to-have."

- **`board.style` — numeric is additive, not exclusive.** Even *with* a board
  shown, a plain **numeric score readout can coexist** (board for the metaphor +
  a "97 – 88" number for at-a-glance reading). So `numeric` is better modeled as a
  companion toggle (`board.numeric-readout: on/off`) than a mutually-exclusive
  board style — reinforcing the review's split of `board.style` into
  metaphor × readout.

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
  - **Today:** `timed` **for pegging calls**, but the **show is already `click`** — the reveal is gated by a `Continue` button / space / Enter (`waitContinue` in `main.js`). So "Today" is really a *mix*, not pure `timed`; the two phases advance differently. *(Review note 2026-07-10.)*
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
  - **Today:** during the show the crib cards are revealed in the dealer's pegging area. **Correction (2026-07-10):** during *play* the dealer already gets a labeled dashed `crib` slot + starter on their pegging row (`deckGroup()` in `main.js`, drawn only when `state.dealer === p && !state.show`) — so a weak form of `crib-space` is *already Today*, not absent. What's genuinely missing is a **persistent, always-on** dealer flag (the `crib` slot vanishes at the show, and there's no name-badge). So the real gap is `name-badge`/`both`, not `crib-space` from zero.
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

> **Mobile baseline correction (2026-07-10):** the newuser-journey H3 finding
> ("cards overflow, pinch-zoom disabled, no `overflow-x`") is now *partly stale*.
> `game.html` no longer sets `maximum-scale=1` (only `learn.html` still does), and
> the `@media (max-width:560px)` block in `styles.css` now sets
> `.pegged, .show-row { overflow-x: auto }` and `.felt/.table-inner { overflow-x:
> hidden }`, with cards shrunk to `--card-w:52px`. So on a phone the play/show rows
> now **scroll inside their zone** rather than clipping the page — U3 is *partially
> in progress*, not untouched. The remaining problem is not "it clips" but "a
> horizontally-scrolling pegging row is a poor phone interaction" — which is exactly
> what the layout dimensions below (`unified` pegging, `count-only` opponent,
> `numeric` board) are meant to remove. Frame U3 as *reflow*, not *rescue*.

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

## Review additions (2026-07-10)

An independent critique-and-enrich pass. Everything below is *new* relative to the
original draft; the sections above were left intact except for dated inline
corrections. Nothing here renames an existing id — id changes are floated as
suggestions only.

### R1. Framing critiques (where the decomposition strains)

1. **Several "dimensions" are entangled, not independent — the doc admits some but
   not all.** The doc flags pacing↔messages and a few `Depends on`. Missed
   couplings worth naming explicitly:
   - `pegging.layout=unified` and `messages.display` are entangled through
     *ownership* — a unified pile needs owner cues (tint/offset), which is the same
     signal a `single` message area needs (who's speaking). Choose them together.
   - `board.location` and `info.location` are **the same layout decision** viewed
     twice: both compete for the vertical/edge budget. On a phone you can't put a
     rail board *and* a rail info panel; picking one constrains the other. They read
     as independent knobs but aren't.
   - `opponent.hand` and `pegging.layout` jointly decide how many horizontal rows
     exist. `count-only` + `unified` is one row; `show/full` + `per-player` is four.
     The width budget is a *product*, not a sum, of these — the doc treats them
     additively.

2. **`opponent.hand-size` is a false sub-dimension.** `full` vs `small/mini` is a
   continuous *scale factor*, not an enum, and it's really just card scaling applied
   to one zone. Suggest folding it into a single `cards.scale` continuous setting
   (see R3) rather than a per-zone enum — otherwise you'll grow a `*.hand-size` twin
   for every zone.

3. **`board.style` mixes two axes.** `numeric` vs `long` vs `classic` conflates
   *metaphor* (board vs bare number) with *shape/footprint* (straight vs folded).
   A "small straight board" and a "large straight board" are the same style at
   different scale; "numeric" is a different *kind* of thing. Suggest splitting into
   `board.metaphor` (`peg-board` | `numeric` | `hybrid`) × `board.shape`
   (`straight` | `folded` | `rail`) × scale. As written, adding a "tiny board" forces
   a new enum value instead of a scale tweak.

4. **`starter.location` is nearly continuous and over-specified as 6 enums.** Six
   compass positions relative to the board is really "an anchor + a side." It will
   also *conflict* with `board.location` (a `left-rail` board can't take a
   `left-of-board` starter cleanly). Consider `starter.anchor` (`board` | `pile` |
   `hand`) + a derived side, rather than 6 absolute slots.

5. **Naming collisions / schema hygiene for the future config:** `messages.log`
   and a proposed game-event log; `info.location` vs `board.location` vs
   `starter.location` all use bare compass words (`left-rail`, `top-bar`,
   `above-board`) in *different value spaces* — a builder form will want a shared
   `Placement` type so these validate consistently. And several "options" are
   really **booleans dressed as enums** (`starter.label` labeled/unlabeled =
   `starter.label: bool`; `pegging.round-sep` spaced/continuous = bool). Reserve
   enums for genuinely 3+ mutually-exclusive choices; make the binaries booleans so
   the settings UI renders them as toggles.

6. **Mis-categorization: `show.scoring` is half interaction.** `explain-on-click`
   is an *interaction* affordance (a tap target), while `total-only` /
   `explain-always` are *display density*. They're bundled as one enum but a user
   could plausibly want "always explain, but only on tap." Consider
   `show.detail` (density) × `show.explain-trigger` (auto | click | off).

7. **The doc is glib that presets are "just a named set of bindings."** True as
   data, but presets also need **responsive fallthrough** (what happens when Desktop
   Classic is opened on a 375px phone?) and **conflict resolution** (two bindings
   that can't co-exist). A preset system is a small constraint-solver, not a dict.
   Worth saying so before someone scopes it as trivial.

### R2. New options inside existing dimensions

- **`messages.display += rail-log`** — a persistent side/bottom transcript instead
  of ephemeral bubbles (what BGA does with its move log). Strong desktop option.
- **`pegging.layout += table-realistic`** — cards angled/fanned toward each seat
  (the physical-table look), distinct from a flat `unified` stagger.
- **`board.style += ring/circular`** and **`+= no-board`** (Cribbage Pro literally
  ships "No Board" — scores by the avatar; this is your `numeric` validated in the
  wild).
- **`opponent.hand += fanned-back-mini`** — a tiny fanned card-back cluster that
  reads as "a hand" for one-glance orientation without costing a full row.
- **`show.sequence += dealer-suspense`** — reveal pone, then crib last (the
  traditional "his crib" tension) as an explicit ordering, not just
  `one-at-a-time`.
- **`info.content += streak / mmr / title`** and **`+= turn-clock`** (see R3).
- **`dealer.indicator += felt-rotation`** — rotate the whole table so the dealer is
  always "you-side," a common physical convention; heavy but an option.

### R3. Entirely new dimensions the draft is missing

Ranked by value for the mobile redesign (H = high, worth designing now; M = medium;
L = nice-to-have / later).

- **`turn.indicator` (H)** — *how "it's your turn" is signaled.* Options:
  `none` (today — implicit from enabled cards) | `highlight-seat` | `pulse-cards`
  | `banner` | `arrow`. This is the single biggest missing dimension: newcomers
  routinely don't know it's their move. On mobile a clear turn cue matters more than
  any board style. Today cribbager signals turn only by which cards become clickable
  — effectively `none`.
- **`count.display` (H)** — *how the running count-to-31 is shown.* Options:
  `spoken-only` (today — it's embedded in the call text "15 for 2") | `persistent-number`
  (a live "Count: 22" chip) | `on-pile` (number rendered on the pegging pile) |
  `both`. The draft catalogs *messages* and *pegging cards* but never the **count
  itself**, which is the one number a player needs every second of the play. Big gap.
- **`timer` (M)** — `none` (today) | `soft` (gentle nudge) | `hard` (turn clock,
  needed for ranked/async). Entangled with `info.content` turn-clock display.
- **`motion` (M)** — *is peg/card movement animated?* `instant` | `animated-peg`
  (the peg physically travels the track — big teaching + delight value) |
  `animated-cards`. Interacts with `prefers-reduced-motion` (already respected in
  CSS) — so this needs an a11y override, making it a real dimension not just polish.
- **`sound` (M)** and **`haptics` (M, mobile)** — `off` | `sfx` | `sfx+voice`
  (Grandpas ships ambient + voice); haptic tap on peg/score. Accessibility-relevant
  (non-visual turn/score cues) and a mobile expectation.
- **`confirm-move` (M)** — `off` (today — tap a card = it's played) | `tap-tap`
  | `drag-to-play` | `undo-window`. Fat-finger protection on mobile; also a
  teaching-mode safety net. Currently an accidental tap is irreversible.
- **`orientation` (H, mobile)** — `portrait` (target) | `landscape` |
  `responsive-both`. The doc assumes portrait but never makes it a choosable axis;
  landscape radically changes which layouts work (rails become viable again).
- **`hand.sort` (M)** — `rank-then-suit` (today, per MEMORY convention) |
  `by-suit` | `as-dealt` | `manual-reorder`. A real preference in physical play;
  cheap to offer.
- **`disconnect.resume` (H)** — *now that bot games resume on refresh, what does the
  player see on return?* `silent-restore` (today) | `recap-banner` ("you're
  mid-play, count is 22, your turn") | `replay-last-N`. The resume feature exists but
  its *display* is unspecified; a returning player can be disoriented mid-hand.
- **`handedness` (L)** — `right` (today, actions/rail on the right) | `left`
  (mirror). Cheap once layout is config-driven; genuine accessibility/comfort win.
- **`spectator` (L→M)** — `off` | `read-only-live` | `with-log`. Cribbage Pro ships
  live spectating; relevant if human multiplayer grows.
- **`colorblind` / `a11y.palette` (H as a11y, currently buried under `cards.design`)** —
  promote to its own dimension: `default` | `deuteranopia` | `protanopia` |
  `tritanopia` | `high-contrast`. It cross-cuts cards *and* peg/track colors and
  you/bot call tints (`--you`/`--bot`), so it's not a card-skin sub-option — it's a
  global theme axis.
- **`font.scale` (H as a11y)** — the doc mentions font scaling in Accessibility prose
  but never gives it an id; it belongs as a first-class continuous setting.

### R4. Honest "not worth it (yet)" list

`handedness`, `spectator`, `felt-rotation`, and the full `board.metaphor×shape×scale`
refactor are real but low-ROI until (a) human multiplayer matters and (b) the game
view is already config-driven. Flagging so they don't inflate U3 scope. The
high-value cluster for the *mobile* problem is narrow: `turn.indicator`,
`count.display`, `opponent.hand=count-only`, `board.style=numeric/no-board`,
`orientation`, plus `disconnect.resume` recap.

---

## Competitor scan (to fill in)

Placeholder to record what shipping cribbage apps/venues actually do per
dimension — the owner may survey these. Seed list in
`../research/apps-and-venues.md`. For each competitor, note its choices for:
board style/location, opponent-hand visibility, message model, show sequence,
and mobile layout. Divergences from our defaults are candidate options worth
stealing.

Filled 2026-07-10 from web research. Cited claims link a source; where an app's
internal UI could not be verified from the open web, it is marked *(not web-verified)*
rather than guessed — those need a hands-on survey (install + screenshot) to confirm.

| App | Board | Opp hand | Messages | Show | Mobile layout |
|-----|-------|----------|----------|------|---------------|
| **Cribbage Pro** (Fuller Systems)[^cp1][^cp2] | Non-standard track; **scores shown next to each player's avatar**. Offers a **"No Board"** mode (reclaims play area) and a **"Tournament Style"** board — i.e. board style is a *user option*, incl. a numeric-only-ish fallback. | Hidden in play; **opponent's hands shown in the post-game Game Summary**; live **spectate** of friends' games (beta). | Avatar-anchored; count can be **auto or self-counted (muggins)**. | Post-game summary lists hands/scores. *(In-game show sequence not web-verified.)* | iOS/Android/Mac native; "No Board" exists specifically to free up screen area — validates our `board.style=numeric` mobile bet. |
| **Cribbage With Grandpas**[^cwg1][^cwg2] | Board **borders/frames the screen edge** (not a central slab). | Character opponent ("Grandpa") at a virtual table; hand hidden until show *(not web-verified)*. | Warm character voice + **ambient sound**; narration themed to the chosen setting. | *(Not web-verified.)* | **Explicitly portrait, one-handed play**; "large cards and buttons… perfect for small screens or big fingers." The clearest mobile-first exemplar. |
| **Cribbage Classic**[^cc1] | Standard board *(not web-verified)*. | *(Not web-verified.)* | Hint system flags **sub-optimal plays**. | **Manual-count option (teaching) vs "fast mode" full auto-count.** | iPhone-native. Orientation not web-verified. |
| **MSN / Microsoft Cribbage**[^ms1] | Standard board in browser. | Hidden until show. | Terse. | **Count-it-yourself or auto** ("fast mode… counting is done for you"). | Browser; responsive-ish. Specifics not web-verified. |
| **Board Game Arena**[^bga1] | Peg **board shows each player's score**; standard rules. | Hidden until show (real-table model). | Play announces **cumulative count aloud** ("four", "eleven", …) as log lines. | Standard show/counting per rules. *(Exact UI not web-verified.)* | Browser; BGA's generic responsive shell, not cribbage-tuned. |
| **CardGames.io** ("Bill" AI)[^cg1] | **Custom-made board**, HTML/CSS/JS + jQuery animations. | Hidden until show. | Minimal; **auto-count** (casual, frictionless). | Auto. | Browser + native wrapper; the frictionless-instant benchmark (no install, vs AI or human). |
| **BSD `cribbage`** (open-source, text)[^bsd1] | **ASCII text** board. | Opponent hand hidden; you're prompted for plays. | Text prompts; **self-count / muggins** is the classic model. | Text, self-counted. | N/A (terminal). Useful as the *minimal `numeric`/`count-only` extreme*. *(UI details not fully web-verified this pass.)* |

[^cp1]: Cribbage Corner Android review — board layout, avatar-anchored scores, difficulty. https://cribbagecorner.com/android-cribbage/
[^cp2]: Cribbage Pro — official site / App Store (No Board & Tournament board options, spectate, Game Summary). https://www.cribbagepro.net/ , https://apps.apple.com/us/app/cribbage-pro/id409644287
[^cwg1]: Android Central review — portrait one-handed, large cards/buttons, board frames screen. https://www.androidcentral.com/cribbage-grandpas-review
[^cwg2]: Cribbage With Grandpas — official site. https://cribbagewithgrandpas.com/
[^cc1]: Cribbage Classic — App Store / Microsoft Store (manual vs fast auto-count, hint system). https://apps.apple.com/us/app/cribbage-classic/id901900997
[^ms1]: Games from MSN — Cribbage (browser, count-yourself or auto). https://www.msn.com/en-us/play/games/cribbage/cg-9pjpfs62v92r
[^bga1]: Board Game Arena — Cribbage game help (score board, cumulative-count announcements). https://en.doc.boardgamearena.com/Gamehelpcribbage
[^cg1]: CardGames.io — Cribbage (custom board, jQuery animations, vs Bill AI or human). https://cardgames.io/cribbage/
[^bsd1]: BSD games `cribbage` (Linux). https://cribbagecorner.com/linux/ , https://www.linuxjournal.com/content/counting-cards-cribbage

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

### Resolved by the owner (2026-07-10, round 2)

- **Mobile-first or desktop-derived? → MOBILE-FIRST.** The efficient/minimal end
  of the master axis is canonical; it comes out of the U3 mobile-interface effort.
  Desktop/faithful becomes the enhanced preset.
- **Build the gameplay builder? → DEFER.** "Pie-in-the-sky, more cool-factor than
  need — like the board/card builder." Hand-pick presets; don't build the
  config-driven renderer yet.
- **Persistent running-count chip by default? → NO, keep spoken.** The count-in-
  the-call model mirrors an in-person game and is kept (`count.display=spoken-only`).
- **Is `turn.indicator` a fix-now bug? → NO.** Turn is inferred from the opponent's
  spoken call, by design (mimics the table). Stays a *future option* for
  beginner/mobile presets, not a bug.
- **Which dimensions are user-options vs. fixed? → DEFER.** Don't decide the
  option/preset split up front; let it fall out of exploring the designs.

**(Historical analysis of these questions is preserved below.)**

### Added 2026-07-10

**Decisions only the owner can make (product calls):**
- **Mobile-first or desktop-derived?** The competitor evidence leans mobile-first:
  the best-loved app (Grandpas) is portrait-one-handed by design, and Cribbage Pro
  ships a "No Board" mode *to reclaim phone space*. Is the owner willing to make the
  **Mobile Compact cluster the canonical layout** and treat desktop as the enhanced
  variant — or keep Desktop Classic as truth? This choice gates the entire U3 shape.
- **Which dimensions are user-options vs. fixed design decisions?** Over-optioning
  has real cost (settings UI, test matrix, support). Proposed default split: ship
  **a11y axes** (`colorblind`, `font.scale`, `motion`, `confirm-move`, message
  persistence) and **`hand.sort`** as user options; keep **layout** dimensions
  (`board.location`, `pegging.layout`, `info.location`) as *preset-level*, not
  free knobs. Owner sign-off needed.
- **Is the gameplay builder worth building?** The doc calls it the north star. My
  read: **defer it.** Hand-pick 2–3 presets (Desktop Classic, Mobile Compact,
  Beginner) and hard-code them first; only build the builder if empirical preset
  tuning proves it's needed. The config-driven-renderer refactor it requires is the
  expensive part and can be earned incrementally.
- **Is `turn.indicator` a bug or a dimension?** Today "it's your turn" is signaled
  only by cards becoming clickable (effectively `none`). Should a clear turn cue ship
  as a *fix now* (independent of U3), like the always-on dealer indicator?
- **How authentic vs. assisted is the default?** `count.display=spoken-only` and
  self-imposed counting are the "silent veteran" stance; competitors overwhelmingly
  offer **auto-count + a persistent count number**. Does the owner want a persistent
  running-count chip by default, or keep it embedded in the call text?

**Things research / a hands-on survey can resolve (not owner judgment):**
- The competitor table has several *(not web-verified)* cells (in-game show
  sequencing, opponent-hand handling for Grandpas/Classic/BGA). Resolve by
  **installing 2–3 apps and screenshotting** rather than more web search.
- Does any shipping app animate the **peg traveling the track** (motion dimension),
  and is it valued? Verify before investing in `motion=animated-peg`.
- What do apps show on **disconnect/resume** mid-hand? Directly informs
  `disconnect.resume`.

**Schema questions (from R1, resolve before the config exists):**
- Adopt a shared `Placement` type across `*.location` dimensions? Convert the
  boolean-shaped enums (`starter.label`, `pegging.round-sep`) to actual booleans?
- Split `board.style` into metaphor × shape × scale, and fold `opponent.hand-size`
  into a single `cards.scale`? (Prevents an enum-explosion as tiny/mini variants
  multiply.)
