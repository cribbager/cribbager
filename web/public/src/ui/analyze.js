// Post-game discard analysis view (A8). A focused, read-only page that surfaces
// the engine's verdict on each of the signed-in player's discards for ONE
// finished game. It is strictly post-game and account-scoped: it consumes
// GET /users/me/games/{id}/analysis (built in A3), which 404s for a live game,
// a game the user wasn't in, or an unknown id, and 401s for a guest.
//
// FIRST CUT: this is a per-hand discard verdict list, NOT a full board replay.
// The richer move-by-move board replay (pegging quality, the whole hand) is a
// separate later task (A2/A4); only the discard analysis the backend exposes
// today is rendered here.

import { mountHeader } from './header.js';
import { analysisBody } from './analysisRender.js';

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

const root = document.getElementById('analyze');
mountHeader();

// gameId comes from ?game=<id>; without it there is nothing to analyze.
const gameId = new URLSearchParams(location.search).get('game');

// message renders a single centered notice (loading / error / empty states).
function message(title, body, action) {
    root.replaceChildren(
        h('h1', { class: 'an-title' }, 'Game analysis'),
        h('div', { class: 'panel an-message' },
            h('p', { class: 'an-message-title' }, title),
            body ? h('p', { class: 'an-message-body' }, body) : null,
            action || null));
}

function renderAnalysis(data) {
    // A2: a link to the richer move-by-move replay of this same finished game.
    const replayLink = h('a', { class: 'an-replay', href: '/replay.html?game=' + encodeURIComponent(gameId) },
        'Open the full move-by-move replay →');

    root.replaceChildren(
        h('h1', { class: 'an-title' }, 'Game analysis'),
        h('p', { class: 'an-subtitle' }, 'How your discards stacked up against the engine.'),
        replayLink,
        ...analysisBody(data));
}

async function load() {
    if (!gameId) {
        message('No game selected', 'Open this page from a finished game to see its analysis.');
        return;
    }
    message('Loading analysis…', '');
    let r;
    try {
        r = await fetch(`/users/me/games/${encodeURIComponent(gameId)}/analysis`);
    } catch {
        message('Could not load analysis',
            'There was a network problem reaching the server.',
            h('button', { class: 'btn btn-primary', onclick: load }, 'Try again'));
        return;
    }
    if (r.status === 401) {
        message('Log in to view analysis', 'Game analysis is only available for your own finished games. Use Login in the header above, then reopen this page.');
        return;
    }
    if (r.status === 404) {
        message('Analysis not available', "We couldn't find a finished game of yours with this id. Analysis is only available for your own completed games.");
        return;
    }
    if (!r.ok) {
        message('Could not load analysis', 'The server returned an unexpected error.',
            h('button', { class: 'btn btn-primary', onclick: load }, 'Try again'));
        return;
    }
    let data;
    try {
        data = await r.json();
    } catch {
        message('Could not load analysis', 'The analysis response was malformed.',
            h('button', { class: 'btn btn-primary', onclick: load }, 'Try again'));
        return;
    }
    renderAnalysis(data);
}

load();
