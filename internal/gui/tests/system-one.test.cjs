// Run with Node's test runner and Playwright on the module path; see README.md.
// Bailian's decision model (#647): its editor picks a workspace's region or
// the Token Plan, asks for the workspace ID and fills the System One
// address in with it; Add without one says so and sends nothing. A custom
// provider can be a System One API: Detect APIs posts its address as
// decide, and the Add sends it. A mixed provider such as OpenCode Zen
// says the field is the base for routing groups' classifier alone.
// Every language; the API is faked.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const ws = (r) => `https://{WorkspaceId}.${r}.maas.aliyuncs.com/compatible-mode/v1`;
const presets = [
  { id: "typesafe", name: "TypeSafe Jev", icon: "typesafe", kind: "vendor", decide: "https://api.typesafe.ai/v1", note: "routes groups · picks model and effort", added: false },
  { id: "opencode-zen", name: "OpenCode Zen", icon: "opencode", kind: "vendor", chat: "https://opencode.ai/zen/v1", decide: "https://opencode.ai/zen/v1", note: "chats and routes groups", added: false },
  { id: "bailian-decision", name: "Bailian Decision Model", icon: "qwen-color", kind: "vendor", decide: ws("cn-beijing"), note: "routes groups · picks model and effort", added: false,
    regionLabel: "Plan", regions: [
      { id: "cn-beijing", name: "Workspace · Beijing", decide: ws("cn-beijing") },
      { id: "ap-southeast-1", name: "Workspace · Singapore", decide: ws("ap-southeast-1") },
      { id: "token-plan", name: "Token Plan · Beijing", decide: "https://token-plan.cn-beijing.maas.aliyuncs.com/compatible-mode/v1" },
    ] },
];

const providers = [{ id: "openrouter", name: "OpenRouter", icon: "openai", preset: "openrouter", models: [], agents: [], key: { set: true, masked: "sk-…ab12" } }];

