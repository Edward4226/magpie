// Run with Node's test runner and the repository's Playwright setup.
// The hint above a subscription's accounts must name the routing's own
// rule. Smart (the default, no routing saved) picks by each account's
// quota, so the saved order only settles ties; In order is the routing
// that spends the first first. The Routing field below already says this.
// All API responses are fulfilled in memory; no server or live accounts.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");
const assets = path.resolve(__dirname, "../assets");

const codex = (routing) => ({
  id: "codex", name: "Codex", icon: "openai", chat: "", responses: "", anthropic: "", catalog: "",
  models: [], agents: [], fallback: [], headers: {}, keyList: [], ...(routing ? { routing } : {}),
  account: {
    agent: "codex", agentName: "Codex", user: "one@example.com", plan: "Plus",
    logins: [
      { user: "one@example.com", plan: "Plus", active: true, on: true },
      { user: "two@example.com", plan: "Pro", on: true },
    ],
  },
});
const antigravity = {
  id: "antigravity", name: "Antigravity", icon: "antigravity-color", chat: "", responses: "", anthropic: "", catalog: "",
  models: [], agents: [], fallback: [], headers: {}, keyList: [],
  account: {
    agent: "antigravity", agentName: "Antigravity", user: "one@example.com", plan: "Pro",
    logins: [
      { user: "one@example.com", plan: "Pro", active: true, on: true },
      { user: "two@example.com", plan: "Pro", on: true },
    ],
  },
};

function serve(lang, providers) {
  const state = { agents: [], profiles: [], settings: { lang, theme: "light" } };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs={lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window={};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/providers") return json({ providers, presets: [], excluded: [], gateway: { running: false, window: true } });
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/login/usage") return json({});
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

// The hint under the Accounts field, not the Routing field's or the
// editor's other hints.
const accountsHint = (page) => page.locator(".editor .accts").first().evaluate((el) => {
  const fld = el.parentElement;
  return [...fld.children].filter((x) => x.classList.contains("expl")).map((x) => x.textContent.trim()).join(" ");
});

const words = {
  en: {
    inOrder: "Tick every account to use; Routing says how requests spread over them.",
    stale: "The gateway uses the first",
    out: "out of quota",
    routingSmart: /quota renews soonest/,
    routingOrder: /Requests go to the first/,
    sessions: "Sessions already running keep theirs until restarted.",
    own: "Only requests through magpie use them; Antigravity itself stays signed in as it is.",
  },
  zh: {
    inOrder: "勾选要使用的账号；请求如何分配由「路由」决定。",
    stale: "网关优先用第一个账号",
    out: "额度用完",
    routingSmart: /额度最先重置/,
    routingOrder: /请求先发给第一个/,
    sessions: "已在运行的会话保持原账号，重启后切换。",
    own: "只有经过 magpie 的请求会用这些账号；Antigravity 本身的登录不变。",
  },
};
const smartRule = {
  en: [
    /The gateway picks the account for each request by its quota; the order only settles ties\./,
    /requests through magpie go by each account's quota, so the order only settles ties\./,
  ],
  zh: [
    /网关按每个账号的额度为每次请求挑选账号，顺序只在条件相同时起作用。/,
    /经 magpie 的请求按每个账号的额度挑选，顺序只在条件相同时起作用。/,
  ],
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of Object.keys(words)) {
    const w = words[lang];
    test(`${engine} ${lang}: the Accounts hint tells Smart from In order`, { timeout: 90000 }, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(() => browser.close());
      const page = await (await browser.newContext({ viewport: { width: 1000, height: 850 }, reducedMotion: "reduce" })).newPage();
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      const errors = [];
      page.on("pageerror", (e) => errors.push(e.message));

      for (const [routing, provider, name] of [["", codex(""), "Codex"], ["order", codex("order"), "Codex"], ["", antigravity, "Antigravity"]]) {
        await page.unroute("**/*").catch(() => {});
        await page.route("**/*", serve(lang, [provider]));
        await page.goto("http://magpie.test/?view=providers");
        await page.locator(".row.provider", { hasText: name }).first().click();
        await page.locator(".editor .acc").first().waitFor();
        const note = await accountsHint(page);
        const hints = await page.locator(".editor .hint.expl").allTextContents();
        const routingHint = hints.find((x) => w.routingSmart.test(x) || w.routingOrder.test(x)) || "";
        if (routing === "") {
          assert.ok(smartRule[lang].some((re) => re.test(note)), `${name} ${lang}: Smart's Accounts hint is not the quota rule:\n${note}`);
          assert.ok(!note.includes(w.stale), `${name} ${lang}: Smart's Accounts hint still claims the first takes requests:\n${note}`);
          assert.ok(!note.includes(w.out), `${name} ${lang}: Smart's Accounts hint still says out of quota:\n${note}`);
          assert.match(routingHint, w.routingSmart, `${name} ${lang}: the Routing field doesn't explain Smart`);
        } else {
          assert.ok(note.includes(w.inOrder), `${name} ${lang}: In order's Accounts hint is not the routing note:\n${note}`);
          assert.ok(smartRule[lang].every((re) => !re.test(note)), `${name} ${lang}: In order's Accounts hint borrowed Smart's rule:\n${note}`);
          assert.match(routingHint, w.routingOrder, `${name} ${lang}: the Routing field doesn't explain In order`);
        }
        if (name === "Codex" && routing === "") assert.ok(note.includes(w.sessions), `${name} ${lang}: Smart's hint dropped the running-session caveat:\n${note}`);
        if (name === "Antigravity") assert.ok(note.includes(w.own), `${name} ${lang}: the own-sign-in caveat is missing:\n${note}`);
      }
      assert.deepEqual(errors, []);
    });
  }
}
