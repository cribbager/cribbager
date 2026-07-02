// Discard evaluator (T1) — a standalone tool page, NOT a live game. The user sets
// up a discard situation (six dealt cards, both scores, whose crib) and sees how
// the engine evaluates it through three lenses side by side: point EV (the
// champion's mid-game objective), max hand (expected hand value alone, for when
// only your own hand can save you), and win probability (the champion's endgame
// objective). One request, one set of 15 holds — the lenses are re-sorts of the
// same rows (evaluatorLenses.js), never recomputations.
//
// It reuses the shared card model/renderer (cards.js + cardFace.js) and the
// engine itself via POST /tools/discard-eval with scores (eval.RankDiscardsWin
// on the server); no scoring is reimplemented here.

import { mountHeader } from './header.js';
import { cardFace } from './cardFace.js';
import { parseCard, cardCode, cardsEqual, sortCards, RANK_LABELS, SUIT_SYMBOLS } from '../engine/cards.js';
import { LENSES, sortHolds, situationLines, situationGuidance, pct } from './evaluatorLenses.js';

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

const root = document.getElementById('discard-evaluator');
mountHeader();

// isRed mirrors cardFace's own suit colouring (diamonds, hearts).
const isRed = (c) => c.suit === 1 || c.suit === 2;
// evFmt shows an EV to two decimals (the wire value is already rounded to 4).
const evFmt = (n) => Number(n).toFixed(2);
// cribFmt keeps the crib term's sign visible — it is what flips with the crib owner.
const cribFmt = (n) => (n >= 0 ? '+' : '−') + Math.abs(n).toFixed(2);

// cardLabel is a compact rank+suit chip (suit-coloured), the same vocabulary as
// the practice/analysis views.
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
    picked: [],        // up to six {rank, suit} chosen in the deck grid
    myScore: 0,        // 0..120, or null while the input is unparseable
    oppScore: 0,       // 0..120, or null while the input is unparseable
    dealer: true,      // true = you deal (your crib), false = opponent deals
    lens: 'ev',        // active lens: 'ev' | 'hand' | 'win'
    result: null,      // { holds, situation, dealer } from the engine, or null
    busy: false,       // a /tools/discard-eval request is in flight
    error: null,       // a friendly error message + retry, or null
};

// invalidate drops a stale verdict whenever the situation it graded changes.
function invalidate() { state.result = null; state.error = null; }

function togglePick(card) {
    const i = state.picked.findIndex((c) => cardsEqual(c, card));
    if (i >= 0) state.picked.splice(i, 1);
    else if (state.picked.length < 6) state.picked.push(card);
    else return; // six already picked; deselect one first
    invalidate();
    render();
}

function setScore(key, raw) {
    const n = Number.parseInt(raw, 10);
    state[key] = Number.isInteger(n) && n >= 0 && n <= 120 ? n : null;
    invalidate();
    render();
}

const ready = () => state.picked.length === 6 && state.myScore != null && state.oppScore != null;

async function evaluate() {
    if (!ready() || state.busy) return;
    state.busy = true;
    state.error = null;
    render();

    let r;
    try {
        r = await fetch('/tools/discard-eval', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                hand: sortCards(state.picked).map(cardCode),
                dealer: state.dealer,
                my_score: state.myScore,
                opp_score: state.oppScore,
            }),
        });
    } catch {
        state.busy = false;
        state.error = 'There was a network problem reaching the engine. Check your connection and try again.';
        render();
        return;
    }
    let data;
    try {
        data = r.ok ? await r.json() : null;
    } catch {
        data = null;
    }
    state.busy = false;
    if (!data || !Array.isArray(data.holds) || !data.situation) {
        state.error = 'The engine could not evaluate that situation. Adjust the setup and try again.';
        render();
        return;
    }
    state.result = { holds: data.holds, situation: data.situation, dealer: data.dealer };
    render();
}

// --- rendering ----------------------------------------------------------------

