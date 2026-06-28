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
const clickButton = (page, re) => page.evaluate((src) => {
    const rx = new RegExp(src, 'i');
    [...document.querySelectorAll('.overlay .card-modal button')].find((b) => rx.test(b.textContent))?.click();
}, re.source);

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

    // Joiner: open the link → "Join game".
    const joiner = await joinCtx.newPage();
    await joiner.setViewport({ width: 900, height: 760 });
    track(joiner, 'join');
    await joiner.goto(link, { waitUntil: 'networkidle0' });
    await joiner.waitForSelector('.overlay .card-modal button', { timeout: 15000 });
    await clickButton(joiner, /join/);

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
    if (hostOver && joinOver && errors.length === 0) {
        console.log('✅ Human-vs-human playthrough PASSED');
    } else {
        console.log('❌ Human-vs-human playthrough FAILED');
        process.exitCode = 1;
    }
} finally {
    await browser.close();
}
