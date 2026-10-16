// Drives the mobile play prototype (/m/) through a full game in a phone-size
// headless browser: taps whatever the screen offers (pack, discard, card, the one
// button) until the game-over card, checking the hold sequence on the way.
//   SMOKE_URL=http://localhost:8091/ node scripts/smoke-mobile.mjs
//   SHOT_DIR=/some/dir   also saves a screenshot the first time each state appears
import puppeteer from 'puppeteer-core';
import { mkdirSync } from 'node:fs';
import { join } from 'node:path';

const CHROME = process.env.CHROME ?? '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome';
const base = process.env.SMOKE_URL ?? 'http://localhost:8080/';
const shotDir = process.env.SHOT_DIR;
if (shotDir) mkdirSync(shotDir, { recursive: true });
const wait = (ms) => new Promise((r) => setTimeout(r, ms));
const errors = [];
const browser = await puppeteer.launch({ executablePath: CHROME, headless: true, args: ['--no-sandbox', '--disable-gpu'] });
try {
  const page = await browser.newPage();
  await page.setViewport({ width: 390, height: 844, deviceScaleFactor: 2, isMobile: true, hasTouch: true });
  page.on('console', (m) => { if (m.type() === 'error') errors.push('console.error: ' + m.text()); });
  page.on('pageerror', (e) => errors.push('pageerror: ' + e.message));
  await page.goto(base + 'm/?new=1', { waitUntil: 'networkidle0' });

  const seen = new Set();
  const holds = [];
  const snap = async (name) => {
    if (!shotDir || seen.has(name)) return;
    seen.add(name);
    await wait(450); // let the short card animations land
    await page.screenshot({ path: join(shotDir, name + '.png') });
  };
  const state = () => page.evaluate(() => {
    const a = document.getElementById('app');
    const hasHold = !!a.dataset.hold;
    return {
      hold: a.dataset.hold, prompt: a.dataset.prompt, pending: Number(a.dataset.pending),
      over: !!document.querySelector('.sheet[aria-label="Game over"]'),
      speech: [...document.querySelectorAll('.speech')].map((e) => e.textContent).join('|'),
      hasHold,
    };
  });
  let plays = 0, hands = 0;
  let flaky = 0;
  for (let i = 0; i < 4000; i++) {
   try {
    const s = await state();
    if (s.over) { await snap('game-over'); break; }
    if (s.hold) {
      if (holds.at(-1) !== s.hold || s.hold === 'nextHand') holds.push(s.hold);
      await snap('hold-' + s.hold);
      if (s.hold === 'nextHand') await snap('hold-nextHand-' + (seen.has('hold-nextHand-1') ? 2 : 1));
      if (s.hold === 'cut') await page.click('.pack');
      else { if (s.hold === 'deal') hands++; await page.click('.slot .btn'); }
    } else if (s.prompt === 'discard') {
      await snap('discard-prompt');
      await page.click('.my-hand .card.tap'); // a chosen card leaves the hand for the crib row
      await page.click('.my-hand .card.tap');
      await snap('discard-selected');
      await page.click('.rc .btn');
    } else if (s.prompt === 'play') {
      await snap(s.speech.includes('GO') ? 'play-after-go' : 'play-prompt');
      if (!(await page.$('.my-hand .card.tap'))) throw new Error('prompt=play but no playable card');
      await page.click('.my-hand .card.tap');
      plays++;
      if (holds.at(-1) !== 'play') holds.push('play');
    } else {
      await wait(40);
    }
    await wait(15);
   } catch (e) {
    // the table re-renders under us; a missed tap is retried on the next look
    if (++flaky > 40) throw e;
   }
  }
  const final = await state();
  if (!final.over) errors.push('game did not finish');
  const bad = holds.join(',').match(/go,next/);
  if (bad) errors.push('a Go hold was followed by a Next hold: ' + bad);
  console.log(`hands=${hands} plays=${plays} holds=${holds.length}`);
  if (process.env.TRACE) console.log(holds.join(' '));
  console.log('hold kinds: ' + [...new Set(holds)].join(' '));
} catch (e) {
  errors.push(String(e));
  if (shotDir) await (await browser.pages())[1]?.screenshot({ path: join(shotDir, 'failure.png') });
} finally {
  await browser.close();
}
if (errors.length) { console.error(errors.join('\n')); process.exit(1); }
console.log('mobile playthrough ok');
