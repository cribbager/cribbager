/**
 * A theme bundles the board's game/geometry config with its default visual
 * styling (CSS custom properties, injected once).
 *
 * The straight long board is laid out on a fixed grid: four straight rows (two
 * per player), holes in groups of five, with isolated start/game holes at the
 * left end. The classic board is a continuous track that meanders back across
 * the face. See geometry.js / classic.js for the index → position mappings.
 *
 * @typedef {Object} Theme
 * @property {string} name
 * @property {number} players
 * @property {number} winHole                 hole index that wins (also the clamp ceiling)
 * @property {{ front: number, back: number }} start  initial peg positions
 * @property {Object.<string,string>} [colors]  palette as CSS custom properties (applied inline)
 * @property {number|number[]} [skunkHole]    hole index(es) to mark with a skunk line (off by default)
 * @property {Geometry} geometry
 *
 * @typedef {Object} Geometry
 * @property {boolean} startPair   true when the front peg's home (index 0) is the inner start hole
 * @property {import('./geometry.js').Straight} [straight]  straight-board design params
 * @property {import('./classic.js').Classic} [classic]    classic-board design params
 */

/** Default theme: the straight long board. */
export const straightBoard = {
  name: 'straight',
  players: 2,
  winHole: 121,
  start: { front: 0, back: 0 },
  // Self-contained palette — applied as inline CSS custom properties, overriding
  // the injected defaults. A consumer can still restyle via their own CSS.
  colors: {
    '--board-bg': '#e7d6ad',
    '--board-edge': '#c7a86a',
    '--hole-deep': '#6d5c3f',
    '--hole-edge': '#b39d73',
    '--peg-0': '#c0392b',
    '--peg-1': '#20507e',
  },
  geometry: {
    layout: 'straight',
    startPair: true,
    // Compact design parameters — everything (viewBox, body, hole positions) is
    // derived from these by resolveLayout(). Tweak them in examples/designer.html.
    straight: {
      spacing: 8,    // hole pitch within a group
      groupGap: 8,   // extra gap between groups — group step is spacing + groupGap (2× the pitch)
      groupSize: 5,
      rowGap: 13,    // a player's two rows sit close together
      playerGap: 30, // the two players are separated by a wider gap
      edge: 20,      // inset from the board edge to the outermost holes (all four sides)
      bodyMargin: 10,
      bodyRadius: 14,
      holeRadius: 2.8,
      pegRadius: 3.6,
    },
  },
};

/**
 * A classic board: a meandering track that doubles back across the face.
 * Each player makes three runs joined by two U-turns — start outside, curve to
 * the opposite side, curve to finish in the middle. Two parallel tracks.
 */
export const classicBoard = {
  name: 'classic',
  players: 2,
  winHole: 121,
  start: { front: 0, back: 0 },
  // Self-contained palette: dark drilled holes on coloured lanes, gold/white pegs.
  colors: {
    '--board-bg': '#e7d6ad',
    '--board-edge': '#c7a86a',
    '--hole-deep': '#0d0d0d',
    '--hole-edge': '#303030',
    '--peg-0': '#ffd633',
    '--peg-1': '#f2f2f2',
    '--track-0': '#b8332b',
    '--track-1': '#2f6f9e',
    '--lane-edge': '#1c1c1c',
    '--number': '#1c1c1c',   // score labels in the same near-black as the borders
    '--label': '#6a5a36',
  },
  geometry: {
    layout: 'classic',
    startPair: false,     // both pegs begin together at the start
    colorByTrack: false,  // holes are dark/drilled; the lanes carry the colour
    trackBorder: true,    // filled colour lanes with a shared black border
    classic: {
      groups: 7,         // 7 groups of 5 = 35 holes per stretch
      groupSize: 5,
      spacing: 10,       // even pitch — groups of five shown with lines, not gaps
      rightArc: 8,       // holes on the right turn's arc (2 anchors added → 10 total)
      leftArc: 3,        // holes on the left turn's arc (2 anchors added → 5 total)
      laneGap: 56,       // vertical gap between stretches; keeps the inner turn radius > lane width
      trackGap: 14,      // lane width (the two players' tracks abut on a shared border)
      edge: 20,
      bodyMargin: 10,
      bodyRadius: 16,
      holeRadius: 3,
      pegRadius: 4,
    },
  },
};

