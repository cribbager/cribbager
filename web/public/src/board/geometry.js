/**
 * Geometry for the straight long board — holes on a fixed grid (no curves).
 *
 * Everything is derived from a compact set of design parameters (the `straight`
 * config): the viewBox, the board body, and every hole position. Nothing is
 * hand-tuned, so a board designer can drive the whole layout by changing numbers.
 *
 * Each player has two straight rows. You peg UP the outer row (left→right,
 * holes 1..60), then BACK along the inner row (right→left, holes 61..120).
 * Index 0 is the outer start hole and the win hole is the inner game hole —
 * both isolated at the left end. The two players mirror around the centre gap.
 *
 * @typedef {Object} Straight
 * @property {number} spacing     centre-to-centre between holes within a group
 * @property {number} groupGap    extra gap between groups (group step = spacing + groupGap)
 * @property {number} groupSize   holes per group (5)
 * @property {number} rowGap      between a player's two rows
 * @property {number} playerGap   between the two players (centre channel)
 * @property {number} edge        inset from the board body to the outermost holes (all sides)
 * @property {number} bodyMargin  inset from the viewBox to the board body
 * @property {number} bodyRadius  corner radius of the board body
 * @property {number} holeRadius
 * @property {number} pegRadius
 */

import { classicLayout } from './classic.js';

/**
 * Resolve a layout from a theme's geometry, dispatching on `geometry.layout`
 * ('straight' by default, or 'classic'). Every layout returns the same shape, so
 * the renderer, board, and scoring are layout-agnostic.
 *
 * @returns {{ width:number, height:number,
 *            body:{x:number,y:number,width:number,height:number,radius:number},
 *            holes:{x:number,y:number}[][], holeRadius:number, pegRadius:number }}
 */
export function resolveLayout(geometry, players, winHole) {
  if (geometry.layout === 'classic') return classicLayout(geometry.classic, players, winHole);
  return straightLayout(geometry.straight, players, winHole);
}

/**
 * Resolve the straight long-board layout from its design parameters.
 *
 * @param {Straight} straight
 * @param {number} players
 * @param {number} winHole
 */
export function straightLayout(straight, players, winHole) {
  const { spacing, groupGap, groupSize, rowGap, playerGap, edge, bodyMargin, bodyRadius } = straight;
  const cols = (winHole - 1) / 2; // track holes per row (60 for a 121 board)
  const isoGap = spacing + groupGap; // the iso hole sits one group-step before group 1

  // x offset of track column c (1..cols) from the isolated hole (offset 0).
  const colOffset = (c) => isoGap + (c - 1) * spacing + Math.floor((c - 1) / groupSize) * groupGap;

  const x0 = bodyMargin + edge; // isolated start/game hole
  const y0 = bodyMargin + edge; // outer row of player 0
  const width = x0 + colOffset(cols) + edge + bodyMargin;
  const height = y0 + (2 * rowGap + playerGap) + edge + bodyMargin;

  const body = {
    x: bodyMargin,
    y: bodyMargin,
    width: width - 2 * bodyMargin,
    height: height - 2 * bodyMargin,
    radius: bodyRadius,
  };

  const colX = (c) => x0 + colOffset(c);
  const holes = Array.from({ length: players }, (_, p) => {
    // Players mirror around the centre gap so both inner rows face it.
    const block = y0 + p * (rowGap + playerGap);
    const outerY = p === 0 ? block : block + rowGap;
    const innerY = p === 0 ? block + rowGap : block;

    const pos = new Array(winHole + 1);
    // Peg UP the outer row first, then BACK along the inner row. The back peg
    // waits in the inner-left game hole and hops onto the outer track when the
    // player scores; the front peg stays in the outer-left start hole.
    pos[0] = { x: x0, y: outerY }; // start — outer-left isolated hole (front/stationary peg)
    for (let i = 1; i <= cols; i++) pos[i] = { x: colX(i), y: outerY };               // up the outer row
    for (let i = cols + 1; i <= 2 * cols; i++) pos[i] = { x: colX(2 * cols + 1 - i), y: innerY }; // back the inner row
    pos[winHole] = { x: x0, y: innerY }; // game hole — inner-left isolated (back/moving peg's home)
    return pos;
  });

  return { width, height, body, holes, holeRadius: straight.holeRadius, pegRadius: straight.pegRadius };
}
