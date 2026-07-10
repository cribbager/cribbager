// Cribbager profile page (U6). The signed-in user's home for two already-built
// backend features: lifetime scoring statistics (E1, GET /users/me/stats) and
// finished-game history (GET /users/me/games, the list that used to live on the
// home page). It is account-scoped: a guest sees a friendly "log in" prompt.
//
// Identity/auth is owned by the global site header (header.js); this page only
// reacts to its onAuthChange callback (initial guest state, the resolved user
// after the /auth/me probe, and on login/logout) and re-renders accordingly.

import { mountHeader } from './header.js';

// tiny DOM helper (matches the on*-listener + text-node style used elsewhere)
function h(tag, attrs = {}, ...kids) {
    const e = document.createElement(tag);
    for (const [k, v] of Object.entries(attrs)) {
        if (k === 'class') e.className = v;
        else if (k.startsWith('on') && typeof v === 'function') e.addEventListener(k.slice(2).toLowerCase(), v);
        else if (v != null) e.setAttribute(k, v);
    }
    for (const kid of kids.flat()) if (kid != null && kid !== false) e.append(kid.nodeType ? kid : document.createTextNode(kid));
    return e;
}

const root = document.getElementById('profile');

// relativeDate renders an ISO timestamp as a short "3h ago" string (ported from
// the home page's history list so the rows read identically).
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

// ---- section builders --------------------------------------------------------

// accountPanel shows the identity fields /auth/me actually returns: a display
// name, the @username, and email — the latter only when present (it always is
// for an account, but we stay defensive). There is no member-since field on the
// wire, so none is shown.
function accountPanel(user) {
    const name = user.display_name || user.username || 'Player';
    const rows = [
        h('div', { class: 'profile-name' }, name),
        h('div', { class: 'profile-handle' }, '@' + (user.username || '')),
    ];
    if (user.email) rows.push(h('div', { class: 'profile-field' }, user.email));
    return h('div', { class: 'panel profile-section' },
        h('div', { class: 'profile-section-label' }, 'Account'),
        ...rows);
}

// statRow is one labelled category line (pegging / hand / crib) in the stats
// grid: min / max / avg / total, with the average shown to one decimal.
function statRow(label, cat) {
    return h('div', { class: 'profile-stat-row' },
        h('span', { class: 'profile-stat-name' }, label),
        h('span', { class: 'profile-stat-cell' }, String(cat.min)),
        h('span', { class: 'profile-stat-cell' }, String(cat.max)),
        h('span', { class: 'profile-stat-cell' }, Number(cat.avg).toFixed(1)),
        h('span', { class: 'profile-stat-cell' }, String(cat.total)));
}

// statsPanel renders the lifetime scoring breakdown (E1) plus the win/loss
// record carried by /users/me/games. A new account with no finished games gets
// a friendly prompt instead of a grid of zeros.
function statsPanel(stats, record) {
    const section = h('div', { class: 'panel profile-section' },
        h('div', { class: 'profile-section-label' }, 'Statistics'));
    const games = stats && stats.games ? stats.games : 0;
    if (!games) {
        section.append(h('div', { class: 'profile-empty' }, 'Play a game to see your stats.'));
        return section;
    }
    const head = [`${games} game${games === 1 ? '' : 's'} played`];
    if (record && record.total) {
        const rate = Math.round((record.wins / record.total) * 100);
        head.push(`${record.wins}W–${record.losses}L · ${rate}% wins`);
    }
    section.append(h('div', { class: 'profile-stat-summary' }, head.join(' · ')));
    section.append(
        h('div', { class: 'profile-stat-grid' },
            h('div', { class: 'profile-stat-row profile-stat-head' },
                h('span', { class: 'profile-stat-name' }, ''),
                h('span', { class: 'profile-stat-cell' }, 'Min'),
                h('span', { class: 'profile-stat-cell' }, 'Max'),
                h('span', { class: 'profile-stat-cell' }, 'Avg'),
                h('span', { class: 'profile-stat-cell' }, 'Total')),
            statRow('Pegging', stats.pegging),
            statRow('Hand', stats.hand),
            statRow('Crib', stats.crib)));
    return section;
}

