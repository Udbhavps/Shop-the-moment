// Renders the animated sample ad clips from the same vector scenes the app serves.
// Each scene becomes a short clip: products slide in, a light streak crosses, dust drifts.
// After the entrance (about 1.6 s) the products stand still, so the cached bounding boxes stay accurate.
//
// Usage (server must be running; ffmpeg must be installed):
//   (COMBINE_ONLY=1 skips rendering and only rebuilds the full-<ad>.mp4 files)
//   NODE_PATH=$(npm root -g) BASE_URL=http://localhost:8080 OUT=web/video FRAMES=/tmp/frames node scripts/render_videos.mjs [sceneId ...]
import { createRequire } from 'node:module';
import fs from 'node:fs';
import path from 'node:path';
import { spawnSync } from 'node:child_process';
const require = createRequire(import.meta.url);
const { chromium } = require('playwright');

const BASE = process.env.BASE_URL || 'http://localhost:8080';
const OUT = process.env.OUT || 'web/video';
const FRAMES = process.env.FRAMES || '/tmp/stm_frames';
const FPS = 30;
const SCALE = 1.5; // 1280x720 scene -> 1920x1080 video
const CONC = Number(process.env.CONC || 1);

const cfg = await (await fetch(BASE + '/api/config')).json().catch(() => ({}));
const adsRes = await (await fetch(BASE + '/api/ads')).json();
const ads = adsRes.ads || adsRes;
const scenes = ads.flatMap((a) => a.scenes.map((s) => ({ ad: a, s })));
const only = process.argv.slice(2);
const todo = process.env.COMBINE_ONLY ? [] : only.length ? scenes.filter((x) => only.includes(x.s.id)) : scenes;
fs.mkdirSync(OUT, { recursive: true });

const PAGE = (svg, dur) => `<!doctype html><html><body style="margin:0;background:#000;overflow:hidden">
<div id="root" style="width:1280px;height:720px">${svg}</div>
<script>
const NS = 'http://www.w3.org/2000/svg';
const D = ${dur};
const root = document.querySelector('#root > svg');
const kids = [...root.children].filter((n) => n.tagName !== 'defs');
const firstItem = kids.findIndex((n) => n.tagName === 'ellipse');
const lastSvg = kids.map((n) => n.tagName).lastIndexOf('svg');
const wrap = (nodes, extra) => { const g = document.createElementNS(NS, 'g'); nodes[0].before(g); nodes.forEach((n) => g.append(n)); return g; };
const bgNodes = kids.slice(0, firstItem);
const itemNodes = kids.slice(firstItem, lastSvg + 1);
const lowerNodes = kids.slice(lastSvg + 1);
const gBg = wrap(bgNodes);
const items = [];
for (let i = 0; i < itemNodes.length; i += 2) items.push({ g: wrap(itemNodes.slice(i, i + 2)), svg: itemNodes[i + 1] });
const gFx = document.createElementNS(NS, 'g'); lowerNodes[0].before(gFx);
const gLower = wrap(lowerNodes);

// light streak
const defs = root.querySelector('defs');
defs.insertAdjacentHTML('beforeend', '<linearGradient id="streak" x1="0" y1="0" x2="1" y2="0"><stop offset="0" stop-color="#fff" stop-opacity="0"/><stop offset=".5" stop-color="#fff" stop-opacity=".22"/><stop offset="1" stop-color="#fff" stop-opacity="0"/></linearGradient>');
const streak = document.createElementNS(NS, 'polygon');
streak.setAttribute('points', '0,0 150,0 90,720 -60,720'); streak.setAttribute('fill', 'url(#streak)');
gFx.append(streak);
// dust motes (deterministic)
let seed = 7; const rnd = () => (seed = (seed * 16807) % 2147483647) / 2147483647;
const motes = Array.from({ length: 34 }, () => {
  const c = document.createElementNS(NS, 'circle'); c.setAttribute('fill', '#fff'); gFx.append(c);
  return { c, x: rnd() * 1280, y: rnd() * 720, r: 1.4 + rnd() * 3, sp: 10 + rnd() * 26, ph: rnd() * 6.28, o: 0.18 + rnd() * 0.32 };
});

const clamp = (v) => Math.min(1, Math.max(0, v));
const outBack = (p) => { const c1 = 1.5, c3 = c1 + 1; return 1 + c3 * Math.pow(p - 1, 3) + c1 * Math.pow(p - 1, 2); };
const outCubic = (p) => 1 - Math.pow(1 - p, 3);

window.render = (t) => {
  // background: very slow push-in (items are not inside this group, so boxes never move)
  const z = 1 + 0.05 * (t / D);
  gBg.setAttribute('transform', 'translate(640 360) scale(' + z + ') translate(-640 -360)');
  gBg.querySelectorAll('circle').forEach((c, i) => c.setAttribute('opacity', String(0.18 + 0.05 * Math.sin(t * 1.3 + i))));
  items.forEach((it, k) => {
    const p = clamp((t - (0.25 + 0.35 * k)) / 0.95);
    const e = outBack(p), a = clamp(p * 3);
    const x = +it.svg.getAttribute('x'), y = +it.svg.getAttribute('y'), w = +it.svg.getAttribute('width');
    const cx = x + w / 2, cy = y + w / 2;
    const s = 0.9 + 0.1 * e;
    it.g.setAttribute('opacity', String(a));
    it.g.setAttribute('transform', 'translate(' + cx + ' ' + (cy + (1 - e) * 80) + ') scale(' + s + ') translate(' + -cx + ' ' + -cy + ')');
  });
  const lp = outCubic(clamp((t - 0.45) / 0.7));
  gLower.setAttribute('transform', 'translate(0 ' + (1 - lp) * 100 + ')');
  const sp = clamp((t - 2.6) / 2.0);
  streak.setAttribute('transform', 'translate(' + (-300 + sp * 1750) + ' 0)');
  streak.setAttribute('opacity', sp > 0 && sp < 1 ? '1' : '0');
  motes.forEach((m) => {
    const x = (m.x + t * m.sp * 0.6 + Math.sin(t * 0.8 + m.ph) * 14) % 1280;
    const y = ((m.y - t * m.sp) % 720 + 720) % 720;
    m.c.setAttribute('cx', x); m.c.setAttribute('cy', y); m.c.setAttribute('r', m.r);
    m.c.setAttribute('opacity', String(m.o * (0.6 + 0.4 * Math.sin(t * 1.7 + m.ph))));
  });
};
window.render(0);
</script></body></html>`;

