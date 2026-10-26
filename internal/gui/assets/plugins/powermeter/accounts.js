// The Balance page's account management and diagnostics: power-meter's
// /api/accounts/* and /api/login/* through /api/powermeter/ (the proxy), so
// an account is added, signed in, read or removed without leaving magpie.
//
// client.js holds the markup these draw into and page.js calls them. Every
// call is a sidecar route the proxy forwards; nothing here writes a
// credential itself, and a revealed key goes straight to the clipboard.

// The statuses a login session ends at (power-meter's login-session.ts).
const PM_LOGIN_ENDED = ["ready", "expired", "failed", "cancelled", "invalid"];

let pmLoginTimer = 0; // the poll in flight; one session at a time
let pmLastSummary = null; // the last /summary, for diagnostics
let pmLastConfig = null; // the last /config, for diagnostics and revealable keys
let pmLastOverview = null; // the last /accounts/overview
let pmLastStatus = null; // the proxy's last word on the sidecar (pmChipState)

// pmLoginStop drops a pending poll, for a page left or a session ended.
function pmLoginStop() {
  if (pmLoginTimer) {
    clearTimeout(pmLoginTimer);
    pmLoginTimer = 0;
  }
}

// pmMsg is a form's or a panel's line under its actions.
function pmMsg(text, cls) {
  return el("p", "pm-msg" + (cls ? " " + cls : ""), text);
}

// pmAcctLoad reads the accounts overview and the config, then draws both the
// account section and diagnostics. A sidecar that answers no accounts still
// leaves the rest of the page drawn: the section says why it is empty.
async function pmAcctLoad() {
  const box = document.getElementById("pmAccounts");
  try {
    pmLastOverview = await api("powermeter/accounts/overview");
  } catch (e) {
    box.replaceChildren(pmMsg(t("Couldn't read the accounts: {error}", { error: e.message }), "bad"));
    pmDiagRender();
    return;
  }
  try {
    pmLastConfig = await api("powermeter/config");
  } catch {
    pmLastConfig = null; // diagnostics says what it has; reveal is simply not offered
  }
  pmAcctRender(pmLastOverview, pmLastConfig);
  pmDiagRender();
}

// pmAcctRender draws one row per provider that has something to manage: an
// account, the way to add one, or a sign-in. Providers with neither and no
// account would only be noise (power-meter has several).
function pmAcctRender(overview, cfg) {
  // a redraw takes the providers' panels with it: a poll left over would
  // write into a panel the page no longer shows
  pmLoginStop();
  const box = document.getElementById("pmAccounts");
  const providers = (overview.providers || [])
    .filter((p) => (p.accounts || []).length || p.manageAccounts || p.login)
    .slice()
    .sort(
      (a, b) =>
        (b.accounts || []).length - (a.accounts || []).length ||
        String(a.displayName || a.id).localeCompare(String(b.displayName || b.id)),
    );
  if (!providers.length) {
    box.replaceChildren(el("p", "pm-empty", t("No providers can be managed here.")));
    return;
  }
  // power-meter repeats the config's own warnings on every provider that has
  // none of its own; said once above the list, they read as one problem.
  const shared = new Set(overview.warnings || []);
  box.replaceChildren(
    ...[...shared].map((w) => pmMsg(w, "warn")),
    ...providers.map((p) => pmAcctProvider(p, cfg, shared)),
  );
}

