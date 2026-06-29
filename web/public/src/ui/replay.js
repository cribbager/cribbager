// Stored-game replay view (A2). A dedicated, read-only page that steps through a
// finished game move-by-move with FULL visibility — both hands face-up, the crib
// face-up, the opponent's discards revealed — the richer companion to the A8
// per-hand discard analysis.
//
// It is account-scoped and post-game: it consumes GET /users/me/games/{id}/replay
// (built by a sibling task against the same contract), which 404s for a live game,
// a game the user wasn't in, or an unknown id, and 401s for a guest.
//
// This is a DEDICATED view, not the live board's render(): that renderer is
// seat-relative ("me" at the bottom) and only draws the opponent's cards face-down.
// Here we fold the event log into immutable spectator frames (replayFrames.js) and
// render the chosen frame from the decoupled lower-level modules — the board, the
// shared card face, and the existing .seat/.hand/.show-* styles.
import { createBoard, straightBoard } from '../board/board.js';
import { cardFace } from './cardFace.js';
import { mountHeader } from './header.js';
import { buildFrames, handStarts } from './replayFrames.js';

// tiny DOM helper (matches the on*-listener + text-node style used elsewhere)
function h(tag, attrs = {}, ...kids) {
  const e = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs)) {
    if (k === 'class') e.className = v;
    else if (k.startsWith('on') && typeof v === 'function') e.addEventListener(k.slice(2).toLowerCase(), v);
    else if (v != null) e.setAttribute(k, v);
  }
  for (const kid of kids.flat()) if (kid != null && kid !== false) e.append(kid.nodeType ? kid : document.createTextNode(kid));
  return e;
}

const root = document.getElementById('replay');
mountHeader();

// gameId comes from ?game=<id>; without it there is nothing to replay.
const gameId = new URLSearchParams(location.search).get('game');

const sortHand = (cards) => [...cards].sort((a, b) => a.rank - b.rank || a.suit - b.suit);

// message renders a single centered notice (loading / error / empty states),
// mirroring analyze.js's states exactly.
function message(title, body, action) {
  root.replaceChildren(
    h('h1', { class: 'an-title' }, 'Game replay'),
    h('div', { class: 'panel an-message' },
      h('p', { class: 'an-message-title' }, title),
      body ? h('p', { class: 'an-message-body' }, body) : null,
      action || null));
}

// ---- the board (reused module), mounted once, same theme as the live table ----
const boardMount = h('div', { class: 'board-mount' });
const board = createBoard(boardMount, {
  theme: {
    ...straightBoard,
    colors: {
      ...straightBoard.colors,
      '--board-bg': '#e7d6ad',
      '--board-edge-width': '0',
      '--peg-0': '#2f6f9e', // seat 0 (top) — blue
      '--peg-1': '#b8332b', // seat 1 (bottom) — red
    },
    geometry: {
      ...straightBoard.geometry,
      straight: { ...straightBoard.geometry.straight, playerGap: 20, rowGap: 12 },
    },
  },
  animMsPerHole: 42,
  animCatchupMs: 85,
});
// Seat 0 rides the TOP board track (0); seat 1 rides the BOTTOM track (1). Unlike
// the live board there is no "me" — both seats are shown plainly.
const boardTrack = (seat) => seat;

// ---- card rows (face-up everywhere — this is a spectator view) ----
function handRow(seat, frame) {
  return h('div', { class: 'hand' }, ...sortHand(frame.hands[seat]).map((c) => cardFace(c)));
}

// The dealer's deck: the crib (face-up for the replay) and the starter, pushed to
// the right of the pegging row (matching the live table's .deck placement).
function deckGroup(seat, frame) {
  if (frame.dealer !== seat) return null;
  const kids = [h('span', { class: 'crib-tag' }, 'crib')];
  if (frame.crib.length) for (const c of frame.crib) kids.push(cardFace(c, { small: true }));
  else kids.push(h('div', { class: 'crib-slot' }, 'crib'));
  if (frame.starter) kids.push(cardFace(frame.starter, { extra: 'show-starter' }));
  return h('div', { class: 'deck' }, ...kids);
}

