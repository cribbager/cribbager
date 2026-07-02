// Drives a FULL human-vs-human game through TWO browser contexts: one hosts
// (creates a shared link), the other joins, then both play to game over over the
// SSE delta stream. Fails on any console/page error. Point at a running server:
//   SMOKE_URL=http://localhost:PORT/ node scripts/smoke-multiplayer.mjs
import puppeteer from 'puppeteer-core';
const CHROME = process.env.CHROME ?? '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome';
const base = process.env.SMOKE_URL ?? 'http://localhost:8080/';
const wait = (ms) => new Promise((r) => setTimeout(r, ms));
const errors = [];

const browser = await puppeteer.launch({
    executablePath: CHROME,
    headless: true,
    args: ['--no-sandbox', '--disable-gpu', '--disable-dev-shm-usage'],
});

const track = (page, who) => {
    page.on('console', (m) => { if (m.type() === 'error') errors.push(`[${who}] console.error: ${m.text()}`); });
    page.on('pageerror', (e) => errors.push(`[${who}] pageerror: ${e.message}`));
};

// One in-page action: select+discard two cards / play a legal card / click a
// control button (Start game, Continue). Game over = an overlay modal saying
// "win". A non-game-over overlay (the menu/invite still up) means "wait".
const STEP = () => {
    const ov = document.querySelector('.overlay .card-modal');
    if (ov && /win/i.test(ov.textContent || '')) return 'over';
    if (ov) return 'wait';
    if (document.querySelector('.hand .card.selectable')) {
        if (document.querySelectorAll('.hand .card.selected').length < 2) {
            document.querySelector('.hand .card.selectable:not(.selected)')?.click();
            return 'select';
        }
        document.querySelector('.controls button.primary')?.click();
        return 'discard';
    }
    const legal = document.querySelector('.hand .card.legal');
    if (legal) { legal.click(); return 'play'; }
    const btn = document.querySelector('.controls button.primary');
    if (btn) { btn.click(); return 'button'; }
    return 'wait';
};
// --- single-source-of-truth regression helpers ---
// Each player's identity (game id + token + server seat), read from the same
// localStorage the client persists. Captured while the game is live, because the
// client clears this entry at game over.
const creds = (page) => page.evaluate(() => {
    const params = new URLSearchParams(location.search);
    const id = params.get('game') || params.get('join');
    const saved = (JSON.parse(localStorage.getItem('cribbager:games') || '{}')[id]) || {};
    return { id, token: saved.token, seat: saved.seat };
});
// The two numbers in the game-over overlay, in the page's own [me, opponent] order.
const overlayScores = (page) => page.evaluate(() => {
    const p = document.querySelector('.overlay .card-modal p');
    const nums = ((p && p.textContent) || '').match(/\d+/g) || [];
    return nums.slice(0, 2).map(Number);
});
// The server's authoritative View.Scores (absolute [seat0, seat1]) for this seat's
// token — an authenticated GET /games/{id} with that player's bearer token.
const viewScores = (page, cred) => page.evaluate(async (c) => {
    const res = await fetch('/games/' + c.id, { headers: { Authorization: 'Bearer ' + c.token } });
    if (!res.ok) throw new Error('snapshot HTTP ' + res.status);
    return (await res.json()).Scores; // absolute [seat0, seat1]
}, cred);
const eq = (a, b) => a.length === b.length && a.every((x, i) => x === b[i]);
// uiScores: reindex absolute [seat0, seat1] into the seat's [me, opp] view order.
const ui = (abs, seat) => (seat === 0 ? [abs[0], abs[1]] : [abs[1], abs[0]]);
// inverse: a seat's [me, opp] back into absolute [seat0, seat1].
const toAbs = (mo, seat) => (seat === 0 ? [mo[0], mo[1]] : [mo[1], mo[0]]);
// The game-over overlay caps the displayed score at 121 (cribbage ends the instant
// a player reaches it, even though the engine reports the raw show total), so the
// rendered number is min(View score, 121) — applied identically by both clients.
const cap = (arr) => arr.map((v) => Math.min(v, 121));

