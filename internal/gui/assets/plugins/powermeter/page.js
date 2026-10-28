// The Balance page's code (client.js has its markup and hooks): run after
// app.js, whose helpers (api, el, t, status) and state it shares.
//
// Everything here reads the power-meter sidecar through /api/powermeter/
// (internal/powermeterproxy). The page never invents a state: the status
// chip is the proxy's own word — disabled, starting, up, down.

let pmStatusTimer = 0; // a pending status retry while the sidecar starts
let pmStatusWait = 0; // that retry's delay, growing to pmStatusPoll's cap

// pmNum formats an amount for a card or a lot, grouped, no trailing zeros.
function pmNum(n) {
  if (n === null || n === undefined || isNaN(n)) return "–";
  const v = Number(n);
  const s = Math.abs(v) >= 1000
    ? v.toLocaleString(undefined, { maximumFractionDigits: 2 })
    : v.toLocaleString(undefined, { maximumFractionDigits: 4 });
  return s;
}

// pmWhen renders an ISO time to the minute, in the reader's own time zone.
// A lot's expiry is a moment: "2026-10-29" alone can't tell the 00:00 batch
// from the 23:59 one, which is the difference between spending it today and
// losing it tonight.
function pmWhen(iso) {
  if (!iso) return "–";
  const d = new Date(iso);
  if (isNaN(d)) return "–";
  const p = (n) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`;
}

// pmLeft says how long is left, to the minute under an hour and to the hour
// under a day: "4 小时 12 分后" is what a batch lost at midnight needs, and
// "12 天后" is enough for one a fortnight out. A lot that is already past its
// time says nothing here — the timeline lists what is still to come, and the
// date above it already reads as past.
function pmLeft(iso, now) {
  const ms = Date.parse(iso) - now;
  if (isNaN(ms) || ms <= 0) return "";
  const mins = Math.round(ms / 60000);
  if (mins < 60) return t("{n} minutes left", { n: mins });
  if (mins < 1440) return t("{n} hours {m} minutes left", { n: Math.floor(mins / 60), m: mins % 60 });
  return t("{n} days left", { n: Math.ceil(ms / 86400000) });
}

// pmDays is the whole days from now to an ISO time, null when there is none.
function pmDays(iso) {
  if (!iso) return null;
  const d = new Date(iso).getTime();
  if (isNaN(d)) return null;
  return Math.ceil((d - Date.now()) / 86400000);
}

// PM_TIME_FULL_DAYS is the horizon a time bar fills against: a lot expiring
// within it shows how much of the wait is left, one further out reads full.
// It is only used when the sidecar doesn't know the lot's granted total.
const PM_TIME_FULL_DAYS = 30;

// pmLotBar is one lot's progress. When the sidecar knows the lot's granted
// total the bar is what is still there (amount / granted); otherwise it is
// the share of PM_TIME_FULL_DAYS still to wait. It is never the amount
// against itself, which would read full until the moment it vanished.
function pmLotBar(hit, now) {
  const bar = el("div", "pm-lot-bar");
  const fill = el("span", "pm-lot-bar-fill");
  let pct, why;
  if (hit.granted > 0) {
    pct = (hit.amount / hit.granted) * 100;
    why = t("The bar is how much of the lot is left: {left} of {granted}",
      { left: pmNum(hit.amount), granted: pmNum(hit.granted) });
  } else {
    pct = ((Date.parse(hit.expiresAt) - now) / (PM_TIME_FULL_DAYS * 86400000)) * 100;
    why = t("The bar is the time left until it expires; {n} days is full", { n: PM_TIME_FULL_DAYS });
  }
  pct = Math.max(0, Math.min(100, pct));
  if (!isFinite(pct)) pct = 0;
  bar.title = why;
  fill.style.setProperty("--w", pct.toFixed(1) + "%");
  bar.append(fill);
  return bar;
}

// pmLotCaption says the figures behind a lot's bar in words: how much of
// this lot is left of what it was granted, what share of the provider's own
// left it is, and, for a lot whose platform tells only what it was granted,
// that the amount on the right is that total rather than what is left.
function pmLotCaption(hit, providerLeft, basis) {
  const bits = [];
  if (hit.granted > 0) {
    bits.push(t("Remaining {left} of {granted}", { left: pmNum(hit.amount), granted: pmNum(hit.granted) }));
  } else if (basis === "granted") {
    bits.push(t("Granted {n}; how much is left isn't told", { n: pmNum(hit.amount) }));
  }
  if (providerLeft > 0 && hit.amount <= providerLeft) {
    bits.push(t("{pct}% of the provider's left", { pct: Math.round((hit.amount / providerLeft) * 100) }));
  }
  return bits.join(" · ");
}

// pmLevel buckets a lot by the page's thresholds: critical, warning, or ok.
function pmLevel(inDays, th) {
  if (inDays === null || inDays === undefined) return "ok";
  if (inDays <= th.criticalDays) return "critical";
  if (inDays <= th.warningDays) return "warning";
  return "ok";
}

// pmCard builds one provider's balance card. A provider with several accounts
// (power-meter's groupSnapshots merges them) lists each entry, lowest first.
function pmCard(p) {
  const card = el("section", "pm-card");
  const head = el("div", "pm-card-head");
  head.append(
    el("span", "pm-card-status pm-st-" + (p.status || "error"), ""),
    el("strong", "pm-card-name", p.displayName || p.provider),
  );
  if (p.plan) head.append(el("span", "pm-card-plan", p.plan));
  card.append(head);

  // balance: each currency on its own line (units aren't mixed)
  const bal = el("div", "pm-card-bal");
  const balances = p.balances || (p.balance ? [p.balance] : []);
  if (balances.length) {
    for (const b of balances) {
      bal.append(el("div", "pm-card-amount",
        el("span", "pm-num", pmNum(b.amount)) )); // number
      bal.lastChild.append(el("span", "pm-cur", b.currency || ""));
    }
  } else {
    bal.append(el("div", "pm-card-none", t("No balance")));
  }
  card.append(bal);
  const usage = pmUsage(p);
  if (usage) card.append(usage);

  // accounts: a merged card's entries, lowest balance first. An entry names
  // itself by id ("Provider · seat") and carries its amount/currency flat.
  if (Array.isArray(p.entries) && p.entries.length > 1) {
    const list = el("div", "pm-entries");
    for (const e of p.entries) {
      const row = el("div", "pm-entry");
      const name = (e.id && String(e.id).includes("·") ? String(e.id).split("·").slice(1).join("·").trim() : e.id) || e.label || "?";
      row.append(el("span", "pm-entry-name", name));
      const eb = e.amount !== undefined ? e : (e.balances || (e.balance ? [e.balance] : []))[0];
      row.append(el("span", "pm-entry-bal", eb ? pmNum(eb.amount) + " " + (eb.currency || "") : "–"));
      list.append(row);
    }
    card.append(list);
  }
  return card;
}

// PM_USAGE_SOURCE names where a day figure came from, field by field: the
// provider's own usage page, this computer's sampling of its balances, or
// both. It is shown on hover, because a local sample is not the provider's
// word and the reader should be able to tell.
const PM_USAGE_SOURCE = {
  upstream: "as the provider's own usage page says",
  local: "as this computer's sampling saw it",
  mixed: "the provider's usage page and this computer's sampling",
};

// pmUsage is a card's day figures: today's and yesterday's use (what the
// day took) and additions (what it gained, such as a check-in or a top-up).
// A null is not 0 — local sampling has no record of a day it did not see —
// so it reads as "–".
function pmUsage(p) {
  const du = p.dailyUsage;
  if (!du) return null;
  const wrap = el("div", "pm-usage");
  const cell = (key, value, source) => {
    const c = el("div", "pm-usage-cell");
    c.append(el("span", "pm-usage-v", value === null || value === undefined ? "–" : pmNum(value)));
    c.append(el("span", "pm-usage-k", t(key)));
    if (value === null || value === undefined) c.classList.add("pm-usage-none");
    if (source) c.title = t(PM_USAGE_SOURCE[source] || source);
    return c;
  };
  wrap.append(cell("Today used", du.todayUsed, du.sources && du.sources.todayUsed));
  wrap.append(cell("Yesterday used", du.yesterdayUsed, du.sources && du.sources.yesterdayUsed));
  wrap.append(cell("Today added", du.todayAdded, du.sources && du.sources.todayAdded));
  wrap.append(cell("Yesterday added", du.yesterdayAdded, du.sources && du.sources.yesterdayAdded));
  if (du.currency) wrap.append(el("span", "pm-usage-cur", du.currency));
  return wrap;
}

// pmLot builds one credit lot's row on the timeline: what it is, when it
// runs out (to the minute), how long that is from now, and how much of it is
// left.
function pmLot(hit, th, now, providerLeft, basis) {
  const days = hit.inDays !== undefined ? Math.ceil(hit.inDays) : pmDays(hit.expiresAt);
  const level = pmLevel(days, th);
  const row = el("div", "pm-lot pm-lv-" + level);
  row.append(el("span", "pm-lot-dot", ""));
  const body = el("div", "pm-lot-body");
  body.append(el("span", "pm-lot-label", hit.lotLabel || hit.displayName || hit.provider || "–"));
  const meta = [];
  if (hit.displayName) meta.push(hit.displayName);
  meta.push(hit.expiresAt ? t("expires {date}", { date: pmWhen(hit.expiresAt) }) : t("never expires"));
  const left = pmLeft(hit.expiresAt, now);
  if (left) meta.push(left);
  body.append(el("span", "pm-lot-meta", meta.join(" · ")));
  body.append(pmLotBar(hit, now));
  const cap = pmLotCaption(hit, providerLeft, basis);
  if (cap) body.append(el("span", "pm-lot-caption", cap));
  row.append(body);
  // the figures travel together: on a narrow window they take a line of
  // their own under the bar rather than squeezing it to a stub
  const figs = el("span", "pm-lot-figs");
  if (hit.cny !== undefined && hit.cny !== null) {
    figs.append(el("span", "pm-lot-cny", "≈ ¥" + pmNum(hit.cny)));
  }
  if (hit.amount !== undefined) {
    figs.append(el("span", "pm-lot-amt", pmNum(hit.amount) + " " + (hit.currency || "")));
  }
  row.append(figs);
  return row;
}

// pmBasisByLot is each lot's basis, by provider and lot id: "granted" marks
// a platform that tells what it granted and not what is left, so the figure
// on the row is not a remainder.
function pmBasisByLot(providers) {
  const basis = new Map();
  for (const p of providers) {
    for (const lot of p.credits || []) basis.set((p.provider || "") + "|" + lot.id, lot.basis);
  }
  return basis;
}

// pmLeftByProvider is each provider's remaining amount per currency, the
// sum of its lots that are not just "granted" figures (those don't add up).
// A lot's share of it says whether this one is the bulk of what is left.
function pmLeftByProvider(providers) {
  const left = new Map();
  for (const p of providers) {
    for (const lot of p.credits || []) {
      if (lot.basis === "granted") continue;
      const key = (p.provider || "") + "|" + (lot.currency || "");
      left.set(key, (left.get(key) || 0) + (lot.amount || 0));
    }
  }
  return left;
}

// pmRender draws the whole payload: totals, the timeline, then the cards.
function pmRender(data) {
  pmLastSummary = data;
  const th = data.expiryThresholds || { criticalDays: 5, warningDays: 14 };
  const now = Date.now();
  const providers = (data.providers || []).slice();
  const leftBy = pmLeftByProvider(providers);
  const basisBy = pmBasisByLot(providers);

  // totals
  const totals = el("div", "pm-total-row");
  const tt = data.totals || {};
  // el() takes one child: its label is appended, or the cell would read as a
  // number with nothing saying what it counts
  const cell = (label, value, cls) => {
    const c = el("div", "pm-total " + (cls || ""), el("span", "pm-total-num", value));
    c.append(el("span", "pm-total-label", t(label)));
    return c;
  };
  totals.append(cell("Providers", tt.providers ?? "–"));
  totals.append(cell("Healthy", tt.ok ?? "–", "ok"));
  totals.append(cell("Failed", tt.failed ?? "–", tt.failed ? "bad" : ""));
  const byCur = tt.availableByCurrency || {};
  const balTxt = Object.keys(byCur).length
    ? Object.entries(byCur).map(([c, v]) => pmNum(v) + " " + c).join("  ")
    : "–";
  totals.append(cell("Balances", balTxt, "wide"));
  const timeline = data.expiryTimeline || [];
  totals.append(cell("Expiring soon", String(timeline.length), timeline.length ? "warn" : ""));
  document.getElementById("pmTotals").replaceChildren(totals);

  // the timeline first: what runs out soonest is the page's reason for
  // being, and each lot's bar says how much of it is left
  const tl = document.getElementById("pmTimeline");
  if (!timeline.length) {
    tl.replaceChildren(el("p", "pm-empty", t("Nothing is expiring")));
  } else {
    tl.replaceChildren(...timeline.map((h) =>
      pmLot(h, th, now, leftBy.get((h.provider || "") + "|" + (h.currency || "")) || 0,
        basisBy.get((h.provider || "") + "|" + h.lotId))));
  }

  // then the cards, lowest balance first
  const cards = document.getElementById("pmCards");
  if (!providers.length) {
    cards.replaceChildren(el("p", "pm-empty", t("No providers configured")));
  } else {
    cards.replaceChildren(...providers.map(pmCard));
  }
}

// pmChipState paints the status chip and toggles the off/body panels. The
// last word is kept for the diagnostics section, which says it in full.
function pmChipState(state, detail, url) {
  pmLastStatus = { state, detail: detail || "", url: url || pmLastStatus?.url || "" };
  const chip = document.getElementById("pmStatus");
  const text = document.getElementById("pmStatusText");
  chip.dataset.state = state;
  const off = document.getElementById("pmOff");
  const body = document.getElementById("pmBody");
  const on = state === "up";
  off.hidden = state !== "disabled";
  body.hidden = !on;
  // the switch out is beside the refresh, only while there is a sidecar to
  // stop; a disabled page offers the switch on in its place
  document.getElementById("pmTurnOff").hidden = state === "disabled";
  const key =
    state === "up" ? "Refreshed just now" :
    state === "disabled" ? "The power-meter sidecar is off" :
    state === "down" ? "The sidecar is unreachable" :
    "The sidecar is starting…";
  text.textContent = t(key);
  // The chip is a [data-t] node, and translateStatic remembers a node's
  // English to re-render it when the language changes. Left alone, it would put
  // back whichever state the chip said when the language last changed — a
  // "starting" chip over a sidecar that is up — so the key is kept here.
  text.dataset.en = key;
  if (detail) chip.title = detail;
}

// pmPageLoad reads the sidecar's status, then its summary, its accounts and
// its diagnostics when up.
async function pmPageLoad(force) {
  let st;
  try {
    st = await api("powermeter/status");
  } catch (e) {
    pmChipState("down", e.message);
    pmDiagRender();
    return;
  }
  pmChipState(st.state, st.detail, st.url);
  if (st.state !== "up") {
    pmDiagRender();
    // The sidecar takes a few seconds to come up (and restarts after a crash),
    // so the page asks again rather than sit on "starting…" until Refresh now.
    if (st.state === "starting") pmStatusPoll();
    else pmStatusStop();
    return;
  }
  pmStatusStop();
  try {
    const data = await api("powermeter/summary" + (force ? "?force=1" : ""));
    pmRender(data);
    pmChipState("up");
  } catch (e) {
    pmChipState("down", e.message);
    status(t(e.message), "err");
  }
  await pmAcctLoad();
}

// pmStatusStop drops a pending status retry, for a sidecar that answered or
// for a page that leaves.
function pmStatusStop() {
  clearTimeout(pmStatusTimer);
  pmStatusTimer = 0;
  pmStatusWait = 0;
}

// pmStatusPoll asks about the sidecar again a little later, backing off to a
// quiet 15s so a sidecar that stays down costs no more than that.
function pmStatusPoll() {
  clearTimeout(pmStatusTimer);
  pmStatusWait = Math.min(pmStatusWait ? pmStatusWait * 2 : 1500, 15000);
  pmStatusTimer = setTimeout(() => pmPageLoad(false), pmStatusWait);
}

// pmFoldBind gives every row head's fold its chevron and its toggle. The
// page draws its own folds (the window's data-fold has no handler of its
// own); the box is the one the button names in aria-controls.
function pmFoldBind() {
  for (const b of document.querySelectorAll("#view-powermeter .row-head .fold[aria-controls]")) {
    if (b.dataset.bound) continue;
    b.dataset.bound = "1";
    if (!b.querySelector("svg")) b.prepend(svg(CHEV_R, 11, 1.6));
    b.addEventListener("click", () => {
      const box = document.getElementById(b.getAttribute("aria-controls"));
      const open = b.getAttribute("aria-expanded") !== "false";
      b.setAttribute("aria-expanded", String(!open));
      if (box) box.hidden = open;
    });
  }
}

// pmPowerSet turns the sidecar on or off from this page: the switch is the
// page's own, because a sidecar the user hasn't started is exactly what this
// page shows them. A sidecar takes a few seconds to come up, so the status
// poll then watches it arrive.
async function pmPowerSet(on, button) {
  button.disabled = true;
  try {
    await api("settings/power-meter", { on });
  } catch (e) {
    status(t(e.message), "err");
    button.disabled = false;
    return;
  }
  button.disabled = false;
  await pmPageLoad(false);
}

function pmPageOpen() {
  const reload = document.getElementById("pmReload");
  if (!reload.dataset.bound) {
    reload.dataset.bound = "1";
    reload.addEventListener("click", async () => {
      reload.disabled = true;
      try {
        await api("powermeter/refresh", {});
      } catch (e) { /* a failed refresh is reported by the reload below */ }
      await pmPageLoad(true);
      reload.disabled = false;
    });
    const on = document.getElementById("pmTurnOn");
    on.addEventListener("click", () => pmPowerSet(true, on));
    const off = document.getElementById("pmTurnOff");
    off.addEventListener("click", () => pmPowerSet(false, off));
  }
  pmFoldBind();
  return pmPageLoad(false);
}
