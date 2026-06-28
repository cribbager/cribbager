/**
 * Public API. A stateful board for ergonomics, with the scoring brain living in
 * the pure applyScore core (not here). Mount it into a container and drive it
 * with score()/setPegs(); read with getScore()/getState().
 */
import { defaultTheme, defaultCss } from './theme.js';
import { createState, applyScore, hasWon, isLegalScore } from './applyScore.js';
import { createBoardElement, drawPegs, injectStyles } from './renderer.js';

export { applyScore, createState, hasWon, isLegalScore } from './applyScore.js';
export { defaultTheme, straightBoard, classicBoard, defaultCss } from './theme.js';
export { pegCenters } from './renderer.js';

/**
 * Build the moving peg's animation path and per-segment durations — pure, so the
 * timing logic is unit-testable without a DOM/rAF. The peg starts at `fromPos`
 * (its on-screen origin) and visits each hole `fromIdx+1..toIdx`. Holes up to
 * `oldFront` are the quick catch-up (whole phase = `catchupMs`); holes past it
 * are the per-hole count (`msPerHole` each).
 *
 * @param {{x:number,y:number}[]} lane    hole centres for the player's track
 * @returns {{ path: {x:number,y:number}[], durs: number[] }}
 */
export function buildScorePath(lane, fromPos, fromIdx, oldFront, toIdx, catchupMs, msPerHole) {
  const path = [fromPos];
  const durs = [];
  const catchupSegs = Math.max(0, oldFront - fromIdx);
  const catchupPer = catchupSegs > 0 ? catchupMs / catchupSegs : 0;
  for (let k = fromIdx + 1; k <= toIdx; k++) {
    if (!lane?.[k]) continue;
    path.push(lane[k]);
    durs.push(k <= oldFront ? catchupPer : msPerHole);
  }
  return { path, durs };
}

/**
 * Create and mount a cribbage board.
 *
 * @param {Element} container  element to mount the SVG into
 * @param {{ theme?: import('./theme.js').Theme, css?: string,
 *           interactive?: boolean, onScore?: (result: object) => void,
 *           animMsPerHole?: number, animCatchupMs?: number }} [options]
 *   interactive — enable drag-the-back-peg-to-score (default false; also toggleable
 *   via setInteractive). onScore — called after each drag attempt with
 *   { player, points, position, won } on success or { player, rejected, reason, points }.
 *   animMsPerHole — counting speed, ms per hole counted past the front peg (default
 *   50, 0 = instant); settable via setAnimationSpeed. animCatchupMs — total ms for
 *   the quick slide up to the front peg before counting begins (default 100).
 * @returns {Board}
 */
