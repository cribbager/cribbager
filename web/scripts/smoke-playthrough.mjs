// Drives a FULL game through the UI headlessly: clicks whatever the UI asks for
// (cut, discard 2, play a legal card, Continue) until the game-over overlay
// appears, failing on any console/page error. Exercises every UI event path.
// The game runs on the Go server; point this at a running server:
//   node scripts/smoke-playthrough.mjs        (defaults to http://localhost:8080/)
//   SMOKE_URL=http://localhost:PORT/ node scripts/smoke-playthrough.mjs
import puppeteer from 'puppeteer-core';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
const CHROME = process.env.CHROME ?? '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome';
const fileUrl = process.env.SMOKE_URL ?? 'http://localhost:8080/';
const shot = (n) => join(tmpdir(), n);
const wait = (ms) => new Promise((r) => setTimeout(r, ms));
const errors = [];
const browser = await puppeteer.launch({
    executablePath: CHROME,
    headless: true,
    args: ['--no-sandbox', '--disable-gpu', '--disable-dev-shm-usage'],
});
try {
    const page = await browser.newPage();
    await page.setViewport({ width: 960, height: 760 });
    page.on('console', (m) => {
        if (m.type() === 'error')
            errors.push('console.error: ' + m.text());
    });
    page.on('pageerror', (e) => {
        errors.push('pageerror: ' + e.message);
    });
    // The menu now lives on the homepage (index.html); the game page starts a bot
    // game directly from ?new=bot. This smoke test drives that single-player flow.
    await page.goto(fileUrl + 'game.html?new=bot', { waitUntil: 'networkidle0' });
    // Now the bot game starts; make sure the table is actually driving (a control
    // button such as "Start game", or the game-over overlay) before stepping.
    await page.waitForSelector('.controls button.primary, .overlay', { timeout: 15000 });
    // Decide AND act in a single in-page step: the UI rebuilds the whole table
    // (`replaceChildren`) on every event, so a held element handle detaches
    // mid-render — querying and clicking atomically inside the page avoids that,
    // and collapsing the per-iteration logic into one `evaluate` keeps the CDP
    // round-trips (and thus the flakiness window) low over a multi-minute game.
    // Synthetic `.click()` fires the same listeners the UI attaches. Returns the
    // action taken: 'over' | 'discard' | 'play' | 'button' | 'select' | 'wait'.
    //
    // All clicks are scoped to `.controls`, never the game-over overlay's
    // "New game" — so the overlay is the only way the loop exits.
    const step = () => page.evaluate(() => {
        if (document.querySelector('.overlay'))
            return 'over';
        // Discard: select two selectable hand cards, then confirm.
        if (document.querySelector('.hand .card.selectable')) {
            if (document.querySelectorAll('.hand .card.selected').length < 2) {
                document.querySelector('.hand .card.selectable:not(.selected)')?.click();
                return 'select';
            }
            document.querySelector('.controls button.primary')?.click(); // "Discard to … crib"
            return 'discard';
        }
        // Pegging: play the first legal card.
        const legal = document.querySelector('.hand .card.legal');
        if (legal) {
            legal.click();
            return 'play';
        }
        // Otherwise an in-flight control button: "Start game", "Continue ▸", etc.
        const btn = document.querySelector('.controls button.primary');
        if (btn) {
            btn.click();
            return 'button';
        }
        return 'wait';
    });
    let hands = 0;
    let plays = 0;
    let clicks = 0; // Start game / Continue / count advances
    let firstErr = '';
    let consecErr = 0;
    for (let i = 0; i < 6000; i++) {
        let action;
        try {
            action = await step();
            consecErr = 0;
        }
        catch (e) {
            // A transient CDP hiccup mid-render is fine to retry; a dead browser is not.
            if (!firstErr)
                firstErr = e.message;
            if (!browser.connected)
                throw e;
            // If every step throws, don't burn the full 6000-iteration budget.
            if (++consecErr > 40) {
                console.log('  step() threw repeatedly:', firstErr);
                break;
            }
            await wait(120);
            continue;
        }
        if (action === 'over')
            break;
        if (action === 'discard')
            hands++;
        else if (action === 'play')
            plays++;
        else if (action === 'button')
            clicks++;
        // 'button' settles longer: a resolved Continue lingers through the next paced
        // delay, so we don't re-click it many times before the render clears it.
        await wait(action === 'wait' ? 80 : action === 'button' ? 160 : 35);
    }
    const overlay = await page.$('.overlay');
    const result = overlay ? await page.$eval('.overlay', (o) => o.textContent || '') : '(no game over)';
    await page.screenshot({ path: shot('_smoke-3-gameover.png') });
    console.log('Discards confirmed    :', hands);
    console.log('Cards pegged by human :', plays);
    console.log('Continue/Start clicks :', clicks);
    console.log('Game-over overlay text:', JSON.stringify(result.replace(/\s+/g, ' ').trim()));
    console.log('Console/page errors   :', errors.length);
    for (const e of errors)
        console.log('   ' + e);
    const ok = !!overlay && /wins|win/i.test(result) && errors.length === 0;
    if (!ok) {
        // Show what the page looked like at the end so a stall is diagnosable.
        const snap = await page.evaluate(() => ({
            controlsBtn: document.querySelector('.controls button.primary')?.textContent ?? null,
            selectable: document.querySelectorAll('.hand .card.selectable').length,
            legal: document.querySelectorAll('.hand .card.legal').length,
            hands: document.querySelectorAll('.hand').length,
            bodyText: (document.body.innerText || '').replace(/\s+/g, ' ').trim().slice(0, 240),
        }));
        console.log('  DOM at exit:', JSON.stringify(snap));
    }
    console.log('\n' + (ok ? '✅ Full-game UI playthrough PASSED' : '❌ playthrough FAILED'));
    process.exit(ok ? 0 : 1);
}
finally {
    await browser.close();
}
