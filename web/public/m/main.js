// Mobile play prototype: tap to advance (docs/design/play-flow.md), vs the bot.
// The server resolves whole batches; flow.js holds them back and this file reveals
// them one delta at a time, stopping at each hold for a single button.
import { GameClient } from '../src/net/client.js';
import { RANK_LABELS, SUIT_SYMBOLS, cardCode, sortCards } from '../src/engine/cards.js';
import { createFlow, HOLD_LABEL, TABLE_TAP_HOLDS } from './flow.js';
import { applyDelta, cardKey, freshModel, fromSnapshot } from './model.js';
import { createStrip } from './strip.js';

const client = new GameClient();
const SAVE_KEY = 'cribbager.m.game';
const STAGGER_MS = 320; // animation pacing only; never gates state or input

const app = document.getElementById('app');
const live = document.getElementById('live');

// ---------- tiny DOM helper ----------
function h(tag, attrs = {}, ...kids) {
  const e = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs)) {
    if (v == null || v === false) continue;
    if (k === 'class') e.className = v;
    else if (k.startsWith('on')) e.addEventListener(k.slice(2), v);
    else e.setAttribute(k, v === true ? '' : v);
  }
  for (const kid of kids.flat()) if (kid != null && kid !== false) e.append(kid);
  return e;
}

// ---------- state ----------
const g = {
  id: null, token: null,
  flow: null, m: freshModel(0),
  final: null, // the server's snapshot once the current batch is fully revealed
  hold: null, // {hold, delta}
  prompt: null, // 'discard' | 'play' | null
  legal: [], // cards you may play (from the settled snapshot)
  staged: [], // cards chosen for the crib, in order chosen
  timer: null, busy: false, swallow: false, menu: false, explain: null,
};

// The board is mounted once so pegs can glide between renders.
const boardHost = h('div', { class: 'board-host' });
const strip = createStrip(boardHost);

// ---------- cards ----------
function cardEl(c, o = {}) {
  const arrived = c && g.m.arrived[cardKey(c)] === g.m.seq && o.animate !== false;
  if (!c) return h('div', { class: 'card back' + (o.enter ? ' enter' : '') });
  const red = c.suit === 1 || c.suit === 2;
  const tag = o.onclick ? 'button' : 'div';
  return h(tag, {
    class: ['card', red ? 'red' : '', arrived || o.enter ? 'enter' : '', o.dim ? 'dim' : '', o.onclick ? 'tap' : ''].filter(Boolean).join(' '),
    'aria-label': `${RANK_LABELS[c.rank]} of ${['clubs', 'diamonds', 'hearts', 'spades'][c.suit]}`,
    onclick: o.onclick,
    'data-card': cardCode(c),
  }, h('div', { class: 'corner' }, h('span', { class: 'r' }, RANK_LABELS[c.rank]), h('span', { class: 's' }, SUIT_SYMBOLS[c.suit])));
}
const fan = (cards, o = {}) => h('div', { class: 'fan' + (o.gapped ? ' gapped' : '') }, cards);
const backs = (n) => fan(Array.from({ length: Math.max(0, n) }, () => cardEl(null)));

// ---------- reveal loop ----------
function delayFor(d) {
  return d.type === 'card_played' || d.type === 'starter_cut' || d.type === 'go' ? STAGGER_MS : 0;
}

function pump(flushing = false) {
  clearTimeout(g.timer);
  g.timer = null;
  for (;;) {
    const s = g.flow.peek();
    if (s.type === 'idle') return settled();
    if (s.type === 'hold') {
      g.hold = s;
      g.prompt = null;
      render();
      announce(s.hold);
      return;
    }
    g.hold = null;
    g.flow.take();
    applyDelta(g.m, s.delta, s.next);
    render();
    const wait = delayFor(s.delta);
    if (wait && !flushing && g.flow.pending > 0) {
      g.timer = setTimeout(() => pump(), wait);
      return;
    }
  }
}

/** The queue has drained: now, and only now, the settled snapshot decides what is asked. */
function settled() {
  g.hold = null;
  g.prompt = null;
  g.legal = [];
  const v = g.final;
  if (!g.m.over && v && v.Winner == null) {
    g.m.hand = sortCards((v.YourHand || []).map(parse));
    if (v.Phase === 'discard' && v.YourHand.length === 6) g.prompt = 'discard';
    else if (v.Phase === 'play' && v.ToPlay === v.You) {
      g.prompt = 'play';
      g.legal = (v.LegalPlays || []).map(parse);
    }
  }
  g.staged = [];
  render();
}

function flush() {
  if (!g.timer) return false;
  pump(true);
  return true;
}

