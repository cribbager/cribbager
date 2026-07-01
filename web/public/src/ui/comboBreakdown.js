// Shared "count-aloud" breakdown rendering — one source of truth for the
// `[card faces] <combo> for N` scoring line used in BOTH the Hand Counting
// Tutorial (scoringQuiz.js) and the live game's "Explain the score" overlay
// (main.js). Extracting it here keeps the two surfaces visually identical and
// DRY. It is display-only: it turns engine combos into ordered lines with
// running counts and renders them; it never scores anything itself (grading and
// atom decomposition live in scoringQuizGrading.js).

import { cardFace } from './cardFace.js';
import { sortCards } from '../engine/cards.js';
import { atomsFromEngineCombo, atomPhrase } from './scoringQuizGrading.js';

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

// comboLine renders one combo as real card faces followed by how it's called and
// the trailing number: "[faces] run of 3 for 7". `phrase` is the already-resolved
// call ("double run of 3", "fifteen", …). For a declaration the number is the
// running count after it; for a missed engine atom it's the atom's own points.
// opts.wrong crosses it out; opts.missed marks it as one the user didn't declare.
// opts.correctNumber, when set, appends the running count the line SHOULD read
// (not struck) — used to teach when the cards are right but the entered count is wrong.
export function comboLine(cards, phrase, forNumber, opts = {}) {
    const cls = 'sq-combo'
        + (opts.wrong ? ' is-wrong' : '')
        + (opts.missed ? ' is-missed' : '');
    const kids = [];
    // Leading chip, flush left: before grading it's the remove (✕) button; after
    // grading it's a green circled check (correct) or red circled ✕ (wrong/missed).
    // Show-me / explain lines are `plain` — the breakdown is the answer, so no chip.
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

// buildBreakdownLines turns the engine's combos into the count-aloud breakdown:
// every combo decomposed to atoms, ordered the way you'd count aloud (fifteens,
// pairs, runs, flush, nobs), each carrying the running count up to it (ending at
// the total). Returns { lines:[{cards, phrase, count}], total }.
export function buildBreakdownLines(rawCombos, total) {
    const atoms = [];
    for (const c of rawCombos) atoms.push(...atomsFromEngineCombo(c));
    const order = { fifteen: 0, pair: 1, run: 2, flush: 3, nobs: 4 };
    atoms.sort((a, b) => (order[a.kind] - order[b.kind]) || ((a.runLen || 0) - (b.runLen || 0)));
    let run = 0;
    const lines = atoms.map((a) => { run += a.points; return { cards: a.cards, phrase: atomPhrase(a), count: run }; });
    return { lines, total };
}

// breakdownList renders the read-only breakdown as a `.sq-decl-list` of plain
// combo lines (no chips), or a single "No points in this hand" line when a hand
// scores nothing. `lines` is the array from buildBreakdownLines(...).lines.
export function breakdownList(lines) {
    if (!lines.length) {
        return h('div', { class: 'sq-decl-list' },
            h('div', { class: 'sq-combo' }, h('span', { class: 'sq-combo-call' }, 'No points in this hand')));
    }
    const rows = lines.map((l) => comboLine(l.cards, l.phrase, l.count, { plain: true }));
    return h('div', { class: 'sq-decl-list' }, ...rows);
}
