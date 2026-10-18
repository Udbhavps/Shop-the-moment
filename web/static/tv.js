/* Shop the Moment - TV experience. Plain JS, no build step. */
(() => {
  'use strict';

  const $ = (id) => document.getElementById(id);
  const stage = $('stage');
  const S = {
    cfg: { live_ai: false, currency: 'INR' },
    ads: [], adIdx: 0, sceneIdx: 0, sceneStart: 0, paused: false,
    session: null, mobileUrl: '',
    panelOpen: false, handoffOpen: false, payOpen: false, payBusy: false,
    req: 0,                       // guards against stale async results
    analysis: null, detIdx: 0,
    view: 'list',                 // list | detail | results
    detail: null,                 // {product, color, level, fromAlt}
    results: null,                // last chat result
    history: [],                  // chat turns
    layers: [], layer: 0,
  };

  // ------------------------------------------------------------ helpers ---
  function h(tag, attrs, ...kids) {
    const el = document.createElement(tag);
    for (const [k, v] of Object.entries(attrs || {})) {
      if (v == null || v === false) continue;
      if (k === 'class') el.className = v;
      else if (k.startsWith('on')) el.addEventListener(k.slice(2), v);
      else if (v === true) el.setAttribute(k, '');
      else el.setAttribute(k, v);
    }
    for (const kid of kids.flat()) {
      if (kid == null || kid === false) continue;
      el.append(kid.nodeType ? kid : document.createTextNode(String(kid)));
    }
    return el;
  }
  const money = (v) => new Intl.NumberFormat('en-IN', { style: 'currency', currency: S.cfg.currency || 'INR', maximumFractionDigits: 0 }).format(v);
  const pretty = (s) => String(s || '').replace(/_/g, ' ');
  const cap = (s) => s.charAt(0).toUpperCase() + s.slice(1);
  const img = (id, color) => `/img/${encodeURIComponent(id)}.svg?color=${encodeURIComponent(color || '')}`;

  async function api(path, body, method, retried) {
    const res = await fetch(path, {
      method: method || (body ? 'POST' : 'GET'),
      headers: body ? { 'Content-Type': 'application/json' } : undefined,
      body: body ? JSON.stringify(body) : undefined,
    });
    const data = await res.json().catch(() => ({}));
    // The server keeps sessions in memory, so a restart or a long idle tab loses ours.
    // Start a fresh session quietly and repeat the call once instead of showing "not found".
    if (res.status === 404 && !retried && S.session && path.includes(`/api/sessions/${S.session.id}`) && /not found|expired/i.test(data.error || '')) {
      const old = S.session.id;
      await recoverSession();
      return api(path.replace(old, S.session.id), body, method, true);
    }
    if (!res.ok) throw new Error(data.error || `Request failed (${res.status})`);
    return data;
  }

  async function recoverSession() {
    const sess = await api('/api/sessions', { ad_id: ad() ? ad().id : 'morning-run' });
    S.session = sess.session; S.mobileUrl = sess.mobile_url; S.qrUrl = sess.qr_url;
    S.cart = []; S.selectedCards = [];
  }
  function ev(name, props) {
    if (!S.session) return;
    fetch('/api/events', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ session_id: S.session.id, name, props: props || {} }) }).catch(() => {});
  }

  function fit() {
    const s = Math.min(window.innerWidth / 1920, window.innerHeight / 1080);
    stage.style.transform = `translate(-50%, -50%) scale(${s})`;
  }
  window.addEventListener('resize', fit);
  fit();

  // ----------------------------------------------------------- the "ad" ---
  const ad = () => S.ads[S.adIdx];
  const scene = () => ad().scenes[S.sceneIdx];

  function buildAdBar() {
    const bar = $('adbar');
    bar.replaceChildren(
      ...S.ads.map((a, i) => h('button', { type: 'button', 'aria-pressed': String(i === S.adIdx), onclick: () => startAd(i) }, a.title)),
      h('span', { class: 'note' }, 'Sample ads and a sample show, made for this demo'),
    );
  }

  // Clips are rendered as .webm (VP9) and .mp4 (H.264); use whichever this browser plays.
  const clipExt = (() => {
    const v = document.createElement('video');
    return v.canPlayType('video/webm; codecs="vp9"') ? 'webm' : 'mp4';
  })();
  const ENTRANCE = 1.7; // seconds until the products have settled in place

  function showScene(i) {
    S.sceneIdx = i;
    S.sceneStart = performance.now();
    const next = 1 - S.layer;
    const el = S.layers[next];
    el.poster = scene().svg_url;
    el.src = `${scene().video_url}.${clipExt}`;
    el.currentTime = 0;
    el.play().catch(() => {});
    el.classList.add('on');
    S.layers[S.layer].classList.remove('on');
    S.layers[S.layer].pause();
    S.layer = next;
  }

  function startAd(i) {
    if (S.panelOpen) closePanel();
    S.adIdx = i;
    buildAdBar();
    showScene(0);
    $('invite').classList.remove('show');
    clearTimeout(S.inviteTimer);
    S.inviteTimer = setTimeout(() => { if (!S.panelOpen) $('invite').classList.add('show'); }, 2000);
    ev('ad_view', { ad_id: ad().id });
  }

  const ICON = {
    play: '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M7 4.5v15l13-7.5z"/></svg>',
    pause: '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M6 4.5h4.5v15H6zM13.5 4.5H18v15h-4.5z"/></svg>',
  };
  const mmss = (ms) => { const t = Math.max(0, Math.round(ms / 1000)); return `${Math.floor(t / 60)}:${String(t % 60).padStart(2, '0')}`; };

  // The player chrome shows what a viewer expects: state icon, title and time.
  function updateOsd(t) {
    if (!S.ads.length) return;
    const secs = ad().seconds_per_scene * 1000, total = ad().scenes.length;
    const icon = S.paused ? ICON.play : ICON.pause; // the icon names the action a press would take
    if ($('osdicon').dataset.s !== String(S.paused)) { $('osdicon').innerHTML = icon; $('osdicon').dataset.s = String(S.paused); }
    $('osdtitle').textContent = ad().kind === 'show' ? `${ad().title} · ${ad().episode}` : ad().title;
    const cur = S.sceneIdx * secs + Math.min(t, secs);
    $('osdtime').textContent = `${mmss(cur)} / ${mmss(secs * total)}`;
  }

  function flashIcon(paused) {
    const f = $('flash');
    f.innerHTML = paused ? ICON.pause : ICON.play;
    f.classList.remove('pop'); void f.offsetWidth; f.classList.add('pop');
  }

  function tick() {
    if (!S.ads.length || S.paused) return;
    const secs = ad().seconds_per_scene * 1000;
    const v = S.layers[S.layer];
    const playing = v.readyState >= 2 && !v.error;
    const t = playing ? v.currentTime * 1000 : performance.now() - S.sceneStart;
    const total = ad().scenes.length;
    $('progress').firstElementChild.style.width = (((S.sceneIdx + Math.min(t / secs, 1)) / total) * 100).toFixed(1) + '%';
    updateOsd(t);
    if (t >= secs - 60 || v.ended) showScene((S.sceneIdx + 1) % total);
  }
  setInterval(tick, 100);

  function pause(on) {
    const changed = S.paused !== on;
    S.paused = on;
    $('screen').classList.toggle('paused', on);
    const v = S.layers[S.layer];
    updateOsd(v.currentTime * 1000 || 0);
    if (changed) flashIcon(on);
    if (on) {
      v.pause();
      // Pausing during the entrance would freeze products mid-slide, so settle on the finished frame.
      if (v.readyState >= 1 && v.currentTime < ENTRANCE) v.currentTime = ENTRANCE;
    } else {
      S.sceneStart = performance.now() - v.currentTime * 1000;
      v.play().catch(() => {});
    }
  }

  // After pausing we may seek to the settled frame; wait for it before capturing.
  function frameReady() {
    const v = S.layers[S.layer];
    if (!v.seeking) return Promise.resolve();
    return new Promise((done) => { v.addEventListener('seeked', done, { once: true }); setTimeout(done, 1500); });
  }

  // Capture the frame the viewer is looking at as a JPEG data URL.
  function captureFrame() {
    const el = S.layers[S.layer];
    if (el.readyState < 2 || !el.videoWidth) return null;
    const c = document.createElement('canvas');
    c.width = 1920; c.height = 1080; // full HD keeps small brand text readable for the model
    c.getContext('2d').drawImage(el, 0, 0, 1920, 1080);
    try { return c.toDataURL('image/jpeg', 0.9); } catch (e) { return null; }
  }

  // ----------------------------------------------------------- the panel ---
  function openPanel() {
    if (S.panelOpen) return;
    S.panelOpen = true;
    stage.classList.add('split');
    pause(true);
    $('invite').classList.remove('show');
    $('panel').classList.add('open');
    ev('panel_opened', { scene_id: scene().id });
    analyze();
  }

  function closePanel() {
    S.panelOpen = false;
    stage.classList.remove('split');
    S.req++;
    $('panel').classList.remove('open');
    $('boxes').replaceChildren();
    S.analysis = null; S.view = 'list'; S.detail = null; S.results = null; S.history = [];
    $('reply').textContent = ''; $('replysrc').textContent = ''; $('caption').textContent = '';
    pause(false);
    clearTimeout(S.inviteTimer);
    S.inviteTimer = setTimeout(() => { if (!S.panelOpen) $('invite').classList.add('show'); }, 1500);
    $('invite').focus({ preventScroll: true });
  }

  async function analyze() {
    const req = ++S.req;
    $('ptitle').textContent = 'In this scene';
    $('source').textContent = '';
    $('source').className = 'source';
    $('suggest').replaceChildren();
    $('boxes').replaceChildren();
    $('body').replaceChildren(
      h('div', { class: 'status' }, S.cfg.live_ai ? 'Looking at this frame with Gemini' : 'Looking at this frame'),
      h('div', { class: 'skeleton', style: 'height:200px' }),
      h('div', { class: 'cards many' }, [1, 2, 3].map(() => h('div', { class: 'skeleton', style: 'height:250px' }))),
    );
    try {
      await frameReady();
      const frame = S.cfg.live_ai ? captureFrame() : null;
      const res = await api('/api/analyze', { session_id: S.session.id, scene_id: scene().id, frame });
      if (req !== S.req) return;
      S.analysis = res; S.detIdx = 0; S.view = 'list';
      const live = res.source === 'gemini';
      $('source').className = 'source' + (live ? ' live' : '');
      $('source').textContent = live
        ? `Analysed live with Gemini in ${(res.latency_ms / 1000).toFixed(1)} s`
        : (res.fallback_reason ? 'Live analysis unavailable, showing a saved result' : 'Showing a saved analysis of this scene');
      render();
      focusFirst();
    } catch (e) {
      if (req !== S.req) return;
      $('body').replaceChildren(
        h('div', { class: 'status error' }, `We couldn't analyse this scene. ${e.message}`),
        h('div', { class: 'actions' }, h('button', { class: 'btn primary', type: 'button', onclick: analyze }, 'Try again'), h('button', { class: 'btn', type: 'button', onclick: closePanel }, 'Back to the ad')),
      );
      focusFirst();
    }
  }

  const levelOf = (d) => d.match_level; // exact | similar | none
  const det = () => S.analysis && S.analysis.detections[S.detIdx];

  function drawCaption() {
    const d = det();
    const el = $('caption');
    if (!d) { el.textContent = ''; return; }
    const c = d.candidate;
    const who = S.analysis.source === 'gemini' ? 'Gemini saw' : 'Saved analysis';
    el.replaceChildren(h('strong', {}, `Item ${S.detIdx + 1}. `), `${who}: ${c.evidence || 'no description'} Confidence ${Math.round((c.confidence || 0) * 100)}%.`);
  }

  function drawBoxes() {
    const wrap = $('boxes');
    wrap.replaceChildren();
    if (!S.analysis) { $('caption').textContent = ''; return; }
    drawCaption();
    S.analysis.detections.forEach((d, i) => {
      const b = d.candidate.box_2d;
      if (!b || b.length !== 4) return;
      const [y0, x0, y1, x1] = b;
      const el = h('div', { class: `box ${levelOf(d)}${i === S.detIdx ? ' active' : ''}` }, h('b', {}, String(i + 1)));
      el.style.left = (x0 / 1000) * 1920 + 'px';
      el.style.top = (y0 / 1000) * 1080 + 'px';
      el.style.width = ((x1 - x0) / 1000) * 1920 + 'px';
      el.style.height = ((y1 - y0) / 1000) * 1080 + 'px';
      wrap.append(el);
    });
  }

  function card(match, opts) {
    const p = match.product;
    const color = match.color || p.variants[0].color;
    const reasons = (match.reasons || []).slice(0, 2).join(', ');
    return h('button', {
      type: 'button', class: `card focusable${opts.similar ? ' similar' : ''}${opts.hero ? ' hero' : ''}`,
      'aria-label': `${p.name}, ${money(p.price)}, ${opts.similar ? 'alternative, not the exact product' : 'exact match'}`,
      onclick: () => openDetail(p, color, opts.similar ? 'similar' : 'exact', opts.alt),
    },
      h('img', { src: img(p.id, color), alt: '', width: 200, height: 200 }),
      h('div', { class: 'meta' },
        h('div', { class: 'name' }, p.name),
        h('div', { class: 'price' }, money(p.price)),
        opts.hero && reasons ? h('div', { class: 'why' }, cap(reasons)) : null,
      ),
    );
  }

  function render() {
    drawBoxes();
    const body = $('body');
    const a = S.analysis;
    if (S.view === 'detail' && S.detail) return renderDetail();
    if (S.view === 'results' && S.results) return renderResults();
    if (!a) return;
    $('ptitle').textContent = 'In this scene';
    const d = det();
    body.replaceChildren();

    body.append(h('div', { class: 'chips', role: 'group', 'aria-label': 'Items found in this scene' },
      a.detections.map((x, i) => h('button', {
        type: 'button', class: `chip focusable ${levelOf(x)}`, 'aria-pressed': String(i === S.detIdx),
        onclick: () => { S.detIdx = i; S.view = 'list'; render(); focusSel(`.chip:nth-child(${i + 1})`); },
      }, h('span', { class: 'n' }, String(i + 1)), cap(x.candidate.label || pretty(x.candidate.category))))));

    if (!a.detections.length) {
      body.append(h('div', { class: 'status' }, 'No products found in this scene. Try another moment of the ad.'));
    } else {
      body.append(h('div', { class: 'verdict' }, h('div', {}, h('strong', {}, d.match_level === 'exact' ? 'Exact match. ' : d.match_level === 'similar' ? 'Not an exact match. ' : 'Not in the catalog. '), d.note)));
      if (d.story) body.append(storyCard(d.story));
      if (d.exact) {
        body.append(h('div', {}, h('h3', { class: 'group-title' }, h('span', { class: 'tag' }, 'Exact match')), h('div', { class: 'cards one' }, card(d.exact, { hero: true }))));
      }
      const look = lookFor(d);
      if (look) body.append(h('button', { type: 'button', class: 'btn look focusable', onclick: () => shopLook(look) },
        `Shop ${look.character.split(' ')[0]}'s look`, h('span', { class: 'sub' }, `${look.items.length} items · ${money(look.total)}`)));
      if (d.similar && d.similar.length) {
        body.append(h('div', {}, h('h3', { class: 'group-title' }, h('span', { class: 'tag similar' }, 'Similar'), d.exact ? 'Other options' : 'Closest in the catalog'),
          h('div', { class: 'cards many' }, d.similar.map((m) => card(m, { similar: true, alt: true })))));
      }
    }
    setSuggestions();
  }

  // ---- show tie-in: story card and "shop the look" ----
  function storyCard(st) {
    const kind = st.kind === 'prop' ? 'Prop from the show' : `Worn by ${st.character}`;
    return h('div', { class: `story ${st.kind}` },
      h('div', { class: 'kind' }, kind),
      h('p', {}, st.moment),
      h('div', { class: 'from' }, `${st.show} · ${st.episode}`, h('span', {}, ' · from the show\'s scene notes')));
  }

  // Everything in this scene tied to one character that we can match exactly.
  function lookFor(d) {
    if (!d.story || !S.analysis) return null;
    const items = S.analysis.detections.filter((x) => x.story && x.story.character === d.story.character && x.exact);
    if (items.length < 2) return null;
    const total = items.reduce((t, x) => t + x.exact.product.price, 0);
    return { character: d.story.character, items, total };
  }

  async function shopLook(look) {
    try {
      for (const x of look.items) {
        await api(`/api/sessions/${S.session.id}/cart`, { product_id: x.exact.product.id, color: x.exact.color || x.exact.product.variants[0].color, delta: 1, channel: 'tv' });
      }
      await refreshSession();
      openPay();
    } catch (e) {
      $('reply').textContent = `Couldn't add that look. ${e.message}`;
    }
  }

  function setSuggestions() {
    const onDetail = S.view === 'detail';
    const list = onDetail
      ? ['Show me this in black', 'Find a cheaper alternative']
      : ['Show me a cheaper one', 'Any in black?'];
    $('suggest').replaceChildren(...list.map((t) => h('button', { type: 'button', class: 'focusable', onclick: () => ask(t) }, t)));
  }

  // ---------------------------------------------------------- detail view ---
  function openDetail(product, color, level, fromAlt) {
    S.detail = {
      product, level, from: S.view === 'results' ? 'results' : 'list',
      color: product.variants.some((v) => v.color === color && v.in_stock) ? color : (product.variants.find((v) => v.in_stock) || product.variants[0]).color,
    };
    S.view = 'detail';
    ev('product_opened', { product_id: product.id });
    if (fromAlt) ev('alternative_selected', { product_id: product.id });
    renderDetail();
    focusSel('.btn.primary');
  }

  // True only when the placed order contains this exact product and colour.
  function isOrdered(productId, color) {
    const o = S.session && S.session.order;
    return !!(o && (o.items || []).some((i) => i.product_id === productId && i.color === color));
  }

  function renderDetail() {
    const { product: p, color, level } = S.detail;
    $('ptitle').textContent = level === 'exact' ? 'Exact match' : level === 'similar' ? 'Similar item' : 'Product';
    const sw = p.variants.map((v) => h('button', {
      type: 'button', class: 'swatch focusable', style: `background:${v.hex}`, 'aria-label': v.color + (v.in_stock ? '' : ' (out of stock)'),
      'aria-pressed': String(v.color === color), disabled: !v.in_stock,
      onclick: () => { S.detail.color = v.color; renderDetail(); focusSel(`.swatch[aria-label^="${v.color}"]`); },
    }));
    const ordered = isOrdered(p.id, color);
    const queued = S.session && S.session.selected && S.session.selected.some((s) => s.product_id === p.id && s.color === color);
    $('body').replaceChildren(h('div', { class: `detail ${level === 'similar' ? 'similar' : ''}` },
      h('img', { src: img(p.id, color), alt: `${p.name} in ${color}`, width: 230, height: 230 }),
      h('div', {},
        h('h2', {}, p.name),
        h('div', { class: 'price' }, money(p.price)),
        p.licensed ? h('div', { class: 'licensed' }, `Official ${p.licensed} merchandise`) : null,
        h('div', { class: 'colorname' }, color),
        h('div', { class: 'swatches', role: 'group', 'aria-label': 'Colours' }, sw),
      ),
      h('div', { class: 'more' },
        h('p', {}, p.description),
        h('ul', { class: 'features' }, p.attributes.slice(0, 4).map((f) => h('li', {}, f))),
        h('div', { class: 'actions' },
          h('button', { class: 'btn primary focusable', type: 'button', onclick: buyNow }, ordered ? 'Order placed' : 'Buy now'),
          h('button', { class: 'btn focusable', type: 'button', onclick: sendToPhone }, queued ? 'On your phone' : 'Send to phone'),
          h('button', { class: 'btn focusable', type: 'button', onclick: back }, 'Back'),
        ),
      )));
    setSuggestions();
  }

  async function sendToPhone() {
    const { product, color } = S.detail;
    try {
      const v = await api(`/api/sessions/${S.session.id}/select`, { product_id: product.id, color });
      S.session = v.session; S.session.selected = v.selected.map((c) => ({ product_id: c.product.id, color: c.color }));
      S.selectedCards = v.selected;
      updatePhoneBtn();
      openHandoff();
    } catch (e) {
      $('reply').textContent = `Couldn't send that to your phone. ${e.message}`;
    }
  }

  // ------------------------------------------------- buy on the TV (demo) ---
  // Buy now adds the item to the UCP cart, then shows a QR for a demo UPI payment.
  // The QR opens an approval page on the phone; approving completes the UCP checkout.
  // The TV also offers a simulated card so the flow works with no phone at all.
  async function buyNow() {
    const { product, color } = S.detail;
    if (isOrdered(product.id, color)) { openPay(); return; }
    try {
      await api(`/api/sessions/${S.session.id}/cart`, { product_id: product.id, color, delta: 1, channel: 'tv' });
      await refreshSession();
      openPay();
    } catch (e) {
      $('reply').textContent = `Couldn't start checkout. ${e.message}`;
    }
  }

  function stopPayPoll() { clearInterval(S.payTimer); S.payTimer = null; }

  async function openPay() {
    S.payOpen = true;
    S.upi = null;
    $('pay').classList.add('open');
    drawPay();
    if (!S.session.order && (S.cart || []).length) await startIntent();
  }

  function closePay() {
    stopPayPoll();
    S.payOpen = false;
    S.upi = null;
    $('pay').classList.remove('open');
    if (S.panelOpen) { render(); focusFirst(); }
  }

  async function startIntent(focusAfter) {
    stopPayPoll();
    try {
      S.upi = await api(`/api/sessions/${S.session.id}/pay-intent`, {});
    } catch (e) {
      S.upi = { status: 'error', error: e.message };
    }
    drawPay(null, focusAfter);
    if (S.upi.intent_id) S.payTimer = setInterval(pollIntent, 1500);
  }

  async function pollIntent() {
    if (!S.payOpen || !S.upi || !S.upi.intent_id || S.payBusy) return;
    try {
      const r = await api(`/api/pay/${S.upi.intent_id}`);
      if (r.status === S.upi.status) return;
      S.upi.status = r.status;
      if (r.status === 'paid') { stopPayPoll(); await refreshSession(); }
      drawPay();
    } catch (e) { /* intent gone: the next draw offers a new QR */ }
  }

  async function changeQty(line, delta) {
    if (S.payBusy) return;
    S.payBusy = true;
    stopPayPoll();
    try {
      await api(`/api/sessions/${S.session.id}/cart`, { product_id: line.product.id, color: line.color, delta, channel: 'tv' });
      await refreshSession();
      S.payBusy = false;
      if ((S.cart || []).length) await startIntent(`.pay-line[data-k="${line.product.id}-${line.color}"] .${delta > 0 ? 'plus' : 'minus'}`);
      else { S.upi = null; drawPay(); }
    } catch (e) { S.payBusy = false; drawPay(e.message); }
  }

  async function payWithCard() {
    if (S.payBusy) return;
    S.payBusy = true;
    stopPayPoll();
    try {
      await api(`/api/sessions/${S.session.id}/checkout`, { channel: 'tv' });
      await refreshSession();
      drawPay();
    } catch (e) { drawPay(e.message); }
    S.payBusy = false;
  }

  function drawPay(error, focusSel2) {
    const lines = S.cart || [];
    const order = S.session.order;
    const total = lines.reduce((n, l) => n + l.product.price * l.qty, 0);
    const card = h('div', { class: 'pay-card' });
    if (order) {
      card.append(h('h2', { id: 'pay-title' }, 'Demo order placed'),
        h('div', { class: 'pay-done' }, h('strong', {}, order.id),
          `${order.item_count} item${order.item_count === 1 ? '' : 's'}, ${money(order.total)}. This was a simulation: no payment was taken and nothing will ship.`),
        h('div', { class: 'actions', style: 'margin-top:28px' }, h('button', { class: 'btn primary focusable', type: 'button', onclick: closePay }, 'Back to the ad')));
    } else {
      const u = S.upi;
      card.append(h('h2', { id: 'pay-title' }, 'Pay with UPI'), h('p', { class: 'demo' }, 'Demo checkout. Nothing is charged.'));
      if (error) card.append(h('p', { class: 'pay-error', role: 'alert' }, error));
      const qrBox = h('div', { class: 'pay-qrbox' });
      const live = u && u.qr_url && u.status === 'pending';
      if (live) {
        qrBox.append(h('div', { class: 'pay-qr' }, h('img', { src: u.qr_url, alt: 'QR code to approve the demo payment on your phone', width: 340, height: 340 })),
          h('p', { class: 'pay-qrnote' }, 'Scan with your phone camera'),
          h('p', { class: 'pay-state' }, h('span', { class: 'dot' }), 'Waiting for your phone'));
      } else {
        const msg = !u ? 'Getting your QR code' : u.status === 'declined' ? 'Declined on the phone' : u.status === 'expired' ? 'This QR code expired' : u.status === 'error' ? (u.error || 'Could not create a QR code') : 'This QR code is out of date';
        qrBox.append(h('div', { class: 'pay-qr off' }, msg), u ? h('button', { class: 'btn primary focusable', type: 'button', onclick: () => startIntent() }, 'New QR code') : null);
      }
      const right = h('div', { class: 'pay-right' });
      if (!lines.length) right.append(h('p', { class: 'demo' }, 'Your cart is empty.'));
      for (const l of lines) {
        right.append(h('div', { class: 'pay-line', 'data-k': `${l.product.id}-${l.color}` },
          h('img', { src: img(l.product.id, l.color), alt: '', width: 80, height: 80 }),
          h('div', {}, h('div', { class: 'n' }, l.product.name), h('div', { class: 'c' }, h('span', { style: 'text-transform:capitalize' }, l.color), `, ${money(l.product.price)} each`)),
          h('div', { class: 'pay-qty' },
            h('button', { type: 'button', class: 'minus focusable', 'aria-label': `Remove one ${l.product.name}`, onclick: () => changeQty(l, -1) }, '-'),
            h('output', {}, String(l.qty)),
            h('button', { type: 'button', class: 'plus focusable', 'aria-label': `Add one ${l.product.name}`, onclick: () => changeQty(l, 1) }, '+')),
          h('div', { class: 'amt' }, money(l.product.price * l.qty))));
      }
      right.append(h('div', { class: 'pay-total' }, h('span', {}, 'Total'), h('span', {}, money(total))));
      card.append(h('div', { class: 'pay-grid' }, qrBox, right),
        h('div', { class: 'actions' },
          h('button', { class: 'btn focusable', type: 'button', disabled: !lines.length, onclick: payWithCard }, 'Pay with demo card instead'),
          h('button', { class: 'btn focusable', type: 'button', onclick: closePay }, 'Keep browsing')),
        h('p', { class: 'pay-foot' }, 'Demo only: the QR opens an approval page in this app, not a UPI app, and no bank is involved. Cart and checkout use the Universal Commerce Protocol (UCP) against a demo merchant.'));
    }
    $('pay').replaceChildren(card);
    requestAnimationFrame(() => {
      const el = (focusSel2 && $('pay').querySelector(focusSel2)) || $('pay').querySelector('.btn.primary:not([disabled])') || $('pay').querySelector('.plus') || $('pay').querySelector('button');
      if (el) el.focus({ preventScroll: true });
    });
  }

  // ----------------------------------------------------------- chat / ask ---
  async function ask(text) {
    text = (text || '').trim();
    if (!text) return;
    const req = ++S.req;
    $('askinput').value = '';
    $('reply').className = 'pending';
    $('reply').textContent = 'Checking the catalog';
    $('replysrc').textContent = '';
    const d = det();
    const ctx = S.detail
      ? { product_id: S.detail.product.id, color: S.detail.color, category: S.detail.product.category }
      : { product_id: d && d.exact ? d.exact.product.id : '', color: d && d.exact ? d.exact.color : '', category: d ? d.candidate.category : '' };
    try {
      const out = await api('/api/chat', { session_id: S.session.id, message: text, history: S.history, ...ctx });
      if (req !== S.req) return;
      const r = out.result;
      S.history.push({ role: 'user', text }, { role: 'assistant', text: r.reply });
      $('reply').className = '';
      $('reply').textContent = r.reply;
      $('replysrc').textContent = r.source === 'gemini' ? 'Answered by Gemini using catalog tools' : (r.fallback_reason ? 'Gemini was unavailable, so the built-in rules answered' : 'Answered by built-in rules');
      if (r.shortlist_changed) await refreshSession();
      const first = r.cards && r.cards[0];
      if (r.focus && first && first.product.id === r.focus.product_id) {
        // "this one in black": stay on (or open) the same product with the new colour
        if (S.detail && S.detail.product.id === r.focus.product_id) {
          S.detail.color = r.focus.color; S.view = 'detail';
        } else {
          const d = det();
          const exact = d && d.exact && d.exact.product.id === r.focus.product_id;
          openDetail(first.product, r.focus.color, exact ? 'exact' : 'similar', false);
          return;
        }
      } else if (r.cards && r.cards.length) {
        S.results = r; S.view = 'results';
      }
      render();
    } catch (e) {
      if (req !== S.req) return;
      $('reply').className = '';
      $('reply').textContent = `That didn't work. ${e.message}`;
    }
  }

  function renderResults() {
    $('ptitle').textContent = 'Results';
    const cards = S.results.cards;
    $('body').replaceChildren(h('div', {},
      h('h3', { class: 'group-title' }, h('span', { class: 'tag similar' }, 'Catalog results'), `${cards.length} found`),
      h('div', { class: `cards ${cards.length === 4 ? 'two' : 'many'}` },
        cards.map((c) => card({ product: c.product, color: c.color, reasons: [] }, { similar: true, alt: true }))),
      h('div', { class: 'actions', style: 'margin-top:22px' }, h('button', { class: 'btn focusable', type: 'button', onclick: back }, 'Back to this scene'))));
    setSuggestions();
  }

  $('askform').addEventListener('submit', (e) => { e.preventDefault(); ask($('askinput').value); });

  // -------------------------------------------------------------- handoff ---
  async function refreshSession() {
    const v = await api(`/api/sessions/${S.session.id}`);
    S.session = Object.assign({}, v.session, { selected: v.selected.map((c) => ({ product_id: c.product.id, color: c.color })) });
    S.selectedCards = v.selected;
    S.cart = v.cart;
    updatePhoneBtn();
    return v;
  }

  function updatePhoneBtn() {
    const n = (S.selectedCards || []).length;
    $('phonebtn').dataset.n = String(n);
    $('phonebtn').textContent = n ? `Continue on phone (${n})` : 'Continue on phone';
  }

  function openHandoff() {
    S.handoffOpen = true;
    $('qrimg').src = S.qrUrl;
    $('mobileurl').textContent = S.mobileUrl;
    $('openphone').href = S.mobileUrl;
    $('handoff').classList.add('open');
    drawHandoffState();
    clearInterval(S.pollTimer);
    S.pollTimer = setInterval(async () => { try { await refreshSession(); drawHandoffState(); } catch (e) { /* session may have expired */ } }, 2000);
    $('handdone').focus({ preventScroll: true });
  }

  function closeHandoff() {
    S.handoffOpen = false;
    clearInterval(S.pollTimer);
    $('handoff').classList.remove('open');
    if (S.panelOpen) { render(); focusFirst(); }
  }

  function drawHandoffState() {
    const s = S.session;
    const box = $('phonestate');
    const order = s.order;
    const cartQty = (S.cart || []).reduce((n, l) => n + l.qty, 0);
    box.classList.toggle('on', !!s.phone_opened);
    $('phonetext').textContent = order ? `Demo order ${order.id} placed on your phone` : s.phone_opened ? (cartQty ? `Phone connected, ${cartQty} in cart` : 'Phone connected') : 'Waiting for your phone';
    $('queue').replaceChildren(...(S.selectedCards || []).map((c) => h('img', { src: img(c.product.id, c.color), alt: `${c.product.name}, ${c.color}`, width: 84, height: 84 })));
  }

  // ------------------------------------------------------------ navigation ---
  function back() {
    if (S.payOpen) return closePay();
    if (S.handoffOpen) return closeHandoff();
    if (!S.panelOpen) return;
    if (S.view === 'detail') { S.view = S.detail.from === 'results' && S.results ? 'results' : 'list'; S.detail = null; render(); return focusFirst(); }
    if (S.view === 'results') { S.view = 'list'; S.results = null; render(); return focusFirst(); }
    closePanel();
  }

  function visible(el) {
    const r = el.getBoundingClientRect();
    return r.width > 0 && r.height > 0 && !el.disabled && getComputedStyle(el).visibility !== 'hidden';
  }
  function scope() {
    if (S.payOpen) return [...$('pay').querySelectorAll('button')];
    if (S.handoffOpen) return [...$('handoff').querySelectorAll('button, a')];
    if (S.panelOpen) return [...$('panel').querySelectorAll('button, input')];
    return [...document.querySelectorAll('#adbar button, #invite')];
  }
  function focusSel(sel) { const el = $('panel').querySelector(sel); if (el) el.focus({ preventScroll: true }); else focusFirst(); }
  function focusFirst() {
    requestAnimationFrame(() => {
      const el = (S.handoffOpen ? $('handoff') : S.panelOpen ? $('body') : document).querySelector('.focusable, .btn, #invite');
      if (el && visible(el)) el.focus({ preventScroll: true });
    });
  }

  // Directional focus: pick the nearest visible control in the pressed direction.
  function move(dir) {
    const items = scope().filter(visible);
    const cur = document.activeElement;
    if (!items.includes(cur)) { if (items[0]) items[0].focus({ preventScroll: true }); return; }
    const c = cur.getBoundingClientRect();
    const cx = c.left + c.width / 2, cy = c.top + c.height / 2;
    let best = null, bestScore = Infinity;
    for (const el of items) {
      if (el === cur) continue;
      const r = el.getBoundingClientRect();
      const dx = r.left + r.width / 2 - cx, dy = r.top + r.height / 2 - cy;
      const along = dir === 'ArrowRight' ? dx : dir === 'ArrowLeft' ? -dx : dir === 'ArrowDown' ? dy : -dy;
      if (along <= 4) continue;
      const across = dir === 'ArrowRight' || dir === 'ArrowLeft' ? Math.abs(dy) : Math.abs(dx);
      const score = along + across * 2.2;
      if (score < bestScore) { bestScore = score; best = el; }
    }
    if (best) { best.focus({ preventScroll: true }); best.scrollIntoView({ block: 'nearest' }); }
  }

  document.addEventListener('keydown', (e) => {
    const typing = document.activeElement && document.activeElement.tagName === 'INPUT';
    if (e.key === 'Escape' || (e.key === 'Backspace' && !typing) || e.key === 'BrowserBack') {
      if (typing && e.key === 'Escape') { document.activeElement.blur(); focusFirst(); e.preventDefault(); return; }
      if (S.payOpen || S.handoffOpen || S.panelOpen) { e.preventDefault(); back(); }
      return;
    }
    if (typing) { if (e.key === 'ArrowDown' || e.key === 'ArrowUp') { e.preventDefault(); move(e.key); } return; }
    if (e.key.startsWith('Arrow')) {
      e.preventDefault();
      if (!S.panelOpen && !S.handoffOpen && !S.payOpen && !document.activeElement.closest('#adbar') && document.activeElement.id !== 'invite') {
        // nothing focused yet on the ad screen: left/right switch ads
        startAd((S.adIdx + (e.key === 'ArrowRight' ? 1 : e.key === 'ArrowLeft' ? S.ads.length - 1 : 0)) % S.ads.length);
        return;
      }
      move(e.key);
      return;
    }
    if (e.key === 'Enter' && !S.panelOpen && !S.handoffOpen && !S.payOpen) {
      const a = document.activeElement;
      if (a && a.closest('#adbar')) return; // let the focused button handle it
      e.preventDefault(); openPanel();
    }
  });

  $('invite').addEventListener('click', openPanel);
  $('osd').addEventListener('click', () => (S.panelOpen ? closePanel() : openPanel()));
  $('screen').addEventListener('click', () => { if (!S.panelOpen) openPanel(); });
  $('phonebtn').addEventListener('click', openHandoff);
  $('handdone').addEventListener('click', closeHandoff);

  // ---------------------------------------------------------------- boot ---
  async function boot() {
    try {
      const [cfg, adsRes] = await Promise.all([api('/api/config'), api('/api/ads')]);
      S.cfg = cfg; S.ads = adsRes.ads;
      const sess = await api('/api/sessions', { ad_id: S.ads[0].id });
      S.session = sess.session; S.mobileUrl = sess.mobile_url; S.qrUrl = sess.qr_url;
      for (let i = 0; i < 2; i++) {
        const l = h('video', { class: 'layer', muted: '', playsinline: '', preload: 'auto', 'aria-hidden': 'true' });
        l.muted = true;
        $('screen').append(l); S.layers.push(l);
      }
      startAd(0);
    } catch (e) {
      $('screen').replaceChildren(h('div', { class: 'status error', style: 'padding:80px;font-size:34px' }, `The demo could not start. ${e.message}`));
    }
  }
  boot();
})();
