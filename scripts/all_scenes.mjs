// Pauses every scene of every sample ad/show and checks the boxes and items.
// Usage: NODE_PATH=$(npm root -g) BASE_URL=http://localhost:8080 OUT=./shots node scripts/all_scenes.mjs
import { createRequire } from 'node:module';
import fs from 'node:fs';
const require = createRequire(import.meta.url);
const { chromium } = require('playwright');
const BASE = process.env.BASE_URL || 'http://localhost:8080';
const OUT = process.env.OUT || './shots';
fs.mkdirSync(OUT, { recursive: true });
const ads = (await (await fetch(BASE + '/api/ads')).json()).ads;
const br = await chromium.launch();
let bad = 0;
for (const ad of ads) {
  const page = await br.newPage({ viewport: { width: 1920, height: 1080 } });
  const errs = []; page.on('pageerror', e => errs.push(e.message)); page.on('console', m => m.type()==='error' && errs.push(m.text()));
  await page.goto(BASE + '/');
  await page.click(`#adbar button:has-text("${ad.title}")`);
  for (let i = 0; i < ad.scenes.length; i++) {
    const sc = ad.scenes[i];
    await page.waitForFunction((id) => document.querySelector('#screen video.on')?.currentSrc.includes(id) && document.querySelector('#screen video.on').currentTime > 0.3, sc.id, { timeout: 20000 });
    await page.keyboard.press('Enter');
    await page.waitForSelector('#boxes > *', { timeout: 10000 });
    const n = await page.locator('#boxes > *').count();
    const kinds = await page.$$eval('#body .chips button, #body .chips > *', els => els.map(e => e.textContent.trim()));
    const exact = await page.locator('text=Exact match').count();
    const t = await page.evaluate(() => document.querySelector('#screen video.on').currentTime);
    const ok = n === sc.expected.length && t >= 1.69;
    if (!ok) bad++;
    console.log(`${ok ? 'PASS' : 'FAIL'} ${sc.id}: ${n}/${sc.expected.length} boxes, t=${t.toFixed(2)}, items=${JSON.stringify(kinds)}`);
    await page.screenshot({ path: `${OUT}/scene_${sc.id}.png` });
    await page.keyboard.press('Escape');
    // wait for the scene change to the next one
    if (i < ad.scenes.length - 1) await page.waitForFunction((id) => document.querySelector('#screen video.on')?.currentSrc.includes(id), ad.scenes[i+1].id, { timeout: 15000 });
  }
  console.log(errs.length ? 'ERRORS ' + errs.join(' | ') : `no errors in ${ad.id}`);
  if (errs.length) bad++;
  await page.close();
}
await br.close(); process.exit(bad ? 1 : 0);
