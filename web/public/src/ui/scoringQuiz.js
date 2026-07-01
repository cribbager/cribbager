// Hand Counting Tutorial (A6) — a standalone practice surface, NOT a live game. You are
// dealt four cards plus a starter (the cut, shown set apart and labelled), and
// you count the show yourself the way you'd count aloud: select the cards forming
// each combo, declare what it is, and enter the RUNNING COUNT so far (the
// cumulative total after that combo). Your total for grading is simply the
// running count of the last combo you declare. Grading runs the engine via POST
// /tools/score-hand (scoring/hand.Score on the server) — no scoring is
// reimplemented here. The verdict grades every line on BOTH its cards and its
// running count: a line reads Correct (green ✓) only when its cards match a
// distinct engine combo AND the running count you typed equals the correct running
// count at that point. The overall verdict is Correct only when every declared
// line is green and nothing is missed. Because the last correct line's running
// count is necessarily the engine total, all-green always means Correct — it is
// impossible to see every line ✓ yet an overall "Not quite". Teaching feedback is
// rendered in place: a line with the wrong cards is crossed out (it counts for
// nothing); a line with the right cards but a wrong running count is crossed out
// too, but shows the count it should read; combos you missed are appended; the
// correct/wrong graphic sits at the bottom.
// This is exactly where teaching belongs — official games carry no in-game coaching.

import { mountHeader } from './header.js';
import { cardFace } from './cardFace.js';
import { cardCode, sortCards } from '../engine/cards.js';
// Pure grading logic lives in scoringQuizGrading.js (DOM-free, unit-tested). This
// UI module owns state + rendering and passes resolved declarations into grade().
import {
    KINDS, kindById, callPhrase, atomPhrase,
    grade,
} from './scoringQuizGrading.js';
// The `[cards] <phrase> for N` combo-line renderer and the count-aloud line
// builder are shared with the live game's "Explain the score" overlay so the two
// stay visually identical (comboBreakdown.js).
import { comboLine, buildBreakdownLines, breakdownList } from './comboBreakdown.js';

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

// The declarable combo vocabulary (KINDS), kindById, and the call/atom phrase
// helpers live in scoringQuizGrading.js alongside the grading logic, and are
// imported above so the UI and the grader share one source of truth.

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
    mode: 'practice',     // 'practice' (score it yourself) | 'show' (show me how)
    cards: [],            // five {rank, suit}: [0..3] sorted hand, [4] starter
    selected: new Set(),  // indices into cards currently picked for a declaration
    kindId: 'fifteen',    // the selected dropdown entry id for the next declaration
    count: '',            // the running total count the user claims after this combo
    declarations: [],     // [{ kindId, idxs:[...], count:int }]
    result: null,         // graded verdict { correct, total, declTotal, declared:[...], missed:[...] }, or null
    answer: null,         // show-me breakdown { lines:[{cards, phrase, count}], total }, or null
    busy: false,          // a /tools/score-hand request is in flight
    builderError: null,   // inline reason an "Add to score" was rejected, or null
    error: null,          // a friendly request-level error message, or null
};

// resetHand clears the per-hand interaction state (keeps the dealt cards + mode).
function resetHand() {
    state.selected = new Set();
    state.kindId = 'fifteen';
    state.count = '';
    state.declarations = [];
    state.result = null;
    state.answer = null;
    state.busy = false;
    state.builderError = null;
    state.error = null;
}

function newDeal() {
    state.cards = dealShow();
    resetHand();
    render();
    if (state.mode === 'show') loadAnswer();
}

// setMode switches between Practice and Show-me on the SAME hand, resetting the
// interaction; entering Show-me fetches and displays the correct scoring.
function setMode(mode) {
    if (state.mode === mode) return;
    state.mode = mode;
    resetHand();
    render();
    if (mode === 'show') loadAnswer();
}

function toggleSelect(i) {
    if (state.result) return; // locked once graded; deal again to retry
    if (state.selected.has(i)) state.selected.delete(i);
    else state.selected.add(i);
    state.builderError = null;
    render();
}

