// Post-game discard analysis view (A8). A focused, read-only page that surfaces
// the engine's verdict on each of the signed-in player's discards for ONE
// finished game. It is strictly post-game and account-scoped: it consumes
// GET /users/me/games/{id}/analysis (built in A3), which 404s for a live game,
// a game the user wasn't in, or an unknown id, and 401s for a guest.
//
// FIRST CUT: this is a per-hand discard verdict list, NOT a full board replay.
// The richer move-by-move board replay (pegging quality, the whole hand) is a
// separate later task (A2/A4); only the discard analysis the backend exposes
// today is rendered here.

import { mountHeader } from './header.js';
import { cardFace } from './cardFace.js';
import { parseCard, RANK_LABELS, SUIT_SYMBOLS } from '../engine/cards.js';

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

// gameId comes from ?game=<id>; without it there is nothing to analyze.
const gameId = new URLSearchParams(location.search).get('game');

// isRed mirrors cardFace's own suit colouring (diamonds, hearts).
const isRed = (c) => c.suit === 1 || c.suit === 2;

// evFmt shows an EV to two decimals (the wire value is already rounded to 4).
const evFmt = (n) => Number(n).toFixed(2);
// ptsFmt trims trailing zeros for the summary's "points lost" figure.
const ptsFmt = (n) => String(Math.round(Number(n) * 100) / 100);

// cardLabel is a compact rank+suit chip (suit-coloured) used in the verdict
// sentences, where a full card face would be visually heavy.
function cardLabel(c) {
    return h('span', { class: 'an-chip ' + (isRed(c) ? 'red' : 'black') },
        RANK_LABELS[c.rank] + SUIT_SYMBOLS[c.suit]);
}
const cardLabels = (cards) => cards.map(cardLabel);

// message renders a single centered notice (loading / error / empty states).
function message(title, body, action) {
    root.replaceChildren(
        h('h1', { class: 'an-title' }, 'Game analysis'),
        h('div', { class: 'panel an-message' },
            h('p', { class: 'an-message-title' }, title),
            body ? h('p', { class: 'an-message-body' }, body) : null,
            action || null));
}

// renderHand builds one hand's verdict card: the dealt six (the thrown pair
// highlighted), the player's choice, and — when sub-optimal — the engine's.
function renderHand(d, i) {
    const hand = (d.hand || []).map(parseCard);
    const thrown = (d.throw || []).map(parseCard);
    const isThrown = (c) => thrown.some((t) => t.rank === c.rank && t.suit === c.suit);

    const six = h('div', { class: 'an-hand-cards' },
        hand.map((c) => cardFace(c, { small: true, extra: isThrown(c) ? 'an-thrown' : 'an-kept' })));

    const cribTag = h('span', { class: 'an-crib' }, d.dealer ? 'Your crib' : "Opponent's crib");
    const badge = d.optimal
        ? h('span', { class: 'an-badge ok' }, '✓ optimal')
        : h('span', { class: 'an-badge off' }, '−' + evFmt(d.delta_ev));

    const header = h('div', { class: 'an-hand-head' },
        h('span', { class: 'an-hand-no' }, 'Hand ' + (i + 1)),
        cribTag,
        badge);

    const yours = h('div', { class: 'an-line' },
        h('span', { class: 'an-line-label' }, 'You threw '),
        ...cardLabels(thrown),
        h('span', { class: 'an-line-sep' }, ' — kept '),
        ...cardLabels((d.keep || []).map(parseCard)),
        h('span', { class: 'an-ev' }, ' (EV ' + evFmt(d.keep_ev) + ')'));

    const lines = [yours];
    if (!d.optimal) {
        lines.push(h('div', { class: 'an-line an-line-engine' },
            h('span', { class: 'an-line-label' }, 'Engine: throw '),
            ...cardLabels((d.best_throw || []).map(parseCard)),
            h('span', { class: 'an-line-sep' }, ' — keep '),
            ...cardLabels((d.best_keep || []).map(parseCard)),
            h('span', { class: 'an-ev' }, ' (EV ' + evFmt(d.best_ev) + ')')));
    }

    return h('div', { class: 'panel an-hand' + (d.optimal ? ' is-optimal' : '') }, header, six, ...lines);
}

function renderAnalysis(data) {
    const s = data.summary || { hands: 0, optimal_discards: 0, total_ev_lost: 0 };
    const summary = h('div', { class: 'panel an-summary' },
        h('div', { class: 'an-summary-main' },
            `${s.optimal_discards} / ${s.hands} discards optimal`),
        h('div', { class: 'an-summary-sub' },
            `${ptsFmt(s.total_ev_lost)} points lost to the crib EV`));

    const hands = (data.discards || []).map((d, i) => renderHand(d, i));
    const body = hands.length
        ? hands
        : [h('div', { class: 'panel an-message' }, h('p', { class: 'an-message-body' }, 'No discards were recorded for this game.'))];

    root.replaceChildren(
        h('h1', { class: 'an-title' }, 'Game analysis'),
        h('p', { class: 'an-subtitle' }, 'How your discards stacked up against the engine.'),
        summary,
        h('div', { class: 'an-hands' }, ...body));
}

async function load() {
    if (!gameId) {
        message('No game selected', 'Open this page from a finished game to see its analysis.');
        return;
    }
    message('Loading analysis…', '');
    let r;
    try {
        r = await fetch(`/users/me/games/${encodeURIComponent(gameId)}/analysis`);
    } catch {
        message('Could not load analysis',
            'There was a network problem reaching the server.',
            h('button', { class: 'btn btn-primary', onclick: load }, 'Try again'));
        return;
    }
    if (r.status === 401) {
        message('Log in to view analysis', 'Game analysis is only available for your own finished games. Use Login in the header above, then reopen this page.');
        return;
    }
    if (r.status === 404) {
        message('Analysis not available', "We couldn't find a finished game of yours with this id. Analysis is only available for your own completed games.");
        return;
    }
    if (!r.ok) {
        message('Could not load analysis', 'The server returned an unexpected error.',
            h('button', { class: 'btn btn-primary', onclick: load }, 'Try again'));
        return;
    }
    let data;
    try {
        data = await r.json();
    } catch {
        message('Could not load analysis', 'The analysis response was malformed.',
            h('button', { class: 'btn btn-primary', onclick: load }, 'Try again'));
        return;
    }
    renderAnalysis(data);
}

load();
