// Cribbager UI (the "board in the middle" layout). A thin client: it renders the
// table, POSTs the human's discard/play to the Go server, and animates the
// server's per-seat deltas — translated into UI events that onEvent plays out
// (see the server driver near the bottom of this file). A real cribbage board
// (reused module) sits between the two seats and is advanced on every peg and
// every counted element. The opponent is a silent veteran; the only "voice" is
// functional cribbage language announced as each player plays/counts.
import { createBoard, straightBoard } from '../board/board.js';
import { cardsEqual, parseCard, sortCards, cardCode } from '../engine/cards.js';
import { cardFace } from './cardFace.js';
// AM1: the "Explain the score" overlay re-derives the show breakdown and renders
// it with the SAME combo-line renderer the Hand Counting Tutorial uses.
import { buildBreakdownLines, breakdownList } from './comboBreakdown.js';
import { GameClient } from '../net/client.js';
import { mountHeader } from './header.js';
// In-game "Evaluate game": rate this game's discards client-side (works for
// guests/bot/mp, no login) and render them with the shared analysis view.
import { buildEvaluationData } from './evaluation.js';
import { analysisBody } from './analysisRender.js';
// HUMAN/BOT are UI positions: "me" at the bottom, the opponent at the top.
const HUMAN = 0;
const BOT = 1;
const other = (p) => 1 - p; // the other of the two seats
// MY_SEAT is which *server* seat is me (0 if I created the game, 1 if I joined;
// always 0 vs the bot). uiSeat maps a server seat in a delta to its UI position,
// so all the rendering below is seat-agnostic: my moves render at the bottom and
// the opponent's at the top regardless of which server seat I hold.
let MY_SEAT = 0;
const uiSeat = (s) => (s === MY_SEAT ? HUMAN : BOT);
let oppLabel = 'Opponent'; // opponent's display name, from the roster ("players") delta
const seatName = (p) => (p === HUMAN ? 'You' : oppLabel);
// ---------- tiny DOM helper ----------
function h(tag, attrs = {}, ...kids) {
    const e = document.createElement(tag);
    for (const [k, v] of Object.entries(attrs)) {
        if (k === 'class')
            e.className = v;
        else
            e.setAttribute(k, v);
    }
    for (const kid of kids)
        if (kid != null && kid !== false) e.append(kid); // skip nullish so `cond ? node : null` is safe
    return e;
}
const delay = (ms) => new Promise((r) => setTimeout(r, ms));
const fresh = () => ({
    dealer: null,
    score: [0, 0],
    humanHand: [],
    botHandCount: 0,
    played: [[], []],
    speech: ['', ''],
    starter: null,
    cribOwner: null,
    cribCount: 0,
    count: 0,
    inPlay: false,
    discarding: false,
    selected: [],
    legal: null,
    cut: null,
    // The show now reveals every hand + the crib AT ONCE (no per-hand Continue):
    // show.hands[p] = { cards, starter, score } per UI seat, show.crib =
    // { owner, cards, starter, score }. score is null for an uncounted reveal
    // (the game ended mid-show). null show === not in the show.
    show: null,
    // over: the terminal result, shown as an in-page box (no modal). Null until the
    // game ends: { won, my, opp, skunk }.
    over: null,
});
let state = fresh();
// ---------- scaffold ----------
const app = document.getElementById('app');
const elFelt = h('div', { class: 'felt' });
const elControls = h('div', { class: 'controls' });
// Game-flow status ("waiting for Bob…", "opponent left"). It occupies the
// action-control zone: when you're waiting you have no move, so the message
// simply takes the button's place. That zone has a reserved min-height, so
// swapping between a button and this text never resizes the layout.
//
// Crucially it must NOT clobber an ACTIVE prompt/Continue button — the player
// still has to click it (the opponent can disconnect while you have a pending
// move or a "Continue" gate; destroying that button would deadlock your pump).
// The status reappears once the prompt resolves and maybePrompt runs again.
function setStatus(text) {
    if (resolveDiscard || resolvePlay || resolveContinue) return;
    elControls.replaceChildren(text ? h('div', { class: 'status' }, text) : '');
}
// Leave/quit: available throughout a game. In head-to-head it tells the server
// (so the opponent is informed they left); then it returns to the home menu.
const quitButton = h('button', { class: 'quit-toggle' }, 'Leave game');
quitButton.addEventListener('click', onQuit);
// The signed-in identity now lives in the global site header, and branding comes
// from the global wordmark — so the game view no longer needs its own title. The
// Leave control is hidden for now, so the board's footer is dropped entirely
// (quitButton stays referenced by onQuit/setChrome, just not mounted).
app.append(elFelt);
// Mount the global site header (wordmark + auth/identity). It sits above #app and
// never sets the page width, so the board layout is unaffected. We also mirror the
// auth state locally: post-game discard analysis is account-scoped (it lives under
// /users/me/...), so the "Analyze this game" link on the game-over overlay only
// appears for a signed-in participant — a guest's account-less game would 404.
let currentUser = null;
mountHeader({ onAuthChange: (u) => { currentUser = u; } });