// historyPanel ports the home page's "Your games" list verbatim: one row per
// finished game (W/L badge, opponent, your–their score, when), each with an
// Analyze link into the A8 post-game view by game id.
function historyPanel(games) {
    const section = h('div', { class: 'panel profile-section', id: 'history' },
        h('div', { class: 'profile-section-label' }, 'Game history'));
    if (!games || !games.length) {
        section.append(h('div', { class: 'profile-empty' }, 'Your finished games will show up here.'));
        return section;
    }
    const list = h('div', { class: 'home-history-list' });
    for (const g of games) {
        // These rows are the signed-in user's own finished games, so the unified
        // replay+analysis page (A10) always applies — link straight to it by id
        // (the login cookie authorizes it, no token needed).
        const analyze = g.id
            ? h('a', { class: 'home-history-analyze', href: '/analyze.html?game=' + encodeURIComponent(g.id) }, 'Evaluate')
            : null;
        list.append(h('div', { class: 'home-history-row ' + (g.won ? 'won' : 'lost') },
            h('span', { class: 'home-history-badge' }, g.won ? 'W' : 'L'),
            h('span', { class: 'home-history-opp' }, 'vs ' + (g.opponent || 'opponent')),
            h('span', { class: 'home-history-score' }, `${g.your_score}–${g.opponent_score}`),
            h('span', { class: 'home-history-date' }, relativeDate(g.ended_at)),
            analyze,
        ));
    }
    section.append(list);
    return section;
}

// signedOut shows a friendly prompt; the actual Login control lives in the
// header above, so we just point the guest at it.
function signedOut() {
    root.replaceChildren(
        h('h1', { class: 'profile-title' }, 'Profile'),
        h('div', { class: 'panel profile-section' },
            h('div', { class: 'profile-empty' }, 'Log in to view your profile — use Login in the header above.')));
}

// ---- render orchestration ----------------------------------------------------
// renderSeq guards against a stale async render: if the auth state changes (or
// re-fires) while a fetch is in flight, only the latest render writes the DOM.
let renderSeq = 0;

async function renderSignedIn(user) {
    const seq = ++renderSeq;
    root.replaceChildren(
        h('h1', { class: 'profile-title' }, 'Profile'),
        accountPanel(user),
        h('div', { class: 'panel profile-section' }, h('div', { class: 'profile-empty' }, 'Loading…')));

    // Stats (category breakdown) and games (history + W/L record) come from two
    // endpoints; fetch them together and tolerate either failing independently.
    let stats = null;
    let games = [];
    let record = null;
    const [statsRes, gamesRes] = await Promise.allSettled([
        fetch('/users/me/stats'),
        fetch('/users/me/games'),
    ]);
    if (statsRes.status === 'fulfilled' && statsRes.value.ok) {
        try { stats = await statsRes.value.json(); } catch { /* leave null */ }
    }
    if (gamesRes.status === 'fulfilled' && gamesRes.value.ok) {
        try {
            const data = await gamesRes.value.json();
            games = data.games || [];
            record = data.stats || null;
        } catch { /* leave defaults */ }
    }
    if (seq !== renderSeq) return; // a newer render superseded this one

    root.replaceChildren(
        h('h1', { class: 'profile-title' }, 'Profile'),
        accountPanel(user),
        statsPanel(stats, record),
        historyPanel(games));
}

// The site header owns auth and calls back here whenever the known auth state
// changes. A guest (null) sees the log-in prompt; a signed-in user gets the full
// profile. ++renderSeq on sign-out cancels any in-flight signed-in render.
mountHeader({
    onAuthChange: (user) => {
        if (user) {
            renderSignedIn(user);
        } else {
            renderSeq++;
            signedOut();
        }
    },
});
