// Run with Node's test runner and Playwright on the module path; see README.md.
// The Balance page's sidecar switch and its four chip states. The sidecar is
// opt-in: with the setting off the page shows what it is and offers to start
// it, and the switch is the page's own (POST /api/settings/power-meter), not
// a row somewhere the reader has to find. Turning it on draws the board the
// proxy then answers; turning it off takes the board away again. A sidecar
// that answers no status, or a status of down, leaves the page saying so
// rather than blank. Both engines, English and Chinese; the proxy and the
// sidecar are faked here (internal/powermeterproxy's own tests cover the
// forwarding, internal/gui/powermeter_test.go the switch's Go side).
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");

// what power-meter's /api/summary answers with for a sidecar that is up,
// in the shape the sidecar really sends (src/types.ts): a lot carries its
// lotLabel and displayName, the expiry as a moment, and its yuan value as
// cny; a card carries its own lots and the day's four figures.
const summary = () => {
  const expiresAt = new Date(Date.now() + 3 * 86400000).toISOString();
  const farAt = new Date(Date.now() + 60 * 86400000).toISOString();
  return {
    totals: { providers: 2, ok: 1, failed: 1, availableByCurrency: { USD: 42.5, CNY: 8 } },
    providers: [
      {
        provider: "qoder", displayName: "Qoder", plan: "Pro", status: "ok",
        balance: { amount: 12.5, currency: "USD" },
        credits: [
          { id: "qoder-1", label: "Qoder credits", amount: 500, currency: "credits", expiresAt, note: "", basis: "remaining" },
          { id: "qoder-2", label: "Qoder bonus", amount: 200, currency: "credits", expiresAt: farAt, note: "", basis: "remaining" },
        ],
        dailyUsage: {
          todayUsed: 12, yesterdayUsed: 40, todayAdded: 5, yesterdayAdded: 0, currency: "USD",
          sources: { todayUsed: "upstream", yesterdayUsed: "upstream", todayAdded: "local", yesterdayAdded: "local" },
        },
      },
      { provider: "trae", displayName: "Trae", status: "error", message: "403 from the vendor", credits: [], dailyUsage: null },
    ],
    expiryTimeline: [
      {
        provider: "qoder", displayName: "Qoder", lotId: "qoder-1", lotLabel: "Qoder credits",
        amount: 500, currency: "credits", expiresAt, inDays: 3, cny: 8, cnyNote: "",
      },
      {
        provider: "qoder", displayName: "Qoder", lotId: "qoder-2", lotLabel: "Qoder bonus",
        amount: 200, currency: "credits", expiresAt: farAt, inDays: 60, cny: null, cnyNote: null,
      },
    ],
    expiryThresholds: { criticalDays: 5, warningDays: 14 },
    warnings: [],
    missingEnvVars: [],
  };
};

// serve is the window's backend with the sidecar's state in sidecar.state:
// "disabled" (the setting off), "starting", "down" or "up". The switch's POST
// moves it, as settings.SetPowerMeter and the live supervisor do. Every
// sidecar read is recorded, so a page that asks while it should be off is
// visible.
function serve(lang, sidecar) {
  const settings = { lang, theme: "light", powerMeter: sidecar.state !== "disabled" };
  return async (route) => {
    const req = route.request();
    const url = new URL(req.url());
    const json = (data, status) => route.fulfill({ status: status || 200, json: data });
    const off = () => route.fulfill({ status: 503, json: { error: "power-meter sidecar is " + (sidecar.state === "disabled" ? "disabled" : "starting"), state: sidecar.state } });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:${JSON.stringify(lang)},theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings });
    if (url.pathname === "/api/settings" || url.pathname === "/api/settings/power-meter") {
      if (url.pathname === "/api/settings/power-meter") {
        sidecar.posts.push(JSON.parse(req.postData() || "{}"));
        // the switch writes the setting, and the supervisor follows it
        sidecar.state = sidecar.posts.at(-1).on ? "up" : "disabled";
        settings.powerMeter = sidecar.state !== "disabled";
      }
      return json({ ...settings });
    }
    if (url.pathname === "/api/powermeter/status") {
      sidecar.reads.push("status");
      const out = { state: sidecar.state, enabled: sidecar.state !== "disabled" };
      if (sidecar.state === "up") out.url = "http://127.0.0.1:9/";
      if (sidecar.state === "down") out.detail = "the sidecar couldn't start: server.js is missing";
      return json(out);
    }
    if (url.pathname === "/api/powermeter/summary") {
      sidecar.reads.push(url.searchParams.get("force") === "1" ? "summary?force" : "summary");
      return sidecar.state === "up" ? json(summary()) : off();
    }
    if (url.pathname === "/api/powermeter/refresh") {
      sidecar.reads.push("refresh");
      return sidecar.state === "up" ? json({ ok: true }) : off();
    }
    if (url.pathname === "/api/powermeter/accounts/overview") {
      sidecar.reads.push("accounts");
      return sidecar.state === "up" ? json({ providers: [], warnings: [] }) : off();
    }
    if (url.pathname === "/api/powermeter/config") {
      sidecar.reads.push("config");
      return sidecar.state === "up" ? json({ accountsFilePath: "/tmp/magpie/powermeter/accounts.json", warnings: [], missingEnvVars: [] }) : off();
    }
    if (url.pathname === "/api/groups") return json({ groups: [], models: [] });
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/plugins/updates") return json({ waiting: [] });
    if (url.pathname === "/api/plugin-pages" || url.pathname === "/api/plugin-panels") return json([]);
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true } });
    if (url.pathname === "/api/usage/quotas" || url.pathname === "/api/usage/quotas/history") return json([]);
    if (url.pathname === "/api/agents/cli") return json({ agents: {}, pending: false });
    if (url.pathname === "/api/gateway/trace") return json({ mine: false, now: "", totals: { requests: 0, rerouted: 0, errors: 0 } });
    if (url.pathname === "/api/update" || url.pathname === "/api/drift" || url.pathname === "/api/whatsnew") return json({});
    // everything else the window asks on its way up (installs, CLIs, the
    // usage series): nothing, as a fresh install answers
    assert(!url.pathname.startsWith("/api/powermeter/"), "Unexpected sidecar read: " + url.pathname);
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

