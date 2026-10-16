import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createFlow, HOLD_LABEL, TABLE_TAP_HOLDS } from '../public/m/flow.js';
import { applyDelta, freshModel } from '../public/m/model.js';

// Delta builders (seat 0 is you, seat 1 the bot).
const play = (seat, card, count, points = 0) => ({ type: 'card_played', seat, card, count, points });
const pass = (seat) => ({ type: 'pass', seat });
const go = (seat, points = 1) => ({ type: 'go', seat, points });
const reset = () => ({ type: 'series_reset' });
const dealt = (n = 6) => ({ type: 'hand_dealt', dealer: 0, hand: ['AC', '2D', '3H', '4S', '5C', '6D'].slice(0, n), opponentCards: 6 });
const shown = (seat) => ({ type: 'hand_shown', seat, cards: ['AC', '2D', '3H', '4S'], total: 4, combos: [] });
const crib = () => ({ type: 'crib_shown', cards: ['AC', '2D', '3H', '4S'], total: 4, combos: [] });

/** Drain a flow, tapping every hold; returns the sequence of events as strings. */
function run(flow) {
  const out = [];
  for (let i = 0; i < 200; i++) {
    const s = flow.peek();
    if (s.type === 'idle') break;
    if (s.type === 'hold') {
      out.push('hold:' + s.hold);
      flow.release();
    } else {
      out.push(s.delta.type);
      flow.take();
    }
  }
  return out;
}

/** A flow that has revealed a dealt hand and the discard, so `hand` cards are held. */
function inPegging(deltas, { hand = 4 } = {}) {
  const f = createFlow({ mySeat: 0 });
  f.push([dealt(6), { type: 'discarded', seat: 0, cards: ['AC', '2D'] }]);
  run(f);
  assert.equal(f.state.hand, hand);
  f.push(deltas);
  return f;
}

test('every hold has a label, and only pure acknowledgements answer a table tap', () => {
  for (const h of ['start', 'go', 'next', 'count', 'nextHand', 'deal', 'continue']) assert.ok(HOLD_LABEL[h], h);
  assert.deepEqual([...TABLE_TAP_HOLDS].sort(), ['count', 'next', 'nextHand']);
});

test('plain plays, the bot reply and the starter never hold', () => {
  const f = inPegging([{ type: 'starter_cut', card: '7S', points: 0 }, play(1, '5H', 5), play(0, '10C', 15, 2), play(1, '9D', 24)]);
  assert.deepEqual(run(f), ['starter_cut', 'card_played', 'card_played', 'card_played']);
});

test('your pass with cards in hand holds Go before it is revealed', () => {
  const f = inPegging([play(1, 'KH', 28), pass(0), play(1, 'JD', 28)]);
  assert.equal(f.peek().type, 'reveal'); // the bot's card first
  f.take();
  const s = f.peek();
  assert.deepEqual([s.type, s.hold], ['hold', 'go']);
  assert.equal(s.delta.type, 'pass'); // held BEFORE the pass shows
  // still held until released, however often it is asked
  assert.equal(f.peek().hold, 'go');
  f.release();
  assert.equal(f.peek().type, 'reveal');
  assert.deepEqual(run(f), ['pass', 'card_played']);
});

test('a pass for an empty hand is ignored, never a Go hold', () => {
  const f = createFlow({ mySeat: 0 });
  f.push([dealt(1), { type: 'discarded', seat: 0, cards: ['AC'] }]);
  run(f);
  assert.equal(f.state.hand, 0);
  f.push([play(1, '5H', 5), pass(0), play(1, '4H', 9)]);
  assert.deepEqual(run(f), ['card_played', 'pass', 'card_played']);
});

test('the bot passing never holds, and a go after the bot said go is not tapped', () => {
  const f = inPegging([play(0, '9C', 30), pass(1), pass(0), go(0), reset(), play(1, '4H', 4)]);
  // the bot said go first, so your own pass is not a Go tap; the count just closes
  assert.deepEqual(run(f), ['card_played', 'pass', 'pass', 'go', 'hold:next', 'series_reset', 'card_played']);
});

test('series_reset holds Next only when the bot leads the next count', () => {
  const bot = inPegging([play(0, '9C', 31, 2), reset(), play(1, '4H', 4)]);
  assert.deepEqual(run(bot), ['card_played', 'hold:next', 'series_reset', 'card_played']);
  const me = inPegging([play(1, '9C', 31, 2), reset()]); // queue ends: you lead
  assert.deepEqual(run(me), ['card_played', 'series_reset']);
});

