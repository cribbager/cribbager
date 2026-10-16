// The reveal queue for the tap-to-advance play flow (docs/design/play-flow.md).
//
// The server resolves a whole batch of engine events per action (your move, the
// bot's replies, the show, even the next deal). The client holds that batch here
// and reveals it one delta at a time. At certain deltas the queue stops and asks
// for a tap: that stop is a "hold", and each hold maps to exactly one button.
// Pure and DOM-free so the rules can be unit tested.
//
// holds: cut (tap the pack, no button) | start | go | next | count | nextHand |
//        deal | continue

export const HOLD_LABEL = {
  start: 'Start game',
  go: 'Go',
  next: 'Next',
  count: 'Count',
  nextHand: 'Next hand',
  deal: 'Deal',
  continue: 'Continue',
};

// Holds that merely acknowledge: a tap on empty table space does the same as the
// button. Never Go (a call), start, deal or continue (commitments).
export const TABLE_TAP_HOLDS = new Set(['next', 'count', 'nextHand']);

export function createFlow({ mySeat = 0 } = {}) {
  /** @type {object[]} */
  let queue = [];
  let released = false; // the head delta's hold has been tapped through
  // Revealed-so-far facts the hold rules need (never the server's current state).
  const st = {
    hand: 0, // cards still in my hand, as revealed
    shown: 0, // counts turned over this hand (hands + crib)
    oppGo: false, // the opponent has said go in the count now open
    cutSeen: false,
    started: false,
  };

  const opp = (s) => s !== mySeat;

  function holdFor(d, rest) {
    switch (d.type) {
      case 'cut_for_deal':
        return 'cut';
      case 'hand_dealt':
        if (st.cutSeen && !st.started) return 'start';
        return st.shown > 0 ? 'deal' : null;
      case 'pass':
        // Go is only said holding cards, and only when the opponent has not
        // already said it (then the count simply closes with your point).
        return d.seat === mySeat && st.hand > 0 && !st.oppGo ? 'go' : null;
      case 'series_reset': {
        const n = rest[0];
        return n && n.type === 'card_played' && opp(n.seat) ? 'next' : null;
      }
      case 'hand_shown':
        return st.shown === 0 ? 'count' : 'nextHand';
      case 'crib_shown':
        return 'nextHand';
      case 'game_won':
        return 'continue';
      default:
        return null;
    }
  }

  function track(d) {
    switch (d.type) {
      case 'cut_for_deal':
        st.cutSeen = true;
        break;
      case 'hand_dealt':
        st.hand = (d.hand || []).length;
        st.shown = 0;
        st.oppGo = false;
        if (st.cutSeen) st.started = true;
        break;
      case 'discarded':
        if (d.seat === mySeat) st.hand -= (d.cards || []).length;
        break;
      case 'card_played':
        if (d.seat === mySeat) st.hand -= 1;
        break;
      case 'pass':
        if (opp(d.seat)) st.oppGo = true;
        break;
      case 'series_reset':
        st.oppGo = false;
        break;
      case 'hand_shown':
      case 'crib_shown':
        st.shown += 1;
        break;
    }
  }

  return {
    /** Append a server batch. The winning delta is moved ahead of any uncounted
     *  reveals so the winning count is the last thing on the table before Continue
     *  and the leftovers show behind the game-over card. */
    push(deltas) {
      queue.push(...deltas);
      const w = queue.findIndex((d) => d.type === 'game_won');
      if (w > 0) {
        let first = queue.findIndex((d) => d.type === 'show_uncounted');
        if (first >= 0 && first < w) {
          const [won] = queue.splice(w, 1);
          queue.splice(first, 0, won);
        }
      }
    },
    /** What happens next: {type:'idle'} | {type:'hold', hold, delta} |
     *  {type:'reveal', delta, next}. `next` is the delta after it, if any. */
    peek() {
      const d = queue[0];
      if (!d) return { type: 'idle' };
      const rest = queue.slice(1);
      if (!released) {
        const hold = holdFor(d, rest);
        if (hold) return { type: 'hold', hold, delta: d };
      }
      return { type: 'reveal', delta: d, next: rest[0] };
    },
    /** The player tapped the current hold's button. */
    release() {
      released = true;
    },
    /** Pop the head delta (a reveal, or a hold already released). */
    take() {
      const d = queue.shift();
      released = false;
      if (d) track(d);
      return d;
    },
    get pending() {
      return queue.length;
    },
    get state() {
      return { ...st };
    },
    /** Resume from a snapshot (a reload): there is no queue, only the hand. */
    resume(handCount) {
      queue = [];
      released = false;
      st.hand = handCount;
      st.shown = 0;
      st.oppGo = false;
      st.cutSeen = false;
      st.started = true;
    },
  };
}
