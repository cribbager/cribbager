// The table as the player has seen it so far: a fold over the revealed deltas.
// Index 0 is you, 1 is the bot (UI positions, not server seats). Pure.
import { parseCard, sortCards } from '../src/engine/cards.js';

const WIN = 121;

export function freshModel(mySeat = 0) {
  return {
    mySeat,
    dealer: null, // UI index of the dealer
    scores: [0, 0],
    prev: [0, 0], // score before each player's last move: the trailing peg
    hand: [], // my cards in hand (revealed)
    oppCards: 0,
    played: [[], []], // cards laid down this hand, in order
    speech: ['', ''],
    count: 0,
    closed: false, // a count has closed; calls stay until the next card lands
    starter: null,
    cribCount: 0,
    cut: null, // {cards:[mine, bot], dealer}
    show: null, // {hands:[?,?], crib:?}; each {cards,total,combos}
    over: null, // {winner}
    seq: 0, // bumped per reveal; cards stamped with the seq they arrived in animate
    arrived: {}, // card key -> seq, for the enter animation
  };
}

export const cardKey = (c) => c.rank * 10 + c.suit;
const ui = (m, seat) => (seat === m.mySeat ? 0 : 1);
const C = (s) => (typeof s === 'string' ? parseCard(s) : s);
const CS = (a) => (a || []).map(C);

function award(m, who, pts) {
  if (!pts) return;
  m.prev[who] = m.scores[who];
  m.scores[who] = Math.min(WIN, m.scores[who] + pts);
}

function clearCalls(m) {
  m.speech = ['', ''];
  m.count = 0;
  m.closed = false;
}

function say(m, who, text) {
  m.speech[who] = text;
}

/** Fold one revealed delta into the model. `next` is the delta queued after it. */
export function applyDelta(m, d, next) {
  m.seq += 1;
  switch (d.type) {
    case 'cut_for_deal':
      m.cut = { cards: [C(d.cut[m.mySeat]), C(d.cut[1 - m.mySeat])], dealer: ui(m, d.dealer) };
      m.dealer = m.cut.dealer;
      break;
    case 'hand_dealt':
      m.cut = null;
      m.show = null;
      m.dealer = ui(m, d.dealer);
      m.hand = sortCards(CS(d.hand));
      m.oppCards = d.opponentCards;
      m.played = [[], []];
      m.starter = null;
      m.cribCount = 0;
      clearCalls(m);
      break;
    case 'discarded':
      m.cribCount += 2;
      if (d.seat === m.mySeat) {
        const gone = CS(d.cards).map(cardKey);
        m.hand = m.hand.filter((c) => !gone.includes(cardKey(c)));
      } else m.oppCards -= 2;
      break;
    case 'starter_cut':
      m.starter = C(d.card);
      m.arrived[cardKey(m.starter)] = m.seq;
      if (d.points) {
        award(m, m.dealer, d.points);
        say(m, m.dealer, `HIS HEELS FOR ${d.points}`);
      }
      break;
    case 'card_played': {
      const who = ui(m, d.seat);
      const card = C(d.card);
      if (m.closed) clearCalls(m); // the closed count resets as the next card lands
      m.played[who].push(card);
      m.arrived[cardKey(card)] = m.seq;
      if (who === 0) m.hand = m.hand.filter((c) => cardKey(c) !== cardKey(card));
      else m.oppCards -= 1;
      m.count = d.count;
      say(m, who, d.points ? `${d.count} FOR ${d.points}` : `${d.count}`);
      // A play answers the other player's call, except a "Go", which stays.
      if (!/^GO/.test(m.speech[1 - who])) m.speech[1 - who] = '';
      if (d.count === 31) m.closed = true;
      award(m, who, d.points);
      break;
    }
    case 'pass':
      say(m, ui(m, d.seat), 'GO');
      break;
    case 'go': {
      const who = ui(m, d.seat);
      const last = m.hand.length === 0 && m.oppCards === 0;
      say(m, who, `${last ? 'LAST CARD' : 'GO'} FOR ${d.points}`);
      award(m, who, d.points);
      m.closed = true;
      break;
    }
    case 'series_reset':
      // Bot leads next (you tapped Next): clear now. You lead: your card clears it.
      if (next && next.type === 'card_played' && next.seat !== m.mySeat) clearCalls(m);
      else m.closed = true;
      break;
    case 'hand_shown': {
      if (!m.show) {
        m.show = { hands: [null, null], crib: null };
        clearCalls(m);
      }
      const who = ui(m, d.seat);
      m.show.hands[who] = { cards: sortCards(CS(d.cards)), total: d.total, combos: d.combos || [] };
      award(m, who, d.total);
      break;
    }
    case 'crib_shown':
      if (!m.show) m.show = { hands: [null, null], crib: null };
      m.show.crib = { cards: sortCards(CS(d.cards)), total: d.total, combos: d.combos || [] };
      award(m, m.dealer, d.total);
      break;
    case 'show_uncounted': {
      if (!m.show) m.show = { hands: [null, null], crib: null };
      const e = { cards: sortCards(CS(d.cards)), total: null, combos: [] };
      if (d.isCrib) m.show.crib = e;
      else m.show.hands[ui(m, d.seat)] = e;
      break;
    }
    case 'game_won':
      m.over = { winner: ui(m, d.seat) };
      break;
  }
  return m;
}

/** Paint the table straight from a snapshot (reload: no replay, no holds). */
export function fromSnapshot(view) {
  const m = freshModel(view.You);
  const me = view.You;
  m.dealer = view.Dealer === me ? 0 : 1;
  m.scores = [view.Scores[me], view.Scores[1 - me]];
  m.prev = [...m.scores];
  m.hand = sortCards(CS(view.YourHand));
  m.oppCards = view.OpponentCards;
  m.played = [CS(view.YourPlayed), CS(view.OpponentPlayed)];
  m.starter = view.Starter ? C(view.Starter) : null;
  m.cribCount = view.CribCards;
  m.count = view.Count;
  return m;
}
