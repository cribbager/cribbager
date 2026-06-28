import { test } from 'node:test';
import assert from 'node:assert/strict';
import { resolveLayout } from '../public/src/board/geometry.js';
import { pegCenters, buildScorePath, isLegalScore } from '../public/src/board/board.js';
import { straightBoard, classicBoard } from '../public/src/board/theme.js';

// --- pegCenters: which hole each peg renders in --------------------------------
// Use the real layouts so we test the actual geometry, not a synthetic stand-in.
const classic = resolveLayout(classicBoard.geometry, 2, classicBoard.winHole);
const straight = resolveLayout(straightBoard.geometry, 2, straightBoard.winHole);
const cOpts = { startPair: false, winHole: 121, backStart: classic.backStart };
const sOpts = { startPair: true, winHole: 121, backStart: straight.backStart };

test('pegCenters (classic): fresh board homes the back peg in the back-start hole', () => {
  const r = pegCenters({ track: 0, front: 0, back: 0 }, classic.holes, cOpts);
  assert.equal(r.back, classic.backStart[0]);   // dedicated back-start hole
  assert.equal(r.front, classic.holes[0][0]);   // start line
});

test('pegCenters (classic): after scoring, trailing peg drops to the start line', () => {
  const r = pegCenters({ track: 1, front: 4, back: 0 }, classic.holes, cOpts);
  assert.equal(r.back, classic.holes[1][0]);    // start line (back-start hole emptied)
  assert.equal(r.front, classic.holes[1][4]);
});

test('pegCenters (classic): mid-game pegs sit at their own indices', () => {
  const r = pegCenters({ track: 0, front: 10, back: 7 }, classic.holes, cOpts);
  assert.equal(r.back, classic.holes[0][7]);
  assert.equal(r.front, classic.holes[0][10]);
});

test('pegCenters (straight): fresh board homes the back peg in the inner game hole', () => {
  assert.equal(straight.backStart, undefined);  // straight has no dedicated back-start hole
  const r = pegCenters({ track: 0, front: 0, back: 0 }, straight.holes, sOpts);
  assert.equal(r.back, straight.holes[0][121]); // inner game hole (inside track)
  assert.equal(r.front, straight.holes[0][0]);  // outer start hole
});

test('pegCenters (straight): after scoring, moved peg is on the outer track, trailing peg at outer start', () => {
  const r = pegCenters({ track: 1, front: 4, back: 0 }, straight.holes, sOpts);
  assert.equal(r.back, straight.holes[1][0]);   // outer start line
  assert.equal(r.front, straight.holes[1][4]);  // outer track hole 4
});

test('pegCenters: winning front peg lands on the game hole; back peg has moved away', () => {
  const r = pegCenters({ track: 0, front: 121, back: 116 }, classic.holes, cOpts);
  assert.equal(r.front, classic.holes[0][121]);
  assert.equal(r.back, classic.holes[0][116]);
  assert.notEqual(r.front, r.back);
});

// --- buildScorePath: animation path + per-segment timing ----------------------
const lane = straight.holes[0];

test('buildScorePath: fresh score has no catch-up — every segment counts', () => {
  const { path, durs } = buildScorePath(lane, lane[121], 0, 0, 4, 100, 50);
  assert.equal(path.length, 5);                 // origin + holes 1..4
  assert.deepEqual(durs, [50, 50, 50, 50]);     // all counting (oldFront 0)
});

test('buildScorePath: with a gap, catch-up is fast then counting is per-hole', () => {
  // back=2, front=10 (gap 8), score 5 → toIdx 15.
  const { path, durs } = buildScorePath(lane, lane[2], 2, 10, 15, 100, 50);
  assert.equal(path.length, 14);                // origin + holes 3..15
  assert.equal(durs.length, 13);
  // holes 3..10 are catch-up: 8 segments sharing 100ms → 12.5ms each
  for (let i = 0; i < 8; i++) assert.equal(durs[i], 12.5);
  // holes 11..15 are counting: 50ms each
  for (let i = 8; i < 13; i++) assert.equal(durs[i], 50);
  const total = durs.reduce((a, b) => a + b, 0);
  assert.equal(total, 100 + 5 * 50);            // catch-up total + counting
});

test('buildScorePath: path ends exactly on the destination hole', () => {
  const { path } = buildScorePath(lane, lane[2], 2, 10, 15, 100, 50);
  assert.equal(path[path.length - 1], lane[15]);
});

// --- isLegalScore -------------------------------------------------------------
test('isLegalScore: accepts 0..29 except the impossible counts', () => {
  for (const ok of [0, 1, 2, 12, 18, 20, 21, 22, 23, 24, 28, 29]) {
    assert.equal(isLegalScore(ok), true, `${ok} should be legal`);
  }
  for (const bad of [19, 25, 26, 27, 30, 40, -1, 1.5, NaN]) {
    assert.equal(isLegalScore(bad), false, `${bad} should be illegal`);
  }
});
