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

// The combo kinds the user can declare — the full vocabulary a player counts
// aloud, in conventional counting order. Compound combos (pair royal, double
// runs, …) are first-class entries: grading reduces BOTH them and the engine's
// bundled combos to the same multiset of ATOMIC components (see grade()), so a
// player may declare a double run either as one bundle or fully decomposed and
// both read Correct. Each entry has an `id` (dropdown value), a `family`
// (fifteen | pair | run | flush | nobs) driving atom generation, a `count`
// (exact card count, or [min,max] for the looser fifteen/flush/nobs) for
// add-time validation, and a `call` (how it's read aloud).
const KINDS = [
    { id: 'fifteen', label: 'Fifteen', family: 'fifteen', count: [2, 5], call: 'fifteen' },
    { id: 'pair', label: 'Pair', family: 'pair', count: 2, call: 'pair' },
    { id: 'pairroyal', label: 'Pair royal', family: 'pair', count: 3, call: 'pair royal' },
    { id: 'doublepairroyal', label: 'Double pair royal', family: 'pair', count: 4, call: 'double pair royal' },
    { id: 'run3', label: 'Run of 3', family: 'run', count: 3, call: 'run of 3' },
    { id: 'run4', label: 'Run of 4', family: 'run', count: 4, call: 'run of 4' },
    { id: 'run5', label: 'Run of 5', family: 'run', count: 5, call: 'run of 5' },
    { id: 'doublerun3', label: 'Double run of 3', family: 'run', count: 4, call: 'double run of 3' },
    { id: 'doublerun4', label: 'Double run of 4', family: 'run', count: 5, call: 'double run of 4' },
    { id: 'doubledoublerun3', label: 'Double double run of 3', family: 'run', count: 5, call: 'double double run of 3' },
    { id: 'triplerun3', label: 'Triple run of 3', family: 'run', count: 5, call: 'triple run of 3' },
    { id: 'flush', label: 'Flush', family: 'flush', count: [4, 5], call: 'flush' },
    { id: 'nobs', label: 'Nobs', family: 'nobs', count: [1, 2], call: 'nobs' },
];
const kindById = (id) => KINDS.find((k) => k.id === id) || KINDS[0];
// callPhrase reads a DECLARATION aloud from its dropdown id ("double run of 3").
// atomPhrase reads a single ATOMIC component (used for missed lines): runs fold
// in their length ("run of 3"); everything else is the bare type word.
const callPhrase = (kindId) => kindById(kindId).call;
const atomPhrase = (a) => (a.kind === 'run' ? `run of ${a.runLen}` : a.kind);

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

// cardKey is an order-independent identity for a set of cards (their wire codes,
// sorted) used to match one atomic component against another.
const cardKey = (cards) => cards.map(cardCode).sort().join(',');

// --- atomic decomposition -----------------------------------------------------
// Grading reduces every combo — engine-reported or user-declared — to the same
// alphabet of ATOMIC components: a 2-pt fifteen, a 2-pt pair (exactly 2 cards), a
// length-L / L-pt run, a 4-or-5-pt flush, a 1-pt nobs. An atom's identity is its
// type (+ run length) and its order-independent card set; matching at this level
// means a bundled "double run of 3" and its hand-decomposed pair + two runs are
// interchangeable. Each atom is { kind, cards, points, runLen? }.

// pairSubsets turns k same-rank cards into all C(k,2) atomic pairs. Used for the
// pair family (pair/pair royal/double pair royal) and for the duplicated ranks a
// run combo absorbs.
function pairSubsets(cards) {
    const atoms = [];
    for (let i = 0; i < cards.length; i++)
        for (let j = i + 1; j < cards.length; j++)
            atoms.push({ kind: 'pair', cards: [cards[i], cards[j]], points: 2 });
    return atoms;
}

// runAtoms decomposes a bundled run (single or multiple) into its atomic parts.
// Group the cards by rank: the distinct ranks form the consecutive base run, and
// the cartesian product taking one card per rank yields exactly `multiplicity`
// atomic runs of that length. Any rank appearing more than once also contributes
// its C(k,2) atomic pairs. (e.g. 3,4,5,5 → runs 3-4-5a & 3-4-5b + pair 5a-5b.)
// The SAME logic serves the engine's `run` combos and the user's run-family
// declarations; if the selected cards can't form the claimed run the generated
// atoms simply won't match the engine pool and the declaration grades wrong.
function runAtoms(cards) {
    const byRank = new Map();
    for (const c of cards) {
        if (!byRank.has(c.rank)) byRank.set(c.rank, []);
        byRank.get(c.rank).push(c);
    }
    const ranks = [...byRank.keys()].sort((a, b) => a - b);
    const runLen = ranks.length;
    let runs = [[]];
    for (const rank of ranks) {
        const next = [];
        for (const combo of runs)
            for (const card of byRank.get(rank)) next.push([...combo, card]);
        runs = next;
    }
    const atoms = runs.map((rc) => ({ kind: 'run', runLen, cards: rc, points: runLen }));
    for (const rank of ranks) {
        const group = byRank.get(rank);
        if (group.length > 1) atoms.push(...pairSubsets(group));
    }
    return atoms;
}

