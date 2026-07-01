import { test } from 'node:test';
import assert from 'node:assert/strict';
import { sortCards, parseCard, cardCode } from '../public/src/engine/cards.js';

// The app-wide display order (U8): rank ascending (ace low), then suit in the
// SUIT_SYMBOLS order clubs<diamonds<hearts<spades (suit indices 0..3).
const order = (cards) => cards.map(cardCode);

test('sortCards orders by rank ascending, ace low', () => {
  const hand = ['KH', 'AC', 'TD', '5S', '2C', 'JH'].map(parseCard);
  assert.deepEqual(order(sortCards(hand)), ['AC', '2C', '5S', 'TD', 'JH', 'KH']);
});

test('sortCards breaks rank ties by suit (clubs<diamonds<hearts<spades)', () => {
  const sameRank = ['5S', '5H', '5C', '5D'].map(parseCard);
  assert.deepEqual(order(sortCards(sameRank)), ['5C', '5D', '5H', '5S']);
});

test('sortCards returns a new array and does not mutate its input', () => {
  const hand = ['KH', 'AC'].map(parseCard);
  const snapshot = order(hand);
  const sorted = sortCards(hand);
  assert.notEqual(sorted, hand);              // a fresh array
  assert.deepEqual(order(hand), snapshot);    // input untouched
  assert.deepEqual(order(sorted), ['AC', 'KH']);
});
