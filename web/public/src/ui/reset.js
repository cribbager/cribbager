// Cribbager password-reset page. It reads ?token=<id> from the URL, shows a
// new-password form (password + confirm), and POSTs /auth/password/reset. On
// success it links back to the homepage to log in; on failure it shows the error.
// Standalone and tiny — it carries its own little h().

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

const token = new URLSearchParams(location.search).get('token') || '';
const root = document.getElementById('reset');

function render() {
    root.replaceChildren(card());
}

function card() {
    if (!token) {
        return h('div', { class: 'home-card' },
            h('h1', { class: 'home-title' }, 'Reset password'),
            h('div', { class: 'home-auth-err' }, 'Missing reset token. Use the link from your email.'),
            h('a', { class: 'home-auth-toggle', href: '/' }, 'Back to log in'),
        );
    }

    const err = h('div', { class: 'home-auth-err', role: 'alert' });
    const pass = h('input', { type: 'password', placeholder: 'New password', class: 'home-input', autocomplete: 'new-password', 'aria-label': 'New password' });
    const confirm = h('input', { type: 'password', placeholder: 'Confirm password', class: 'home-input', autocomplete: 'new-password', 'aria-label': 'Confirm password' });
    const submit = h('button', { class: 'primary', type: 'submit' }, 'Set new password');

    const form = h('form', {}, pass, confirm, h('div', { class: 'home-actions' }, submit));
    form.addEventListener('submit', async (e) => {
        e.preventDefault();
        err.textContent = '';
        if (pass.value.length < 8) { err.textContent = 'Password must be at least 8 characters.'; return; }
        if (pass.value !== confirm.value) { err.textContent = 'Passwords do not match.'; return; }
        const label = submit.textContent;
        submit.disabled = true;
        submit.textContent = 'Working…';
        try {
            const r = await fetch('/auth/password/reset', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ token, password: pass.value }),
            });
            if (!r.ok) {
                const data = await r.json().catch(() => ({}));
                throw new Error(data.error || `HTTP ${r.status}`);
            }
            root.replaceChildren(h('div', { class: 'home-card' },
                h('h1', { class: 'home-title' }, 'Password reset'),
                h('div', { class: 'home-auth-note', role: 'status' }, 'You can now log in with your new password.'),
                h('a', { class: 'home-auth-toggle', href: '/' }, 'Go to log in'),
            ));
        } catch (e) {
            err.textContent = e.message;
            submit.disabled = false;
            submit.textContent = label;
        }
    });

    return h('div', { class: 'home-card' },
        h('h1', { class: 'home-title' }, 'Reset password'),
        form,
        err,
        h('a', { class: 'home-auth-toggle', href: '/' }, 'Back to log in'),
    );
}

render();