// renderDeck is the card picker: the full deck, one row per suit with ranks
// ascending, as small card faces. Click toggles; six at most.
function renderDeck() {
    const rows = [];
    for (let suit = 0; suit < 4; suit++) {
        const cells = [];
        for (let rank = 1; rank <= 13; rank++) {
            const card = { rank, suit };
            const picked = state.picked.some((c) => cardsEqual(c, card));
            const full = !picked && state.picked.length >= 6;
            const face = cardFace(card, { extra: 'de-pick' + (picked ? ' selected' : '') + (full ? ' de-full' : '') });
            face.setAttribute('role', 'button');
            face.setAttribute('aria-pressed', picked ? 'true' : 'false');
            face.setAttribute('tabindex', '0');
            face.addEventListener('click', () => togglePick(card));
            face.addEventListener('keydown', (e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); togglePick(card); } });
            cells.push(face);
        }
        rows.push(h('div', { class: 'de-deck-row' }, ...cells));
    }
    return h('div', { class: 'de-deck', role: 'group', 'aria-label': 'Pick the six dealt cards' }, ...rows);
}

// renderChosen shows the picked cards full-size in the app-wide display order
// (rank then suit) — the hand as the user would hold it.
function renderChosen() {
    const cards = sortCards(state.picked).map((c) => cardFace(c));
    while (cards.length < 6) cards.push(h('div', { class: 'de-slot', 'aria-hidden': 'true' }));
    return h('div', { class: 'pr-hand de-chosen' }, ...cards);
}

function scoreField(label, key) {
    const input = h('input', {
        class: 'input de-score', type: 'number', min: '0', max: '120', step: '1',
        value: state[key] == null ? '' : String(state[key]),
        'aria-label': label,
        onchange: (e) => setScore(key, e.target.value),
    });
    return h('label', { class: 'de-field' }, h('span', { class: 'pr-controls-label' }, label), input);
}

function renderControls() {
    const seg = (label, on, set) => h('button', {
        type: 'button',
        class: 'pr-seg' + (on ? ' is-on' : ''),
        'aria-pressed': on ? 'true' : 'false',
        onclick: () => { state.dealer = set; invalidate(); render(); },
    }, label);
    return h('div', { class: 'de-controls' },
        scoreField('Your score', 'myScore'),
        scoreField('Opponent score', 'oppScore'),
        h('div', { class: 'de-field' },
            h('span', { class: 'pr-controls-label' }, 'Deal'),
            h('div', { class: 'pr-seg-group', role: 'group', 'aria-label': 'Who deals (the dealer owns the crib)' },
                seg('You deal (your crib)', state.dealer, true),
                seg('Opponent deals (their crib)', !state.dealer, false))));
}

function renderActions() {
    const n = state.picked.length;
    const hint = state.myScore == null || state.oppScore == null
        ? 'Scores must be whole numbers from 0 to 120.'
        : n === 6 ? 'Ready to evaluate.' : `Pick ${6 - n} more card${6 - n === 1 ? '' : 's'} from the deck.`;
    return h('div', { class: 'pr-actions' },
        h('span', { class: 'pr-hint' }, hint),
        h('button', {
            class: 'btn', type: 'button',
            onclick: () => { state.picked = dealHand(); invalidate(); render(); },
        }, 'Random hand'),
        h('button', {
            class: 'btn', type: 'button',
            ...(n > 0 ? {} : { disabled: 'disabled' }),
            onclick: () => { state.picked = []; invalidate(); render(); },
        }, 'Clear'),
        h('button', {
            class: 'btn btn-primary', type: 'button',
            ...(ready() && !state.busy ? {} : { disabled: 'disabled' }),
            onclick: () => evaluate(),
        }, state.busy ? 'Evaluating…' : 'Evaluate'));
}

// renderSituation is the plain-language read of the position: the facts derived
// from the scores plus one sentence on how they shift the discard decision.
function renderSituation() {
    const { situation, dealer } = state.result;
    return h('div', { class: 'panel de-situation' },
        h('div', { class: 'pr-controls-label' }, 'Game situation'),
        ...situationLines(situation, dealer).map((line) => h('p', { class: 'de-situation-line' }, line)),
        h('p', { class: 'de-situation-guidance' }, situationGuidance(situation, dealer)));
}

