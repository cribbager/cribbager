# Play flow: tap to advance

The single spec for **what waits for the player** during a hand, from the deal
to the next deal. Both clients implement it: the native iOS app
([cribbager-app](https://github.com/cribbager/cribbager-app), SwiftUI) and the
mobile-first web prototype in this repo. It mirrors the iOS app's table, labels
and button conventions; it does not rework the existing desktop client in
`web/public/src/ui/`.

## The rules

1. **Nothing moves the game forward on a timer.** The game advances only when
   the player plays a card, discards, or taps the primary button.
2. **The bot never makes you wait.** Its discard and its plays resolve the
   moment it is the bot's turn. They are shown with a short animation (about
   0.3s or less), and the animation never holds anything up: the state has
   already advanced. Any tap during an animation finishes every queued animation
   at once and is then handled as normal.
3. **One action at a time.** At every waiting point exactly one thing moves the
   game on: a card in your hand, or the primary button. Never both.
4. **This lives in the UI/session layer, not the engines.** The Go and Swift
   engines still resolve go, 31, last card, the show and (in Go) the next deal
   automatically. The client holds engine events back and reveals them one tap
   at a time. The shared contract files (`testdata/vectors`, bot weights,
   `parity_*.json`) do not change.

Two consequences follow. The **game-speed setting** goes away, because there is
nothing left for it to scale. **Skip-pause** goes too, because there are no
pauses left to skip.

## Waiting points in one hand

"Button" means the primary button in the action slot (see *Button placement*).
"—" means no button: either the cards are the action or nobody waits.

| # | Waiting point | On screen | Button | Who acts | The tap does |
|---|---|---|---|---|---|
| 0 | Cut for the deal (first hand of a game only) | The pack, then both cut cards and the verdict | **Start game** (shown once the reveal is done) | You | Tap the pack to cut. The staged reveal is animation only, and a second tap finishes it. *Start game* deals the first hand. |
| 1 | Discarding | Your six cards. The bot's two are already in the crib, because it threw at the deal. | **Discard** (appears with your second choice) | You | Sends your two cards to the crib. The starter is cut in the same step, then pegging starts with no tap in between. |
| 2 | Your turn, with a card that fits | Your hand, with unplayable cards dimmed | — | You | Tapping a legal card plays it. |
| 3 | Your turn, with no card that fits (you still hold cards) | Your hand, all dimmed | **Go** | You | You say "Go", and the bot carries on at once. |
| 4 | The bot's turn | The bot's card animates in | — | Bot | Nobody waits. |
| 5 | A count has closed and the bot leads the next one | The closed count stays up: cards, "31" or the final count, and the last calls | **Next** | You | Clears the calls and resets the count to 0, and the bot leads at once. Cards already played stay on the table. |
| 6 | A count has closed and you lead the next one | The same as 5, with your hand live | — | You | Playing your lead card is the acknowledgement. The count resets as your card lands. |
| 7 | Pegging is over (the last card of the hand has been played) | The final count and the last calls | **Count** | You | Opens the show and turns over the non-dealer's hand. Its pegs move. |
| 8 | Show: first count is out | The non-dealer's hand and its total | **Next hand** | You | Turns over the dealer's hand. |
| 9 | Show: second count is out | Both hands | **Next hand** | You | Turns over the crib. |
| 10 | Show: every count is out | Both hands and the crib | **Deal** | You | Clears the show and deals the next hand. The bot discards immediately, which takes you back to row 1. |
| — | Someone reaches 121 (at any point) | The winning call or count, and the pegs at 121 | **Continue** | You | Opens the game-over card. |

In the show, tapping a revealed count opens its breakdown, as it does today.
The breakdown is a view, not a step: it does not change the button. On a
121 (the last row), no later step is revealed. The winning count is always the
last thing on the table before *Continue*.

## Pegging: every edge case

Two terms are used below. A **count closes** when it reaches 31, or when neither
player can play (the go or last-card point is awarded). **Who leads next** is the
player who did *not* play the last card, unless that player has no cards left;
both engines already apply this rule.

| Case | Resolution |
|---|---|
| The bot says go and you can still play | You don't tap. It is still your turn, and the bot's "Go" stays beside its cards. Keep playing. |
| The bot says go, then you can't play either | The count closes with your last-card point. The bot leads next, so you get **Next**. If the bot has no cards left, you lead and there is no Next. |
| You tap **Go** | The bot plays out at once (animated, with no taps between its cards) until the count closes. You always lead the next count, because you said go holding cards. That means no Next: your lead card is the acknowledgement. **A Go tap is never followed by a Next tap.** The only exception is the game ending (see below). |
| You tap Go and the bot can't play either | The bot takes 1 for go immediately. You lead, so there is no Next. |
| You have no cards left and the bot has several | There is no Go button, because go is only said while holding cards. The Go engine emits `pass` for an empty hand, and the client ignores it. The bot plays its cards out one animation after another, with no taps. If one of its cards makes 31 while it still holds more, the count closes and the bot leads again, so you get **Next**. |
| You play your last card and the bot still has cards | No tap. The bot continues as in the row above. If your card closed the count, the bot leads next: **Next**. |
| The last card of the hand is played, by either player (31 or last card) | The count closes and pegging is over. The button is **Count**, not Next: one tap both acknowledges the final count and opens the show. There is never a Next followed by a Count. |
| The bot leads after a reset | That is the row 5 case. *Next* resets the count and the bot's lead card animates in. You can always answer a lead, because no card can take a count from 10 or less past 31. So no Go follows immediately. |
| A go immediately after a 31 | This can't happen. After 31 the count is 0 and every card fits. A go needs a count of at least 22, so it needs at least three cards into the new count. Both engines reset without a go. The reverse order (you say go, then the bot makes 31) is the Go case above: the bot takes 2, you lead, and there is no Next. |
| The first count of the hand | The non-dealer leads straight after the discard. If that's the bot, its card animates in with no tap. |
| His heels at the cut | Shown as the dealer's call. No tap, unless it wins the game. |
| The game ends mid-pegging | This covers a winning play, a winning go or last-card point, and a winning heels. The call that won stays on the table, the pegs reach 121, and the button is **Continue**. Nothing covers the winning moment until it is tapped, which is the same rule as for a game won in the show. Any later engine events (Go: `show_uncounted`) are revealed behind the game-over card, not before it. |

**Calls (speech) have no dwell timers.** A player's call stays until something
newer replaces it:

- When a card is played, the other player's call clears.
- A "Go" stays until its count resets.
- Every call clears when the count resets or the show opens.

The round log keeps everything said, so a call that is replaced is not lost.

## Button placement

These conventions come from the iOS app (`GameView.actionSlot`,
`docs/table-layout-spec.md` § Buttons):

- **One action slot, directly below your hand, in thumb reach.** Every
  advancing button the game offers (*Go, Next, Count, Next hand, Deal,
  Continue*) appears there and nowhere else. It is one place to look and the
  same place to press, every time.
- **The slot always keeps its height,** even when it is empty, so nothing
  moves when a button comes or goes.
- **Every button is drawn the same way:** the same height, fill and tint, and
  the same minimum width. The minimum is set on the label, so *Go* and
  *Next hand* are the same object.
- **One exception, inherited from iOS:** in the *discard to the play area*
  variant, **Discard** sits on the right-hand column of the staging row, next
  to the cards it sends. In the *lift in hand* variant it is in the action
  slot.
- **Web:** Space and Enter press the primary button, as `waitContinue` does
  today. On a phone the action slot sits at the bottom, under the hand.
- **Tapping empty table space** does the same as the button **only** for the
  pure acknowledgements (*Next, Count, Next hand*). This extends what the iOS
  show already does. It never does Go, Deal, Discard or Continue, because each
  of those is a call or a commitment.
- **Accessibility:**
  - *Go* has the hint "No card you hold fits the count".
  - When a hold appears, it is announced with the closing call ("31 for 2.
    Next.").

## What gets removed (notes for implementers)

### iOS (`cribbager-app`)

Line numbers are as of `Cribbager/GameSession.swift` at the time of writing.

- **Speed setting:**
  - `GameSpeed` and `AppSettings.gameSpeed`/`gameSpeedKey`
    (`Settings.swift:3-25, 70-80`)
  - The Game speed picker and its footer (`SettingsView.swift:4, 12-20`)
- **The scheduler:**
  - `runAfter` (`:746-754`)
  - `pendingTask` (`:167`)
  - `skipPause()` (`:525-527`)
  - `pause()` (`:532-534`)
  - `GameView.tapCatcher`'s skip branch (`GameView.swift:212-222`). Keep its
    show-paging branch, widened to Next and Count, as described under
    *Button placement*.
- **Bot timing:**
  - `scheduleBotDiscard`'s random 1.1–2.4s think (`:429-437`) becomes an
    immediate discard at the deal.
  - `advance()`'s 0.25/0.9s bot play (`:722-732`) becomes immediate.
- **Show and outcome beats:**
  - `advance()`'s 1.8s pre-show pause (`:711-721`) is replaced by the
    **Count** hold.
  - `scheduleOutcome`'s 1.5s (`:784-786`) is replaced by the **Continue** hold.
- **Toast dwell:** remove `ScoreToast.dwell`/`isBig`-driven dwell (`:26-35`)
  and the sleep tasks in `present()` (`:817-835`). Calls persist by the speech
  rule above.
- **Autopilot timing:**
  - Remove `autoAdvanceShow`'s sleeps (`:686-697`) and the autopilot's 0.6s
    play (`:733-737`).
  - Autopilot should just press the primary action synchronously.
  - The snapshot harness (`fastForward`) already drives with no pauses.
- **Cosmetic beats:**
  - The cut's staged reveal (`CutView.swift:109-113`) stays as animation, but
    no longer goes through the session's scheduler. A tap finishes it.
  - The breakdown's 0.32s combo cadence (`GameView.swift:1188`) stays, without
    the speed multiplier.
- **New in `GameSession`:**
  - Add a `hold` (`go / next / count / continue`, or nil). The action slot
    renders from it.
  - **Go:** stop draining events at a `.go(human)` and don't run the bot until
    Go is tapped. The engine emits the human's go in the same `playCard` as the
    bot's card, so the session must hold its event cursor there.
  - **Next / Count:** set the hold when a `.lastCard` or a 31 closes the count
    and the bot leads, or when the phase becomes `.show`.
  - `displayPile` already keeps the closed count on screen until the next card
    is played.
- **Persistence:** save the hold in `SavedGame`. Today `init(restoring:)` asks
  "whoever was to act" again. With holds, a restore after the human's go would
  otherwise run the bot straight past the Go tap.

### Web (existing desktop client, for reference only)

The new prototype must not copy the existing `web/public/src/ui/main.js` delay
chain:

- `delay()` (`:42`)
- starter 850/350ms (`:500`)
- pass 650ms (`:506`)
- play 750/500ms (`:519`)
- go 750ms (`:527`)
- reset 250ms (`:531`)

Its two existing click gates map onto this spec:

- `playDone` → `waitContinue` (`:539`) becomes **Count**.
- `handComplete` → `waitContinue` (`:569`) becomes **Deal**.

The all-at-once show reveal (`:542-564`) becomes three steps, one per tap.

## Web prototype notes

**The server already supports this.** In a bot game:

- `POST /games/{id}/actions` runs the bot synchronously (`session.driveBots`).
  It returns every resulting delta in one response: your move, the bot's
  replies, go and reset events, the show, and even the next deal and the bot's
  next discard. There are no server-side delays.
- So the client already holds a batch locally and decides when to reveal each
  delta.

**The reveal loop.**

- Push the response deltas onto a local queue.
- Reveal them in order, each with its short animation.
- Stop at a hold, show its button, and continue on the tap.
- Only prompt for your next move once the queue has drained to a human
  decision. Never drive the hand from a snapshot's `ToPlay` or `LegalPlays`
  while deltas are still unrevealed.

**Where to hold:**

| Delta | Hold |
|---|---|
| `pass` for your seat, with cards still in your revealed hand | **Go**, held *before* revealing it. Ignore a `pass` for an empty hand. |
| `series_reset` | **Next** if the next queued delta is the opponent's `card_played`. If the queue ends there, you lead, so there is no hold. |
| The first `hand_shown` of a hand | **Count**, held before it. The Go engine emits no `series_reset` at the end of a hand: it goes straight from the last `go` (last card) or the final 31 to `hand_shown`. |
| The second `hand_shown`, and `crib_shown` | **Next hand**, held before each |
| A `hand_dealt` that follows a show | **Deal**, held before it. The bot's `discarded` delta reveals straight after the deal, with no hold. |
| `game_won` | **Continue**, held before the game-over card. Reveal `show_uncounted` behind it. |
| `starter_cut`, `discarded`, the opponent's `card_played`, the `go` point | None. Animate and carry on. |

**Gaps (none block a vs-bot prototype):**

1. **`series_reset` drops the leader.** The engine's `SeriesReset{Leader}` is
   projected without a seat (`wire.go`, `projectEvent`). Vs the bot, the leader
   can be read off the batch as above. In a human-vs-human game the opponent's
   lead arrives later on the stream, so the client cannot tell Next from
   your-lead.
   - Fix: an additive `seat` field on the delta. This is a wire change only,
     not the shared contract.
2. **Resume shows current state, not the reveal point.**
   - `GET /games/{id}` is the server's *current* state. A reload mid-show
     would land on the already-dealt next hand.
   - The stream can replay a seat's whole log (`/stream?since=0`), so the
     client can persist its revealed seq and rebuild by folding deltas from 0.
     But that needs SSE for a one-shot read.
   - Fix: a plain `GET /games/{id}/deltas?since=N`. For a first prototype,
     jumping to current state on reload is acceptable.
3. **`pass` is emitted for an empty hand** (Go engine). The Swift engine emits
   go only for a player holding cards. Clients must check the hand before
   offering Go. This is a client rule, not an engine change.

## Open questions

Each of these was decided provisionally. Reopen if they're wrong in play.

- **Speech persistence.** A play clears the other player's call, and "Go"
  persists until the reset. This replaces iOS's independent per-player dwell.
- **Table tap as a shortcut** for Next, Count and Next hand. This extends the
  iOS show behaviour; dropping it to button-only is equally defensible.
- **Bot runs with no taps.** When you are out of cards, the bot's consecutive
  plays reveal without holds, so its intermediate calls ("15 for 2") show only
  in passing and in the round log.
- **Multiplayer is out of scope.** Holds are local to each client, so your
  Next never makes the opponent wait. Gap 1 must be fixed before human-vs-human
  play uses this flow.