// setChrome shows the right header controls for the current screen. The Leave
// button is hidden for now (kept in the DOM so onQuit and its wiring stay intact).
function setChrome(_mode) { // 'bot' | 'mp' | 'menu'
    quitButton.style.display = 'none';
}
// elControls lives INSIDE the player area (appended to the you-seat in render()).
// ---------- the board (reused module), mounted once ----------
const boardMount = h('div', { class: 'board-mount' });
const board = createBoard(boardMount, {
    theme: {
        ...straightBoard,
        colors: {
            ...straightBoard.colors,
            '--board-bg': '#e7d6ad',
            '--board-edge-width': '0', // no body border
            '--peg-0': '#2f6f9e', // Opponent — blue pegs (top track)
            '--peg-1': '#b8332b', // You — red pegs (bottom track)
        },
        geometry: {
            ...straightBoard.geometry,
            // keep the board's internal margin/inset (only the border line is removed,
            // via --board-edge-width: 0 above).
            straight: { ...straightBoard.geometry.straight, playerGap: 20, rowGap: 12 },
        },
    },
    animMsPerHole: 42,
    animCatchupMs: 85,
});
// Opponent rides the TOP board track (0, blue); You ride the BOTTOM track (1, red).
const boardTrack = (p) => (p === HUMAN ? 1 : 0);
// Advance the board on every score (the board is the score display — no header score).
// Cap at 121: cribbage ends the instant a player reaches the target mid-count, so
// the displayed score can never exceed it (the server is authoritative on the win).
function awardPoints(player, points) {
    if (points <= 0)
        return;
    state.score[player] = Math.min(121, state.score[player] + points);
    board.score(boardTrack(player), points);
}
// snapScore advances a player's score WITHOUT the step-by-step counting animation:
// the peg jumps straight to its new position. Used at the show, where every hand
// and the crib are revealed at once (no per-hand peg-counting animation).
function snapScore(player, points) {
    if (points <= 0)
        return;
    const track = boardTrack(player);
    const prevFront = board.getState().pegs[track].front;
    state.score[player] = Math.min(121, state.score[player] + points);
    board.setPegs(track, { front: state.score[player], back: Math.min(prevFront, state.score[player]) });
}
// renderFromView is the correctness keystone: a SOFT projection of the server's
// authoritative per-seat View onto the board. The event reducer (translate/onEvent/
// awardPoints) still produces the *animation*, but it builds the score and other
// state by ACCUMULATING per-delta increments seeded at 0 — so if a delta is ever
// delivered a different number of times to the two players (a transient SSE
// drop/dup/reconnect edge), their boards drift apart permanently ("the score is
// different for each player"). renderFromView overwrites every authoritative field
// from the snapshot, so each is self-healing at every caught-up point.
//
// Call this ONLY when the client is fully caught up to the snapshot
// (appliedSeq >= snap.Version), so the View reflects exactly the deltas we have
// applied: any difference is drift to be corrected, never a not-yet-applied event.
//
// It overwrites the authoritative fields and PRESERVES the transient ones (show,
// discarding, selected, speech, cut) so it never wipes mid-animation narration or a
// mid-selection. While a human decision is pending (discard/play prompt) it also
// leaves the hand/legal/selection alone, so it can't clobber the cards the user is
// actively choosing — belt-and-braces atop the single-flighted pump.
function renderFromView(snap) {
    const pending = resolveDiscard || resolvePlay;
    state.dealer = snap.Dealer == null ? null : uiSeat(snap.Dealer);
    state.cribOwner = state.dealer;
    state.score = uiScores(snap.Scores);
    state.botHandCount = snap.OpponentCards;
    state.played = [CS(snap.YourPlayed), CS(snap.OpponentPlayed)]; // [me, opponent]
    state.count = snap.Count;
    state.starter = snap.Phase !== 'discard' && snap.Starter ? C(snap.Starter) : null;
    state.cribCount = snap.CribCards;
    state.inPlay = snap.Phase === 'play';
    if (!pending) {
        // Only safe to re-seat the hand/legal when the user isn't mid-decision.
        state.humanHand = CS(snap.YourHand);
        state.legal = (snap.Phase === 'play' && snap.ToPlay === MY_SEAT) ? CS(snap.LegalPlays) : null;
    }
    // Snap the peg front to the truth, keeping a valid leapfrog pair (back never
    // ahead of front), so the board display matches the View exactly.
    for (const p of [HUMAN, BOT]) {
        const peg = board.getState().pegs[boardTrack(p)];
        if (peg.front !== state.score[p])
            board.setPegs(boardTrack(p), { front: state.score[p], back: Math.min(peg.back, state.score[p]) });
    }
    render();
}
// ---------- cards ----------
function makeInteractive(el, onClick, label) {
    el.setAttribute('role', 'button');
    el.setAttribute('tabindex', '0');
    el.setAttribute('aria-label', label);
    el.addEventListener('click', onClick);
    el.addEventListener('keydown', (ev) => {
        if (ev.key === 'Enter' || ev.key === ' ') {
            ev.preventDefault();
            onClick();
        }
    });
}
function cardEl(card, opts = {}) {
    // Render via the shared cardFace (same renderer the card designer uses); this
    // function only layers on the game's click interactivity.
    const e = cardFace(card, { faceDown: opts.faceDown, small: opts.small, extra: opts.cls });
    if (!opts.faceDown && card && opts.onClick) {
        const playable = opts.cls?.includes('legal') ? ', playable' : '';
        makeInteractive(e, opts.onClick, (e.getAttribute('aria-label') || '') + playable);
    }
    return e;
}
// The dealer's crib (a dashed "crib" outline, left) + starter card (right),
// shown on their pegging row, pushed to the right. No text labels.
function deckGroup(p) {
    if (state.dealer !== p || state.cribOwner === null || state.show)
        return '';
    return h('div', { class: 'deck' }, h('div', { class: 'crib-slot' }, 'crib'), state.starter ? cardEl(state.starter) : cardEl(null, { faceDown: true }));
}
// A player's pegging row: played cards, the spoken call, and (if dealer) the deck.
function peggingRow(p) {
    const row = h('div', { class: 'pegged' }, ...state.played[p].map((c) => cardEl(c)));
    if (state.speech[p])
        row.append(h('div', { class: 'call ' + (p === HUMAN ? 'you' : 'bot') }, state.speech[p]));
    const deck = deckGroup(p);
    if (deck)
        row.append(deck);
    return row;
}
// Central stage is now only the cut-for-deal.
function stageEl() {
    if (!state.cut)
        return '';
    return h('div', { class: 'stage' }, h('div', { class: 'cut-row' }, h('div', { class: 'cut-seat' }, h('div', { class: 'rail-label' }, 'Opponent'), cardEl(state.cut.cards[BOT])), h('div', { class: 'cut-seat' }, h('div', { class: 'rail-label' }, 'You'), cardEl(state.cut.cards[HUMAN]))), h('div', { class: 'cut-result' }, `${seatName(state.cut.dealer)} deal${state.cut.dealer === HUMAN ? '' : 's'} first.`));
}
// The show now reveals every hand and the crib AT ONCE: each player's four cards
// go in their own hand area, the crib in the dealer's pegging area (see render).
// showHandEls builds the face-up cards + starter + the count for ONE revealed hand
// or crib. A counted reveal shows its total (with an "Explain" affordance); an
// uncounted one (the game ended mid-show) shows the cards with no score.
function showHandEls(entry, isCrib, player) {
    const cards = sortCards(entry.cards).map((c) => cardEl(c));
    const starterEl = cardEl(entry.starter, { cls: 'show-starter' });
    const lead = isCrib ? [h('span', { class: 'crib-tag' }, 'crib')] : [];
    const kids = [...lead, ...cards, starterEl];
    if (entry.score) {
        kids.push(h('div', { class: 'show-score' }, String(entry.score.total)));
        const explain = h('button', { class: 'explain-btn' }, 'Explain');
        explain.addEventListener('click', () => openExplain({ hand: entry.cards, starter: entry.starter, isCrib, player }));
        kids.push(explain);
    } else {
        kids.push(h('div', { class: 'show-uncounted' }, 'not counted'));
    }
    return kids;
}