function announce(hold) {
  const calls = g.m.speech.filter(Boolean).join('. ');
  live.textContent = `${calls ? calls + '. ' : ''}${HOLD_LABEL[hold] || 'Tap the pack to cut'}.`;
}

function tapHold() {
  if (!g.hold) return;
  g.flow.release();
  pump();
}

// ---------- server ----------
const parse = (s) => {
  const t = s.toUpperCase();
  const rank = { A: 1, T: 10, J: 11, Q: 12, K: 13 }[t[0]] ?? parseInt(t.slice(0, -1), 10);
  return { rank, suit: 'CDHS'.indexOf(t.slice(-1)) };
};

function readInitial(id, token, upTo) {
  return new Promise((resolve, reject) => {
    const out = [];
    const es = client.stream(id, token, 0);
    const done = () => { es.close(); resolve(out); };
    const timeout = setTimeout(() => { es.close(); reject(new Error('stream timeout')); }, 8000);
    es.onmessage = (ev) => {
      const d = JSON.parse(ev.data);
      if (!d.seq) return; // roster
      out.push(d);
      if (d.seq >= upTo) { clearTimeout(timeout); done(); }
    };
    if (upTo === 0) { clearTimeout(timeout); done(); }
  });
}

async function newGame() {
  clearTimeout(g.timer);
  g.menu = false;
  g.busy = true;
  g.flow = createFlow({ mySeat: 0 });
  g.m = freshModel(0);
  g.hold = null; g.prompt = null; g.staged = []; g.final = null;
  render();
  try {
    const c = await client.create({ mode: 'bot' });
    g.id = c.game_id; g.token = c.player_token;
    if (location.search) history.replaceState(null, '', location.pathname); // a reload resumes this game
    try { localStorage.setItem(SAVE_KEY, JSON.stringify({ id: g.id, token: g.token })); } catch { /* optional */ }
    const snap = await client.snapshot(g.id, g.token);
    const deltas = await readInitial(g.id, g.token, snap.Version);
    g.flow = createFlow({ mySeat: c.seat });
    g.m = freshModel(c.seat);
    g.flow.push(deltas);
    g.final = snap;
    g.busy = false;
    pump();
  } catch (e) {
    g.busy = false;
    toast(e.message);
  }
}

async function resume() {
  let saved = null;
  try { saved = JSON.parse(localStorage.getItem(SAVE_KEY) || 'null'); } catch { /* none */ }
  if (saved && !new URLSearchParams(location.search).has('new')) {
    try {
      const v = await client.snapshot(saved.id, saved.token);
      if (v.Winner == null) {
        g.id = saved.id; g.token = saved.token;
        g.flow = createFlow({ mySeat: v.You });
        g.flow.resume(v.YourHand.length);
        g.m = fromSnapshot(v);
        g.final = v;
        settled();
        return;
      }
    } catch { /* gone: start fresh */ }
  }
  newGame();
}

async function send(action) {
  if (g.busy) return;
  g.busy = true;
  g.prompt = null; // no second tap while the move is in flight
  g.legal = [];
  render();
  try {
    const resp = await client.act(g.id, g.token, action);
    const snap = await client.snapshot(g.id, g.token);
    g.flow.push(resp.deltas);
    g.final = snap;
    g.prompt = null;
    g.staged = [];
    g.busy = false;
    pump();
  } catch (e) {
    g.busy = false;
    toast(e.message);
    render();
  }
}

function toast(msg) {
  const t = h('div', { class: 'toast', role: 'status' }, msg);
  document.body.append(t);
  setTimeout(() => t.remove(), 2600);
}

// ---------- interaction ----------
function onHandCard(c) {
  if (g.prompt === 'discard') {
    const i = g.staged.findIndex((s) => cardKey(s) === cardKey(c));
    if (i >= 0) g.staged.splice(i, 1);
    else {
      g.staged.push(c);
      if (g.staged.length > 2) g.staged.shift(); // a third replaces the earliest
    }
    render();
  } else if (g.prompt === 'play' && g.legal.some((l) => cardKey(l) === cardKey(c))) {
    send({ type: 'play', card: cardCode(c) });
  }
}

function discard() {
  if (g.staged.length === 2) send({ type: 'discard', cards: g.staged.map(cardCode) });
}

document.addEventListener('pointerdown', () => { if (flush()) g.swallow = true; }, true);
document.addEventListener('keydown', (e) => {
  if (e.key === 'Escape') { g.menu = false; g.explain = null; render(); return; }
  if (e.key !== ' ' && e.key !== 'Enter') return;
  if (e.target.closest && e.target.closest('button, a')) return;
  if (flush()) return;
  if (g.hold && g.hold.hold !== 'cut') { e.preventDefault(); tapHold(); }
});
app.addEventListener('click', (e) => {
  if (g.swallow) { g.swallow = false; return; }
  // Empty table space is a shortcut for the pure acknowledgements only.
  if (g.hold && TABLE_TAP_HOLDS.has(g.hold.hold) && !e.target.closest('button, a, .card, .show-hand, .scrim')) tapHold();
});