export function createBoard(container, options = {}) {
  const theme = options.theme ?? defaultTheme;
  injectStyles(options.css ?? defaultCss);

  const { svg, holes, pegLayer, pegRadius, backStart } = createBoardElement(theme);
  // Apply the theme's palette as inline CSS custom properties. Being inline,
  // they take precedence over the injected default stylesheet. A consumer
  // restyles by supplying their own theme.colors (or overriding a var with
  // !important / their own element.style).
  if (theme.colors) for (const [k, v] of Object.entries(theme.colors)) svg.style.setProperty(k, v);
  container.appendChild(svg);

  let state = createState(theme);
  const pegOpts = { startPair: theme.geometry.startPair, winHole: theme.winHole, pegRadius, backStart };

  // Peg-movement animation: the moving peg slides quickly up to the front peg
  // (the whole catch-up takes `animCatchupMs`), then counts one hole at a time
  // past it to the scoring hole at `animMsPerHole` per hole (0 = instant). State
  // updates synchronously; only the visual animates. Tweak via setAnimationSpeed().
  let animMsPerHole = options.animMsPerHole ?? 50;
  let animCatchupMs = options.animCatchupMs ?? 100;
  let rafId = 0;
  const cancelAnim = () => { if (rafId) cancelAnimationFrame(rafId); rafId = 0; };
  const render = () => { cancelAnim(); drawPegs(pegLayer, holes, state, pegOpts); };
  render();

  const pegEl = (player, which) => pegLayer.querySelector(`.cb-peg[data-track="${player}"][data-peg="${which}"]`);
  // Glide the just-moved peg to the scoring hole. After a score that peg is the
  // new FRONT (data-peg="front"), so that's the element we animate.
  const animateScore = (player, fromIdx, fromPos, oldFront, toIdx) => {
    const el = pegEl(player, 'front');
    if (!el || !fromPos) return;
    const { path, durs } = buildScorePath(holes[player], fromPos, fromIdx, oldFront, toIdx, animCatchupMs, animMsPerHole);
    const segs = path.length - 1;
    const total = durs.reduce((a, b) => a + b, 0);
    if (segs < 1 || total <= 0) return;
    const ends = [];
    let acc = 0;
    for (const d of durs) ends.push((acc += d));                  // cumulative segment end times
    el.setAttribute('cx', path[0].x);   // start at the origin so there's no flash at the end
    el.setAttribute('cy', path[0].y);
    let t0 = 0;
    const step = (now) => {
      if (!t0) t0 = now;                // base the clock on the rAF timestamp itself
      const elapsed = Math.min(total, now - t0);
      let i = 0;
      while (i < segs - 1 && elapsed >= ends[i]) i++;
      const segStart = i === 0 ? 0 : ends[i - 1];
      const u = Math.min(1, (elapsed - segStart) / (ends[i] - segStart || 1)); // 0..1 within segment i
      const a = path[i], b = path[i + 1];
      el.setAttribute('cx', a.x + (b.x - a.x) * u);
      el.setAttribute('cy', a.y + (b.y - a.y) * u);
      rafId = elapsed < total ? requestAnimationFrame(step) : 0;
    };
    rafId = requestAnimationFrame(step);
  };

  // --- Optional interactive drag-to-peg --------------------------------------
  // Disabled by default. When on, dragging a player's back peg onto a hole pegs
  // a score equal to the distance past the front peg (validated). The host is
  // notified via options.onScore so it can sync any surrounding UI.
  let interactive = false;
  const onScore = typeof options.onScore === 'function' ? options.onScore : null;
  let drag = null;

  const setInteractiveClass = () => svg.classList.toggle('cb-interactive', interactive);
  const userPoint = (x, y) => {
    const m = svg.getScreenCTM();
    if (!m) return null;
    const pt = svg.createSVGPoint();
    pt.x = x; pt.y = y;
    return pt.matrixTransform(m.inverse());
  };
  const winner = () => state.pegs.findIndex((p) => p.front >= theme.winHole);
  const nearestHole = (player, u) => {
    const row = holes[player] || [];
    let index = -1, dist = Infinity;
    for (let i = 1; i < row.length; i++) {
      if (!row[i]) continue;
      const d = Math.hypot(row[i].x - u.x, row[i].y - u.y);
      if (d < dist) { dist = d; index = i; }
    }
    // Reject drops further than roughly one hole-step from any hole.
    const row1 = row[1], row2 = row[2];
    const step = row1 && row2 ? Math.hypot(row2.x - row1.x, row2.y - row1.y) : 12;
    return { index, dist, step };
  };
  const endDrag = () => {
    if (!drag) return;
    drag.pegEl.style.pointerEvents = '';
    drag.pegEl.classList.remove('cb-dragging');
    drag = null;
  };

  const onDown = (e) => {
    if (!interactive || e.button) return;
    const pegEl = e.target.closest?.('.cb-peg[data-peg="back"]');
    if (!pegEl) return;
    const player = Number(pegEl.getAttribute('data-track'));
    if (winner() !== -1) { onScore?.({ player, rejected: true, reason: 'gameover' }); return; }
    drag = { pegEl, player, front: state.pegs[player].front, x0: e.clientX, y0: e.clientY, moved: false };
    pegEl.style.pointerEvents = 'none'; // let holes beneath be hit-tested
    pegEl.classList.add('cb-dragging');
    e.preventDefault();
  };
  const onMove = (e) => {
    if (!drag) return;
    if (!drag.moved && Math.hypot(e.clientX - drag.x0, e.clientY - drag.y0) > 3) drag.moved = true;
    const u = userPoint(e.clientX, e.clientY);
    if (!u) return;
    drag.pegEl.setAttribute('cx', u.x);
    drag.pegEl.setAttribute('cy', u.y);
  };
  const onUp = (e) => {
    if (!drag) return;
    const d = drag;
    endDrag();
    if (!d.moved) { render(); return; }            // a plain click does nothing
    const u = userPoint(e.clientX, e.clientY);
    const { index, dist, step } = u ? nearestHole(d.player, u) : { index: -1 };
    const finish = () => render();                 // snap pegs back to the grid
    if (index < 1 || dist > step) { finish(); onScore?.({ player: d.player, rejected: true, reason: 'offtrack' }); return; }
    const points = index - d.front;
    if (points <= 0) { finish(); onScore?.({ player: d.player, rejected: true, reason: 'backward' }); return; }
    if (!isLegalScore(points)) { finish(); onScore?.({ player: d.player, rejected: true, reason: 'illegal', points }); return; }
    state = applyScore(state, d.player, points, theme);
    render();
    onScore?.({ player: d.player, points, position: state.pegs[d.player].front, won: hasWon(state, d.player, theme) });
  };

  // The drag needs window-level move/up listeners (the pointer roams off the svg).
  // Attach them only while interactive so a non-interactive board adds zero global
  // listeners — and detach on disable/destroy so nothing leaks across recreates.
  let listening = false;
  const startListening = () => {
    if (listening) return;
    listening = true;
    window.addEventListener('pointermove', onMove);
    window.addEventListener('pointerup', onUp);
    window.addEventListener('pointercancel', onUp);
  };
  const stopListening = () => {
    if (!listening) return;
    listening = false;
    window.removeEventListener('pointermove', onMove);
    window.removeEventListener('pointerup', onUp);
    window.removeEventListener('pointercancel', onUp);
  };

  svg.addEventListener('pointerdown', onDown); // local to the svg; removed with it
  interactive = !!options.interactive;
  if (interactive) startListening();
  setInteractiveClass();

  /** @typedef {Object} Board */
  return {
    /** The mounted <svg> element. */
    element: svg,

    /**
     * Advance a player by `points` (leapfrog, clamped at the win hole). State
     * updates synchronously; the peg glides to the scoring hole (see
     * setAnimationSpeed).
     * @returns {{ position: number, won: boolean }}
     */
    score(player, points) {
      // Capture the moving (back) peg's current on-screen position before the move.
      const fromIdx = state.pegs[player].back;
      const oldFront = state.pegs[player].front;
      const backEl = pegEl(player, 'back');
      const fromPos = backEl
        ? { x: +backEl.getAttribute('cx'), y: +backEl.getAttribute('cy') }
        : holes[player]?.[fromIdx];
      state = applyScore(state, player, points, theme);
      render();
      const toIdx = state.pegs[player].front;
      if (points > 0 && toIdx > fromIdx) animateScore(player, fromIdx, fromPos, oldFront, toIdx);
      return { position: state.pegs[player].front, won: hasWon(state, player, theme) };
    },

    /**
     * Set a player's pegs directly — for training scenarios. Values are clamped
     * to 0..winHole, and the back peg can't sit ahead of the front (it's clamped
     * to the front), keeping the pair a valid leapfrog state.
     */
    setPegs(player, { front, back }) {
      if (player < 0 || player >= state.pegs.length) throw new RangeError('player out of range');
      const clamp = (n) => Math.max(0, Math.min(theme.winHole, n));
      const f = clamp(front);
      const b = Math.min(clamp(back), f);
      const next = { ...state.pegs[player], front: f, back: b };
      state = { ...state, pegs: state.pegs.map((p, i) => (i === player ? next : p)) };
      render();
    },

    /** Front peg position for a player; with no argument, all players' scores. */
    getScore(player) {
      return player === undefined ? state.pegs.map((p) => p.front) : state.pegs[player].front;
    },

    /** Full peg positions (a copy — safe to keep). */
    getState() {
      return structuredClone(state);
    },

    /** Index of the player who has reached the win hole, or -1 if none. */
    getWinner() {
      return winner();
    },

    /** Return every player to the theme's start position. */
    reset() {
      state = createState(theme);
      render();
    },

    /** Enable or disable drag-the-back-peg-to-score interaction. */
    setInteractive(on) {
      interactive = !!on;
      if (interactive) startListening();
      else { endDrag(); stopListening(); render(); }
      setInteractiveClass();
    },

    /** Set peg-movement animation speed in ms per hole travelled (0 = instant). */
    setAnimationSpeed(msPerHole) {
      animMsPerHole = Math.max(0, Number(msPerHole) || 0);
    },

    /** Remove the board from the DOM and detach its global listeners. */
    destroy() {
      cancelAnim();
      stopListening();
      svg.remove();
    },
  };
}