// ---------- the left-side versus panel + game-over results box ----------
// myName / oppName label the two seats. A guest is "Anonymous"; my own row is
// suffixed "(You)". The bot is always "Cribbager Bot".
const myName = () => `${(currentUser && currentUser.DisplayName) || 'Anonymous'} (You)`;
const oppName = () => (lastMode === 'bot' ? 'Cribbager Bot' : oppLabel);
function playerRow(p) {
    return h('div', { class: 'vs-row' },
        h('span', { class: 'vs-dot ' + (p === HUMAN ? 'you' : 'opp') }),
        h('span', { class: 'vs-name' }, p === HUMAN ? myName() : oppName()),
        h('span', { class: 'vs-score' }, String(state.score[p])));
}
// resultsBox is the in-page game-over card (no modal): outcome, final score (mine
// first), and the actions — Play again / Rematch, plus Evaluate (client-side, so
// it's offered to everyone).
function resultsBox() {
    const o = state.over;
    const skunkTxt = o.skunk === 'double' ? ' — double skunk!' : o.skunk === 'skunk' ? ' — skunk!' : '';
    const again = h('button', { class: 'primary' }, lastMode === 'mp' ? 'Rematch' : 'Play again');
    again.addEventListener('click', lastMode === 'mp' ? goNewOpen : goNewBot);
    const evalBtn = h('button', {}, 'Evaluate game');
    evalBtn.addEventListener('click', openEvaluation);
    return h('div', { class: 'panel results-box ' + (o.won ? 'won' : 'lost') },
        h('div', { class: 'results-outcome' }, (o.won ? 'You win' : 'You lose') + skunkTxt),
        h('div', { class: 'results-score' }, `${o.my} – ${o.opp}`),
        h('div', { class: 'results-actions' }, again, evalBtn),
        h('a', { class: 'results-home', href: '/' }, 'Back to home'));
}
function sidePanel() {
    return h('div', { class: 'side-panel' },
        h('div', { class: 'panel vs-box' }, playerRow(HUMAN), playerRow(BOT)),
        state.over ? resultsBox() : null);
}
// openEvaluation rates the discards of the game just played and shows the shared
// analysis view in an overlay. It runs entirely client-side (buildEvaluationData
// → POST /tools/discard-eval per hand), so it needs no login or stored result.
async function openEvaluation() {
    const body = h('div', { class: 'eval-body' }, h('div', { class: 'sq-answer-loading' }, 'Evaluating…'));
    const close = h('button', { class: 'primary' }, 'Close');
    const overlay = h('div', { class: 'overlay' });
    close.addEventListener('click', () => overlay.remove());
    overlay.addEventListener('click', (ev) => { if (ev.target === overlay) overlay.remove(); });
    overlay.append(h('div', { class: 'card-modal eval-modal' },
        h('h2', {}, 'Game evaluation'), body, h('div', {}, close)));
    document.body.append(overlay);
    try {
        const data = await buildEvaluationData(evalHands);
        body.replaceChildren(...analysisBody(data));
    } catch {
        body.replaceChildren(h('p', {}, 'Sorry — we couldn’t evaluate this game.'));
    }
}

