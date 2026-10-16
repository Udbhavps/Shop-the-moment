// Neon Harbor (sample show): story card, prop labelling, official merchandise badge, "shop the look" checkout.
// Usage: NODE_PATH=$(npm root -g) BASE_URL=http://localhost:8080 OUT=./shots node scripts/show_flow.mjs
import { createRequire } from 'node:module';
import fs from 'node:fs';
const require = createRequire(import.meta.url);
const { chromium } = require('playwright');
const BASE = process.env.BASE_URL || 'http://localhost:8080';
const OUT = process.env.OUT || './shots';
fs.mkdirSync(OUT, { recursive: true });
let failed = 0;
const check = (ok, msg) => { console.log((ok ? 'PASS ' : 'FAIL ') + msg); if (!ok) failed++; };

const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 1920, height: 1080 } });
const errors = [];
page.on('pageerror', (e) => errors.push(e.message));
page.on('console', (m) => { if (m.type() === 'error') errors.push(m.text()); });
await page.goto(BASE + '/');
await page.click('#adbar button:has-text("Neon Harbor")');
await page.waitForFunction(() => document.querySelector('#screen video.on')?.currentSrc.includes('neon-harbor-1') && document.querySelector('#screen video.on').currentTime > 0.3, null, { timeout: 20000 });
await page.keyboard.press('Enter');
await page.waitForSelector('.story', { timeout: 10000 });

const chips = await page.$$eval('#body .chips .chip', (els) => els.map((e) => e.textContent.trim()));
check(chips.length === 3, 'three items found: ' + JSON.stringify(chips));
let story = await page.textContent('.story');
check(/Worn by Mira Vale/.test(story) && /Neon Harbor · S1 E3/.test(story), 'jacket shows who wears it: ' + story.replace(/\s+/g, ' ').slice(0, 90));
check(await page.locator('.btn.look').count() === 1, 'shop-the-look button: ' + (await page.textContent('.btn.look')).replace(/\s+/g, ' '));
await page.screenshot({ path: OUT + '/show_worn.png' });

// the pendant is a prop and the official replica
await page.click('#body .chips .chip:nth-child(3)');
await page.waitForFunction(() => /Prop from the show/.test(document.querySelector('.story')?.textContent || ''));
check(true, 'pendant is labelled a prop from the show');
await page.screenshot({ path: OUT + '/show_prop.png' });
await page.click('.card.hero');
await page.waitForSelector('.licensed');
check(/Official Neon Harbor merchandise/.test(await page.textContent('.licensed')), 'replica is marked official merchandise');
await page.screenshot({ path: OUT + '/show_merch.png' });
await page.keyboard.press('Escape');
await page.waitForSelector('.btn.look');

// shop the look: all three exact items go through the UCP cart
const prices = await Promise.all(['northpeak-shell', 'tempo-one', 'tidewell-compass'].map(async (id) => (await (await fetch(`${BASE}/api/products/${id}`)).json()).price));
const want = prices.reduce((a, b) => a + b, 0);
await page.click('.btn.look');
await page.waitForSelector('#pay.open .pay-qr img', { timeout: 10000 });
const payText = await page.textContent('#pay');
const fmt = new Intl.NumberFormat('en-IN').format(want);
check(payText.includes(fmt), `checkout total is ₹${fmt} for the three items`);
check(/3 items|× ?1/.test(payText) || (payText.match(/Stride|Northpeak|NorthPeak/i)), 'cart lines listed');
await page.screenshot({ path: OUT + '/show_checkout.png' });

check(errors.length === 0, 'no page errors' + (errors.length ? ': ' + errors.join(' | ') : ''));
await browser.close();
process.exit(failed ? 1 : 0);