// pmAcctProvider is one provider's block: its accounts, then the add form
// and the login panel, both folded away until asked for.
function pmAcctProvider(p, cfg, shared) {
  const box = el("div", "pm-prov");

  const top = el("div", "pm-prov-top");
  top.append(el("strong", "pm-prov-name", p.displayName || p.id));
  const n = (p.accounts || []).length;
  top.append(el("span", "pm-prov-count", t(n === 1 ? "{n} account" : "{n} accounts", { n })));
  top.append(el("span", "grow"));

  const form = pmAcctForm(p, box);
  const login = el("div", "pm-login");
  login.hidden = true;

  if (p.login) {
    const b = el("button", "text", p.login.label || t("Sign in"));
    b.type = "button";
    b.title = p.login.hint || "";
    b.onclick = () => pmLoginOpen(p, login);
    top.append(b);
  }
  if (p.manageAccounts) {
    const b = el("button", "text", t("Add account"));
    b.type = "button";
    b.onclick = () => {
      pmLoginStop();
      login.hidden = true;
      form.hidden = !form.hidden;
      if (!form.hidden) form.querySelector("input, textarea, select")?.focus();
    };
    top.append(b);
  }
  box.append(top);
  if (p.credentialHint) box.append(el("div", "pm-prov-hint", p.credentialHint));

  for (const w of p.warnings || []) if (!shared?.has(w)) box.append(pmMsg(w, "warn"));
  if (!n) box.append(el("div", "pm-prov-none", t("No accounts yet")));
  for (const a of p.accounts || []) box.append(pmAcctRow(p, a, cfg));

  box.append(form, login);
  return box;
}

// pmAcctRow is one account: its label, its masked credentials, and the
// ways to read or remove it.
function pmAcctRow(p, a, cfg) {
  const row = el("div", "pm-acct");
  row.append(el("span", "pm-acct-name", a.label || a.id || "–"));
  if (a.complete === false) row.append(el("span", "pm-acct-flag", t("incomplete")));
  const masks = Object.entries(a.previews || {})
    .map(([k, v]) => k + " " + v)
    .join(" · ");
  if (masks) row.append(el("span", "pm-acct-mask", masks));
  if ((a.envRefs || []).length) row.title = (a.envRefs || []).join("\n");
  row.append(el("span", "grow"));

  // the keys power-meter lets this page read, by accounts.json's displayName
  const shown = (cfg?.accounts || []).find((c) => c.provider === p.id && c.displayName === a.id);
  for (const key of shown?.revealableKeys || []) {
    const b = el("button", "text", t("Show {key}", { key }));
    b.type = "button";
    b.onclick = async () => {
      b.disabled = true;
      try {
        const res = await api("powermeter/reveal", { provider: p.id, accountId: a.id, key });
        await copy(res.value, t("Credential"), b);
      } catch (e) {
        status(e.message, "err");
      }
      b.disabled = false;
    };
    row.append(b);
  }

  if (p.manageAccounts) {
    const b = el("button", "text pm-danger", t("Remove"));
    b.type = "button";
    b.onclick = async () => {
      // two clicks, as the window asks before anything that can't be undone
      if (b.dataset.armed !== "1") {
        b.dataset.armed = "1";
        b.textContent = t("Remove it?");
        return;
      }
      b.disabled = true;
      try {
        await api("powermeter/accounts/remove", { provider: p.id, accountId: a.id });
        status(t("Removed {name}", { name: a.label || a.id }), "ok");
        await pmPageLoad(false);
      } catch (e) {
        b.disabled = false;
        b.dataset.armed = "";
        b.textContent = t("Remove");
        status(e.message, "err");
      }
    };
    row.append(b);
  }
  return row;
}

// pmFieldInput is one manageFields entry's input: a choice's select, a long
// value's textarea (a cookie runs to kilobytes), else a password field.
function pmFieldInput(f) {
  let input;
  if (Array.isArray(f.choices) && f.choices.length) {
    input = document.createElement("select");
    for (const c of f.choices) {
      const o = document.createElement("option");
      o.value = c;
      o.textContent = c;
      input.append(o);
    }
    if (f.defaultValue) input.value = f.defaultValue;
  } else if (f.multiline) {
    input = document.createElement("textarea");
    input.rows = 3;
  } else {
    input = document.createElement("input");
    input.type = f.secret === false ? "text" : "password";
    input.autocomplete = "off";
    input.spellcheck = false;
  }
  input.dataset.key = f.key;
  input.dataset.label = f.label || f.key;
  input.dataset.target = f.target === "option" ? "option" : "credential";
  input.dataset.required = f.required === false ? "0" : "1";
  if (f.placeholder) input.placeholder = f.placeholder;
  return input;
}