// lensNote explains the win lens's deferred state instead of showing 15 zeros.
function lensNote() {
    const { situation } = state.result;
    if (state.lens !== 'win' || situation.endgame) return null;
    return h('p', { class: 'de-lens-note' },
        'Neither player can reach 121 this deal, so the win-probability objective is not active yet: '
        + 'every hold’s win chance moves with its points, and the engine defers to the point-EV '
        + 'ranking below — which provably maximizes wins from here.');
}

function renderTable() {
    const { holds, situation } = state.result;
    const sorted = sortHolds(holds, state.lens, situation.endgame);
    const champion = holds[0]; // the server's order IS the champion's objective for this situation
    const lensCol = LENSES.find((l) => l.id === state.lens).field;
    const th = (label, field, cls = '') => h('th', {
        class: cls + (field === lensCol ? ' is-lens' : ''),
    }, label);

    const rows = sorted.map((hld, i) => {
        const isChampion = hld === champion;
        const td = (kids, field, cls = '') => h('td', { class: cls + (field === lensCol ? ' is-lens' : '') }, kids);
        return h('tr', { class: 'pr-row' + (i === 0 ? ' is-you' : '') },
            h('td', { class: 'pr-td-rank' }, String(i + 1)),
            h('td', {}, ...cardLabels(sortCards(hld.keep.map(parseCard)))),
            h('td', {}, ...cardLabels(sortCards(hld.throw.map(parseCard)))),
            td(evFmt(hld.hand_ev), 'hand_ev', 'pr-td-ev'),
            td(cribFmt(hld.crib_ev), 'crib_ev', 'pr-td-ev'),
            td(evFmt(hld.ev), 'ev', 'pr-td-ev'),
            td(situation.endgame ? pct(hld.win) : '—', 'win', 'pr-td-ev'),
            h('td', { class: 'pr-td-tag' }, isChampion ? '★ engine' : (i === 0 ? 'best' : '')));
    });

    return h('table', { class: 'pr-table de-table' },
        h('thead', {}, h('tr', {},
            h('th', {}, '#'),
            h('th', {}, 'Keep'),
            h('th', {}, 'Throw'),
            th('Hand', 'hand_ev', 'pr-td-ev'),
            th('Crib', 'crib_ev', 'pr-td-ev'),
            th('Total', 'ev', 'pr-td-ev'),
            th('Win', 'win', 'pr-td-ev'),
            h('th', {}, ''))),
        h('tbody', {}, ...rows));
}

function renderResult() {
    const tabs = LENSES.map((l) => h('button', {
        type: 'button',
        class: 'pr-seg' + (state.lens === l.id ? ' is-on' : ''),
        'aria-pressed': state.lens === l.id ? 'true' : 'false',
        onclick: () => { state.lens = l.id; render(); },
    }, l.label));
    return h('div', { class: 'panel de-result' },
        h('div', { class: 'de-lenses' },
            h('span', { class: 'pr-controls-label' }, 'Rank by'),
            h('div', { class: 'pr-seg-group', role: 'group', 'aria-label': 'Ranking lens' }, ...tabs)),
        lensNote(),
        renderTable(),
        h('p', { class: 'pr-verdict-note' },
            'Hand is the kept four’s expected show value over every starter; Crib is the thrown pair’s '
            + 'expected crib value, signed for who owns it; Total is their sum — what the champion maximizes '
            + 'mid-game. Win is the chance of winning the game with that hold, the champion’s objective '
            + 'once either player is in reach of 121. ★ marks the hold the engine would choose here.'));
}

function render() {
    const kids = [
        h('h1', { class: 'pr-title' }, 'Discard evaluator'),
        h('p', { class: 'pr-subtitle' },
            'Set up any discard situation — the six dealt cards, both scores, and who deals — and see all '
            + '15 keep/throw splits through three lenses: expected points, raw hand value, and win probability.'),
        h('div', { class: 'panel pr-board de-setup' },
            renderDeck(),
            renderChosen(),
            renderControls(),
            renderActions()),
    ];
    if (state.error) {
        kids.push(h('div', { class: 'panel pr-message' },
            h('p', { class: 'pr-message-body' }, state.error),
            h('button', { class: 'btn btn-primary', type: 'button', onclick: () => evaluate() }, 'Try again')));
    }
    if (state.result) kids.push(renderSituation(), renderResult());
    root.replaceChildren(...kids);
}

render();
