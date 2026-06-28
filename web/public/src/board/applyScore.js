/**
 * Pure peg-mechanics core. No DOM, no rendering — the single source of truth
 * for cribbage scoring. The board uses this internally; a game server or
 * front-end controller can import it directly to compute state.
 *
 * @typedef {{ track: number, front: number, back: number }} PegPair
 * @typedef {{ pegs: PegPair[] }} BoardState
 * @typedef {{ players: number, winHole: number, start: { front: number, back: number } }} BoardConfig
 */

/**
 * Build the initial board state from a config: every player's pegs sit at the
 * configured start position.
 *
 * @param {BoardConfig} config
 * @returns {BoardState}
 */
export function createState(config) {
  return {
    pegs: Array.from({ length: config.players }, (_, track) => ({
      track,
      front: config.start.front,
      back: config.start.back,
    })),
  };
}

/**
 * Advance a player by `points`, returning a new state (input is never mutated).
 *
 * Cribbage pegging is leapfrog: the rear peg jumps ahead of the front by the
 * points scored, so the gap between a player's pegs equals their last score.
 * A result at or beyond the win hole is clamped to the win hole.
 *
 * @param {BoardState} state
 * @param {number} player  index into state.pegs
 * @param {number} points  non-negative integer
 * @param {BoardConfig} config
 * @returns {BoardState}
 */
export function applyScore(state, player, points, config) {
  if (!Number.isInteger(points)) throw new TypeError('points must be an integer');
  if (points < 0) throw new RangeError('points must be >= 0');
  if (player < 0 || player >= state.pegs.length) throw new RangeError('player out of range');

  if (points === 0) return state; // a scoreless turn moves nothing

  const peg = state.pegs[player];
  const moved = {
    ...peg,
    back: peg.front,
    front: Math.min(peg.front + points, config.winHole),
  };

  return {
    ...state,
    pegs: state.pegs.map((p, i) => (i === player ? moved : p)),
  };
}

/**
 * Has a player reached the win hole?
 *
 * @param {BoardState} state
 * @param {number} player
 * @param {BoardConfig} config
 * @returns {boolean}
 */
export function hasWon(state, player, config) {
  return state.pegs[player].front >= config.winHole;
}

/** Scores impossible to make in a single cribbage count. */
export const IMPOSSIBLE_SCORES = new Set([19, 25, 26, 27]);

/**
 * Is `points` a possible single cribbage count? 0–29, excluding 19/25/26/27.
 * @param {number} points
 * @returns {boolean}
 */
export function isLegalScore(points) {
  return Number.isInteger(points) && points >= 0 && points <= 29 && !IMPOSSIBLE_SCORES.has(points);
}
