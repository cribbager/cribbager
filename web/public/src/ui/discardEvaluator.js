// Discard evaluator (T1) — a standalone tool page, NOT a live game. The user sets
// up a discard situation (six dealt cards, both scores, whose crib) and sees how
// the engine evaluates it through four lenses side by side: point EV (the
// champion's mid-game objective), max hand (expected hand value alone, for when
// only your own hand can save you), win probability (the champion's endgame
// objective), and upside (the right-tail chance of a big hand). One request, one
// set of 15 holds — the lenses are re-sorts of the same rows (evaluatorLenses.js),
// never recomputations, and each hold carries its full hand-score distribution,
// charted below the table.
//
// It reuses the shared card model/renderer (cards.js + cardFace.js) and the
// engine itself via POST /tools/discard-eval with scores (eval.RankDiscardsWin
// on the server); no scoring is reimplemented here.

import { mountHeader } from './header.js';
import { cardFace } from './cardFace.js';
import { parseCard, cardCode, cardsEqual, sortCards, RANK_LABELS, SUIT_SYMBOLS } from '../engine/cards.js';
import {
    LENSES, sortHolds, situationLines, situationGuidance, pct,
    BIG_HAND_THRESHOLD, distBars, distDomainMax,
} from './evaluatorLenses.js';

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

// svg builds a namespaced SVG element (h() only makes HTML nodes) — same
// attribute style, minus the on*/class shortcuts, which the charts don't need.
const SVGNS = 'http://www.w3.org/2000/svg';
function svg(tag, attrs = {}, ...kids) {
    const e = document.createElementNS(SVGNS, tag);
    for (const [k, v] of Object.entries(attrs)) if (v != null) e.setAttribute(k, String(v));
    for (const kid of kids.flat()) if (kid != null && kid !== false) e.append(kid);
    return e;
}

// topRoundedBar is a column path with rounded top corners and a square baseline —
// the mark spec's "4px rounded data-end, square at the baseline". x,w are the bar's
// left and width; yTop is the cap; yBase is the baseline.
function topRoundedBar(x, w, yTop, yBase, r = 2) {
    const rr = Math.min(r, w / 2, Math.max(0, yBase - yTop));
    return `M${x},${yBase} L${x},${yTop + rr} Q${x},${yTop} ${x + rr},${yTop}`
        + ` L${x + w - rr},${yTop} Q${x + w},${yTop} ${x + w},${yTop + rr} L${x + w},${yBase} Z`;
}

const root = document.getElementById('discard-evaluator');
mountHeader();

// isRed mirrors cardFace's own suit colouring (diamonds, hearts).
const isRed = (c) => c.suit === 1 || c.suit === 2;
// evFmt shows an EV to two decimals (the wire value is already rounded to 4).
const evFmt = (n) => Number(n).toFixed(2);
// cribFmt keeps the crib term's sign visible — it is what flips with the crib owner.
const cribFmt = (n) => (n >= 0 ? '+' : '−') + Math.abs(n).toFixed(2);
// pctInt shows a probability as a whole-percent (the upside column and bar labels
// don't need a decimal; pct() keeps one where win probability warrants it).
const pctInt = (p) => `${Math.round(p * 100)}%`;

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
    lens: 'ev',        // active lens: 'ev' | 'hand' | 'win' | 'upside'
    result: null,      // { holds, situation, dealer } from the engine, or null
    selected: null,    // the hold whose distribution chart is expanded, or null (= top of view)
    busy: false,       // a /tools/discard-eval request is in flight
    error: null,       // a friendly error message + retry, or null
};

// invalidate drops a stale verdict whenever the situation it graded changes.
function invalidate() { state.result = null; state.error = null; state.selected = null; }

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

// --- distribution charts ------------------------------------------------------
// The hand-score distribution is the "upside" signal EV hides: a compact histogram
// of P(hand score = s) over every starter, so a tight, safe keep and a fat-tailed
// one read apart at a glance. Both charts are a single series (magnitude by score),
// so one accent hue, no legend; scores ≥ BIG_HAND_THRESHOLD are the "big hand" tail,
// marked by an annotation band + line (not a second colour). Colours come from CSS
// tokens (.de-bar / .de-tail / .de-rule) so the chart tracks the page theme.