// openExplain shows a display-only modal breaking the hand (or crib) down as
// `[cards] <combo> for <running count>`, the same style as the Hand Counting
// Tutorial. The live show deltas carry combos WITHOUT per-combo cards, so the
// breakdown is re-derived on demand via the stateless POST /tools/score-hand
// endpoint (crib:true for the crib, whose flush needs all five cards). It touches
// no game state — closing it just removes the overlay.
async function openExplain(sh) {
    const title = sh.isCrib ? 'The crib' : `${sh.player === HUMAN ? 'Your' : oppLabel + '’s'} hand`;
    const body = h('div', { class: 'explain-body' }, h('div', { class: 'sq-answer-loading' }, 'Scoring…'));
    const close = h('button', { class: 'primary' }, 'Close');
    const overlay = h('div', { class: 'overlay' });
    close.addEventListener('click', () => overlay.remove());
    overlay.addEventListener('click', (ev) => { if (ev.target === overlay) overlay.remove(); });
    overlay.append(h('div', { class: 'card-modal explain-modal' },
        h('h2', {}, title), body, h('div', {}, close)));
    document.body.append(overlay);

    const hand = sortCards(sh.hand).slice(0, 4).map(cardCode);
    const starter = cardCode(sh.starter);
    let data;
    try {
        const r = await fetch('/tools/score-hand', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ hand, starter, crib: sh.isCrib }),
        });
        if (!r.ok) throw new Error('bad status');
        data = await r.json();
    } catch {
        body.replaceChildren(h('p', {}, 'Sorry — we couldn’t load the breakdown.'));
        return;
    }
    const { lines } = buildBreakdownLines(data.combos || [], data.total || 0);
    body.replaceChildren(breakdownList(lines));
}
// ---------- main render (the board self-manages inside boardMount) ----------
// handArea builds one seat's hand area during the show (face-up cards) or, off
// the show, the seat's normal hand (face-down count for the opponent, interactive
// cards for you). cribPeg builds the pegging area: the played pile normally, or
// the crib (for its owner) during the show.
function handArea(p, youHand) {
    const sh = state.show;
    if (sh && sh.hands[p])
        return h('div', { class: 'hand show-hand' }, ...showHandEls(sh.hands[p], false, p));
    if (p === BOT)
        return h('div', { class: 'hand' }, ...Array.from({ length: state.botHandCount }, () => cardEl(null, { faceDown: true })));
    return youHand;
}
function cribPeg(p) {
    const sh = state.show;
    if (sh) {
        return sh.crib && sh.crib.owner === p
            ? h('div', { class: 'pegged show-crib' }, ...showHandEls(sh.crib, true, p))
            : h('div', { class: 'pegged' });
    }
    return peggingRow(p);
}
function render() {
    // Opponent (top): hand area then pegging area (crib during the show).
    const oppSeat = h('div', { class: 'seat opp' }, handArea(BOT), cribPeg(BOT));
    // Your hand (interactive) — built here so handArea can slot it in off the show.
    const youHand = h('div', { class: 'hand' });
    for (const card of sortCards(state.humanHand)) {
        if (state.discarding) {
            const sel = state.selected.some((c) => cardsEqual(c, card));
            youHand.append(cardEl(card, { cls: 'selectable' + (sel ? ' selected' : ''), onClick: () => toggleSelect(card) }));
        }
        else if (state.legal) {
            const legal = state.legal.some((c) => cardsEqual(c, card));
            youHand.append(cardEl(card, { cls: legal ? 'legal' : 'dim', onClick: legal ? () => playCard(card) : undefined }));
        }
        else {
            youHand.append(cardEl(card));
        }
    }
    // You (bottom): pegging area then hand. Actions always last.
    const youSeat = h('div', { class: 'seat you' }, cribPeg(HUMAN), handArea(HUMAN, youHand), elControls);
    const inGame = state.dealer !== null || state.over;
    const tableInner = h('div', { class: 'table-inner' }, oppSeat, h('div', { class: 'board-area' }, h('div', { class: 'board-row' }, boardMount)), stageEl(), youSeat);
    elFelt.replaceChildren(h('div', { class: 'table-layout' }, inGame ? sidePanel() : null, tableInner));
}
// ---------- interaction ----------
let resolveDiscard = null;
let resolvePlay = null;
let resolveContinue = null;
function toggleSelect(card) {
    const i = state.selected.findIndex((c) => cardsEqual(c, card));
    if (i >= 0)
        state.selected.splice(i, 1);
    else if (state.selected.length < 2)
        state.selected.push(card);
    renderControls();
    render();
}
function playCard(card) {
    if (!resolvePlay)
        return;
    const r = resolvePlay;
    resolvePlay = null;
    state.legal = null;
    state.humanHand = state.humanHand.filter((c) => !cardsEqual(c, card));
    r(card);
}
function confirmDiscard() {
    if (!resolveDiscard || state.selected.length !== 2)
        return;
    const chosen = state.selected.slice();
    const r = resolveDiscard;
    resolveDiscard = null;
    state.discarding = false;
    state.humanHand = state.humanHand.filter((c) => !chosen.some((s) => cardsEqual(s, c)));
    state.selected = [];
    elControls.replaceChildren();
    render();
    r(chosen);
}
function clearControls() {
    resolveContinue = resolvePlay = resolveDiscard = null;
    elControls.replaceChildren();
}
function renderControls() {
    if (!state.discarding)
        return;
    const btn = h('button', { class: 'primary' }, `Discard to ${state.dealer === HUMAN ? 'your' : "opponent's"} crib`);
    btn.disabled = state.selected.length !== 2;
    btn.addEventListener('click', confirmDiscard);
    elControls.replaceChildren(btn);
}
function button(label, onClick) {
    const btn = h('button', { class: 'primary' }, label);
    btn.addEventListener('click', onClick);
    elControls.replaceChildren(btn);
}
function waitContinue(label = 'Continue') {
    return new Promise((res) => {
        const done = () => { resolveContinue = null; res(); };
        resolveContinue = done;
        button(label + ' ▸', done);
    });
}
window.addEventListener('keydown', (e) => {
    if ((e.key === ' ' || e.key === 'Enter') && resolveContinue) {
        e.preventDefault();
        resolveContinue();
    }
});
// ---------- functional cribbage language ----------
const pegSay = (count, points) => (points > 0 ? `${count} for ${points}` : `${count}`);
// ---------- events ----------
async function onEvent(e) {
    switch (e.type) {
        case 'cutForDeal': {
            state.dealer = e.dealer;
            state.cut = { cards: e.cuts.slice(), dealer: e.dealer }; // cards indexed by player
            render();
            await new Promise((res) => button('Start game', () => { clearControls(); res(); }));
            break;
        }
        case 'deal': {
            state.dealer = e.dealer;
            state.cut = null;
            state.humanHand = e.hands[HUMAN].slice();
            state.botHandCount = 6;
            state.cribOwner = e.dealer;
            state.cribCount = 0;
            state.starter = null;
            state.played = [[], []];
            state.speech = ['', ''];
            state.count = 0;
            state.inPlay = false;
            render();
            break;
        }
        case 'discardComplete': {
            state.cribCount = 4;
            state.botHandCount = 4;
            render();
            break;
        }
        case 'starter': {
            state.starter = e.card;
            state.inPlay = true;
            if (e.heels) {
                state.speech[e.dealer] = 'his heels for 2';
                awardPoints(e.dealer, e.heels);
            }
            render();
            await delay(e.heels ? 850 : 350);
            break;
        }
        case 'pass': {
            state.speech[e.player] = 'go';
            render();
            await delay(650);
            break;
        }
        case 'play': {
            if (e.player === BOT)
                state.botHandCount = Math.max(0, state.botHandCount - 1);
            state.played[e.player].push(e.card);
            state.count = e.count;
            state.speech[e.player] = pegSay(e.count, e.points);
            state.speech[other(e.player)] = ''; // clear the other's last call
            if (e.points > 0)
                awardPoints(e.player, e.points);
            render();
            await delay(e.points > 0 ? 750 : 500);
            break;
        }
        case 'go': {
            // The player who could not continue keeps "go"; the other pegs 1.
            state.speech[e.player] = e.isLastCard ? 'one for last card' : 'one for go';
            awardPoints(e.player, e.points);
            render();
            await delay(750);
            break;
        }
        case 'resetPile': {
            await delay(250);
            state.count = 0;
            render();
            break;
        }
        case 'playDone': {
            // The play is over — wait for a click so the final call ("31 for 2", "go",
            // last card) is never skipped straight into the show.
            await waitContinue();
            break;
        }
        case 'show':
        case 'showUncounted': {
            // Every hand and the crib are revealed AT ONCE and left on screen until a
            // single Continue (handled at handComplete) or the game-over box. Each
            // 'show'/'showUncounted' event just ADDS its reveal to state.show and snaps
            // the peg to the new total (no per-hand counting animation). An uncounted
            // reveal (game ended mid-show) carries no score.
            const counted = e.type === 'show';
            const isCrib = e.which === 'crib';
            if (e.player === BOT && !isCrib)
                state.botHandCount = 0;
            state.starter = e.starter;
            state.inPlay = false;
            state.played = [[], []];
            state.speech = ['', ''];
            if (!state.show) state.show = { hands: [null, null], crib: null };
            const entry = { cards: e.hand.slice(), starter: e.starter, score: counted ? e.score : null };
            if (isCrib) { entry.owner = e.player; state.show.crib = entry; }
            else state.show.hands[e.player] = entry;
            if (counted) snapScore(e.player, e.score.total); // peg jumps straight to final
            render();
            break;
        }
        case 'handComplete': {
            // A single Continue gates the move from the show to the next deal (only if a
            // show is on screen — the opening deal has none). Then clear the reveal.
            if (state.show) {
                await waitContinue();
                state.show = null;
            }
            state.dealer = e.nextDealer;
            state.played = [[], []];
            state.speech = ['', ''];
            state.count = 0;
            state.inPlay = false;
            render();
            break;
        }
        case 'gameOver': {
            const won = e.winner === HUMAN;
            // Cribbage ends the instant a player reaches 121, so the DISPLAYED score
            // is capped there even though the engine reports the raw show total (e.g.
            // a final hand can total the winner past 121). This is a pure display
            // policy applied identically by both clients (and matching the board peg,
            // which already clamps at 121), so it introduces no divergence. Skunk
            // thresholds use the LOSER's score (always < 121), so they're unaffected.
            const shown = [Math.min(state.score[HUMAN], 121), Math.min(state.score[BOT], 121)];
            // No modal: the terminal result is an in-page box under the versus panel
            // (see resultsBox). It offers Play again / Rematch and Evaluate (the latter
            // computed client-side, so every player — guest, bot, or mp — can use it).
            // Keep any show reveal on screen beneath it. Clean up the live game like
            // endOverlay did (drop the saved entry, close the stream).
            if (curGameId) clearGame(curGameId);
            if (activeStream) { activeStream.close(); activeStream = null; }
            clearControls();
            state.over = { won, my: shown[HUMAN], opp: shown[BOT], skunk: e.skunk };
            render();
            break;
        }
    }
}
// ---------- the human as an Agent ----------
const humanAgent = {
    name: 'You',
    discard(_v) {
        state.discarding = true;
        state.selected = [];
        renderControls();
        render();
        return new Promise((res) => { resolveDiscard = res; });
    },
    play(v) {
        state.legal = v.legal.slice();
        render();
        return new Promise((res) => { resolvePlay = res; });
    },
};
// ---------- server driver ----------
// The game now runs on the Go server (mode:"bot"). This client POSTs the human's
// moves and translates the server's per-seat semantic deltas back into the solo
// event vocabulary that onEvent already animates — so all the rendering/pacing
// above is unchanged; only the data source moved off the in-browser engine.
//
// The human is seat 0. For a vs-bot game the action response already carries the
// bot's replies as deltas (the server drives the bot until it is the human's turn
// again), so v1 needs no SSE stream. The opening cut-for-deal happens before any
// action, so it is rendered from the snapshot rather than animated (a later SSE
// pass restores that intro and enables human-vs-human).
const RANK_TX = ['', 'A', '2', '3', '4', '5', '6', '7', '8', '9', 'T', 'J', 'Q', 'K'];
const cardToStr = (c) => RANK_TX[c.rank] + 'CDHS'[c.suit];
const C = (s) => parseCard(s);
const CS = (a) => (a ?? []).map(C);
// The live show delta carries the total (and combos WITHOUT per-combo cards). We
// only need the total inline; the per-combo breakdown is re-derived on demand via
// /tools/score-hand when the player opens "Explain the score" (see openExplain).
function showScore(d) {
    return { total: d.total };
}
function skunkOf(scores, winner) {
    const lose = scores[1 - winner];
    return lose < 61 ? 'double' : lose < 91 ? 'skunk' : 'none';
}
// translate maps one server delta to zero or more solo GameEvents. Synthetic
// events (discardComplete, playDone, handComplete) bridge the two vocabularies.
function translate(d, ctx) {
    switch (d.type) {
        case 'cut_for_deal':
            // Cut-for-deal animation is disabled for now — just record who deals
            // (hand_dealt carries the dealer too) and go straight to the deal.
            ctx.dealer = uiSeat(d.dealer);
            return [];
        case 'hand_dealt':
            ctx.dealer = uiSeat(d.dealer);
            ctx.starter = null;
            ctx.inPlay = false;
            // A dealt hand mid-game means the previous hand finished and a new one began.
            // d.hand is my hand (server-filtered), so it goes to the bottom seat.
            return [
                { type: 'handComplete', nextDealer: ctx.dealer },
                { type: 'deal', dealer: ctx.dealer, hands: [CS(d.hand), []] },
            ];
        case 'discarded':
            return []; // crib filling is shown via discardComplete on the starter cut
        case 'starter_cut':
            ctx.starter = C(d.card);
            ctx.inPlay = true;
            return [
                { type: 'discardComplete' },
                { type: 'starter', card: C(d.card), dealer: ctx.dealer, heels: d.points },
            ];
        case 'card_played':
            return [{ type: 'play', player: uiSeat(d.seat), card: C(d.card), count: d.count, points: d.points }];
        case 'pass':
            return [{ type: 'pass', player: uiSeat(d.seat) }];
        case 'go':
            return [{ type: 'go', player: uiSeat(d.seat), points: d.points, isLastCard: false }];
        case 'series_reset':
            return [{ type: 'resetPile' }];
        case 'hand_shown': {
            const evs = [];
            if (ctx.inPlay) {
                evs.push({ type: 'playDone' });
                ctx.inPlay = false;
            }
            evs.push({ type: 'show', player: uiSeat(d.seat), which: 'hand', hand: CS(d.cards), starter: ctx.starter, score: showScore(d) });
            return evs;
        }
        case 'crib_shown':
            return [{ type: 'show', player: ctx.dealer, which: 'crib', hand: CS(d.cards), starter: ctx.starter, score: showScore(d) }];
        case 'show_uncounted':
            // A hand or crib revealed but never scored (the game ended mid-show).
            return [{ type: 'showUncounted', player: uiSeat(d.seat), which: d.isCrib ? 'crib' : 'hand', hand: CS(d.cards), starter: ctx.starter }];
        case 'game_won': {
            const winner = uiSeat(d.seat);
            return [{ type: 'gameOver', winner, skunk: skunkOf(state.score, winner) }];
        }
        default:
            return [];
    }
}
async function animate(deltas, ctx) {
    for (const d of deltas) {
        for (const ev of translate(d, ctx)) {
            await onEvent(ev);
        }
    }
}
// Snapshot scores are server-indexed [seat0, seat1]; reindex to UI order [me, opp].
const uiScores = (sc) => (MY_SEAT === 0 ? sc : [sc[1], sc[0]]);
const discardViewFromSnap = (s) => ({
    me: HUMAN, hand: CS(s.YourHand), dealer: uiSeat(s.Dealer),
    isMyCrib: s.Dealer === MY_SEAT, scores: uiScores(s.Scores), target: 121,
});
const playViewFromSnap = (s) => ({
    me: HUMAN, hand: CS(s.YourHand), legal: CS(s.LegalPlays), starter: C(s.Starter),
    pile: CS(s.Pile), count: s.Count, myPlayed: CS(s.YourPlayed), oppPlayed: CS(s.OpponentPlayed),
    dealer: uiSeat(s.Dealer), scores: uiScores(s.Scores), target: 121,
});
// ---------- run ----------
const client = new GameClient();

