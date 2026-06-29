// Discard trainer (A5) — a standalone practice surface, NOT a live game. You are
// dealt six cards, pick the two to throw to the crib (with a toggle for whose crib
// it is, since that changes the right play), and the engine grades your choice:
// the best discard, where yours ranked among the 15, and the EV you gave up. This
// is exactly where teaching belongs — official games carry no in-game coaching.
//
// It reuses the shared card model/renderer (cards.js + cardFace.js) and the engine
// itself via POST /tools/discard-eval (eval.RankDiscards on the server); no scoring
// is reimplemented here.

import { mountHeader } from './header.js';
import { cardFace } from './cardFace.js';
import { parseCard, cardCode, cardsEqual, RANK_LABELS, SUIT_SYMBOLS } from '../engine/cards.js';

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

const root = document.getElementById('practice');
mountHeader();

// isRed mirrors cardFace's own suit colouring (diamonds, hearts).
const isRed = (c) => c.suit === 1 || c.suit === 2;
// evFmt shows an EV to two decimals (the wire value is already rounded to 4).
const evFmt = (n) => Number(n).toFixed(2);

// cardLabel is a compact rank+suit chip (suit-coloured), reused from the analysis
// view's vocabulary for the verdict sentences.
function cardLabel(c) {
    return h('span', { class: 'pr-chip ' + (isRed(c) ? 'red' : 'black') },
        RANK_LABELS[c.rank] + SUIT_SYMBOLS[c.suit]);
}
const cardLabels = (cards) => cards.map(cardLabel);

// dealHand returns six distinct random cards by shuffling a fresh 52-card deck.
function dealHand() {
    const deck = [];
    for (let rank = 1; rank <= 13; rank++) {
        for (let suit = 0; suit < 4; suit++) deck.push({ rank, suit });
    }
    for (let i = deck.length - 1; i > 0; i--) {
        const j = Math.floor(Math.random() * (i + 1));
        [deck[i], deck[j]] = [deck[j], deck[i]];
    }
    return deck.slice(0, 6);
}

// --- view state ---------------------------------------------------------------
const state = {
    hand: [],          // six {rank, suit}
    selected: new Set(), // indices into hand the user will throw
    dealer: true,      // true = your crib (you deal), false = opponent's crib
    result: null,      // the rendered verdict, or null before checking
    busy: false,       // a /tools/discard-eval request is in flight
    error: null,       // a friendly error message + retry, or null
};

function newDeal() {
    state.hand = dealHand();
    state.selected = new Set();
    state.result = null;
    state.error = null;
    state.busy = false;
    render();
}

function toggleSelect(i) {
    if (state.result) return; // locked once checked; deal again to retry
    if (state.selected.has(i)) state.selected.delete(i);
    else if (state.selected.size < 2) state.selected.add(i);
    render();
}

// locateChoice finds the user's chosen throw among the ranked holds and returns
// { rank, hold } (1-based rank), or null if it somehow isn't present.
function locateChoice(holds, thrown) {
    const sameThrow = (throwPair) => {
        const t = throwPair.map(parseCard);
        return thrown.every((c) => t.some((x) => cardsEqual(x, c)));
    };
    for (let i = 0; i < holds.length; i++) {
        if (sameThrow(holds[i].throw)) return { rank: i + 1, hold: holds[i] };
    }
    return null;
}

async function check() {
    if (state.selected.size !== 2 || state.busy) return;
    state.busy = true;
    state.error = null;
    render();

    const hand = state.hand.map(cardCode);
    let r;
    try {
        r = await fetch('/tools/discard-eval', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ hand, dealer: state.dealer }),
        });
    } catch {
        state.busy = false;
        state.error = 'There was a network problem reaching the engine. Check your connection and try again.';
        render();
        return;
    }
    if (!r.ok) {
        state.busy = false;
        state.error = 'The engine could not grade that hand. Deal a new hand and try again.';
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

    const thrown = [...state.selected].map((i) => state.hand[i]);
    const choice = locateChoice(data.holds || [], thrown);
    state.busy = false;
    state.result = { holds: data.holds || [], dealer: data.dealer, thrown, choice };
    render();
}

// --- rendering ----------------------------------------------------------------
function renderControls() {
    const yourCrib = h('button', {
        type: 'button',
        class: 'pr-seg' + (state.dealer ? ' is-on' : ''),
        'aria-pressed': state.dealer ? 'true' : 'false',
        onclick: () => { if (!state.result) { state.dealer = true; render(); } },
    }, 'Your crib');
    const oppCrib = h('button', {
        type: 'button',
        class: 'pr-seg' + (!state.dealer ? ' is-on' : ''),
        'aria-pressed': !state.dealer ? 'true' : 'false',
        onclick: () => { if (!state.result) { state.dealer = false; render(); } },
    }, "Opponent's crib");

    return h('div', { class: 'pr-controls' },
        h('span', { class: 'pr-controls-label' }, 'Crib'),
        h('div', { class: 'pr-seg-group', role: 'group', 'aria-label': 'Whose crib' }, yourCrib, oppCrib));
}

