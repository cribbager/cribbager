import { test } from 'node:test';
import assert from 'node:assert/strict';
import { straightLayout, resolveLayout } from '../public/src/board/geometry.js';
import { straightBoard, classicBoard } from '../public/src/board/theme.js';

const { players, winHole } = straightBoard;
const straight = straightBoard.geometry.straight;
const layout = straightLayout(straight, players, winHole);
const holes = layout.holes;

test('every player has winHole + 1 holes', () => {
  assert.equal(holes.length, players);
  for (const lane of holes) assert.equal(lane.length, winHole + 1);
});

test('start (0) and game (121) are the isolated left holes, vertically stacked', () => {
  const [p0] = holes;
  assert.equal(p0[0].x, p0[winHole].x);       // same column
  assert.notEqual(p0[0].y, p0[winHole].y);     // outer row vs inner row
});

test('outer row runs left→right, inner row right→left', () => {
  const p0 = holes[0];
  assert.ok(p0[1].x < p0[60].x, 'outer row ascends');
  assert.ok(p0[61].x > p0[120].x, 'inner row descends');
  assert.equal(p0[1].x, p0[120].x); // outer start col == inner end col (leftmost track column)
});

test('outer and inner rows sit on two distinct y values', () => {
  const p0 = holes[0];
  const outerY = p0[1].y, innerY = p0[61].y;
  assert.notEqual(outerY, innerY);
  for (let i = 1; i <= 60; i++) assert.equal(p0[i].y, outerY);
  for (let i = 61; i <= 120; i++) assert.equal(p0[i].y, innerY);
});

test('within-group spacing is uniform', () => {
  const p0 = holes[0];
  for (let c = 1; c <= 60; c++) {
    if (c % 5 === 0) continue; // skip group boundaries
    assert.equal(p0[c + 1].x - p0[c].x, straight.spacing, `holes ${c}->${c + 1}`);
  }
});

test('every group boundary AND the start/end hole gap are equal', () => {
  const p0 = holes[0];
  const groupStep = straight.spacing + straight.groupGap;
  assert.equal(p0[1].x - p0[0].x, groupStep, 'start hole -> group 1');
  for (let c = 5; c < 60; c += 5) {
    assert.equal(p0[c + 1].x - p0[c].x, groupStep, `group boundary after hole ${c}`);
  }
  assert.ok(groupStep > straight.spacing);
});

test('the track is inset by an equal margin on all four sides', () => {
  const xs = holes.flat().map((h) => h.x);
  const ys = holes.flat().map((h) => h.y);
  const left = Math.min(...xs) - layout.body.x;
  const right = (layout.body.x + layout.body.width) - Math.max(...xs);
  const top = Math.min(...ys) - layout.body.y;
  const bottom = (layout.body.y + layout.body.height) - Math.max(...ys);
  assert.equal(left, right, 'left vs right');
  assert.equal(top, bottom, 'top vs bottom');
  assert.equal(left, top, 'horizontal vs vertical inset');
  assert.equal(left, straight.edge, 'inset equals straight.edge');
});

test('the two players mirror: both inner rows face the centre gap', () => {
  const [p0, p1] = holes;
  // Holes 1..60 run along the outer row; 61..120 the inner row (toward the centre).
  assert.ok(p0[61].y > p0[1].y, 'player 0 inner row (61-120) is below its outer row');
  assert.ok(p1[61].y < p1[1].y, 'player 1 inner row (61-120) is above its outer row');
  assert.ok(p0[61].y < p1[61].y, 'player 0 inner sits above player 1 inner');
});

test('classic layout: same output shape, correct counts, inset', () => {
  const s = resolveLayout(classicBoard.geometry, classicBoard.players, classicBoard.winHole);
  assert.equal(s.holes.length, classicBoard.players);
  for (const lane of s.holes) assert.equal(lane.length, classicBoard.winHole + 1);
  // viewBox is positive and the body is inset by bodyMargin
  assert.ok(s.width > 0 && s.height > 0);
  assert.equal(s.body.x, classicBoard.geometry.classic.bodyMargin);
  // every hole lies inside the body rect
  for (const lane of s.holes) for (const h of lane) {
    assert.ok(h.x >= s.body.x && h.x <= s.body.x + s.body.width, 'hole within body x');
    assert.ok(h.y >= s.body.y && h.y <= s.body.y + s.body.height, 'hole within body y');
  }
});

test('classic: the two players run parallel (never coincide)', () => {
  const s = resolveLayout(classicBoard.geometry, 2, classicBoard.winHole);
  const [a, b] = s.holes;
  for (let i = 1; i <= 120; i++) {
    const d = Math.hypot(a[i].x - b[i].x, a[i].y - b[i].y);
    assert.ok(d > 1, `tracks separated at hole ${i}`);
  }
});
