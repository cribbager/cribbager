// Scoring quiz (A6) — a standalone practice surface, NOT a live game. You are
// dealt four cards plus a starter (the cut, shown distinguished), and you count
// the show yourself: select the cards forming each combo, declare what it is and
// what it's worth, and build up your total in the scoring area. Submit grades
// your declared total against the engine and reveals the full breakdown, so you
// learn what you missed. This is exactly where teaching belongs — official games
// carry no in-game coaching.
//
// A combined combo is declared as ONE group: a double run is a single selection
// of all its cards, not a run plus a separately-marked pair. Grading reuses the
// engine itself via POST /tools/score-hand (scoring/hand.Score on the server); no
// scoring is reimplemented here — the user supplies the count, the engine the
// truth.

import { mountHeader } from './header.js';
import { cardFace } from './cardFace.js';
import { parseCard, cardCode, RANK_LABELS, SUIT_SYMBOLS } from '../engine/cards.js';

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

const root = document.getElementById('scoring-quiz');
mountHeader();

// isRed mirrors cardFace's own suit colouring (diamonds, hearts).
const isRed = (c) => c.suit === 1 || c.suit === 2;

// The combo kinds the user can declare. Labels are presentation-only; the engine
// is the authority on points (the user supplies their own count to be graded).
const KINDS = [
    { id: 'fifteen', label: 'Fifteen' },
    { id: 'pair', label: 'Pair' },
    { id: 'run', label: 'Run' },
    { id: 'flush', label: 'Flush' },
    { id: 'nobs', label: 'His nobs' },
    { id: 'other', label: 'Other' },
];
const kindLabel = (id) => (KINDS.find((k) => k.id === id) || { label: id }).label;

// cardLabel is a compact rank+suit chip (suit-coloured), reused from the discard
// trainer's vocabulary for the verdict and scoring-area sentences.
function cardLabel(c) {
    return h('span', { class: 'pr-chip ' + (isRed(c) ? 'red' : 'black') },
        RANK_LABELS[c.rank] + SUIT_SYMBOLS[c.suit]);
}
const cardLabels = (cards) => cards.map(cardLabel);

// dealShow returns five distinct random cards by shuffling a fresh 52-card deck:
// the first four are the hand, the fifth is the starter (cut) card.
function dealShow() {
    const deck = [];
    for (let rank = 1; rank <= 13; rank++) {
        for (let suit = 0; suit < 4; suit++) deck.push({ rank, suit });
    }
    for (let i = deck.length - 1; i > 0; i--) {
        const j = Math.floor(Math.random() * (i + 1));
        [deck[i], deck[j]] = [deck[j], deck[i]];
    }
    return deck.slice(0, 5);
}

// --- view state ---------------------------------------------------------------
// Cards are indexed 0..4 across the show; index 4 is the starter (the cut).
const STARTER = 4;
const state = {
    cards: [],            // five {rank, suit}: [0..3] hand, [4] starter
    selected: new Set(),  // indices into cards currently picked for a declaration
    kind: 'fifteen',      // the kind for the next declaration
    points: '',           // the points the user claims for the next declaration
    declarations: [],     // [{ kind, idxs:[...], points:int }]
    result: null,         // the engine's graded breakdown, or null before submit
    busy: false,          // a /tools/score-hand request is in flight
    error: null,          // a friendly error message, or null
};

function newDeal() {
    state.cards = dealShow();
    state.selected = new Set();
    state.kind = 'fifteen';
    state.points = '';
    state.declarations = [];
    state.result = null;
    state.error = null;
    state.busy = false;
    render();
}

function toggleSelect(i) {
    if (state.result) return; // locked once graded; deal again to retry
    if (state.selected.has(i)) state.selected.delete(i);
    else state.selected.add(i);
    render();
}

// addDeclaration commits the current selection + kind + points as one scored
// group (a double run is added as a single group of all its cards).
function addDeclaration() {
    const pts = parseInt(state.points, 10);
    if (state.selected.size === 0 || !Number.isFinite(pts) || pts < 1) return;
    state.declarations.push({ kind: state.kind, idxs: [...state.selected].sort((a, b) => a - b), points: pts });
    state.selected = new Set();
    state.points = '';
    render();
}

