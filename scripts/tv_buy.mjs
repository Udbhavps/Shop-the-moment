// Buy entirely on the TV: ad -> identify -> product -> Buy now -> change quantity -> pay (demo) -> receipt.
import { createRequire } from 'node:module';
import fs from 'node:fs';
const require = createRequire(import.meta.url);
const { chromium } = require('playwright');
const BASE = process.env.BASE_URL || 'http://localhost:8080';
const OUT = process.env.OUT || './shots';
fs.mkdirSync(OUT, { recursive: true });
const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 1920, height: 1080 } });
const errors = [];
page.on('pageerror', (e) => errors.push('pageerror: ' + e.message));
page.on('console', (m) => { if (m.type() === 'error') errors.push('console: ' + m.text()); });
await page.goto(BASE + '/');
await page.waitForSelector('#invite.show'); await page.keyboard.press('Enter');
await page.waitForSelector('.card.hero'); await page.click('.card.hero');
await page.waitForSelector('.detail');
await page.click('.detail .btn:has-text("Buy now")');
await page.waitForSelector('#pay.open .pay-line');
await page.click('#pay .plus');                       // quantity 2
await page.waitForFunction(() => document.querySelector('#pay .pay-qty output')?.textContent === '2');
await page.screenshot({ path: `${OUT}/12-tv-pay.png` });
// keyboard only: focus must be on a control and Enter must work
const focusedOK = await page.evaluate(() => !!document.activeElement && document.activeElement.closest('#pay') !== null);
await page.click('#pay .btn.primary');
await page.waitForSelector('#pay .pay-done');
await page.screenshot({ path: `${OUT}/13-tv-done.png` });
const text = await page.evaluate(() => document.querySelector('#pay').innerText.replace(/\n/g, ' | '));
const sess = await page.evaluate(async () => { const r = await fetch('/api/analytics'); return r.json(); });
console.log(JSON.stringify({ focusedOK, text, live: sess.live.funnel.map((f) => f.event + ':' + f.sessions), errors }, null, 2));
await browser.close();
