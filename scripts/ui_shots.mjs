// Headless-browser walkthrough of the TV flow with screenshots.
// Usage: NODE_PATH=<dir containing playwright> BASE_URL=http://localhost:8080 OUT=./shots node scripts/ui_shots.mjs
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
page.on('requestfailed', (r) => errors.push('requestfailed: ' + r.url()));

await page.goto(BASE + '/');
await page.waitForSelector('#invite.show', { timeout: 8000 });
await page.screenshot({ path: `${OUT}/01-ad.png` });

await page.keyboard.press('Enter');                         // OK on the remote
await page.waitForSelector('.card', { timeout: 8000 });
await page.waitForTimeout(600);
await page.screenshot({ path: `${OUT}/02-results.png` });

await page.click('.card.similar');                          // open a similar item
await page.waitForSelector('.detail');
await page.screenshot({ path: `${OUT}/03-detail.png` });
await page.keyboard.press('Escape');
await page.waitForSelector('.chips');

await page.click('.suggest button:nth-child(1)');           // "Show me a cheaper one"
await page.waitForSelector('#reply:not(.pending)');
await page.waitForTimeout(300);
await page.screenshot({ path: `${OUT}/04-chat.png` });

await page.keyboard.press('Escape');
await page.click('.card.hero');
await page.waitForSelector('.detail');
await page.click('.detail .btn:has-text("Send to phone")');   // Send to phone
await page.waitForSelector('#handoff.open');
await page.waitForTimeout(400);
await page.screenshot({ path: `${OUT}/05-handoff.png` });

console.log(JSON.stringify({ errors }, null, 2));
await browser.close();
