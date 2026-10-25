// The Balance page's code (client.js has its markup and hooks): run after
// app.js, whose helpers (api, el, t, status) and state it shares.
//
// Everything here reads the power-meter sidecar through /api/powermeter/
// (internal/powermeterproxy). The page never invents a state: the status
// chip is the proxy's own word — disabled, starting, up, down.

// pmNum formats an amount for a card or a lot, grouped, no trailing zeros.
function pmNum(n) {
  if (n === null || n === undefined || isNaN(n)) return "–";
  const v = Number(n);
  const s = Math.abs(v) >= 1000
    ? v.toLocaleString(undefined, { maximumFractionDigits: 2 })
    : v.toLocaleString(undefined, { maximumFractionDigits: 4 });
  return s;
}

// pmDate renders an ISO date as YYYY-MM-DD (the lot's expiry), "–" when none.
function pmDate(iso) {
  if (!iso) return "–";
  const d = new Date(iso);
  return isNaN(d) ? "–" : d.toLocaleDateString(undefined, { year: "numeric", month: "2-digit", day: "2-digit" });
}

// pmDays is the whole days from now to an ISO time, null when there is none.
function pmDays(iso) {
  if (!iso) return null;
  const d = new Date(iso).getTime();
  if (isNaN(d)) return null;
  return Math.ceil((d - Date.now()) / 86400000);
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

// pmLot builds one credit lot's row on the timeline.
function pmLot(hit, th) {
  const days = hit.inDays !== undefined ? Math.ceil(hit.inDays) : pmDays(hit.expiresAt);
  const level = pmLevel(days, th);
  const row = el("div", "pm-lot pm-lv-" + level);
  row.append(el("span", "pm-lot-dot", ""));
  const body = el("div", "pm-lot-body");
  body.append(el("span", "pm-lot-label", hit.label || hit.provider || "–"));
  const meta = [];
  if (hit.provider) meta.push(hit.provider);
  if (hit.expiresAt) meta.push(t("expires {date}", { date: pmDate(hit.expiresAt) }));
  else meta.push(t("never expires"));
  if (days !== null && days !== undefined) meta.push(t(days === 1 ? "within {n} day" : "within {n} days", { n: days }));
  body.append(el("span", "pm-lot-meta", meta.join(" · ")));
  row.append(body);
  const amt = hit.amount !== undefined ? pmNum(hit.amount) + " " + (hit.currency || "") : "";
  if (hit.cnyValue !== undefined && hit.cnyValue !== null) {
    row.append(el("span", "pm-lot-cny", "≈ ¥" + pmNum(hit.cnyValue)));
  }
  row.append(el("span", "pm-lot-amt", amt));
  return row;
}

// pmRender draws the whole payload: totals, cards, the timeline.
function pmRender(data) {
  const th = data.expiryThresholds || { criticalDays: 5, warningDays: 14 };

  // totals
  const totals = el("div", "pm-total-row");
  const tt = data.totals || {};
  const cell = (label, value, cls) => el("div", "pm-total " + (cls || ""),
    el("span", "pm-total-num", value), el("span", "pm-total-label", t(label)));
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

  // cards, lowest balance first
  const cards = document.getElementById("pmCards");
  const providers = (data.providers || []).slice();
  if (!providers.length) {
    cards.replaceChildren(el("p", "pm-empty", t("No providers configured")));
  } else {
    cards.replaceChildren(...providers.map(pmCard));
  }

  // timeline, by expiry
  const tl = document.getElementById("pmTimeline");
  if (!timeline.length) {
    tl.replaceChildren(el("p", "pm-empty", t("Nothing is expiring")));
  } else {
    tl.replaceChildren(...timeline.map((h) => pmLot(h, th)));
  }
}

// pmChipState paints the status chip and toggles the off/body panels.
function pmChipState(state, detail) {
  const chip = document.getElementById("pmStatus");
  const text = document.getElementById("pmStatusText");
  chip.dataset.state = state;
  const off = document.getElementById("pmOff");
  const body = document.getElementById("pmBody");
  const on = state === "up";
  off.hidden = state !== "disabled";
  body.hidden = !on;
  text.textContent =
    state === "up" ? t("Refreshed") :
    state === "disabled" ? t("The power-meter sidecar is off") :
    state === "down" ? t("The sidecar is unreachable") :
    t("The sidecar is starting…");
  if (detail) chip.title = detail;
}

// pmPageLoad reads the sidecar's status, then its summary when up.
async function pmPageLoad(force) {
  let st;
  try {
    st = await api("powermeter/status");
  } catch (e) {
    pmChipState("down", e.message);
    return;
  }
  pmChipState(st.state, st.detail);
  if (st.state !== "up") return;
  try {
    const data = await api("powermeter/summary" + (force ? "?force=1" : ""));
    pmRender(data);
    pmChipState("up");
  } catch (e) {
    pmChipState("down", e.message);
    status(t(e.message), "err");
  }
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
    document.getElementById("pmOpenSettings").addEventListener("click", () => {
      // open Settings where the sidecar's switch lives
      document.querySelector('[data-view="settings"]')?.click();
    });
  }
  return pmPageLoad(false);
}
