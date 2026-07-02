/**
 * GameClient — a thin wrapper over the cribbager HTTP API. Same-origin by
 * default (the Go server serves this page), so no CORS. A vs-bot game runs purely
 * on the action responses (the server drives the bot until it is the human's turn
 * again); human-vs-human additionally consumes the SSE delta stream so each player
 * sees the other's moves live.
 */
export class GameClient {
  constructor(base = '') { this.base = base; }

  async request(method, path, token, body) {
    const res = await fetch(this.base + path, {
      method,
      headers: {
        ...(body ? { 'Content-Type': 'application/json' } : {}),
        ...(token ? { Authorization: 'Bearer ' + token } : {}),
      },
      body: body ? JSON.stringify(body) : undefined,
    });
    const data = await res.json().catch(() => ({}));
    if (!res.ok) throw new Error(data.error || `HTTP ${res.status}`);
    return data;
  }

  create(opts) { return this.request('POST', '/games', null, opts); }
  join(gameId) { return this.request('POST', `/games/${gameId}/join`, null, {}); }
  snapshot(gameId, token) { return this.request('GET', `/games/${gameId}`, token); }
  act(gameId, token, action) { return this.request('POST', `/games/${gameId}/actions`, token, action); }
  abandon(gameId, token) { return this.request('POST', `/games/${gameId}/abandon`, token); }

  /**
   * stream opens the SSE delta stream for a game and returns the EventSource.
   * The browser EventSource API can't set the Authorization header, so the token
   * goes in the query string (the server's stream endpoint accepts ?token=).
   * `since` resumes after a given game-event sequence number. Game-event deltas
   * carry an `id:`; the roster ("players") delta does not, so it never disturbs
   * EventSource's Last-Event-ID on reconnect.
   */
  stream(gameId, token, since = 0) {
    const q = new URLSearchParams({ token, since: String(since) });
    return new EventSource(`${this.base}/games/${gameId}/stream?${q}`);
  }
}
