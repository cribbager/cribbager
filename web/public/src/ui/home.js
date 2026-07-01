// Cribbager homepage — a light page with no game engine. It offers three ways to
// start a game (a PUBLIC game that lists in the lobby, a PRIVATE "challenge a
// friend" game reachable only by its link, or a game vs the computer), joins one
// by id/link, resumes one in progress, and lists the public games waiting in the
// lobby. Each game lives on its own /game.html?game=<id> URL; the in-progress list
// is read straight from localStorage, and the lobby is polled from GET /lobby.

import { mountHeader } from './header.js';

// In-progress games are persisted by the game page under this key, as a map of
// { [gameId]: { token, seat, name, opp } }. We only read/forget here.
const SAVE_KEY = 'cribbager:games';
const allSaved = () => { try { return JSON.parse(localStorage.getItem(SAVE_KEY) || '{}'); } catch { return {}; } };
const forget = (id) => { try { const m = allSaved(); delete m[id]; localStorage.setItem(SAVE_KEY, JSON.stringify(m)); } catch { /* ignore */ } };

// tiny DOM helper (home.js is standalone, so it carries its own)
function h(tag, attrs = {}, ...kids) {
    const e = document.createElement(tag);
    for (const [k, v] of Object.entries(attrs)) {
        if (k === 'class') e.className = v;
        else if (k.startsWith('on') && typeof v === 'function') e.addEventListener(k.slice(2).toLowerCase(), v);
        else e.setAttribute(k, v);
    }
    for (const kid of kids.flat()) if (kid != null && kid !== false) e.append(kid.nodeType ? kid : document.createTextNode(kid));
    return e;
}

// parseGameId accepts a bare game id, a full invite link/URL (?join=<id> or
// ?game=<id>), or a legacy "<id>.<token>" code, and returns just the id.
function parseGameId(s) {
    s = (s || '').trim();
    if (!s) return '';
    const m = s.match(/[?&](?:join|game)=([^&]+)/);
    let id = m ? m[1] : s;
    try { id = decodeURIComponent(id); } catch { /* malformed % escape — use the raw text */ }
    id = id.trim();
    id = id.split('.')[0];   // drop any legacy ".token"
    id = id.split('/').pop(); // tolerate a trailing path segment
    return id;
}

const go = (url) => { stopLobby(); location.href = url; };

// relativeDate renders an ISO timestamp as a short "3h ago" string (same helper
// the profile/history lists use, inlined here since home.js is standalone).
function relativeDate(iso) {
    const t = new Date(iso).getTime();
    if (!t) return '';
    const s = Math.max(0, Math.floor((Date.now() - t) / 1000));
    if (s < 60) return 'just now';
    const m = Math.floor(s / 60); if (m < 60) return m + 'm ago';
    const hr = Math.floor(m / 60); if (hr < 24) return hr + 'h ago';
    const d = Math.floor(hr / 24); if (d < 30) return d + 'd ago';
    const mo = Math.floor(d / 30); if (mo < 12) return mo + 'mo ago';
    return Math.floor(mo / 12) + 'y ago';
}

const nameInput = h('input', { type: 'text', placeholder: 'Your name (optional)', maxlength: '24', class: 'home-input', 'aria-label': 'Your name (optional)' });
const joinInput = h('input', { type: 'text', placeholder: 'Game ID or invite link', class: 'home-input', 'aria-label': 'Game ID or invite link' });
joinInput.addEventListener('keydown', (e) => { if (e.key === 'Enter') doJoin(); });

function doJoin() {
    const id = parseGameId(joinInput.value);
    if (id) go('/game.html?join=' + encodeURIComponent(id));
}

const playerName = () => encodeURIComponent(nameInput.value.trim());
// startAction builds one of the three "create a game" buttons: a title line plus a
// compact helper sub-label so a newcomer can tell the options apart. `primary`
// styles the recommended default (playing the computer, for a newcomer).
function startAction(title, help, onClick, { primary = false } = {}) {
    const btn = h('button', { class: primary ? 'home-action primary' : 'home-action', type: 'button' },
        h('span', { class: 'home-action-title' }, title),
        h('span', { class: 'home-action-help' }, help),
    );
    btn.addEventListener('click', onClick);
    return btn;
}
// Play the computer → vs the bot. The instant, no-waiting option, so it is the
// primary default and listed first for a newcomer.
const playBot = startAction('Play the computer', 'Play a bot right now',
    () => go('/game.html?new=bot'), { primary: true });
// Create a game → a PUBLIC open game that lists in the lobby (public=1). The host
// lands in the game waiting for anyone to join.
const createGame = startAction('Create a game', 'Open a public game others can join',
    () => go('/game.html?new=open&public=1&name=' + playerName()));