async function renderScene(browser, { ad, s }) {
  const dur = ad.seconds_per_scene;
  const svg = await (await fetch(BASE + s.svg_url)).text();
  const page = await browser.newPage({ viewport: { width: 1280, height: 720 }, deviceScaleFactor: SCALE });
  await page.setContent(PAGE(svg, dur));
  const dir = path.join(FRAMES, s.id);
  fs.rmSync(dir, { recursive: true, force: true });
  fs.mkdirSync(dir, { recursive: true });
  const n = dur * FPS;
  for (let i = 0; i < n; i++) {
    await page.evaluate((t) => window.render(t), i / FPS);
    for (let tries = 0; ; tries++) {
      try { await page.screenshot({ path: path.join(dir, String(i).padStart(4, '0') + '.jpg'), type: 'jpeg', quality: 92, timeout: 60000 }); break; }
      catch (e) { if (tries >= 2) throw e; }
    }
  }
  await page.close();
  const inp = path.join(dir, '%04d.jpg');
  const run = (args) => { const r = spawnSync('ffmpeg', ['-y', '-loglevel', 'error', ...args], { stdio: 'inherit' }); if (r.status) throw new Error('ffmpeg failed for ' + s.id); };
  run(['-framerate', String(FPS), '-i', inp, '-c:v', 'libx264', '-preset', 'slow', '-crf', '21', '-pix_fmt', 'yuv420p', '-movflags', '+faststart', '-an', path.join(OUT, s.id + '.mp4')]);
  run(['-framerate', String(FPS), '-i', inp, '-c:v', 'libvpx-vp9', '-crf', '33', '-b:v', '0', '-row-mt', '1', '-pix_fmt', 'yuv420p', '-an', path.join(OUT, s.id + '.webm')]);
  fs.rmSync(dir, { recursive: true, force: true });
  console.log('rendered', s.id);
}

const browser = await chromium.launch();
const queue = [...todo];
await Promise.all(Array.from({ length: CONC }, async () => {
  while (queue.length) await renderScene(browser, queue.shift());
}));
await browser.close();

// one continuous file per ad (for sharing / recording), with a half-second cross-fade between scenes
for (const ad of ads) {
  const files = ad.scenes.map((s) => path.join(OUT, s.id + '.mp4'));
  if (!files.every((f) => fs.existsSync(f)) || (only.length && !ad.scenes.some((s) => only.includes(s.id)))) continue;
  const x = 0.5, d = ad.seconds_per_scene;
  let filter = '', last = '[0:v]';
  for (let i = 1; i < files.length; i++) {
    const out = i === files.length - 1 ? '[v]' : `[x${i}]`;
    filter += `${last}[${i}:v]xfade=transition=fade:duration=${x}:offset=${(d - x) * i}${out};`;
    last = out;
  }
  const args = ['-y', '-loglevel', 'error', ...files.flatMap((f) => ['-i', f]), '-filter_complex', filter.slice(0, -1), '-map', '[v]', '-c:v', 'libx264', '-preset', 'slow', '-crf', '21', '-pix_fmt', 'yuv420p', '-movflags', '+faststart', path.join(OUT, 'full-' + ad.id + '.mp4')];
  const r = spawnSync('ffmpeg', args, { stdio: 'inherit' });
  if (r.status) throw new Error('concat failed for ' + ad.id);
  console.log('combined', ad.id);
}