// ---------- render ----------
function speechEl(who) {
  return h('span', { class: 'speech ' + (who === 0 ? 'you' : 'bot') }, g.m.speech[who]);
}

function spots(owner) {
  const m = g.m;
  if (m.dealer !== owner || m.show) return h('div', { class: 'rc' });
  const col = owner === 0 ? 'you' : 'bot';
  const crib = h('div', { class: 'spot' },
    m.cribCount > 0 ? cardEl(null, {}) : h('div', { class: 'empty' }),
    h('div', { class: 'label ' + col }, 'Crib'));
  const deckOrStarter = h('div', { class: 'spot' },
    m.starter ? cardEl(m.starter) : cardEl(null),
    h('div', { class: 'label' + (m.starter ? '' : ' ' + col) }, m.starter ? 'Starter' : 'Deck'));
  return h('div', { class: 'rc' }, crib, deckOrStarter);
}

function showRow(entry, who, kind) {
  const m = g.m;
  const col = who === 0 ? 'you' : 'bot';
  const row = h('div', { class: 'row' + (who === 0 ? ' mine' : '') });
  if (!entry) return row;
  row.append(h('div', {
    class: 'show-hand', role: 'button', tabindex: '0', 'aria-label': kind + ' ' + (entry.total ?? ''),
    onclick: () => { if (entry.total != null) { g.explain = { entry, kind }; render(); } },
  },
    h('div', { class: 'main' }, fan(entry.cards.map((c) => cardEl(c, { animate: false }))), h('div', { style: 'width:18px;flex:none' }), m.starter ? cardEl(m.starter, { animate: false }) : null),
    h('div', { class: 'rc' }, entry.total != null ? h('span', { class: 'count ' + col }, String(entry.total)) : null)));
  return row;
}

function tableView() {
  const m = g.m;
  const rows = [];
  // bot hand row / bot play row / board / my play row / my hand row
  if (m.show) {
    rows.push(showRow(m.show.hands[1], 1, 'Hand'));
    rows.push(showRow(m.dealer === 1 ? m.show.crib : null, 1, 'Crib'));
    rows.push(boardHost);
    rows.push(showRow(m.show.hands[0], 0, 'Hand'));
    rows.push(showRow(m.dealer === 0 ? m.show.crib : null, 0, 'Crib'));
    return rows;
  }
  rows.push(h('div', { class: 'row' }, h('div', { class: 'main center' }, backs(m.oppCards)), spots(1)));
  rows.push(h('div', { class: 'row' }, h('div', { class: 'main' }, fan(m.played[1].map((c) => cardEl(c))), speechEl(1))));
  rows.push(boardHost);

  // my play row: the pile you have laid down, or the crib throw while discarding
  const discarding = g.prompt === 'discard';
  const mineMain = discarding
    ? h('div', { class: 'main center' }, fan(sortCards(g.staged).map((c) => cardEl(c, { animate: false, onclick: () => onHandCard(c) }))))
    : h('div', { class: 'main' }, fan(m.played[0].map((c) => cardEl(c))), speechEl(0));
  const rc = h('div', { class: 'rc' }, discarding && g.staged.length === 2
    ? h('button', { class: 'btn', onclick: discard }, h('span', { class: 'lbl' }, 'Discard')) : null);
  const myPlay = h('div', { class: 'row mine' }, mineMain, rc);
  if (discarding) myPlay.append(h('div', { class: 'crib-say ' + (m.dealer === 0 ? 'you' : 'bot') }, m.dealer === 0 ? 'YOUR CRIB' : "BOT'S CRIB"));
  rows.push(myPlay);

  const stagedKeys = g.staged.map(cardKey);
  const shown = m.hand.filter((c) => !stagedKeys.includes(cardKey(c)));
  const allDim = g.hold && g.hold.hold === 'go';
  const hand = shown.map((c) => {
    const legal = g.prompt === 'play' && g.legal.some((l) => cardKey(l) === cardKey(c));
    const dim = allDim || (g.prompt === 'play' && !legal);
    const interactive = g.prompt === 'discard' || legal;
    return cardEl(c, { dim, animate: g.m.seq === g.m.arrived[cardKey(c)], onclick: interactive ? () => onHandCard(c) : null });
  });
  rows.push(h('div', { class: 'row mine my-hand' }, h('div', { class: 'main center' }, fan(hand)), spots(0)));
  return rows;
}