// Challenge a friend → a PRIVATE open game (no public flag): link/token only, the
// host gets a shareable join link.
const challenge = startAction('Challenge a friend', 'Invite someone with a private link',
    () => go('/game.html?new=open&name=' + playerName()));
const joinBtn = h('button', {}, 'Join');
joinBtn.addEventListener('click', doJoin);

const gamesSection = h('div', { class: 'home-games' });

function renderGames() {
    const m = allSaved();
    const ids = Object.keys(m);
    gamesSection.replaceChildren();
    if (!ids.length) return;
    gamesSection.append(h('div', { class: 'home-label' }, 'Games in progress'));
    for (const id of ids) {
        const rec = m[id] || {};
        const opp = rec.opp && rec.opp !== 'Opponent' ? rec.opp : 'an opponent';
        const resume = h('a', { class: 'home-resume', href: '/game.html?game=' + encodeURIComponent(id) }, `Resume — vs ${opp}`);
        const x = h('button', { class: 'home-forget', title: 'Forget this game' }, '✕');
        x.addEventListener('click', () => { forget(id); renderGames(); });
        gamesSection.append(h('div', { class: 'home-game-row' }, resume, x));
    }
}

// ---- lobby: public open games waiting for a joiner ----
// GET /lobby (no auth) returns joinable public open games, newest-first. We poll
// it while this page is open and render each as a "host — age" row with a Join
// button (the game_id is the join credential). Errors are swallowed quietly so a
// transient blip doesn't spam the page; the next tick simply retries.
const LOBBY_POLL_MS = 5000;
let lobbyTimer = null;
const lobbyList = h('div', { class: 'home-lobby-list' });

function renderLobby(games) {
    lobbyList.replaceChildren();
    if (!games.length) {
        lobbyList.append(h('div', { class: 'home-lobby-empty' }, 'No open games right now — create one!'));
        return;
    }
    for (const g of games) {
        const host = g.host_name && g.host_name.trim() ? g.host_name : 'Anonymous';
        const join = h('button', { class: 'home-lobby-join' }, 'Join');
        join.addEventListener('click', () => go('/game.html?join=' + encodeURIComponent(g.game_id)));
        lobbyList.append(h('div', { class: 'home-lobby-row' },
            h('div', { class: 'home-lobby-meta' },
                h('span', { class: 'home-lobby-host' }, host),
                h('span', { class: 'home-lobby-age' }, relativeDate(g.created_at))),
            join,
        ));
    }
}

async function fetchLobby() {
    try {
        const res = await fetch('/lobby', { headers: { Accept: 'application/json' } });
        if (!res.ok) return; // quiet: keep the last good list, retry next tick
        const data = await res.json();
        renderLobby(Array.isArray(data.games) ? data.games : []);
    } catch { /* network blip — stay quiet and retry on the next interval */ }
}

function stopLobby() {
    if (lobbyTimer) { clearInterval(lobbyTimer); lobbyTimer = null; }
}
function startLobby() {
    fetchLobby();
    lobbyTimer = setInterval(fetchLobby, LOBBY_POLL_MS);
}
// Don't leak the timer when the page is hidden/navigated away (pagehide also
// covers bfcache); go() already clears it before navigating.
window.addEventListener('pagehide', stopLobby);

const lobbySection = h('div', { class: 'panel home-lobby' },
    h('div', { class: 'home-label' }, 'Lobby'),
    lobbyList,
);

// ---- accounts (optional; guests can still play without one) ----
// Identity and the login/signup/forgot-password flows live in the global site
// header (header.js). The home page only reacts to the resulting auth state to
// hide the redundant per-game name field when signed in. Completed-game history
// (the old "Your games" list) now lives on the profile page (/profile.html).

document.getElementById('home').append(
    h('div', { class: 'home-card' },
        h('h1', { class: 'home-title' }, 'Cribbager'),
        h('p', { class: 'home-tagline' }, 'Play and learn cribbage online — vs the computer or a friend.'),
        nameInput,
        h('div', { class: 'home-actions' }, playBot, createGame, challenge),
        h('div', { class: 'home-join' }, joinInput, joinBtn),
        gamesSection,
    ),
    lobbySection,
);
renderGames();
renderLobby([]); // friendly empty state until the first fetch resolves
startLobby();

// The site header owns auth; it calls back here whenever the known auth state
// changes (initial guest state, the resolved user after probing /auth/me, and on
// login/logout). We only hide the redundant name field when signed in.
mountHeader({
    onAuthChange: (user) => {
        nameInput.style.display = user ? 'none' : '';
    },
});