// atomsFromEngineCombo expands one engine combo into atoms. fifteen/flush/nobs
// are atomic already; pair combos (pair/pair-royal/double-pair-royal, points
// k*(k-1)) fan out to their 2-card subsets; run combos go through runAtoms.
function atomsFromEngineCombo(c) {
    const cards = (c.cards || []).map(parseCard);
    switch (c.kind) {
        case 'fifteen': return [{ kind: 'fifteen', cards, points: 2 }];
        case 'pair': return pairSubsets(cards);
        case 'run': return runAtoms(cards);
        case 'flush': return [{ kind: 'flush', cards, points: cards.length }];
        case 'nobs': return [{ kind: 'nobs', cards, points: 1 }];
        default: return [{ kind: c.kind, cards, points: c.points }];
    }
}

// atomsFromDeclaration expands a user declaration into atoms by its kind family,
// reusing the very same generators as the engine side so the two pools line up.
function atomsFromDeclaration(entry, cards) {
    switch (entry.family) {
        case 'fifteen': return [{ kind: 'fifteen', cards, points: 2 }];
        case 'pair': return pairSubsets(cards);
        case 'run': return runAtoms(cards);
        case 'flush': return [{ kind: 'flush', cards, points: cards.length }];
        case 'nobs': return [{ kind: 'nobs', cards, points: 1 }];
        default: return [];
    }
}

// atomsMatch compares a pool atom against a wanted atom by type + card set (runs
// also by length). nobs stays lenient: any engine nobs satisfies a nobs atom,
// regardless of which card(s) the user picked.
function atomsMatch(pool, want) {
    if (want.kind === 'nobs') return pool.kind === 'nobs';
    if (pool.kind !== want.kind) return false;
    if (want.kind === 'run' && pool.runLen !== want.runLen) return false;
    return cardKey(pool.cards) === cardKey(want.cards);
}

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
    state.result = grade(data.combos || [], data.total || 0);
    render();
}

// grade reduces the engine's combos AND the user's declarations to atomic
// components and matches them in that common alphabet, then grades each line on
// BOTH its cards and the running count the user typed.
//
// Cards (cardsOk): a declaration's cards are right iff EVERY one of its atoms is
// found unconsumed in the engine pool (which it then consumes); otherwise the
// cards are wrong and the declaration consumes nothing. This is what lets a double
// run be declared bundled or fully decomposed and grade the same; nobs atoms match
// leniently. Leftover engine atoms become the "missed" list, each its own line.
//
// Running count: the expected running count at a line is the cumulative of the
// REAL points over the sequence of CORRECTLY-CARDED declarations, in order. A
// card-wrong line breaks the chain — it contributes 0 and is skipped — so a later
// correct line's expected count is the running total of the correct-carded points
// only. A line is fully correct (green ✓) iff its cards match AND the count the
// user typed equals that expected running count. This keeps the per-line marks and
// the overall verdict logically consistent: since the last correct line's running
// count is exactly the sum of all correct-carded points, "every line green + none
// missed" necessarily equals the engine total, so all-green ⇒ Correct always and a
// separate declTotal===engineTotal check is redundant.
function grade(rawCombos, engineTotal) {
    const pool = [];
    for (const c of rawCombos)
        for (const a of atomsFromEngineCombo(c)) pool.push({ ...a, used: false });

    // First pass — cards only: decide each declaration's cardsOk and its real point
    // value, independent of the running count the user typed.
    const declared = state.declarations.map((d) => {
        const entry = kindById(d.kindId);
        const cards = d.idxs.map((idx) => state.cards[idx]);
        const want = atomsFromDeclaration(entry, cards);
        // Find a distinct unconsumed pool atom for each wanted atom; only commit
        // (consume) if ALL are found, so a card-wrong declaration leaves the pool intact.
        const found = [];
        let cardsOk = want.length > 0;
        for (const a of want) {
            const m = pool.find((p) => !p.used && !found.includes(p) && atomsMatch(p, a));
            if (!m) { cardsOk = false; break; }
            found.push(m);
        }
        if (cardsOk) for (const m of found) m.used = true;
        const points = cardsOk ? want.reduce((s, a) => s + a.points, 0) : 0;
        return { kindId: d.kindId, cards, count: d.count, cardsOk, points };
    });

    const missed = pool.filter((p) => !p.used)
        .map((p) => ({ kind: p.kind, cards: p.cards, points: p.points, runLen: p.runLen }));

    // Second pass — running count: walk the declarations in order, accumulating the
    // real points of correctly-carded lines only. Each correct-carded line records
    // the running count it SHOULD read (correctCount); a line is green only if the
    // count the user typed matches it. Card-wrong lines don't advance the chain and
    // carry no expected running count.
    let run = 0;
    for (const d of declared) {
        if (d.cardsOk) {
            run += d.points;
            d.correctCount = run;            // the running count this line should read
            d.correct = d.count === run;     // green only when cards AND count agree
        } else {
            d.correctCount = null;
            d.correct = false;
        }
    }
    // Missed atoms continue the same running count so the teaching progression ends
    // at the engine total.
    for (const m of missed) { run += m.points; m.correctCount = run; }

    // Overall verdict: every declared line green (cards right AND count right) and
    // nothing missed. All-green + none-missed ⇒ the last line's running count equals
    // the engine total, so "all lines ✓ yet Not quite" is impossible by construction.
    const correct = declared.every((d) => d.correct) && missed.length === 0;

    return { correct, total: engineTotal, declared, missed };
}