// Persist active human-vs-human games, keyed by id, so a refresh or brief drop
// resumes the right one — and you can have several at once (each lives at its own
// ?game=<id> URL). An entry is dropped on game over, quit, or opponent-left.
const SAVE_KEY = 'cribbager:games';
const allSaved = () => { try { return JSON.parse(localStorage.getItem(SAVE_KEY) || '{}'); } catch { return {}; } };
const saveGame = (id, o) => { try { const m = allSaved(); m[id] = o; localStorage.setItem(SAVE_KEY, JSON.stringify(m)); } catch { /* private mode */ } };
const loadGame = (id) => allSaved()[id] || null;
const clearGame = (id) => { try { const m = allSaved(); delete m[id]; localStorage.setItem(SAVE_KEY, JSON.stringify(m)); } catch { /* ignore */ } };
let lastMode = 'bot';    // 'bot' | 'mp' — drives the game-over options (new game vs rematch)
let activeStream = null; // the live EventSource, closed when a new game starts
// evalHands captures every hand I discarded from, for the client-side "Evaluate
// game" (openEvaluation). Reset at the start of each game (startGame/startMultiplayer).
let evalHands = [];
const recordDiscard = (view, chosen) => evalHands.push({ hand: view.hand.map(cardCode), dealer: view.isMyCrib, throw: chosen.map(cardCode) });
let curGameId = null;    // the active game + my token, for the Leave button + cleanup
let curToken = null;

