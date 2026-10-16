// Server restarts lose in-memory sessions. The TV page must recover quietly when a shopper clicks Buy now afterwards.
// Usage: RESTART_CMD=/path/to/restart-script NODE_PATH=$(npm root -g) BASE_URL=http://localhost:8080 node scripts/session_recover.mjs
import { createRequire } from 'node:module';
import { execSync } from 'node:child_process';
const require = createRequire(import.meta.url);
const { chromium } = require('playwright');
const BASE = process.env.BASE_URL || 'http://localhost:8080';
if (!process.env.RESTART_CMD) { console.log('set RESTART_CMD to a command that restarts the server'); process.exit(2); }
const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 1920, height: 1080 } });
const errors = [];
page.on('pageerror', (e) => errors.push(e.message));
await page.goto(BASE + '/');
await page.waitForFunction(() => document.querySelector('#screen video.on')?.currentTime > 0.3);
await page.keyboard.press('Enter');
await page.waitForSelector('.card.hero');
execSync(process.env.RESTART_CMD, { stdio: 'inherit' });       // all sessions are gone now
await page.click('.card.hero');
await page.click('.btn.primary');                                // Buy now with a dead session
await page.waitForSelector('#pay.open .pay-qr img', { timeout: 10000 });
const text = await page.textContent('#pay');
const ok = /₹/.test(text) && !/not found/i.test(text) && errors.length === 0;
console.log(ok ? 'PASS buy now recovers after a server restart' : 'FAIL ' + text.slice(0, 200) + errors.join('|'));
await browser.close();
process.exit(ok ? 0 : 1);