// miniHistogram is the per-row sparkline: a bare shape, no axis, drawn on the
// shared domain (0..domainMax) so all 15 rows compare on one x-scale.
function miniHistogram(hold, domainMax) {
    const { bars, maxP } = distBars(hold.hand_dist, domainMax);
    const W = 108, H = 30, slot = W / bars.length, bw = Math.max(1, slot - 1);
    const marks = bars.map((b) => {
        if (b.p <= 0) return null;
        const barH = maxP > 0 ? (b.p / maxP) * (H - 2) : 0;
        return svg('rect', {
            class: 'de-bar', x: (b.score * slot + (slot - bw) / 2).toFixed(2), y: (H - barH).toFixed(2),
            width: bw.toFixed(2), height: barH.toFixed(2), rx: Math.min(1.5, bw / 2),
        });
    });
    // A faint tick under where the big-hand tail begins, for orientation.
    const tx = BIG_HAND_THRESHOLD * slot;
    const tail = tx <= W ? svg('line', { class: 'de-rule de-rule-faint', x1: tx.toFixed(2), y1: 0, x2: tx.toFixed(2), y2: H }) : null;
    return svg('svg', {
        class: 'de-mini', viewBox: `0 0 ${W} ${H}`, width: W, height: H,
        preserveAspectRatio: 'none', role: 'img',
        'aria-label': `Hand-score shape: ${pctInt(hold.hand_p_ge_12)} chance of 12 or more, up to ${hold.hand_ceiling}.`,
    }, tail, ...marks);
}

// distChart is the expanded chart for the selected hold: the full histogram with a
// score axis, the mean marked, and the big-hand tail annotated.
function distChart(hold, domainMax) {
    const { bars, maxP } = distBars(hold.hand_dist, domainMax);
    const M = { t: 24, r: 12, b: 26, l: 12 };
    const slot = 22, plotW = bars.length * slot, plotH = 128;
    const W = M.l + plotW + M.r, H = M.t + plotH + M.b;
    const yBase = M.t + plotH;
    const xOf = (s) => M.l + s * slot;
    const yOf = (p) => (maxP > 0 ? yBase - (p / maxP) * plotH : yBase);

    const kids = [];
    // Big-hand tail band (annotation, not a data mark) from the threshold to the end.
    const bandX = xOf(BIG_HAND_THRESHOLD);
    if (bandX < M.l + plotW) {
        kids.push(svg('rect', { class: 'de-tail', x: bandX.toFixed(2), y: M.t, width: (M.l + plotW - bandX).toFixed(2), height: plotH }));
        kids.push(svg('text', { class: 'de-anno', x: (bandX + 4).toFixed(2), y: (M.t - 8).toFixed(2) }, `${BIG_HAND_THRESHOLD}+ big hands`));
    }
    // Baseline.
    kids.push(svg('line', { class: 'de-axis', x1: M.l, y1: yBase, x2: (M.l + plotW).toFixed(2), y2: yBase }));
    // Bars (skip zero-probability scores) with a value label on the tallest.
    const bw = Math.min(16, slot - 2);
    for (const b of bars) {
        if (b.p <= 0) continue;
        const x = xOf(b.score) + (slot - bw) / 2, yTop = yOf(b.p);
        kids.push(svg('path', { class: 'de-bar', d: topRoundedBar(x, bw, yTop, yBase, 4) }));
        if (b.p === maxP) {
            kids.push(svg('text', { class: 'de-bar-label', x: (x + bw / 2).toFixed(2), y: (yTop - 6).toFixed(2), 'text-anchor': 'middle' }, pctInt(b.p)));
        }
    }
    // Mean marker (hand_ev sits between bars — the point EV of this hold's hand).
    const meanX = xOf(hold.hand_ev + 0.5); // +0.5 centres on the score's slot
    if (hold.hand_ev <= domainMax) {
        kids.push(svg('line', { class: 'de-rule de-mean', x1: meanX.toFixed(2), y1: M.t - 2, x2: meanX.toFixed(2), y2: yBase }));
        kids.push(svg('text', { class: 'de-anno de-anno-mean', x: meanX.toFixed(2), y: (yBase + 22).toFixed(2), 'text-anchor': 'middle' }, `avg ${evFmt(hold.hand_ev)}`));
    }
    // X ticks every 5 points (plus the endpoints), centred under each score slot.
    for (let s = 0; s <= domainMax; s++) {
        if (s % 5 !== 0 && s !== domainMax) continue;
        kids.push(svg('text', { class: 'de-tick', x: (xOf(s) + slot / 2).toFixed(2), y: (yBase + 14).toFixed(2), 'text-anchor': 'middle' }, String(s)));
    }
    return svg('svg', {
        class: 'de-chart', viewBox: `0 0 ${W} ${H}`, width: W, height: H, role: 'img',
        'aria-label': `Distribution of the kept hand's show score over every starter: `
            + `average ${evFmt(hold.hand_ev)}, ${pctInt(hold.hand_p_ge_12)} chance of ${BIG_HAND_THRESHOLD} or more, `
            + `90th percentile ${hold.hand_p90}, ceiling ${hold.hand_ceiling}.`,
    }, ...kids);
}

