import { test } from 'node:test';
import assert from 'node:assert/strict';
import { applyScore, createState, hasWon } from '../public/src/board/applyScore.js';

const config = { players: 2, winHole: 121, start: { front: 0, back: 0 } };

test('createState places every player at the start position', () => {
  const state = createState(config);
  assert.deepEqual(state.pegs, [
    { track: 0, front: 0, back: 0 },
    { track: 1, front: 0, back: 0 },
  ]);
});

test('scoring from the start moves the front peg, back stays at start', () => {
  const next = applyScore(createState(config), 0, 5, config);
  assert.deepEqual(next.pegs[0], { track: 0, front: 5, back: 0 });
});

test('normal leapfrog: rear peg jumps ahead of the front', () => {
  const start = { pegs: [{ track: 0, front: 5, back: 0 }] };
  const next = applyScore(start, 0, 8, { ...config, players: 1 });
  assert.deepEqual(next.pegs[0], { track: 0, front: 13, back: 5 });
});

test('gap between pegs equals the last score', () => {
  const next = applyScore(createState(config), 0, 7, config);
  const { front, back } = next.pegs[0];
  assert.equal(front - back, 7);
});

test('overshooting the win hole clamps the front peg', () => {
  const start = { pegs: [{ track: 0, front: 118, back: 110 }] };
  const next = applyScore(start, 0, 10, { ...config, players: 1 });
  assert.deepEqual(next.pegs[0], { track: 0, front: 121, back: 118 });
});

test('landing exactly on the win hole', () => {
  const start = { pegs: [{ track: 0, front: 116, back: 108 }] };
  const next = applyScore(start, 0, 5, { ...config, players: 1 });
  assert.equal(next.pegs[0].front, 121);
  assert.ok(hasWon(next, 0, config));
});

test('scoring again after winning stays clamped at the win hole', () => {
  const start = { pegs: [{ track: 0, front: 121, back: 116 }] };
  const next = applyScore(start, 0, 5, { ...config, players: 1 });
  assert.equal(next.pegs[0].front, 121); // clamped
  assert.equal(next.pegs[0].back, 121);  // back = old front (also 121) → gap 0
  assert.ok(hasWon(next, 0, config));
});

test('a scoreless turn (0 points) moves nothing', () => {
  const state = createState(config);
  assert.equal(applyScore(state, 0, 0, config), state);
});

test('scoring one player leaves the other untouched', () => {
  const state = createState(config);
  const next = applyScore(state, 0, 9, config);
  assert.deepEqual(next.pegs[1], state.pegs[1]);
});

test('the input state is never mutated', () => {
  const state = createState(config);
  const snapshot = structuredClone(state);
  applyScore(state, 0, 12, config);
  assert.deepEqual(state, snapshot);
});

test('negative points throw', () => {
  assert.throws(() => applyScore(createState(config), 0, -3, config), RangeError);
});

test('non-integer points throw', () => {
  assert.throws(() => applyScore(createState(config), 0, 2.5, config), TypeError);
});

test('an out-of-range player throws', () => {
  assert.throws(() => applyScore(createState(config), 5, 4, config), RangeError);
});
