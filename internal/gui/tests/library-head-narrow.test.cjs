// Run with Node's test runner and Playwright on the module path; see README.md.
// #2625 (linusxiong): beside the sidebar the Library's title bar says where
// the reader is ("Work / Library / MCP servers") and carries the page's
// tools. A narrow window left the tools less than their labels need, so the
// first of them (Discover) ran back over the crumb — "MCP 服务器现". They take
// the room that is there and their labels ellipsize now; the crumb keeps its
// side of the title bar and nothing runs out of it. Chromium and WebKit, a
// narrow and a wide window, English and Chinese; no backend, the API is
// faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const HOME = "/Users/linus";
const agent = (id, name) => ({ id, name, icon: "", skills: `${HOME}/.${id}/skills`, mcp: `${HOME}/.${id}/mcp.json` });
const servers = Array.from({ length: 8 }, (_, i) => ({ name: `server-${i + 1}`, transport: "stdio", command: "npx", args: ["fs-mcp"], agents: ["codex"] }));

function serve(lang) {
  const view = {
    dir: `${HOME}/.magpie/library`, backups: `${HOME}/.magpie/backups`, home: HOME,
    agents: [agent("codex", "Codex")],
    instructions: { agents: [], sets: [] }, servers, foundServers: [], projects: [], foundSkills: [], skills: [],
  };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:${JSON.stringify(lang)},theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/library") return json(view);
    if (url.pathname === "/api/library/market/servers" || url.pathname === "/api/library/market/skills") return json({ items: [] });
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/usage/quotas") return json([]);
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/providers") return json({ providers: [], gateway: { running: true } });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    const body = await fs.readFile(file).catch(() => null);
    await (body ? route.fulfill({ body, contentType }) : route.fulfill({ status: 404, body: "" }));
  };
}

const words = { en: { crumb: "MCP servers", discover: "Discover" }, zh: { crumb: "MCP 服务器", discover: "发现" } };

// the crumb's right edge against Discover's left, on the one title-bar line,
// and whether anything in the strip runs past its own right edge
const bar = (page) => page.evaluate(() => {
  const crumb = document.querySelector("#crumb"), head = document.querySelector("#view-library .lib-head"), go = head.querySelector(".lib-discover");
  const c = crumb.getBoundingClientRect(), h = head.getBoundingClientRect(), g = go.getBoundingClientRect();
  const outside = [...head.querySelectorAll("*")].filter((e) => e.getClientRects().length)
    .map((e) => e.getBoundingClientRect()).filter((r) => r.right > h.right + 1).length;
  return {
    crumbRight: c.right, discoverLeft: g.left,
    sameRow: Math.min(c.bottom, g.bottom) - Math.max(c.top, g.top) > 0,
    outside,
  };
});

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    test(`${engine} ${lang}: the Library's title bar keeps the crumb clear of Discover (#2625)`, async (t) => {
      assert(["chromium", "webkit"].includes(engine), "BROWSER must be chromium or webkit");
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(() => browser.close());
      for (const width of [560, 650, 1400]) await t.test(`${width}px`, async () => {
        const context = await browser.newContext({ viewport: { width, height: 800 }, reducedMotion: "reduce" });
        const page = await context.newPage();
        page.setDefaultTimeout(5000);
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", serve(lang));
        await page.addInitScript(() => { try { localStorage.setItem("magpie.libTab", "mcp"); localStorage.setItem("magpie.navLayout", "side"); } catch {} });
        await page.goto("http://magpie.test/?view=library");
        await page.waitForSelector("#nav button.on");
        assert.ok(await page.evaluate(() => document.body.classList.contains("side-nav")), "the sidebar is shown");
        // the crumb is filled with the page the reader is on, and app.js has
        // measured it, setting the room the strip's tools may have
        await page.locator("#crumb b").filter({ hasText: words[lang].crumb }).waitFor();
        await page.locator("#view-library .lib-discover").waitFor();
        await page.waitForFunction(() => document.documentElement.style.getPropertyValue("--tools-l") !== "");
        // the room the tools get and the crumb's width are measured off each
        // other (app.js), so they land over a frame or two; read them once
        // two polls in a row agree rather than in the middle of that
        await page.waitForFunction(() => {
          const crumb = document.querySelector("#crumb"), go = document.querySelector("#view-library .lib-discover");
          const held = window.__headAt
            && Math.abs(crumb.getBoundingClientRect().right - window.__headAt[0]) < 0.5
            && Math.abs(go.getBoundingClientRect().left - window.__headAt[1]) < 0.5;
          window.__headAt = [crumb.getBoundingClientRect().right, go.getBoundingClientRect().left];
          return held;
        });
        const r = await bar(page);
        assert.ok(r.sameRow, `Discover is on the title bar's line: ${JSON.stringify(r)}`);
        // the crumb stops before Discover: no "MCP 服务器现" any more
        assert.ok(r.discoverLeft >= r.crumbRight - 0.5, `the crumb runs under Discover: crumb.right ${r.crumbRight} > Discover.left ${r.discoverLeft}`);
        // and nothing in the strip runs out of its right edge
        assert.equal(r.outside, 0, `something runs out of the strip: ${JSON.stringify(r)}`);
        assert.deepEqual(errors, []);
        await context.close();
      });
    });
  }
}
