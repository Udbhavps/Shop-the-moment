// Records the full Shop the Moment demo flow as a 1920x1080 video, paced for narration.
// Usage:  BASE_URL=https://shop-the-moment-165921751280.asia-south1.run.app node scripts/record_demo.mjs
// Needs:  npm i playwright && npx playwright install chromium   (run on your Mac or in Cloud Shell)
// Output: ./demo-recording/demo.webm  plus  ./demo-recording/marks.json (time of each step, seconds)
import { createRequire } from 'node:module';
import fs from 'node:fs';
const require = createRequire(import.meta.url);
const { chromium } = require('playwright');
const BASE = (process.env.BASE_URL || 'http://localhost:8099').replace(/\/$/, '');
const OUT = process.env.OUT || './demo-recording';
fs.mkdirSync(OUT, { recursive: true });
const browser = await chromium.launch();
const ctx = await browser.newContext({ viewport: { width: 1920, height: 1080 }, recordVideo: { dir: OUT, size: { width: 1920, height: 1080 } } });
const page = await ctx.newPage(); page.setDefaultTimeout(10000);
const t0 = Date.now(); const marks = [];
const mark = (name) => { marks.push({ name, t: +((Date.now() - t0) / 1000).toFixed(2) }); console.log(marks.at(-1).t, name); };
const wait = (s) => page.waitForTimeout(s * 1000);
const tryDo = async (name, fn) => { try { await fn(); } catch (e) { console.log('SKIP', name, e.message.split('\n')[0]); } };

await page.goto(BASE + '/'); mark('tv_loaded');
await page.waitForFunction(() => document.querySelector('#screen video.on')?.currentTime > 0.5, null, { timeout: 30000 }).catch(() => {});
await wait(3);                                              // the ad plays
mark('pause_press'); await page.keyboard.press('Enter');    // pause and identify
await page.waitForSelector('#body .chips .chip', { timeout: 30000 }); mark('products_found');
await wait(3.5);
await tryDo('chip2', async () => { await page.click('#body .chips .chip:nth-child(2)'); mark('chip2'); await wait(2); await page.click('#body .chips .chip:nth-child(1)'); await wait(1.5); });
await tryDo('detail', async () => { await page.click('.card.hero'); await page.waitForSelector('.detail'); mark('product_detail'); await wait(4); await page.keyboard.press('Escape'); await wait(1); });
await tryDo('ask', async () => { await page.click('#suggest button:nth-child(1)'); mark('ask_cheaper'); await wait(6); });
await tryDo('buy', async () => { await page.keyboard.press('Escape'); await wait(0.5); await page.click('#body .chips .chip:nth-child(1)'); await wait(0.8); await page.click('.card.hero'); await page.waitForSelector('.detail'); await page.click('.detail .btn:has-text("Buy now")'); await page.waitForSelector('#pay.open .pay-qr img'); mark('pay_qr'); await wait(5); await page.click('#pay .btn:has-text("Keep browsing")'); await wait(1); });
await tryDo('show', async () => { await page.click('#adbar button:has-text("Neon Harbor")'); await page.waitForFunction(() => document.querySelector('#screen video.on')?.currentSrc.includes('neon-harbor-1') && document.querySelector('#screen video.on').currentTime > 0.3, null, { timeout: 20000 }); await wait(2); mark('show_playing'); await page.keyboard.press('Enter'); await page.waitForSelector('.story', { timeout: 30000 }); mark('story_card'); await wait(5); await page.click('#body .chips .chip:nth-child(3)'); mark('prop_card'); await wait(3.5); });
await tryDo('look', async () => { await page.keyboard.press('Escape'); await page.waitForSelector('.btn.look'); await page.click('.btn.look'); await page.waitForSelector('#pay.open .pay-qr img'); mark('shop_look_pay'); await wait(5); await page.click('#pay .btn:has-text("Pay with demo card")'); await page.waitForSelector('#pay .pay-done'); mark('order_done'); await wait(4); });
await tryDo('dashboard', async () => { await page.goto(BASE + '/dashboard'); mark('dashboard'); await wait(6); });
await tryDo('config', async () => { await page.goto(BASE + '/api/config'); mark('api_config'); await wait(3); });
const video = page.video(); await ctx.close(); const p = await video.path(); fs.renameSync(p, OUT + '/demo.webm');
fs.writeFileSync(OUT + '/marks.json', JSON.stringify(marks, null, 1)); await browser.close(); console.log('saved', OUT + '/demo.webm');