// hydrateFromSnapshot paints the board directly from a snapshot (no replay), used
// when rejoining a game already in progress.
function hydrateFromSnapshot(snap) {
    state = fresh();
    state.dealer = snap.Dealer == null ? null : uiSeat(snap.Dealer);
    state.cribOwner = state.dealer;
    state.score = uiScores(snap.Scores);
    state.humanHand = CS(snap.YourHand);
    state.botHandCount = snap.OpponentCards;
    state.played = [CS(snap.YourPlayed), CS(snap.OpponentPlayed)]; // [me, opponent]
    state.count = snap.Count;
    state.starter = snap.Phase !== 'discard' && snap.Starter ? C(snap.Starter) : null;
    state.inPlay = snap.Phase === 'play';
    state.cribCount = snap.Phase === 'discard' ? 0 : 4;
    render();
}

// ---- vs the bot: a synchronous loop (the server drives the bot between my moves,
// so the action response already carries the bot's replies as deltas) ----
async function startGame() {
    MY_SEAT = 0;
    oppLabel = 'Opponent';
    lastMode = 'bot';
    evalHands = [];
    setChrome('bot');
    if (activeStream) { activeStream.close(); activeStream = null; }
    setStatus(null);
    state = fresh();
    board.reset();
    clearControls();
    render();
    const created = await client.create({ mode: 'bot' });
    const { game_id, player_token } = created;
    curGameId = game_id; curToken = player_token; // for the Leave button (bot games aren't persisted/resumed)
    const ctx = { starter: null, dealer: HUMAN, inPlay: false };
    let snap = await client.snapshot(game_id, player_token);
    ctx.dealer = snap.Dealer;
    await onEvent({ type: 'deal', dealer: snap.Dealer, hands: [CS(snap.YourHand), []] });
    while (true) {
        snap = await client.snapshot(game_id, player_token);
        if (snap.Winner != null) break;
        state.humanHand = CS(snap.YourHand);
        if (snap.Phase === 'discard' && snap.YourHand.length === 6) {
            const dview = discardViewFromSnap(snap);
            const chosen = await humanAgent.discard(dview);
            recordDiscard(dview, chosen);
            const resp = await client.act(game_id, player_token, { type: 'discard', cards: chosen.map(cardToStr) });
            await animate(resp.deltas, ctx);
        } else if (snap.Phase === 'play' && snap.ToPlay === MY_SEAT) {
            const chosen = await humanAgent.play(playViewFromSnap(snap));
            const resp = await client.act(game_id, player_token, { type: 'play', card: cardToStr(chosen) });
            await animate(resp.deltas, ctx);
        } else break;
    }
}

