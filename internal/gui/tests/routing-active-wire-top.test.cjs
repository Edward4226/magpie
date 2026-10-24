// Run with Node's test runner and Playwright on the module path; see README.md.
// Several candidate rows for one model share most of the way from magpie.
// SVG paints in document order, so the later rows' grey wires used to cover
// the live or lit wire of the active row. The request shown must stay on
// top from its agent into magpie and out to the row, in the agent's colour.
// A completed source wire stays lit but still; only one in flight flows.
// No backend, the API is faked here; reduced motion is on.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const now = Date.now();
const iso = (ms) => new Date(ms).toISOString();
const M = "deepseek-v4.1-flash";
const order = [
  { id: "alpha", provider: "alpha", name: "Alpha", kind: "provider", model: M },
  { id: "beta", provider: "beta", name: "Beta", kind: "provider", model: M },
  { id: "gamma", provider: "gamma", name: "Gamma", kind: "provider", model: M },
];
const first = {
  id: 1, seq: 1, time: iso(now - 10e3), agent: "codex", model: M, provider: "beta",
  order, tries: [{ id: "beta", model: M, start: iso(now - 10e3), done: true, status: 200, ms: 800 }],
  done: true, status: 200, ms: 800,
};

function serve(live) {
  return async (request) => {
    const url = new URL(request.request().url());
    const json = (data) => request.fulfill({ json: data });
    if (url.pathname === "/boot.js") return request.fulfill({ contentType: "text/javascript", body: 'window.bootPrefs = {lang:"en",theme:"light",web:true};' });
    if (url.pathname === "/wails/runtime.js") return request.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [{ id: "codex", name: "Codex", path: "/test/config.toml", fields: [] }], profiles: [], settings: { lang: "en", theme: "light" } });
    if (url.pathname === "/api/gateway/trace") {
      if (url.searchParams.get("wait")) {
        if (live.queued.length) return json(live.queued.shift());
        live.pending.push(request);
        return;
      }
      return json({ mine: true, now: new Date().toISOString(), seq: 1, totals: { requests: 1, rerouted: 0, errors: 0 }, routes: [first] });
    }
    if (url.pathname === "/api/gateway/history") return json({ cut: false, days: [], routes: [] });
    if (url.pathname === "/api/groups") return json({ groups: [], models: [], pools: [] });
    if (url.pathname === "/api/providers") return json({ providers: [], excluded: [], gateway: { running: true, window: true } });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await request.fulfill({ body: await fs.readFile(file), contentType });
  };
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(`${engine}: the active route's wires paint over every overlapping idle wire`, async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    const page = await browser.newPage({ viewport: { width: 1100, height: 900 }, reducedMotion: "reduce" });
    page.setDefaultTimeout(5000);
    const errors = [];
    page.on("pageerror", (error) => errors.push(error.message));
    const live = { pending: [], queued: [], seq: 1 };
    await page.route("**/*", serve(live));
    const send = (route) => {
      const data = { mine: true, now: new Date().toISOString(), seq: ++live.seq, totals: { requests: live.seq }, routes: [route] };
      const waiting = live.pending.shift();
      if (waiting) waiting.fulfill({ json: data }); else live.queued.push(data);
    };
    const stage = () => page.evaluate(() => {
      const ps = [...document.querySelectorAll(".rt-wires path[d]")];
      const at = (p) => p.getPointAtLength(Math.min(2, p.getTotalLength()));
      const active = ps.filter((p) => p.classList.contains("live") || p.classList.contains("lit") || p.classList.contains("trail"));
      const idle = ps.filter((p) => !active.includes(p));
      const lit = ps.filter((p) => p.classList.contains("lit"));
      const flowing = ps.filter((p) => p.classList.contains("live"));
      const trail = ps.filter((p) => p.classList.contains("trail"));
      const ai = active.map((p) => ps.indexOf(p)), ii = idle.map((p) => ps.indexOf(p));
      const overlap = lit.length > 0 && idle.some((p) => {
        const a = at(lit[0]), b = at(p);
        return Math.hypot(a.x - b.x, a.y - b.y) < 1;
      });
      return {
        count: ps.length, lit: lit.length, live: flowing.length, trail: trail.length, overlap,
        onTop: active.length > 0 && ii.length > 0 && Math.min(...ai) > Math.max(...ii),
        hues: [...new Set(active.map((p) => p.style.getPropertyValue("--agent").trim()))],
        strokes: [...new Set(active.map((p) => getComputedStyle(p).stroke))],
        idleStrokes: [...new Set(idle.map((p) => getComputedStyle(p).stroke))],
        animations: [...new Set(active.map((p) => getComputedStyle(p).animationName))],
      };
    });

    await page.goto("http://magpie.test/?view=routing");
    await page.waitForFunction(() => document.querySelectorAll(".rt-accts > li").length === 3);
    await page.locator(".rt-wires path.lit[d]").waitFor({ state: "attached" });
    // The active row is the second of the three alike rows, with Gamma made
    // after it. All three wires start at magpie's same point.
    assert.deepEqual(await page.locator(".rt-accts > li").evaluateAll((lis) => lis.map((li) => li.title)), ["alpha", "beta", "gamma"]);
    let s = await stage();
    assert.equal(s.count, 4, "one wire from the agent and three to the rows");
    assert.deepEqual([s.lit, s.live, s.trail], [1, 0, 1], "the row and completed source are lit, nothing flows");
    assert(s.overlap, "the active row's wire overlaps an idle row's wire");
    assert(s.onTop, "every active wire is after every idle wire in SVG paint order");
    assert.deepEqual(s.hues, ["#6366f1"]);
    assert.equal(s.strokes.length, 1, "the active wires are one colour");
    assert(!s.idleStrokes.includes(s.strokes[0]), "the active colour is not the idle wire's grey");
    assert.deepEqual(s.animations, ["none"], "reduced motion leaves the completed route still");

    // The same request comes in again, now in flight: both its wires flow.
    const going = {
      ...first, seq: 2, done: false, status: 0, ms: 0,
      tries: [{ id: "beta", model: M, start: iso(Date.now()), done: false }],
    };
    send(going);
    await page.locator(".rt-accts > li[title='beta'].now").waitFor();
    await page.waitForFunction(() => document.querySelectorAll(".rt-wires path.live[d]").length === 2);
    s = await stage();
    assert.deepEqual([s.lit, s.live, s.trail], [0, 2, 0], "only an in-flight route uses live wires");
    assert(s.onTop);
    assert.deepEqual(s.hues, ["#6366f1"]);

    // Answered, it returns to one solid colour without losing either end.
    const done = {
      ...first, seq: 3,
      tries: [{ id: "beta", model: M, start: iso(Date.now() - 1e3), done: true, status: 200, ms: 800 }],
    };
    send(done);
    await page.waitForFunction(() => document.querySelectorAll(".rt-wires path.live[d]").length === 0);
    s = await stage();
    assert.deepEqual([s.lit, s.live, s.trail], [1, 0, 1]);
    assert(s.onTop);
    assert.deepEqual(errors, []);
  });
}