function peggingRow(seat, frame) {
  const row = h('div', { class: 'pegged' }, ...frame.played[seat].map((c) => cardFace(c)));
  const deck = deckGroup(seat, frame);
  if (deck) row.append(deck);
  return row;
}

// The show rows (counted hand + starter + total, then the breakdown), reusing the
// live table's .show-* markup.
function showRows(show) {
  const cards = show.hand.map((c) => cardFace(c));
  const starterEl = show.starter ? cardFace(show.starter, { extra: 'show-starter' }) : null;
  const lead = show.isCrib ? [h('span', { class: 'crib-tag' }, 'crib')] : [];
  const showRow = h('div', { class: 'show-row' }, ...lead, ...cards, starterEl,
    h('div', { class: 'show-score' }, String(show.score.total)));
  const bd = h('div', { class: 'show-breakdown' });
  if (!show.score.items.length) bd.append(h('span', { class: 'bd-empty' }, 'no points'));
  else for (const it of show.score.items) bd.append(h('span', { class: 'bd-item' }, `${it.label} — ${it.points}`));
  return [showRow, bd];
}

// A seat's stack. Seat 0 (top) mirrors the live "opp" layout (hand above its
// pegging row); seat 1 (bottom) mirrors "you" (pegging above the hand). When that
// seat is being counted, its show rows replace the hand/pegging.
function seatEl(seat, frame, posClass) {
  const sh = frame.show;
  const rows = sh && sh.seat === seat
    ? showRows(sh)
    : seat === 0
      ? [handRow(seat, frame), peggingRow(seat, frame)]
      : [peggingRow(seat, frame), handRow(seat, frame)];
  const name = (seats[seat] && seats[seat].name) || `Seat ${seat + 1}`;
  const dealerTag = frame.dealer === seat ? h('span', { class: 'replay-dealer-tag' }, 'dealer') : null;
  return h('div', { class: 'seat ' + posClass },
    h('div', { class: 'rail-label' }, name, dealerTag),
    ...rows);
}

// The running pegging count, shown between the seats during play.
function countEl(frame) {
  if (frame.phase !== 'play') return null;
  return h('div', { class: 'replay-count' }, 'Count ', h('strong', {}, String(frame.count)));
}

// ---- state ----
let frames = [];
let starts = [];
let seats = [];
let target = 121;
let idx = 0;

// syncBoard moves the pegs to a frame's cumulative scores. A single step forward
// is animated via board.score(); any other move (back, jump, scrub) snaps via
// reset()+setPegs(), so the board always matches the frame exactly.
function syncBoard(scores, animateForward) {
  if (animateForward) {
    const cur = board.getScore(); // [front0, front1]
    const d0 = scores[0] - cur[0];
    const d1 = scores[1] - cur[1];
    if (d0 < 0 || d1 < 0) { snapBoard(scores); return; }
    if (d0 > 0) board.score(0, d0);
    if (d1 > 0) board.score(1, d1);
    return;
  }
  snapBoard(scores);
}
function snapBoard(scores) {
  board.reset();
  board.setPegs(boardTrack(0), { front: scores[0], back: scores[0] });
  board.setPegs(boardTrack(1), { front: scores[1], back: scores[1] });
}

// Controls, built once and kept in refs so render() only updates them.
let elPrev, elNext, elSelect, elScrub, elStep, elBoardWrap;

function buildControls() {
  elPrev = h('button', { class: 'btn', type: 'button', onclick: () => go(idx - 1, false) }, '◂ Prev');
  elNext = h('button', { class: 'btn btn-primary', type: 'button', onclick: () => go(idx + 1, true) }, 'Next ▸');
  elSelect = h('select', { class: 'replay-jump', 'aria-label': 'Jump to hand',
    onchange: (e) => go(Number(e.target.value), false) });
  for (const s of starts) elSelect.append(h('option', { value: String(s.index) }, `Hand ${s.hand}`));
  elScrub = h('input', { type: 'range', class: 'replay-scrub', min: '0', max: String(frames.length - 1),
    step: '1', value: '0', 'aria-label': 'Scrub through the game',
    oninput: (e) => go(Number(e.target.value), false) });
  elStep = h('div', { class: 'replay-label' });
  return h('div', { class: 'replay-controls' },
    elPrev, elNext,
    h('label', { class: 'replay-jump-wrap' }, 'Hand ', elSelect),
    elScrub, elStep);
}

