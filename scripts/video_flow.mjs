// Checks that the TV page plays the ad clips and that pausing identifies products on the settled frame.
// Usage: NODE_PATH=$(npm root -g) BASE_URL=http://localhost:8080 OUT=./shots node scripts/video_flow.mjs
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

const live = () => page.evaluate(() => { const v = [...document.querySelectorAll('#screen video')].find((x) => x.classList.contains('on')); return v && { t: v.currentTime, paused: v.paused, src: v.currentSrc, w: v.videoWidth, ready: v.readyState }; });
await page.waitForFunction(() => { const v = document.querySelector('#screen video.on'); return v && v.currentTime > 0.2; }, null, { timeout: 15000 });
let a = await live();
check(a && a.w === 1920 && /\/video\/morning-run-1\.(webm|mp4)$/.test(a.src), 'clip plays: ' + (a && a.src.replace(BASE, '')) + ' ' + (a && a.w) + 'px wide');
await page.waitForTimeout(600);
let b = await live();
check(b.t > a.t, `video clock advances (${a.t.toFixed(2)} -> ${b.t.toFixed(2)})`);

// Press OK early, during the entrance: it must settle on the finished frame.
await page.keyboard.press('Enter');
await page.waitForSelector('#boxes > *', { timeout: 10000 });
const p = await live();
check(p.paused && p.t >= 1.69, `paused on a settled frame (t=${p.t.toFixed(2)})`);
const nBox = await page.locator('#boxes > *').count();
check(nBox >= 2, `${nBox} detection boxes drawn`);
await page.waitForTimeout(500);
await page.screenshot({ path: OUT + '/video_paused.png' });
const still = await live();
check(Math.abs(still.t - p.t) < 0.01, 'frame stays still while the panel is open');

// Close the panel: playback resumes and carries on to the next scene.
await page.keyboard.press('Escape');
await page.waitForFunction(() => { const v = document.querySelector('#screen video.on'); return v && !v.paused; }, null, { timeout: 5000 });
check(true, 'playback resumes after closing the panel');
await page.waitForFunction(() => /morning-run-2/.test(document.querySelector('#screen video.on')?.currentSrc || ''), null, { timeout: 15000 });
check(true, 'moves on to the next scene');

check(errors.length === 0, 'no page errors' + (errors.length ? ': ' + errors.join(' | ') : ''));
await browser.close();
process.exit(failed ? 1 : 0);