// ---- human vs human: stream-driven ----
// Both players run this. The SSE stream is the single ordered source of game
// events (my own moves echo back too) and each is applied once, keyed by seq; my
// actions are POSTed and NOT animated locally — the stream delivers them. The
// roster ("players") delta (names + presence) is handled out of band. Game events
// are only processed once both seats are connected, so the opening cut animates
// for both at once and never hides behind the invite overlay. We prompt for my
// move only when the stream has caught up and the snapshot says it's my turn.
async function startMultiplayer(gameId, token, mySeat, opts = {}) {
    const { onBothConnected, resume = false } = opts;
    MY_SEAT = mySeat;
    oppLabel = 'Opponent';
    lastMode = 'mp';
    evalHands = [];
    setChrome('mp');
    if (activeStream) { activeStream.close(); activeStream = null; }
    curGameId = gameId; curToken = token;
    saveGame(gameId, { token, seat: mySeat });
    state = fresh();
    board.reset();
    clearControls();
    const ctx = { starter: null, dealer: HUMAN, inPlay: false };
    let oppWasPresent = false; // distinguishes "disconnected" from "not joined yet"
    const statusMessage = () => !bothConnected
        ? (oppWasPresent
            ? `${oppLabel} disconnected — waiting for them to return…`
            : `Waiting for ${oppLabel === 'Opponent' ? 'your opponent' : oppLabel} to join…`)
        : `Waiting for ${oppLabel}…`; // both here, opponent's turn
    let appliedSeq = 0;
    if (resume) {
        // Rejoin a game in progress: paint the current board from the snapshot
        // (no replay), then stream only the events that follow it.
        let snap;
        try { snap = await client.snapshot(gameId, token); }
        catch { clearGame(gameId); goHome(); return; }
        if (snap.Winner != null) { clearGame(gameId); goHome(); return; }
        hydrateFromSnapshot(snap);
        ctx.dealer = state.dealer;
        ctx.starter = state.starter;
        ctx.inPlay = state.inPlay;
        appliedSeq = snap.Version;
    } else {
        render();
    }
    const queue = [];
    let pumping = false;
    let bothConnected = false;
    let gameOver = false;

    async function maybePrompt() {
        if (gameOver) return false;
        if (!bothConnected) { setStatus(statusMessage()); return false; }
        // A failed snapshot (transient network blip) must not throw out of the pump
        // or leave it stuck mid-await — return false and let a later delta re-pump.
        let snap;
        try { snap = await client.snapshot(gameId, token); }
        catch { return false; }
        if (gameOver) return false; // may have flipped (game_won / opponent-left) during the await
        // Once we're fully caught up the View reflects exactly the deltas we've applied,
        // so project ALL authoritative state from it — healing any drift every action
        // (incl. before the final/winner snapshot), not just the score.
        if (appliedSeq >= snap.Version) renderFromView(snap);
        if (snap.Winner != null) return false;
        // Don't prompt until the animation has caught up to the snapshot — otherwise
        // we'd render the decision (e.g. the discard) before the deal/plays that led
        // to it have animated, leaving stale board state (no opponent cards, wrong
        // dealer). The roster delta can trigger a pump before the game deltas arrive.
        if (appliedSeq < snap.Version) return false;
        state.humanHand = CS(snap.YourHand);
        if (snap.Phase === 'discard' && snap.YourHand.length === 6) {
            setStatus(null);
            const dview = discardViewFromSnap(snap);
            const chosen = await humanAgent.discard(dview);
            recordDiscard(dview, chosen);
            await client.act(gameId, token, { type: 'discard', cards: chosen.map(cardToStr) });
            return true;
        }
        if (snap.Phase === 'play' && snap.ToPlay === mySeat) {
            setStatus(null);
            const chosen = await humanAgent.play(playViewFromSnap(snap));
            await client.act(gameId, token, { type: 'play', card: cardToStr(chosen) });
            return true;
        }
        setStatus(statusMessage());
        return false;
    }

    async function pump() {
        if (pumping || !bothConnected) return;
        pumping = true;
        try {
            for (;;) {
                while (queue.length) {
                    const d = queue.shift();
                    if (d.seq <= appliedSeq) continue; // each game event applied once
                    appliedSeq = d.seq;
                    if (d.type === 'game_won') {
                        gameOver = true;
                        clearGame(gameId);
                        // Source the final score (and skunk calc) from the authoritative
                        // View, not the accumulated state, so the game-over number is
                        // drift-proof — the last user-visible number off accumulation.
                        // onEvent('gameOver') below reads the now-reconciled state.score.
                        try { renderFromView(await client.snapshot(gameId, token)); }
                        catch { /* fall back to the accumulated score */ }
                    }
                    for (const ev of translate(d, ctx)) await onEvent(ev);
                }
                const acted = await maybePrompt();
                if (!acted && queue.length === 0) break; // wait for the next delta
            }
        } finally {
            pumping = false;
        }
    }

    function handleRoster(players) {
        const opp = players.find((p) => p.seat !== mySeat);
        oppLabel = opp && opp.name && opp.name.trim() ? opp.name : 'Opponent';
        // Opponent deliberately quit (distinct from a transient disconnect): end the
        // game with a terminal "they left" state and the option to start a new one.
        if (opp && opp.left && !gameOver) {
            gameOver = true;
            endOverlay(`${oppLabel} left the game`, '', 'New game', goNewOpen);
            return;
        }
        // Record the opponent's name so the homepage's in-progress list can show it.
        if (!gameOver) saveGame(gameId, { token, seat: mySeat, opp: oppLabel });
        const nowBoth = players.length === 2 && players.every((p) => p.connected);
        const was = bothConnected;
        bothConnected = nowBoth;
        if (nowBoth) oppWasPresent = true;
        if (nowBoth && !was && onBothConnected) onBothConnected();
        if (!gameOver && !nowBoth) setStatus(statusMessage());
        render();
        pump();
    }

    // On a dropped+reconnected stream the server replays every delta after our
    // cursor (Last-Event-ID / ?since — see handleStream), so the normal pump path
    // catches us back up; we deliberately do NOT hydrate from a snapshot here, which
    // would race that replay (swapping the state object mid-animation) and wedge.
    const es = client.stream(gameId, token, appliedSeq);
    activeStream = es;
    es.onmessage = (e) => {
        let d;
        try { d = JSON.parse(e.data); } catch { return; }
        if (d.type === 'players') { handleRoster(d.players || []); return; }
        queue.push(d);
        pump();
    };
    es.onerror = () => { if (!gameOver) setStatus('Reconnecting…'); };
}