async function open(browser, t, lang, state) {
  const sidecar = { state, reads: [], posts: [] };
  const errors = [];
  const context = await browser.newContext({ viewport: { width: 900, height: 760 }, reducedMotion: "reduce" });
  t.after(() => context.close());
  const page = await context.newPage();
  page.setDefaultTimeout(5000);
  page.on("pageerror", (e) => errors.push(e.message));
  await page.route("**/*", serve(lang, sidecar));
  await page.goto("http://magpie.test/?view=powermeter");
  // the page is open once its status read is in
  await page.waitForFunction(() => !document.getElementById("pmStatus")?.closest("main")?.hidden);
  return { page, sidecar, errors };
}

// the words the page shows, read from the page's own dictionaries so the test
// doesn't carry a copy of each language
const word = (page, key, vars) => page.evaluate(([k, v]) => t(k, v), [key, vars]);
const chip = (page) => page.locator("#pmStatusText").textContent();
const visible = (page, sel) => page.locator(sel).isVisible();
// a word every language says in its own words, for the fallback check
const LANGS = ["en", "zh", "zh-TW", "ja", "de"];

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of LANGS) {
    const launch = () => (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));

    test(`${engine} ${lang}: the sidecar off is said, and the page turns it on`, async (t) => {
      const browser = await launch();
      t.after(() => browser.close());
      const { page, sidecar, errors } = await open(browser, t, lang, "disabled");
      await page.waitForFunction(() => document.getElementById("pmOff") && !document.getElementById("pmOff").hidden);

      // the off panel is the page's answer, with the switch on it
      assert.equal(await chip(page), await word(page, "The power-meter sidecar is off"));
      if (lang !== "en") assert.notEqual(await chip(page), "The power-meter sidecar is off", `${lang} says it in its own words`);
      assert.equal(await page.locator("#pmStatus").getAttribute("data-state"), "disabled");
      assert(await visible(page, "#pmOff"), "the off panel is shown");
      assert(await visible(page, "#pmTurnOn"), "the switch on is on the page");
      assert(!(await visible(page, "#pmBody")), "no board while the sidecar is off");
      assert(!(await visible(page, "#pmTurnOff")), "nothing to turn off while it is off");
      assert(!sidecar.reads.includes("summary"), `no board read while off: ${sidecar.reads}`);

      // turning it on writes the setting, and the board the proxy now answers is drawn
      await page.locator("#pmTurnOn").click();
      await page.waitForFunction(() => !document.getElementById("pmBody").hidden);
      assert.deepEqual(sidecar.posts, [{ on: true }], `the switch posts the setting: ${JSON.stringify(sidecar.posts)}`);
      assert.equal(await chip(page), await word(page, "Refreshed just now"));
      if (lang !== "en") assert.notEqual(await chip(page), "Refreshed just now", `${lang} says it in its own words`);
      assert.equal(await page.locator("#pmStatus").getAttribute("data-state"), "up");
      assert(!(await visible(page, "#pmOff")), "the off panel goes with the sidecar on");
      assert(await visible(page, "#pmTurnOff"), "the switch off appears");
      assert.ok((await page.locator("#pmCards").textContent()).includes("Qoder"), "the cards are drawn");
      assert.ok((await page.locator("#pmTotals").textContent()).includes("42.5 USD"), "the totals are drawn");
      assert.ok((await page.locator("#pmTimeline").textContent()).includes("Qoder credits"), "the timeline is drawn");

      // and off again: the setting is written, the board goes
      sidecar.state = "up";
      await page.locator("#pmTurnOff").click();
      await page.waitForFunction(() => !document.getElementById("pmOff").hidden);
      assert.deepEqual(sidecar.posts, [{ on: true }, { on: false }], `the switch off posts the setting: ${JSON.stringify(sidecar.posts)}`);
      assert.equal(await chip(page), await word(page, "The power-meter sidecar is off"));
      assert(!(await visible(page, "#pmBody")), "the board goes with the sidecar off");
      assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1), "the page fits");
      assert.deepEqual(errors, []);
    });

    test(`${engine} ${lang}: a sidecar that is up draws the board, and one that is down says so`, async (t) => {
      const browser = await launch();
      t.after(() => browser.close());
      const { page, sidecar, errors } = await open(browser, t, lang, "up");
      await page.waitForFunction(() => !document.getElementById("pmBody").hidden);

      assert.equal(await chip(page), await word(page, "Refreshed just now"));
      assert(!(await visible(page, "#pmOff")), "no off panel for a sidecar that is up");
      // a failed provider has its own card, marked as failed, and the totals
      // count it
      const cards = await page.evaluate(() =>
        [...document.querySelectorAll("#pmCards .pm-card")].map((c) => ({
          name: c.querySelector(".pm-card-name").textContent,
          status: c.querySelector(".pm-card-status").className,
          bal: c.querySelector(".pm-card-bal").textContent,
        })),
      );
      assert.deepEqual(cards.map((c) => c.name), ["Qoder", "Trae"], `one card each: ${JSON.stringify(cards)}`);
      assert.match(cards[1].status, /pm-st-error/);
      assert.ok(cards[0].bal.includes("12.5"), `the balance is drawn: ${cards[0].bal}`);
      const failed = await page.evaluate(() =>
        [...document.querySelectorAll("#pmTotals .pm-total")]
          .map((c) => [c.querySelector(".pm-total-label").textContent, c.querySelector(".pm-total-num").textContent])
          .find(([label]) => label === t("Failed")),
      );
      assert.deepEqual(failed, [await word(page, "Failed"), "1"], `the failed count is drawn: ${JSON.stringify(failed)}`);
      // the nearest lot reads as days away, to the minute, and its value in yuan
      const lot = await page.locator("#pmTimeline").textContent();
      assert.ok(lot.includes(await word(page, "{n} days left", { n: 3 })), `the lot says how soon: ${lot}`);
      assert.ok(lot.includes("¥8"), `the lot says what it is worth: ${lot}`);
      assert.match(lot, /\d{2}:\d{2}/, `the expiry is a moment, not a day: ${lot}`);
      assert.equal(await page.locator("#pmTimeline .pm-lot").first().getAttribute("class"), "pm-lot pm-lv-critical");

      // the bar and the figure under it are the same number: the share of the
      // 30 days still to wait, so the nearest lot is the shortest bar. This
      // lot expires in 3 days of 30, whatever it has left of what it was
      // granted.
      assert.equal(
        await page.locator("#pmTimeline .pm-lot-bar-fill").first().evaluate((n) => n.style.getPropertyValue("--w")),
        "10.0%",
        "the lot's bar is the time left until it expires",
      );
      // and its caption says that same share in words
      const cap = await page.locator("#pmTimeline .pm-lot-caption").first().textContent();
      assert.ok(cap.includes(await word(page, "Time left {pct}% of {n} days", { pct: "10", n: "30" })), `the caption says how long is left: ${cap}`);

      // a lot further out than the 30 days fills the bar, and says it is past
      // the horizon rather than reading as exactly 30 days
      assert.equal(
        await page.locator("#pmTimeline .pm-lot-bar-fill").nth(1).evaluate((n) => n.style.getPropertyValue("--w")),
        "100.0%",
        "a lot past the horizon fills its bar",
      );
      const far = await page.locator("#pmTimeline .pm-lot-caption").nth(1).textContent();
      assert.ok(far.includes(await word(page, "Time left: more than {n} days", { n: "30" })), `the far lot says it is past the horizon: ${far}`);

      // a card carries the day's four figures, each named
      const usage = await page.evaluate(() =>
        [...document.querySelectorAll("#pmCards .pm-card")[0].querySelectorAll(".pm-usage-cell")]
          .map((c) => [c.querySelector(".pm-usage-k").textContent, c.querySelector(".pm-usage-v").textContent]),
      );
      assert.deepEqual(
        usage.map(([k]) => k),
        [await word(page, "Today used"), await word(page, "Yesterday used"), await word(page, "Today added"), await word(page, "Yesterday added")],
        `the card names every figure: ${JSON.stringify(usage)}`,
      );
      assert.deepEqual(usage.map(([, v]) => v), ["12", "40", "5", "0"], `the figures are the day's: ${JSON.stringify(usage)}`);
      // the timeline is the page's first section, the cards the second
      assert.equal(
        await page.evaluate(() => Boolean(
          document.getElementById("pmTimeline").compareDocumentPosition(document.getElementById("pmCards")) & Node.DOCUMENT_POSITION_FOLLOWING,
        )),
        true,
        "the expiry timeline comes before the balance cards",
      );

      // Diagnostics is folded, and says where the config is once opened
      const diag = page.locator('#view-powermeter .fold[aria-controls="pmDiag"]');
      assert(await page.locator("#pmDiag").isHidden(), "diagnostics starts folded");
      await diag.click();
      assert(await visible(page, "#pmDiag"), "the fold opens it");
      const said = await page.locator("#pmDiag").textContent();
      assert.ok(said.includes("/tmp/magpie/powermeter/accounts.json"), `diagnostics names the config file: ${said}`);
      assert.ok(said.includes("up"), `diagnostics says the sidecar is up: ${said}`);

      // Refresh now asks the sidecar to read again, and the board after it is
      // an asked read (power-meter's ?force=1). The button is disabled while
      // both are on their way, so it is back once both are in.
      await page.locator("#pmReload").click();
      await page.waitForFunction(() => !document.getElementById("pmReload").disabled);
      assert.ok(sidecar.reads.includes("refresh"), `Refresh now asks the sidecar: ${sidecar.reads}`);
      assert.ok(sidecar.reads.includes("summary?force"), `and reads the board asked, not kept: ${sidecar.reads}`);
      assert.equal(await chip(page), await word(page, "Refreshed just now"));
      assert.deepEqual(errors, []);
    });

    test(`${engine} ${lang}: a sidecar that won't start is unreachable, not blank`, async (t) => {
      const browser = await launch();
      t.after(() => browser.close());
      const { page, errors } = await open(browser, t, lang, "down");
      await page.waitForFunction(() => document.getElementById("pmStatus").dataset.state === "down");

      assert.equal(await chip(page), await word(page, "The sidecar is unreachable"));
      // the chip's tooltip carries the reason, and the page still offers a way out
      assert.match(await page.locator("#pmStatus").getAttribute("title"), /server\.js is missing/);
      assert(!(await visible(page, "#pmBody")), "no board for a sidecar that isn't up");
      assert(!(await visible(page, "#pmOff")), "the off panel is for the setting, not a broken sidecar");
      assert(await visible(page, "#pmTurnOff"), "the page still offers to turn it off");
      assert.deepEqual(errors, []);
    });

    test(`${engine} ${lang}: a sidecar that is starting says starting, and asks again`, async (t) => {
      const browser = await launch();
      t.after(() => browser.close());
      const { page, sidecar, errors } = await open(browser, t, lang, "starting");
      await page.waitForFunction(() => document.getElementById("pmStatus").dataset.state === "starting");

      assert.equal(await chip(page), await word(page, "The sidecar is starting…"));
      assert(!(await visible(page, "#pmBody")), "no board while it starts");
      assert(!(await visible(page, "#pmOff")), "not the off panel either");
      // the page asks again by itself, rather than waiting on Refresh now: the
      // sidecar comes up after the page has already said "starting", and the
      // next status read finds it
      const before = sidecar.reads.filter((r) => r === "status").length;
      sidecar.state = "up";
      await page.waitForFunction(() => !document.getElementById("pmBody").hidden, null, { timeout: 8000 });
      assert.ok(sidecar.reads.filter((r) => r === "status").length > before, `the page asked again: ${sidecar.reads}`);
      assert.equal(await chip(page), await word(page, "Refreshed just now"));
      assert.deepEqual(errors, []);
    });
  }
}
