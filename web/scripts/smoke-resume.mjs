// Verifies a bot game SURVIVES a page refresh: start a bot game, make a move so
// the game is mid-hand, then RELOAD the page at its clean /game/<id> URL and
// assert the SAME game resumes (same id, board rendered from state — not a fresh
// deal / brand-new game). Point at a running server on a non-8080 port:
//   SMOKE_URL=http://localhost:PORT/ node scripts/smoke-resume.mjs
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
function fail(msg) { console.log('\n❌ ' + msg); process.exit(1); }
try {
    const page = await browser.newPage();
    await page.setViewport({ width: 960, height: 760 });
    page.on('console', (m) => { if (m.type() === 'error') errors.push('console.error: ' + m.text()); });
    page.on('pageerror', (e) => { errors.push('pageerror: ' + e.message); });

    // Start a fresh bot game.
    await page.goto(base + 'game.html?new=bot', { waitUntil: 'networkidle0' });
    await page.waitForSelector('.hand .card.selectable, .controls button.primary', { timeout: 15000 });

    // The URL must have been swapped in place to the clean /game/<id> form.
    const url1 = page.url();
    const m = url1.match(/\/game\/([^/?#]+)/);
    if (!m) fail(`after start, URL is not /game/<id>: ${url1}`);
    const id1 = m[1];
    console.log('Started bot game at:', url1);

    // Make a real move: select two cards and confirm the discard, so the game is
    // now genuinely mid-hand (past the opening deal).
    await page.waitForSelector('.hand .card.selectable', { timeout: 15000 });
    await page.evaluate(() => {
        const cards = document.querySelectorAll('.hand .card.selectable');
        cards[0]?.click();
        cards[1]?.click();
    });
    await wait(120);
    await page.evaluate(() => document.querySelector('.controls button.primary')?.click());
    await wait(600); // let the discard POST + reply deltas animate

    // Capture the resumable state: the human's score and the number of cards in
    // hand (a fresh game would deal 6 and offer selectable cards again).
    const before = await page.evaluate(() => ({
        handCards: document.querySelectorAll('.seat.you .hand .card').length,
        score: document.querySelector('.vs-row .vs-score')?.textContent ?? null,
        savedIds: Object.keys(JSON.parse(localStorage.getItem('cribbager:games') || '{}')),
    }));
    console.log('Before reload:', JSON.stringify(before));
    if (!before.savedIds.includes(id1)) fail('game was not persisted to localStorage');

    // RELOAD at the game's own URL — the crux of the fix.
    await page.reload({ waitUntil: 'networkidle0' });
    await page.waitForSelector('.vs-box, .hand .card', { timeout: 15000 });
    await wait(400);

    const url2 = page.url();
    const m2 = url2.match(/\/game\/([^/?#]+)/);
    if (!m2) fail(`after reload, URL is not /game/<id>: ${url2}`);
    const id2 = m2[1];
    if (id2 !== id1) fail(`refresh created a NEW game (${id2}) instead of resuming ${id1}`);

    const after = await page.evaluate(() => ({
        board: !!document.querySelector('.vs-box'),
        hand: document.querySelectorAll('.seat.you .hand .card').length,
        savedIds: Object.keys(JSON.parse(localStorage.getItem('cribbager:games') || '{}')),
    }));
    console.log('After reload :', JSON.stringify(after));
    if (!after.board) fail('board did not render after reload');
    if (!after.savedIds.includes(id1)) fail('resumed game missing from localStorage after reload');
    if (errors.length) fail('console/page errors: ' + errors.join(' | '));

    console.log('\n✅ Bot game RESUMED the same game (' + id1 + ') after refresh');
    process.exit(0);
}
finally {
    await browser.close();
}
