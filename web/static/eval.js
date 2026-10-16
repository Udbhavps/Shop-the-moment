(() => {
  'use strict';
  const $ = (id) => document.getElementById(id);
  let cfg = {}, ads = [];

  function rasterise(url) {
    return new Promise((resolve, reject) => {
      const im = new Image();
      im.onload = () => { const c = document.createElement('canvas'); c.width = 1920; c.height = 1080; c.getContext('2d').drawImage(im, 0, 0, 1920, 1080); resolve(c.toDataURL('image/jpeg', 0.9)); };
      im.onerror = () => reject(new Error('could not load ' + url));
      im.src = url;
    });
  }

  // Score one scene: every expected item must be found by category; an expected exact id must be the exact match.
  function score(scene, dets) {
    const rows = [];
    let falseExact = 0;
    const used = new Set();
    for (const exp of scene.expected) {
      const d = dets.find((x, i) => !used.has(i) && (x.candidate.category || 'other') === exp.category);
      if (d) used.add(dets.indexOf(d));
      const gotExact = d && d.exact ? d.exact.product.id : '';
      const catOK = !!d;
      const exactOK = gotExact === (exp.product_id || '');
      if (gotExact && gotExact !== exp.product_id) falseExact++;
      rows.push({ exp, d, catOK, exactOK, gotExact });
    }
    return { rows, falseExact };
  }

  async function run() {
    const btn = $('run'); btn.disabled = true; btn.textContent = 'Running';
    const live = !!cfg.live_ai;
    const root = $('scenes'); root.replaceChildren();
    let items = 0, cats = 0, exactOK = 0, claims = 0, claimsRight = 0, falseExact = 0, lat = [];
    for (const ad of ads) for (const sc of ad.scenes) {
      let res;
      try {
        const frame = await rasterise(sc.svg_url);
        const r = await fetch('/api/analyze', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ scene_id: sc.id, frame, mode: live ? 'live' : 'cached' }) });
        res = await r.json(); if (!r.ok) throw new Error(res.error || r.status);
      } catch (e) { const d = document.createElement('div'); d.className = 'scene'; d.textContent = `${sc.id}: ${e.message}`; root.append(d); continue; }
      lat.push(res.latency_ms);
      const s = score(sc, res.detections);
      falseExact += s.falseExact;
      const box = document.createElement('div'); box.className = 'scene';
      const h = document.createElement('h3'); h.textContent = `${sc.title} (${sc.id}) - source: ${res.source}${res.fallback_reason ? ', fallback: ' + res.fallback_reason : ''}`; box.append(h);
      const t = document.createElement('table');
      t.innerHTML = '<tr><th>Expected</th><th>Category found</th><th>Exact claim</th><th>Result</th></tr>';
      for (const r of s.rows) {
        items++; if (r.catOK) cats++; if (r.exactOK) exactOK++;
        if (r.gotExact) { claims++; if (r.gotExact === r.exp.product_id) claimsRight++; }
        const tr = document.createElement('tr');
        const cells = [r.exp.category + (r.exp.product_id ? ' / ' + r.exp.product_id : ' / no exact expected'), r.catOK ? 'yes' : 'no', r.gotExact || 'none', r.exactOK && r.catOK ? 'correct' : 'wrong'];
        cells.forEach((c, i) => { const td = document.createElement('td'); td.textContent = c; if (i === 3) td.className = c === 'correct' ? 'ok' : 'bad'; tr.append(td); });
        t.append(tr);
      }
      box.append(t); root.append(box);
    }
    const pct = (a, b) => b ? Math.round(a / b * 100) + '%' : 'n/a';
    const avg = lat.length ? Math.round(lat.reduce((a, b) => a + b, 0) / lat.length) : 0;
    const sum = [
      [pct(claimsRight, claims), `Exact-claim precision (${claimsRight} of ${claims})`],
      [String(falseExact), 'Wrong exact claims'],
      [pct(cats, items), `Items found by category (${cats} of ${items})`],
      [pct(exactOK, items), `Exact/not-exact decision right (${exactOK} of ${items})`],
      [live ? avg + ' ms' : 'n/a', live ? 'Average Gemini latency' : 'Latency (not measured on saved analysis)'],
    ];
    $('summary').replaceChildren(...sum.map(([b, s]) => { const d = document.createElement('div'); const bb = document.createElement('b'); bb.textContent = b; const ss = document.createElement('span'); ss.textContent = s; d.append(bb, ss); return d; }));
    $('caveat').textContent = live ? 'Scenes are synthetic vector illustrations, so these numbers show the pipeline works on a controlled set, not real-world accuracy.'
      : 'Live AI is off, so this run replays saved detections. It only tests catalog matching and the exact/similar rule, not Gemini itself.';
    btn.disabled = false; btn.textContent = 'Run again';
  }

  (async () => {
    cfg = await (await fetch('/api/config')).json();
    ads = (await (await fetch('/api/ads')).json()).ads;
    $('mode').textContent = cfg.live_ai ? `Live mode: Gemini (${cfg.model}) analyses each scene.` : 'Saved-analysis mode: Gemini is not configured on this server.';
    $('run').disabled = false; $('run').addEventListener('click', run);
  })();
})();
