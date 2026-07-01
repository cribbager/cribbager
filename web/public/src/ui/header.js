// Cribbager global site header — a small, dependency-free module mounted on every
// page (home, game, reset). On the left a text wordmark links home; on the right
// it shows the auth state: logged out → Login / Register buttons that open an
// auth modal; logged in → the account's display name with an accessible menu
// (Profile, Game history, Log out). It is the canonical home for identity/auth,
// so pages no longer carry their own inline auth bar.
//
// It reuses the same endpoints the app already uses: GET /auth/me, POST
// /auth/login | /auth/signup | /auth/logout | /auth/password/reset-request.

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

// ---- auth plumbing (same contract as the old inline home-page auth bar) ----
// AUTHED_KEY caches whether a session likely exists so guests don't fire a
// speculative (and console-logged) 401 on every page load.
const AUTHED_KEY = 'cribbager:authed';
const setAuthed = (v) => { try { localStorage.setItem(AUTHED_KEY, v ? '1' : '0'); } catch { /* private mode */ } };
const wasAuthed = () => { try { return localStorage.getItem(AUTHED_KEY) === '1'; } catch { return false; } };

async function authMe() {
    try { const r = await fetch('/auth/me'); const ok = r.ok; setAuthed(ok); return ok ? await r.json() : null; } catch { return null; }
}
async function authPost(path, body) {
    const r = await fetch(path, { method: 'POST', headers: body ? { 'Content-Type': 'application/json' } : {}, body: body ? JSON.stringify(body) : undefined });
    const data = await r.json().catch(() => ({}));
    if (!r.ok) throw new Error(data.error || `HTTP ${r.status}`);
    return data;
}

// withInflight disables a submit button and shows a "Working…" state while an
// async action runs, so a double-click can't fire two requests.
async function withInflight(btn, fn) {
    const label = btn.textContent;
    btn.disabled = true;
    btn.textContent = 'Working…';
    try { await fn(); }
    finally { btn.disabled = false; btn.textContent = label; }
}

// ---- the auth modal (login / signup / forgot-password) -----------------------
// Built from the F2 modal + form primitives; no native dialog/alert. Closes on
// outside-click, Escape, the ✕, or a successful sign-in.
function openAuthModal(mode, onSignedIn) {
    let signupMode = mode === 'signup';
    let resetMode = false;

    const box = h('div', { class: 'modal auth-modal', role: 'dialog', 'aria-modal': 'true', 'aria-label': 'Account' });
    const overlay = h('div', { class: 'modal-overlay' }, box);

    function close() {
        document.removeEventListener('keydown', onKey);
        overlay.remove();
    }
    function onKey(e) { if (e.key === 'Escape') close(); }
    overlay.addEventListener('mousedown', (e) => { if (e.target === overlay) close(); });
    document.addEventListener('keydown', onKey);

    const closeBtn = h('button', { type: 'button', class: 'auth-modal-close', 'aria-label': 'Close', onclick: close }, '✕');

    function renderResetRequest() {
        const err = h('div', { class: 'home-auth-err', role: 'alert' });
        const note = h('div', { class: 'home-auth-note', role: 'status' });
        const email = h('input', { type: 'email', placeholder: 'Your account email', class: 'input', autocomplete: 'email', 'aria-label': 'Account email' });
        const submit = h('button', { class: 'btn btn-primary', type: 'submit' }, 'Send reset link');
        const form = h('form', { class: 'auth-form' }, email, submit);
        form.addEventListener('submit', (e) => {
            e.preventDefault();
            err.textContent = '';
            note.textContent = '';
            withInflight(submit, async () => {
                try {
                    const data = await authPost('/auth/password/reset-request', { email: email.value.trim() });
                    note.textContent = data.message || 'If that email exists, a reset link was sent.';
                } catch (ex) { err.textContent = ex.message; }
            });
        });
        const back = h('a', { href: '#', class: 'home-auth-toggle', onclick: (e) => { e.preventDefault(); resetMode = false; render(); } }, 'Back to log in');
        box.replaceChildren(closeBtn, h('h2', { class: 'auth-modal-title' }, 'Reset password'), form, note, back, err);
        email.focus();
    }

    function render() {
        if (resetMode) { renderResetRequest(); return; }
        const err = h('div', { class: 'home-auth-err', role: 'alert' });
        const user = h('input', { type: 'text', placeholder: 'Username', class: 'input', autocomplete: 'username', 'aria-label': 'Username' });
        const email = h('input', { type: 'email', placeholder: 'Email', class: 'input', autocomplete: 'email', 'aria-label': 'Email' });
        const pass = h('input', { type: 'password', placeholder: 'Password', class: 'input', autocomplete: signupMode ? 'new-password' : 'current-password', 'aria-label': 'Password' });
        const submit = h('button', { class: 'btn btn-primary', type: 'submit' }, signupMode ? 'Sign up' : 'Log in');
        const fields = signupMode ? [user, email, pass] : [user, pass];
        const form = h('form', { class: 'auth-form' }, ...fields, submit);
        form.addEventListener('submit', (e) => {
            e.preventDefault();
            err.textContent = '';
            withInflight(submit, async () => {
                try {
                    const u = signupMode
                        ? await authPost('/auth/signup', { username: user.value.trim(), email: email.value.trim(), password: pass.value })
                        : await authPost('/auth/login', { username: user.value.trim(), password: pass.value });
                    setAuthed(true);
                    close();
                    onSignedIn(u);
                } catch (ex) { err.textContent = ex.message; }
            });
        });
        const toggle = h('a', { href: '#', class: 'home-auth-toggle', onclick: (e) => { e.preventDefault(); signupMode = !signupMode; render(); } },
            signupMode ? 'Have an account? Log in' : 'Need an account? Sign up');
        const kids = [closeBtn, h('h2', { class: 'auth-modal-title' }, signupMode ? 'Create account' : 'Log in'), form, toggle];
        if (!signupMode) {
            kids.push(h('a', { href: '#', class: 'home-auth-toggle', onclick: (e) => { e.preventDefault(); resetMode = true; render(); } }, 'Forgot password?'));
        }
        kids.push(err);
        box.replaceChildren(...kids);
        user.focus();
    }

    render();
    document.body.append(overlay);
}

