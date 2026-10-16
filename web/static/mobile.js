/* Shop the Moment - phone view. Loads the session the TV created. */
(() => {
  'use strict';
  const app = document.getElementById('app');
  const sid = location.pathname.split('/').filter(Boolean)[1] || '';
  let state = null, busy = false, expired = false, error = '';

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
  const img = (id, color) => `/img/${encodeURIComponent(id)}.svg?color=${encodeURIComponent(color || '')}`;

  async function api(path, body) {
    const res = await fetch(path, { method: body !== undefined ? 'POST' : 'GET', headers: body ? { 'Content-Type': 'application/json' } : undefined, body: body ? JSON.stringify(body) : undefined });
    const data = await res.json().catch(() => ({}));
    if (res.status === 404) { expired = true; throw new Error('expired'); }
    if (!res.ok) throw new Error(data.error || `Something went wrong (${res.status})`);
    return data;
  }

  async function act(fn) {
    if (busy) return;
    busy = true; error = '';
    try { state = await fn(); } catch (e) { if (!expired) error = e.message; }
    busy = false; render();
  }

  const inCart = (id, color) => ((state.cart || []).find((l) => l.product.id === id && l.color === color) || { qty: 0 }).qty;

  function pick(c) {
    const p = c.product, qty = inCart(p.id, c.color);
    const ordered = !!state.session.order;
    return h('article', { class: 'item' },
      h('img', { src: img(p.id, c.color), alt: `${p.name} in ${c.color}`, width: 96, height: 96 }),
      h('div', {},
        h('h3', {}, p.name),
        h('div', { class: 'price' }, money(p.price, state.currency)),
        h('div', { class: 'sw', role: 'group', 'aria-label': `Colours for ${p.name}` },
          p.variants.map((v) => h('button', {
            type: 'button', style: `background:${v.hex}`, 'aria-label': v.color + (v.in_stock ? '' : ' (out of stock)'), 'aria-pressed': String(v.color === c.color),
            disabled: !v.in_stock || ordered,
            onclick: () => act(() => api(`/api/sessions/${sid}/select`, { product_id: p.id, color: v.color })),
          }))),
        h('div', { class: 'row' },
          h('button', { class: 'btn primary', type: 'button', disabled: ordered, onclick: () => act(() => api(`/api/sessions/${sid}/cart`, { product_id: p.id, color: c.color, delta: 1, channel: 'phone' })) }, qty ? `Add another (${qty} in cart)` : 'Add to cart'),
          ordered ? null : h('button', { class: 'btn ghost', type: 'button', onclick: () => act(() => api(`/api/sessions/${sid}/unselect`, { product_id: p.id })) }, 'Remove'),
        )));
  }

  function cartView() {
    const lines = state.cart || [];
    if (!lines.length) return null;
    const ordered = !!state.session.order;
    return h('section', { 'aria-labelledby': 'cart-h' },
      h('h2', { id: 'cart-h' }, 'Cart'),
      h('div', { class: 'cart' },
        lines.map((l) => h('div', { class: 'line' },
          h('img', { src: img(l.product.id, l.color), alt: '', width: 56, height: 56 }),
          h('div', {}, h('div', { class: 'n' }, l.product.name), h('div', { class: 'c' }, `${l.color}, ${money(l.product.price, state.currency)}`)),
          ordered ? h('div', {}, `x${l.qty}`) : h('div', { class: 'qty' },
            h('button', { type: 'button', 'aria-label': `Remove one ${l.product.name}`, onclick: () => act(() => api(`/api/sessions/${sid}/cart`, { product_id: l.product.id, color: l.color, delta: -1, channel: 'phone' })) }, '-'),
            h('output', {}, String(l.qty)),
            h('button', { type: 'button', 'aria-label': `Add one ${l.product.name}`, onclick: () => act(() => api(`/api/sessions/${sid}/cart`, { product_id: l.product.id, color: l.color, delta: 1, channel: 'phone' })) }, '+')),
        )),
        h('div', { class: 'total' }, h('span', {}, 'Total'), h('span', {}, money(state.cart_total, state.currency)))));
  }

  function render() {
    app.replaceChildren();
    if (expired) {
      app.append(h('div', { class: 'notice error' }, 'This session has expired or the link is wrong. Scan the QR code on the TV again to get your picks back.'));
      return;
    }
    if (!state) return;
    if (error) app.append(h('div', { class: 'notice error', role: 'alert' }, error));
    const order = state.session.order;
    if (order) {
      app.append(h('div', { class: 'done' }, h('h2', {}, 'Demo order placed'),
        h('div', {}, `Order ${order.id}: ${order.item_count} item${order.item_count === 1 ? '' : 's'}, ${money(order.total, order.currency)}.`),
        h('div', {}, 'This was a simulation. No payment was taken and nothing will ship.')));
    }
    const picks = state.selected || [];
    app.append(h('section', { 'aria-labelledby': 'picks-h' }, h('h2', { id: 'picks-h' }, 'Sent from the TV'),
      picks.length ? h('div', { class: 'col', style: 'display:flex;flex-direction:column;gap:12px' }, picks.map(pick))
        : h('div', { class: 'empty' }, 'Nothing here yet. On the TV, open a product and choose Send to phone. It will appear on this page within a few seconds.')));
    const cv = cartView();
    if (cv) app.append(cv);

    const bar = document.querySelector('.stick');
    if (bar) bar.remove();
    if (!order && (state.cart || []).length) {
      document.body.append(h('div', { class: 'stick' }, h('button', { class: 'btn primary', type: 'button', disabled: busy, onclick: () => act(async () => { const v = await api(`/api/sessions/${sid}/checkout`, { channel: 'phone' }); return v; }) }, `Place demo order (${money(state.cart_total, state.currency)})`)));
    }
  }

  async function poll() {
    if (busy || expired || (state && state.session.order)) return;
    try { state = await api(`/api/sessions/${sid}`); error = ''; render(); } catch (e) { if (expired) render(); }
  }

  (async () => {
    try { state = await api(`/api/sessions/${sid}/phone-opened`, {}); render(); } catch (e) { if (expired) render(); else { error = e.message; render(); } }
    setInterval(poll, 3000);
  })();
})();
