// Cribbager homepage — a light page with no game engine. It offers two ways to
// start a game (a game vs the computer, or a private "challenge a friend" game
// reachable only by its link) and resumes any game already in progress. Each
// game lives on its own /game.html?game=<id> URL; the in-progress list is read
// straight from localStorage.

import { mountHeader } from './header.js';

// In-progress games are persisted by the game page under this key, as a map of
// { [gameId]: { token, seat, opp } }. We only read/forget here.
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

const go = (url) => { location.href = url; };

// startAction builds one of the "start a game" buttons. `primary` styles the
// recommended default (playing the computer, for a newcomer).
function startAction(title, onClick, { primary = false } = {}) {
    const btn = h('button', { class: primary ? 'home-action primary' : 'home-action', type: 'button' }, title);
    btn.addEventListener('click', onClick);
    return btn;
}
// Play the Bot → vs the computer. The instant, no-waiting option, so it is the
// primary default and listed first for a newcomer.
const playBot = startAction('Play the Bot', () => go('/game.html?new=bot'), { primary: true });
// Challenge a Friend → a private open game (link only): the host gets a
// shareable join link to send to whoever they want to play.
const challenge = startAction('Challenge a Friend', () => go('/game.html?new=open'));

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
// header (header.js). Guests play anonymously — there is no name field here.
// Completed-game history lives on the profile page (/profile.html).

// A single centered card with the two ways to start plus any games in progress.
// No page heading — the "Cribbager" wordmark already lives in the header.
const playSection = h('div', { class: 'panel home-play' },
    h('div', { class: 'home-actions' }, playBot, challenge),
    gamesSection,
);

document.getElementById('home').append(playSection);
renderGames();

// The site header owns auth. Nothing on this page depends on the auth state
// anymore (guests play anonymously), so we just mount it.
mountHeader({});
