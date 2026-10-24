// Run with Node's test runner and Playwright on the module path; see README.md.
// #2916: installing the skills picked after a find could fail after more than
// six seconds, while the footer's reason disappeared after six. The reason is
// kept beside the picker, the install button stays there to try again, and the
// next attempt clears the old reason. In English and Chinese, Chromium and
// WebKit. No backend: the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const HOME = "/Users/hyperframes";
const FAILED = "context deadline exceeded (Client.Timeout or context cancellation while reading body)";
const agent = { id: "claude", name: "Claude Code", icon: "", skills: `${HOME}/.claude/skills` };
const candidate = { path: "skills/hyperframes-animation", name: "hyperframes-animation", description: "Animation skills", have: false };
const probe = { source: "heygen-com/hyperframes", kind: "github", candidates: [candidate] };
const library = (skills = []) => ({
  dir: `${HOME}/.magpie/library`, backups: `${HOME}/.magpie/backups`, home: HOME,
  agents: [agent], instructions: { agents: [] }, servers: [], skills,
  foundServers: [], projects: [], foundSkills: [],
});

function server(lang, attempts, started, release) {
  return async (route) => {
    const req = route.request(), url = new URL(req.url());
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return route.fulfill({ json: { agents: [], profiles: [], settings: { lang, theme: "light" } } });
    if (url.pathname === "/api/library") return route.fulfill({ json: library() });
    if (url.pathname === "/api/library/skills/probe") return route.fulfill({ json: probe });
    if (url.pathname === "/api/library/skills/install") {
      attempts.push(req.postDataJSON());
      if (attempts.length === 1) return route.fulfill({ status: 400, json: { error: FAILED } });
      started();
      await release.promise;
      return route.fulfill({ json: { ...library([{ name: candidate.name, description: candidate.description, kind: "github", agents: ["claude"] }]), result: { changed: ["claude"], installed: [candidate.name], problems: [] } } });
    }
    if (url.pathname === "/api/plugins") return route.fulfill({ json: { plugins: [] } });
    if (url.pathname === "/api/usage/quotas") return route.fulfill({ json: [] });
    if (url.pathname === "/api/groups") return route.fulfill({ json: { groups: [] } });
    if (url.pathname === "/api/providers") return route.fulfill({ json: { providers: [], gateway: { running: true } } });
    if (url.pathname.startsWith("/api/")) return route.fulfill({ json: {} });
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": a selected skill's install error waits for the next try", async (t) => {
    assert(["chromium", "webkit"].includes(engine), "BROWSER must be chromium or webkit");
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    const errors = [];
    t.after(async () => {
      if (errors.length) console.log(errors);
      await browser.close();
    });

    for (const lang of ["en", "zh"]) {
      await t.test(lang, async () => {
        const ctx = await browser.newContext({ viewport: { width: 900, height: 800 } });
        await ctx.addInitScript(() => { try { localStorage.setItem("magpie.libTab", "skills"); } catch {} });
        const page = await ctx.newPage();
        page.setDefaultTimeout(10000);
        page.on("pageerror", (e) => errors.push(e.message));
        const attempts = [];
        let releaseStarted;
        const started = new Promise((resolve) => { releaseStarted = resolve; });
        let releaseRetry;
        const release = new Promise((resolve) => { releaseRetry = resolve; });
        await page.route("**/*", server(lang, attempts, releaseStarted, { promise: release }));
        await page.goto("http://magpie.test/");
        await page.locator('button[data-view="library"]').click();
        const src = page.locator('#view-library input[data-lib="source"]');
        await src.fill(probe.source);
        await src.press("Enter");
        const install = page.locator("#view-library .lib-probefoot button.primary");
        await install.waitFor();
        await install.click();

        const failed = page.locator("#view-library .lib-install-error");
        await failed.waitFor();
        assert.equal((await failed.textContent()).trim(), FAILED);
        assert.equal((await page.locator("#status").textContent()).trim(), "", "the long failure is beside the picker, not the six-second footer");
        await page.waitForTimeout(6500);
        assert.equal(await failed.isVisible(), true, "the failure waits for the next operation");
        assert.equal((await failed.textContent()).trim(), FAILED);
        assert.equal(await install.isEnabled(), true, "the same Install button retries");

        await install.click();
        await started;
        assert.equal(await failed.count(), 0, "the next attempt clears the old failure");
        assert.notEqual((await page.locator("#status").textContent()).trim(), FAILED);
        releaseRetry();
        await page.locator("#view-library .lib-probe").waitFor({ state: "detached" });
        assert.equal(attempts.length, 2);
        await ctx.close();
      });
    }
    assert.deepEqual(errors, []);
  });
}