// ---- entry: menu, host (share link), or join-from-link ----
function modalOverlay(...kids) {
    const ov = h('div', { class: 'overlay' });
    ov.append(h('div', { class: 'card-modal' }, ...kids));
    document.body.append(ov);
    return ov;
}
// The menu now lives on the homepage (index.html); these are navigations to it or
// to a fresh game on the game page. Leaving a game means leaving this page.
const goHome = () => { location.href = '/'; };
const goNewBot = () => { location.href = '/game.html?new=bot'; };
const goNewOpen = () => { location.href = '/game.html?new=open'; };

// onQuit leaves the current game from the Leave button. In head-to-head it tells
// the server (so the opponent learns you left), then returns to the homepage.
async function onQuit() {
    if (!confirm('Leave this game?')) return;
    if (lastMode === 'mp' && curGameId && curToken) {
        try { await client.abandon(curGameId, curToken); } catch { /* best effort */ }
    }
    if (curGameId) clearGame(curGameId);
    goHome();
}

// endOverlay is the terminal modal (game over / opponent-left): a primary action
// (rematch / new game, a navigation) plus Menu (home). `extra`, when given, is an
// extra node rendered below the buttons (used for the post-game "Analyze" link).
function endOverlay(title, sub, primaryLabel, onPrimary, extra) {
    clearControls();
    if (curGameId) clearGame(curGameId);
    if (activeStream) { activeStream.close(); activeStream = null; }
    const overlay = h('div', { class: 'overlay' });
    const primary = h('button', { class: 'primary' }, primaryLabel);
    primary.addEventListener('click', onPrimary);
    const menu = h('button', {}, 'Menu');
    menu.style.marginLeft = '8px';
    menu.addEventListener('click', goHome);
    overlay.append(h('div', { class: 'card-modal' },
        h('h2', {}, title),
        sub ? h('p', {}, sub) : '',
        h('div', {}, primary, menu),
        extra || ''));
    document.body.append(overlay);
}

// hostGame opens a private `open` game and seats the host as the waiting host.
// The game is reachable only by its shareable link ("Challenge a friend"), so the
// copy-link is the point — the host sends it to whoever they want to play.
async function hostGame() {
    state = fresh(); board.reset(); clearControls(); render();
    setChrome('mp');
    setStatus('Waiting for your opponent to join…');
    const created = await client.create({ mode: 'open' });
    const { game_id, player_token } = created;
    history.replaceState(null, '', `?game=${game_id}`); // this game lives at its own URL
    // The game id IS the join credential now: share the link or just the id.
    const link = `${location.origin}/game.html?join=${game_id}`;
    const linkBox = h('input', { type: 'text', readonly: 'true', value: link });
    linkBox.style.cssText = 'display:block;margin:10px auto;padding:8px;width:95%;box-sizing:border-box;font-size:12px;';
    linkBox.addEventListener('focus', () => linkBox.select());
    const copy = h('button', { class: 'primary' }, 'Copy link');
    copy.addEventListener('click', async () => { try { await navigator.clipboard.writeText(link); copy.textContent = 'Copied!'; } catch { linkBox.focus(); } });
    const ov = modalOverlay(h('h2', {}, 'Challenge a friend'),
        h('p', {}, 'Send this link, or share the game ID below — any client can join with it. The game begins when they join.'),
        linkBox, h('p', { class: 'gameid' }, 'Game ID: ' + game_id), copy);
    startMultiplayer(game_id, player_token, 0, { onBothConnected: () => ov.remove() }); // dismiss once they connect
}

// joinGame claims the open seat and drops straight into the game — the game id is
// the only credential, and guests play anonymously, so there is nothing to prompt.
async function joinGame(gameId) {
    setChrome('mp');
    try {
        const res = await client.join(gameId);
        history.replaceState(null, '', `?game=${gameId}`); // move to this game's URL
        startMultiplayer(gameId, res.player_token, res.seat, {});
    } catch (e) {
        endOverlay('Could not join', e.message, 'Home', goHome);
    }
}

// boot routes the game page from the URL. There is no in-page menu anymore — an
// unrecognized/empty URL bounces to the homepage.
function boot() {
    const params = new URLSearchParams(location.search);
    const newMode = params.get('new');
    if (newMode === 'bot') { startGame(); return; }
    // "Challenge a friend" hosts a private open game reachable only by its link.
    if (newMode === 'open') { hostGame(); return; }
    const join = params.get('join'); // a game id to join (or resume, if it's already ours)
    if (join) {
        const mine = loadGame(join);
        if (mine && mine.token) { startMultiplayer(join, mine.token, mine.seat, { resume: true }); return; }
        joinGame(join);
        return;
    }
    const gameId = params.get('game');
    if (gameId) {
        const saved = loadGame(gameId);
        if (saved && saved.token) {
            // Resume after a refresh/drop (falls back to the homepage if it's gone —
            // handled inside startMultiplayer).
            startMultiplayer(gameId, saved.token, saved.seat, { resume: true });
            return;
        }
    }
    goHome(); // nothing actionable on the game page → homepage
}

// The dev render-test page (dev/render-test.html) sets this flag before importing
// this module so it can drive the EXACT render path (renderFromView → render) with
// arbitrary Views, without booting a live game (which would navigate away). Only
// the render harness is exposed; the game loop is untouched.
if (typeof window !== 'undefined' && window.__CRIBBAGER_NO_BOOT__) {
    window.__cribbagerRenderTest = {
        renderFromView,
        reset: () => { state = fresh(); board.reset(); render(); },
        setSeat: (s) => { MY_SEAT = s; },
    };
} else {
    boot();
}
