// The single source of how a playing card looks in the app. The game UI and the
// card designer both render through this (paired with cardFace.css), so they can
// never drift — same idea as the board: one renderer, used everywhere.
import { cardId, RANK_LABELS, SUIT_NAMES, SUIT_SYMBOLS } from '../engine/cards.js';

const isRed = (c) => c.suit === 1 || c.suit === 2; // diamonds, hearts

// cardFace builds the DOM for one card and returns the <div>. opts:
//   faceDown — render the back; small — the .sm size; extra — extra class names
//   (e.g. 'selectable selected', 'legal', 'dim') the caller layers on.
// The card is a face-up div with a corner index (rank over suit) and one large
// centre pip — the current game card, verbatim.
export function cardFace(card, opts = {}) {
  const faceDown = opts.faceDown || !card;
  const e = document.createElement('div');
  e.className = ['card', opts.small && 'sm', faceDown ? 'facedown' : isRed(card) ? 'red' : 'black', opts.extra]
    .filter(Boolean)
    .join(' ');
  if (faceDown) {
    e.setAttribute('aria-hidden', 'true');
    return e;
  }
  e.setAttribute('data-cardid', String(cardId(card)));
  const corner = document.createElement('div');
  corner.className = 'corner';
  corner.innerHTML = `<span class="r">${RANK_LABELS[card.rank]}</span><span class="s">${SUIT_SYMBOLS[card.suit]}</span>`;
  const pip = document.createElement('div');
  pip.className = 'pip';
  pip.textContent = SUIT_SYMBOLS[card.suit];
  e.append(corner, pip);
  e.setAttribute('aria-label', `${RANK_LABELS[card.rank]} of ${SUIT_NAMES[card.suit]}`);
  return e;
}