function server(lang, posted) {
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    const all = { providers, presets, excluded: [], gateway: { running: true, window: true } };
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/providers") return json(all);
    if (url.pathname.startsWith("/api/provider/")) {
      const body = route.request().postDataJSON();
      const action = url.pathname.slice("/api/provider/".length);
      posted.push({ action, body });
      if (action === "detect") return json({ results: [{ protocol: "decide", ok: true, status: 200, ms: 40, model: body.model, base: body.decide.replace(/\/systemone$/, "") }] });
      return json(all);
    }
    if (url.pathname === "/api/groups") return json({ groups: [], pools: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

const L = {
  en: { ws: "Workspace ID", ep: "System One endpoint", sg: "Workspace · Singapore", tp: "Token Plan · Beijing", add: "Add", needed: "Give the workspace ID your API key belongs to, or pick the Token Plan", custom: "Custom provider", detect: "Detect APIs", use: "Use these",
    zen: "Only a routing group's classifier uses this. Enter the base address: magpie sends the decision request to …/systemone beneath it. Ordinary conversations don't use it; leave the default for them." },
  zh: { ws: "业务空间 ID", ep: "System One 端点", sg: "业务空间 · 新加坡", tp: "Token Plan · 北京", add: "添加", needed: "请填写 API Key 所属的业务空间 ID，或选择 Token Plan", custom: "自定义供应商", detect: "检测协议", use: null,
    zen: "只有路由组用作分类的模型会使用这里。请填写基础地址，magpie 会把决策请求发到其下的 …/systemone。普通对话不会使用它，保持默认即可。" },
  "zh-TW": { ws: "業務空間 ID", ep: "System One 端點", sg: "業務空間 · 新加坡", tp: "Token Plan · 北京", add: "新增", needed: "請填寫 API Key 所屬的業務空間 ID，或選擇 Token Plan", custom: "自訂供應商", detect: "檢測協議", use: null,
    zen: "只有路由組用來分類的模型會使用這裡。請填寫基礎位址，magpie 會把決策請求傳送到其下的 …/systemone。一般對話不會使用它，保留預設值即可。" },
  ja: { ws: "ワークスペース ID", ep: "System One エンドポイント", sg: "ワークスペース · シンガポール", tp: "Token Plan · 北京", add: "追加", needed: "API キーが属するワークスペース ID を入力するか、Token Plan を選んでください", custom: "カスタムプロバイダー", detect: "API を検出", use: null,
    zen: "ルーティンググループの分類モデルだけがこれを使います。ベースアドレスを入力すると、magpie はその下の …/systemone に判定リクエストを送ります。通常の会話では使わないため、既定値のままで構いません。" },
  de: { ws: "Workspace-ID", ep: "System-One-Endpunkt", sg: "Workspace · Singapur", tp: "Token Plan · Peking", add: "Hinzufügen", needed: "Geben Sie die Workspace-ID an, zu der Ihr API-Schlüssel gehört, oder wählen Sie den Token Plan", custom: "Benutzerdefinierter Anbieter", detect: "APIs erkennen", use: null,
    zen: "Nur der Klassifikator einer Weiterleitungsgruppe verwendet dies. Geben Sie die Stammadresse ein; magpie sendet die Entscheidungsanfrage an …/systemone darunter. Normale Unterhaltungen verwenden sie nicht, daher können Sie den Standardwert beibehalten." },
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": Bailian's decision model and a custom System One", async (t) => {
    assert(["chromium", "webkit"].includes(engine), "BROWSER must be chromium or webkit");
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
      await t.test(lang, async () => {
        const w = L[lang];
        const posted = [];
        const saves = () => posted.filter((x) => x.action === "save").map((x) => x.body);
        const page = await (await browser.newContext({ viewport: { width: 900, height: 760 } })).newPage();
        page.setDefaultTimeout(5000);
        const errors = [];
        // WebKit reports a ResizeObserver round left to the next frame as a
        // page error; it is not an error of this page.
        page.on("pageerror", (e) => { if (!/^ResizeObserver loop completed with undelivered notifications/.test(e.message)) errors.push(e.message); });
        await page.route("**/*", server(lang, posted));
        await page.goto("http://magpie.test/?view=providers");
        const sheet = page.locator("#addSheet");
        const ed = page.locator(".editor.new");
        const open = async (name) => {
          await page.locator("#addProvider").click();
          await sheet.locator(".tile .n", { hasText: new RegExp("^" + name + "$") }).click();
          await ed.locator(".ehead b", { hasText: name }).waitFor();
        };

        // the workspace: asked for, and the address filled in with it
        await open("Bailian Decision Model");
        const wsIn = ed.locator("input.workspace-id");
        assert.equal(await wsIn.isVisible(), true);
        assert.equal(await ed.locator("label", { hasText: new RegExp("^" + w.ws + "$") }).count(), 1);
        const epLabel = ed.locator("label", { hasText: new RegExp("^" + w.ep + "$") });
        assert.equal(await epLabel.count(), 1);
        const ep = ed.locator("input[type=url]").last();
        assert.equal(await ep.inputValue(), ws("cn-beijing"));
        await ed.locator("input[type=password]").first().fill("sk-test");
        await ed.locator(".bar button.primary", { hasText: w.add }).click();
        assert.equal(await ed.locator(".editor-error").textContent(), w.needed);
        assert.equal(saves().length, 0);
        await wsIn.fill("ws-abc");
        assert.equal(await ep.inputValue(), "https://ws-abc.cn-beijing.maas.aliyuncs.com/compatible-mode/v1");
        await ed.locator(".segs.regions .opt", { hasText: w.sg }).click();
        assert.equal(await ep.inputValue(), "https://ws-abc.ap-southeast-1.maas.aliyuncs.com/compatible-mode/v1");
        await ed.locator(".bar button.primary", { hasText: w.add }).click();
        await page.waitForFunction(() => !document.querySelector(".editor.new"));
        assert.equal(saves().length, 1);
        assert.equal(saves()[0].preset, "bailian-decision");
        assert.equal(saves()[0].key, "sk-test");
        assert.equal(saves()[0].decide, "https://ws-abc.ap-southeast-1.maas.aliyuncs.com/compatible-mode/v1");

        // the Token Plan has no workspace
        await open("Bailian Decision Model");
        await ed.locator(".segs.regions .opt", { hasText: w.tp }).click();
        assert.equal(await ed.locator("input.workspace-id").isVisible(), false);
        assert.equal(await ed.locator("input[type=url]").last().inputValue(), "https://token-plan.cn-beijing.maas.aliyuncs.com/compatible-mode/v1");
        await ed.locator("input[type=password]").first().fill("sk-sp-test");
        await ed.locator(".bar button.primary", { hasText: w.add }).click();
        await page.waitForFunction(() => !document.querySelector(".editor.new"));
        assert.equal(saves()[1].decide, "https://token-plan.cn-beijing.maas.aliyuncs.com/compatible-mode/v1");

        // Jev's preset keeps its own field and no workspace
        await open("TypeSafe Jev");
        assert.equal(await ed.locator("input.workspace-id").isVisible(), false);
        assert.equal(await ed.locator("label", { hasText: lang === "en" ? /^Jev endpoint$/ : lang === "de" ? /^Jev-Endpunkt$/ : lang === "ja" ? /^Jev エンドポイント$/ : lang === "zh-TW" ? /^Jev 端點$/ : /^Jev 端点$/ }).count(), 1);

        // a mixed provider keeps the generic name, and says what the base
        // address is for and that ordinary chats can keep the default
        await page.goto("http://magpie.test/?view=providers");
        await open("OpenCode Zen");
        assert.equal(await ed.locator("label", { hasText: new RegExp("^" + w.ep + "$") }).count(), 1);
        const epIn = ed.locator("input.decide-url");
        assert.equal(await epIn.inputValue(), "https://opencode.ai/zen/v1");
        const help = await epIn.getAttribute("aria-describedby");
        assert.equal(await page.locator("#" + help).textContent(), w.zen);

        // a custom System One: probed at its address, saved as decide
        await page.goto("http://magpie.test/?view=providers");
        await page.locator("#addProvider").click();
        await sheet.locator(".custom-foot .custom").click();
        await ed.waitFor();
        await ed.locator("input").first().fill("My Decider");
        await ed.locator(".segs .opt", { hasText: "System One" }).click();
        await ed.locator("input[type=url]").first().fill("https://decide.example.com/v1/systemone");
        await ed.locator("input[type=password]").first().fill("sk-1");
        await ed.locator("input.detect-model").fill("my-decider");
        await ed.locator(".detect button", { hasText: new RegExp("^" + w.detect + "$") }).click();
        await ed.locator(".detect-out .ep[data-api=decide] .res.ok").waitFor();
        const d = posted.find((x) => x.action === "detect").body;
        assert.equal(d.decide, "https://decide.example.com/v1/systemone");
        assert.equal(d.model, "my-decider");
        await ed.locator(".detect-acts button").first().click();
        assert.equal(await ed.locator("input[type=url]").first().inputValue(), "https://decide.example.com/v1");
        await ed.locator(".bar button.primary", { hasText: w.add }).click();
        await page.waitForFunction(() => !document.querySelector(".editor.new"));
        const c = saves()[2];
        assert.equal(c.name, "My Decider");
        assert.equal(c.decide, "https://decide.example.com/v1");
        assert.equal(c.chat, "");
        assert.deepEqual(errors, []);
        await page.context().close();
      });
    }
  });
}
