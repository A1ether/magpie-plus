// The Balance page (powermeter): every provider's balance as cards, and each
// credit lot's expiry as a timeline, read from the power-meter sidecar
// through /api/powermeter/.
//
// A page the powermeter plugin adds to the window (kernel.js), its words in
// i18n.js beside this file. This file registers the page and its markup,
// before app.js; page.js, after app.js, is the page's code. The hooks are
// what the shell asks of it: open (the reader came to the page), refresh
// (the window came back), reload (Refresh in the sidebar). The sidecar's own
// on/off is Settings' powerMeter, separate from this plugin's switch.
magpie.plugin("powermeter", (ctx) => {
  ctx.slots.add("page", {
    key: "powermeter",
    title: "Balance board",
    html: `
  <div class="pm-head">
    <span class="pm-status" id="pmStatus" data-state="starting">
      <span class="pm-dot" aria-hidden="true"></span>
      <span id="pmStatusText" data-t>The sidecar is starting…</span>
    </span>
    <span class="grow"></span>
    <button class="icon" id="pmReload" type="button" aria-label="Refresh now" data-tt><svg viewBox="0 0 16 16" width="13" height="13" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M13.5 8a5.5 5.5 0 1 1-1.6-3.9M13.5 2.5v3h-3"/></svg></button>
  </div>

  <section class="pm-off" id="pmOff" hidden>
    <p class="pm-off-title" data-t>The power-meter sidecar is off</p>
    <p class="pm-off-sub" data-t>Turn it on in Settings to see balances and expiry here.</p>
    <button class="text" id="pmOpenSettings" type="button" data-t>Open Settings</button>
  </section>

  <div class="pm-body" id="pmBody" hidden>
    <section class="pm-totals" id="pmTotals"></section>

    <div class="row-head"><button class="fold" type="button" data-fold="cards" aria-expanded="true" aria-controls="pmCards"><span class="label" data-t>Balance cards</span></button>
      <span class="row-sub" data-t>Every provider, lowest balance first</span></div>
    <section class="pm-cards" id="pmCards"></section>

    <div class="row-head"><button class="fold" type="button" data-fold="timeline" aria-expanded="true" aria-controls="pmTimeline"><span class="label" data-t>Expiry timeline</span></button>
      <span class="row-sub" data-t>Every credit lot, by expiry</span></div>
    <section class="pm-timeline" id="pmTimeline"></section>
  </div>
`,
    open() { return pmPageOpen(); },
    refresh(again) { return pmPageLoad(again); },
    reload() { return pmPageLoad(true); },
  });
});
