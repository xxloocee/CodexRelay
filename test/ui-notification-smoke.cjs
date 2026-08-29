/*
 * @Author        : 顾青离
 * @Url            : sucaijun.com
 * @Email          : Ricky@LiHai.La
 * @Project        : CodexRelay
 * @Description    : 公告、工具栏、加载反馈和弹窗的本地界面冒烟测试
 * @File           : Playwright 前端脱敏状态检查
 * @Read me        : 感谢使用 CodexRelay，源码注释齐全，支持二次开发。
 * @Remind         : 二次开发请保留原版权信息，谢谢。
 */
const fs = require("fs");
const http = require("http");
const path = require("path");
const { chromium } = require("playwright");

const projectRoot = path.resolve(__dirname, "..");
const frontendRoot = path.join(projectRoot, "frontend");
const state = {
  version: "0.5.0",
  needsOnboarding: false,
  proxyPort: 8765,
  listenOnAllInterfaces: false,
  proxyUrl: "http://127.0.0.1:8765/codex",
  proxyUrls: { codex: "http://127.0.0.1:8765/codex" },
  localAccessToken: "sk-local-placeholder",
  dataDirectory: "C:\\Users\\Ricky-Desktop\\.CodexRelay",
  activeProfiles: {},
  profiles: [],
  clientConfigs: [
    { category: "codex", label: "Codex", configDir: "C:\\Users\\Ricky-Desktop\\.codex", configFile: "config.toml", skipConfigReplacement: false, status: "not_detected", statusText: "未检测到配置" },
    { category: "claude", label: "Claude", configDir: "C:\\Users\\Ricky-Desktop\\.claude", configFile: "settings.json", skipConfigReplacement: true, status: "not_detected", statusText: "未检测到配置" },
  ],
  network: { mode: "direct", proxyUrl: "" },
  systemProxy: { enabled: false, note: "" },
  requests: [],
  usage: { updatedAt: "", total: {}, profiles: {}, days: {} },
  uptimeSeconds: 1,
  tokenSwitch: { mode: "prompt", loop: true, trigger401: true, trigger403: true, trigger5xx: true, triggerNetwork: true, triggerDirectoryInvalid: true, triggerDirectoryMissing: true, authFailureThreshold: 5, upstreamFailureThreshold: 5, upstreamFailureWindowMinutes: 3 },
  preferences: { closeToTray: true, launchAtStartup: false, startHidden: false, defaultSource: "", defaultCategory: "codex", restoreViewMode: "current" },
  taskNotification: { enabled: true, webhookUrl: "https://notify.example.test/webhook", events: { taskCompleted: true, taskAborted: true, tokenRequestFailed: true, tokenAutoSwitched: true, tokenAutoSwitchFailed: true, accountBalanceLow: true, subscriptionBalanceLow: true }, idleGraceSeconds: 5, requestTimeoutSeconds: 10, maxAttempts: 0, status: { enabled: true, pending: 1, outbox: 2, dead: 0, lastError: "" } },
  doge: {
    bound: false,
    walletUsd: 0,
    subscriptionsUsd: 0,
    totalUsd: 0,
    subscriptions: [],
    redemptionEnabled: false,
    topupLink: "",
    user: {},
    account: { userId: 7, nickname: "测试用户", email: "user@example.test", balanceUsd: 3, usedUsd: 0.5, requestCount: 12 },
    groups: [],
    tokens: [],
    notifications: {
      initialized: true,
      enabled: true,
      currentNotice: "# 当前公告\n\n**当前公告内容**\n\n[官方网站](https://example.com) [危险链接](javascript:alert(1))",
      announcements: [{ id: 11, content: "## 历史公告\n\n**历史公告内容**", publishDate: "2026-08-22T00:00:00Z", type: "default", read: false }],
      unreadCount: 1,
      alerts: [],
      lastSyncAt: "2026-08-22T00:00:00Z",
      lastSyncError: "",
    },
    syncIntervalMinutes: 3,
    lastSyncAt: "",
    lastSyncError: "",
    tokenSwitches: {},
  },
};
state.doge.notifications.alerts = [{ kind: "announcement", key: "announcement:11", title: "新的系统公告", message: "平台发布了新的公告", announcementId: 11 }];

function contentType(filePath) {
  return filePath.endsWith(".html") ? "text/html" : filePath.endsWith(".css") ? "text/css" : filePath.endsWith(".js") ? "text/javascript" : filePath.endsWith(".svg") ? "image/svg+xml" : "application/octet-stream";
}

const server = http.createServer((request, response) => {
  let requestPath = decodeURIComponent((request.url || "/").split("?")[0]);
  if (requestPath === "/wails/runtime.js") {
    response.writeHead(200, { "Content-Type": "text/javascript" });
  response.end(`export const CancellablePromise = Promise; export const Create = { Any: x => x, Array: f => xs => (xs || []).map(f), Map: (keyFn, valueFn) => x => Object.fromEntries(Object.entries(x || {}).map(([key, value]) => [keyFn(key), valueFn(value)])), Nullable: f => x => x == null ? null : f(x), }; export const Events = { On: (name, callback) => { globalThis.__wailsEvents = globalThis.__wailsEvents || {}; globalThis.__wailsEvents[name] = callback; }, }; const wait = ms => new Promise(resolve => setTimeout(resolve, ms)); export const Call = { ByID: async (id, ...args) => { if (id === 3062805628) return globalThis.__relayState; if (id === 2130459618) return { ok: false, status: 503, durationMs: 12 }; if (id === 2419338323) { if (globalThis.__openExternalURLShouldFail) throw new Error("默认浏览器打开失败"); globalThis.__openedExternalURL = args[0]; return; } if (id === 1536866851 || id === 2451788025 || id === 1654359792) { await wait(450); return; } if (id === 215903019) { globalThis.__relayState.doge.notifications.unreadCount = 0; return; } if (id === 93543395) { globalThis.__relayState.doge.tokenSwitch = null; if (globalThis.__relayState.doge.tokenSwitches) for (const [category, prompt] of Object.entries(globalThis.__relayState.doge.tokenSwitches)) if (prompt?.key === args[0]) delete globalThis.__relayState.doge.tokenSwitches[category]; return; } if (id === 3647092161) { globalThis.__relayState.doge.tokenSwitch = null; return; } if (id === 3707202435) { globalThis.__relayState.needsOnboarding = false; return; } if (id === 1746439210) { globalThis.__relayState.proxyPort = args[0]; return; } if (id === 4273331261) { globalThis.__relayState.listenOnAllInterfaces = args[0]; return; } if (id === 3841417432) { globalThis.__relayState.doge.syncIntervalMinutes = args[0]; return; } if (id === 3757833444) { const [category, ids] = args; globalThis.__lastFailoverOrderArgs = { category, ids: [...ids] }; globalThis.__relayState.failoverOrder[category] = [...ids]; return; } if (id === 1862834343) { const profile = globalThis.__relayState.profiles.find(item => item.id === args[0]); if (profile) profile.skipAutoSwitch = !args[1]; return; } if (id === 1477342492) return (args[0] || "C:\\\\Users\\\\Ricky-Desktop\\\\.CodexRelay") + "\\\\picked"; if (id === 2510123141) { const client = globalThis.__relayState.clientConfigs.find(item => item.category === args[0]); if (client) client.configDir = args[1]; return; } if (id === 2542801733) { const client = globalThis.__relayState.clientConfigs.find(item => item.category === args[0]); if (client) client.skipConfigReplacement = args[1]; return; } if (id === 3032635246) { globalThis.__relayState.dataDirectory = args[0]; return; } if (id === 3515011149) { Object.assign(globalThis.__relayState.taskNotification, args[0]); return; } if (id === 2021288413) { Object.assign(globalThis.__relayState.tokenSwitch, args[0]); return; } if (id === 2204274041) { const input = args[0]; Object.assign(globalThis.__relayState.doge, { balanceAlertEnabled: input.balanceEnabled, balanceAlertThresholdUsd: input.balanceThresholdUsd, subscriptionAlertEnabled: input.subscriptionEnabled, subscriptionAlertThresholdUsd: input.subscriptionThresholdUsd }); return; } if (id === 1947134911) return; return; } };`);
    return;
  }
  const filePath = requestPath === "/logo.png" ? path.join(projectRoot, "logo.png") : path.join(frontendRoot, requestPath === "/" ? "index.html" : requestPath.slice(1));
  if (!filePath.startsWith(frontendRoot) && filePath !== path.join(projectRoot, "logo.png")) {
    response.writeHead(403); response.end(); return;
  }
  if (!fs.existsSync(filePath)) { response.writeHead(404); response.end(); return; }
  response.writeHead(200, { "Content-Type": contentType(filePath) });
  fs.createReadStream(filePath).pipe(response);
});