// mountHeader renders the header into the page's <header id="site-header"> and
// wires up auth. onAuthChange(user|null) is called whenever the known auth state
// changes (initial null, then the resolved user after the /auth/me probe, and
// again on login/logout) so a page can react (e.g. the home page's history list).
export function mountHeader({ onAuthChange } = {}) {
    const host = document.getElementById('site-header');
    if (!host) return;
    host.classList.add('site-header');

    let currentUser = null;
    const notify = () => { if (onAuthChange) onAuthChange(currentUser); };

    const right = h('div', { class: 'site-header-right' });
    // Main nav, available to everyone (no auth): two menus, Play and Learn. The
    // top label is a real link (Play → home, Learn → How to Play) so it works on
    // touch, and hovering/focusing reveals the dropdown of specific options
    // (CSS-driven — see .site-nav-menu). Play mirrors the home page's three start
    // options; Learn gathers the learning surfaces.
    const navMenu = (label, topHref, items) => {
        const top = h('a', { class: 'site-nav-top', href: topHref }, label);
        const dropdown = h('div', { class: 'site-nav-dropdown', role: 'menu' },
            ...items.map((it) => h('a', { class: 'site-nav-drop-item', role: 'menuitem', href: it.href }, it.label)));
        return h('div', { class: 'site-nav-menu' }, top, dropdown);
    };
    const nav = h('nav', { class: 'site-nav', 'aria-label': 'Main' },
        navMenu('Play', '/', [
            { label: 'Create a new game', href: '/game.html?new=open&public=1' },
            { label: 'Challenge a friend', href: '/game.html?new=open' },
            { label: 'Play against the computer', href: '/game.html?new=bot' },
        ]),
        navMenu('Learn', '/learn.html', [
            { label: 'How to Play', href: '/learn.html' },
            { label: 'Discard Practice', href: '/practice.html' },
            { label: 'Hand Counting', href: '/practice-scoring.html' },
        ]),
    );
    const inner = h('div', { class: 'site-header-inner' },
        h('a', { class: 'site-wordmark', href: '/' }, 'Cribbager'),
        nav,
        right,
    );
    host.replaceChildren(inner);

    // ---- the signed-in account menu ----
    function buildMenu(name) {
        const menu = h('div', { class: 'site-menu', role: 'menu', hidden: 'hidden' });
        const items = [];
        const addItem = (label, opts = {}) => {
            const attrs = { class: 'site-menu-item', role: 'menuitem', tabindex: '-1' };
            if (opts.href) attrs.href = opts.href;
            if (opts.disabled) { attrs.class += ' is-disabled'; attrs['aria-disabled'] = 'true'; }
            const el = h(opts.href && !opts.disabled ? 'a' : 'button', attrs, label);
            if (!opts.href) el.type = 'button';
            if (opts.onClick && !opts.disabled) el.addEventListener('click', opts.onClick);
            if (opts.disabled) el.addEventListener('click', (e) => e.preventDefault());
            menu.append(el);
            items.push(el);
            return el;
        };

        // Profile (U6): account, lifetime stats, and game history.
        addItem('Profile', { href: '/profile.html' });
        // Game history now lives on the profile page (under #history); jump to it.
        // If we're already on the profile page, smooth-scroll to the section instead.
        addItem('Game history', {
            href: '/profile.html#history',
            onClick: (e) => {
                const onProfile = location.pathname.endsWith('/profile.html');
                const target = onProfile && document.getElementById('history');
                if (target) { e.preventDefault(); closeMenu(); target.scrollIntoView({ behavior: 'smooth' }); }
            },
        });
        addItem('Log out', {
            onClick: async () => {
                closeMenu();
                try { await authPost('/auth/logout'); } catch { /* best effort */ }
                setAuthed(false);
                currentUser = null;
                renderRight();
                notify();
            },
        });

        const trigger = h('button', {
            type: 'button', class: 'site-menu-trigger',
            'aria-haspopup': 'true', 'aria-expanded': 'false',
        }, h('span', { class: 'site-menu-name' }, name), h('span', { class: 'site-menu-caret', 'aria-hidden': 'true' }, '▾'));

        const wrap = h('div', { class: 'site-menu-wrap' }, trigger, menu);

        let open = false;
        function openMenu(focusFirst) {
            open = true;
            menu.hidden = false;
            trigger.setAttribute('aria-expanded', 'true');
            document.addEventListener('mousedown', onOutside);
            document.addEventListener('keydown', onMenuKey);
            if (focusFirst && items[0]) items[0].focus();
        }
        function closeMenu(refocus) {
            if (!open) return;
            open = false;
            menu.hidden = true;
            trigger.setAttribute('aria-expanded', 'false');
            document.removeEventListener('mousedown', onOutside);
            document.removeEventListener('keydown', onMenuKey);
            if (refocus) trigger.focus();
        }
        function onOutside(e) { if (!wrap.contains(e.target)) closeMenu(); }
        function onMenuKey(e) {
            if (e.key === 'Escape') { e.preventDefault(); closeMenu(true); return; }
            if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
                e.preventDefault();
                const i = items.indexOf(document.activeElement);
                const next = e.key === 'ArrowDown' ? (i + 1) % items.length : (i - 1 + items.length) % items.length;
                items[next].focus();
            }
        }
        trigger.addEventListener('click', () => (open ? closeMenu() : openMenu(false)));
        trigger.addEventListener('keydown', (e) => {
            if (e.key === 'ArrowDown' || e.key === 'Enter' || e.key === ' ') { e.preventDefault(); openMenu(true); }
        });

        return { wrap };
    }

    function renderRight() {
        if (currentUser) {
            const name = currentUser.display_name || currentUser.username || 'Account';
            const { wrap } = buildMenu(name);
            right.replaceChildren(wrap);
            return;
        }
        const login = h('button', { type: 'button', class: 'btn' }, 'Login');
        login.addEventListener('click', () => openAuthModal('login', onSignedIn));
        const register = h('button', { type: 'button', class: 'btn btn-primary' }, 'Register');
        register.addEventListener('click', () => openAuthModal('signup', onSignedIn));
        right.replaceChildren(login, register);
    }

    function onSignedIn(user) {
        currentUser = user;
        renderRight();
        notify();
    }

    renderRight();
    notify(); // initial (guest) state, so pages render immediately

    // Reconcile with the server only when we last knew we were signed in — a guest
    // load shows the logged-out controls regardless, so an unconditional probe just
    // yields a wasted 401.
    if (wasAuthed()) {
        authMe().then((u) => { if (u) { currentUser = u; renderRight(); notify(); } });
    }
}