// go moves to a frame. animateForward is honoured only for an exact single step
// forward (the Next button / ArrowRight); everything else snaps.
function go(target_, animateForward) {
  const next = Math.max(0, Math.min(frames.length - 1, target_));
  const single = animateForward && next === idx + 1;
  idx = next;
  render(single);
}

function render(animateForward) {
  const frame = frames[idx];
  syncBoard(frame.scores, animateForward);

  // Controls state.
  elPrev.disabled = idx === 0;
  elNext.disabled = idx === frames.length - 1;
  elScrub.value = String(idx);
  elStep.textContent = `${frame.label}  ·  ${idx + 1} / ${frames.length}`;
  // Reflect the current hand in the jump select (without re-firing onchange).
  let cur = 0;
  for (const s of starts) if (s.index <= idx) cur = s.index;
  if (elSelect.value !== String(cur)) elSelect.value = String(cur);

  // The table.
  const top = seatEl(0, frame, 'opp');
  const bottom = seatEl(1, frame, 'you');
  const mid = h('div', { class: 'board-area' },
    h('div', { class: 'board-row' }, boardMount),
    countEl(frame));
  elBoardWrap.replaceChildren(h('div', { class: 'table-inner' }, top, mid, bottom));
}

// Arrow-key stepping (ignored while focus is in the jump select / scrub, where the
// arrows have their own native meaning).
function onKey(e) {
  const tag = (e.target && e.target.tagName) || '';
  if (tag === 'SELECT' || tag === 'INPUT') return;
  if (e.key === 'ArrowRight') { e.preventDefault(); go(idx + 1, true); }
  else if (e.key === 'ArrowLeft') { e.preventDefault(); go(idx - 1, false); }
}

function renderReplay(data) {
  seats = data.seats || [];
  target = data.target || 121;
  frames = buildFrames(data);
  starts = handStarts(frames);

  if (frames.length <= 1) {
    message('Nothing to replay', 'This finished game has no recorded moves to step through.');
    return;
  }

  const nameOf = (i) => (seats[i] && seats[i].name) || `Seat ${i + 1}`;
  const winnerName = data.winner != null ? nameOf(data.winner) : null;
  const sub = winnerName
    ? `${nameOf(0)} vs ${nameOf(1)} — ${winnerName} won (to ${target}).`
    : `${nameOf(0)} vs ${nameOf(1)}.`;

  elBoardWrap = h('div', { class: 'felt replay-felt' });
  const controls = buildControls();

  root.replaceChildren(
    h('h1', { class: 'an-title' }, 'Game replay'),
    h('p', { class: 'an-subtitle' }, sub),
    controls,
    elBoardWrap);

  idx = 0;
  render(false);

  window.removeEventListener('keydown', onKey);
  window.addEventListener('keydown', onKey);
}

async function load() {
  if (!gameId) {
    message('No game selected', 'Open this page from a finished game to replay it.');
    return;
  }
  message('Loading replay…', '');
  let r;
  try {
    r = await fetch(`/users/me/games/${encodeURIComponent(gameId)}/replay`);
  } catch {
    message('Could not load replay', 'There was a network problem reaching the server.',
      h('button', { class: 'btn btn-primary', onclick: load }, 'Try again'));
    return;
  }
  if (r.status === 401) {
    message('Log in to view replays', 'Game replays are only available for your own finished games. Use Login in the header above, then reopen this page.');
    return;
  }
  if (r.status === 404) {
    message('Replay not available', "We couldn't find a finished game of yours with this id. Replays are only available for your own completed games.");
    return;
  }
  if (!r.ok) {
    message('Could not load replay', 'The server returned an unexpected error.',
      h('button', { class: 'btn btn-primary', onclick: load }, 'Try again'));
    return;
  }
  let data;
  try {
    data = await r.json();
  } catch {
    message('Could not load replay', 'The replay response was malformed.',
      h('button', { class: 'btn btn-primary', onclick: load }, 'Try again'));
    return;
  }
  renderReplay(data);
}

load();