/** The default theme used when a consumer doesn't supply one. */
export const defaultTheme = straightBoard;

/**
 * Default visual styling. Injected once per page; consumers restyle by
 * overriding any of these custom properties in their own CSS.
 */
export const defaultCss = `
.cribbage-board {
  --board-bg: #e7d6ad;        /* board body */
  --board-edge: #c7a86a;      /* body border */
  --board-edge-width: 2;
  --hole-deep: #6d5c3f;       /* recessed centre of a drilled hole */
  --hole-edge: #b39d73;       /* hole rim, near the board colour */
  --peg-0: #ffd633;           /* player 1 pegs — high contrast against the lane */
  --peg-1: #f2f2f2;           /* player 2 pegs */
  --peg-stroke: #141414;
  --track-0: #b8332b;         /* per-track lane colour (classic) */
  --track-1: #2f6f9e;
  --lane-edge: #1c1c1c;       /* thin shared black border between/around lanes */
  --lane-edge-width: 1.1;
  --label: #6a5a36;           /* START / FINISH text */
  --skunk-line: #7a4fb0;

  display: block;
  width: 100%;
  height: auto;
}
.cribbage-board .cb-body { fill: var(--board-bg); stroke: var(--board-edge); stroke-width: var(--board-edge-width); }
.cribbage-board .cb-hole-deep { stop-color: var(--hole-deep); }
.cribbage-board .cb-hole-edge { stop-color: var(--hole-edge); }
.cribbage-board .cb-hole-edge-0 { stop-color: var(--track-0); }
.cribbage-board .cb-hole-edge-1 { stop-color: var(--track-1); }
.cribbage-board .cb-hole { stroke: rgba(0, 0, 0, 0.18); stroke-width: 0.4; }
.cribbage-board .cb-lane-fill { fill: none; stroke-linejoin: miter; stroke-linecap: butt; }
.cribbage-board .cb-lane-fill[data-track="0"] { stroke: var(--track-0); }
.cribbage-board .cb-lane-fill[data-track="1"] { stroke: var(--track-1); }
.cribbage-board .cb-lane-edge { fill: none; stroke: var(--lane-edge); stroke-width: var(--lane-edge-width); stroke-linejoin: miter; stroke-linecap: butt; }
.cribbage-board .cb-startcell[data-track="0"] { fill: var(--track-0); }
.cribbage-board .cb-startcell[data-track="1"] { fill: var(--track-1); }
.cribbage-board .cb-startedge { fill: none; stroke: var(--lane-edge); stroke-width: var(--lane-edge-width); }
.cribbage-board .cb-group { stroke: var(--lane-edge); stroke-width: var(--lane-edge-width); stroke-linecap: butt; }
.cribbage-board .cb-label { fill: var(--label); font: 600 5.5px system-ui, sans-serif; letter-spacing: 0.5px; }
.cribbage-board .cb-number { fill: var(--number, #1c1c1c); font: 700 4px system-ui, sans-serif; }
.cribbage-board .cb-peg { stroke: var(--peg-stroke); stroke-width: 0.6; }
.cribbage-board .cb-peg[data-track="0"] { fill: var(--peg-0); }
.cribbage-board .cb-peg[data-track="1"] { fill: var(--peg-1); }
.cribbage-board.cb-interactive .cb-peg[data-peg="back"] { cursor: grab; }
.cribbage-board.cb-interactive .cb-peg[data-peg="back"]:hover { stroke-width: 1.2; }
.cribbage-board.cb-interactive .cb-peg.cb-dragging { cursor: grabbing; stroke-width: 1.2; }
.cribbage-board .cb-skunk { stroke: var(--skunk-line); stroke-width: 2; stroke-linecap: round; }
`;