function slotView() {
  const s = g.hold;
  if (!s || s.hold === 'cut') return h('div', { class: 'slot' });
  const goHint = s.hold === 'go' ? 'No card you hold fits the count' : null;
  return h('div', { class: 'slot' }, h('button', { class: 'btn', onclick: tapHold, 'aria-describedby': goHint ? 'gohint' : null },
    h('span', { class: 'lbl' }, HOLD_LABEL[s.hold]), goHint ? h('span', { id: 'gohint', class: 'sr-only' }, goHint) : null));
}

function cutView() {
  const c = g.m.cut;
  const cutting = g.hold && g.hold.hold === 'cut';
  const body = h('div', { class: 'cut' });
  if (c) {
    body.append(
      h('div', {}, h('div', {}, cardEl(c.cards[1], { enter: true })), h('div', { class: 'who bot' }, 'Bot')),
      h('div', { class: 'verdict' }, c.dealer === 0 ? 'You deal first' : 'Bot deals first'),
      h('div', {}, h('div', {}, cardEl(c.cards[0], { enter: true })), h('div', { class: 'who you' }, 'You')));
  } else {
    body.append(
      h('div', { class: 'pack', role: 'button', tabindex: '0', 'aria-label': 'Cut the pack', onclick: () => cutting && tapHold(),
        onkeydown: (e) => { if (cutting && (e.key === 'Enter' || e.key === ' ')) { e.preventDefault(); tapHold(); } } },
      h('div', { class: 'card back', style: '--cw:96px' })),
      h('div', { class: 'hint' }, g.busy || !cutting ? 'Shuffling' : 'Tap the pack to cut'));
  }
  return body;
}

function explainView() {
  const { entry, kind } = g.explain;
  const names = { fifteen: 'Fifteen', pair: 'Pair', flush: 'Flush', nobs: 'His nobs' };
  const label = (c) => c.kind === 'run' ? `Run of ${c.length}` : names[c.kind] || c.kind;
  return h('div', { class: 'scrim', onclick: () => { g.explain = null; render(); } },
    h('div', { class: 'sheet explain', role: 'dialog', 'aria-label': kind + ' breakdown' },
      fan([...entry.cards.map((c) => cardEl(c, { animate: false })), g.m.starter ? cardEl(g.m.starter, { animate: false }) : null]),
      h('ul', {}, entry.combos.map((c) => h('li', {}, h('span', {}, label(c)), h('span', {}, String(c.points))))),
      h('div', { class: 'tot' }, `${kind}: ${entry.total}`)));
}

function overView() {
  const m = g.m;
  const won = m.over.winner === 0;
  return h('div', { class: 'scrim' },
    h('div', { class: 'sheet', role: 'dialog', 'aria-label': 'Game over' },
      h('h2', {}, won ? 'You win!' : 'Bot wins'),
      h('div', { class: 'final' }, won ? `${m.scores[0]} – ${m.scores[1]}` : `${m.scores[1]} – ${m.scores[0]}`),
      h('div', { class: 'btns' },
        h('button', { class: 'btn', onclick: newGame }, h('span', { class: 'lbl' }, 'New game')),
        h('a', { href: '/' }, 'Home'))));
}

function render() {
  const m = g.m;
  const inCut = !m.over && (m.cut != null || m.dealer == null || (g.hold && (g.hold.hold === 'cut' || g.hold.hold === 'start')));
  strip.set(m.scores, m.prev);
  const head = h('div', { class: 'top' },
    h('span', { class: 'name' + (inCut ? '' : ' bot') }, inCut ? 'Cutting the pack' : 'Bot'),
    h('button', { class: 'menu-btn', 'aria-label': 'Menu', onclick: () => { g.menu = !g.menu; render(); } }, '≡'));
  const body = inCut ? h('div', { class: 'table' }, cutView()) : h('div', { class: 'table' }, tableView());
  const kids = [head, body, slotView()];
  if (g.menu) {
    kids.push(h('div', { class: 'scrim', onclick: () => { g.menu = false; render(); } },
      h('div', { class: 'sheet' }, h('div', { class: 'btns' },
        h('button', { class: 'btn', onclick: newGame }, h('span', { class: 'lbl' }, 'New game')),
        h('a', { href: '/' }, 'Home')))));
  }
  if (g.explain) kids.push(explainView());
  if (m.over) kids.push(overView());
  app.replaceChildren(...kids);
  app.dataset.hold = g.hold ? g.hold.hold : '';
  app.dataset.prompt = g.prompt || '';
  app.dataset.pending = String(g.flow ? g.flow.pending : 0);
}

render();
resume();
