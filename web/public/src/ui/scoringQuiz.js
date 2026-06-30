// Scoring quiz (A6) — a standalone practice surface, NOT a live game. You are
// dealt four cards plus a starter (the cut, shown set apart and labelled), and
// you count the show yourself: select the cards forming each combo, declare what
// it is and what it's worth, then enter your own total and submit. Grading runs
// the engine via POST /tools/score-hand (scoring/hand.Score on the server) — no
// scoring is reimplemented here. The verdict is rendered in place over the list
// you built: your total is marked Correct/Wrong against the engine, declarations
// that don't match an engine combo are crossed out, and combos you missed are
// appended. This is exactly where teaching belongs — official games carry no
// in-game coaching.

import { mountHeader } from './header.js';
import { cardFace } from './cardFace.js';
import { parseCard, cardCode, sortCards } from '../engine/cards.js';

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

// The combo kinds the user can declare — exactly the simple scoring elements.
// TODO: add guidance for double runs and three/four-of-a-kind; until then those
// stay out of the picker and the engine's bundled run combos surface as "missed".
const KINDS = [
    { id: 'fifteen', label: 'Fifteen' },
    { id: 'pair', label: 'Pair' },
    { id: 'run', label: 'Run' },
    { id: 'nobs', label: 'Nobs' },
    { id: 'flush', label: 'Flush' },
];
// How each kind is called aloud in the rendered "[cards] <call> for N" line.
const CALL = { fifteen: 'fifteen', pair: 'pair', run: 'run', nobs: 'nobs', flush: 'flush' };
const callWord = (kind) => CALL[kind] || kind;

// dealShow returns five distinct random cards by shuffling a fresh 52-card deck:
// the first four are the hand (sorted for display), the fifth is the starter.
function dealShow() {
    const deck = [];
    for (let rank = 1; rank <= 13; rank++) {
        for (let suit = 0; suit < 4; suit++) deck.push({ rank, suit });
    }
    for (let i = deck.length - 1; i > 0; i--) {
        const j = Math.floor(Math.random() * (i + 1));
        [deck[i], deck[j]] = [deck[j], deck[i]];
    }
    return [...sortCards(deck.slice(0, 4)), deck[4]];
}

// --- view state ---------------------------------------------------------------
// Cards are indexed 0..4 across the show; index 4 is the starter (the cut).
const STARTER = 4;
const state = {
    cards: [],            // five {rank, suit}: [0..3] sorted hand, [4] starter
    selected: new Set(),  // indices into cards currently picked for a declaration
    kind: 'fifteen',      // the kind for the next declaration
    points: '',           // the points the user claims for the next declaration
    total: '',            // the user's own total count, graded against the engine
    declarations: [],     // [{ kind, idxs:[...], points:int }]
    result: null,         // graded verdict { total, declTotal, declared:[...], missed:[...] }, or null
    busy: false,          // a /tools/score-hand request is in flight
    builderError: null,   // inline reason an "Add to score" was rejected, or null
    error: null,          // a friendly request-level error message, or null
};

function newDeal() {
    state.cards = dealShow();
    state.selected = new Set();
    state.kind = 'fifteen';
    state.points = '';
    state.total = '';
    state.declarations = [];
    state.result = null;
    state.busy = false;
    state.builderError = null;
    state.error = null;
    render();
}

function toggleSelect(i) {
    if (state.result) return; // locked once graded; deal again to retry
    if (state.selected.has(i)) state.selected.delete(i);
    else state.selected.add(i);
    state.builderError = null;
    render();
}

// addDeclaration commits the current selection + kind + points as one combo.
// Validation lives here (not in a disabled button) so the action never silently
// no-ops: invalid input surfaces a brief inline reason instead.
function addDeclaration() {
    const pts = parseInt(state.points, 10);
    if (state.selected.size === 0) {
        state.builderError = 'Select the card(s) forming the combo first.';
        render();
        return;
    }
    if (!Number.isFinite(pts) || pts < 1) {
        state.builderError = 'Enter the points for this combo.';
        render();
        return;
    }
    state.declarations.push({ kind: state.kind, idxs: [...state.selected].sort((a, b) => a - b), points: pts });
    state.selected = new Set();
    state.points = '';
    state.builderError = null;
    render();
}

function removeDeclaration(idx) {
    if (state.result) return;
    state.declarations.splice(idx, 1);
    render();
}

// cardKey is an order-independent identity for a set of cards (their wire codes,
// sorted) used to match a declaration against an engine combo.
const cardKey = (cards) => cards.map(cardCode).sort().join(',');

async function submit() {
    if (state.busy || state.result) return;
    const total = parseInt(state.total, 10);
    if (!Number.isFinite(total) || total < 0) {
        state.error = 'Enter your total count before submitting.';
        render();
        return;
    }

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
    state.result = grade(total, data.combos || [], data.total || 0);
    render();
}

// grade matches each declaration to an engine combo by kind + card-set equality
// (order-independent), EXCEPT nobs: a nobs declaration is lenient — it's correct
// whenever the engine reports a nobs at all, regardless of which card(s) the user
// picked (Jack, Jack+starter, or starter), since nobs attribution is ambiguous.
function grade(total, rawCombos, engineTotal) {
    const engine = rawCombos.map((c) => ({
        kind: c.kind,
        points: c.points,
        cards: (c.cards || []).map(parseCard),
        used: false,
    }));

    const declared = state.declarations.map((d) => {
        const cards = d.idxs.map((idx) => state.cards[idx]);
        let match;
        if (d.kind === 'nobs') {
            match = engine.find((e) => !e.used && e.kind === 'nobs');
        } else {
            const key = cardKey(cards);
            match = engine.find((e) => !e.used && e.kind === d.kind && cardKey(e.cards) === key);
        }
        if (match) match.used = true;
        return { kind: d.kind, cards, points: d.points, correct: !!match };
    });

    const missed = engine.filter((e) => !e.used)
        .map((e) => ({ kind: e.kind, cards: e.cards, points: e.points }));

    return { total: engineTotal, declTotal: total, declared, missed };
}

