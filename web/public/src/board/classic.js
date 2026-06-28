/**
 * Classic layout — a meandering track that doubles back across the board.
 *
 * Per player: two start holes, then three 35-hole stretches joined by two
 * U-turns — stretch 1 (top) → big RIGHT turn → stretch 2 (bottom) → smaller
 * LEFT turn → stretch 3 (middle) → a shared finishing hole centred between the
 * two players' final stretches.
 *
 * Each turn's first and last hole are ANCHORS: they sit on the straightaway
 * (one column past the last straight hole), so only the holes *between* them
 * need arc maths. The right turn is 2 anchors + `rightArc` (8) = 10 holes; the
 * left turn is 2 anchors + `leftArc` (3) = 5. With 35-hole stretches that is
 * exactly 35·3 + 10 + 5 = 120 scoring holes.
 *
 * The two players run as parallel tracks; the left radius is nudged by
 * trackGap/4 so BOTH tracks' three stretches are evenly spaced. Pure (no DOM).
 *
 * @typedef {Object} Classic
 * @property {number} groups     groups per stretch (7 → 35 holes)
 * @property {number} groupSize  holes per group (5); group lines drawn between them
 * @property {number} spacing    even hole pitch on the straights
 * @property {number} rightArc   holes on the right turn's arc (anchors added automatically)
 * @property {number} leftArc    holes on the left turn's arc
 * @property {number} laneGap    vertical gap between stretches (sets the turn radii)
 * @property {number} trackGap   gap between the two players' tracks
 * @property {number} edge        inset from the board edge to the outermost holes
 * @property {number} bodyMargin
 * @property {number} bodyRadius
 * @property {number} holeRadius
 * @property {number} pegRadius
 */

const HALF_PI = Math.PI / 2;
const arcPoint = (cx, cy, r, a) => ({ x: cx + r * Math.cos(a), y: cy + r * Math.sin(a) });