// pmAcctForm builds a provider's add form from the fields it declares, and
// sends it to /accounts/add. The one it must not invent is the name: two
// accounts with one name are a conflict power-meter answers 409 for.
function pmAcctForm(p, panelBox) {
  const form = el("form", "pm-add");
  form.hidden = true;

  for (const f of p.manageFields || []) {
    const row = el("label", "pm-field");
    row.append(el("span", "pm-field-k", f.label || f.key));
    row.append(pmFieldInput(f));
    if (f.hint) row.append(el("span", "pm-field-hint", f.hint));
    form.append(row);
  }

  const nameRow = el("label", "pm-field");
  nameRow.append(el("span", "pm-field-k", t("Name (optional)")));
  const name = el("input", "pm-name");
  name.type = "text";
  name.autocomplete = "off";
  name.spellcheck = false;
  name.placeholder = p.displayName || p.id;
  nameRow.append(name);
  form.append(nameRow);

  const forceRow = el("label", "pm-force");
  const force = document.createElement("input");
  force.type = "checkbox";
  forceRow.append(force, el("span", "", t("Update the account if this name exists")));
  form.append(forceRow);

  if (p.manageHint) form.append(el("p", "pm-field-hint", p.manageHint));

  const actions = el("div", "pm-add-actions");
  const submit = el("button", "text pm-go", t("Add"));
  submit.type = "submit";
  const cancel = el("button", "text", t("Cancel"));
  cancel.type = "button";
  cancel.onclick = () => {
    form.hidden = true;
    form.reset();
  };
  actions.append(submit, cancel);
  const msg = pmMsg("");
  form.append(actions, msg);

  form.onsubmit = async (ev) => {
    ev.preventDefault();
    const credentials = {};
    const options = {};
    for (const input of form.querySelectorAll("[data-key]")) {
      const key = input.dataset.key;
      const value = input.value.trim();
      if (!value) {
        if (input.dataset.required === "1") {
          msg.textContent = t("{field} is required", { field: input.dataset.label });
          input.focus();
          return;
        }
        continue;
      }
      if (input.dataset.target === "option") options[key] = value;
      else credentials[key] = value;
    }
    const payload = { provider: p.id, credentials, options };
    if (name.value.trim()) payload.name = name.value.trim();
    if (force.checked) payload.force = true;

    submit.disabled = true;
    msg.textContent = t("Checking the account with the provider…");
    try {
      const res = await api("powermeter/accounts/add", payload);
      status(t("Added {name}", { name: res.name || payload.name || p.displayName }), "ok");
      form.reset();
      form.hidden = true;
      await pmPageLoad(false);
    } catch (e) {
      msg.textContent = e.message;
      submit.disabled = false;
    }
  };
  return form;
}

// pmLoginOpen starts power-meter's own sign-in for a provider and polls it
// until it ends. The auth page opens in the system browser; the token never
// reaches this page (login-session.ts).
async function pmLoginOpen(p, panel) {
  pmLoginStop();
  panel.hidden = false;
  panel.replaceChildren(el("p", "pm-empty", t("Opening the sign-in page…")));
  try {
    const res = await api("powermeter/login/start", { provider: p.id });
    pmLoginPaint(p, panel, res.session);
  } catch (e) {
    panel.replaceChildren(pmMsg(e.message, "bad"));
  }
}

