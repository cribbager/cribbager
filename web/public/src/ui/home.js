// Cribbager homepage — a light page with no game engine. It only starts a game
// (vs the bot, or by inviting a human), joins one by id/link, or resumes one in
// progress. Each game lives on its own /game.html?game=<id> URL; this page reads
// the in-progress list straight from localStorage. This is the seed of a lobby.

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
// Same-origin fetch sends the session cookie automatically.
// AUTHED_KEY caches whether a session exists, so the game page (where the session
// cookie is HttpOnly and unreadable from JS) can decide whether to call /auth/me
// at all — avoiding a speculative 401 on every game load for guests.
const AUTHED_KEY = 'cribbager:authed';
const setAuthed = (v) => { try { localStorage.setItem(AUTHED_KEY, v ? '1' : '0'); } catch { /* private mode */ } };
async function authMe() {
    try { const r = await fetch('/auth/me'); const ok = r.ok; setAuthed(ok); return ok ? await r.json() : null; } catch { return null; }
}
async function authPost(path, body) {
    const r = await fetch(path, { method: 'POST', headers: body ? { 'Content-Type': 'application/json' } : {}, body: body ? JSON.stringify(body) : undefined });
    const data = await r.json().catch(() => ({}));
    if (!r.ok) throw new Error(data.error || `HTTP ${r.status}`);
    return data;
}

// withInflight disables a submit button and shows a "Working…" state while an async
// action runs, so a double-click can't fire two requests; it restores the button's
// label and enabled state afterwards (whether the action resolved or threw).
async function withInflight(btn, fn) {
    const label = btn.textContent;
    btn.disabled = true;
    btn.textContent = 'Working…';
    try { await fn(); }
    finally { btn.disabled = false; btn.textContent = label; }
}

let currentUser = null;
let signupMode = false;
let resetMode = false; // showing the "forgot password" reset-request view
const authBar = h('div', { class: 'home-auth' });

// renderResetRequest shows the email-only "send me a reset link" view. The
// response is always generic (the server never reveals whether the email exists),
// so we show the same confirmation regardless and a link back to log in.
function renderResetRequest() {
    authBar.replaceChildren();
    const err = h('div', { class: 'home-auth-err', role: 'alert' });
    const note = h('div', { class: 'home-auth-note', role: 'status' });
    const email = h('input', { type: 'email', placeholder: 'Your account email', class: 'home-input', autocomplete: 'email', 'aria-label': 'Account email' });
    const submit = h('button', { class: 'primary', type: 'submit' }, 'Send reset link');
    const form = h('form', { class: 'home-auth-form' }, email, submit);
    form.addEventListener('submit', async (e) => {
        e.preventDefault();
        err.textContent = '';
        note.textContent = '';
        withInflight(submit, async () => {
            try {
                const data = await authPost('/auth/password/reset-request', { email: email.value.trim() });
                note.textContent = data.message || 'If that email exists, a reset link was sent.';
            } catch (e) {
                err.textContent = e.message;
            }
        });
    });
    const back = h('a', { href: '#', class: 'home-auth-toggle' }, 'Back to log in');
    back.addEventListener('click', (e) => { e.preventDefault(); resetMode = false; renderAuth(); });
    authBar.append(form, note, back, err);
}

function renderAuth() {
    // When signed in, the account's display name is used for games, so the
    // optional per-game name field is redundant — hide it.
    nameInput.style.display = currentUser ? 'none' : '';
    authBar.replaceChildren();
    if (currentUser) {
        const logout = h('button', { class: 'home-forget', title: 'Log out of your account' }, 'Log out');
        logout.addEventListener('click', async () => { try { await authPost('/auth/logout'); } catch { /* ignore */ } setAuthed(false); currentUser = null; signupMode = false; resetMode = false; renderAuth(); renderHistory(); });
        authBar.append(h('span', {}, 'Signed in as '), h('b', {}, currentUser.display_name || currentUser.username), logout);
        return;
    }
    if (resetMode) { renderResetRequest(); return; }
    const err = h('div', { class: 'home-auth-err', role: 'alert' });
    const user = h('input', { type: 'text', placeholder: 'Username', class: 'home-input', autocomplete: 'username', 'aria-label': 'Username' });
    const email = h('input', { type: 'email', placeholder: 'Email', class: 'home-input', autocomplete: 'email', 'aria-label': 'Email' });
    const pass = h('input', { type: 'password', placeholder: 'Password', class: 'home-input', autocomplete: signupMode ? 'new-password' : 'current-password', 'aria-label': 'Password' });
    const submit = h('button', { class: 'primary', type: 'submit' }, signupMode ? 'Sign up' : 'Log in');
    const fields = signupMode ? [user, email, pass] : [user, pass];
    const form = h('form', { class: 'home-auth-form' }, ...fields, submit);
    form.addEventListener('submit', (e) => {
        e.preventDefault();
        err.textContent = '';
        withInflight(submit, async () => {
            try {
                currentUser = signupMode
                    ? await authPost('/auth/signup', { username: user.value.trim(), email: email.value.trim(), password: pass.value })
                    : await authPost('/auth/login', { username: user.value.trim(), password: pass.value });
                setAuthed(true);
                renderAuth();
                renderHistory();
            } catch (e) { err.textContent = e.message; }
        });
    });
    const toggle = h('a', { href: '#', class: 'home-auth-toggle' }, signupMode ? 'Have an account? Log in' : 'Need an account? Sign up');
    toggle.addEventListener('click', (e) => { e.preventDefault(); signupMode = !signupMode; renderAuth(); });
    authBar.append(form, toggle);
    if (!signupMode) {
        const forgot = h('a', { href: '#', class: 'home-auth-toggle' }, 'Forgot password?');
        forgot.addEventListener('click', (e) => { e.preventDefault(); resetMode = true; renderAuth(); });
        authBar.append(forgot);
    }
    authBar.append(err);
}

// ---- completed-game history (shown when signed in) ----
const historySection = h('div', { class: 'home-history' });

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
        list.append(h('div', { class: 'home-history-row ' + (g.won ? 'won' : 'lost') },
            h('span', { class: 'home-history-badge' }, g.won ? 'W' : 'L'),
            h('span', { class: 'home-history-opp' }, 'vs ' + (g.opponent || 'opponent')),
            h('span', { class: 'home-history-score' }, `${g.your_score}–${g.opponent_score}`),
            h('span', { class: 'home-history-date' }, relativeDate(g.ended_at)),
        ));
    }
    historySection.append(list);
}

document.getElementById('home').append(
    h('div', { class: 'home-card' },
        authBar,
        h('h1', { class: 'home-title' }, 'Cribbage'),
        nameInput,
        h('div', { class: 'home-actions' }, playBot, invite),
        h('div', { class: 'home-join' }, joinInput, joinBtn),
        gamesSection,
        historySection,
    ),
);
renderGames();
renderAuth();
// Only probe /auth/me when we last knew we were signed in — a guest's homepage
// shows the login form regardless, so an unconditional probe just yields a wasted
// (and console-logged) 401. authMe() reconciles the flag with the server.
let wasAuthed = '0';
try { wasAuthed = localStorage.getItem(AUTHED_KEY) || '0'; } catch { /* private mode */ }
if (wasAuthed === '1') {
    authMe().then((u) => { if (u) { currentUser = u; renderAuth(); renderHistory(); } });
}