try {
    // Host and joiner get ISOLATED browser contexts (separate localStorage) — they
    // are two different players. Sharing storage would make the joiner see the
    // host's saved token and resume-as-host instead of joining.
    const hostCtx = await browser.createBrowserContext();
    const joinCtx = await browser.createBrowserContext();

    // Host: game page with ?new=open hosts an open game → read the share link from
    // the invite overlay (now /game.html?join=<id>; the game id is the credential).
    const host = await hostCtx.newPage();
    await host.setViewport({ width: 900, height: 760 });
    track(host, 'host');
    // domcontentloaded, not networkidle0: hosting opens the SSE stream on load, so
    // the network never goes idle. waitForSelector covers the async create.
    await host.goto(base + 'game.html?new=open', { waitUntil: 'domcontentloaded' });
    await host.waitForSelector('.overlay .card-modal input', { timeout: 15000 });
    const link = await host.$eval('.overlay .card-modal input', (i) => i.value);
    if (!link || !link.includes('/game.html?join=')) throw new Error('no share link: ' + link);

    // Joiner: open the link → the client claims the open seat automatically (no
    // prompt) and the game begins. Joining opens the SSE stream on load, so use
    // domcontentloaded (the network never goes idle), same as the host above.
    const joiner = await joinCtx.newPage();
    await joiner.setViewport({ width: 900, height: 760 });
    track(joiner, 'join');
    await joiner.goto(link, { waitUntil: 'domcontentloaded' });

    // Opening invariant: once both reach the discard prompt, each must already see
    // the deal animated — the opponent's 6 face-down cards up top. (A race once let
    // the prompt fire before the deal animated, leaving a blank opponent seat.)
    const atDiscard = (p) => p.$eval('.controls button', (b) => /discard/i.test(b.textContent || '')).catch(() => false);
    const oppFaceDown = (p) => p.$$eval('.seat.opp .hand .card.facedown', (els) => els.length).catch(() => 0);
    for (let i = 0; i < 60; i++) {
        if ((await atDiscard(host)) && (await atDiscard(joiner))) break;
        await wait(150);
    }
    const ho = await oppFaceDown(host), jo = await oppFaceDown(joiner);
    console.log('opening opp face-down — host:', ho, ' join:', jo, '(expect 6 each)');
    if (ho !== 6 || jo !== 6) { console.log('❌ opening did not animate for both seats'); process.exitCode = 1; }

    // Capture each player's id/token/seat NOW (the client clears this at game over),
    // so we can authenticate a View fetch per seat for the score assertions below.
    const hostCred = await creds(host), joinCred = await creds(joiner);

    // Drive both pages to game over.
    let hostOver = false, joinOver = false, acts = 0;
    for (let i = 0; i < 9000 && !(hostOver && joinOver); i++) {
        const a = await host.evaluate(STEP).catch(() => 'err');
        const b = await joiner.evaluate(STEP).catch(() => 'err');
        if (a === 'discard' || a === 'play' || b === 'discard' || b === 'play') acts++;
        if (a === 'over') hostOver = true;
        if (b === 'over') joinOver = true;
        await wait(40);
    }

    const text = async (p) => (await p.$('.overlay .card-modal')) ? p.$eval('.overlay .card-modal', (o) => o.textContent || '') : '(no game over)';
    console.log('actions driven       :', acts);
    console.log('host  game-over      :', await text(host));
    console.log('join  game-over      :', await text(joiner));
    console.log('console/page errors  :', errors.length);
    for (const e of errors.slice(0, 8)) console.log('  ', e);

    // Single-source-of-truth assertions (the point of this PR): the rendered score
    // must be the server's authoritative View, not a locally-accumulated number that
    // can drift between the two clients.
    let scoreOk = true;
    if (hostOver && joinOver) {
        try {
            const hOv = await overlayScores(host), jOv = await overlayScores(joiner); // each [me, opp]
            // (a) Both players agree on the score (normalize to absolute seat order).
            const hAbs = toAbs(hOv, hostCred.seat), jAbs = toAbs(jOv, joinCred.seat);
            const agree = eq(hAbs, jAbs);
            console.log('rendered (abs)       : host', hAbs, ' join', jAbs, agree ? '✓ agree' : '✗ DIVERGED');
            if (!agree) scoreOk = false;
            // (b) Each player's rendered score is a deterministic function of the View:
            // min(uiScores(View.Scores)[seat], 121) — the display cap applied identically.
            const hView = await viewScores(host, hostCred), jView = await viewScores(joiner, joinCred);
            const hWant = cap(ui(hView, hostCred.seat)), jWant = cap(ui(jView, joinCred.seat));
            const hMatch = eq(hOv, hWant), jMatch = eq(jOv, jWant);
            console.log('host  rendered/View  :', hOv, 'vs', hWant, hMatch ? '✓' : '✗');
            console.log('join  rendered/View  :', jOv, 'vs', jWant, jMatch ? '✓' : '✗');
            if (!hMatch || !jMatch) scoreOk = false;
        } catch (e) {
            console.log('❌ score assertion error:', e.message);
            scoreOk = false;
        }
    }

    if (hostOver && joinOver && errors.length === 0 && scoreOk) {
        console.log('✅ Human-vs-human playthrough PASSED');
    } else {
        console.log('❌ Human-vs-human playthrough FAILED');
        process.exitCode = 1;
    }
} finally {
    await browser.close();
}
