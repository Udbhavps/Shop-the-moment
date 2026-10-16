/* Shop the Moment - demo payment approval on the phone. Not UPI. */
(() => {
  'use strict';
  const app = document.getElementById('app');
  const id = location.pathname.split('/').filter(Boolean)[1] || '';
  let info = null, busy = false, error = '';

  function h(tag, attrs, ...kids) {
    const el = document.createElement(tag);
    for (const [k, v] of Object.entries(attrs || {})) {
      if (v == null || v === false) continue;
      if (k === 'class') el.className = v;
      else if (k.startsWith('on')) el.addEventListener(k.slice(2), v);
      else if (v === true) el.setAttribute(k, '');
      else el.setAttribute(k, v);
    }
    for (const kid of kids.flat()) if (kid != null && kid !== false) el.append(kid.nodeType ? kid : document.createTextNode(String(kid)));
    return el;
  }
  const money = (v, cur) => new Intl.NumberFormat('en-IN', { style: 'currency', currency: cur || 'INR', maximumFractionDigits: v % 1 ? 2 : 0 }).format(v);

  async function call(path, method) {
    const res = await fetch(path, { method: method || 'GET', headers: method === 'POST' ? { 'Content-Type': 'application/json' } : undefined, body: method === 'POST' ? '{}' : undefined });
    const data = await res.json().catch(() => ({}));
    if (res.status === 404) { info = null; throw new Error('missing'); }
    return { ok: res.ok, status: res.status, data };
  }

  async function decide(kind) {
    if (busy) return;
    busy = true; error = ''; render();
    try {
      const r = await call(`/api/pay/${id}/${kind}`, 'POST');
      if (!r.ok) error = r.data.error || 'That did not work.';
      await load();
    } catch (e) { error = 'Could not reach the demo server.'; }
    busy = false; render();
  }

  async function load() {
    try { const r = await call(`/api/pay/${id}`); info = r.data; } catch (e) { /* handled in render */ }
  }

  function render() {
    app.replaceChildren();
    document.querySelector('.stick')?.remove();
    if (!info) {
      app.append(h('div', { class: 'notice error' }, 'This payment link has expired or is wrong. Press Buy now on the TV again to get a new QR code.'));
      return;
    }
    if (error) app.append(h('div', { class: 'notice error', role: 'alert' }, error));
    const done = {
      paid: ['Payment approved (demo)', `Order ${info.order_id || ''} is placed. Look at your TV. No money moved.`],
      declined: ['Payment declined', 'Nothing was ordered. You can start again from the TV.'],
      expired: ['This QR code expired', 'Press Buy now on the TV to get a new one.'],
      superseded: ['This QR code is out of date', 'The cart changed on the TV. Scan the newest QR code.'],
    }[info.status];
    if (done) {
      app.append(h('div', { class: info.status === 'paid' ? 'done' : 'notice' }, h('h2', {}, done[0]), h('div', {}, done[1])));
      return;
    }
    app.append(h('section', { class: 'payto' },
      h('div', { class: 'merchant' }, 'Paying ', h('strong', {}, info.merchant), h('span', { class: 'pill' }, 'Demo')),
      h('div', { class: 'amount' }, money(info.amount, info.currency)),
      h('ul', { class: 'pay-items' }, (info.lines || []).map((l) => h('li', {}, h('span', {}, `${l.qty} x ${l.name}`), h('span', { class: 'c' }, l.color))))));
    app.append(h('div', { class: 'notice' }, 'Approving only completes a simulated order. No bank, UPI app or card is used.'));
    document.body.append(h('div', { class: 'stick' }, h('div', { class: 'row2' },
      h('button', { class: 'btn ghost', type: 'button', disabled: busy, onclick: () => decide('decline') }, 'Decline'),
      h('button', { class: 'btn primary', type: 'button', disabled: busy, onclick: () => decide('approve') }, `Approve ${money(info.amount, info.currency)} (demo)`))));
  }

  (async () => { await load(); render(); setInterval(async () => { if (!busy && info && info.status === 'pending') { await load(); render(); } }, 3000); })();
})();