// pmLoginPaint draws one session state and schedules the next poll while it
// is still going.
function pmLoginPaint(p, panel, s) {
  pmLoginStop();
  const box = el("div", "pm-login-box");
  if (s.hint) box.append(el("div", "pm-login-hint", s.hint));
  if (s.message) box.append(el("div", "pm-login-msg", s.message));

  if (s.authUrl && (s.status === "waiting" || s.status === "landing")) {
    const b = el("button", "text pm-go", s.qr
      ? t("Open the sign-in page (scan it from your phone)")
      : t("Open the sign-in page"));
    b.type = "button";
    b.onclick = () => api("open", { url: s.authUrl });
    box.append(b);
    const u = el("div", "pm-login-url", s.authUrl);
    u.title = s.authUrl;
    box.append(u);
  }
  if (s.status === "ready") {
    box.append(pmMsg(s.account?.name
      ? t("Signed in: {name}", { name: s.account.name })
      : t("Signed in"), "ok"));
  }

  const actions = el("div", "pm-add-actions");
  if (PM_LOGIN_ENDED.includes(s.status)) {
    const again = el("button", "text", t("Try again"));
    again.type = "button";
    again.onclick = () => pmLoginOpen(p, panel);
    const close = el("button", "text", t("Close"));
    close.type = "button";
    close.onclick = () => {
      pmLoginStop();
      panel.hidden = true;
      panel.replaceChildren();
    };
    actions.append(again, close);
  } else {
    const cancel = el("button", "text", t("Cancel"));
    cancel.type = "button";
    cancel.onclick = async () => {
      try {
        await api("powermeter/login/cancel", { id: s.id });
      } catch {
        /* the session may have ended on its own; the panel closes either way */
      }
      pmLoginStop();
      panel.hidden = true;
      panel.replaceChildren();
    };
    actions.append(cancel);
  }
  box.append(actions);
  panel.replaceChildren(box);

  if (s.status === "ready") {
    // the list is stale now: one account more, or one less. Said in the
    // toast too, which a redraw of the section doesn't take away.
    status(s.account?.name ? t("Signed in: {name}", { name: s.account.name }) : t("Signed in"), "ok");
    if (panel.dataset.reloaded !== s.id) {
      panel.dataset.reloaded = s.id;
      setTimeout(() => pmPageLoad(false), 900);
    }
    return;
  }
  if (!PM_LOGIN_ENDED.includes(s.status)) {
    pmLoginTimer = setTimeout(async () => {
      try {
        const res = await api("powermeter/login/poll?id=" + encodeURIComponent(s.id));
        pmLoginPaint(p, panel, res.session);
      } catch (e) {
        pmLoginStop();
        panel.replaceChildren(pmMsg(e.message, "bad"));
      }
    }, 2000);
  }
}

// pmDiagRender says what the sidecar is, where its config lives, and what
// failed: the platforms that answered with an error, the warnings the
// config and the refresh carried, and the environment variables a config
// names but the process doesn't have.
function pmDiagRender() {
  const box = document.getElementById("pmDiag");
  if (!box) return;
  const rows = [];
  // el() takes one child, so the row's two spans are appended, not passed.
  const row = (label, value, cls) => {
    const r = el("div", "pm-diag-row" + (cls ? " " + cls : ""));
    r.append(el("span", "pm-diag-k", label), el("span", "pm-diag-v", value));
    rows.push(r);
  };

  row(
    t("Sidecar"),
    pmLastStatus
      ? [pmLastStatus.state, pmLastStatus.url, pmLastStatus.detail].filter(Boolean).join(" · ")
      : "–",
    pmLastStatus?.state === "up" ? "ok" : "bad",
  );
  if (pmLastConfig?.accountsFilePath) row(t("Config file"), pmLastConfig.accountsFilePath);

  const warnings = new Set(pmLastConfig?.warnings || []);
  for (const w of pmLastSummary?.warnings || []) warnings.add(w);
  for (const p of pmLastOverview?.providers || []) for (const w of p.warnings || []) warnings.add(w);
  const missing = new Set([
    ...(pmLastConfig?.missingEnvVars || []),
    ...(pmLastSummary?.missingEnvVars || []),
  ]);
  const failed = (pmLastSummary?.providers || []).filter(
    (p) => p.status !== "ok" && p.status !== "demo",
  );

  for (const p of failed) {
    row(
      p.displayName || p.provider,
      [p.status, p.message].filter(Boolean).join(" · "),
      "bad",
    );
  }
  for (const w of warnings) row(t("Warning"), w, "warn");
  for (const v of missing) row(t("Missing environment variable"), v, "warn");

  if (!failed.length && !warnings.size && !missing.size) {
    rows.push(el("p", "pm-empty", t("Nothing to report: every provider answered.")));
  }
  box.replaceChildren(...rows);
}