function renderHand() {
    const cards = state.hand.map((c, i) => {
        const sel = state.selected.has(i);
        const locked = !!state.result;
        const face = cardFace(c, { extra: locked ? (sel ? 'pr-thrown' : '') : ('selectable' + (sel ? ' selected' : '')) });
        if (!locked) {
            face.setAttribute('role', 'button');
            face.setAttribute('aria-pressed', sel ? 'true' : 'false');
            face.setAttribute('tabindex', '0');
            face.addEventListener('click', () => toggleSelect(i));
            face.addEventListener('keydown', (e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); toggleSelect(i); } });
        }
        return face;
    });
    return h('div', { class: 'pr-hand' }, ...cards);
}

function renderActions() {
    if (state.result) {
        return h('div', { class: 'pr-actions' },
            h('button', { class: 'btn btn-primary', type: 'button', onclick: newDeal }, 'Deal again'));
    }
    const n = state.selected.size;
    const checkBtn = h('button', {
        class: 'btn btn-primary', type: 'button',
        ...(n === 2 && !state.busy ? {} : { disabled: 'disabled' }),
        onclick: () => check(),
    }, state.busy ? 'Checking…' : 'Check');
    return h('div', { class: 'pr-actions' },
        h('span', { class: 'pr-hint' }, n === 2 ? 'Ready to check.' : `Select ${2 - n} more card${2 - n === 1 ? '' : 's'} to throw.`),
        checkBtn);
}

function renderVerdict() {
    const { holds, choice, dealer } = state.result;
    const best = holds[0];
    const cribText = dealer ? 'your crib' : "the opponent's crib";

    const kids = [];

    if (!choice) {
        // Defensive: the engine enumerates all 15 holds, so this should never happen.
        kids.push(h('p', { class: 'pr-message-body' }, "Couldn't locate your discard in the engine's ranking."));
        return h('div', { class: 'panel pr-verdict' }, ...kids);
    }

    const optimal = choice.rank === 1;
    const delta = best.ev - choice.hold.ev;

    const badge = optimal
        ? h('span', { class: 'pr-badge ok' }, '✓ optimal')
        : h('span', { class: 'pr-badge off' }, `−${evFmt(delta)} EV`);
    kids.push(h('div', { class: 'pr-verdict-head' },
        h('span', { class: 'pr-rank' }, optimal ? 'Best discard!' : `Your discard ranked ${ordinal(choice.rank)} of ${holds.length}`),
        badge));

    kids.push(h('div', { class: 'pr-line' },
        h('span', { class: 'pr-line-label' }, 'You threw '),
        ...cardLabels(choice.hold.throw.map(parseCard)),
        h('span', { class: 'pr-line-sep' }, ' — kept '),
        ...cardLabels(choice.hold.keep.map(parseCard)),
        h('span', { class: 'pr-ev' }, ` (EV ${evFmt(choice.hold.ev)})`)));

    if (!optimal) {
        kids.push(h('div', { class: 'pr-line pr-line-engine' },
            h('span', { class: 'pr-line-label' }, 'Best: throw '),
            ...cardLabels(best.throw.map(parseCard)),
            h('span', { class: 'pr-line-sep' }, ' — keep '),
            ...cardLabels(best.keep.map(parseCard)),
            h('span', { class: 'pr-ev' }, ` (EV ${evFmt(best.ev)})`)));
    }

    kids.push(h('p', { class: 'pr-verdict-note' },
        `Expected points playing to ${cribText}, computed over every starter and opponent discard.`));

    // Full ranked table, collapsed by default.
    const rows = holds.map((hld, i) => h('tr', { class: 'pr-row' + (choice.rank === i + 1 ? ' is-you' : '') },
        h('td', { class: 'pr-td-rank' }, String(i + 1)),
        h('td', {}, ...cardLabels(hld.throw.map(parseCard))),
        h('td', { class: 'pr-td-ev' }, evFmt(hld.ev)),
        h('td', { class: 'pr-td-tag' }, choice.rank === i + 1 ? 'you' : (i === 0 ? 'best' : ''))));
    const table = h('details', { class: 'pr-details' },
        h('summary', {}, 'All 15 discards'),
        h('table', { class: 'pr-table' },
            h('thead', {}, h('tr', {}, h('th', {}, '#'), h('th', {}, 'Throw'), h('th', { class: 'pr-td-ev' }, 'EV'), h('th', {}, ''))),
            h('tbody', {}, ...rows)));
    kids.push(table);

    return h('div', { class: 'panel pr-verdict' + (optimal ? ' is-optimal' : '') }, ...kids);
}

function ordinal(n) {
    const s = ['th', 'st', 'nd', 'rd'];
    const v = n % 100;
    return n + (s[(v - 20) % 10] || s[v] || s[0]);
}

function render() {
    const kids = [
        h('h1', { class: 'pr-title' }, 'Discard trainer'),
        h('p', { class: 'pr-subtitle' }, 'You’re dealt six cards. Throw two to the crib, then see how your choice ranks against the engine.'),
        h('div', { class: 'panel pr-board' },
            renderControls(),
            renderHand(),
            renderActions()),
    ];
    if (state.error) {
        kids.push(h('div', { class: 'panel pr-message' },
            h('p', { class: 'pr-message-body' }, state.error),
            h('button', { class: 'btn btn-primary', type: 'button', onclick: () => check() }, 'Try again')));
    }
    if (state.result) kids.push(renderVerdict());
    root.replaceChildren(...kids);
}

newDeal();
