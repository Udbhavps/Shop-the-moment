(() => {
  'use strict';
  const LABELS = {
    ad_view: 'Watched the ad', panel_opened: 'Paused and looked', product_opened: 'Opened a product',
    sent_to_phone: 'Sent to phone', qr_handoff_opened: 'Opened on phone',  add_to_cart: 'Added to cart',
    demo_purchase_completed: 'Demo order placed', alternative_selected: 'Picked an alternative', chat_message: 'Asked the assistant',
  };
  const label = (k) => LABELS[k] || k;
  const $ = (id) => document.getElementById(id);
  function el(tag, cls, text) { const e = document.createElement(tag); if (cls) e.className = cls; if (text != null) e.textContent = text; return e; }

  function funnel(root, stages) {
    root.replaceChildren();
    const max = Math.max(1, ...stages.map((s) => s.sessions));
    for (const s of stages) {
      const row = el('div', 'stage');
      row.append(el('div', 'name', label(s.event)));
      const bar = el('div', 'bar'); const fill = el('i'); fill.style.width = (s.sessions / max * 100) + '%'; bar.append(fill);
      row.append(bar);
      const val = el('div', 'val', String(s.sessions));
      const pct = el('small', null, ' ' + Math.round((s.rate_from_first || 0) * 100) + '%');
      val.append(pct);
      row.append(val);
      root.append(row);
    }
  }

  function branches(root, rows) {
    root.replaceChildren(el('h3', null, 'Side paths (a shopper can skip these)'), ...rows.map((r) => {
      const row = el('div', 'branch');
      row.append(el('span', null, label(r.event)), el('b', null, String(r.sessions)), el('small', null, ' ' + Math.round((r.rate_from_first || 0) * 100) + '%'));
      return row;
    }));
  }

  async function tick() {
    try {
      const a = await (await fetch('/api/analytics')).json();
      $('live-note').textContent = (a.live.note || '') + ` Sessions seen: ${a.live.sessions}. Percentages are shares of the first stage.`;
      funnel($('live'), a.live.funnel || []);
      branches($('live-branch'), a.live.branches || []);
      $('sim-note').textContent = a.simulated_sample.label || 'Illustrative numbers, not real viewers.';
      funnel($('sim'), a.simulated_sample.funnel || []);
      branches($('sim-branch'), a.simulated_sample.branches || []);
      const ev = (await (await fetch('/api/events')).json()).events || [];
      const feed = $('feed');
      if (ev.length) {
        feed.replaceChildren();
        for (const e of ev.slice(-30).reverse()) {
          const li = el('li');
          li.append(el('time', null, new Date(e.t).toLocaleTimeString()), el('span', null, label(e.name)), el('code', null, e.session ? e.session.slice(0, 4) + '...' : ''));
          feed.append(li);
        }
      }
    } catch (_) { /* keep last view */ }
  }
  tick(); setInterval(tick, 3000);
})();