function removeDeclaration(idx) {
    if (state.result) return;
    state.declarations.splice(idx, 1);
    render();
}

const declaredTotal = () => state.declarations.reduce((sum, d) => sum + d.points, 0);

async function submit() {
    if (state.busy || state.result) return;
    state.busy = true;
    state.error = null;
    render();

    const hand = state.cards.slice(0, 4).map(cardCode);
    const starter = cardCode(state.cards[STARTER]);
    let r;
    try {
        r = await fetch('/tools/score-hand', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ hand, starter, crib: false }),
        });
    } catch {
        state.busy = false;
        state.error = 'There was a network problem reaching the engine. Check your connection and try again.';
        render();
        return;
    }
    if (!r.ok) {
        state.busy = false;
        state.error = 'The engine could not score that hand. Deal a new hand and try again.';
        render();
        return;
    }
    let data;
    try {
        data = await r.json();
    } catch {
        state.busy = false;
        state.error = 'The engine returned an unexpected response. Try again.';
        render();
        return;
    }

    state.busy = false;
    state.result = { total: data.total || 0, combos: data.combos || [], declared: declaredTotal() };
    render();
}

// --- rendering ----------------------------------------------------------------
function renderShow() {
    const cards = state.cards.map((c, i) => {
        const sel = state.selected.has(i);
        const locked = !!state.result;
        const extra = (i === STARTER ? 'sq-starter ' : '') + (locked ? '' : 'selectable' + (sel ? ' selected' : ''));
        const face = cardFace(c, { extra: extra.trim() });
        if (!locked) {
            face.setAttribute('role', 'button');
            face.setAttribute('aria-pressed', sel ? 'true' : 'false');
            face.setAttribute('tabindex', '0');
            face.addEventListener('click', () => toggleSelect(i));
            face.addEventListener('keydown', (e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); toggleSelect(i); } });
        }
        const wrap = h('div', { class: 'sq-card-slot' }, face);
        if (i === STARTER) wrap.append(h('span', { class: 'sq-starter-tag' }, 'Starter'));
        return wrap;
    });
    return h('div', { class: 'sq-show' }, ...cards);
}

function renderBuilder() {
    const kindSel = h('select', {
        class: 'input sq-kind', 'aria-label': 'Combo type',
        onchange: (e) => { state.kind = e.target.value; },
    }, ...KINDS.map((k) => h('option', { value: k.id, ...(k.id === state.kind ? { selected: 'selected' } : {}) }, k.label)));

    const ptsInput = h('input', {
        type: 'number', min: '1', class: 'input sq-points', placeholder: 'Points',
        'aria-label': 'Points for this combo', value: state.points,
        oninput: (e) => { state.points = e.target.value; },
        onkeydown: (e) => { if (e.key === 'Enter') { e.preventDefault(); addDeclaration(); } },
    });

    const n = state.selected.size;
    const canAdd = n > 0 && parseInt(state.points, 10) >= 1;
    const addBtn = h('button', {
        class: 'btn', type: 'button',
        ...(canAdd ? {} : { disabled: 'disabled' }),
        onclick: addDeclaration,
    }, 'Add to score');

    const hint = n === 0
        ? 'Select the cards forming a combo, choose its type, and enter the points.'
        : `${n} card${n === 1 ? '' : 's'} selected.`;

    return h('div', { class: 'sq-builder' },
        h('div', { class: 'sq-builder-row' }, kindSel, ptsInput, addBtn),
        h('span', { class: 'pr-hint' }, hint));
}