// addDeclaration commits the current selection + kind + running count as one
// combo. Validation lives here (not in a disabled button) so the action never
// silently no-ops: invalid input surfaces a brief inline reason instead.
function addDeclaration() {
    if (state.selected.size === 0) {
        state.builderError = 'Select the card(s) forming the combo first.';
        render();
        return;
    }
    const entry = kindById(state.kindId);
    // Validate the selected card COUNT against the chosen kind. Fixed-count kinds
    // (pair royal, run of 4, double run of 3, …) demand an exact number; the
    // looser fifteen/flush/nobs accept a [min,max] range.
    const n = state.selected.size;
    const cc = entry.count;
    if (Array.isArray(cc)) {
        if (n < cc[0] || n > cc[1]) {
            state.builderError = `${entry.label} needs ${cc[0]}–${cc[1]} cards.`;
            render();
            return;
        }
    } else if (n !== cc) {
        state.builderError = `${entry.label} needs ${cc} card${cc === 1 ? '' : 's'}.`;
        render();
        return;
    }
    const count = parseInt(state.count, 10);
    if (!Number.isFinite(count) || count < 1) {
        state.builderError = 'Enter the total count.';
        render();
        return;
    }
    state.declarations.push({ kindId: entry.id, idxs: [...state.selected].sort((a, b) => a - b), count });
    state.selected = new Set();
    state.count = '';
    state.builderError = null;
    render();
}

function removeDeclaration(idx) {
    if (state.result) return;
    state.declarations.splice(idx, 1);
    render();
}

// The atomic decomposition + matching + grade() live in scoringQuizGrading.js
// (pure, DOM-free, unit-tested); atomsFromEngineCombo and grade are imported at
// the top. This module only builds the state grade() consumes and renders it.

// declaredTotal is the score the user has entered so far: the running count of the
// last combo declared, or 0 with none (a legitimate claim — some hands score
// nothing). Drives both the Submit Score button label and grading, so they agree.
function declaredTotal() {
    return state.declarations.length
        ? state.declarations[state.declarations.length - 1].count
        : 0;
}

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
    // grade() is pure: resolve each declaration's card indices into the actual
    // cards and hand it the engine combos + total (see scoringQuizGrading.js).
    state.result = grade(resolvedDeclarations(), data.combos || [], data.total || 0);
    render();
}

// resolvedDeclarations maps the view's index-based declarations into the plain
// { kindId, cards, count } shape the pure grade() consumes, so grading never
// reaches back into view state.
function resolvedDeclarations() {
    return state.declarations.map((d) => ({
        kindId: d.kindId,
        cards: d.idxs.map((idx) => state.cards[idx]),
        count: d.count,
    }));
}

// The Show-me breakdown (buildBreakdownLines) is shared with the live game's
// "Explain the score" overlay, imported from comboBreakdown.js.

// loadAnswer fetches the engine score for the current hand and stores the Show-me
// breakdown. Reuses the same /tools/score-hand endpoint and error handling as submit.
async function loadAnswer() {
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
    state.answer = buildBreakdownLines(data.combos || [], data.total || 0);
    render();
}

