// The board strip: a compact tournament long board across the width (see the iOS
// app's docs/board-strip-spec.md). Four rows of sixty plus one hole of their own,
// in two pairs. Each player starts on the outer row (top for the bot, bottom for
// you), runs out to sixty, and comes back on the inner row into their end hole.
const NS = 'http://www.w3.org/2000/svg';
const W = 390;
const H = 60;
const MARGIN = 7;
const GROUP_GAP = 0.9; // extra gap between fives, in pitches
const PITCH = (W - 2 * MARGIN) / (59 + 12 * (1 + GROUP_GAP));
const ROWS = { // y of [outer, inner] per player (0 you, 1 bot)
  0: [51, 41],
  1: [9, 19],
};
const COLORS = ['#0a7aff', '#c0281f'];

const colX = (i) => MARGIN + (i === 0 ? 0 : PITCH * (1 + GROUP_GAP) + (i - 1) * PITCH + Math.floor((i - 1) / 5) * PITCH * GROUP_GAP);

/** Hole position for a score: 0 start hole, 1..60 out, 61..120 back, 121 end hole. */
export function holeFor(who, score) {
  const [outer, inner] = ROWS[who];
  if (score <= 0) return { x: colX(0), y: outer };
  if (score <= 60) return { x: colX(score), y: outer };
  if (score <= 120) return { x: colX(121 - score), y: inner };
  return { x: colX(0), y: inner };
}

function el(tag, attrs) {
  const e = document.createElementNS(NS, tag);
  for (const [k, v] of Object.entries(attrs)) e.setAttribute(k, v);
  return e;
}

export function createStrip(host) {
  const svg = el('svg', { viewBox: `0 0 ${W} ${H}`, class: 'strip', role: 'img', 'aria-label': 'Cribbage board' });
  svg.append(el('rect', { x: 0, y: 0, width: W, height: H, fill: '#c8a370' }));
  for (const who of [0, 1]) {
    for (const y of ROWS[who]) {
      for (let i = 0; i <= 60; i++) svg.append(el('circle', { cx: colX(i), cy: y, r: 1.55, fill: '#8a6a42' }));
    }
  }
  const pegs = [0, 1].map((who) => ({
    back: el('circle', { r: 2.6, fill: COLORS[who], class: 'peg back', opacity: '0.85' }),
    front: el('circle', { r: 3.4, fill: COLORS[who], class: 'peg' }),
  }));
  for (const p of pegs) svg.append(p.back, p.front);
  host.append(svg);

  function place(c, pos) {
    c.setAttribute('cx', pos.x);
    c.setAttribute('cy', pos.y);
  }
  return {
    /** scores/prev are [you, bot]. Nil: leading peg in the end hole, trailing in the start hole. */
    set(scores, prev) {
      for (const who of [0, 1]) {
        const s = scores[who];
        place(pegs[who].front, s === 0 ? holeFor(who, 121) : holeFor(who, s));
        place(pegs[who].back, holeFor(who, s === 0 ? 0 : prev[who]));
        svg.setAttribute('data-score-' + who, String(s));
      }
    },
  };
}
