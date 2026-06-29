import { test } from 'node:test';
import assert from 'node:assert/strict';
import { buildFrames, handStarts, verdictsByHand } from '../public/src/ui/replayFrames.js';

// A complete one-hand fixture (Alice deals; the bot is the pone). target:12 is
// deliberately small so the dealer's crib pushes her exactly to the win — which
// exercises the clamp and gives a concrete final score to assert. Card scores
// are illustrative totals (this tests the FOLD, not the scoring engine).
const fixture = {
  game_id: 'fx1',
  seats: [{ name: 'Alice', bot: false }, { name: 'champion', bot: true }],
  winner: 0,
  target: 12,
  events: [
    { seq: 1, type: 'cut_for_deal', dealer: 0 },
    { seq: 2, type: 'hand_dealt', dealer: 0, hands: [
      ['5H', '5C', 'JD', 'KC', '2H', '3H'],
      ['6D', '7D', '8C', '9C', 'AS', 'TS'],
    ] },
    { seq: 3, type: 'discarded', seat: 0, cards: ['2H', '3H'] },
    { seq: 4, type: 'discarded', seat: 1, cards: ['AS', 'TS'] },
    { seq: 5, type: 'starter_cut', card: '4S', points: 0 },
    { seq: 6, type: 'card_played', seat: 1, card: '6D', count: 6, points: 0 },
    { seq: 7, type: 'card_played', seat: 0, card: '5H', count: 11, points: 0 },
    { seq: 8, type: 'card_played', seat: 1, card: '7D', count: 18, points: 0 },
    { seq: 9, type: 'card_played', seat: 0, card: '5C', count: 23, points: 0 },
    { seq: 10, type: 'card_played', seat: 1, card: '8C', count: 31, points: 2 },
    { seq: 11, type: 'series_reset' },
    { seq: 12, type: 'card_played', seat: 0, card: 'JD', count: 10, points: 0 },
    { seq: 13, type: 'card_played', seat: 1, card: '9C', count: 19, points: 0 },
    { seq: 14, type: 'card_played', seat: 0, card: 'KC', count: 29, points: 0 },
    { seq: 15, type: 'go', seat: 0, points: 1 },
    { seq: 16, type: 'hand_shown', seat: 1, cards: ['6D', '7D', '8C', '9C'], total: 8,
      combos: [{ kind: 'run', length: 4, points: 4 }, { kind: 'fifteen', points: 2 }, { kind: 'fifteen', points: 2 }] },
    { seq: 17, type: 'hand_shown', seat: 0, cards: ['5H', '5C', 'JD', 'KC'], total: 6,
      combos: [{ kind: 'fifteen', points: 2 }, { kind: 'fifteen', points: 2 }, { kind: 'pair', points: 2 }] },
    { seq: 18, type: 'crib_shown', cards: ['2H', '3H', 'AS', 'TS'], total: 5,
      combos: [{ kind: 'run', length: 3, points: 3 }, { kind: 'fifteen', points: 2 }] },
    { seq: 19, type: 'game_won', seat: 0 },
  ],
};

const last = (a) => a[a.length - 1];
const find = (frames, pred) => frames.find(pred);

test('buildFrames: an initial empty frame precedes any event', () => {
  const frames = buildFrames(fixture);
  const f0 = frames[0];
  assert.equal(f0.phase, 'start');
  assert.deepEqual(f0.scores, [0, 0]);
  assert.equal(f0.hands[0].length, 0);
  assert.equal(f0.hands[1].length, 0);
  assert.equal(f0.label, 'Start');
});

test('buildFrames: after the deal both seats hold their full six', () => {
  const frames = buildFrames(fixture);
  const dealt = find(frames, (f) => f.phase === 'discard' && f.crib.length === 0);
  assert.ok(dealt, 'a post-deal frame exists');
  assert.equal(dealt.hand, 1);
  assert.equal(dealt.dealer, 0);
  assert.equal(dealt.hands[0].length, 6);
  assert.equal(dealt.hands[1].length, 6);
  assert.equal(dealt.label, 'Hand 1 — the deal');
});

test('buildFrames: discards fill the crib to four and shrink both hands to four', () => {
  const frames = buildFrames(fixture);
  // The frame right after the second discard, before the starter cut.
  const afterDiscards = find(frames, (f) => f.crib.length === 4 && f.starter === null);
  assert.ok(afterDiscards);
  assert.equal(afterDiscards.hands[0].length, 4);
  assert.equal(afterDiscards.hands[1].length, 4);
  // The discarded cards are no longer in hand and ARE in the crib (face-up).
  const cribIds = afterDiscards.crib.map((c) => `${c.rank}/${c.suit}`);
  assert.ok(cribIds.length === 4 && new Set(cribIds).size === 4);
});

test('buildFrames: the pegging count accumulates and resets on a series reset', () => {
  const frames = buildFrames(fixture);
  // Highest count reached before the reset is 31.
  const at31 = find(frames, (f) => f.count === 31);
  assert.ok(at31, 'a frame reaches 31');
  assert.equal(at31.pile.length, 5); // five cards laid this series (6,5,7,5,8)
  // After series_reset, the count and the current pile are cleared…
  const afterReset = frames[frames.indexOf(at31) + 1];
  assert.equal(afterReset.count, 0);
  assert.equal(afterReset.pile.length, 0);
  // …but each seat's laid cards stay on the table for the whole hand.
  assert.equal(afterReset.played[0].length, 2);
  assert.equal(afterReset.played[1].length, 3);
});

