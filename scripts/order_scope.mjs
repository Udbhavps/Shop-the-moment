// Checks that "Order placed" applies only to the product that was bought.
// Usage: BASE_URL=http://localhost:8099 node scripts/order_scope.mjs  (needs playwright)
import { createRequire } from 'node:module';
const { chromium } = createRequire(import.meta.url)('playwright');
const BASE = process.env.BASE_URL || 'http://localhost:8099';
const b = await chromium.launch(); const page = await b.newPage({ viewport: { width: 1920, height: 1080 } });
page.setDefaultTimeout(15000);
const fail = (m) => { console.error('FAIL', m); process.exitCode = 1; };
await page.goto(BASE + '/'); await page.waitForSelector('#adbar button'); await page.waitForTimeout(2500);
await page.keyboard.press('Enter'); await page.waitForSelector('#body .chips .chip');
const label = async (n) => { await page.click(`#body .chips .chip:nth-child(${n})`); await page.click('.card.hero'); await page.waitForSelector('.detail'); const t = (await page.textContent('.detail .btn.primary')).trim(); return t; };
const back = async () => { await page.keyboard.press('Escape'); await page.waitForSelector('#body .chips .chip'); };
if ((await label(1)) !== 'Buy now') fail('item 1 should offer Buy now before any order');
await page.click('.detail .btn:has-text("Buy now")'); await page.waitForSelector('#pay.open .pay-qr img');
await page.click('#pay .btn:has-text("Pay with demo card")'); await page.waitForSelector('#pay .pay-done');
await page.click('#pay .btn:has-text("Keep browsing")').catch(() => page.keyboard.press('Escape'));
await page.waitForTimeout(500);
await page.keyboard.press('Escape'); await page.waitForTimeout(300);
const first = await label(1); await back(); const second = await label(2);
console.log('bought item shows:', first, '| other item shows:', second);
if (first !== 'Order placed') fail('the bought item should show Order placed');
if (second !== 'Buy now') fail('a different item must still offer Buy now');
await page.click('.detail .btn:has-text("Buy now")'); await page.waitForSelector('#pay.open .pay-qr img');
console.log('second purchase reaches payment');
await b.close();
if (!process.exitCode) console.log('OK');
