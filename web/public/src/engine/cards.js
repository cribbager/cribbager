// Core card model. Pure, DOM-free — used by the browser UI and the web tests.
export const SUIT_SYMBOLS = ['♣', '♦', '♥', '♠'];
export const SUIT_NAMES = ['clubs', 'diamonds', 'hearts', 'spades'];
export const RANK_LABELS = ['', 'A', '2', '3', '4', '5', '6', '7', '8', '9', '10', 'J', 'Q', 'K'];

/** Stable 0..51 id for a card: (rank-1)*4 + suit. */
export function cardId(card) {
    return (card.rank - 1) * 4 + card.suit;
}

export function cardsEqual(a, b) {
    return a.rank === b.rank && a.suit === b.suit;
}

const SUIT_LETTERS = { c: 0, d: 1, h: 2, s: 3 };
const NAMED_RANKS = { a: 1, t: 10, j: 11, q: 12, k: 13 };

// Rank letters in the compact wire notation the Go engine parses: ten is "T"
// (not "10"), so this is the inverse of parseCard for serialization, not display.
const RANK_CODES = ['', 'A', '2', '3', '4', '5', '6', '7', '8', '9', 'T', 'J', 'Q', 'K'];

/** Serialize a {rank, suit} card to the engine's two-char form, e.g. "5H", "TD". */
export function cardCode(card) {
    return RANK_CODES[card.rank] + 'CDHS'[card.suit];
}

/** Parse shorthand like "5H", "10d", "TD", "AC", "Js" into a {rank, suit} card. */
export function parseCard(s) {
    const t = s.trim().toLowerCase();
    const suit = SUIT_LETTERS[t.slice(-1)];
    if (suit === undefined)
        throw new Error(`bad card "${s}": suit must be C/D/H/S`);
    const rankPart = t.slice(0, -1);
    // Named (a/t/j/q/k) or a digit rank 2..10 EXACTLY — reject trailing garbage
    // ("5x"→would-be 5) and the ambiguous "1" (Ace must be written "A").
    let rank;
    if (rankPart in NAMED_RANKS)
        rank = NAMED_RANKS[rankPart];
    else if (/^([2-9]|10)$/.test(rankPart))
        rank = parseInt(rankPart, 10);
    else
        throw new Error(`bad card "${s}": rank must be A,2-10,J,Q,K`);
    return { rank, suit };
}
