/**
 * SVG renderer. Builds the static board (body + drilled holes) once and draws
 * the peg layer from state on every update. Pure drawing — it never mutates the
 * game state. Holes carry `data-track` / `data-index` so input handling can be
 * added later by reading attributes off the clicked element.
 */
import { resolveLayout } from './geometry.js';

const SVGNS = 'http://www.w3.org/2000/svg';
const STYLE_ID = 'cribbage-board-styles';

let boardSeq = 0; // unique gradient ids when multiple boards share a page

/** Inject the default stylesheet once per document. */
export function injectStyles(css) {
  if (document.getElementById(STYLE_ID)) return;
  const style = document.createElement('style');
  style.id = STYLE_ID;
  style.textContent = css;
  document.head.appendChild(style);
}

function el(name, attrs) {
  const node = document.createElementNS(SVGNS, name);
  for (const [k, v] of Object.entries(attrs)) node.setAttribute(k, v);
  return node;
}

/**
 * Build the board's SVG and its static frame.
 *
 * @param {import('./theme.js').Theme} theme
 * @returns {{ svg: SVGSVGElement, holes: {x:number,y:number}[][], pegLayer: SVGGElement }}
 *   `holes[track][index]` is the centre of each hole.
 */
export function createBoardElement(theme) {
  const { geometry, players, winHole } = theme;
  const layout = resolveLayout(geometry, players, winHole);

  const svg = el('svg', {
    class: 'cribbage-board',
    viewBox: `0 0 ${layout.width} ${layout.height}`,
    xmlns: SVGNS,
  });

  // Drilled-hole gradient(s): a recessed look. One neutral gradient by default,
  // or one per track when the theme colours holes by track (--track-N).
  const base = `cb-hole-${boardSeq++}`;
  const colorByTrack = !!geometry.colorByTrack;
  const defs = el('defs', {});
  const makeGrad = (id, edgeClass) => {
    const grad = el('radialGradient', { id, cx: '0.5', cy: '0.35', r: '0.65' });
    grad.appendChild(el('stop', { class: 'cb-hole-deep', offset: '0' }));
    grad.appendChild(el('stop', { class: edgeClass, offset: '1' }));
    defs.appendChild(grad);
  };
  if (colorByTrack) {
    for (let t = 0; t < players; t++) makeGrad(`${base}-${t}`, `cb-hole-edge-${t}`);
  } else {
    makeGrad(base, 'cb-hole-edge');
  }
  svg.appendChild(defs);
  const holeFill = (t) => `url(#${colorByTrack ? `${base}-${t}` : base})`;

  // Solid board body — holes are drilled into this, not connected by a line.
  svg.appendChild(el('rect', {
    class: 'cb-body',
    x: layout.body.x, y: layout.body.y,
    width: layout.body.width, height: layout.body.height,
    rx: layout.body.radius,
  }));

  // Separated start box: a mini two-lane segment (red over blue) with a shared
  // border, matching the track. Drawn under the holes.
  if (layout.startBox) {
    const b = layout.startBox;
    const box = el('g', { class: 'cb-startbox' });
    box.appendChild(el('rect', { class: 'cb-startcell', 'data-track': 0, x: b.x, y: b.y, width: b.width, height: b.laneH }));
    box.appendChild(el('rect', { class: 'cb-startcell', 'data-track': 1, x: b.x, y: b.y + b.laneH, width: b.width, height: b.laneH }));
    box.appendChild(el('rect', { class: 'cb-startedge', x: b.x, y: b.y, width: b.width, height: b.height, rx: 2 }));
    box.appendChild(el('line', { class: 'cb-startedge', x1: b.x, y1: b.y + b.laneH, x2: b.x + b.width, y2: b.y + b.laneH }));
    svg.appendChild(box);
  }

  // Per-track lanes (classic): filled colour bands that abut, with three
  // boundary lines — the middle one is shared, so tracks meet on a single border.
  if (layout.lanePaths && geometry.trackBorder) {
    const laneLayer = el('g', { class: 'cb-lanes' });
    layout.lanePaths.forEach((d, t) => laneLayer.appendChild(
      el('path', { class: 'cb-lane-fill', 'data-track': t, d, 'stroke-width': layout.laneWidth })));
    for (const d of layout.borderPaths) laneLayer.appendChild(el('path', { class: 'cb-lane-edge', d }));
    svg.appendChild(laneLayer);
  }

  // Group-of-five lines (classic), drawn under the holes.
  if (layout.groupLines) {
    const lineLayer = el('g', { class: 'cb-groups' });
    for (const l of layout.groupLines) lineLayer.appendChild(el('line', { class: 'cb-group', x1: l.x1, y1: l.y1, x2: l.x2, y2: l.y2 }));
    svg.appendChild(lineLayer);
  }

  const holes = layout.holes;
  const holeLayer = el('g', { class: 'cb-holes' });
  holes.forEach((centers, t) => {
    for (let i = 0; i < centers.length; i++) {
      const c = centers[i];
      if (!c) continue;
      holeLayer.appendChild(el('circle', {
        class: 'cb-hole',
        'data-track': t,
        'data-index': i,
        cx: c.x,
        cy: c.y,
        r: layout.holeRadius,
        fill: holeFill(t),
      }));
    }
  });
  // Extra back-start holes (a second start hole per player), if the layout has them.
  if (layout.backStart) {
    layout.backStart.forEach((c, t) => holeLayer.appendChild(el('circle', {
      class: 'cb-hole', 'data-track': t, 'data-index': -1,
      cx: c.x, cy: c.y, r: layout.holeRadius, fill: holeFill(t),
    })));
  }
  svg.appendChild(holeLayer);

  // Scale numbers at every group-of-five boundary, between the two tracks —
  // rotated vertical, transparent background.
  if (layout.numbers) {
    const numLayer = el('g', { class: 'cb-numbers' });
    for (const n of layout.numbers) {
      const attrs = { class: 'cb-number', x: n.x, y: n.y, 'text-anchor': 'middle', 'dominant-baseline': 'central' };
      if (n.rotate) attrs.transform = `rotate(${n.rotate} ${n.x} ${n.y})`;
      const t = el('text', attrs);
      t.textContent = n.text;
      numLayer.appendChild(t);
    }
    svg.appendChild(numLayer);
  }

  // START / FINISH labels (classic), optionally rotated to run vertically.
  for (const label of [layout.startLabel, layout.finishLabel]) {
    if (!label) continue;
    const attrs = { class: 'cb-label', x: label.x, y: label.y, 'text-anchor': 'middle', 'dominant-baseline': 'middle' };
    if (label.rotate) attrs.transform = `rotate(${label.rotate} ${label.x} ${label.y})`;
    const t = el('text', attrs);
    t.textContent = label.text;
    svg.appendChild(t);
  }

  // Optional skunk line(s): a tick across the track at the given hole(s). Off
  // unless the theme sets `skunkHole`.
  if (theme.skunkHole != null) {
    const markLayer = el('g', { class: 'cb-marks' });
    for (const centers of holes) {
      for (const i of [].concat(theme.skunkHole)) addSkunkTick(markLayer, centers, i, layout.holeRadius * 2.5);
    }
    svg.appendChild(markLayer);
  }

  const pegLayer = el('g', { class: 'cb-pegs' });
  svg.appendChild(pegLayer); // on top of holes

  return { svg, holes, pegLayer, pegRadius: layout.pegRadius, backStart: layout.backStart };
}

