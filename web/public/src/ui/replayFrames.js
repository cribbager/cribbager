// Pure reducer for the stored-game replay board (A2, now the board half of the
// unified evaluate page). It folds a finished game's event log (GET
// /games/{id}/replay) into an array of immutable "spectator" frames — one per
// step — that the page steps through with FULL visibility (both hands, the
// crib, the opponent's discards).
//
// The event vocabulary is the SAME type/field set the live SSE stream uses, so
// this mirrors main.js's translate()/onEvent() (the reference reducer) — but
// WITHOUT animation/awaits, and with the two replay-only extras the contract
// guarantees: hand_dealt carries BOTH seats' full `hands`, and `discarded`
// carries the actual `cards`. Cards are the two-char wire form ("5H", "TC").
//
// No DOM, no timers — pure and unit-testable.
import { parseCard } from '../engine/cards.js';

// comboLabel mirrors main.js: turn a scoring combo into its spoken label.
function comboLabel(c) {
  switch (c.kind) {
    case 'fifteen': return 'fifteen';
    case 'pair': return c.points === 6 ? 'pair royal' : c.points === 12 ? 'double pair royal' : 'pair';
    case 'run': return `run of ${c.length}`;
    case 'flush': return 'flush';
    case 'nobs': return 'nobs';
    default: return c.kind;
  }
}

// showScore normalizes a hand_shown/crib_shown event into the same { total, items }
// shape main.js's showScore produces (so the page can reuse the .show-* markup).
function showScore(ev) {
  return {
    total: ev.total ?? 0,
    items: (ev.combos ?? []).map((c) => ({ label: comboLabel(c), points: c.points })),
  };
}

// removeCards returns a copy of `hand` with the first match of each card removed.
function removeCards(hand, cards) {
  const out = hand.slice();
  for (const c of cards) {
    const i = out.findIndex((x) => x.rank === c.rank && x.suit === c.suit);
    if (i >= 0) out.splice(i, 1);
  }
  return out;
}

// labelFor builds the human-readable step label, e.g. "Deal 2 — pegging".
// "Deal" (not "Hand") matches the evaluate page's rail and the analysis
// payload's vocabulary.
function labelFor(s) {
  switch (s.phase) {
    case 'start': return 'Start';
    case 'cut': return 'Cut for deal';
    case 'discard': return `Deal ${s.hand} — the discard`;
    case 'play': return `Deal ${s.hand} — pegging`;
    case 'show': return `Deal ${s.hand} — the show`;
    case 'done': return 'Game over';
    default: return `Deal ${s.hand}`;
  }
}

// snapshot freezes a deep-enough copy of the working state into a Frame. Card
// objects are never mutated, so they may be shared by reference.
function snapshot(s) {
  return Object.freeze({
    dealer: s.dealer,
    hand: s.hand,
    scores: [s.scores[0], s.scores[1]],
    hands: [s.hands[0].slice(), s.hands[1].slice()],
    crib: s.crib.slice(),
    starter: s.starter,
    played: [s.played[0].slice(), s.played[1].slice()],
    pile: s.pile.slice(),
    count: s.count,
    phase: s.phase,
    show: s.show
      ? Object.freeze({ ...s.show, hand: s.show.hand.slice(), score: s.show.score })
      : null,
    label: labelFor(s),
  });
}

/**
 * Fold a replay payload into an array of immutable spectator frames.
 *
 * @param {{ target?: number, events?: object[] }} replay
 * @returns {ReadonlyArray<Frame>} frames[0] is the empty pre-game state; each
 *   subsequent frame is the state AFTER applying one (recognized) event.
 */