test('buildFrames: a hand_shown frame carries the scored breakdown', () => {
  const frames = buildFrames(fixture);
  const shown = find(frames, (f) => f.show && !f.show.isCrib && f.show.seat === 1);
  assert.ok(shown);
  assert.equal(shown.phase, 'show');
  assert.equal(shown.show.score.total, 8);
  assert.equal(shown.show.score.items.length, 3);
  assert.equal(shown.show.score.items[0].label, 'run of 4');
  assert.equal(shown.label, 'Hand 1 — the show');
});

test('buildFrames: a crib_shown frame is attributed to the dealer and tagged as crib', () => {
  const frames = buildFrames(fixture);
  const crib = find(frames, (f) => f.show && f.show.isCrib);
  assert.ok(crib);
  assert.equal(crib.show.seat, 0); // dealer
  assert.equal(crib.show.score.total, 5);
});

test('buildFrames: final scores accumulate, clamp at target, and match the winner', () => {
  const frames = buildFrames(fixture);
  const end = last(frames);
  assert.equal(end.phase, 'done');
  // Pegging: bot +2 (31), Alice +1 (go). Shows: bot +8, Alice +6, crib +5.
  // Alice 1+6+5 = 12 → clamped at target 12 (the win); bot 2+8 = 10.
  assert.deepEqual(end.scores, [12, 10]);
  assert.equal(end.scores[fixture.winner], fixture.target);
});

test('buildFrames: heels (a cut jack) pegs the dealer two', () => {
  const frames = buildFrames({
    target: 121,
    events: [
      { type: 'hand_dealt', dealer: 1, hands: [['2H', '3H', '4H', '5H', '6H', '7H'], ['2C', '3C', '4C', '5C', '6C', '7C']] },
      { type: 'starter_cut', card: 'JD', points: 2 },
    ],
  });
  assert.deepEqual(last(frames).scores, [0, 2]); // dealer is seat 1
  assert.ok(last(frames).starter);
});

test('handStarts: lists the first frame index of each hand', () => {
  const frames = buildFrames({
    target: 121,
    events: [
      { type: 'hand_dealt', dealer: 0, hands: [['2H', '3H', '4H', '5H', '6H', '7H'], ['2C', '3C', '4C', '5C', '6C', '7C']] },
      { type: 'card_played', seat: 1, card: '2C', count: 2, points: 0 },
      { type: 'hand_dealt', dealer: 1, hands: [['8H', '9H', 'TH', 'JH', 'QH', 'KH'], ['8C', '9C', 'TC', 'JC', 'QC', 'KC']] },
    ],
  });
  const hs = handStarts(frames);
  assert.equal(hs.length, 2);
  assert.equal(hs[0].hand, 1);
  assert.equal(hs[1].hand, 2);
  assert.equal(frames[hs[1].index].hand, 2);
});

test('buildFrames: unrecognized events produce no frame', () => {
  const frames = buildFrames({ target: 121, events: [{ type: 'players', players: [] }, { type: 'noise' }] });
  assert.equal(frames.length, 1); // only the initial frame
});

test('buildFrames: empty / missing input yields just the initial frame', () => {
  assert.equal(buildFrames().length, 1);
  assert.equal(buildFrames({}).length, 1);
  assert.equal(buildFrames({ events: [] }).length, 1);
});

// ---- A4: verdictsByHand (the discard-verdict ↔ hand association) ----

// Two-hand replay so handStarts() yields two hands in order.
const twoHandFrames = buildFrames({
  target: 121,
  events: [
    { type: 'hand_dealt', dealer: 0, hands: [['2H', '3H', '4H', '5H', '6H', '7H'], ['2C', '3C', '4C', '5C', '6C', '7C']] },
    { type: 'card_played', seat: 1, card: '2C', count: 2, points: 0 },
    { type: 'hand_dealt', dealer: 1, hands: [['8H', '9H', 'TH', 'JH', 'QH', 'KH'], ['8C', '9C', 'TC', 'JC', 'QC', 'KC']] },
  ],
});

test('verdictsByHand: aligns discards[i] with the i-th hand by number', () => {
  const starts = handStarts(twoHandFrames); // hands 1 and 2
  const analysis = { seat: 0, discards: [{ optimal: true, hand: ['A'] }, { optimal: false, hand: ['B'] }] };
  const v = verdictsByHand(analysis, starts);
  assert.equal(v[1].optimal, true);
  assert.equal(v[2].optimal, false);
  assert.equal(v[1].hand[0], 'A');
});

test('verdictsByHand: extra hand-starts beyond discards are simply unmapped', () => {
  const starts = handStarts(twoHandFrames);
  const analysis = { seat: 1, discards: [{ optimal: true }] }; // only one verdict
  const v = verdictsByHand(analysis, starts);
  assert.ok(v[1]);
  assert.equal(v[2], undefined);
});

test('verdictsByHand: missing / empty analysis yields no verdicts', () => {
  const starts = handStarts(twoHandFrames);
  assert.deepEqual(verdictsByHand(null, starts), {});
  assert.deepEqual(verdictsByHand({}, starts), {});
  assert.deepEqual(verdictsByHand({ seat: 0, discards: [] }, starts), {});
  assert.deepEqual(verdictsByHand({ seat: 0, discards: [{}] }, []), {});
});
