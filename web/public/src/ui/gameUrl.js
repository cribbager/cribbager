// gameUrl — pure helpers for the game page's URL scheme, kept dependency-free so
// they can be unit-tested without a DOM. The game page is a single-page client
// reachable three ways:
//   /game.html?new=bot | ?new=open   — create a NEW game (the entry links)
//   /game.html?join=<id>             — join (or resume) a shared open game
//   /game/<id>                       — a live game's own URL (bot or resumed MP
//                                      via ?game=<id>), used so a refresh targets
//                                      that game instead of re-reading ?new=bot
//                                      and spawning a brand-new one.
// A created game replaceState()s to its own /game/<id> URL, so a subsequent hard
// refresh resumes rather than creates.

// gamePath is the clean per-game URL for a game id. The id is URL-encoded, though
// server ids are hex tokens with nothing to escape.
export function gamePath(id) {
  return '/game/' + encodeURIComponent(id);
}

// parseGameTarget reads the routing intent from a location-like object
// ({ pathname, search }). It returns { newMode, join, gameId }:
//   newMode — 'bot' | 'open' | null (the ?new= create-a-new-game entry)
//   join    — a game id to join from a shared link (?join=<id>), or null
//   gameId  — an existing game to resume, from the /game/<id> path OR ?game=<id>,
//             or null. The clean path wins when both are present.
export function parseGameTarget({ pathname = '/', search = '' } = {}) {
  const params = new URLSearchParams(search);
  const m = pathname.match(/^\/game\/([^/]+)\/?$/);
  const pathId = m ? decodeURIComponent(m[1]) : null;
  return {
    newMode: params.get('new'),
    join: params.get('join'),
    gameId: pathId || params.get('game'),
  };
}
