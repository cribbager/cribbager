// Unified post-game replay + analysis (A10) — "Evaluate game". ONE page that
// steps through a finished game move-by-move with FULL visibility (the replay
// board, folded from the event log by replayFrames.js) AND shows every graded
// decision beside it: a right rail lists each deal with its starting scores
// and per-deal ✓/✗ marks (discard / pegging optimal under the selected
// engine), and the deal being viewed reveals its decision details — the
// discard (your throw vs the engine's best, with the expected-value swing)
// and every non-forced pegging decision.
//
// Data: GET /games/{id}/replay (the event log) + GET /games/{id}/analysis
// (multi-engine verdicts; contract in internal/server/analysis2.go). ONE
// analysis payload carries ALL engines' verdicts in aligned arrays, so the
// engine selector switches verdicts instantly, client-side — no refetch, no
// URL params. Both endpoints accept a per-game player token (Bearer) OR the
// login cookie, so the page works for GUESTS straight off the game-over
// screen: main.js stashes the token by game id at game over, and we present
// it here; the server falls back to the login session when the token no
// longer opens a seat (e.g. the live session was reaped but the game is in
// the signed-in user's history).
//
// The analysis half degrades gracefully: if it can't load, the replay still
// renders and the rail says so. The replay half is the page — without it we
// show the error state.
import { createBoard, straightBoard } from '../board/board.js';
import { cardFace } from './cardFace.js';
import { mountHeader } from './header.js';
import { buildFrames, handStarts } from './replayFrames.js';
import { parseCard, sortCards, RANK_LABELS, SUIT_SYMBOLS } from '../engine/cards.js';
import {
    engineLabel, defaultEngineIndex, formatDelta, formatValue,
    dealsByIndex, dealMarks, summaryLines, frameIndexForDeal,
} from './evaluateModel.js';

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

const root = document.getElementById('analyze');
mountHeader();

// gameId comes from ?game=<id>; without it there is nothing to evaluate.
const gameId = new URLSearchParams(location.search).get('game');

// ---- auth: the per-game player token main.js stashed at game over ----------
// This is the guest path: the same credential that authorized the player's
// moves also opens the post-game endpoints. Logged-in users need no token —
// the login cookie serves their history forever.
const EVAL_TOKENS_KEY = 'cribbager:eval-tokens';
function playerTokenFor(id) {
    try {
        const entry = JSON.parse(localStorage.getItem(EVAL_TOKENS_KEY) || '{}')[id];
        return (entry && entry.token) || null;
    } catch { return null; }
}

// authedFetch sends the stashed player token (when we have one) as Bearer;
// cookies ride along either way, and the server tries token-then-login.
function authedFetch(path) {
    const token = gameId && playerTokenFor(gameId);
    return fetch(path, token ? { headers: { Authorization: 'Bearer ' + token } } : undefined);
}

// ---- shared little renderers ------------------------------------------------
const isRed = (c) => c.suit === 1 || c.suit === 2; // diamonds, hearts
// A compact, suit-coloured rank+suit chip (a full card face would be heavy).
function cardLabel(c) {
    return h('span', { class: 'an-chip ' + (isRed(c) ? 'red' : 'black') },
        RANK_LABELS[c.rank] + SUIT_SYMBOLS[c.suit]);
}
const chips = (codes) => sortCards((codes || []).map(parseCard)).map(cardLabel);
const chipsInOrder = (codes) => (codes || []).map(parseCard).map(cardLabel);