// distSummary is the metrics strip under the expanded chart — the derived upside
// numbers spelled out, so the chart's shape has words attached.
function distSummary(hold) {
    const stat = (label, value) => h('div', { class: 'de-stat' },
        h('span', { class: 'de-stat-value' }, value),
        h('span', { class: 'de-stat-label' }, label));
    return h('div', { class: 'de-stats' },
        stat('average', evFmt(hold.hand_ev)),
        stat(`chance of ${BIG_HAND_THRESHOLD}+`, pctInt(hold.hand_p_ge_12)),
        stat('90th pct', String(hold.hand_p90)),
        stat('ceiling', String(hold.hand_ceiling)));
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
    const domainMax = distDomainMax(holds);
    const selected = selectedHold();
    const lensCol = LENSES.find((l) => l.id === state.lens).field;
    const th = (label, field, cls = '') => h('th', {
        class: cls + (field === lensCol ? ' is-lens' : ''),
    }, label);

    const rows = sorted.map((hld, i) => {
        const isChampion = hld === champion;
        const td = (kids, field, cls = '') => h('td', { class: cls + (field === lensCol ? ' is-lens' : '') }, kids);
        return h('tr', {
            class: 'pr-row de-row' + (i === 0 ? ' is-you' : '') + (hld === selected ? ' is-selected' : ''),
            role: 'button', tabindex: '0', 'aria-pressed': hld === selected ? 'true' : 'false',
            'aria-label': 'Show the score distribution for this hold',
            onclick: () => { state.selected = hld; render(); },
            onkeydown: (e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); state.selected = hld; render(); } },
        },
        h('td', { class: 'pr-td-rank' }, String(i + 1)),
        h('td', {}, ...cardLabels(sortCards(hld.keep.map(parseCard)))),
        h('td', {}, ...cardLabels(sortCards(hld.throw.map(parseCard)))),
        td(evFmt(hld.hand_ev), 'hand_ev', 'pr-td-ev'),
        td(cribFmt(hld.crib_ev), 'crib_ev', 'pr-td-ev'),
        td(evFmt(hld.ev), 'ev', 'pr-td-ev'),
        td(situation.endgame ? pct(hld.win) : '—', 'win', 'pr-td-ev'),
        td(pctInt(hld.hand_p_ge_12), 'hand_p_ge_12', 'pr-td-ev'),
        h('td', { class: 'de-td-shape' }, miniHistogram(hld, domainMax)),
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
            th(`${BIG_HAND_THRESHOLD}+`, 'hand_p_ge_12', 'pr-td-ev'),
            h('th', {}, 'Shape'),
            h('th', {}, ''))),
        h('tbody', {}, ...rows));
}

// selectedHold is the hold whose distribution is expanded below the table: the
// user's clicked row if it is still in the current result, else the top of the
// active lens's ranking (so the panel always shows the best hold by default).
function selectedHold() {
    const { holds, situation } = state.result;
    if (state.selected && holds.includes(state.selected)) return state.selected;
    return sortHolds(holds, state.lens, situation.endgame)[0];
}

// renderDistribution is the expanded chart panel for the selected hold: the full
// histogram, its metrics strip, and one line reading the shape.
function renderDistribution() {
    const { holds } = state.result;
    const hold = selectedHold();
    const domainMax = distDomainMax(holds);
    const keep = cardLabels(sortCards(hold.keep.map(parseCard)));
    return h('div', { class: 'panel de-dist' },
        h('div', { class: 'de-dist-head' },
            h('span', { class: 'pr-controls-label' }, 'Score distribution'),
            h('span', { class: 'de-dist-keep' }, 'Keep ', ...keep)),
        h('div', { class: 'de-chart-wrap' }, distChart(hold, domainMax)),
        distSummary(hold),
        h('p', { class: 'de-dist-note' },
            'Each bar is the chance the kept four score that many points once the starter is cut. '
            + `The shaded band is the “big hand” tail (${BIG_HAND_THRESHOLD}+); the vertical rule marks the average. `
            + 'Two holds can share an average yet differ wildly in tail — that fat right tail is the upside you '
            + 'want when only a big hand can count you out. Click any row to inspect its shape.'));
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
            + `once either player is in reach of 121. ${BIG_HAND_THRESHOLD}+ is the chance the hand alone scores `
            + `${BIG_HAND_THRESHOLD} or more once the starter is cut — the upside the Upside lens ranks by — and `
            + 'Shape sketches the full score distribution behind it. ★ marks the hold the engine would choose here.'),
        renderDistribution());
}

function render() {
    const kids = [
        h('h1', { class: 'pr-title' }, 'Discard evaluator'),
        h('p', { class: 'pr-subtitle' },
            'Set up any discard situation — the six dealt cards, both scores, and who deals — and see all '
            + '15 keep/throw splits through four lenses: expected points, raw hand value, win probability, and '
            + 'upside — the chance of a big hand, with each hold’s full score distribution charted.'),
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