function renderScoringArea() {
    const items = state.declarations.map((d, i) => {
        const cards = d.idxs.map((idx) => state.cards[idx]);
        const row = h('div', { class: 'sq-decl' },
            h('span', { class: 'sq-decl-kind' }, kindLabel(d.kind)),
            h('span', { class: 'sq-decl-cards' }, ...cardLabels(cards)),
            h('span', { class: 'sq-decl-pts' }, `${d.points} pt${d.points === 1 ? '' : 's'}`));
        if (!state.result) {
            row.append(h('button', {
                class: 'sq-decl-remove', type: 'button', 'aria-label': 'Remove this combo',
                onclick: () => removeDeclaration(i),
            }, '✕'));
        }
        return row;
    });
    const body = items.length
        ? h('div', { class: 'sq-decl-list' }, ...items)
        : h('p', { class: 'pr-message-body' }, 'No combos declared yet.');
    return h('div', { class: 'sq-scoring' },
        h('div', { class: 'sq-scoring-head' },
            h('span', { class: 'sq-scoring-title' }, 'Scoring area'),
            h('span', { class: 'sq-total' }, `Your total: ${declaredTotal()}`)),
        body);
}

function renderActions() {
    if (state.result) {
        return h('div', { class: 'pr-actions' },
            h('button', { class: 'btn btn-primary', type: 'button', onclick: newDeal }, 'Deal again'));
    }
    const submitBtn = h('button', {
        class: 'btn btn-primary', type: 'button',
        ...(state.busy ? { disabled: 'disabled' } : {}),
        onclick: submit,
    }, state.busy ? 'Scoring…' : 'Submit count');
    return h('div', { class: 'pr-actions' },
        h('span', { class: 'pr-hint' }, 'Declare every combo, then submit your count.'),
        submitBtn);
}

// comboLabel renders one engine combo as a teachable line: a name (with run shape),
// its cards, and its points.
function comboLabel(combo) {
    let name = kindLabel(combo.kind);
    if (combo.kind === 'run') {
        const shape = combo.multiplicity === 2 ? 'Double run'
            : combo.multiplicity === 3 ? 'Triple run'
                : combo.multiplicity >= 4 ? 'Double-double run'
                    : `Run of ${combo.run_length}`;
        name = shape;
    }
    const cards = (combo.cards || []).map(parseCard);
    return h('div', { class: 'pr-line' },
        h('span', { class: 'pr-line-label' }, name + ' '),
        ...cardLabels(cards),
        h('span', { class: 'pr-ev' }, ` — ${combo.points} pt${combo.points === 1 ? '' : 's'}`));
}

function renderVerdict() {
    const { total, combos, declared } = state.result;
    const correct = declared === total;
    const delta = declared - total;

    const badge = correct
        ? h('span', { class: 'pr-badge ok' }, '✓ correct')
        : h('span', { class: 'pr-badge off' }, `${delta > 0 ? '+' : ''}${delta}`);

    const headline = correct
        ? `Correct — the hand scores ${total}.`
        : delta > 0
            ? `You declared ${declared}, but the hand scores ${total} (over by ${delta}).`
            : `You declared ${declared}, but the hand scores ${total} (missed ${-delta}).`;

    const kids = [
        h('div', { class: 'pr-verdict-head' },
            h('span', { class: 'pr-rank' }, headline),
            badge),
    ];

    if (combos.length) {
        kids.push(h('div', { class: 'sq-breakdown' }, ...combos.map(comboLabel)));
    } else {
        kids.push(h('p', { class: 'pr-verdict-note' }, 'This hand scores nothing — a clean zero.'));
    }
    kids.push(h('p', { class: 'pr-verdict-note' },
        'A double/triple run is one combo whose points already include the pairs it makes.'));

    return h('div', { class: 'panel pr-verdict' + (correct ? ' is-optimal' : '') }, ...kids);
}

function render() {
    const board = [renderShow()];
    if (!state.result) board.push(renderBuilder());
    board.push(renderScoringArea(), renderActions());

    const kids = [
        h('h1', { class: 'pr-title' }, 'Scoring quiz'),
        h('p', { class: 'pr-subtitle' }, 'You’re dealt four cards and a starter. Count the show: declare each combo and its points, then submit to check against the engine.'),
        h('div', { class: 'panel pr-board' }, ...board),
    ];
    if (state.error) {
        kids.push(h('div', { class: 'panel pr-message' },
            h('p', { class: 'pr-message-body' }, state.error),
            h('button', { class: 'btn btn-primary', type: 'button', onclick: submit }, 'Try again')));
    }
    if (state.result) kids.push(renderVerdict());
    root.replaceChildren(...kids);
}

newDeal();