// message renders a single centered notice (loading / error / empty states).
function message(title, body, action) {
    root.replaceChildren(
        h('h1', { class: 'an-title' }, 'Evaluate game'),
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
// Seat 0 rides the TOP board track (0); seat 1 rides the BOTTOM track (1).
// There is no "me" here — both seats are shown plainly, like a spectator.
const boardTrack = (seat) => seat;

// ---- state ----
let frames = [];
let starts = [];
let seats = [];
let target = 121;
let idx = 0;
// The analysis payload (null when unavailable), the reason it is unavailable,
// the selected engine index, and the analyzed deals keyed by 0-based deal.
let analysis = null;
let analysisNote = '';
let engineIdx = 0;
let dealByIdx = {};

// Unnamed seats follow the live roster's convention: a nameless human seat is
// "Anonymous" (a guest), a nameless bot seat is just "Bot".
const nameOf = (i) => (seats[i] && seats[i].name) || (seats[i] && seats[i].bot ? 'Bot' : 'Anonymous');

// ---- the replay table (face-up everywhere — a spectator view) --------------
function handRow(seat, frame) {
    return h('div', { class: 'hand' }, ...sortCards(frame.hands[seat]).map((c) => cardFace(c)));
}

// The dealer's deck: the crib (face-up) and the starter, pushed to the right
// of the pegging row (matching the live table's .deck placement).
function deckGroup(seat, frame) {
    if (frame.dealer !== seat) return null;
    const kids = [h('span', { class: 'crib-tag' }, 'crib')];
    if (frame.crib.length) for (const c of sortCards(frame.crib)) kids.push(cardFace(c, { small: true }));
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

// The show rows (counted hand + starter + total, then the breakdown), reusing
// the live table's .show-* markup.
function showRows(show) {
    const cards = sortCards(show.hand).map((c) => cardFace(c));
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
// pegging row); seat 1 (bottom) mirrors "you". When that seat is being
// counted, its show rows replace the hand/pegging.
function seatEl(seat, frame, posClass) {
    const sh = frame.show;
    const rows = sh && sh.seat === seat
        ? showRows(sh)
        : seat === 0
            ? [handRow(seat, frame), peggingRow(seat, frame)]
            : [peggingRow(seat, frame), handRow(seat, frame)];
    const dealerTag = frame.dealer === seat ? h('span', { class: 'replay-dealer-tag' }, 'dealer') : null;
    return h('div', { class: 'seat ' + posClass },
        h('div', { class: 'rail-label' }, nameOf(seat), dealerTag),
        ...rows);
}

// The running pegging count, shown between the seats during play.
function countEl(frame) {
    if (frame.phase !== 'play') return null;
    return h('div', { class: 'replay-count' }, 'Count ', h('strong', {}, String(frame.count)));
}

// syncBoard moves the pegs to a frame's cumulative scores. A single step
// forward is animated via board.score(); any other move (back, jump, scrub)
// snaps via reset()+setPegs(), so the board always matches the frame exactly.
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

// ---- the right rail: engines, summary, deals, decisions ---------------------

// agreeMark is the divergence badge: the owner wants to SEE where the engines
// split, so any decision (or deal) where they disagree carries this marker.
const agreeMark = () => h('span', { class: 'ev-disagree', title: 'The engines pick different moves here' }, 'engines disagree');

// verdictBadge renders one engine verdict as ✓-optimal or the swing.
function verdictBadge(v) {
    const delta = formatDelta(v.delta, v.units);
    return v.optimal || !delta
        ? h('span', { class: 'an-badge ok' }, '✓ optimal')
        : h('span', { class: 'an-badge off', title: `yours ${formatValue(v.chosen_value, v.units)} vs best ${formatValue(v.best_value, v.units)}` }, delta);
}

// engineSwitch is the segmented control (same idiom as the practice pages'
// .pr-seg). Switching re-renders the rail from the SAME payload — the arrays
// are aligned by engine index, so this is instant.
function engineSwitch() {
    return h('div', { class: 'ev-engines' },
        h('span', { class: 'ev-rail-label' }, 'Engine'),
        h('div', { class: 'pr-seg-group' },
            ...analysis.engines.map((e, i) =>
                h('button', {
                    class: 'pr-seg' + (i === engineIdx ? ' is-on' : ''),
                    type: 'button',
                    title: `${engineLabel(e)} v${e.version}`,
                    onclick: () => { engineIdx = i; renderRail(); },
                }, engineLabel(e)))));
}

// discardDetail renders the deal's discard decision under the selected
// engine: the dealt six (throw dimmed), your line, and — when sub-optimal —
// the engine's preferred split.
function discardDetail(deal) {
    const d = deal.discard;
    const v = d.engines[engineIdx];
    const thrown = (d.throw || []).map(parseCard);
    const isThrown = (c) => thrown.some((t) => t.rank === c.rank && t.suit === c.suit);
    const six = h('div', { class: 'an-hand-cards' },
        sortCards((d.hand || []).map(parseCard)).map((c) =>
            cardFace(c, { small: true, extra: isThrown(c) ? 'an-thrown' : 'an-kept' })));

    const head = h('div', { class: 'ev-decision-head' },
        h('span', { class: 'ev-decision-label' }, 'Discard'),
        h('span', { class: 'an-crib' }, deal.dealer === analysis.seat ? 'your crib' : "opponent's crib"),
        verdictBadge(v));

    const yours = h('div', { class: 'an-line' },
        h('span', { class: 'an-line-label' }, 'You threw '),
        ...chips(d.throw),
        h('span', { class: 'an-line-sep' }, ' — kept '),
        ...chips(d.keep));

    const lines = [yours];
    if (!v.optimal) {
        lines.push(h('div', { class: 'an-line an-line-engine' },
            h('span', { class: 'an-line-label' }, 'Engine: throw '),
            ...chips(v.best_throw),
            h('span', { class: 'an-line-sep' }, ' — keep '),
            ...chips(v.best_keep)));
    }
    if (d.agree === false) lines.push(agreeMark());
    return h('div', { class: 'ev-decision' + (v.optimal ? ' is-optimal' : '') }, head, six, ...lines);
}

// playDetail renders one non-forced pegging decision under the selected
// engine: the series so far (count), what was played, and the engine's pick
// when it differs. Suits are pegging-equivalent, so a same-rank "best" is the
// same move — the server's delta/optimal flags already encode that.
function playDetail(p, i) {
    const v = p.engines[engineIdx];
    const head = h('div', { class: 'ev-decision-head' },
        h('span', { class: 'ev-decision-label' }, `Pegging ${i + 1}`),
        h('span', { class: 'ev-count' }, `count ${p.count}`),
        verdictBadge(v));
    const played = h('div', { class: 'an-line' },
        h('span', { class: 'an-line-label' }, p.pile && p.pile.length ? 'On ' : 'Led '),
        ...(p.pile && p.pile.length ? [...chipsInOrder(p.pile), h('span', { class: 'an-line-sep' }, ' you played ')] : []),
        cardLabel(parseCard(p.played)),
        h('span', { class: 'an-line-sep' }, ' holding '),
        ...chips(p.hand));
    const lines = [played];
    if (!v.optimal) {
        lines.push(h('div', { class: 'an-line an-line-engine' },
            h('span', { class: 'an-line-label' }, 'Engine: play '),
            cardLabel(parseCard(v.best))));
    }
    if (p.agree === false) lines.push(agreeMark());
    return h('div', { class: 'ev-decision' + (v.optimal ? ' is-optimal' : '') }, head, ...lines);
}

// markEl is one ✓/✗ in a deal row: green check when every decision of that
// kind was optimal under the selected engine, red cross otherwise. A deal
// with no graded pegging decision shows a muted dash instead of a vacuous ✓.
function markEl(kind, ok, vacuous) {
    if (vacuous) return h('span', { class: 'ev-mark none', title: `no ${kind} decisions` }, kind + ' –');
    return h('span', { class: 'ev-mark ' + (ok ? 'ok' : 'bad'), title: `${kind} ${ok ? 'optimal' : 'not optimal'}` },
        kind + ' ' + (ok ? '✓' : '✗'));
}

// dealRow is one entry in the rail's deal list: "Deal N · s0–s1" plus the two
// ✓/✗ marks and the divergence dot. Clicking it jumps the replay to the
// deal's first frame, which also expands its decision details (the expanded
// deal always follows the frame being viewed). `startScores` comes from the
// replay frame, so even a deal the analysis skipped still lists correctly.
function dealRow(handNo, startScores, current) {
    const dealIdx = handNo - 1;
    const deal = dealByIdx[dealIdx];
    const row = h('button', {
        class: 'ev-deal' + (current ? ' is-current' : ''),
        type: 'button',
        onclick: () => { const f = frameIndexForDeal(starts, dealIdx); if (f != null) go(f, false); },
    },
    h('span', { class: 'ev-deal-no' }, `Deal ${handNo}`),
    h('span', { class: 'ev-deal-scores' }, `${startScores[0]}–${startScores[1]}`),
    ...(deal
        ? (() => {
            const m = dealMarks(deal, engineIdx);
            return [
                markEl('discard', m.discard, false),
                markEl('pegging', m.pegging, m.playCount === 0),
                m.disagree ? h('span', { class: 'ev-disagree-dot', title: 'The engines disagree somewhere in this deal' }, '≠') : null,
            ];
        })()
        : [h('span', { class: 'ev-mark none' }, 'not graded')]));

    if (!current || !deal) return [row];
    // The viewed deal reveals its decisions inline, under its row.
    return [row, h('div', { class: 'ev-decisions' },
        discardDetail(deal),
        ...deal.plays.map((p, i) => playDetail(p, i)),
        deal.plays.length === 0 ? h('div', { class: 'ev-no-plays' }, 'No pegging decisions — every play was forced.') : null)];
}

// renderRail rebuilds the right rail: engine switch, the selected engine's
// game summary, and the deal list with the viewed deal expanded. Cheap enough
// to run on every frame change / engine switch.
let elRail;
function renderRail() {
    if (!elRail) return;
    if (!analysis) {
        elRail.replaceChildren(h('div', { class: 'panel ev-rail-note' },
            h('p', { class: 'an-message-title' }, 'Analysis unavailable'),
            h('p', { class: 'an-message-body' }, analysisNote || 'The decision analysis for this game could not be loaded; the replay still works.')));
        return;
    }
    const curDeal = frames[idx].hand - 1; // 0-based deal being viewed (-1 pre-deal)
    const rows = [];
    for (const s of starts) {
        const startScores = frames[s.index].scores; // dealing scores nothing: this IS the deal's start
        rows.push(...dealRow(s.hand, startScores, s.hand - 1 === curDeal));
    }
    elRail.replaceChildren(
        engineSwitch(),
        h('div', { class: 'panel ev-summary' },
            ...summaryLines(analysis.summary[engineIdx]).map((l) => h('div', { class: 'ev-summary-line' }, l))),
        h('div', { class: 'panel ev-deals' }, ...rows));
}

// ---- replay controls ----
let elPrev, elNext, elSelect, elScrub, elStep, elBoardWrap;

function buildControls() {
    elPrev = h('button', { class: 'btn', type: 'button', onclick: () => go(idx - 1, false) }, '◂ Prev');
    elNext = h('button', { class: 'btn btn-primary', type: 'button', onclick: () => go(idx + 1, true) }, 'Next ▸');
    elSelect = h('select', { class: 'replay-jump', 'aria-label': 'Jump to deal',
        onchange: (e) => go(Number(e.target.value), false) });
    for (const s of starts) elSelect.append(h('option', { value: String(s.index) }, `Deal ${s.hand}`));
    elScrub = h('input', { type: 'range', class: 'replay-scrub', min: '0', max: String(frames.length - 1),
        step: '1', value: '0', 'aria-label': 'Scrub through the game',
        oninput: (e) => go(Number(e.target.value), false) });
    elStep = h('div', { class: 'replay-label' });
    return h('div', { class: 'replay-controls' },
        elPrev, elNext,
        h('label', { class: 'replay-jump-wrap' }, 'Deal ', elSelect),
        elScrub, elStep);
}

// go moves to a frame. animateForward is honoured only for an exact single
// step forward (the Next button / ArrowRight); everything else snaps.
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
    // Reflect the current deal in the jump select (without re-firing onchange).
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

    // The rail's expanded deal follows the frame being viewed.
    renderRail();
}

// Arrow-key stepping (ignored while focus is in the jump select / scrub,
// where the arrows have their own native meaning).
function onKey(e) {
    const tag = (e.target && e.target.tagName) || '';
    if (tag === 'SELECT' || tag === 'INPUT') return;
    if (e.key === 'ArrowRight') { e.preventDefault(); go(idx + 1, true); }
    else if (e.key === 'ArrowLeft') { e.preventDefault(); go(idx - 1, false); }
}

function renderPage(replayData) {
    seats = replayData.seats || [];
    target = replayData.target || 121;
    frames = buildFrames(replayData);
    starts = handStarts(frames);
    dealByIdx = dealsByIndex(analysis);
    if (analysis) engineIdx = defaultEngineIndex(analysis.engines);

    if (frames.length <= 1) {
        message('Nothing to evaluate', 'This finished game has no recorded moves to step through.');
        return;
    }

    const winnerName = replayData.winner != null ? nameOf(replayData.winner) : null;
    const sub = winnerName
        ? `${nameOf(0)} vs ${nameOf(1)} — ${winnerName} won (to ${target}).`
        : `${nameOf(0)} vs ${nameOf(1)}.`;

    elBoardWrap = h('div', { class: 'felt replay-felt' });
    elRail = h('div', { class: 'ev-rail' });
    const controls = buildControls();

    root.replaceChildren(
        h('h1', { class: 'an-title' }, 'Evaluate game'),
        h('p', { class: 'an-subtitle' }, sub),
        controls,
        h('div', { class: 'ev-layout' },
            h('div', { class: 'ev-main' }, elBoardWrap),
            elRail));

    // Open on the first deal (frame 0 is the empty pre-game table).
    idx = starts.length ? starts[0].index : 0;
    render(false);

    window.removeEventListener('keydown', onKey);
    window.addEventListener('keydown', onKey);
}

// fetchAnalysis loads the multi-engine verdicts. It DEGRADES GRACEFULLY: any
// failure resolves to null (with a human note) and the replay renders alone.
async function fetchAnalysis(id) {
    try {
        const r = await authedFetch(`/games/${encodeURIComponent(id)}/analysis`);
        if (!r.ok) {
            analysisNote = r.status === 409 ? 'The game has not finished yet.' : '';
            return null;
        }
        return await r.json();
    } catch {
        return null;
    }
}

async function load() {
    if (!gameId) {
        message('No game selected', 'Open this page from a finished game — the game-over screen or your profile — to evaluate it.');
        return;
    }
    message('Loading game…', '');
    // Both halves in parallel; the analysis result is folded in at render.
    const analysisP = fetchAnalysis(gameId);
    let r;
    try {
        r = await authedFetch(`/games/${encodeURIComponent(gameId)}/replay`);
    } catch {
        message('Could not load the game', 'There was a network problem reaching the server.',
            h('button', { class: 'btn btn-primary', onclick: load }, 'Try again'));
        return;
    }
    if (r.status === 401) {
        message('This game needs a sign-in', 'Open the evaluation right from the game-over screen, or log in (header above) to evaluate any finished game in your history.');
        return;
    }
    if (r.status === 404) {
        message('Game not available', "We couldn't find a finished game of yours with this id. Guest games are only evaluable for a while after they end; sign in to keep yours forever.");
        return;
    }
    if (r.status === 409) {
        message('Still in progress', 'Evaluation is available once the game finishes.');
        return;
    }
    if (!r.ok) {
        message('Could not load the game', 'The server returned an unexpected error.',
            h('button', { class: 'btn btn-primary', onclick: load }, 'Try again'));
        return;
    }
    let replayData;
    try {
        replayData = await r.json();
    } catch {
        message('Could not load the game', 'The replay response was malformed.',
            h('button', { class: 'btn btn-primary', onclick: load }, 'Try again'));
        return;
    }
    analysis = await analysisP; // null on any failure → replay without verdicts
    renderPage(replayData);
}

load();