export function classicLayout(p, players, winHole) {
  const { groups, groupSize, spacing: s, rightArc, leftArc, laneGap, trackGap: g,
          edge, bodyMargin, bodyRadius, holeRadius, pegRadius } = p;
  const L = groups * groupSize; // 35 holes per stretch

  // Right turn spans two band-gaps, left turn one. Rl is nudged by trackGap/4 so
  // BOTH players' three stretches end up evenly spaced.
  const Rr = laneGap;
  const Rl = laneGap / 2 + g / 4;

  const X0 = 0;
  const rCx = X0 + L * s;   // right-turn centre x = the anchor column (one past stretch end)
  const lCx = X0 - s;       // left-turn centre x = one column before the stretch start
  const endX = X0 + (L - 1) * s; // last straight-hole column

  // Start box: its left edge is flush with the left turn's outer edge (symmetry).
  // The two start pegs per player sit in their own straight, with a little spacing.
  const boxLeft = lCx - (Rl + g / 2);
  const startBackX = boxLeft + 0.85 * s;
  const startFrontX = startBackX + 1.3 * s;   // a little room for the start pegs
  const boxRight = startFrontX + 0.85 * s;
  const laneStartX = X0 - s / 2;              // where the track lane begins

  // Finish: centred between the two middle rows, set apart from the stretch end.
  const finishX = endX + 2.6 * s;
  const finishY = (2 * Rr - 2 * Rl) + g / 2;

  const lanes = [];
  const backStart = [];
  for (let pl = 0; pl < players; pl++) {
    const off = pl * g;                          // player 0 outside, player 1 inset
    const rr = Rr - off, rl = Rl - off;
    const topY = off, botY = 2 * Rr - off, midY = 2 * Rr - 2 * Rl + off;

    const pos = new Array(winHole + 1);
    pos[0] = { x: startFrontX, y: topY };          // front start hole (in the start box)
    backStart[pl] = { x: startBackX, y: topY };     // back start hole

    let i = 1;
    for (let j = 0; j < L; j++) pos[i++] = { x: X0 + j * s, y: topY };                    // stretch 1, L→R
    // right turn: rightArc+2 points incl. the two anchors (which land on topY / botY)
    for (let k = 0; k <= rightArc + 1; k++) pos[i++] = arcPoint(rCx, Rr, rr, -HALF_PI + (k / (rightArc + 1)) * Math.PI);
    for (let j = 0; j < L; j++) pos[i++] = { x: X0 + (L - 1 - j) * s, y: botY };          // stretch 2, R→L
    // left turn: leftArc+2 points incl. the two anchors (which land on botY / midY)
    for (let k = 0; k <= leftArc + 1; k++) pos[i++] = arcPoint(lCx, 2 * Rr - Rl, rl, HALF_PI + (k / (leftArc + 1)) * Math.PI);
    for (let j = 0; j < L; j++) pos[i++] = { x: X0 + j * s, y: midY };                    // stretch 3, L→R
    pos[winHole] = { x: finishX, y: finishY };     // shared finish, centred between the middle rows
    lanes.push(pos);
  }

  // Group borders: a vertical line at every group-of-five boundary on each
  // straight — including the ends (k = 0 and k = L), so the first and last
  // groups are bordered too. Each line spans exactly the band (lane outer edges,
  // ext = g/2), no overhang.
  const ext = g / 2;
  const midTop = 2 * Rr - 2 * Rl;
  // `score(k)` is the running total at the k-th group boundary of each stretch
  // (stretch 2 runs right→left, so it counts down).
  const stretches = [
    { yLo: 0, yHi: g, score: (k) => k, gaps: [] },                    // stretch 1: 5..35
    { yLo: 2 * Rr - g, yHi: 2 * Rr, score: (k) => 80 - k, gaps: [] },  // stretch 2: 80..45
    { yLo: midTop, yHi: midTop + g, score: (k) => 85 + k, gaps: [] },  // stretch 3: 85..120
  ];
  // Numbers are rotated 90° and centred on the boundary line, so each one breaks
  // its border with a gap: along the line the digits span ~text-length × advance,
  // across the line just the font height. `gapAlong`/`gapAcross` clear each.
  const NUM_FONT = 4, GLYPH = 0.62 * NUM_FONT, NUM_PAD = 1.3;
  const gapAlong = (text) => (text.length * GLYPH) / 2 + NUM_PAD;
  const gapAcross = NUM_FONT / 2 + NUM_PAD;
  // The final boundary (stretch 3, k = L) lands on the lane's end; inset it a hair
  // so the coloured lane runs past it and forms the flush terminal edge.
  const END_INSET = 0.65;

  const groupLines = [];
  const numbers = [];
  stretches.forEach((st, si) => {
    const cy = (st.yLo + st.yHi) / 2; // between the two tracks
    for (let k = 0; k <= L; k += groupSize) {
      const endInset = si === 2 && k === L ? END_INSET : 0;
      const x = X0 + (k - 0.5) * s - endInset;
      const yLo = st.yLo - ext, yHi = st.yHi + ext;
      const sc = st.score(k);
      if (sc > 0 && sc <= 120) {
        // Split the vertical border, leaving a gap centred on the number.
        const text = String(sc), g0 = gapAlong(text);
        numbers.push({ x, y: cy, text });
        groupLines.push({ x1: x, y1: yLo, x2: x, y2: cy - g0 });
        groupLines.push({ x1: x, y1: cy + g0, x2: x, y2: yHi });
        st.gaps.push(x); // the shared horizontal border breaks here too
      } else {
        groupLines.push({ x1: x, y1: yLo, x2: x, y2: yHi });
      }
    }
  });
  // The right turn holds two groups of five; add the radial divider between them
  // (at its rightmost point) and its score number (40) — gapped across the line.
  const innerR = Rr - 1.5 * g, outerR = Rr + 0.5 * g;
  const num40X = rCx + (Rr - g / 2);
  groupLines.push({ x1: rCx + innerR, y1: Rr, x2: num40X - gapAcross, y2: Rr });
  groupLines.push({ x1: num40X + gapAcross, y1: Rr, x2: rCx + outerR, y2: Rr });
  numbers.push({ x: num40X, y: Rr, text: '40' });

  // Bounding box → translate so the outermost holes sit `edge` inside the body.
  let minX = Infinity, minY = Infinity, maxX = -Infinity, maxY = -Infinity;
  const consider = (x, y) => { minX = Math.min(minX, x); minY = Math.min(minY, y); maxX = Math.max(maxX, x); maxY = Math.max(maxY, y); };
  for (const lane of lanes) for (const pt of lane) consider(pt.x, pt.y);
  for (const pt of backStart) consider(pt.x, pt.y);
  for (const l of groupLines) { consider(l.x1, l.y1); consider(l.x2, l.y2); }
  consider(boxLeft, -g / 2); consider(boxRight, 3 * g / 2); // start box extents

  const pad = bodyMargin + edge;
  const tx = pad - minX, ty = pad - minY;
  const shiftPt = (pt) => ({ x: pt.x + tx, y: pt.y + ty });
  const shiftLine = (l) => ({ x1: l.x1 + tx, y1: l.y1 + ty, x2: l.x2 + tx, y2: l.y2 + ty });

  const width = (maxX - minX) + 2 * pad;
  const height = (maxY - minY) + 2 * pad;
  const body = { x: bodyMargin, y: bodyMargin, width: width - 2 * bodyMargin, height: height - 2 * bodyMargin, radius: bodyRadius };
  const holes = lanes.map((lane) => lane.map(shiftPt));

  // A track centreline offset perpendicular by `off` (straights shift in y, turns
  // change radius). Used both for the filled lanes (off = 0 and g) and for the
  // three boundary lines (off = -g/2, g/2, 3g/2) — so the middle border is shared.
  const fx = (x) => (x + tx).toFixed(2);
  const fy = (y) => (y + ty).toFixed(2);
  const genPath = (off) => {
    const rr = Rr - off, rl = Rl - off;
    const topY = off, botY = 2 * Rr - off, midY = 2 * Rr - 2 * Rl + off;
    return `M ${fx(X0 - s / 2)} ${fy(topY)} L ${fx(rCx)} ${fy(topY)}`  // s/2 margin before the first hole
         + ` A ${rr} ${rr} 0 0 1 ${fx(rCx)} ${fy(botY)}`
         + ` L ${fx(lCx)} ${fy(botY)}`
         + ` A ${rl} ${rl} 0 0 1 ${fx(lCx)} ${fy(midY)}`  // sweep 1 → bulge left, matching the holes
         + ` L ${fx(endX + s / 2)} ${fy(midY)}`;          // s/2 margin past the last hole
  };

  // The shared middle border runs straight through every number. Build it like
  // genPath(g/2) but break each straight run with a gap (an `M` move) where a
  // number sits, and split the right-turn arc at the rightmost point for the 40.
  const horiz = (xFrom, xTo, y, centers, hw) => {
    const dir = Math.sign(xTo - xFrom);
    const lo = Math.min(xFrom, xTo) + hw, hi = Math.max(xFrom, xTo) - hw;
    const within = centers.filter((c) => c > lo && c < hi).sort((a, b) => dir * (a - b));
    let d = '';
    for (const c of within) d += ` L ${fx(c - dir * hw)} ${fy(y)} M ${fx(c + dir * hw)} ${fy(y)}`;
    return d + ` L ${fx(xTo)} ${fy(y)}`;
  };
  const genMidBorder = () => {
    const off = g / 2, hw = gapAcross;
    const rr = Rr - off, rl = Rl - off;
    const topY = off, botY = 2 * Rr - off, midY = 2 * Rr - 2 * Rl + off;
    const aGap = hw / rr;                                  // angular half-gap for the 40
    const ax = (a) => rCx + rr * Math.cos(a), ay = (a) => Rr + rr * Math.sin(a);
    return `M ${fx(X0 - s / 2)} ${fy(topY)}`
         + horiz(X0 - s / 2, rCx, topY, stretches[0].gaps, hw)
         + ` A ${rr} ${rr} 0 0 1 ${fx(ax(-aGap))} ${fy(ay(-aGap))}`   // up to just before the 40
         + ` M ${fx(ax(aGap))} ${fy(ay(aGap))}`                       // skip the 40
         + ` A ${rr} ${rr} 0 0 1 ${fx(rCx)} ${fy(botY)}`             // resume to the bottom
         + horiz(rCx, lCx, botY, stretches[1].gaps, hw)
         + ` A ${rl} ${rl} 0 0 1 ${fx(lCx)} ${fy(midY)}`
         // Stop the middle border just left of the final number (120) so it
         // doesn't run through it — the coloured cap + vertical line close the end.
         + horiz(lCx, endX + s / 2 - END_INSET - hw, midY, stretches[2].gaps, hw);
  };

  // A separated start box: a mini two-lane segment (red over blue, shared border)
  // holding the two start pegs per player. `laneH` is each coloured cell's height.
  const startBox = { x: boxLeft + tx, y: -g / 2 + ty, width: boxRight - boxLeft, height: 2 * g, laneH: g };
  // Labels run vertically (rotated 90° clockwise), centred in the gap between the
  // start box / finish hole and the track border, vertically centred on the band.
  const startLabel = { x: (boxRight + laneStartX) / 2 + tx, y: g / 2 + ty, text: 'START', rotate: 90 };
  const finishLabel = { x: (endX + s / 2 + finishX) / 2 + tx, y: finishY + ty, text: 'FINISH', rotate: 90 };

  return {
    width, height, body, holes,
    backStart: backStart.map(shiftPt),
    startBox, startLabel, finishLabel,
    numbers: numbers.map((n) => ({ x: n.x + tx, y: n.y + ty, text: n.text, rotate: 90 })),
    groupLines: groupLines.map(shiftLine),
    lanePaths: [genPath(0), genPath(g)],                              // filled, one per player
    borderPaths: [genPath(-g / 2), genMidBorder(), genPath(3 * g / 2)], // outer-0, shared middle (gapped at numbers), outer-1
    laneWidth: g,
    holeRadius, pegRadius,
  };
}
