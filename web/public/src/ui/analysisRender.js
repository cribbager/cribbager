// Pure, page-agnostic rendering of a game's discard analysis. Both the dedicated
// analysis page (analyze.js, account-scoped historical games) and the in-game
// "Evaluate game" panel (main.js, computed client-side for the just-played game)
// build the same data shape and render it here, so the verdict cards look
// identical in both places. This module has NO side effects on import — no header
// mount, no fetch, no DOM lookups — it only turns a data object into nodes.
//
// The data shape (gameAnalysisResponse, mirrored client-side):
//   { summary: { hands, optimal_discards, total_ev_lost },
//     discards: [ { hand[6], throw[2], keep[4], keep_ev, dealer,
//                   best_throw[2], best_keep[4], best_ev, delta_ev, optimal } ] }
// Card fields are rank+suit code strings (e.g. "5H"), parsed with parseCard.

import { cardFace } from './cardFace.js';
import { parseCard, sortCards, RANK_LABELS, SUIT_SYMBOLS } from '../engine/cards.js';

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

// renderHand builds one hand's verdict card: the dealt six (the thrown pair
// highlighted), the player's choice, and — when sub-optimal — the engine's.
export function renderHand(d, i) {
    const hand = (d.hand || []).map(parseCard);
    const thrown = (d.throw || []).map(parseCard);
    const isThrown = (c) => thrown.some((t) => t.rank === c.rank && t.suit === c.suit);

    const six = h('div', { class: 'an-hand-cards' },
        sortCards(hand).map((c) => cardFace(c, { small: true, extra: isThrown(c) ? 'an-thrown' : 'an-kept' })));

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
        ...cardLabels(sortCards(thrown)),
        h('span', { class: 'an-line-sep' }, ' — kept '),
        ...cardLabels(sortCards((d.keep || []).map(parseCard))),
        h('span', { class: 'an-ev' }, ' (EV ' + evFmt(d.keep_ev) + ')'));

    const lines = [yours];
    if (!d.optimal) {
        lines.push(h('div', { class: 'an-line an-line-engine' },
            h('span', { class: 'an-line-label' }, 'Engine: throw '),
            ...cardLabels(sortCards((d.best_throw || []).map(parseCard))),
            h('span', { class: 'an-line-sep' }, ' — keep '),
            ...cardLabels(sortCards((d.best_keep || []).map(parseCard))),
            h('span', { class: 'an-ev' }, ' (EV ' + evFmt(d.best_ev) + ')')));
    }

    return h('div', { class: 'panel an-hand' + (d.optimal ? ' is-optimal' : '') }, header, six, ...lines);
}

// analysisBody returns the summary panel plus the per-hand verdict cards (an
// array of nodes), with no page title/subtitle wrapper — the caller frames it.
export function analysisBody(data) {
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

    return [summary, h('div', { class: 'an-hands' }, ...body)];
}