// --- rendering ----------------------------------------------------------------
function renderShow() {
    const cards = state.cards.map((c, i) => {
        const sel = state.selected.has(i);
        const locked = !!state.result || state.mode === 'show';
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
    // After grading the form stays in place (disabled) so the row doesn't collapse
    // and the board height holds steady — no jump on submit.
    const locked = !!state.result;
    const kindSel = h('select', {
        class: 'input sq-kind', 'aria-label': 'Combo type',
        ...(locked ? { disabled: 'disabled' } : {}),
        // Runs are explicit "Run of N" entries now, so changing the kind no longer
        // toggles any other control — just record the selection.
        onchange: (e) => { state.kindId = e.target.value; },
    }, ...KINDS.map((k) => h('option', { value: k.id, ...(k.id === state.kindId ? { selected: 'selected' } : {}) }, k.label)));

    const countInput = h('input', {
        type: 'number', min: '1', class: 'input sq-count', placeholder: 'count',
        'aria-label': 'Count', value: state.count,
        ...(locked ? { disabled: 'disabled' } : {}),
        oninput: (e) => { state.count = e.target.value; },
        onkeydown: (e) => { if (e.key === 'Enter') { e.preventDefault(); addDeclaration(); } },
    });

    const addBtn = h('button', { class: 'btn', type: 'button', ...(locked ? { disabled: 'disabled' } : {}), onclick: addDeclaration }, 'Add to score');

    // Reads as "[kind dropdown] for [total count]" — the literal "for" sits between
    // the selector and the count input.
    const row = h('div', { class: 'sq-controls' }, kindSel, h('span', { class: 'sq-for' }, 'for'), countInput, addBtn);
    return state.builderError
        ? h('div', { class: 'sq-builder' }, row, h('span', { class: 'sq-builder-error' }, state.builderError))
        : row;
}

// comboLine (the `[cards] <phrase> for N` row) is imported from
// comboBreakdown.js so this tutorial and the live "Explain the score" overlay
// render identically.

// renderModeToggle is the Practice / Show-me segmented control above the deck.
function renderModeToggle() {
    const tab = (mode, label) => h('button', {
        class: 'sq-mode-tab' + (state.mode === mode ? ' is-active' : ''),
        type: 'button', 'aria-pressed': state.mode === mode ? 'true' : 'false',
        onclick: () => setMode(mode),
    }, label);
    return h('div', { class: 'sq-modes' }, tab('practice', 'Practice'), tab('show', 'Show me'));
}

// renderAnswer is the Show-me breakdown: the correct combos with running counts.
function renderAnswer() {
    if (state.busy) return h('div', { class: 'sq-answer-loading' }, 'Scoring…');
    if (!state.answer) return null;
    return breakdownList(state.answer.lines);
}

// renderDeclared lists the combos inline (no panel, no heading). Before grading
// it's the editable declaration list; after grading the same list is mutated in
// place — wrong combos crossed out, missed combos appended.
function renderDeclared() {
    const rows = [];

    if (state.result) {
        for (const d of state.result.declared) {
            // Three cases, so the marks never contradict the verdict:
            //  - cards right AND count right → green ✓, showing the running count.
            //  - cards right BUT count wrong → ✕, entered count struck through, with
            //    the correct running count shown alongside to teach.
            //  - cards wrong → ✕, struck through; it counts for nothing.
            // The phrase comes from the user's chosen kind ("double run of 3").
            if (d.correct) {
                rows.push(comboLine(d.cards, callPhrase(d.kindId), d.correctCount, { status: 'correct' }));
            } else if (d.cardsOk) {
                rows.push(comboLine(d.cards, callPhrase(d.kindId), d.count, { wrong: true, status: 'wrong', correctNumber: d.correctCount }));
            } else {
                rows.push(comboLine(d.cards, callPhrase(d.kindId), d.count, { wrong: true, status: 'wrong' }));
            }
        }
        for (const m of state.result.missed) {
            // Missed lines are atomic, so they read as the bare atom ("run of 3").
            rows.push(comboLine(m.cards, atomPhrase(m), m.correctCount, { missed: true, status: 'missed' }));
        }
    } else {
        state.declarations.forEach((d, i) => {
            const cards = d.idxs.map((idx) => state.cards[idx]);
            rows.push(comboLine(cards, callPhrase(d.kindId), d.count, { onRemove: () => removeDeclaration(i) }));
        });
    }

    if (!rows.length) return null;
    return h('div', { class: 'sq-decl-list' }, ...rows);
}

// renderBottom is the bottom row. Before grading: the Submit Score button on the
// left. After grading: the Submit button is replaced in place (bottom left) by the
// overall Correct / Not-quite graphic, with the New Hand button at the right.
function renderBottom() {
    // Show-me mode: there's nothing to submit — just a Next Hand button.
    if (state.mode === 'show') {
        return h('div', { class: 'sq-bottom' },
            h('button', { class: 'btn btn-primary', type: 'button', onclick: newDeal }, 'Next Hand'));
    }
    if (state.result) {
        const verdict = state.result.correct
            ? h('span', { class: 'pr-badge ok' }, '✓ Correct')
            : h('span', { class: 'pr-badge off' }, 'Not quite');
        return h('div', { class: 'sq-bottom' },
            verdict,
            h('button', { class: 'btn btn-primary', type: 'button', onclick: newDeal }, 'New Hand'));
    }
    // The button previews the score to be submitted: the running count of the last
    // declared combo (0 before anything is added).
    const submitBtn = h('button', {
        class: 'btn btn-primary', type: 'button',
        ...(state.busy ? { disabled: 'disabled' } : {}),
        onclick: submit,
    }, state.busy ? 'Scoring…' : `Submit Score (${declaredTotal()})`);
    return h('div', { class: 'sq-bottom' }, submitBtn);
}

function render() {
    // Mode toggle + deck on top. Practice: form + your added-scores list. Show-me:
    // the engine's breakdown. Then the bottom row (Submit/verdict or Next Hand).
    const board = [renderModeToggle(), renderShow()];
    if (state.mode === 'show') {
        board.push(renderAnswer());
    } else {
        board.push(renderControls(), renderDeclared());
    }
    board.push(renderBottom());

    const kids = [
        h('h1', { class: 'pr-title' }, 'Hand Counting Tutorial'),
        h('div', { class: 'panel pr-board' }, ...board),
    ];
    if (state.error) {
        kids.push(h('div', { class: 'panel pr-message' },
            h('p', { class: 'pr-message-body' }, state.error)));
    }
    root.replaceChildren(...kids);
}

newDeal();