// --- rendering ----------------------------------------------------------------
function renderShow() {
    const cards = state.cards.map((c, i) => {
        const sel = state.selected.has(i);
        const locked = !!state.result;
        const isStarter = i === STARTER;
        const extra = locked ? '' : 'selectable' + (sel ? ' selected' : '');
        const face = cardFace(c, { extra: extra.trim() });
        if (!locked) {
            face.setAttribute('role', 'button');
            face.setAttribute('aria-pressed', sel ? 'true' : 'false');
            face.setAttribute('tabindex', '0');
            face.addEventListener('click', () => toggleSelect(i));
            face.addEventListener('keydown', (e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); toggleSelect(i); } });
        }
        const wrap = h('div', { class: 'sq-card-slot' + (isStarter ? ' is-starter' : '') }, face);
        if (isStarter) wrap.append(h('span', { class: 'sq-starter-tag' }, 'Starter'));
        return wrap;
    });
    return h('div', { class: 'sq-show' }, ...cards);
}

function renderControls() {
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

    const addBtn = h('button', { class: 'btn', type: 'button', onclick: addDeclaration }, 'Add to score');

    const row = h('div', { class: 'sq-controls' }, kindSel, ptsInput, addBtn);
    return state.builderError
        ? h('div', { class: 'sq-builder' }, row, h('span', { class: 'sq-builder-error' }, state.builderError))
        : row;
}

// comboLine renders one combo as real card faces followed by how it's called:
// "[faces] fifteen for 2". opts.wrong crosses it out; opts.missed marks it as one
// the user didn't declare.
function comboLine(cards, kind, points, opts = {}) {
    const cls = 'sq-combo'
        + (opts.wrong ? ' is-wrong' : '')
        + (opts.missed ? ' is-missed' : '');
    const kids = [
        h('span', { class: 'sq-combo-cards' }, ...sortCards(cards).map((c) => cardFace(c, { small: true }))),
        h('span', { class: 'sq-combo-call' }, `${callWord(kind)} for ${points}`),
    ];
    if (opts.missed) kids.push(h('span', { class: 'sq-combo-tag' }, 'missed'));
    if (opts.onRemove) {
        kids.push(h('button', {
            class: 'sq-decl-remove', type: 'button', 'aria-label': 'Remove this combo',
            onclick: opts.onRemove,
        }, '✕'));
    }
    return h('div', { class: cls }, ...kids);
}

// renderDeclared lists the combos inline (no panel, no heading). Before grading
// it's the editable declaration list; after grading the same list is mutated in
// place — wrong combos crossed out, missed combos appended.
function renderDeclared() {
    const rows = [];

    if (state.result) {
        for (const d of state.result.declared) {
            rows.push(comboLine(d.cards, d.kind, d.points, { wrong: !d.correct }));
        }
        for (const m of state.result.missed) {
            rows.push(comboLine(m.cards, m.kind, m.points, { missed: true }));
        }
    } else {
        state.declarations.forEach((d, i) => {
            const cards = d.idxs.map((idx) => state.cards[idx]);
            rows.push(comboLine(cards, d.kind, d.points, { onRemove: () => removeDeclaration(i) }));
        });
    }

    if (!rows.length) {
        return h('p', { class: 'sq-empty' }, 'No combos declared yet.');
    }
    return h('div', { class: 'sq-decl-list' }, ...rows);
}

function renderVerdict() {
    const { total, declTotal } = state.result;
    const correct = declTotal === total;
    const badge = correct
        ? h('span', { class: 'pr-badge ok' }, '✓ correct')
        : h('span', { class: 'pr-badge off' }, '✗ wrong');
    const headline = correct
        ? `Correct — the hand scores ${total}.`
        : `Wrong — you counted ${declTotal}, the hand scores ${total}.`;
    return h('div', { class: 'sq-verdict' },
        h('span', { class: 'sq-verdict-text' }, headline),
        badge);
}

function renderActions() {
    if (state.result) {
        return h('div', { class: 'sq-actions' },
            h('button', { class: 'btn btn-primary', type: 'button', onclick: newDeal }, 'Deal again'));
    }
    const totalInput = h('input', {
        type: 'number', min: '0', class: 'input sq-total-input', placeholder: 'Total',
        'aria-label': 'Your total count', value: state.total,
        oninput: (e) => { state.total = e.target.value; },
        onkeydown: (e) => { if (e.key === 'Enter') { e.preventDefault(); submit(); } },
    });
    const submitBtn = h('button', {
        class: 'btn btn-primary', type: 'button',
        ...(state.busy ? { disabled: 'disabled' } : {}),
        onclick: submit,
    }, state.busy ? 'Scoring…' : 'Submit count');
    return h('div', { class: 'sq-actions' }, totalInput, submitBtn);
}

function render() {
    const board = [renderShow()];
    if (!state.result) board.push(renderControls());
    if (state.result) board.push(renderVerdict());
    board.push(renderDeclared(), renderActions());

    const kids = [
        h('h1', { class: 'pr-title' }, 'Scoring quiz'),
        h('p', { class: 'pr-subtitle' }, 'You’re dealt four cards and a starter. Count the show, then check your total against the engine.'),
        h('div', { class: 'panel pr-board' }, ...board),
    ];
    if (state.error) {
        kids.push(h('div', { class: 'panel pr-message' },
            h('p', { class: 'pr-message-body' }, state.error)));
    }
    root.replaceChildren(...kids);
}

newDeal();
