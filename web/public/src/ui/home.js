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
// Identity and the login/signup/forgot-password flows live in the global site
// header (header.js). The home page only reacts to the resulting auth state to
// hide the redundant per-game name field when signed in. Completed-game history
// (the old "Your games" list) now lives on the profile page (/profile.html).

document.getElementById('home').append(
    h('div', { class: 'home-card' },
        h('h1', { class: 'home-title' }, 'Cribbage'),
        nameInput,
        h('div', { class: 'home-actions' }, playBot, invite),
        h('div', { class: 'home-join' }, joinInput, joinBtn),
        gamesSection,
    ),
);
renderGames();

// The site header owns auth; it calls back here whenever the known auth state
// changes (initial guest state, the resolved user after probing /auth/me, and on
// login/logout). We only hide the redundant name field when signed in.
mountHeader({
    onAuthChange: (user) => {
        nameInput.style.display = user ? 'none' : '';
    },
});
