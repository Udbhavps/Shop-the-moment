// End-to-end: TV -> send to phone -> phone cart -> demo checkout -> dashboard -> eval page.
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
await tv.waitForSelector('.detail'); await tv.click('.detail .btn:has-text("Send to phone")');
await tv.waitForSelector('#handoff.open');
const url = await tv.evaluate(() => document.querySelector('#handoff').innerText.match(/https?:\/\/\S+\/m\/\w+/)?.[0]);
if (!url) throw new Error('no handoff URL on TV');
const sid = url.split('/m/')[1];

const ctx = await browser.newContext({ viewport: { width: 390, height: 844 }, deviceScaleFactor: 2, isMobile: true });
const ph = await ctx.newPage(); watch(ph, 'phone');
await ph.goto(BASE + '/m/' + sid);
await ph.waitForSelector('.item');
await ph.screenshot({ path: `${OUT}/06-phone.png` });
await ph.click('.item .btn.primary');
await ph.waitForSelector('.cart');
await ph.screenshot({ path: `${OUT}/07-phone-cart.png` });
await ph.click('.stick .btn');
await ph.waitForSelector('.done');
await ph.screenshot({ path: `${OUT}/08-phone-done.png` });
const noHScroll = await ph.evaluate(() => document.documentElement.scrollWidth <= innerWidth);

await tv.waitForFunction(() => /opened|connected|placed/i.test(document.querySelector('#handoff').innerText), null, { timeout: 8000 }).catch(() => errors.push('tv did not show phone connected'));
await tv.screenshot({ path: `${OUT}/09-tv-connected.png` });

const d = await browser.newPage({ viewport: { width: 1280, height: 900 } }); watch(d, 'dash');
await d.goto(BASE + '/dashboard'); await d.waitForSelector('#live .stage'); await d.waitForTimeout(500);
await d.screenshot({ path: `${OUT}/10-dashboard.png`, fullPage: true });
const live = await d.evaluate(() => [...document.querySelectorAll('#live .stage')].map((s) => s.innerText.replace(/\n/g, ' ')));

const e = await browser.newPage({ viewport: { width: 1280, height: 900 } }); watch(e, 'eval');
await e.goto(BASE + '/eval'); await e.waitForFunction(() => !document.querySelector('#run').disabled);
await e.click('#run'); await e.waitForFunction(() => document.querySelector('#run').textContent === 'Run again', null, { timeout: 60000 });
await e.screenshot({ path: `${OUT}/11-eval.png`, fullPage: true });
const summary = await e.evaluate(() => document.querySelector('#summary').innerText.replace(/\n/g, ' | '));

console.log(JSON.stringify({ sid, noHScroll, live, summary, errors }, null, 2));
await browser.close();