test('a Go tap is never followed by a Next tap', () => {
  // you go; the bot plays out and the count closes; you lead the next count
  const f = inPegging([play(1, 'KH', 28), pass(0), play(1, 'JD', 28), go(1, 1), reset()]);
  assert.deepEqual(run(f), ['card_played', 'hold:go', 'pass', 'card_played', 'go', 'series_reset']);
});

test('the first count of a hand holds Count; later counts hold Next hand; the deal holds Deal', () => {
  const f = inPegging([play(1, '9C', 31, 2), shown(1), shown(0), crib(), dealt(6)]);
  assert.deepEqual(run(f), ['card_played', 'hold:count', 'hand_shown', 'hold:nextHand', 'hand_shown', 'hold:nextHand', 'crib_shown', 'hold:deal', 'hand_dealt']);
});

test('a bot discard after the deal reveals straight away', () => {
  const f = createFlow({ mySeat: 0 });
  f.push([shown(1), shown(0), crib()]);
  f.push([dealt(6), { type: 'discarded', seat: 1 }]);
  assert.deepEqual(run(f), ['hold:count', 'hand_shown', 'hold:nextHand', 'hand_shown', 'hold:nextHand', 'crib_shown', 'hold:deal', 'hand_dealt', 'discarded']);
});

test('the cut: tap the pack, then Start game before the first deal', () => {
  const f = createFlow({ mySeat: 0 });
  f.push([{ type: 'cut_for_deal', cut: ['2C', '9S'], dealer: 0 }, dealt(6), { type: 'discarded', seat: 1 }]);
  assert.deepEqual(run(f), ['hold:cut', 'cut_for_deal', 'hold:start', 'hand_dealt', 'discarded']);
});

test('a resumed game has no cut and no Start hold', () => {
  const f = createFlow({ mySeat: 0 });
  f.resume(4);
  f.push([play(1, '5H', 5)]);
  assert.deepEqual(run(f), ['card_played']);
});

test('game_won holds Continue, and uncounted reveals come after it', () => {
  const f = inPegging([play(1, '9C', 31, 2), { type: 'show_uncounted', seat: 0, cards: ['AC'] }, { type: 'game_won', seat: 1 }]);
  assert.deepEqual(run(f), ['card_played', 'hold:continue', 'game_won', 'show_uncounted']);
});

test('the winning count stays the last thing on the table before Continue', () => {
  const f = inPegging([shown(1), { type: 'game_won', seat: 1 }]);
  assert.deepEqual(run(f), ['hold:count', 'hand_shown', 'hold:continue', 'game_won']);
});

test('batches pushed one after another keep their hold state', () => {
  const f = inPegging([play(1, 'KH', 28)]);
  assert.deepEqual(run(f), ['card_played']);
  f.push([pass(0)]);
  assert.deepEqual(run(f), ['hold:go', 'pass']);
});

// ---- the model: what the table shows as deltas are revealed ----
test('calls: a play clears the other call, but Go stays until the count resets', () => {
  const m = freshModel(0);
  applyDelta(m, dealt(6));
  applyDelta(m, play(1, 'KH', 28));
  assert.equal(m.speech[1], '28');
  applyDelta(m, pass(1));
  assert.equal(m.speech[1], 'GO');
  applyDelta(m, play(0, '2D', 30, 0));
  assert.equal(m.speech[1], 'GO'); // still there beside the bot's cards
  assert.equal(m.speech[0], '30');
  applyDelta(m, go(0, 1));
  assert.equal(m.speech[0], 'GO FOR 1');
  applyDelta(m, reset(), play(1, '4H', 4)); // bot leads: Next clears now
  assert.deepEqual(m.speech, ['', '']);
  assert.equal(m.count, 0);
});

test('when you lead after a reset the closed count stays until your card lands', () => {
  const m = freshModel(0);
  applyDelta(m, dealt(6));
  applyDelta(m, play(1, 'KH', 31, 2));
  applyDelta(m, reset(), undefined);
  assert.equal(m.speech[1], '31 FOR 2');
  assert.equal(m.closed, true);
  applyDelta(m, play(0, '2D', 2));
  assert.deepEqual(m.speech, ['2', '']);
  assert.equal(m.count, 2);
});

test('pegs move only as each count is revealed, capped at 121', () => {
  const m = freshModel(0);
  applyDelta(m, dealt(6));
  assert.deepEqual(m.scores, [0, 0]);
  applyDelta(m, shown(1));
  assert.deepEqual(m.scores, [0, 4]);
  m.scores[0] = 120;
  applyDelta(m, shown(0));
  assert.deepEqual(m.scores, [121, 4]);
  assert.equal(m.prev[0], 120);
});