// buildAnswer turns the engine's combos into the Show-me breakdown: every combo
// decomposed to atoms, ordered the way you'd count aloud (fifteens, pairs, runs,
// flush, nobs), each carrying the running count up to it (ending at the total).
function buildAnswer(rawCombos, total) {
    const atoms = [];
    for (const c of rawCombos) atoms.push(...atomsFromEngineCombo(c));
    const order = { fifteen: 0, pair: 1, run: 2, flush: 3, nobs: 4 };
    atoms.sort((a, b) => (order[a.kind] - order[b.kind]) || ((a.runLen || 0) - (b.runLen || 0)));
    let run = 0;
    const lines = atoms.map((a) => { run += a.points; return { cards: a.cards, phrase: atomPhrase(a), count: run }; });
    return { lines, total };
}

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
    state.answer = buildAnswer(data.combos || [], data.total || 0);
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

// comboLine renders one combo as real card faces followed by how it's called and
// the trailing number: "[faces] run of 3 for 7". `phrase` is the already-resolved
// call ("double run of 3", "fifteen", …). For a declaration the number is the
// running count after it; for a missed engine atom it's the atom's own points.
// opts.wrong crosses it out; opts.missed marks it as one the user didn't declare.
// opts.correctNumber, when set, appends the running count the line SHOULD read
// (not struck) — used to teach when the cards are right but the entered count is wrong.
function comboLine(cards, phrase, forNumber, opts = {}) {
    const cls = 'sq-combo'
        + (opts.wrong ? ' is-wrong' : '')
        + (opts.missed ? ' is-missed' : '');
    const kids = [];
    // Leading chip, flush left: before grading it's the remove (✕) button; after
    // grading it's a green circled check (correct) or red circled ✕ (wrong/missed).
    // Show-me lines are `plain` — the breakdown is the answer, so no chip.
    if (!opts.plain) {
        if (opts.onRemove) {
            kids.push(h('button', {
                class: 'sq-decl-remove', type: 'button', 'aria-label': 'Remove this combo',
                onclick: opts.onRemove,
            }, '✕'));
        } else if (opts.status === 'correct') {
            kids.push(h('span', { class: 'sq-status ok', 'aria-label': 'correct' }, '✓'));
        } else {
            kids.push(h('span', { class: 'sq-status bad', 'aria-label': opts.status === 'missed' ? 'missed' : 'wrong' }, '✕'));
        }
    }
    kids.push(
        h('span', { class: 'sq-combo-cards' }, ...sortCards(cards).map((c) => cardFace(c, { small: true }))),
        h('span', { class: 'sq-combo-call' }, `${phrase} for ${forNumber}`),
    );
    // Right cards, wrong running count: show the count it should have read, not
    // struck through, so the line still teaches the correct progression.
    if (opts.correctNumber != null) {
        kids.push(h('span', { class: 'sq-combo-fix' }, `should be ${opts.correctNumber}`));
    }
    return h('div', { class: cls }, ...kids);
}

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
    if (!state.answer.lines.length) {
        return h('div', { class: 'sq-decl-list' },
            h('div', { class: 'sq-combo' }, h('span', { class: 'sq-combo-call' }, 'No points in this hand — a nineteen.')));
    }
    const rows = state.answer.lines.map((l) => comboLine(l.cards, l.phrase, l.count, { plain: true }));
    return h('div', { class: 'sq-decl-list' }, ...rows);
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