(async () => {
  await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
  const address = server.address();
  const browser = await chromium.launch({ headless: true, executablePath: "C:\\Program Files (x86)\\Microsoft\\Edge\\Application\\msedge.exe" });
  try {
    const page = await browser.newPage({ viewport: { width: 1170, height: 724 }, deviceScaleFactor: 1 });
    await page.addInitScript((value) => { globalThis.__relayState = value; }, state);
    const errors = [];
    page.on("pageerror", (error) => errors.push(error.message));
    await page.goto(`http://127.0.0.1:${address.port}/`, { waitUntil: "networkidle" });
    await page.evaluate(() => {
      globalThis.__copiedText = "";
      Object.defineProperty(navigator, "clipboard", { configurable: true, value: { writeText: async (text) => { globalThis.__copiedText = text; } } });
    });
    const toolbar = await page.locator(".toolbar-actions > *").evaluateAll((items) => items.map((item) => item.id || item.className));
    const expected = ["pendingDogeImport", "dogeQuotaWrap", "openDogeTopup", "announcementWrap", "refreshDoge", "openSettings", "addProfile"];
    if (toolbar.join() !== expected.join()) throw new Error(`工具栏顺序错误: ${toolbar.join()}`);
    const activeSummary = await page.evaluate(() => {
      const countLine = document.querySelector(".active-count-line");
      const mode = document.querySelector("#failoverStatus");
      return { text: mode.textContent, countTop: countLine.getBoundingClientRect().top, modeTop: mode.getBoundingClientRect().top };
    });
    if (activeSummary.text !== "模式：手动提示" || activeSummary.modeTop <= activeSummary.countTop) throw new Error(`主页启用状态未分为两行: ${JSON.stringify(activeSummary)}`);
    await page.click("#addProfile");
    if (await page.locator("#apiKey").getAttribute("type") !== "password") throw new Error("编辑页 API 密钥仍以明文输入框显示");
    await page.fill("#apiKey", "sk-editor-placeholder");
    if (await page.locator("#copyApiKey").textContent() !== "复制") throw new Error("编辑页 API 密钥复制按钮未显示");
    await page.click("#copyApiKey");
    if (await page.evaluate(() => globalThis.__copiedText) !== "sk-editor-placeholder") throw new Error("编辑页 API 密钥复制内容错误");
    await page.screenshot({ path: path.join(projectRoot, ".tmp", "editor-api-key-copy-ui-smoke.png") });
    if ((await page.locator("#previewToken").textContent()).includes(state.localAccessToken)) throw new Error("编辑页预览仍显示完整本地密钥");
    if (await page.locator("#copyPreviewUrl").textContent() !== "复制地址" || await page.locator("#copyPreviewToken").textContent() !== "复制密钥") throw new Error("编辑页复制操作未拆分为地址和密钥");
    await page.click("#copyPreviewUrl");
    if (await page.evaluate(() => globalThis.__copiedText) !== state.proxyUrls.codex) throw new Error("复制地址内容错误");
    await page.click("#copyPreviewToken");
    if (await page.evaluate(() => globalThis.__copiedText) !== state.localAccessToken) throw new Error("复制密钥内容错误");
    await page.fill("#profileName", "未保存确认测试");
    await page.click("#editorBack");
    if (await page.locator("#confirmModal").isHidden() || await page.locator("#confirmModal .confirm-brand .panel-kicker").textContent() !== "CodexRelay" || !(await page.locator("#confirmMessage").textContent()).includes("当前修改尚未保存")) throw new Error("CodexRelay 自定义确认弹窗未显示");
    await page.screenshot({ path: path.join(projectRoot, ".tmp", "confirm-ui-smoke.png") });
    await page.click("#confirmCancel");
    if (await page.locator("#editorView").isHidden()) throw new Error("取消未保存返回确认后未留在编辑页");
    await page.click("#editorBack");
    await page.click("#confirmAccept");
    if (await page.locator("#profilesView").isHidden()) throw new Error("确认放弃未保存修改后未返回主页");
    await page.click("#addProfile");
    await page.locator(".advanced-fields summary").click();
    if (await page.locator("#modelManagerTitle").textContent() !== "模型管理" || await page.locator("#fetchModels").count() !== 1 || await page.locator("#addModel").count() !== 1) throw new Error("编辑页模型管理入口未显示");
    await page.click("#addModel");
    if (await page.locator("#modelRows .model-row").count() !== 1) throw new Error("手动添加模型行失败");
    await page.reload({ waitUntil: "networkidle" });

    const sortState = JSON.parse(JSON.stringify(state));
    sortState.preferences = { ...sortState.preferences, defaultSource: "", defaultCategory: "codex" };
    sortState.tokenSwitch = { ...sortState.tokenSwitch, mode: "auto" };
    sortState.profiles = [
      { id: "custom-first", source: "custom", category: "codex", name: "自定义测试", note: "自定义来源", active: false },
      { id: "doge-middle", source: "doge", category: "codex", name: "Codex 低价组", note: "二狗子来源", remoteTokenId: 42, active: true },
      { id: "doge-disabled", source: "doge", category: "codex", name: "Codex 临时不可用", note: "等待分组恢复", remoteTokenId: 43, active: false },
      { id: "doge-missing", source: "doge", category: "codex", name: "目录已删除", note: "不应显示", remoteTokenId: 44, active: false },
      { id: "custom-last", source: "custom", category: "codex", name: "自定义备用", note: "自定义来源", active: false, skipAutoSwitch: true },
    ];
    sortState.activeProfiles = { codex: "doge-middle" };
    sortState.failoverOrder = { codex: ["custom-first", "doge-middle", "doge-disabled", "custom-last"] };
    sortState.doge.bound = true;
    sortState.doge.tokens = [
      { id: 42, profileId: "doge-middle", name: "Codex 低价组", maskedKey: "sk-****", status: 1, group: "GPT低价组", groupDisplayName: "GPT低价组", groupRatio: 0.02, category: "codex", imported: true, needsCategory: false, permitted: true, active: true },
      { id: 43, profileId: "doge-disabled", name: "Codex 临时不可用", maskedKey: "sk-****", status: 2, group: "GPT低价组", groupDisplayName: "GPT低价组", groupRatio: 0.02, category: "codex", imported: true, needsCategory: false, permitted: false, active: false },
    ];
    const sortPage = await browser.newPage({ viewport: { width: 1170, height: 724 }, deviceScaleFactor: 1 });
    await sortPage.addInitScript((value) => { globalThis.__relayState = value; }, sortState);
    await sortPage.goto(`http://127.0.0.1:${address.port}/`, { waitUntil: "networkidle" });
    if (await sortPage.locator('[data-profile-id="doge-missing"]').count() !== 0) throw new Error("目录已删除的二狗子 Profile 被回退为普通可切换行");
    const autoModeButtons = sortPage.locator('.profile-auto-switch-icon');
    if (await autoModeButtons.count() !== 4 || !(await autoModeButtons.nth(0).getAttribute("src")).endsWith("/icons/auto.svg") || !(await autoModeButtons.nth(3).getAttribute("src")).endsWith("/icons/skip.svg")) throw new Error("自动/跳过令牌按钮状态错误");
    await autoModeButtons.nth(3).click();
    await sortPage.waitForFunction(() => document.querySelector('[data-profile-id="custom-last"] .profile-auto-switch-icon')?.getAttribute("src")?.endsWith("/icons/auto.svg"));
    const disabledRow = sortPage.locator('[data-profile-id="doge-disabled"]');
    const disabledButtons = disabledRow.locator('.profile-actions button');
    if (!(await disabledRow.getAttribute("draggable")) || !(await disabledRow.locator('.drag-handle').getAttribute("draggable"))) throw new Error("临时禁用令牌没有保留拖拽能力");
    if (!(await disabledButtons.nth(0).isDisabled()) || await disabledButtons.nth(1).isDisabled() || await disabledButtons.nth(2).isDisabled() || await disabledButtons.nth(3).isDisabled() || await disabledButtons.nth(4).isDisabled()) throw new Error("临时禁用令牌应只禁用切换按钮");
    const disabledOpacity = await disabledRow.evaluate((row) => getComputedStyle(row).opacity);
    if (disabledOpacity !== "0.52") throw new Error(`临时禁用令牌应保留不可用视觉状态: opacity=${disabledOpacity}`);
    const draggingBorder = await disabledRow.evaluate((row) => {
      row.classList.add("dragging");
      const style = getComputedStyle(row);
      const result = { outline: style.outlineStyle, shadow: style.boxShadow };
      row.classList.remove("dragging");
      return result;
    });
    if (draggingBorder.outline === "none" || !/3px 0px 0px 0px inset/.test(draggingBorder.shadow)) throw new Error(`拖动状态左边框样式未生效: ${JSON.stringify(draggingBorder)}`);
    const customHandle = sortPage.locator('[data-profile-id="custom-first"] .drag-handle');
    const dogeRow = sortPage.locator('[data-profile-id="doge-middle"]');
    const dogeBounds = await dogeRow.boundingBox();
    if (!dogeBounds) throw new Error("跨来源排序测试未找到二狗子令牌行");
    await customHandle.dragTo(dogeRow, { targetPosition: { x: dogeBounds.width / 2, y: dogeBounds.height - 4 } });
    await sortPage.waitForFunction(() => Boolean(globalThis.__lastFailoverOrderArgs), null, { timeout: 3000 });
    const reorderCall = await sortPage.evaluate(() => globalThis.__lastFailoverOrderArgs);
    if (reorderCall.category !== "codex" || reorderCall.ids.join() !== "doge-middle,custom-first,doge-disabled,custom-last") throw new Error(`跨来源拖拽提交顺序错误: ${JSON.stringify(reorderCall)}`);
    await sortPage.waitForFunction(() => Array.from(document.querySelectorAll('#profileList [data-sort-kind="failover-codex"]')).map((row) => row.dataset.profileId).join() === "doge-middle,custom-first,doge-disabled,custom-last");
    await sortPage.evaluate(() => { globalThis.__lastFailoverOrderArgs = null; });
    const disabledHandle = sortPage.locator('[data-profile-id="doge-disabled"] .drag-handle');
    const firstRow = sortPage.locator('[data-profile-id="doge-middle"]');
    const firstBounds = await firstRow.boundingBox();
    if (!firstBounds) throw new Error("临时禁用令牌排序测试未找到目标行");
    await disabledHandle.dragTo(firstRow, { targetPosition: { x: firstBounds.width / 2, y: 4 } });
    await sortPage.waitForFunction(() => Boolean(globalThis.__lastFailoverOrderArgs), null, { timeout: 3000 });
    const disabledReorderCall = await sortPage.evaluate(() => globalThis.__lastFailoverOrderArgs);
    if (disabledReorderCall.category !== "codex" || disabledReorderCall.ids.join() !== "doge-disabled,doge-middle,custom-first,custom-last") throw new Error(`临时禁用令牌拖拽提交顺序错误: ${JSON.stringify(disabledReorderCall)}`);
    await sortPage.screenshot({ path: path.join(projectRoot, ".tmp", "disabled-token-sort-ui-smoke.png") });
    await sortPage.close();

    if (await page.locator("#announcementBadge").textContent() !== "1") throw new Error("公告未读数字未显示");
    await page.evaluate(() => {
      globalThis.__relayState.doge.bound = true;
      globalThis.__relayState.doge.tokens = [{ id: 42, name: "后台更新令牌", maskedKey: "sk-****", status: 1, group: "余额低价组", groupDisplayName: "余额低价组", groupRatio: 0.02, category: "codex", imported: false, needsCategory: false, permitted: true, active: false }];
      globalThis.__wailsEvents["relay-state-changed"]();
    });
    await page.waitForTimeout(50);
    if (await page.locator(".doge-token-row").count() !== 0) throw new Error("后台同步的待导入令牌不应直接进入主页列表");
    if (await page.locator("#pendingDogeImport").isHidden() || await page.locator("#pendingDogeImportCount").textContent() !== "1") throw new Error("后台同步未显示醒目的待导入入口");
    if (!(await page.locator("#dogeCategoryModal").isHidden())) throw new Error("后台自动同步不应主动弹出分组窗口");
    await page.screenshot({ path: path.join(projectRoot, ".tmp", "pending-import-ui-smoke.png") });
    await page.click("#refreshDoge");
    await page.locator("#dogeCategoryModal").waitFor({ state: "visible", timeout: 3000 }).catch(() => { throw new Error("手动同步后未立即弹出待导入分组窗口"); });
    await page.click("#closeDogeCategoryModal");
    await page.evaluate(() => {
      globalThis.__relayState.profiles = Array.from({ length: 20 }, (_, index) => ({
        id: `profile-${42 + index}`,
        source: "doge",
        category: "codex",
        name: `滚动测试令牌 ${index + 1}`,
        remoteTokenId: 42 + index,
        active: index === 0,
      }));
      globalThis.__relayState.failoverOrder = { codex: globalThis.__relayState.profiles.map((profile) => profile.id) };
      globalThis.__relayState.doge.tokens = Array.from({ length: 20 }, (_, index) => ({
        id: 42 + index,
        profileId: `profile-${42 + index}`,
        name: `滚动测试令牌 ${index + 1}`,
        maskedKey: "sk-****",
        status: 1,
        group: "余额低价组",
        groupDisplayName: "余额低价组",
        groupRatio: 0.02,
        category: "codex",
        imported: true,
        needsCategory: false,
        permitted: true,
        active: index === 0,
      }));
      globalThis.__wailsEvents["relay-state-changed"]();
    });
    await page.waitForTimeout(50);
    if (!(await page.locator("#pendingDogeImport").isHidden()) || await page.locator(".profile-auto-switch-icon").count() !== 0) throw new Error("导入完成或手动模式的主页按钮状态错误");
    if (await page.locator('[aria-label="测试令牌 API"]').count() !== 20) throw new Error("测试令牌 API 按钮名称或数量错误");
    await page.locator('[aria-label="测试令牌 API"]').first().click();
    await page.locator("#toast.error.show").waitFor({ state: "visible" });
    await page.waitForFunction(() => {
      const toast = document.querySelector("#toast");
      return toast?.classList.contains("error") && !toast.classList.contains("show");
    }, null, { timeout: 4000 });
    const fadingErrorToast = await page.locator("#toast").evaluate((node) => ({
      className: node.className,
      backgroundColor: getComputedStyle(node).backgroundColor,
      color: getComputedStyle(node).color,
    }));
    if (!fadingErrorToast.className.includes("error") || fadingErrorToast.backgroundColor !== "rgb(255, 241, 241)" || fadingErrorToast.color !== "rgb(181, 47, 47)") throw new Error(`错误提示淡出时改变了颜色: ${JSON.stringify(fadingErrorToast)}`);
    await page.waitForTimeout(250);
    if (await page.locator("#toast").getAttribute("class") !== "toast") throw new Error("错误提示淡出完成后未清理状态");
    const profileScrollLayout = await page.evaluate(() => {
      const view = document.querySelector("#profilesView");
      const toolbar = view.querySelector(".app-toolbar");
      const filters = view.querySelector(".profile-toolbar");
      const list = view.querySelector(".profile-list");
      const before = { toolbarTop: toolbar.getBoundingClientRect().top, filtersTop: filters.getBoundingClientRect().top };
      list.scrollTop = list.scrollHeight;
      const after = { toolbarTop: toolbar.getBoundingClientRect().top, filtersTop: filters.getBoundingClientRect().top };
      return {
        overflowY: getComputedStyle(list).overflowY,
        scrollable: list.scrollHeight > list.clientHeight,
        outerScrollable: document.documentElement.scrollHeight > document.documentElement.clientHeight || document.body.scrollHeight > document.body.clientHeight,
        before,
        after,
      };
    });
    if (profileScrollLayout.overflowY !== "auto" || !profileScrollLayout.scrollable || profileScrollLayout.outerScrollable) throw new Error(`主页列表滚动区域错误: ${JSON.stringify(profileScrollLayout)}`);
    if (profileScrollLayout.before.toolbarTop !== profileScrollLayout.after.toolbarTop || profileScrollLayout.before.filtersTop !== profileScrollLayout.after.filtersTop) throw new Error(`主页顶部区域随列表滚动: ${JSON.stringify(profileScrollLayout)}`);
    await page.click("#openAnnouncements");
    if (await page.locator("#announcementPanel").isHidden()) throw new Error("公告面板未打开");
    if (!(await page.locator("#announcementNoticePane").textContent()).includes("当前公告内容")) throw new Error("当前公告未显示");
    if (await page.locator("#announcementNoticePane h1").count() !== 1) throw new Error("Markdown 标题未渲染");
    if (await page.locator("#announcementNoticePane strong").count() !== 1) throw new Error("Markdown 加粗未渲染");
    if (await page.locator("#announcementNoticePane a").count() !== 1) throw new Error("危险链接未被过滤");
    if (await page.locator("#announcementNoticePane a").getAttribute("target") !== "_blank") throw new Error("公告链接未设置安全打开方式");
    const announcementPopup = page.waitForEvent("popup", { timeout: 250 }).then(() => true).catch(() => false);
    await page.locator("#announcementNoticePane a").click();
    if (await announcementPopup) throw new Error("公告外链未交给默认浏览器，仍打开了 WebView 新窗口");
    if (!page.url().endsWith(`/`)) throw new Error("点击公告外链后仍在应用内导航");
    await page.click("#refreshDoge");
    const refreshLoadingClass = await page.locator("#refreshDoge .icon").getAttribute("class");
    if (!refreshLoadingClass.split(" ").includes("icon-load") || !refreshLoadingClass.split(" ").includes("spin")) throw new Error("刷新按钮未使用统一加载图标");
    await page.waitForTimeout(900);
    const refreshDoneClass = await page.locator("#refreshDoge .icon").getAttribute("class");
    if (!refreshDoneClass.split(" ").includes("icon-refresh") || refreshDoneClass.split(" ").includes("spin")) throw new Error(`刷新按钮静态图标未恢复: ${refreshDoneClass}`);
    await page.click('[data-announcement-tab="timeline"]');
    if (!(await page.locator("#announcementTimelinePane").textContent()).includes("历史公告内容")) throw new Error("历史公告未显示");
    if (await page.locator("#announcementTimelinePane h2").count() !== 1) throw new Error("历史公告 Markdown 标题未渲染");
    await page.click("#closeAnnouncements");
    await page.click("#openSettings");
    const desktopSettingsTabs = ["general", "network", "taskNotification", "connection", "advanced", "activity", "about"];
    const desktopSettingsBaseline = await page.evaluate(() => {
      const view = document.querySelector("#settingsView");
      const shell = view.querySelector(".settings-shell");
      const tabs = view.querySelector("#settingsTabs");
      const content = view.querySelector("#settingsContent");
      const rect = (node) => {
        const value = node.getBoundingClientRect();
        return { x: value.x, right: value.right, top: value.top, bottom: value.bottom, width: value.width, height: value.height };
      };
      return { view: rect(view), shell: rect(shell), tabs: rect(tabs), content: rect(content), documentWidth: document.documentElement.scrollWidth, viewportWidth: document.documentElement.clientWidth };
    });
    for (const tab of desktopSettingsTabs) {
      await page.click(`#settingsTabs button[data-tab="${tab}"]`);
      const layout = await page.evaluate(() => {
        const view = document.querySelector("#settingsView");
        const shell = view.querySelector(".settings-shell");
        const tabs = view.querySelector("#settingsTabs");
        const content = view.querySelector("#settingsContent");
        const panel = view.querySelector(".settings-panel:not(.hidden)");
        const rect = (node) => {
          const value = node.getBoundingClientRect();
          return { x: value.x, right: value.right, top: value.top, bottom: value.bottom, width: value.width, height: value.height };
        };
        return { shell: rect(shell), tabs: rect(tabs), content: rect(content), panel: rect(panel), documentWidth: document.documentElement.scrollWidth, viewportWidth: document.documentElement.clientWidth };
      });
      const sameTabs = Math.abs(layout.tabs.x - desktopSettingsBaseline.tabs.x) < 1 && Math.abs(layout.tabs.width - desktopSettingsBaseline.tabs.width) < 1 && Math.abs(layout.tabs.height - desktopSettingsBaseline.tabs.height) < 1;
      const contentToRight = layout.content.x >= layout.tabs.right - 1 && layout.panel.x >= layout.content.x - 1 && layout.panel.right <= layout.content.right + 1;
      const noHorizontalOverflow = layout.documentWidth <= layout.viewportWidth;
      if (!sameTabs || !contentToRight || !noHorizontalOverflow || layout.shell.width <= 0 || layout.tabs.height <= 0 || layout.content.width <= 0) throw new Error(`桌面设置分类布局异常: ${tab} ${JSON.stringify({ baseline: desktopSettingsBaseline, layout })}`);
    }
    await page.screenshot({ path: path.join(projectRoot, ".tmp", "settings-tabs-layout-ui-smoke.png") });
    if (await page.locator("#defaultSource").inputValue() !== "" || await page.locator("#defaultCategory").inputValue() !== "codex") throw new Error("默认来源或类别未显示为全部/Codex");
    if (await page.locator("#taskNotificationState").textContent() !== "已开启") throw new Error("主页未显示任务完成通知已开启状态");
    await page.click('#settingsTabs button[data-tab="taskNotification"]');
    if (!(await page.locator("#taskNotificationEnabled").isChecked()) || await page.locator("#taskNotificationWebhookUrl").inputValue() !== "https://notify.example.test/webhook") throw new Error("任务通知设置未显示");
    if (!(await page.locator("#taskNotificationPanel").textContent()).includes("Codex事件推送服务") || !(await page.locator("#taskNotificationPanel").textContent()).includes("当满足已勾选的事件要求时，自动调用推送 URL，避免开发进度中断，长时间无人处理。")) throw new Error("任务通知说明文案错误");
    if (await page.locator("#taskNotificationRequestMethod").count() || await page.locator("#taskNotificationTitle").count() || await page.locator("#taskNotificationContent").count()) throw new Error("任务通知仍保留多余的消息格式设置");
    if (!(await page.locator("#taskNotificationTaskCompleted").isChecked()) || !(await page.locator("#taskNotificationTaskAborted").isChecked()) || !(await page.locator("#taskNotificationTokenRequestFailed").isChecked()) || !(await page.locator("#taskNotificationTokenAutoSwitched").isChecked()) || !(await page.locator("#taskNotificationTokenAutoSwitchFailed").isChecked()) || !(await page.locator("#taskNotificationAccountBalanceLow").isChecked()) || !(await page.locator("#taskNotificationSubscriptionBalanceLow").isChecked()) || !(await page.locator(".task-notification-setting-card").count())) throw new Error("消息通知默认事件未全选");
    await page.locator("#taskNotificationTokenAutoSwitchFailed").click();
    const checkboxFocusStyle = await page.locator("#taskNotificationTokenAutoSwitchFailed").evaluate((node) => ({ outline: getComputedStyle(node).outlineStyle, shadow: getComputedStyle(node).boxShadow }));
    if (checkboxFocusStyle.outline !== "none" || checkboxFocusStyle.shadow !== "none") throw new Error(`复选框点击后仍显示焦点框: ${JSON.stringify(checkboxFocusStyle)}`);
    await page.locator("#taskNotificationTokenAutoSwitchFailed").check();
    if (await page.locator('.task-notification-service-links a[href="https://sct.ftqq.com/"]').count() !== 1 || await page.locator('.task-notification-service-links a[href="https://www.pushplus.plus/"]').count() !== 1) throw new Error("任务通知服务申请入口错误");
    if (!(await page.locator("#taskNotificationQueueState").textContent()).includes("候选 1 · 待投递 2 · 失败 0")) throw new Error("任务通知队列状态未显示");
    await page.screenshot({ path: path.join(projectRoot, ".tmp", "task-notification-settings-ui-smoke.png") });
    await page.locator("#taskNotificationEnabled").uncheck();
    await page.locator("#taskNotificationTokenAutoSwitchFailed").check();
    await page.fill("#taskNotificationWebhookUrl", "https://www.pushplus.plus/send?token=xxx&title={title}&content={content}");
    await page.evaluate(() => globalThis.__wailsEvents["relay-state-changed"]());
    await page.waitForTimeout(50);
    if (await page.locator("#taskNotificationEnabled").isChecked() || !(await page.locator("#taskNotificationTokenAutoSwitchFailed").isChecked()) || await page.locator("#taskNotificationWebhookUrl").inputValue() !== "https://www.pushplus.plus/send?token=xxx&title={title}&content={content}") throw new Error("后台状态刷新覆盖了未保存的任务通知设置");
    await page.fill("#taskNotificationIdleGraceSeconds", "7");
    await page.click("#saveTaskNotification");
    if (await page.locator("#taskNotificationEnabled").isChecked() || !(await page.locator("#taskNotificationTokenAutoSwitchFailed").isChecked()) || await page.locator("#taskNotificationWebhookUrl").inputValue() !== "https://www.pushplus.plus/send?token=xxx&title={title}&content={content}" || await page.locator("#taskNotificationIdleGraceSeconds").inputValue() !== "7") throw new Error("任务通知保存后未保留设置");
    await page.click("#testTaskNotification");
    await page.waitForTimeout(50);
    await page.click('#settingsTabs button[data-tab="advanced"]');
    if (await page.locator("#dataDirectory").inputValue() !== "C:\\Users\\Ricky-Desktop\\.CodexRelay") throw new Error("CodexRelay 默认数据目录未显示");
    if (await page.locator(".client-config-row").count() !== 2) throw new Error("外部客户端路径行未显示");
    if ((await page.locator(".client-config-row").first().textContent()).includes("config.toml") || (await page.locator(".client-config-row").nth(1).textContent()).includes("settings.json")) throw new Error("外部客户端配置文件名仍显示");
    await page.locator('.client-config-row').first().locator('input[type="text"]').fill("C:\\Users\\Ricky-Desktop\\.codex-draft");
    await page.evaluate(() => globalThis.__wailsEvents["relay-state-changed"]());
    await page.waitForTimeout(50);
    if (await page.locator('.client-config-row').first().locator('input[type="text"]').inputValue() !== "C:\\Users\\Ricky-Desktop\\.codex-draft") throw new Error("后台状态刷新覆盖了未保存的客户端目录");
    await page.locator('.client-config-row').first().locator('button').first().click();
    await page.waitForTimeout(50);
    if (await page.locator('.client-config-row').first().locator('input[type="text"]').inputValue() !== "C:\\Users\\Ricky-Desktop\\.codex-draft\\picked") throw new Error("外部客户端路径选择未保存");
    const claudeSkip = page.locator('.client-config-row').nth(1).locator('input[type="checkbox"]');
    if (!(await claudeSkip.isChecked())) throw new Error("跳过配置文件替换状态未显示");
    await claudeSkip.uncheck();
    if (await claudeSkip.isChecked()) throw new Error("跳过配置文件替换取消未生效");
    await page.locator("#chooseDataDirectory").click();
    await page.waitForTimeout(50);
    if (await page.locator("#dataDirectory").inputValue() !== "C:\\Users\\Ricky-Desktop\\.CodexRelay\\picked") throw new Error("CodexRelay 数据目录选择未保存");
    await page.click('#settingsTabs button[data-tab="general"]');
    await page.fill("#authFailureThreshold", "9");
    await page.fill("#balanceAlertThresholdUSD", "2.50");
    await page.evaluate(() => globalThis.__wailsEvents["relay-state-changed"]());
    await page.waitForTimeout(50);
    if (await page.locator("#authFailureThreshold").inputValue() !== "9" || await page.locator("#balanceAlertThresholdUSD").inputValue() !== "2.50") throw new Error("后台状态刷新覆盖了未保存的通用设置阈值");
    const settingsGrid = await page.evaluate(() => ({
      triggerColumns: getComputedStyle(document.querySelector(".trigger-option-grid")).gridTemplateColumns.split(" ").length,
      categoryColumns: getComputedStyle(document.querySelector(".category-visibility-grid")).gridTemplateColumns.split(" ").length,
      thresholdColumns: getComputedStyle(document.querySelector(".failover-threshold-grid")).gridTemplateColumns.split(" ").length,
      triggers: document.querySelectorAll(".trigger-option-grid .compact-check-option").length,
      categories: document.querySelectorAll(".category-visibility-grid .compact-check-option").length,
      thresholds: document.querySelectorAll(".failover-threshold-grid .failover-threshold").length,
      cardHeader: document.querySelector(".failover-setting-card")?.firstElementChild?.classList.contains("failover-card-header") || false,
      cardLoop: document.querySelector(".failover-setting-card")?.lastElementChild?.classList.contains("failover-card-loop") || false,
      homepageHeader: document.querySelector(".homepage-setting-card")?.firstElementChild?.classList.contains("homepage-card-header") || false,
      homepageDefaults: document.querySelector(".homepage-setting-card")?.lastElementChild?.classList.contains("homepage-default-view") || false,
      defaultControls: document.querySelectorAll(".homepage-default-view .preference-selects select").length,
      thresholdGaps: Array.from(document.querySelectorAll(".failover-threshold"), (row) => {
        const label = row.firstElementChild?.getBoundingClientRect();
        const control = row.lastElementChild?.getBoundingClientRect();
        return label && control ? Math.round(control.left - label.right) : -1;
      }),
    }));
    if (settingsGrid.triggerColumns !== 3 || settingsGrid.categoryColumns !== 3 || settingsGrid.thresholdColumns !== 3 || settingsGrid.triggers !== 6 || settingsGrid.categories !== 9 || settingsGrid.thresholds !== 3 || !settingsGrid.cardHeader || !settingsGrid.cardLoop || !settingsGrid.homepageHeader || !settingsGrid.homepageDefaults || settingsGrid.defaultControls !== 3 || settingsGrid.thresholdGaps.some((gap) => gap < 0 || gap > 10)) throw new Error(`通用设置面板布局错误: ${JSON.stringify(settingsGrid)}`);
    await page.locator(".failover-setting-card").scrollIntoViewIfNeeded();
    await page.screenshot({ path: path.join(projectRoot, ".tmp", "settings-option-grid-ui-smoke.png") });
    const settingsScrollLayout = await page.evaluate(() => {
      const view = document.querySelector("#settingsView");
      const toolbar = view.querySelector(".subpage-toolbar");
      const tabs = view.querySelector("#settingsTabs");
      const content = view.querySelector("#settingsContent");
      const before = {
        toolbarTop: toolbar.getBoundingClientRect().top,
        tabsTop: tabs.getBoundingClientRect().top,
      };
      content.scrollTop = content.scrollHeight;
      const after = {
        toolbarTop: toolbar.getBoundingClientRect().top,
        tabsTop: tabs.getBoundingClientRect().top,
      };
      return {
        contentRight: content.getBoundingClientRect().right,
        panelRight: view.querySelector(".settings-panel:not(.hidden)").getBoundingClientRect().right,
        viewOverflow: getComputedStyle(view).overflow,
        contentOverflowY: getComputedStyle(content).overflowY,
        contentScrollable: content.scrollHeight > content.clientHeight,
        outerScrollable: document.documentElement.scrollHeight > document.documentElement.clientHeight || document.body.scrollHeight > document.body.clientHeight,
        before,
        after,
      };
    });
    if (settingsScrollLayout.viewOverflow !== "hidden" || settingsScrollLayout.contentOverflowY !== "auto" || !settingsScrollLayout.contentScrollable || settingsScrollLayout.outerScrollable || settingsScrollLayout.contentRight - settingsScrollLayout.panelRight < 20) throw new Error(`设置页滚动区域错误: ${JSON.stringify(settingsScrollLayout)}`);
    if (settingsScrollLayout.before.toolbarTop !== settingsScrollLayout.after.toolbarTop || settingsScrollLayout.before.tabsTop !== settingsScrollLayout.after.tabsTop) throw new Error(`设置页顶部或左侧导航随内容滚动: ${JSON.stringify(settingsScrollLayout)}`);
    await page.screenshot({ path: path.join(projectRoot, ".tmp", "settings-scroll-ui-smoke.png") });
    await page.click('#settingsTabs button[data-tab="network"]');
    if (await page.locator("#proxyPort").inputValue() !== "8765") throw new Error("默认监听端口未显示");
    await page.click('#networkModes button[data-mode="manual"]');
    await page.fill("#manualProxy", "http://127.0.0.1:18880");
    await page.fill("#proxyPort", "18765");
    await page.evaluate(() => globalThis.__wailsEvents["relay-state-changed"]());
    await page.waitForTimeout(50);
    if (await page.locator("#manualProxy").inputValue() !== "http://127.0.0.1:18880" || await page.locator("#proxyPort").inputValue() !== "18765" || !(await page.locator('#networkModes button[data-mode="manual"]').evaluate((button) => button.classList.contains("active")))) throw new Error("后台状态刷新覆盖了未保存的网络设置");
    await page.fill("#proxyPort", "18766");
    await page.click("#saveProxyPort");
    if (await page.locator("#proxyPort").inputValue() !== "18766") throw new Error("监听端口保存失败");
    if (await page.locator("#listenOnAllInterfaces").isChecked()) throw new Error("监听范围默认不应允许外部访问");
    if (!(await page.locator("#listenScopeNote").textContent()).includes("仅 Windows 本机回环地址")) throw new Error("默认监听范围说明错误");
    await page.locator("#listenOnAllInterfaces").check();
    if (!(await page.locator("#listenOnAllInterfaces").isChecked()) || !((await page.locator("#listenScopeNote").textContent()).includes("所有网卡"))) throw new Error("允许 WSL2 访问开关未生效");
    await page.locator("#listenOnAllInterfaces").uncheck();
    if (await page.locator("#listenOnAllInterfaces").isChecked()) throw new Error("关闭 WSL2 访问开关未生效");
    await page.screenshot({ path: path.join(projectRoot, ".tmp", "network-port-ui-smoke.png"), fullPage: true });
    await page.click('#settingsTabs button[data-tab="about"]');
    const about = await page.locator("#aboutPanel").textContent();
    if (!about.includes("Ricky") || !about.includes("ergouzi.life") || !about.includes("dashboard/overview")) throw new Error("关于页信息未更新");
    const aboutPopup = page.waitForEvent("popup", { timeout: 250 }).then(() => true).catch(() => false);
    await page.locator("#aboutPanel a").first().click();
    if (await aboutPopup) throw new Error("关于页外链未交给默认浏览器，仍打开了 WebView 新窗口");
    const connection = await browser.newPage({ viewport: { width: 1120, height: 780 }, deviceScaleFactor: 1 });
    await connection.addInitScript((value) => { globalThis.__relayState = value; }, {
      ...state,
      doge: { ...state.doge, bound: true, redemptionEnabled: true, lastSyncAt: "2026-08-22T00:00:00Z", account: { userId: 7, nickname: "测试用户", email: "user@example.test", balanceUsd: 3, usedUsd: 0.5, requestCount: 12 } },
    });
    await connection.goto(`http://127.0.0.1:${address.port}/`, { waitUntil: "networkidle" });
    await connection.click("#openSettings");
    await connection.click('#settingsTabs button[data-tab="connection"]');
    const accountText = await connection.locator("#dogeBoundView").textContent();
    if (!accountText.includes("7") || !accountText.includes("测试用户") || !accountText.includes("user@example.test") || !accountText.includes("$3.00") || !accountText.includes("$0.50") || !accountText.includes("12")) throw new Error("连接页账户信息未显示");
    if (await connection.locator("#dogeSyncInterval").inputValue() !== "3") throw new Error("自动同步默认间隔不是 3 分钟");
    await connection.selectOption("#dogeSyncInterval", "1");
    await connection.waitForTimeout(50);
    if (await connection.locator("#dogeSyncInterval").inputValue() !== "1") throw new Error("自动同步间隔保存后未保持选择值");
    const syncOptions = await connection.locator("#dogeSyncInterval option").evaluateAll((options) => options.map((option) => option.value));
    if (syncOptions.join(",") !== "1,3,5,10,15,30,60") throw new Error("自动同步间隔选项不完整");
    await connection.evaluate(() => { globalThis.__wailsEvents["relay-state-changed"](); });
    await connection.waitForTimeout(50);
    if (await connection.locator("#dogeSyncInterval").inputValue() !== "1") throw new Error("重新读取状态后自动同步间隔被重置");
    await connection.click("#settingsBack");
    await connection.click("#openSettings");
    await connection.click('#settingsTabs button[data-tab="connection"]');
    if (await connection.locator("#dogeSyncInterval").inputValue() !== "1") throw new Error("重新打开设置后自动同步间隔未保持");
    await connection.screenshot({ path: path.join(projectRoot, ".tmp", "connection-account-ui-smoke.png"), fullPage: true });

    // 兑换弹窗打开期间，定时 loadState 不得重新计算滚动条补偿或造成主界面跳宽。
    await connection.click("#settingsBack");
    await connection.evaluate(() => { document.body.style.minHeight = "200vh"; });
    await connection.click("#openDogeTopup");
    if (await connection.locator("#dogeTopupModal").isHidden()) throw new Error("兑换弹窗未打开");
    const modalBefore = await connection.evaluate(() => ({
      shellWidth: document.querySelector("#appShell").getBoundingClientRect().width,
      bodyPaddingRight: getComputedStyle(document.body).paddingRight,
      compensation: document.body.style.getPropertyValue("--modal-scrollbar-compensation"),
      locked: document.body.classList.contains("modal-open"),
    }));
    await connection.waitForTimeout(3500);
    const modalAfter = await connection.evaluate(() => ({
      shellWidth: document.querySelector("#appShell").getBoundingClientRect().width,
      bodyPaddingRight: getComputedStyle(document.body).paddingRight,
      compensation: document.body.style.getPropertyValue("--modal-scrollbar-compensation"),
      locked: document.body.classList.contains("modal-open"),
    }));
    if (!modalBefore.locked || !modalAfter.locked) throw new Error("兑换弹窗未保持页面滚动锁定");
    if (modalBefore.shellWidth !== modalAfter.shellWidth) throw new Error(`弹窗期间主界面跳宽: ${modalBefore.shellWidth} -> ${modalAfter.shellWidth}`);
    if (modalBefore.bodyPaddingRight !== modalAfter.bodyPaddingRight || modalBefore.compensation !== modalAfter.compensation) throw new Error("弹窗期间滚动条补偿发生变化");
    await connection.click("#closeDogeTopupModal");
    const modalClosed = await connection.evaluate(() => ({
      paddingRight: getComputedStyle(document.body).paddingRight,
      compensation: document.body.style.getPropertyValue("--modal-scrollbar-compensation"),
      locked: document.body.classList.contains("modal-open"),
    }));
    if (modalClosed.locked || modalClosed.compensation || modalClosed.paddingRight !== "0px") throw new Error("关闭兑换弹窗后滚动条补偿未清理");
    await connection.evaluate(() => { document.body.style.minHeight = ""; });
    await connection.click("#openDogeTopup");
    await connection.fill("#dogeTopupCode", "fake-redemption-code");
    await connection.click("#submitDogeTopup");
    const topupLoadingClass = await connection.locator("#submitDogeTopup .icon").getAttribute("class");
    if (!topupLoadingClass.split(" ").includes("icon-load") || !topupLoadingClass.split(" ").includes("spin")) throw new Error("兑换按钮未使用统一加载图标");
    await connection.waitForTimeout(900);
    await connection.close();
    const popup = await browser.newPage({ viewport: { width: 430, height: 300 }, deviceScaleFactor: 1 });
    await popup.addInitScript((value) => { globalThis.__relayState = value; }, {
      ...state,
      doge: {
        ...state.doge,
        notifications: {
          ...state.doge.notifications,
          announcements: [{ ...state.doge.notifications.announcements[0], content: "# 公告标题\n\n[通知链接](https://example.com/notification)\n\n" + "长公告内容 ".repeat(100) }],
        },
      },
    });
    await popup.goto(`http://127.0.0.1:${address.port}/notification.html?kind=announcement`, { waitUntil: "networkidle" });
    if (await popup.locator(".notification-markdown").count() !== 1) throw new Error("右下角公告正文未显示");
    if (await popup.locator(".notification-markdown h1").count() !== 1) throw new Error("右下角 Markdown 标题未渲染");
    const announcementText = await popup.locator(".notification-markdown").textContent();
    if (announcementText.length <= 300 || !announcementText.includes("长公告内容 长公告内容")) throw new Error("右下角公告正文被截断");
    await popup.evaluate(() => { globalThis.__openExternalURLShouldFail = true; });
    const notificationPopup = popup.waitForEvent("popup", { timeout: 250 }).then(() => true).catch(() => false);
    await popup.locator(".notification-markdown a").click();
    if (await notificationPopup) throw new Error("提醒窗口外链未交给默认浏览器，仍打开了 WebView 新窗口");
    await popup.locator("#notificationStatus").waitFor({ state: "visible" });
    if (!(await popup.locator("#notificationStatus").textContent()).includes("默认浏览器打开失败")) throw new Error("普通通知未显示默认浏览器打开失败信息");
    const announcementCard = await popup.locator("#notificationCard").boundingBox();
    const announcementViewport = popup.viewportSize();
    if (!announcementCard || !announcementViewport || Math.abs(announcementCard.x) > 1 || Math.abs(announcementCard.y) > 1 || Math.abs(announcementViewport.width - announcementCard.width) > 1 || Math.abs(announcementViewport.height - announcementCard.height) > 1) throw new Error(`公告窗口没有填满原生窗口: ${JSON.stringify({ announcementCard, announcementViewport })}`);
    const announcementLayout = await popup.evaluate(() => {
      const card = document.querySelector("#notificationCard");
      const heading = document.querySelector(".notification-heading");
      const content = document.querySelector("#notificationContent");
      const actions = document.querySelector("#notificationActions");
      return { card, heading, content, actions };
    });
    if (!announcementLayout.content || !announcementLayout.actions) throw new Error("通知窗口缺少固定内容区或操作区");
    const announcementScroll = await popup.locator("#notificationContent").evaluate((node) => ({
      overflowY: getComputedStyle(node).overflowY,
      scrollable: node.scrollHeight > node.clientHeight,
    }));
    if (announcementScroll.overflowY !== "auto" || !announcementScroll.scrollable) throw new Error(`通知正文区域未启用独立滚动: ${JSON.stringify(announcementScroll)}`);
    if (!(await popup.locator("#notificationCard").evaluate((node) => getComputedStyle(node).overflow === "hidden"))) throw new Error("通知卡片外层仍可滚动");
    await popup.screenshot({ path: path.join(projectRoot, ".tmp", "announcement-popup-simulated.png") });
    await popup.close();
    const switchPopup = await browser.newPage({ viewport: { width: 430, height: 300 }, deviceScaleFactor: 1 });
    await switchPopup.addInitScript((value) => { globalThis.__relayState = value; }, {
      ...state,
      doge: {
        ...state.doge,
        tokenSwitch: {
          key: "doge-profile|auth",
          category: "codex",
          mode: "manual",
          failureKind: "auth",
          failureCount: 5,
          failureStatus: 403,
          currentTokenId: 41,
          currentName: "当前令牌",
          currentGroup: "余额低价组",
          currentRatio: 0.02,
          stopped: true,
          stopMessage: "不应显示的自动停止状态",
          message: "当前令牌连续 5 次返回 HTTP 403，是否切换同类别的其他可用令牌？",
          candidates: [
            { tokenId: 42, profileId: "doge-42", name: "Codex 低价组 (GPT低价组·0.02)", source: "二狗子 API", group: "余额低价组", ratio: 0.02, selectable: true },
            { tokenId: 43, profileId: "doge-43", name: "Codex 稳定组 (GPT稳定组·0.025)", source: "二狗子 API", group: "余额稳定组", ratio: 0.025, selectable: true },
            { tokenId: 0, profileId: "custom-44", name: "OpenRouter 主线路（自定义 API）", source: "自定义 API", group: "", ratio: 0, selectable: true },
          ],
        },
      },
    });
    await switchPopup.goto(`http://127.0.0.1:${address.port}/notification.html?kind=token-switch`, { waitUntil: "networkidle" });
    if (!(await switchPopup.locator("#tokenSwitchMessage").textContent()).includes("HTTP 403")) throw new Error("令牌切换提示未显示");
    if (await switchPopup.locator("#notificationTitle").textContent() !== "令牌切换提醒" || !(await switchPopup.locator("#tokenStopPanel").isHidden())) throw new Error("手动提示错误显示了自动停止状态");
    const switchCandidates = await switchPopup.locator("#tokenSwitchCandidates option").allTextContents();
    if (switchCandidates.join("|") !== "请选择|Codex 低价组 (GPT低价组·0.02)|Codex 稳定组 (GPT稳定组·0.025)|OpenRouter 主线路（自定义 API）") throw new Error("候选令牌格式错误");
    if (!(await switchPopup.locator("#confirmTokenSwitch").isDisabled())) throw new Error("未选择令牌时确定按钮应禁用");
    await switchPopup.selectOption("#tokenSwitchCandidates", "doge-42");
    if (await switchPopup.locator("#confirmTokenSwitch").isDisabled()) throw new Error("选择令牌后确定按钮未启用");
    if (await switchPopup.locator("#cancelTokenSwitch").textContent() !== "取消" || await switchPopup.locator("#confirmTokenSwitch").textContent() !== "确定") throw new Error("令牌切换按钮文案错误");
    if (await switchPopup.locator("button:visible").count() !== 2 || !(await switchPopup.locator("#dismissNotification").isHidden())) throw new Error("令牌切换窗口出现多余操作按钮");
    const switchCard = await switchPopup.locator("#notificationCard").boundingBox();
    const switchViewport = switchPopup.viewportSize();
    if (!switchCard || !switchViewport || Math.abs(switchCard.x) > 1 || Math.abs(switchCard.y) > 1 || Math.abs(switchViewport.width - switchCard.width) > 1 || Math.abs(switchViewport.height - switchCard.height) > 1) throw new Error(`令牌切换窗口没有填满原生窗口: ${JSON.stringify({ switchCard, switchViewport })}`);
    if (!announcementCard || Math.abs(announcementCard.width - switchCard.width) > 1 || Math.abs(announcementCard.height - switchCard.height) > 1) throw new Error(`提醒窗口尺寸未统一: ${JSON.stringify({ announcementCard, switchCard })}`);
    await switchPopup.screenshot({ path: path.join(projectRoot, ".tmp", "token-switch-ui-smoke.png") });
    await switchPopup.click("#cancelTokenSwitch");
    await switchPopup.close();
    for (const variant of [
      {
        status: 401,
        message: "当前令牌“Codex 低价组”连续 5 次返回 HTTP 401，是否切换同类别的其他可用令牌？",
        screenshot: "token-switch-401-ui-smoke.png",
      },
      {
        status: 502,
        message: "当前令牌“Codex 低价组”在 3 分钟内出现 5 次上游异常。上游连续异常，是否尝试切换同类别的其他可用令牌？",
        screenshot: "token-switch-5xx-ui-smoke.png",
      },
    ]) {
      const variantPopup = await browser.newPage({ viewport: { width: 430, height: 300 }, deviceScaleFactor: 1 });
      await variantPopup.addInitScript((value) => { globalThis.__relayState = value; }, {
        ...state,
        doge: {
          ...state.doge,
          tokenSwitch: {
            key: "doge-profile|variant",
            category: "codex",
            mode: "manual",
            failureKind: variant.status === 401 ? "auth" : "upstream",
            failureCount: 5,
            failureStatus: variant.status,
            currentTokenId: 41,
            currentName: "Codex 低价组 (GPT低价组·0.02)",
            currentGroup: "余额低价组",
            currentRatio: 0.02,
            message: variant.message,
            candidates: [{ tokenId: 42, name: "Codex 低价组 (GPT低价组·0.02)", source: "二狗子 API", group: "余额低价组", ratio: 0.02, selectable: true }],
          },
        },
      });
      await variantPopup.goto(`http://127.0.0.1:${address.port}/notification.html?kind=token-switch`, { waitUntil: "networkidle" });
      if (!(await variantPopup.locator("#tokenSwitchMessage").textContent()).includes(variant.status === 401 ? "HTTP 401" : "3 分钟内出现 5 次上游异常")) throw new Error(`令牌切换 ${variant.status} 文案未显示`);
      await variantPopup.screenshot({ path: path.join(projectRoot, ".tmp", variant.screenshot) });
      await variantPopup.close();
    }
    const autoPopup = await browser.newPage({ viewport: { width: 430, height: 300 }, deviceScaleFactor: 1 });
    await autoPopup.addInitScript((value) => { globalThis.__relayState = value; }, {
      ...state,
      doge: {
        ...state.doge,
        tokenSwitch: {
          key: "doge-profile|auto",
          category: "codex",
          mode: "auto",
          failureKind: "auth",
          failureCount: 5,
          failureStatus: 401,
          currentTokenId: 41,
          currentName: "Codex 低价组 (GPT低价组·0.02)",
          currentGroup: "GPT低价组",
          currentRatio: 0.02,
          switchedToName: "Codex 稳定组 (GPT稳定组·0.025)",
          message: "当前 Codex 低价组 (GPT低价组·0.02) 连续 5 次返回 HTTP 401，已达到故障阈值。\n已自动切换至 Codex 稳定组 (GPT稳定组·0.025)。",
          candidates: [],
        },
      },
    });
    await autoPopup.goto(`http://127.0.0.1:${address.port}/notification.html?kind=token-switch`, { waitUntil: "networkidle" });
    if (await autoPopup.locator("#notificationTitle").textContent() !== "已自动切换令牌") throw new Error("自动切换通知标题错误");
    if (!(await autoPopup.locator("#tokenSwitchMessage").textContent()).includes("已自动切换至 Codex 稳定组 (GPT稳定组·0.025)")) throw new Error("自动切换通知目标名称错误");
    if (await autoPopup.locator("#dismissNotification").isHidden() || !(await autoPopup.locator("#tokenSwitchActions").isHidden()) || !(await autoPopup.locator("#tokenSwitchCandidates").isHidden())) throw new Error("自动切换通知操作区错误");
    if (await autoPopup.locator("button:visible").count() !== 1) throw new Error("自动切换通知出现多余按钮");
    await autoPopup.click("#dismissNotification");
    await autoPopup.close();
    const stoppedPopup = await browser.newPage({ viewport: { width: 430, height: 400 }, deviceScaleFactor: 1 });
    await stoppedPopup.addInitScript((value) => { globalThis.__relayState = value; }, {
      ...state,
      doge: {
        ...state.doge,
        tokenSwitch: {
          key: "profile-c|stopped",
          category: "codex",
          mode: "auto",
          stopped: true,
          currentName: "令牌 C（自定义 API）",
          stopMessage: "当前类别暂无可用令牌，已停止自动切换。",
          message: "本轮令牌均已尝试，自动切换已停止。",
          candidates: [],
          switchHistory: [
            { fromName: "令牌 A（自定义 API）", toName: "令牌 B（自定义 API）", switchedAt: "2026-08-24 12:00:00", failureMessage: "连续 5 次返回 HTTP 401" },
            { fromName: "令牌 C（自定义 API）", toName: "", switchedAt: "2026-08-24 12:02:00", failureMessage: "连续 5 次返回 HTTP 401" },
          ],
        },
      },
    });
    await stoppedPopup.goto(`http://127.0.0.1:${address.port}/notification.html?kind=token-switch`, { waitUntil: "networkidle" });
    const stoppedRows = stoppedPopup.locator(".token-switch-history-row");
    if (await stoppedRows.count() !== 2 || !(await stoppedRows.nth(0).textContent()).includes("切换时间：") || !(await stoppedRows.nth(1).textContent()).includes("故障时间：")) throw new Error("自动切换停止历史的时间类型错误");
    const finalFailureText = await stoppedRows.nth(1).textContent();
    if (!finalFailureText.includes("令牌 C（自定义 API）") || finalFailureText.includes("→") || !finalFailureText.includes("错误信息：连续 5 次返回 HTTP 401")) throw new Error(`最后一个令牌故障记录错误: ${finalFailureText}`);
    await stoppedPopup.screenshot({ path: path.join(projectRoot, ".tmp", "token-switch-stopped-ui-smoke.png") });
    await stoppedPopup.close();
    const categoryState = JSON.parse(JSON.stringify(state));
    categoryState.doge.tokenSwitch = null;
    categoryState.doge.tokenSwitches = {
      codex: { key: "codex|auto", category: "codex", mode: "auto", currentName: "Codex A（自定义 API）", switchedToName: "Codex B（自定义 API）", message: "Codex 类别已自动切换", candidates: [] },
      claude: { key: "claude|auto", category: "claude", mode: "auto", currentName: "Claude A（自定义 API）", switchedToName: "Claude B（自定义 API）", message: "Claude 类别已自动切换", candidates: [] },
    };
    const codexPopup = await browser.newPage({ viewport: { width: 430, height: 300 }, deviceScaleFactor: 1 });
    const claudePopup = await browser.newPage({ viewport: { width: 430, height: 300 }, deviceScaleFactor: 1 });
    await codexPopup.addInitScript((value) => { globalThis.__relayState = value; }, categoryState);
    await claudePopup.addInitScript((value) => { globalThis.__relayState = value; }, categoryState);
    await codexPopup.goto(`http://127.0.0.1:${address.port}/notification.html?kind=token-switch&category=codex`, { waitUntil: "networkidle" });
    await claudePopup.goto(`http://127.0.0.1:${address.port}/notification.html?kind=token-switch&category=claude`, { waitUntil: "networkidle" });
    if (!(await codexPopup.locator("#tokenSwitchMessage").textContent()).includes("Codex 类别") || !(await claudePopup.locator("#tokenSwitchMessage").textContent()).includes("Claude 类别")) throw new Error("不同类别令牌通知发生覆盖");
    await codexPopup.close();
    await claudePopup.close();
    const onboarding = await browser.newPage({ viewport: { width: 560, height: 700 }, deviceScaleFactor: 1 });
    await onboarding.addInitScript((value) => { globalThis.__relayState = value; }, { ...state, needsOnboarding: true });
    await onboarding.goto(`http://127.0.0.1:${address.port}/`, { waitUntil: "networkidle" });
    if (await onboarding.locator("#onboardingModal").isHidden()) throw new Error("首次引导弹窗未显示");
    const onboardingText = await onboarding.locator("#onboardingModal").textContent();
    if (!onboardingText.includes("打开二狗子用户中心 → https://ergouzi.life/profile")) throw new Error("用户中心地址未显示");
    if (!onboardingText.includes("安全 → 访问令牌")) throw new Error("首次引导路径未显示");
    await onboarding.click("#openDogeProfile");
    await onboarding.click("#bindOnboarding");
    if (await onboarding.locator("#onboardingError").isHidden()) throw new Error("空令牌校验未显示");
    await onboarding.evaluate(() => {
      globalThis.__relayState.needsOnboarding = false;
      globalThis.__relayState.doge.bound = true;
      globalThis.__relayState.doge.tokens = [{ id: 91, name: "首次同步令牌", maskedKey: "sk-****", status: 1, imported: false, profileId: "", category: "", needsCategory: true }];
    });
    await onboarding.fill("#onboardingToken", "fake-access-token");
    await onboarding.click("#bindOnboarding");
    if (await onboarding.locator("#dogeCategoryModal").isHidden()) throw new Error("首次绑定同步后未立即弹出令牌分组窗口");
    await onboarding.screenshot({ path: path.join(projectRoot, ".tmp", "onboarding-ui-smoke.png"), fullPage: true });
    await onboarding.close();
    await page.setViewportSize({ width: 390, height: 844 });
    await page.click('#settingsTabs button[data-tab="taskNotification"]');
    const mobileTaskNotificationLayout = await page.evaluate(() => ({
      numberColumns: getComputedStyle(document.querySelector(".task-notification-number-fields")).gridTemplateColumns.split(" ").length,
      panelWidth: document.querySelector("#taskNotificationPanel").getBoundingClientRect().width,
      viewportWidth: document.documentElement.clientWidth,
    }));
    if (mobileTaskNotificationLayout.numberColumns !== 1 || mobileTaskNotificationLayout.panelWidth > mobileTaskNotificationLayout.viewportWidth) throw new Error(`移动端任务通知布局错误: ${JSON.stringify(mobileTaskNotificationLayout)}`);
    await page.screenshot({ path: path.join(projectRoot, ".tmp", "task-notification-mobile-ui-smoke.png"), fullPage: true });
    await page.click('#settingsTabs button[data-tab="general"]');
    const mobileSettingsLayout = await page.evaluate(() => {
      const view = document.querySelector("#settingsView");
      const tabs = view.querySelector("#settingsTabs");
      const content = view.querySelector("#settingsContent");
      const before = tabs.getBoundingClientRect().top;
      content.scrollTop = content.scrollHeight;
      return {
        contentOverflowY: getComputedStyle(content).overflowY,
        contentScrollable: content.scrollHeight > content.clientHeight,
        tabsTop: Math.abs(before - tabs.getBoundingClientRect().top) < 1,
        outerScrollable: document.documentElement.scrollHeight > document.documentElement.clientHeight || document.body.scrollHeight > document.body.clientHeight,
      };
    });
    if (mobileSettingsLayout.contentOverflowY !== "auto" || !mobileSettingsLayout.contentScrollable || !mobileSettingsLayout.tabsTop || mobileSettingsLayout.outerScrollable) throw new Error(`移动端设置页滚动区域错误: ${JSON.stringify(mobileSettingsLayout)}`);
    const overflow = await page.evaluate(() => ({ width: document.documentElement.scrollWidth, viewport: document.documentElement.clientWidth }));
    if (overflow.width > overflow.viewport) throw new Error(`移动端横向溢出: ${JSON.stringify(overflow)}`);
    await page.screenshot({ path: path.join(projectRoot, ".tmp", "notification-ui-smoke.png"), fullPage: true });
    if (errors.length) throw new Error(`页面错误: ${errors.join("; ")}`);
    console.log(JSON.stringify({ toolbar, overflow, screenshot: ".tmp/notification-ui-smoke.png" }));
  } finally {
    await browser.close();
    server.closeAllConnections();
    server.close();
  }
})().catch((error) => { console.error(error); process.exitCode = 1; });