/**
 * Redraw the peg layer from the current state. Both of a player's pegs are the
 * same colour; their separation shows the last score.
 *
 * @param {SVGGElement} pegLayer
 * @param {{x:number,y:number}[][]} holes
 * @param {import('./applyScore.js').BoardState} state
 * @param {{ startPair?: boolean, winHole: number, pegRadius: number }} opts
 */
/**
 * Where a player's two pegs render — pure, depends only on the peg, the hole
 * geometry, and the layout flags (so it's unit-testable without a DOM).
 *
 * The front peg always sits at its index (hole 0 is the start line / outer
 * start). The back peg's index-0 home depends on the board:
 *  - classic: a dedicated back-start hole; once the front advances, the trailing
 *    peg drops to the start line (hole 0) and the back-start hole empties.
 *  - straight (startPair): on a fresh board the back peg waits in the inner game
 *    hole (the inside track) and is the peg that hops onto the outer track to
 *    score; once scored, the trailing peg sits at the outer start line.
 *
 * @returns {{ back: {x:number,y:number}|undefined, front: {x:number,y:number}|undefined }}
 */
export function pegCenters(peg, holes, { startPair, winHole, backStart }) {
  const lane = holes[peg.track];
  let back;
  if (peg.back !== 0) back = lane?.[peg.back];
  else if (peg.front === 0) back = backStart ? backStart[peg.track] : startPair ? lane?.[winHole] : lane?.[0];
  else back = lane?.[0];
  return { back, front: lane?.[peg.front] };
}

export function drawPegs(pegLayer, holes, state, opts) {
  const { pegRadius } = opts;
  const pegs = [];
  for (const peg of state.pegs) {
    const { back: backCenter, front: frontCenter } = pegCenters(peg, holes, opts);
    for (const [which, center] of [['back', backCenter], ['front', frontCenter]]) {
      if (!center) continue;
      pegs.push(el('circle', {
        class: 'cb-peg',
        'data-track': peg.track,
        'data-peg': which,
        cx: center.x,
        cy: center.y,
        r: pegRadius,
      }));
    }
  }
  pegLayer.replaceChildren(...pegs);
}

/** Draw a tick perpendicular to the row at hole `i` (tangent from its neighbours). */
function addSkunkTick(layer, centers, i, half) {
  if (i < 0 || i >= centers.length) return;
  const a = centers[Math.max(0, i - 1)];
  const b = centers[Math.min(centers.length - 1, i + 1)];
  const len = Math.hypot(b.x - a.x, b.y - a.y) || 1;
  const nx = -(b.y - a.y) / len; // perpendicular unit vector
  const ny = (b.x - a.x) / len;
  const c = centers[i];
  layer.appendChild(el('line', {
    class: 'cb-skunk',
    x1: c.x - nx * half, y1: c.y - ny * half,
    x2: c.x + nx * half, y2: c.y + ny * half,
  }));
}
