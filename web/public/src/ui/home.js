// Cribbager homepage — a light page with no game engine. It only starts a game
// (vs the bot, or by inviting a human), joins one by id/link, or resumes one in
// progress. Each game lives on its own /game.html?game=<id> URL; this page reads
// the in-progress list straight from localStorage. This is the seed of a lobby.

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

const go = (url) => { location.href = url; };

const nameInput = h('input', { type: 'text', placeholder: 'Your name (optional)', maxlength: '24', class: 'home-input', 'aria-label': 'Your name (optional)' });
const joinInput = h('input', { type: 'text', placeholder: 'Game ID or invite link', class: 'home-input', 'aria-label': 'Game ID or invite link' });
joinInput.addEventListener('keydown', (e) => { if (e.key === 'Enter') doJoin(); });

function doJoin() {
    const id = parseGameId(joinInput.value);
    if (id) go('/game.html?join=' + encodeURIComponent(id));
}

const playBot = h('button', { class: 'primary' }, 'Play vs the bot');
playBot.addEventListener('click', () => go('/game.html?new=bot'));
const invite = h('button', {}, 'Invite a friend');
invite.addEventListener('click', () => go('/game.html?new=open&name=' + encodeURIComponent(nameInput.value.trim())));
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

// ---- accounts (optional; guests can still play without one) ----
// Identity and the login/signup/forgot-password flows now live in the global site
// header (header.js). The home page only reacts to the resulting auth state: it
// hides the redundant per-game name field when signed in and (re)loads the "Your
// games" history. currentUser is fed by the header's onAuthChange callback.
let currentUser = null;

// ---- completed-game history (shown when signed in) ----
const historySection = h('div', { class: 'home-history', id: 'your-games' });

// relativeDate renders an ISO timestamp as a short "3h ago" string.
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

// renderHistory fills "Your games" from /users/me/games. Cleared when signed out;
// silently empty for a new account with no finished games yet.
async function renderHistory() {
    historySection.replaceChildren();
    if (!currentUser) return;
    let data;
    try {
        const r = await fetch('/users/me/games');
        if (!r.ok) return;
        data = await r.json();
    } catch { return; }
    const games = data.games || [];
    const stats = data.stats || {};
    if (!stats.total) return;
    const rate = Math.round((stats.wins / stats.total) * 100);
    historySection.append(
        h('div', { class: 'home-label' }, 'Your games'),
        h('div', { class: 'home-stats' }, `${stats.total} played · ${stats.wins}W–${stats.losses}L · ${rate}% wins`),
    );
    const list = h('div', { class: 'home-history-list' });
    for (const g of games) {
        // These rows are the signed-in user's own finished games, so the post-game
        // discard analysis (A8) always applies — link straight to its view by id.
        const analyze = g.id
            ? h('a', { class: 'home-history-analyze', href: '/analyze.html?game=' + encodeURIComponent(g.id) }, 'Analyze')
            : null;
        list.append(h('div', { class: 'home-history-row ' + (g.won ? 'won' : 'lost') },
            h('span', { class: 'home-history-badge' }, g.won ? 'W' : 'L'),
            h('span', { class: 'home-history-opp' }, 'vs ' + (g.opponent || 'opponent')),
            h('span', { class: 'home-history-score' }, `${g.your_score}–${g.opponent_score}`),
            h('span', { class: 'home-history-date' }, relativeDate(g.ended_at)),
            analyze,
        ));
    }
    historySection.append(list);
}

document.getElementById('home').append(
    h('div', { class: 'home-card' },
        h('h1', { class: 'home-title' }, 'Cribbage'),
        nameInput,
        h('div', { class: 'home-actions' }, playBot, invite),
        h('div', { class: 'home-join' }, joinInput, joinBtn),
        gamesSection,
        historySection,
    ),
);
renderGames();

// The site header owns auth; it calls back here whenever the known auth state
// changes (initial guest state, the resolved user after probing /auth/me, and on
// login/logout). We mirror it locally to hide the redundant name field and refresh
// the "Your games" history.
mountHeader({
    onAuthChange: (user) => {
        currentUser = user;
        nameInput.style.display = user ? 'none' : '';
        renderHistory();
    },
});