export function buildFrames(replay = {}) {
  const target = replay.target ?? 121;
  const s = {
    dealer: null,
    hand: 0,
    scores: [0, 0],
    hands: [[], []],
    crib: [],
    starter: null,
    played: [[], []],
    pile: [],
    count: 0,
    phase: 'start',
    show: null,
  };
  // award mirrors main.js's awardPoints: accumulate and clamp at the target — a
  // game ends the instant a peg reaches it, so a displayed score never exceeds it.
  const award = (seat, pts) => {
    if (pts > 0 && seat != null) s.scores[seat] = Math.min(target, s.scores[seat] + pts);
  };

  const frames = [snapshot(s)];

  for (const ev of replay.events ?? []) {
    switch (ev.type) {
      case 'cut_for_deal':
        s.dealer = ev.dealer;
        s.phase = 'cut';
        break;

      case 'hand_dealt':
        // A new deal: both seats' six are dealt, and everything hand-scoped resets.
        s.dealer = ev.dealer;
        s.hand += 1;
        s.hands = [(ev.hands?.[0] ?? []).map(parseCard), (ev.hands?.[1] ?? []).map(parseCard)];
        s.crib = [];
        s.starter = null;
        s.played = [[], []];
        s.pile = [];
        s.count = 0;
        s.show = null;
        s.phase = 'discard';
        break;

      case 'discarded': {
        // Replay carries the actual discarded cards: pull them from the seat's
        // hand and add them (face-up, for the spectator) to the crib.
        const cards = (ev.cards ?? []).map(parseCard);
        s.hands[ev.seat] = removeCards(s.hands[ev.seat], cards);
        s.crib = s.crib.concat(cards);
        s.phase = 'discard';
        break;
      }

      case 'starter_cut':
        s.starter = parseCard(ev.card);
        if (ev.points > 0) award(s.dealer, ev.points); // "his heels" — 2 for a cut jack
        s.phase = 'play';
        break;

      case 'card_played': {
        const c = parseCard(ev.card);
        s.hands[ev.seat] = removeCards(s.hands[ev.seat], [c]);
        s.played[ev.seat] = s.played[ev.seat].concat([c]);
        s.pile = s.pile.concat([c]);
        s.count = ev.count;
        award(ev.seat, ev.points);
        s.phase = 'play';
        break;
      }

      case 'pass':
        // "go" — no points here; the peg for go/last-card arrives as a `go` event.
        s.phase = 'play';
        break;

      case 'go':
        award(ev.seat, ev.points);
        s.phase = 'play';
        break;

      case 'series_reset':
        // 31 or a double-go ends the pegging series: the running count and the
        // current pile reset, but each seat's laid cards stay on the table.
        s.count = 0;
        s.pile = [];
        s.phase = 'play';
        break;

      case 'hand_shown':
        s.show = { seat: ev.seat, isCrib: false, hand: (ev.cards ?? []).map(parseCard), starter: s.starter, score: showScore(ev) };
        award(ev.seat, ev.total ?? 0);
        s.phase = 'show';
        break;

      case 'crib_shown':
        s.show = { seat: s.dealer, isCrib: true, hand: (ev.cards ?? []).map(parseCard), starter: s.starter, score: showScore(ev) };
        award(s.dealer, ev.total ?? 0);
        s.phase = 'show';
        break;

      case 'game_won':
        s.phase = 'done';
        break;

      default:
        // Unknown/ignored event type (e.g. a roster delta): produce no frame.
        continue;
    }
    frames.push(snapshot(s));
  }

  return frames;
}

/**
 * Build a jump-to-hand index: for each distinct hand number (1-based), the index
 * of the FIRST frame belonging to that hand. Returns an array of { hand, index }
 * in hand order, suitable for populating a <select>.
 */
export function handStarts(frames) {
  const out = [];
  let seen = -1;
  frames.forEach((f, i) => {
    if (f.hand > 0 && f.hand !== seen) { out.push({ hand: f.hand, index: i }); seen = f.hand; }
  });
  return out;
}

/**
 * @typedef {Object} Frame
 * @property {?number} dealer        seat index of the dealer (or null pre-deal)
 * @property {number}  hand          1-based hand number (0 before the first deal)
 * @property {number[]} scores       [seat0, seat1] pegged scores, clamped at target
 * @property {object[][]} hands      each seat's remaining hand (face-up cards)
 * @property {object[]} crib         the crib's cards so far (face-up for replay)
 * @property {?object} starter       the cut starter card (or null)
 * @property {object[][]} played     each seat's cards laid this hand, in play order
 * @property {object[]} pile         the current pegging series' cards (resets on series_reset)
 * @property {number}  count         the running pegging count
 * @property {string}  phase         'start'|'cut'|'discard'|'play'|'show'|'done'
 * @property {?object} show          when counting: { seat, isCrib, hand, starter, score:{total,items} }
 * @property {string}  label         e.g. "Hand 2 — pegging"
 */
