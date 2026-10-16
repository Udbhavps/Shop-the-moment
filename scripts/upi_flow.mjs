// TV Buy now -> UPI QR -> phone approves -> TV shows the order. Also the card fallback.
import { createRequire } from 'node:module';
import fs from 'node:fs';
const require = createRequire(import.meta.url);
const { chromium } = require('playwright');
const BASE = process.env.BASE_URL || 'http://localhost:8080';
const OUT = process.env.OUT || './shots';
fs.mkdirSync(OUT, { recursive: true });
const browser = await chromium.launch();
const errors = [];
const watch = (p, n) => { p.on('pageerror', (e) => errors.push(n + ' pageerror: ' + e.message)); p.on('console', (m) => { if (m.type() === 'error') errors.push(n + ' console: ' + m.text()); }); };

const tv = await browser.newPage({ viewport: { width: 1920, height: 1080 } }); watch(tv, 'tv');
await tv.goto(BASE + '/');
await tv.waitForSelector('#invite.show'); await tv.keyboard.press('Enter');
await tv.waitForSelector('.card.hero'); await tv.click('.card.hero');
await tv.waitForSelector('.detail'); await tv.click('.detail .btn:has-text("Buy now")');
await tv.waitForSelector('#pay .pay-qr img');
await tv.waitForTimeout(400);
await tv.screenshot({ path: `${OUT}/14-tv-upi.png` });
const payUrl = await tv.evaluate(() => decodeURIComponent(document.querySelector('#pay .pay-qr img').src.split('text=')[1]));

const ctx = await browser.newContext({ viewport: { width: 390, height: 844 }, deviceScaleFactor: 2, isMobile: true });
const ph = await ctx.newPage(); watch(ph, 'phone');
await ph.goto(payUrl.replace(/^https?:\/\/[^/]+/, BASE));
await ph.waitForSelector('.payto');
await ph.screenshot({ path: `${OUT}/15-phone-approve.png` });
await ph.click('.stick .btn.primary');
await ph.waitForSelector('.done');
await ph.screenshot({ path: `${OUT}/16-phone-approved.png` });

await tv.waitForSelector('#pay .pay-done', { timeout: 8000 });
await tv.screenshot({ path: `${OUT}/17-tv-paid.png` });
const text = await tv.evaluate(() => document.querySelector('#pay').innerText.replace(/\n/g, ' | '));
console.log(JSON.stringify({ payUrl, tv: text, errors }, null, 2));
await browser.close();
