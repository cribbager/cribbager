import { test } from 'node:test';
import assert from 'node:assert/strict';
import { gamePath, parseGameTarget } from '../public/src/ui/gameUrl.js';

test('gamePath builds the clean per-game URL', () => {
  assert.equal(gamePath('abc123'), '/game/abc123');
});

test('gamePath url-encodes the id', () => {
  assert.equal(gamePath('a/b?c'), '/game/a%2Fb%3Fc');
});

test('?new=bot is a create-a-new-game entry, no game id', () => {
  const t = parseGameTarget({ pathname: '/game.html', search: '?new=bot' });
  assert.equal(t.newMode, 'bot');
  assert.equal(t.gameId, null);
  assert.equal(t.join, null);
});

test('?new=open is the host-a-game entry', () => {
  const t = parseGameTarget({ pathname: '/game.html', search: '?new=open' });
  assert.equal(t.newMode, 'open');
});

test('clean /game/<id> path yields the game id', () => {
  const t = parseGameTarget({ pathname: '/game/deadbeef', search: '' });
  assert.equal(t.gameId, 'deadbeef');
  assert.equal(t.newMode, null);
});

test('trailing slash on the clean path still parses', () => {
  const t = parseGameTarget({ pathname: '/game/deadbeef/', search: '' });
  assert.equal(t.gameId, 'deadbeef');
});

test('an encoded id in the path is decoded', () => {
  const t = parseGameTarget({ pathname: '/game/a%2Fb', search: '' });
  assert.equal(t.gameId, 'a/b');
});

test('legacy ?game=<id> query still yields the game id', () => {
  const t = parseGameTarget({ pathname: '/game.html', search: '?game=xyz' });
  assert.equal(t.gameId, 'xyz');
});

test('?join=<id> is surfaced as join, not gameId', () => {
  const t = parseGameTarget({ pathname: '/game.html', search: '?join=friend1' });
  assert.equal(t.join, 'friend1');
  assert.equal(t.gameId, null);
});

test('the clean path wins over a stray ?game= query', () => {
  const t = parseGameTarget({ pathname: '/game/pathwins', search: '?game=querywins' });
  assert.equal(t.gameId, 'pathwins');
});

test('game.html itself is not mistaken for a /game/<id> path', () => {
  const t = parseGameTarget({ pathname: '/game.html', search: '' });
  assert.equal(t.gameId, null);
});

test('an empty/unknown location is inert', () => {
  const t = parseGameTarget({ pathname: '/', search: '' });
  assert.deepEqual(t, { newMode: null, join: null, gameId: null });
});

test('defaults are safe when called with no argument', () => {
  const t = parseGameTarget();
  assert.deepEqual(t, { newMode: null, join: null, gameId: null });
});
