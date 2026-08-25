/*
 * @Author        : 顾青离
 * @Url           : sucaijun.com
 * @Email         : Ricky@LiHai.La
 * @Project       : CodexRelay
 * @Description   : Codex API 中转热切换桌面工具
 * @File          : 桌面界面状态、渲染与交互
 * @Read me       : 感谢使用 CodexRelay，源码注释齐全，支持二次开发。
 * @Remind        : 二次开发请保留原版权信息，谢谢。
 */
import {
  ActivateProfile,
  BindDoge,
  CheckClientConfig,
  CheckForUpdate,
  ClearUsage,
  ConfigureClient,
  CompleteOnboarding,
  DeleteProfile,
  EditDogeToken,
  EnableDogeToken,
  GetState,
  InstallUpdate,
  OpenDogeProfile,
  OpenDogeTopup,
  MarkDogeAnnouncementsRead,
  ReorderDogeTokens,
  ReorderFailoverProfiles,
  RedeemDoge,
  SaveProfile,
  SelectDirectory,
  SetDataDirectory,
  SetDogeTokenCategories,
  SetClientConfigPath,
  SetClientConfigSkip,
  SetNetwork,
  SetTaskNotification,
  SetDogeSyncInterval,
  SetTokenSwitchSettings,
  SetDogeAlertSettings,
  SetProxyPort,
  SetPreferences,
  SetProfileAutoSwitch,
  TestProfile,
  TestTaskNotification,
  FetchProfileModels,
  SyncDoge,
  UnbindDoge,
} from "./api.js";
import { renderAnnouncementMarkdown } from "./announcement-markdown.js";
import { registerExternalLinkHandler } from "./external-links.js";
import * as wails from "/wails/runtime.js";

const app = {
  state: null,
  view: "profiles",
  settingsTab: "general",
  selectedId: null,
  isNew: false,
  dirty: false,
  usageRange: "7d",
  usageProfile: "",
  sourceFilter: "",
  categoryFilter: "",
  viewFilterInitialized: false,
  draggingSortKey: null,
  toastTimer: null,
  toastCleanupTimer: null,
  dogeSyncPollTimer: null,
  dogeRemoteSyncing: false,
  dogeCategoryDialogSignature: "",
  announcementTab: "notice",
  localDogeSyncing: false,
  localAnnouncementSyncing: false,
  onboardingFocused: false,
  pendingActivation: null,
  clientSetupCategory: "",
  editorModels: [],
  editorDefaultModel: "",
  preferencesDirty: false,
  preferencesDraftRevision: 0,
  tokenSwitchDirty: false,
  tokenSwitchDraftRevision: 0,
  dogeAlertDirty: false,
  dogeAlertDraftRevision: 0,
  networkModeDirty: false,
  networkProxyDirty: false,
  networkPortDirty: false,
  networkDraftRevision: 0,
  clientConfigDrafts: {},
  clientConfigSkipDrafts: {},
  taskNotificationDirty: false,
  taskNotificationDraftRevision: 0,
  confirmResolver: null,
  updateCheckStarted: false,
  update: {
    supported: false,
    checked: false,
    checking: false,
    installing: false,
    available: false,
    latestVersion: "",
    phase: "",
    written: 0,
    total: 0,
    error: "",
  },
};

const categoryOptions = ["codex", "claude", "gemini", "grok", "opencode", "openclaw", "hermes", "image", "other"];

const $ = (id) => document.getElementById(id);

registerExternalLinkHandler((error) => toast(errorMessage(error), true));

function visibleCategorySet() {
  const configured = app.state?.preferences?.visibleCategories;
  return new Set(Array.isArray(configured) && configured.length ? configured : categoryOptions);
}

function isCategoryVisible(category) {
  return visibleCategorySet().has(category);
}

function applyDefaultViewFilter(force = false) {
  if (!app.state || (!force && app.viewFilterInitialized)) return;
  const preferences = app.state.preferences || {};
  app.sourceFilter = preferences.defaultSource || "";
  app.categoryFilter = preferences.defaultCategory || "";
  if (app.categoryFilter && !isCategoryVisible(app.categoryFilter)) app.categoryFilter = "";
  app.viewFilterInitialized = true;
}

function errorMessage(error) {
  if (typeof error === "string") return error;
  if (error?.message) return error.message;
  return String(error || "操作失败");
}

// 统一异步按钮的视觉反馈；保存原图标和文案，避免请求结束后不同操作留下不一致状态。
function setButtonLoading(button, loading, label = "") {
  if (!button) return;
  let iconNode = button.querySelector(".icon");
  const labelNode = button.querySelector("[data-button-label], [data-topup-label], [data-doge-action-label], span:not(.icon)");
  if (loading) {
    if (button.dataset.loading !== "true") {
      button.dataset.loading = "true";
      if (!iconNode) {
        iconNode = icon("load", "spin");
        iconNode.dataset.loadingCreated = "true";
        button.prepend(iconNode);
      }
      button.dataset.loadingIcon = iconNode.className;
      if (labelNode) button.dataset.loadingLabel = labelNode.textContent;
    }
    button.disabled = true;
    iconNode = button.querySelector(".icon");
    if (iconNode) iconNode.className = "icon icon-load spin";
    if (labelNode && label) labelNode.textContent = label;
    return;
  }
  if (button.dataset.loading !== "true") return;
  if (iconNode?.dataset.loadingCreated === "true") iconNode.remove();
  else if (iconNode) iconNode.className = button.dataset.loadingIcon || "icon icon-load";
  if (labelNode) labelNode.textContent = button.dataset.loadingLabel || labelNode.textContent;
  delete button.dataset.loading;
  delete button.dataset.loadingIcon;
  delete button.dataset.loadingLabel;
  button.disabled = false;
}

function setLoadingText(node, loading, text) {
  if (!node) return;
  if (!loading) {
    node.textContent = text;
    node.classList.remove("inline-loading");
    return;
  }
  node.replaceChildren(icon("load", "spin"), Object.assign(document.createElement("span"), { textContent: text }));
  node.classList.add("inline-loading");
}

async function loadState() {
  try {
    app.state = await GetState();
    applyDefaultViewFilter();
    renderShell();
    renderProfiles();
    renderPreferences();
    renderNetwork();
    renderTaskNotification();
    renderConnection();
    renderDataDirectory();
    renderClientConfigs();
    renderRequests();
    renderAnnouncements();
    renderOnboarding();
    renderDogeSyncToast();
    if (!app.updateCheckStarted && app.state.updateSupported) {
      app.updateCheckStarted = true;
      setTimeout(() => checkForUpdates(false), 0);
    }
  } catch (error) {
    toast(errorMessage(error), true);
  }
}

function renderShell() {
  $("version").textContent = "v" + app.state.version;
  $("aboutVersion").textContent = "版本 " + app.state.version;
  renderUpdateStatus();
  const visibleCategories = visibleCategorySet();
  const activeCategories = new Set((app.state.profiles || []).filter((profile) => profile.active && visibleCategories.has(profile.category)).map((profile) => profile.category));
  $("activeCount").textContent = `${activeCategories.size}/${visibleCategories.size}`;
  const failoverMode = app.state.tokenSwitch?.mode === "auto" ? "模式：自动切换" : "模式：手动提示";
  const failoverStatus = $("failoverStatus");
  failoverStatus.textContent = failoverMode;
  failoverStatus.classList.toggle("manual", app.state.tokenSwitch?.mode !== "auto");
  $("taskNotificationState").textContent = app.state.taskNotification?.enabled ? "已开启" : "已关闭";
  $("taskNotificationSummary").classList.toggle("is-enabled", Boolean(app.state.taskNotification?.enabled));
  renderPendingDogeImport();
  renderDogeQuota();
}

function renderPendingDogeImport() {
  const count = pendingDogeTokens().length;
  $("pendingDogeImport").classList.toggle("hidden", count === 0);
  $("pendingDogeImportCount").textContent = String(count);
}

function renderUpdateStatus() {
  const section = $("windowsUpdate");
  const supported = Boolean(app.state?.updateSupported);
  app.update.supported = supported;
  section.classList.toggle("hidden", !supported);
  if (!supported) return;

  const title = $("updateTitle");
  const status = $("updateStatus");
  const button = $("updateAction");
  const buttonIcon = button.querySelector(".icon");
  const buttonLabel = button.querySelector("[data-button-label]");
  const progress = $("updateProgress");
  const total = Number(app.update.total || 0);
  const written = Number(app.update.written || 0);
  const percent = total > 0 ? Math.max(0, Math.min(100, Math.round(written / total * 100))) : 0;

  status.classList.toggle("error", Boolean(app.update.error));
  progress.classList.toggle("hidden", !app.update.installing || total <= 0);
  progress.setAttribute("aria-valuenow", String(percent));
  $("updateProgressBar").style.width = percent + "%";
  button.disabled = app.update.checking || app.update.installing;

  if (app.update.installing) {
    title.textContent = app.update.phase || "正在准备更新";
    status.textContent = total > 0 ? `已下载 ${percent}%` : "请保持程序运行";
    button.className = "primary-button compact-button";
    buttonIcon.className = "icon icon-load spin";
    buttonLabel.textContent = "更新中";
    return;
  }
  if (app.update.checking) {
    title.textContent = "正在检查新版本";
    status.textContent = "正在连接 GitHub Releases";
    button.className = "secondary-button compact-button";
    buttonIcon.className = "icon icon-load spin";
    buttonLabel.textContent = "检查中";
    return;
  }
  if (app.update.error) {
    title.textContent = "无法检查更新";
    status.textContent = app.update.error;
  } else if (app.update.available) {
    title.textContent = `发现新版本 v${app.update.latestVersion}`;
    status.textContent = "下载完成并校验通过后将自动重启";
  } else if (app.update.checked) {
    title.textContent = "当前已是最新版本";
    status.textContent = `当前版本 v${app.state.version}`;
  } else {
    title.textContent = "检查 Windows 新版本";
    status.textContent = "尚未检查";
  }
  button.className = app.update.available ? "primary-button compact-button" : "secondary-button compact-button";
  buttonIcon.className = app.update.available ? "icon icon-download" : "icon icon-refresh";
  buttonLabel.textContent = app.update.available ? "下载并重启" : "检查更新";
}

async function checkForUpdates(manual = true) {
  if (!app.state?.updateSupported || app.update.checking || app.update.installing) return;
  app.update.checking = true;
  app.update.error = "";
  renderUpdateStatus();
  try {
    const info = await CheckForUpdate();
    app.update.checked = true;
    app.update.available = Boolean(info.available);
    app.update.latestVersion = info.latestVersion || "";
    if (app.update.available) toast(`发现新版本 v${app.update.latestVersion}`);
    else if (manual) toast("当前已是最新版本");
  } catch (error) {
    app.update.checked = true;
    app.update.available = false;
    app.update.error = errorMessage(error);
    if (manual) toast(app.update.error, true);
  } finally {
    app.update.checking = false;
    renderUpdateStatus();
  }
}

async function runWindowsUpdate() {
  if (!app.update.available) {
    await checkForUpdates(true);
    return;
  }
  const confirmed = await showConfirmDialog(`下载并安装 CodexRelay v${app.update.latestVersion}？程序将在更新后自动重启。`, { title: "安装 Windows 更新" });
  if (!confirmed) return;
  app.update.installing = true;
  app.update.phase = "正在下载更新";
  app.update.written = 0;
  app.update.total = 0;
  app.update.error = "";
  renderUpdateStatus();
  try {
    await InstallUpdate();
    app.update.phase = "更新已校验，正在重启";
    renderUpdateStatus();
  } catch (error) {
    app.update.installing = false;
    app.update.error = errorMessage(error);
    toast(app.update.error, true);
    renderUpdateStatus();
  }
}

function formatDogeUSD(value) {
  const amount = Number(value);
  return Number.isFinite(amount) ? `$${amount.toFixed(2)}` : "$0.00";
}

function formatDogeEndTime(timestamp) {
  const value = Number(timestamp);
  if (!Number.isFinite(value) || value <= 0) return "未设置到期时间";
  const date = new Date(value * 1000);
  return Number.isNaN(date.getTime()) ? "未设置到期时间" : `到期 ${date.toLocaleDateString("zh-CN")}`;
}

function isDogeSyncing() {
  const doge = app.state?.doge || {};
  const waitingForFirstSync = Boolean(doge.bound && !doge.lastSyncAt && !doge.lastSyncError);
  return Boolean(app.localDogeSyncing || doge.syncing || waitingForFirstSync);
}

function showDogeSyncToast(phase = "base") {
  const message = phase === "keys" ? "令牌密钥同步中..." : "基础数据同步中...";
  const node = $("toast");
  node.replaceChildren(icon("load", "spin"), Object.assign(document.createElement("span"), { textContent: message }));
  node.className = "toast show";
  clearTimeout(app.toastTimer);
  clearTimeout(app.toastCleanupTimer);
  app.toastTimer = null;
  app.toastCleanupTimer = null;
}

function renderDogeSyncToast() {
  const doge = app.state?.doge || {};
  const remoteSyncing = Boolean(doge.syncing);
  if (remoteSyncing) {
    showDogeSyncToast(doge.syncPhase || "base");
  } else if (!app.localDogeSyncing && app.dogeRemoteSyncing) {
    toast("数据同步完成");
  }
  app.dogeRemoteSyncing = remoteSyncing;
}

// 手动同步期间短轮询本地状态，只读取阶段标记，不重复请求上游；同步结束后由主请求统一刷新完整页面。
function startDogeSyncProgressPolling() {
  if (app.dogeSyncPollTimer) return;
  const poll = async () => {
    app.dogeSyncPollTimer = null;
    if (!app.localDogeSyncing) return;
    try {
      app.state = await GetState();
      renderShell();
      renderConnection();
      renderDogeSyncToast();
    } catch {
      // 主同步请求负责报告错误，阶段轮询失败时保持当前提示，避免覆盖原始错误。
    }
    if (app.localDogeSyncing) app.dogeSyncPollTimer = setTimeout(poll, 350);
  };
  app.dogeSyncPollTimer = setTimeout(poll, 120);
}

function stopDogeSyncProgressPolling() {
  if (!app.dogeSyncPollTimer) return;
  clearTimeout(app.dogeSyncPollTimer);
  app.dogeSyncPollTimer = null;
}

function isDogeAnnouncementSyncing() {
  const doge = app.state?.doge || {};
  const notifications = doge.notifications || {};
  const waitingForFirstSync = Boolean(!notifications.lastSyncAt && !notifications.lastSyncError && !notifications.initialized);
  return Boolean(app.localAnnouncementSyncing || doge.announcementSyncing || notifications.syncing || waitingForFirstSync);
}

function renderDogeQuota() {
  const doge = app.state?.doge || {};
  const bound = Boolean(doge.bound);
  const syncing = bound && isDogeSyncing();
  const syncError = bound && String(doge.lastSyncError || "").trim();
  const failedBeforeData = Boolean(syncError && !doge.lastSyncAt && !syncing);
  const total = bound ? doge.totalUsd : 0;
  const amount = (value) => syncing ? "同步中..." : (failedBeforeData ? "同步失败" : formatDogeUSD(value));
  setLoadingText($("dogeQuotaTotal"), syncing, syncing ? "同步中..." : amount(total));
  $("dogeQuotaPopoverTotal").textContent = amount(total);
  $("dogeWalletQuota").textContent = amount(bound ? doge.walletUsd : 0);
  $("dogeSubscriptionsQuota").textContent = amount(bound ? doge.subscriptionsUsd : 0);
  $("dogeQuotaSummary").classList.toggle("is-syncing", syncing);
  setLoadingText($("dogeQuotaStatus"), syncing, syncing ? "同步中..." : (failedBeforeData ? "同步失败" : (bound ? (syncError ? "同步失败" : "已连接") : "未绑定")));
  $("dogeQuotaStatus").classList.toggle("is-error", !bound || Boolean(syncError));
  const rows = $("dogeSubscriptionRows");
  rows.replaceChildren();
  for (const subscription of (bound && !syncing ? doge.subscriptions || [] : [])) {
    const row = document.createElement("div");
    row.className = "doge-subscription-row";
    const label = document.createElement("span");
    const title = document.createElement("span");
    title.textContent = subscription.planTitle || `套餐 ${subscription.planId || ""}`;
    const expiry = document.createElement("small");
    expiry.textContent = formatDogeEndTime(subscription.endTime);
    label.append(title, expiry);
    const amount = document.createElement("strong");
    amount.textContent = formatDogeUSD(subscription.remainingUsd);
    row.append(label, amount);
    rows.appendChild(row);
  }
  if (syncing) {
    const loading = document.createElement("small");
    loading.className = "usage-missing inline-loading";
    loading.append(icon("load", "spin"), Object.assign(document.createElement("span"), { textContent: "同步中..." }));
    rows.appendChild(loading);
  } else if (failedBeforeData) {
    const failed = document.createElement("small");
    failed.className = "usage-missing";
    failed.textContent = "同步失败，暂无可用缓存";
    rows.appendChild(failed);
  } else if (bound && !rows.children.length) {
    const empty = document.createElement("small");
    empty.className = "usage-missing";
    empty.textContent = "暂无有效套餐";
    rows.appendChild(empty);
  }
  const topupButton = $("openDogeTopup");
  topupButton.disabled = !bound || syncing;
  renderDogeTopupState();
}

function renderDogeTopupState() {
  const doge = app.state?.doge || {};
  const bound = Boolean(doge.bound);
  const syncing = bound && isDogeSyncing();
  const failedBeforeData = Boolean(bound && doge.lastSyncError && !doge.lastSyncAt && !syncing);
  const enabled = bound && !syncing && !failedBeforeData && doge.redemptionEnabled !== false;
  const input = $("dogeTopupCode");
  const submit = $("submitDogeTopup");
  input.disabled = !enabled;
  if (submit.dataset.loading !== "true") submit.disabled = !enabled;
  const error = $("dogeTopupError");
  error.classList.remove("inline-loading");
  if (!bound) {
    error.textContent = "请先在设置中绑定二狗子 API";
    error.classList.remove("hidden");
    error.classList.remove("is-syncing");
  } else if (syncing) {
    setLoadingText(error, true, "同步中... 请稍候");
    error.classList.remove("hidden");
    error.classList.add("is-syncing");
  } else if (failedBeforeData) {
    error.textContent = "二狗子 API 同步失败，请重试";
    error.classList.remove("hidden");
    error.classList.remove("is-syncing");
  } else if (doge.redemptionEnabled === false) {
    error.textContent = "当前账户暂未开放兑换额度";
    error.classList.remove("hidden");
    error.classList.remove("is-syncing");
  } else if (!error.dataset.submission) {
    error.textContent = "";
    error.classList.add("hidden");
    error.classList.remove("is-syncing");
  }
  const purchase = $("dogeTopupPurchase");
  purchase.disabled = !String(doge.topupLink || "").trim();
  purchase.classList.toggle("hidden", purchase.disabled);
}

function formatAnnouncementDate(value) {
  const date = new Date(value || "");
  return Number.isNaN(date.getTime()) ? "时间未知" : date.toLocaleString("zh-CN", { hour12: false });
}

function renderAnnouncements() {
  const notifications = app.state?.doge?.notifications || {};
  const syncing = isDogeAnnouncementSyncing();
  const syncError = String(notifications.lastSyncError || "").trim();
  const badge = $("announcementBadge");
  const unread = Number(notifications.unreadCount || 0);
  badge.textContent = unread > 99 ? "99+" : String(unread);
  badge.classList.toggle("hidden", unread <= 0);
  const noticePane = $("announcementNoticePane");
  noticePane.replaceChildren();
  if (notifications.currentNotice) {
    const article = document.createElement("article");
    article.className = "announcement-current-content";
    article.append(renderAnnouncementMarkdown(notifications.currentNotice));
    noticePane.appendChild(article);
  } else {
    const empty = document.createElement("p");
    empty.className = "announcement-empty";
    if (syncing) setLoadingText(empty, true, "同步中...");
    else empty.textContent = syncError ? "公告同步失败，显示缓存" : (notifications.enabled === false ? "公告功能暂未启用" : "暂无当前公告");
    noticePane.appendChild(empty);
  }
  const timelinePane = $("announcementTimelinePane");
  timelinePane.replaceChildren();
  const announcements = notifications.announcements || [];
  if (!announcements.length) {
    const empty = document.createElement("p");
    empty.className = "announcement-empty";
    if (syncing) setLoadingText(empty, true, "同步中...");
    else empty.textContent = syncError ? "公告同步失败，显示缓存" : "暂无历史公告";
    timelinePane.appendChild(empty);
  } else {
    for (const announcement of announcements) {
      const article = document.createElement("article");
      article.className = `announcement-item type-${announcement.type || "default"}`;
      const marker = document.createElement("span");
      marker.className = "announcement-marker";
      const body = document.createElement("div");
      body.className = "announcement-item-body";
      const content = document.createElement("div");
      content.className = "announcement-item-content";
      content.append(renderAnnouncementMarkdown(announcement.content));
      const meta = document.createElement("small");
      meta.textContent = formatAnnouncementDate(announcement.publishDate);
      body.append(content, meta);
      article.append(marker, body);
      timelinePane.appendChild(article);
    }
  }
  setLoadingText($("announcementSyncState"), syncing, syncError ? "公告同步失败，显示缓存" : (notifications.lastSyncAt ? `更新于 ${formatAnnouncementDate(notifications.lastSyncAt)}` : "同步中..."));
  document.querySelectorAll("[data-announcement-tab]").forEach((button) => button.classList.toggle("active", button.dataset.announcementTab === app.announcementTab));
  noticePane.classList.toggle("hidden", app.announcementTab !== "notice");
  timelinePane.classList.toggle("hidden", app.announcementTab !== "timeline");
}

async function markAnnouncementsRead(showError = true) {
  const ids = (app.state?.doge?.notifications?.announcements || []).map((announcement) => announcement.id).filter((id) => Number(id) > 0);
  if (!ids.length) return;
  const button = showError ? $("markAnnouncementsRead") : null;
  setButtonLoading(button, true, "处理中...");
  try {
    await MarkDogeAnnouncementsRead(ids);
    await loadState();
  } catch (error) {
    if (showError) toast(errorMessage(error), true);
  } finally {
    setButtonLoading(button, false);
  }
}

function setAnnouncementPanel(open) {
  const panel = $("announcementPanel");
  panel.classList.toggle("hidden", !open);
  $("openAnnouncements").setAttribute("aria-expanded", String(open));
  if (open) markAnnouncementsRead(false);
}

function syncModalBody() {
  const modalIDs = ["onboardingModal", "dogeCategoryModal", "dogeTopupModal", "clientSetupModal", "confirmModal"];
  const body = document.body;
  const shouldLock = modalIDs.some((id) => !$(id).classList.contains("hidden"));
  const isLocked = body.classList.contains("modal-open");
  if (shouldLock && !isLocked) {
    // 只在首次锁定时记录滚动条宽度，定时刷新状态不能重复计算，否则页面会在弹窗期间跳宽。
    const scrollbarWidth = Math.max(0, window.innerWidth - document.documentElement.clientWidth);
    body.style.setProperty("--modal-scrollbar-compensation", `${scrollbarWidth}px`);
  } else if (!shouldLock && isLocked) {
    body.style.removeProperty("--modal-scrollbar-compensation");
  }
  body.classList.toggle("modal-open", shouldLock);
}

function closeConfirmDialog(result = false) {
  const modal = $("confirmModal");
  if (modal.classList.contains("hidden")) return;
  modal.classList.add("hidden");
  syncModalBody();
  const resolver = app.confirmResolver;
  app.confirmResolver = null;
  resolver?.(result);
}

// 统一替代浏览器原生 confirm，避免 WebView 使用 wails.localhost 作为系统弹窗标题。
function showConfirmDialog(message, options = {}) {
  if (app.confirmResolver) closeConfirmDialog(false);
  const modal = $("confirmModal");
  const accept = $("confirmAccept");
  $("confirmTitle").textContent = options.title || "确认操作";
  $("confirmMessage").textContent = message;
  $("confirmCancel").textContent = options.cancelLabel || "取消";
  accept.querySelector("[data-confirm-label]").textContent = options.confirmLabel || "确定";
  accept.classList.toggle("danger-button", Boolean(options.danger));
  accept.classList.toggle("primary-button", !options.danger);
  modal.classList.remove("hidden");
  syncModalBody();
  setTimeout(() => $("confirmCancel")?.focus(), 0);
  return new Promise((resolve) => { app.confirmResolver = resolve; });
}

function renderOnboarding() {
  const modal = $("onboardingModal");
  const open = Boolean(app.state?.needsOnboarding);
  modal.classList.toggle("hidden", !open);
  syncModalBody();
  if (!open) {
    app.onboardingFocused = false;
    return;
  }
  if (!app.onboardingFocused) {
    app.onboardingFocused = true;
    setTimeout(() => $("onboardingToken")?.focus(), 0);
  }
}

function openDogeTopupModal() {
  if (!app.state?.doge?.bound) {
    toast("请先在设置中绑定二狗子 API", true);
    return;
  }
  renderDogeTopupState();
  $("dogeTopupError").dataset.submission = "";
  $("dogeTopupModal").classList.remove("hidden");
  syncModalBody();
  setTimeout(() => $("dogeTopupCode").focus(), 0);
}

function closeDogeTopupModal() {
  $("dogeTopupModal").classList.add("hidden");
  syncModalBody();
}

async function openDogeProfile() {
  try {
    await OpenDogeProfile();
  } catch (error) {
    toast(errorMessage(error), true);
  }
}

async function bindOnboarding() {
  const input = $("onboardingToken");
  const errorNode = $("onboardingError");
  const button = $("bindOnboarding");
  const token = input.value.trim();
  if (!token) {
    errorNode.textContent = "请输入二狗子访问令牌";
    errorNode.classList.remove("hidden");
    input.focus();
    return;
  }
  setButtonLoading(button, true, "绑定中...");
  errorNode.classList.add("hidden");
  try {
    await BindDoge(token);
    input.value = "";
    await loadState();
    openDogeCategoryDialog(true);
    toast("二狗子已验证并绑定");
  } catch (error) {
    errorNode.textContent = errorMessage(error);
    errorNode.classList.remove("hidden");
  } finally {
    setButtonLoading(button, false);
  }
}

async function skipOnboarding() {
  const button = $("skipOnboarding");
  setButtonLoading(button, true, "处理中...");
  try {
    await CompleteOnboarding();
    $("onboardingToken").value = "";
    await loadState();
  } catch (error) {
    toast(errorMessage(error), true);
  } finally {
    setButtonLoading(button, false);
  }
}

async function redeemDoge() {
  const input = $("dogeTopupCode");
  const code = input.value.trim();
  if (!code) {
    const error = $("dogeTopupError");
    error.textContent = "请输入兑换码";
    error.dataset.submission = "1";
    error.classList.remove("hidden");
    input.focus();
    return;
  }
  const button = $("submitDogeTopup");
  setButtonLoading(button, true, "兑换中...");
  try {
    await RedeemDoge(code);
    input.value = "";
    closeDogeTopupModal();
    await loadState();
    toast("兑换成功，额度已更新");
  } catch (error) {
    const message = errorMessage(error);
    const errorNode = $("dogeTopupError");
    errorNode.textContent = message;
    errorNode.dataset.submission = "1";
    errorNode.classList.remove("hidden");
  } finally {
    setButtonLoading(button, false);
    renderDogeTopupState();
  }
}

async function openDogePurchase() {
  try {
    await OpenDogeTopup();
  } catch (error) {
    toast(errorMessage(error), true);
  }
}

function orderedProfiles() {
  const byID = new Map((app.state?.profiles || []).map((profile) => [profile.id, profile]));
  const ordered = [];
  const seen = new Set();
  for (const category of categoryOptions) {
    const ids = app.state?.failoverOrder?.[category] || [];
    for (const id of ids) {
      const profile = byID.get(id);
      if (profile && profile.category === category && !seen.has(id)) {
        ordered.push(profile);
        seen.add(id);
      }
    }
  }
  for (const profile of app.state?.profiles || []) {
    if (!seen.has(profile.id)) ordered.push(profile);
  }
  return ordered;
}

function renderProfileRow(profile, list) {
  const row = document.createElement("article");
  row.className = "profile-row" + (profile.active ? " active" : "");
  row.dataset.profileId = profile.id;
  row.dataset.category = profile.category;
  row.dataset.sortKind = `failover-${profile.category}`;

  const dragHandle = createDragHandle();
  const mark = document.createElement("span");
  mark.className = "provider-mark";
  mark.textContent = providerInitial(profile.name);
  const tags = [];
  if (!app.sourceFilter) tags.push({ text: sourceLabel(profile.source), tone: "source" });
  if (!app.categoryFilter) tags.push({ text: categoryLabel(profile.category), tone: "category" });
  const info = createProfileInfo({ name: profile.name, tags, note: profile.note, active: profile.active });
  const actions = buildProfileActions({
    active: profile.active,
    onSwitch: (event) => activateProfile(profile.id, event.currentTarget),
    onTest: (event) => testProfile(profile.id, event.currentTarget),
    autoSwitchEnabled: !profile.skipAutoSwitch,
    onAutoSwitch: (event) => setProfileAutoSwitch(profile.id, profile.skipAutoSwitch, event.currentTarget),
    onEdit: () => openEditor(profile.id),
    onDelete: (event) => deleteProfile(profile.id, event.currentTarget),
  });
  row.append(dragHandle, mark, info, actions);
  installSortableDrag(row, dragHandle, { sortKind: `failover-${profile.category}`, keyAttribute: "profileId", persistOrder: () => persistFailoverOrder(profile.category) });
  list.appendChild(row);
}

function renderProfiles() {
  if (app.draggingSortKey) return;
  if (app.categoryFilter && !isCategoryVisible(app.categoryFilter)) app.categoryFilter = "";
  const list = $("profileList");
  list.replaceChildren();
  const allProfiles = orderedProfiles();
  const dogeTokens = app.state?.doge?.tokens || [];
  const dogeByProfileID = new Map(dogeTokens.filter((token) => token.profileId).map((token) => [token.profileId, token]));
  const profiles = allProfiles.filter((profile) => profileMatchesFilters(profile));
  renderFilterButtons();
  for (const profile of profiles) {
    if (profile.source === "doge") {
      // 二狗子 Profile 必须以最新目录实体为准；缺失项不得退回普通可切换行。
      const token = dogeByProfileID.get(profile.id);
      if (token) renderDogeToken(token, list, { sortable: true });
    } else {
      renderProfileRow(profile, list);
    }
  }
  const dogeSelected = app.sourceFilter === "doge";
  const dogeVisible = !app.sourceFilter || dogeSelected;
  const dogeSyncing = dogeVisible && Boolean(app.state.doge?.bound) && isDogeSyncing();
  const dogeSyncError = dogeVisible && Boolean(app.state.doge?.bound) && Boolean(app.state.doge?.lastSyncError);
  const dogeFailedBeforeData = dogeSyncError && !app.state.doge?.lastSyncAt && !dogeSyncing;
  const hasRows = profiles.length > 0;
  $("emptyProfiles").classList.toggle("hidden", hasRows);
  $("emptyProfilesTitle").textContent = dogeSyncing ? "二狗子 API 同步中..." : (dogeFailedBeforeData ? "二狗子 API 同步失败，请重试" : (dogeSelected ? (app.state.doge?.bound ? "二狗子暂无令牌" : "请在设置中绑定二狗子") : "还没有代理 API"));
  $("emptyAdd").classList.toggle("hidden", dogeSelected);
}

function pendingDogeTokens() {
  return (app.state?.doge?.tokens || []).filter((token) => !token.profileId && !token.imported);
}

function openDogeCategoryDialog(force = false) {
  if (app.state?.needsOnboarding) return;
  const pending = pendingDogeTokens();
  const modal = $("dogeCategoryModal");
  if (!pending.length) {
    modal.classList.add("hidden");
    syncModalBody();
    app.dogeCategoryDialogSignature = "";
    return;
  }
  const signature = pending.map((token) => `${token.id}:${token.orderKey || String(token.id)}`).join("|");
  if (!force && app.dogeCategoryDialogSignature === signature) return;
  app.dogeCategoryDialogSignature = signature;
  const rows = $("dogeCategoryRows");
  const selectedValues = new Map(Array.from(rows.querySelectorAll(".doge-category-select"), (select) => [select.dataset.tokenId, select.value]));
  rows.replaceChildren();
  for (const token of pending) {
    const row = document.createElement("div");
    row.className = "doge-category-row";
    const info = document.createElement("div");
    info.className = "doge-category-info";
    const name = document.createElement("strong");
    name.textContent = nonHomeDogeTokenName(token);
    const detail = document.createElement("small");
    detail.textContent = token.maskedKey || "未返回密钥";
    info.append(name, detail);
    const select = document.createElement("select");
    select.className = "sync-select doge-category-select";
    select.dataset.tokenId = String(token.id);
    select.setAttribute("aria-label", `${nonHomeDogeTokenName(token)}存放分组`);
    select.appendChild(new Option("选择分组", ""));
    for (const category of categoryOptions) select.appendChild(new Option(categoryLabel(category), category));
    if (selectedValues.has(select.dataset.tokenId)) select.value = selectedValues.get(select.dataset.tokenId);
    else if (categoryOptions.includes(token.category)) select.value = token.category;
    row.append(info, select);
    rows.appendChild(row);
  }
  modal.classList.remove("hidden");
  syncModalBody();
  setTimeout(() => rows.querySelector("select")?.focus(), 0);
}

function closeDogeCategoryDialog() {
  $("dogeCategoryModal").classList.add("hidden");
  syncModalBody();
}

async function saveDogeCategoryAssignments() {
  const assignments = Array.from(document.querySelectorAll(".doge-category-select")).map((select) => ({
    id: Number(select.dataset.tokenId),
    category: select.value,
  }));
  if (assignments.some((assignment) => !assignment.category)) {
    toast("请为每个新令牌选择存放分组", true);
    return;
  }
  const button = $("saveDogeCategories");
  setButtonLoading(button, true, "保存中...");
  try {
    await SetDogeTokenCategories(assignments);
    closeDogeCategoryDialog();
    await loadState();
    toast("二狗子令牌存放分组已保存");
  } catch (error) {
    toast(errorMessage(error), true);
  } finally {
    setButtonLoading(button, false);
  }
}

function renderDogeToken(token, list, { sortable = false } = {}) {
  const row = document.createElement("article");
  const unavailable = token.permitted === false;
  const localProfile = findLocalDogeProfile(token);
  const imported = Boolean(localProfile);
  // 目录可用状态只限制切换按钮；已导入 Profile 始终保留在故障顺序中，并继续支持鼠标和键盘排序。
  const sortableInFailoverOrder = sortable && imported;
  row.className = "profile-row doge-token-row" + (token.active ? " active" : "") + (token.needsCategory ? " unassigned" : "") + (unavailable ? " unavailable" : "") + (token.active && unavailable ? " active-unavailable" : "");
  row.dataset.profileId = localProfile?.id || "";
  row.dataset.category = localProfile?.category || token.category || "";
  row.dataset.sortKind = sortableInFailoverOrder ? `failover-${row.dataset.category}` : "doge";
  row.dataset.dogeOrderKey = token.orderKey || String(token.id);
  const dragHandle = createDragHandle();
  const mark = document.createElement("img");
  mark.className = "provider-mark provider-image";
  mark.src = "/logo.png";
  mark.alt = "二狗子";
  const tags = [];
  if (!app.sourceFilter) tags.push({ text: "二狗子", tone: "source" });
  if (!app.categoryFilter) tags.push({ text: token.category ? categoryLabel(token.category) : "待选择", tone: "category" });
  tags.push({ text: token.groupDisplayName || "未分组", tone: "group" });
  if (token.groupRatio > 0) tags.push({ text: `倍率：${formatDogeRatio(token.groupRatio)}`, tone: "ratio" });
  if (unavailable) tags.push({ text: "当前分组不可用", tone: "unavailable" });
  const info = createProfileInfo({ name: token.name || `令牌 ${token.id}`, tags, note: token.note || "未提供备注" });
  const actions = buildProfileActions({
    active: token.active,
    switchDisabled: token.needsCategory || unavailable,
    onSwitch: imported
      ? (event) => activateProfile(localProfile.id, event.currentTarget)
      : (event) => enableDogeToken(token, event.currentTarget),
    onTest: (event) => testDogeToken(token, event.currentTarget),
    autoSwitchEnabled: !localProfile?.skipAutoSwitch,
    onAutoSwitch: imported ? (event) => setProfileAutoSwitch(localProfile.id, localProfile.skipAutoSwitch, event.currentTarget) : null,
    onEdit: imported ? () => openEditor(localProfile.id) : token.needsCategory ? () => openDogeCategoryDialog(true) : (event) => editDogeToken(token, event.currentTarget),
    editTitle: imported ? "编辑代理 API" : "选择存放分组",
    onDelete: imported ? (event) => deleteProfile(localProfile.id, event.currentTarget) : null,
    deleteDisabled: !imported,
  });
  row.append(dragHandle, mark, info, actions);
  if (sortableInFailoverOrder) {
    installSortableDrag(row, dragHandle, { sortKind: `failover-${row.dataset.category}`, keyAttribute: "profileId", persistOrder: () => persistFailoverOrder(row.dataset.category) });
  } else {
    installSortableDrag(row, dragHandle, { sortKind: "doge", keyAttribute: "dogeOrderKey", persistOrder: persistDogeTokenOrder });
  }
  list.appendChild(row);
}

function findLocalDogeProfile(token) {
  const profileID = String(token?.profileId || "").trim();
  if (profileID) {
    return app.state?.profiles?.find((profile) => profile.id === profileID && profile.source === "doge") || null;
  }
  const remoteTokenID = Number(token?.id);
  if (!Number.isFinite(remoteTokenID) || remoteTokenID <= 0) return null;
  return app.state?.profiles?.find((profile) => profile.source === "doge" && Number(profile.remoteTokenId) === remoteTokenID) || null;
}

function clientConfigFor(category) {
  return (app.state?.clientConfigs || []).find((item) => item.category === category) || null;
}

function clientCategoryLabel(category) {
  return clientConfigFor(category)?.label || categoryLabel(category);
}

function clientIsConfigured(category) {
  const status = clientConfigFor(category);
  return !status || status.status === "configured" || status.status === "unsupported";
}

function renderDataDirectory() {
  const input = $("dataDirectory");
  if (!input) return;
  input.value = app.state?.dataDirectory || "";
}

function openClientSetupModal(category, pending) {
  app.clientSetupCategory = category;
  app.pendingActivation = pending;
  $("clientSetupHeading").textContent = clientCategoryLabel(category) + " 配置";
  $("clientSetupQuestion").textContent = `当前 ${clientCategoryLabel(category)} 未使用 CodexRelay 配置信息，是否一键配置？`;
  $("clientSetupURL").textContent = app.state?.proxyUrls?.[category] || app.state?.proxyUrl || "-";
  $("clientSetupKey").textContent = app.state?.localAccessToken || "-";
  $("clientSetupModal").classList.remove("hidden");
  syncModalBody();
}

function closeClientSetupModal() {
  $("clientSetupModal").classList.add("hidden");
  app.pendingActivation = null;
  app.clientSetupCategory = "";
  syncModalBody();
}

async function performActivation(pending) {
  const button = pending?.button || null;
  setButtonLoading(button, true, pending.tokenId ? "启用中..." : "切换中...");
  try {
    if (pending.tokenId) {
      await EnableDogeToken(pending.tokenId);
    } else {
      await ActivateProfile(pending.profileId);
    }
    await loadState();
    toast(`已切换到 ${clientCategoryLabel(pending.category)} 类别`);
  } catch (error) {
    toast(errorMessage(error), true);
  } finally {
    setButtonLoading(button, false);
  }
}

async function beginActivation(pending) {
  const client = clientConfigFor(pending.category);
  if (client?.skipConfigReplacement) {
    await performActivation(pending);
    return;
  }
  let configured = clientIsConfigured(pending.category);
  try {
    const status = await CheckClientConfig(pending.category);
    configured = !status || status.status === "configured" || status.status === "unsupported";
    const cached = app.state?.clientConfigs?.find((item) => item.category === pending.category);
    if (cached && status) Object.assign(cached, status);
  } catch (error) {
    toast(errorMessage(error), true);
    return;
  }
  if (configured) {
    await performActivation(pending);
    return;
  }
  openClientSetupModal(pending.category, pending);
}

async function resolveClientSetup(configure) {
  const pending = app.pendingActivation;
  const category = app.clientSetupCategory;
  if (!pending || !category) {
    closeClientSetupModal();
    return;
  }
  const button = $("clientSetupConfigure");
  if (configure) {
    setButtonLoading(button, true, "配置中...");
    try {
      await ConfigureClient(category, pending.profileId || "");
      await loadState();
    } catch (error) {
      toast(errorMessage(error), true);
      setButtonLoading(button, false);
      return;
    } finally {
      setButtonLoading(button, false);
    }
  }
  closeClientSetupModal();
  await performActivation(pending);
}

async function enableDogeToken(token, button = null) {
  let category = token.category || "";
  if (!category) {
    toast("请先为二狗子令牌选择存放类别", true);
    return;
  }
  if (!token.imported) {
    setButtonLoading(button, true, "准备中...");
    try {
      await EditDogeToken(token.id);
      const nextState = await GetState();
      const imported = nextState.doge?.tokens?.find((item) => item.id === token.id);
      if (!imported?.profileId) throw new Error("二狗子令牌尚未生成本地代理 API");
      token = imported;
      category = imported.category || category;
    } catch (error) {
      toast(errorMessage(error), true);
      setButtonLoading(button, false);
      return;
    } finally {
      setButtonLoading(button, false);
    }
  }
  await beginActivation({ category, profileId: token.profileId, tokenId: 0, button });
}

async function editDogeToken(token, button = null) {
  // 已有本地代理档案时直接打开编辑页，避免因同步竞态重复请求远端密钥。
  if (token?.profileId) {
    openEditor(token.profileId);
    return;
  }
  setButtonLoading(button, true, "读取中...");
  try {
    await EditDogeToken(token.id);
    await loadState();
    const imported = app.state.doge?.tokens?.find((item) => item.id === token.id);
    if (imported?.profileId) openEditor(imported.profileId);
  } catch (error) {
    toast(errorMessage(error), true);
  } finally {
    setButtonLoading(button, false);
  }
}

async function testDogeToken(token, button) {
  if (token.needsCategory) {
    toast("请先为二狗子令牌选择存放类别", true);
    return;
  }
  setButtonLoading(button, true, "测试中...");
  try {
    let profileId = token.profileId;
    if (!profileId) {
      await EditDogeToken(token.id);
      const nextState = await GetState();
      profileId = nextState.doge?.tokens?.find((item) => item.id === token.id)?.profileId;
    }
    if (!profileId) throw new Error("二狗子令牌尚未生成本地代理 API");
    await testProfile(profileId, button);
    await loadState();
  } catch (error) {
    toast(errorMessage(error), true);
  } finally {
    setButtonLoading(button, false);
  }
}

function appendProfileTag(parent, text, tone) {
  const tag = document.createElement("span");
  tag.className = `profile-kind profile-tag-${tone}`;
  tag.textContent = text;
  parent.appendChild(tag);
}

function createDragHandle() {
  const handle = document.createElement("button");
  handle.type = "button";
  handle.className = "drag-handle";
  handle.title = "拖动排序";
  handle.setAttribute("aria-label", "拖动排序");
  handle.appendChild(icon("grip-vertical"));
  return handle;
}

function createProfileInfo({ name, tags = [], note = "", active = false }) {
  const info = document.createElement("div");
  info.className = "profile-info";
  const nameLine = document.createElement("div");
  nameLine.className = "profile-name-line";
  const nameNode = document.createElement("span");
  nameNode.className = "profile-name";
  nameNode.textContent = name;
  nameLine.appendChild(nameNode);
  for (const tag of tags) appendProfileTag(nameLine, tag.text, tag.tone);
  if (active) {
    const pill = document.createElement("span");
    pill.className = "active-pill";
    pill.textContent = "使用中";
    nameLine.appendChild(pill);
  }
  info.appendChild(nameLine);
  if (note) {
    const noteNode = document.createElement("span");
    noteNode.className = "profile-note";
    noteNode.textContent = note;
    noteNode.title = note;
    info.appendChild(noteNode);
  }
  return info;
}

function formatDogeRatio(value) {
  const ratio = Number(value);
  return Number.isFinite(ratio) ? String(ratio) : "-";
}

function formatNonHomeDogeName(name, group, ratio) {
  const cleanName = String(name || "未命名令牌").trim() || "未命名令牌";
  const cleanGroup = String(group || "").trim();
  if (!cleanGroup) return cleanName;
  const numericRatio = Number(ratio);
  return numericRatio > 0
    ? `${cleanName} (${cleanGroup}·${formatDogeRatio(numericRatio)})`
    : `${cleanName} (${cleanGroup})`;
}

function nonHomeDogeTokenName(token) {
  return formatNonHomeDogeName(token?.name || `令牌 ${token?.id || ""}`, token?.groupDisplayName, token?.groupRatio);
}

// 主界面以名称、分组和倍率分开显示；其他页面统一使用完整令牌名称。
function nonHomeProfileName(profile) {
  const name = String(profile?.name || "代理 API").trim();
  if (profile?.source === "custom") return `${name}（自定义 API）`;
  if (profile?.source !== "doge") return name;
  const token = (app.state?.doge?.tokens || []).find((item) => Number(item.id) === Number(profile.remoteTokenId));
  return formatNonHomeDogeName(name, token?.groupDisplayName, token?.groupRatio);
}

function profileMatchesFilters(profile) {
  return isCategoryVisible(profile.category) &&
    (!app.sourceFilter || profile.source === app.sourceFilter) &&
    (!app.categoryFilter || profile.category === app.categoryFilter);
}

function renderFilterButtons() {
  const activeCategories = new Set((app.state.profiles || []).filter((profile) => profile.active).map((profile) => profile.category));
  document.querySelectorAll(".filter-options").forEach((group) => {
    const isCategoryGroup = group.dataset.filterGroup === "category";
    const value = isCategoryGroup ? app.categoryFilter : app.sourceFilter;
    group.querySelectorAll(".filter-option").forEach((button) => {
      const categoryHidden = isCategoryGroup && button.dataset.filterValue && !isCategoryVisible(button.dataset.filterValue);
      button.classList.toggle("hidden", categoryHidden);
      const active = button.dataset.filterValue === value;
      button.classList.toggle("active", active);
      const hasActiveProfile = isCategoryGroup && activeCategories.has(button.dataset.filterValue);
      button.classList.toggle("has-active", hasActiveProfile);
      button.setAttribute("aria-selected", String(active));
      button.title = hasActiveProfile ? `${categoryLabel(button.dataset.filterValue)} 已启用代理 API` : "";
    });
  });
}

function setFilter(group, value) {
  if (group === "category" && value && !isCategoryVisible(value)) return;
  if (group === "source") app.sourceFilter = value;
  if (group === "category") app.categoryFilter = value;
  app.viewFilterInitialized = true;
  renderShell();
  renderProfiles();
}

function categoryLabel(category) {
  return { codex: "Codex", claude: "Claude", gemini: "Gemini", grok: "Grok", opencode: "OpenCode", openclaw: "OpenClaw", hermes: "Hermes", image: "生图", other: "其他" }[category] || "未分类";
}

function sourceLabel(source) {
  return source === "doge" ? "二狗子" : "自定义";
}

// 统一自定义 API 与二狗子令牌的鼠标、键盘排序行为；稳定身份字段和保存回调由调用方提供。
function installSortableDrag(row, handle, { sortKind, keyAttribute, persistOrder }) {
  const readKey = () => row.dataset[keyAttribute] || "";
  const dataAttribute = `data-${keyAttribute.replace(/[A-Z]/g, (letter) => "-" + letter.toLowerCase())}`;
  const findRow = (key) => document.querySelector(`[${dataAttribute}="${CSS.escape(key)}"]`);
  row.draggable = true;
  handle.draggable = true;
  handle.addEventListener("keydown", async (event) => {
    if (event.key !== "ArrowUp" && event.key !== "ArrowDown") return;
    event.preventDefault();
    const sibling = event.key === "ArrowUp" ? previousSortableSibling(row) : nextSortableSibling(row);
    if (!sibling) return;
    moveSortableRow(row.parentElement, row, sibling, event.key === "ArrowDown");
    await persistOrder();
    findRow(readKey())?.querySelector(".drag-handle")?.focus();
  });
  row.addEventListener("dragstart", (event) => {
    if (!event.target.closest(".drag-handle")) {
      event.preventDefault();
      return;
    }
    app.draggingSortKey = readKey();
    row.classList.add("dragging");
    event.dataTransfer.effectAllowed = "move";
    event.dataTransfer.setData("text/plain", readKey());
  });
  row.addEventListener("dragover", (event) => {
    if (!app.draggingSortKey || row.dataset.sortKind !== sortKind || app.draggingSortKey === readKey()) return;
    event.preventDefault();
    event.dataTransfer.dropEffect = "move";
    const dragged = findRow(app.draggingSortKey);
    if (!dragged) return;
    const bounds = row.getBoundingClientRect();
    const insertAfter = event.clientY > bounds.top + bounds.height / 2;
    moveSortableRow(row.parentElement, dragged, row, insertAfter);
  });
  row.addEventListener("drop", (event) => event.preventDefault());
  row.addEventListener("dragend", async () => {
    row.classList.remove("dragging");
    app.draggingSortKey = null;
    await persistOrder();
  });
}

function previousSortableSibling(row) {
  let sibling = row.previousElementSibling;
  while (sibling && sibling.dataset.sortKind !== row.dataset.sortKind) sibling = sibling.previousElementSibling;
  return sibling;
}

function nextSortableSibling(row) {
  let sibling = row.nextElementSibling;
  while (sibling && sibling.dataset.sortKind !== row.dataset.sortKind) sibling = sibling.nextElementSibling;
  return sibling;
}

function moveSortableRow(list, dragged, target, insertAfter) {
  if (insertAfter && target.nextElementSibling === dragged) return;
  if (!insertAfter && target.previousElementSibling === dragged) return;
  const positions = new Map(Array.from(list.children).map((row) => [row, row.getBoundingClientRect().top]));
  list.insertBefore(dragged, insertAfter ? target.nextElementSibling : target);
  for (const row of list.children) {
    if (row === dragged) continue;
    const delta = positions.get(row) - row.getBoundingClientRect().top;
    if (!delta) continue;
    row.animate(
      [{ transform: `translateY(${delta}px)` }, { transform: "translateY(0)" }],
      { duration: 170, easing: "cubic-bezier(.2,.8,.2,1)" },
    );
  }
}

async function persistFailoverOrder(category) {
  const sortKind = `failover-${category}`;
  const ids = Array.from($("profileList").children).filter((row) => row.dataset.sortKind === sortKind).map((row) => row.dataset.profileId).filter(Boolean);
  if (!ids.length) return;
  const current = [...(app.state.failoverOrder?.[category] || [])];
  const visible = new Set(ids);
  const next = current.slice();
  let cursor = 0;
  for (let index = 0; index < next.length; index += 1) {
    if (visible.has(next[index])) next[index] = ids[cursor++];
  }
  while (cursor < ids.length) next.push(ids[cursor++]);
  if (next.every((id, index) => id === current[index]) && next.length === current.length) return;
  await persistOrder(() => ReorderFailoverProfiles(category, next), "令牌切换顺序已更新");
}

async function persistDogeTokenOrder() {
  const ids = Array.from($("profileList").children).filter((row) => row.dataset.sortKind === "doge").map((row) => row.dataset.dogeOrderKey).filter(Boolean);
  await persistOrder(() => ReorderDogeTokens(ids), "二狗子令牌排序已更新");
}

// 统一排序后的刷新、成功提示和失败恢复；排序数据本身由各类别保存函数整理。
async function persistOrder(save, successMessage) {
  try {
    await save();
    await loadState();
    toast(successMessage);
  } catch (error) {
    await loadState();
    toast(errorMessage(error), true);
  }
}

function icon(name, extraClass = "") {
  const node = document.createElement("span");
  node.className = "icon icon-" + name + (extraClass ? " " + extraClass : "");
  node.setAttribute("aria-hidden", "true");
  return node;
}

function buildProfileActions({ active, switchDisabled = false, onSwitch, onTest, autoSwitchEnabled = true, onAutoSwitch, onEdit, onDelete, editTitle = "编辑代理 API", deleteDisabled = false }) {
  const actions = document.createElement("div");
  actions.className = "profile-actions";
  const buttons = [
    actionButton(active ? "当前" : "切换", active ? "use-button current" : "use-button", "切换当前代理 API", active ? "check" : "play", onSwitch, active || switchDisabled),
    actionButton("", "row-icon-button", "测试令牌 API", "activity", onTest),
    actionButton("", "row-icon-button", editTitle, "edit", onEdit),
    actionButton("", "row-icon-button danger", "删除本地代理 API", "trash-2", onDelete, deleteDisabled),
  ];
  if (app.state?.tokenSwitch?.mode === "auto" && onAutoSwitch) buttons.splice(2, 0, autoSwitchButton(autoSwitchEnabled, onAutoSwitch));
  actions.append(...buttons);
  return actions;
}

function autoSwitchButton(enabled, handler) {
  const button = document.createElement("button");
  button.type = "button";
  button.className = "row-icon-button";
  button.title = enabled ? "参与自动切换，点击后跳过此令牌" : "已跳过此令牌，点击后恢复自动切换";
  button.setAttribute("aria-label", button.title);
  const image = document.createElement("img");
  image.className = "profile-auto-switch-icon";
  image.src = enabled ? "/icons/auto.svg" : "/icons/skip.svg";
  image.alt = "";
  button.appendChild(image);
  button.addEventListener("click", handler);
  return button;
}

async function setProfileAutoSwitch(id, currentlySkipped, button) {
  setButtonLoading(button, true);
  try {
    await SetProfileAutoSwitch(id, currentlySkipped);
    await loadState();
    toast(currentlySkipped ? "已恢复参与自动切换" : "自动切换将跳过此令牌");
  } catch (error) {
    toast(errorMessage(error), true);
    setButtonLoading(button, false);
  }
}

function actionButton(label, className, title, iconName, handler, disabled = false) {
  const button = document.createElement("button");
  button.type = "button";
  button.className = className;
  button.appendChild(icon(iconName));
  if (label) {
    const text = document.createElement("span");
    text.textContent = label;
    text.dataset.buttonLabel = "";
    button.appendChild(text);
  }
  button.title = title;
  button.setAttribute("aria-label", title);
  button.disabled = disabled;
  button.addEventListener("click", handler);
  return button;
}

function providerInitial(name) {
  const first = Array.from((name || "C").trim())[0] || "C";
  return /^[A-Za-z0-9]$/.test(first) ? first.toUpperCase() : "API";
}

function showView(name) {
  app.view = name;
  $("profilesView").classList.toggle("hidden", name !== "profiles");
  $("editorView").classList.toggle("hidden", name !== "editor");
  $("settingsView").classList.toggle("hidden", name !== "settings");
  window.scrollTo(0, 0);
}

function openEditor(id = null) {
  app.selectedId = id;
  app.isNew = !id;
  app.dirty = false;
  const profile = id ? app.state.profiles.find((item) => item.id === id) : null;
  $("editorTitle").textContent = profile ? "编辑代理 API" : "新建代理 API";
  $("profileCategory").value = profile?.category || "codex";
  $("profileName").value = profile?.name || "";
  $("profileNote").value = profile?.note || "";
  $("baseUrl").value = profile?.baseUrl || "";
  $("apiKey").value = profile?.apiKey || "";
  const dogeKey = profile?.source === "doge";
  $("apiKey").readOnly = dogeKey;
  $("apiKey").setAttribute("aria-readonly", String(dogeKey));
  $("apiKey").title = dogeKey ? "二狗子令牌密钥由远端管理，不能修改" : "";
  $("headers").value = profile && Object.keys(profile.headers || {}).length ? JSON.stringify(profile.headers, null, 2) : "";
  app.editorModels = (profile?.models || []).map((model) => ({
    id: model.id || "", name: model.name || model.id || "", ownedBy: model.ownedBy || "", contextWindow: Number(model.contextWindow) > 0 ? Number(model.contextWindow) : 0,
  }));
  app.editorDefaultModel = profile?.defaultModel || "";
  setModelManagerStatus("");
  renderModelManager();
  $("activeBadge").classList.toggle("hidden", !profile?.active);
  $("activateProfile").disabled = !profile || profile.active;
  $("testProfile").disabled = !profile;
  $("deleteProfile").disabled = !profile;
  updatePreview();
  showView("editor");
  setTimeout(() => $("profileName").focus(), 0);
}

function readHeadersField() {
  let headers = {};
  const headerText = $("headers").value.trim();
  if (!headerText) return headers;
  try {
    headers = JSON.parse(headerText);
    if (!headers || Array.isArray(headers) || typeof headers !== "object") throw new Error();
    if (!Object.values(headers).every((value) => typeof value === "string")) throw new Error();
  } catch {
    throw new Error("额外请求头必须是值为字符串的 JSON 对象");
  }
  return headers;
}

function renderModelManager() {
  const rows = $("modelRows");
  const empty = $("modelEmpty");
  if (!rows || !empty) return;
  rows.replaceChildren();
  const models = Array.isArray(app.editorModels) ? app.editorModels : [];
  empty.classList.toggle("hidden", models.length > 0);
  for (const [index, model] of models.entries()) {
    const row = document.createElement("div");
    row.className = "model-row";
    const fields = [
      ["id", "模型 ID", "text"],
      ["name", "显示名称", "text"],
      ["ownedBy", "归属（可选）", "text"],
      ["contextWindow", "可选", "number"],
    ];
    for (const [field, placeholder, type] of fields) {
      const input = document.createElement("input");
      input.type = type;
      input.value = model[field] || "";
      input.placeholder = placeholder;
      input.dataset.modelField = field;
      input.setAttribute("aria-label", `${placeholder} ${index + 1}`);
      if (type === "number") {
        input.min = "0";
        input.step = "1";
        input.inputMode = "numeric";
      }
      input.addEventListener("input", () => {
        const previousID = app.editorModels[index][field];
        app.editorModels[index][field] = type === "number" ? (input.value ? Number(input.value) : 0) : input.value;
        if (field === "id" && app.editorDefaultModel === previousID) app.editorDefaultModel = input.value;
        app.dirty = true;
      });
      row.appendChild(input);
    }
    const defaultInput = document.createElement("input");
    defaultInput.type = "radio";
    defaultInput.name = "defaultModel";
    defaultInput.className = "model-default";
    defaultInput.checked = app.editorDefaultModel === model.id && Boolean(model.id);
    defaultInput.title = "设为默认模型";
    defaultInput.setAttribute("aria-label", `将 ${model.id || `模型 ${index + 1}`} 设为默认`);
    defaultInput.addEventListener("change", () => {
      if (defaultInput.checked) {
        app.editorDefaultModel = app.editorModels[index].id.trim();
        app.dirty = true;
      }
    });
    const remove = document.createElement("button");
    remove.type = "button";
    remove.className = "model-delete";
    remove.title = "删除模型";
    remove.setAttribute("aria-label", `删除 ${model.id || `模型 ${index + 1}`}`);
    remove.appendChild(icon("trash-2"));
    remove.addEventListener("click", () => {
      const deletedID = app.editorModels[index]?.id;
      app.editorModels.splice(index, 1);
      if (deletedID && app.editorDefaultModel === deletedID) app.editorDefaultModel = app.editorModels[0]?.id || "";
      app.dirty = true;
      renderModelManager();
    });
    row.append(defaultInput, remove);
    rows.appendChild(row);
  }
}

function setModelManagerStatus(message, error = false) {
  const node = $("modelManagerStatus");
  if (!node) return;
  node.textContent = message || "";
  node.classList.toggle("is-error", error);
  node.classList.toggle("is-success", Boolean(message) && !error);
}

function addEditorModel() {
  app.editorModels.push({ id: "", name: "", ownedBy: "", contextWindow: 0 });
  app.dirty = true;
  setModelManagerStatus("");
  renderModelManager();
  const last = $("modelRows").lastElementChild?.querySelector("input[data-model-field=\"id\"]");
  last?.focus();
}

async function fetchEditorModels(button = null) {
  let headers;
  try {
    headers = readHeadersField();
  } catch (error) {
    toast(errorMessage(error), true);
    return;
  }
  setButtonLoading(button, true, "获取中...");
  try {
    const models = await FetchProfileModels({
      baseUrl: $("baseUrl").value.trim(),
      apiKey: $("apiKey").value.trim(),
      headers,
    });
    app.editorModels = (models || []).map((model) => ({ id: model.id || "", name: model.name || model.id || "", ownedBy: model.ownedBy || "", contextWindow: Number(model.contextWindow) > 0 ? Number(model.contextWindow) : 0 }));
    if (!app.editorModels.some((model) => model.id === app.editorDefaultModel)) app.editorDefaultModel = app.editorModels[0]?.id || "";
    app.dirty = true;
    renderModelManager();
    setModelManagerStatus(`已获取 ${app.editorModels.length} 个模型，保存后用于客户端配置`);
  } catch (error) {
    setModelManagerStatus(errorMessage(error), true);
    toast(errorMessage(error), true);
  } finally {
    setButtonLoading(button, false);
  }
}

async function leaveEditor() {
  if (app.dirty && !(await showConfirmDialog("当前修改尚未保存，确定返回吗？", { title: "未保存的修改" }))) return;
  app.dirty = false;
  showView("profiles");
}

function updatePreview() {
  const category = $("profileCategory").value || "codex";
  $("previewUrl").textContent = "请求地址：" + (app.state?.proxyUrls?.[category] || app.state?.proxyUrl || "-");
  $("previewToken").textContent = "密钥：" + (app.state?.localAccessToken ? "********" : "-");
}

async function saveProfile(event) {
  event.preventDefault();
  let headers;
  try {
    headers = readHeadersField();
  } catch (error) {
    toast(errorMessage(error), true);
    return;
  }
  const models = (app.editorModels || []).map((model) => ({
    id: String(model.id || "").trim(), name: String(model.name || "").trim(), ownedBy: String(model.ownedBy || "").trim(), contextWindow: Number(model.contextWindow) > 0 ? Number(model.contextWindow) : 0,
  }));
  if (models.some((model) => !model.id)) {
    toast("模型 ID 不能为空", true);
    return;
  }
  const modelIDs = new Set();
  for (const model of models) {
    if (modelIDs.has(model.id)) {
      toast(`模型 ID 重复：${model.id}`, true);
      return;
    }
    modelIDs.add(model.id);
  }
  if (app.editorDefaultModel && !modelIDs.has(app.editorDefaultModel)) {
    toast("默认模型不在模型目录中", true);
    return;
  }
  const oldIDs = new Set(app.state.profiles.map((profile) => profile.id));
  const existingProfile = app.state.profiles.find((profile) => profile.id === app.selectedId);
  const payload = {
    id: app.isNew ? "" : app.selectedId,
    source: existingProfile?.source || "custom",
    category: $("profileCategory").value,
    name: $("profileName").value.trim(),
    note: $("profileNote").value.trim(),
    baseUrl: $("baseUrl").value.trim(),
    apiKey: $("apiKey").value.trim(),
    headers,
    models,
    defaultModel: app.editorDefaultModel,
  };
  const button = $("profileForm").querySelector("button[type=submit]");
  setButtonLoading(button, true, "保存中...");
  try {
    await SaveProfile(payload);
    app.dirty = false;
    await loadState();
    if (!payload.id) {
      app.selectedId = app.state.profiles.find((profile) => !oldIDs.has(profile.id))?.id || null;
      app.isNew = false;
    }
    toast("代理 API 配置已保存");
    showView("profiles");
  } catch (error) {
    toast(errorMessage(error), true);
  } finally {
    setButtonLoading(button, false);
  }
}

async function activateProfile(id, button = null) {
  const profile = app.state?.profiles?.find((item) => item.id === id);
  if (!profile) {
    toast("代理 API 不存在", true);
    return;
  }
  await beginActivation({ category: profile.category, profileId: id, tokenId: 0, button });
}

// 本地启用映射已经由后端持久化成功，先同步当前视图，避免等待下一次状态轮询。
function applyLocalActivation(profileID) {
  const profile = app.state?.profiles?.find((item) => item.id === profileID);
  if (!profile) return;
  if (!app.state.activeProfiles) app.state.activeProfiles = {};
  app.state.activeProfiles[profile.category] = profile.id;
  for (const item of app.state.profiles || []) {
    item.active = item.category === profile.category && item.id === profile.id;
  }
  for (const token of app.state.doge?.tokens || []) {
    const localProfile = findLocalDogeProfile(token);
    token.active = Boolean(localProfile && app.state.activeProfiles[localProfile.category] === localProfile.id);
  }
}

async function testProfile(id, button = null) {
  setButtonLoading(button, true, "测试中...");
  try {
    const result = await TestProfile(id);
    const message = result.ok
      ? "连接成功 · HTTP " + result.status + " · " + result.durationMs + " ms"
      : "已连接，上游返回 HTTP " + result.status;
    toast(message, !result.ok);
  } catch (error) {
    toast(errorMessage(error), true);
  } finally {
    setButtonLoading(button, false);
  }
}

async function deleteProfile(id, button = null) {
  const profile = app.state.profiles.find((item) => item.id === id);
  if (!profile || !(await showConfirmDialog("确定删除“" + nonHomeProfileName(profile) + "”吗？", { title: "删除代理 API", danger: true }))) return;
  setButtonLoading(button, true, "删除中...");
  try {
    await DeleteProfile(id);
    app.dirty = false;
    await loadState();
    showView("profiles");
    toast("代理 API 已删除");
  } catch (error) {
    toast(errorMessage(error), true);
  } finally {
    setButtonLoading(button, false);
  }
}

function openSettings(tab = "general") {
  setSettingsTab(tab);
  showView("settings");
}

function setSettingsTab(tab) {
  app.settingsTab = tab;
  document.querySelectorAll("#settingsTabs button").forEach((button) => {
    button.classList.toggle("active", button.dataset.tab === tab);
  });
  for (const name of ["general", "network", "taskNotification", "connection", "advanced", "activity", "about"]) {
    $(name + "Panel").classList.toggle("hidden", name !== tab);
  }
}

function renderClientConfigs() {
  const rows = $("clientConfigRows");
  if (!rows) return;
  const focusedInput = document.activeElement?.dataset?.clientCategory || "";
  const focusedValue = focusedInput ? document.activeElement.value : "";
  rows.replaceChildren();
  for (const client of app.state?.clientConfigs || []) {
    const row = document.createElement("div");
    row.className = "client-config-row";
    const info = document.createElement("div");
    info.className = "client-config-info";
    const title = document.createElement("strong");
    title.textContent = client.label;
    const status = document.createElement("small");
    status.textContent = client.statusText || "未检测到配置";
    status.className = `client-config-status status-${client.status || "not_detected"}`;
    info.append(title, status);
    const control = document.createElement("div");
    control.className = "client-config-control";
    const input = document.createElement("input");
    input.type = "text";
    input.value = Object.prototype.hasOwnProperty.call(app.clientConfigDrafts, client.category)
      ? app.clientConfigDrafts[client.category]
      : (client.configDir || "");
    input.placeholder = client.configDir || "未检测到默认目录";
    input.dataset.clientCategory = client.category;
    input.setAttribute("aria-label", `${client.label} 配置目录`);
    input.readOnly = client.status === "unsupported";
    input.addEventListener("input", () => {
      app.clientConfigDrafts[client.category] = input.value;
    });
    const choose = document.createElement("button");
    choose.type = "button";
    choose.className = "secondary-button compact-button";
    choose.append(icon("edit"), Object.assign(document.createElement("span"), { textContent: "选择路径" }));
    choose.disabled = client.status === "unsupported";
    choose.addEventListener("click", async () => {
      try {
        const selected = await SelectDirectory(input.value.trim());
        if (!selected) return;
        input.value = selected;
        app.clientConfigDrafts[client.category] = selected;
        setButtonLoading(choose, true, "保存中...");
        await SetClientConfigPath(client.category, selected);
        if (app.clientConfigDrafts[client.category] === selected) delete app.clientConfigDrafts[client.category];
        await loadState();
        toast(`${client.label} 配置目录已保存`);
      } catch (error) {
        toast(errorMessage(error), true);
      } finally {
        setButtonLoading(choose, false);
      }
    });
    const save = document.createElement("button");
    save.type = "button";
    save.className = "secondary-button compact-button";
    save.dataset.clientCategory = client.category;
    save.append(icon("save"), Object.assign(document.createElement("span"), { textContent: "保存" }));
    save.disabled = client.status === "unsupported";
    save.addEventListener("click", async () => {
      const directory = input.value.trim();
      app.clientConfigDrafts[client.category] = directory;
      setButtonLoading(save, true, "保存中...");
      try {
        await SetClientConfigPath(client.category, directory);
        if (app.clientConfigDrafts[client.category] === directory) delete app.clientConfigDrafts[client.category];
        await loadState();
        toast(`${client.label} 配置目录已保存`);
      } catch (error) {
        toast(errorMessage(error), true);
      } finally {
        setButtonLoading(save, false);
      }
    });
    control.append(input, choose, save);
    row.append(info, control);
    const skipLabel = document.createElement("label");
    skipLabel.className = "client-config-skip";
    const skip = document.createElement("input");
    skip.type = "checkbox";
    skip.checked = Object.prototype.hasOwnProperty.call(app.clientConfigSkipDrafts, client.category)
      ? app.clientConfigSkipDrafts[client.category]
      : Boolean(client.skipConfigReplacement);
    skip.disabled = client.status === "unsupported";
    skip.setAttribute("aria-label", `${client.label} 跳过配置文件替换`);
    skip.addEventListener("change", async () => {
      const value = skip.checked;
      app.clientConfigSkipDrafts[client.category] = value;
      try {
        await SetClientConfigSkip(client.category, value);
        if (app.clientConfigSkipDrafts[client.category] === value) delete app.clientConfigSkipDrafts[client.category];
        await loadState();
        toast(`${client.label} 的跳过配置文件替换设置已保存`);
      } catch (error) {
        delete app.clientConfigSkipDrafts[client.category];
        skip.checked = !skip.checked;
        toast(errorMessage(error), true);
      }
    });
    skipLabel.append(skip, Object.assign(document.createElement("span"), { textContent: "跳过配置文件替换" }));
    row.append(skipLabel);
    rows.appendChild(row);
    if (client.category === focusedInput) {
      const restored = row.querySelector("input[data-client-category]");
      if (restored) restored.value = focusedValue;
    }
  }
}

function renderPreferences() {
  const preferences = app.state.preferences || {};
  const preserveDraft = app.preferencesDirty;
  if (!preserveDraft) {
    $("closeToTray").checked = preferences.closeToTray;
    $("launchAtStartup").checked = preferences.launchAtStartup;
    $("startHidden").checked = preferences.startHidden;
  }
  if (!app.tokenSwitchDirty) renderTokenSwitchSettings();
  if (!app.dogeAlertDirty) renderDogeAlertSettings();
  const visible = preserveDraft
    ? new Set(Array.from(document.querySelectorAll("#visibleCategories input[data-category]:checked"), (input) => input.dataset.category))
    : visibleCategorySet();
  // 保存开关后会重新读取状态并重建列表，重建前后保持内容滚动位置，避免浏览器滚动锚点跳动。
  const settingsContent = $("settingsContent");
  const scrollTop = settingsContent?.scrollTop || 0;
  const visibleRows = $("visibleCategories");
  visibleRows.replaceChildren();
  for (const category of categoryOptions) {
    const row = document.createElement("label");
    row.className = "compact-check-option category-visibility-option";
    const input = document.createElement("input");
    input.type = "checkbox";
    input.checked = visible.has(category);
    input.dataset.category = category;
    input.setAttribute("aria-label", `显示${categoryLabel(category)}类别`);
    input.addEventListener("change", () => {
      markPreferencesDirty();
      savePreferences();
    });
    const title = document.createElement("span");
    title.textContent = categoryLabel(category);
    row.append(input, title);
    visibleRows.appendChild(row);
  }
  if (!preserveDraft) {
    $("defaultSource").value = preferences.defaultSource || "";
    $("defaultCategory").value = preferences.defaultCategory || "";
    $("restoreViewMode").value = preferences.restoreViewMode || "current";
  }
  if (settingsContent) settingsContent.scrollTop = scrollTop;
}

// 通用设置中的控件会立即保存，但请求返回前仍可能收到后台状态刷新；草稿版本用于避免旧响应覆盖较新的编辑。
function markPreferencesDirty() {
  app.preferencesDirty = true;
  app.preferencesDraftRevision += 1;
}

// 令牌异常处理与通用偏好独立保存，避免其中一方的异步响应覆盖另一方未保存的输入。
function markTokenSwitchDirty() {
  app.tokenSwitchDirty = true;
  app.tokenSwitchDraftRevision += 1;
}

// 余额和套餐提醒独立保存，后台刷新只能更新已提交的区块。
function markDogeAlertDirty() {
  app.dogeAlertDirty = true;
  app.dogeAlertDraftRevision += 1;
}

function renderNetworkDraftState() {
  const preserveDraft = app.networkModeDirty || app.networkProxyDirty || app.networkPortDirty;
  if (!preserveDraft) {
    document.querySelectorAll("#networkModes button").forEach((button) => {
      button.classList.toggle("active", button.dataset.mode === app.state.network.mode);
    });
    $("manualProxyRow").classList.toggle("hidden", app.state.network.mode !== "manual");
    $("manualProxy").value = app.state.network.proxyUrl || "";
    $("proxyPort").value = String(app.state.proxyPort || 8765);
  }
  return preserveDraft;
}

const taskNotificationEventInputs = {
  taskCompleted: "taskNotificationTaskCompleted",
  taskAborted: "taskNotificationTaskAborted",
  tokenRequestFailed: "taskNotificationTokenRequestFailed",
  tokenAutoSwitched: "taskNotificationTokenAutoSwitched",
  tokenAutoSwitchFailed: "taskNotificationTokenAutoSwitchFailed",
  accountBalanceLow: "taskNotificationAccountBalanceLow",
  subscriptionBalanceLow: "taskNotificationSubscriptionBalanceLow",
};

// 通知设置只保存用户完整填写的 URL 和事件选择；发送时不会补充或改写 URL 参数。
function renderTaskNotification() {
  const notification = app.state?.taskNotification || {};
  if (!app.taskNotificationDirty) {
    const events = notification.events || {
      taskCompleted: true,
      taskAborted: true,
      tokenRequestFailed: true,
      tokenAutoSwitched: true,
      tokenAutoSwitchFailed: true,
      accountBalanceLow: true,
      subscriptionBalanceLow: true,
    };
    $("taskNotificationEnabled").checked = Boolean(notification.enabled);
    $("taskNotificationWebhookUrl").value = notification.webhookUrl || "";
    for (const [key, id] of Object.entries(taskNotificationEventInputs)) $(id).checked = Boolean(events[key]);
    $("taskNotificationIdleGraceSeconds").value = String(notification.idleGraceSeconds || 5);
    $("taskNotificationRequestTimeoutSeconds").value = String(notification.requestTimeoutSeconds || 10);
    $("taskNotificationMaxAttempts").value = String(notification.maxAttempts || 0);
  }
  const status = notification.status || {};
  $("taskNotificationQueueState").textContent = `候选 ${status.pending || 0} · 待投递 ${status.outbox || 0} · 失败 ${status.dead || 0}`;
  $("taskNotificationError").textContent = status.lastError || "";
}

// 后台状态刷新只更新队列信息；表单编辑中的值必须保留到用户保存或离开本次操作。
function markTaskNotificationDirty() {
  app.taskNotificationDirty = true;
  app.taskNotificationDraftRevision += 1;
}

function taskNotificationPayload() {
  const events = {};
  for (const [key, id] of Object.entries(taskNotificationEventInputs)) events[key] = $(id).checked;
  return {
    enabled: $("taskNotificationEnabled").checked,
    webhookUrl: $("taskNotificationWebhookUrl").value.trim(),
    events,
    idleGraceSeconds: Number($("taskNotificationIdleGraceSeconds").value),
    requestTimeoutSeconds: Number($("taskNotificationRequestTimeoutSeconds").value),
    maxAttempts: Number($("taskNotificationMaxAttempts").value),
  };
}

async function saveTaskNotification(button = $("saveTaskNotification"), successMessage = "任务完成通知设置已保存") {
  const draftRevision = app.taskNotificationDraftRevision;
  setButtonLoading(button, true, "保存中...");
  try {
    await SetTaskNotification(taskNotificationPayload());
    if (draftRevision === app.taskNotificationDraftRevision) app.taskNotificationDirty = false;
    await loadState();
    toast(successMessage);
    return true;
  } catch (error) {
    await loadState();
    toast(errorMessage(error), true);
    return false;
  } finally {
    setButtonLoading(button, false);
  }
}

async function testTaskNotification() {
  const button = $("testTaskNotification");
  if (!(await saveTaskNotification(button, "设置已保存，正在测试通知..."))) return;
  setButtonLoading(button, true, "测试中...");
  try {
    await TestTaskNotification();
    toast("测试通知已发送");
  } catch (error) {
    toast(errorMessage(error), true);
  } finally {
    setButtonLoading(button, false);
  }
}

function renderTokenSwitchSettings() {
  const settings = app.state?.tokenSwitch || {};
  document.querySelectorAll("#tokenSwitchMode button").forEach((button) => {
    button.classList.toggle("active", button.dataset.mode === (settings.mode || "prompt"));
  });
  for (const id of ["trigger401", "trigger403", "trigger5xx", "triggerNetwork", "triggerDirectoryInvalid", "triggerDirectoryMissing"]) {
    $(id).checked = Boolean(settings[id]);
  }
  $("failoverLoop").checked = Boolean(settings.loop);
  $("authFailureThreshold").value = String(settings.authFailureThreshold || 5);
  $("upstreamFailureThreshold").value = String(settings.upstreamFailureThreshold || 5);
  $("upstreamFailureWindowMinutes").value = String(settings.upstreamFailureWindowMinutes || 3);
}

function renderDogeAlertSettings() {
  const doge = app.state?.doge || {};
  $("balanceAlertEnabled").checked = doge.balanceAlertEnabled !== false;
  $("subscriptionAlertEnabled").checked = doge.subscriptionAlertEnabled !== false;
  $("balanceAlertThresholdUSD").value = Number(doge.balanceAlertThresholdUsd || 1).toFixed(2);
  $("subscriptionAlertThresholdUSD").value = Number(doge.subscriptionAlertThresholdUsd || 1).toFixed(2);
}

async function saveTokenSwitchSettings() {
  const draftRevision = app.tokenSwitchDraftRevision;
  const mode = document.querySelector("#tokenSwitchMode button.active")?.dataset.mode || "prompt";
  const payload = {
    mode,
    trigger401: $("trigger401").checked,
    trigger403: $("trigger403").checked,
    trigger5xx: $("trigger5xx").checked,
    triggerNetwork: $("triggerNetwork").checked,
    triggerDirectoryInvalid: $("triggerDirectoryInvalid").checked,
    triggerDirectoryMissing: $("triggerDirectoryMissing").checked,
    authFailureThreshold: Number($("authFailureThreshold").value),
    upstreamFailureThreshold: Number($("upstreamFailureThreshold").value),
    upstreamFailureWindowMinutes: Number($("upstreamFailureWindowMinutes").value),
    loop: $("failoverLoop").checked,
  };
  try {
    await SetTokenSwitchSettings(payload);
    if (draftRevision === app.tokenSwitchDraftRevision) app.tokenSwitchDirty = false;
    await loadState();
    toast("令牌异常处理设置已保存");
  } catch (error) {
    await loadState();
    toast(errorMessage(error), true);
  }
}

async function saveDogeAlertSettings() {
  const draftRevision = app.dogeAlertDraftRevision;
  const payload = {
    balanceEnabled: $("balanceAlertEnabled").checked,
    balanceThresholdUsd: Number($("balanceAlertThresholdUSD").value),
    subscriptionEnabled: $("subscriptionAlertEnabled").checked,
    subscriptionThresholdUsd: Number($("subscriptionAlertThresholdUSD").value),
  };
  try {
    await SetDogeAlertSettings(payload);
    if (draftRevision === app.dogeAlertDraftRevision) app.dogeAlertDirty = false;
    await loadState();
    toast("余额和套餐提醒设置已保存");
  } catch (error) {
    await loadState();
    toast(errorMessage(error), true);
  }
}

async function savePreferences() {
  const draftRevision = app.preferencesDraftRevision;
  const visibleCategories = Array.from(document.querySelectorAll("#visibleCategories input[data-category]:checked"), (input) => input.dataset.category);
  if (!visibleCategories.length) {
    app.preferencesDirty = false;
    renderPreferences();
    toast("至少保留一个主页类别", true);
    return;
  }
  const defaultCategory = $("defaultCategory").value;
  if (defaultCategory && !visibleCategories.includes(defaultCategory)) {
    renderPreferences();
    toast("默认类别必须处于主页显示状态", true);
    return;
  }
  const payload = {
    closeToTray: $("closeToTray").checked,
    launchAtStartup: $("launchAtStartup").checked,
    startHidden: $("startHidden").checked,
    visibleCategories,
    defaultSource: $("defaultSource").value,
    defaultCategory,
    restoreViewMode: $("restoreViewMode").value,
  };
  try {
    await SetPreferences(payload);
    if (draftRevision === app.preferencesDraftRevision) app.preferencesDirty = false;
    await loadState();
    toast("通用设置已保存");
  } catch (error) {
    await loadState();
    toast(errorMessage(error), true);
  }
}

function renderNetwork() {
  renderNetworkDraftState();
  const system = app.state.systemProxy;
  $("systemProxyState").textContent = system.enabled ? "已检测到 Windows 系统代理" : "使用 Windows 当前路由";
  $("networkNote").textContent = system.note || "";
}

async function setNetworkMode(mode) {
  app.networkModeDirty = true;
  app.networkDraftRevision += 1;
  const proxyUrl = mode === "manual" ? $("manualProxy").value.trim() : "";
  if (mode === "manual" && !proxyUrl) {
    document.querySelectorAll("#networkModes button").forEach((button) => button.classList.toggle("active", button.dataset.mode === mode));
    $("manualProxyRow").classList.remove("hidden");
    $("manualProxy").focus();
    return;
  }
  document.querySelectorAll("#networkModes button").forEach((button) => button.classList.toggle("active", button.dataset.mode === mode));
  $("manualProxyRow").classList.toggle("hidden", mode !== "manual");
  await saveNetwork(mode, proxyUrl);
}

async function saveNetwork(mode = "manual", proxyUrl = $("manualProxy").value.trim()) {
  const draftRevision = app.networkDraftRevision;
  const button = $("saveNetwork");
  setButtonLoading(button, true, "保存中...");
  try {
    await SetNetwork({ mode, proxyUrl });
    if (draftRevision === app.networkDraftRevision) {
      app.networkModeDirty = false;
      app.networkProxyDirty = false;
    }
    await loadState();
    toast("网络出口设置已保存");
  } catch (error) {
    toast(errorMessage(error), true);
  } finally {
    setButtonLoading(button, false);
  }
}

async function saveProxyPort() {
  const input = $("proxyPort");
  const button = $("saveProxyPort");
  const port = Number(input.value);
  if (!Number.isInteger(port) || port < 1 || port > 65535) {
    toast("监听端口必须是 1 到 65535 之间的整数", true);
    input.focus();
    return;
  }
  const draftRevision = app.networkDraftRevision;
  setButtonLoading(button, true, "保存中...");
  try {
    await SetProxyPort(port);
    if (draftRevision === app.networkDraftRevision) app.networkPortDirty = false;
    await loadState();
    toast("监听端口已更新");
  } catch (error) {
    toast(errorMessage(error), true);
  } finally {
    setButtonLoading(button, false);
  }
}

function renderConnection() {
  const doge = app.state.doge || {};
  const bound = Boolean(doge.bound);
  const syncing = bound && isDogeSyncing();
  const syncError = String(doge.lastSyncError || "").trim();
  const user = doge.user || {};
  const account = doge.account || {};
  const accountUnavailable = Boolean(syncError && !doge.lastSyncAt);
  const accountValue = (value, fallback = "-") => syncing ? "同步中..." : (accountUnavailable ? "同步失败" : (value || fallback));
  $("connectionPanel").classList.toggle("doge-is-bound", bound);
  $("dogeUnboundView").classList.toggle("hidden", bound);
  $("dogeAccessToken").value = bound ? "" : $("dogeAccessToken").value;
  setLoadingText($("dogeConnectionState"), syncing, syncing ? "同步中..." : (syncError ? "同步失败" : (bound ? "已连接" : "未绑定")));
  $("dogeConnectionState").classList.toggle("is-connected", bound && !syncing && !syncError);
  $("dogeBoundView").classList.toggle("hidden", !bound);
  const action = $("dogeConnectionAction");
  if (action.dataset.loading !== "true") {
    action.classList.toggle("primary-button", !bound);
    action.classList.toggle("danger-button", bound);
    action.querySelector(".icon").className = "icon icon-" + (bound ? "x" : "check");
    action.querySelector("[data-doge-action-label]").textContent = bound ? "解除绑定" : "验证并绑定";
    action.setAttribute("aria-label", bound ? "解除二狗子绑定" : "验证并绑定二狗子");
  }
  $("dogeAccountUserID").textContent = accountValue(account.userId);
  $("dogeAccountNickname").textContent = accountValue(account.nickname || user.display_name || user.displayName || user.username);
  $("dogeAccountEmail").textContent = accountValue(account.email || user.email);
  $("dogeAccountBalance").textContent = accountValue(formatDogeUSD(account.balanceUsd), "$0.00");
  $("dogeAccountUsed").textContent = accountValue(formatDogeUSD(account.usedUsd), "$0.00");
  $("dogeAccountRequests").textContent = accountValue(formatTokens(account.requestCount), "0");
  setLoadingText($("dogeAccountState"), syncing, syncing ? "同步中... 请稍候" : (syncError ? "账户数据同步失败，当前显示缓存" : "账户信息来自最近一次同步"));
  $("dogeSyncRow").classList.toggle("hidden", !bound);
  $("dogeSyncInterval").value = String(doge.syncIntervalMinutes || 3);
  $("dogeSyncError").textContent = !syncing && doge.lastSyncError ? "同步失败：" + doge.lastSyncError : "";
  $("dogeSyncError").classList.toggle("hidden", syncing || !doge.lastSyncError);
}

function renderRequests() {
  const rows = $("requestRows");
  rows.replaceChildren();
  renderUsageProfileOptions();
  const allRequests = app.state.requests || [];
  const requests = allRequests.filter((request) => usageRequestMatches(request));
  $("requestCount").textContent = requests.length === allRequests.length
    ? requests.length + " 条请求"
    : requests.length + " / " + allRequests.length + " 条请求";
  $("emptyRequests").textContent = allRequests.length ? "当前范围暂无请求" : "暂无请求";
  $("emptyRequests").classList.toggle("hidden", requests.length > 0);
  renderUsageSummary();
  for (const request of requests) {
    const row = document.createElement("tr");
    const reported = request.usageStatus === "reported";
    row.append(
      cell(formatRequestTime(request.startedAt)),
      cell(nonHomeProfileName((app.state.profiles || []).find((profile) => profile.id === request.profileId) || { name: request.profile || "-" })),
      cell(request.model || "-"),
      cell(request.method + " " + request.path, "path"),
      cell(reported ? formatTokens(request.inputTokens) : "-", "token-value"),
      cell(reported ? formatTokens(request.outputTokens) : "-", "token-value"),
      cell(reported ? formatTokens(request.cachedTokens) : "-", "token-value"),
      cell(reported ? formatTokens(request.totalTokens) : "未上报", reported ? "token-value total-token" : "usage-missing"),
      cell(String(request.status), request.status >= 200 && request.status < 400 ? "status-ok" : "status-error"),
      cell(request.durationMs + " ms"),
    );
    rows.appendChild(row);
  }
}

function renderUsageProfileOptions() {
  const select = $("usageProfile");
  const names = new Map((app.state.profiles || []).map((profile) => [profile.id, nonHomeProfileName(profile)]));
  for (const request of app.state.requests || []) {
    if (request.profileId && !names.has(request.profileId)) names.set(request.profileId, request.profile || "已删除中转站");
  }
  const current = app.usageProfile;
  select.replaceChildren(new Option("全部代理 API", ""));
  for (const [id, name] of names) select.appendChild(new Option(name, id));
  if (current && names.has(current)) select.value = current;
  else app.usageProfile = "";
}

function usageRequestMatches(request) {
  if (app.usageProfile && request.profileId !== app.usageProfile) return false;
  const days = usageRangeDays(app.usageRange);
  if (!days) return true;
  const start = new Date();
  start.setHours(0, 0, 0, 0);
  start.setDate(start.getDate() - (days - 1));
  return new Date(request.startedAt) >= start;
}

function usageRangeDays(range) {
  if (range === "today") return 1;
  if (range === "7d") return 7;
  if (range === "30d") return 30;
  return 0;
}

function renderUsageSummary() {
  const usage = app.state.usage || { total: {}, profiles: {}, days: {} };
  const aggregate = emptyUsageAggregate();
  const days = usageRangeDays(app.usageRange);
  if (!days) {
    addUsageAggregate(aggregate, app.usageProfile ? usage.profiles?.[app.usageProfile] : usage.total);
  } else {
    const date = new Date();
    date.setHours(12, 0, 0, 0);
    for (let offset = 0; offset < days; offset++) {
      const key = localDateKey(date);
      const day = usage.days?.[key];
      addUsageAggregate(aggregate, app.usageProfile ? day?.profiles?.[app.usageProfile] : day?.total);
      date.setDate(date.getDate() - 1);
    }
  }
  $("usageTotal").textContent = formatTokens(aggregate.totalTokens);
  $("usageInput").textContent = formatTokens(aggregate.inputTokens);
  $("usageOutput").textContent = formatTokens(aggregate.outputTokens);
  $("usageCached").textContent = formatTokens(aggregate.cachedTokens);
  $("usageRequests").textContent = formatTokens(aggregate.requests);
  $("usageReported").textContent = aggregate.requests && aggregate.reportedRequests !== aggregate.requests
    ? aggregate.reportedRequests + " 条已上报"
    : "";
}

function emptyUsageAggregate() {
  return { requests: 0, reportedRequests: 0, inputTokens: 0, outputTokens: 0, cachedTokens: 0, totalTokens: 0 };
}

function addUsageAggregate(target, source) {
  if (!source) return;
  for (const key of Object.keys(target)) target[key] += Number(source[key] || 0);
}

function localDateKey(date) {
  const year = date.getFullYear();
  const month = String(date.getMonth() + 1).padStart(2, "0");
  const day = String(date.getDate()).padStart(2, "0");
  return `${year}-${month}-${day}`;
}

function formatTokens(value) {
  return new Intl.NumberFormat("zh-CN").format(Number(value || 0));
}

function formatRequestTime(value) {
  const date = new Date(value);
  const days = usageRangeDays(app.usageRange);
  return days === 1
    ? date.toLocaleTimeString("zh-CN", { hour12: false })
    : date.toLocaleString("zh-CN", { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit", hour12: false });
}

async function clearUsage() {
  if (!(await showConfirmDialog("确定清空全部 Token 统计和最近请求明细吗？此操作无法恢复。", { title: "清空用量统计", danger: true }))) return;
  const button = $("clearUsage");
  setButtonLoading(button, true, "清空中...");
  try {
    await ClearUsage();
    await loadState();
    toast("用量统计已清空");
  } catch (error) {
    toast(errorMessage(error), true);
  } finally {
    setButtonLoading(button, false);
  }
}

function cell(text, className = "") {
  const td = document.createElement("td");
  td.textContent = text;
  td.title = text;
  if (className) td.className = className;
  return td;
}

async function copyText(text) {
  if (!text || text === "-") return;
  try {
    await navigator.clipboard.writeText(text);
    toast("已复制");
  } catch {
    toast("复制失败", true);
  }
}

function toast(message, isError = false) {
  const node = $("toast");
  clearTimeout(app.toastTimer);
  clearTimeout(app.toastCleanupTimer);
  node.replaceChildren(Object.assign(document.createElement("span"), { textContent: message }));
  node.className = "toast show" + (isError ? " error" : "");
  app.toastTimer = setTimeout(() => {
    app.toastTimer = null;
    // 淡出期间保留当前提示类型，避免错误提示在透明度过渡时短暂恢复成默认绿色。
    node.classList.remove("show");
    app.toastCleanupTimer = setTimeout(() => {
      app.toastCleanupTimer = null;
      if (!node.classList.contains("show")) node.className = "toast";
    }, 200);
  }, 3000);
}

function setDogeQuotaPopover(open) {
  const popover = $("dogeQuotaPopover");
  popover.classList.toggle("hidden", !open);
  $("dogeQuotaSummary").setAttribute("aria-expanded", String(open));
}

$("addProfile").addEventListener("click", () => openEditor());
$("emptyAdd").addEventListener("click", () => openEditor());
$("dogeQuotaSummary").addEventListener("click", () => setDogeQuotaPopover($("dogeQuotaPopover").classList.contains("hidden")));
$("dogeQuotaWrap").addEventListener("mouseenter", () => setDogeQuotaPopover(true));
$("dogeQuotaWrap").addEventListener("mouseleave", () => setDogeQuotaPopover(false));
$("dogeQuotaWrap").addEventListener("focusin", () => setDogeQuotaPopover(true));
$("dogeQuotaWrap").addEventListener("focusout", (event) => {
  if (!$("dogeQuotaWrap").contains(event.relatedTarget)) setDogeQuotaPopover(false);
});
$("openAnnouncements").addEventListener("click", () => setAnnouncementPanel($("announcementPanel").classList.contains("hidden")));
$("closeAnnouncements").addEventListener("click", () => setAnnouncementPanel(false));
$("markAnnouncementsRead").addEventListener("click", () => markAnnouncementsRead(true));
document.querySelectorAll("[data-announcement-tab]").forEach((button) => button.addEventListener("click", () => {
  app.announcementTab = button.dataset.announcementTab;
  renderAnnouncements();
}));
$("openDogeTopup").addEventListener("click", openDogeTopupModal);
$("closeDogeTopupModal").addEventListener("click", closeDogeTopupModal);
$("openDogeProfile").addEventListener("click", openDogeProfile);
$("bindOnboarding").addEventListener("click", bindOnboarding);
$("skipOnboarding").addEventListener("click", skipOnboarding);
$("dogeTopupPurchase").addEventListener("click", openDogePurchase);
$("submitDogeTopup").addEventListener("click", redeemDoge);
$("clientSetupSkip").addEventListener("click", () => resolveClientSetup(false));
$("clientSetupConfigure").addEventListener("click", () => resolveClientSetup(true));
$("closeClientSetupModal").addEventListener("click", closeClientSetupModal);
$("refreshDoge").addEventListener("click", async () => {
  const button = $("refreshDoge");
  setButtonLoading(button, true);
  app.localDogeSyncing = true;
  app.dogeRemoteSyncing = false;
  showDogeSyncToast("base");
  startDogeSyncProgressPolling();
  renderShell(); renderProfiles(); renderConnection(); renderAnnouncements();
  let synced = false;
  try {
    app.dogeCategoryDialogSignature = "";
    await SyncDoge();
    synced = true;
    toast("数据同步完成");
  } catch (error) {
    toast(errorMessage(error), true);
  } finally {
    app.localDogeSyncing = false;
    stopDogeSyncProgressPolling();
    app.dogeRemoteSyncing = false;
    await loadState();
    if (synced) openDogeCategoryDialog(true);
    setButtonLoading(button, false);
  }
});
document.querySelectorAll(".filter-option").forEach((button) => button.addEventListener("click", () => setFilter(button.closest(".filter-options").dataset.filterGroup, button.dataset.filterValue)));
$("openSettings").addEventListener("click", () => openSettings());
$("pendingDogeImport").addEventListener("click", () => openDogeCategoryDialog(true));
$("settingsBack").addEventListener("click", () => showView("profiles"));
$("updateAction").addEventListener("click", () => app.update.available ? runWindowsUpdate() : checkForUpdates(true));
$("chooseDataDirectory").addEventListener("click", async (event) => {
  const button = event.currentTarget;
  try {
    const selected = await SelectDirectory($("dataDirectory").value.trim());
    if (!selected) return;
    setButtonLoading(button, true, "迁移中...");
    await SetDataDirectory(selected);
    await loadState();
    toast("CodexRelay 配置文件路径已切换");
  } catch (error) {
    toast(errorMessage(error), true);
  } finally {
    setButtonLoading(button, false);
  }
});
$("editorBack").addEventListener("click", leaveEditor);
$("profileForm").addEventListener("submit", saveProfile);
$("profileForm").addEventListener("input", () => { app.dirty = true; });
$('fetchModels').addEventListener("click", (event) => fetchEditorModels(event.currentTarget));
$('addModel').addEventListener("click", addEditorModel);
$("baseUrl").addEventListener("input", updatePreview);
$("profileCategory").addEventListener("change", updatePreview);
$("copyPreviewUrl").addEventListener("click", () => {
  const category = $("profileCategory").value || "codex";
  copyText(app.state?.proxyUrls?.[category] || app.state?.proxyUrl || "-");
});
$("copyPreviewToken").addEventListener("click", () => copyText(app.state?.localAccessToken || "-"));
$("copyApiKey").addEventListener("click", () => copyText($("apiKey").value.trim()));
$("activateProfile").addEventListener("click", (event) => activateProfile(app.selectedId, event.currentTarget));
$("testProfile").addEventListener("click", () => testProfile(app.selectedId, $("testProfile")));
$("deleteProfile").addEventListener("click", (event) => deleteProfile(app.selectedId, event.currentTarget));
document.querySelectorAll("#settingsTabs button").forEach((button) => button.addEventListener("click", () => setSettingsTab(button.dataset.tab)));
document.querySelectorAll("#usageRanges button").forEach((button) => button.addEventListener("click", () => {
  app.usageRange = button.dataset.range;
  document.querySelectorAll("#usageRanges button").forEach((item) => item.classList.toggle("active", item === button));
  renderRequests();
}));
$("usageProfile").addEventListener("change", () => { app.usageProfile = $("usageProfile").value; renderRequests(); });
$("clearUsage").addEventListener("click", clearUsage);
for (const id of ["closeToTray", "launchAtStartup", "startHidden", "defaultSource", "defaultCategory", "restoreViewMode"]) $(id).addEventListener("change", () => {
  markPreferencesDirty();
  savePreferences();
});
document.querySelectorAll("#tokenSwitchMode button").forEach((button) => button.addEventListener("click", async () => {
  document.querySelectorAll("#tokenSwitchMode button").forEach((item) => item.classList.toggle("active", item === button));
  markTokenSwitchDirty();
  await saveTokenSwitchSettings();
}));
for (const id of ["failoverLoop", "trigger401", "trigger403", "trigger5xx", "triggerNetwork", "triggerDirectoryInvalid", "triggerDirectoryMissing", "authFailureThreshold", "upstreamFailureThreshold", "upstreamFailureWindowMinutes"]) $(id).addEventListener("change", () => {
  markTokenSwitchDirty();
  saveTokenSwitchSettings();
});
for (const id of ["authFailureThreshold", "upstreamFailureThreshold", "upstreamFailureWindowMinutes"]) $(id).addEventListener("input", markTokenSwitchDirty);
for (const id of ["balanceAlertThresholdUSD", "subscriptionAlertThresholdUSD"]) $(id).addEventListener("input", markDogeAlertDirty);
for (const id of ["balanceAlertEnabled", "balanceAlertThresholdUSD", "subscriptionAlertEnabled", "subscriptionAlertThresholdUSD"]) $(id).addEventListener("change", () => {
  markDogeAlertDirty();
  saveDogeAlertSettings();
});
document.querySelectorAll("#networkModes button").forEach((button) => button.addEventListener("click", () => setNetworkMode(button.dataset.mode)));
$("manualProxy").addEventListener("input", () => {
  app.networkProxyDirty = true;
  app.networkDraftRevision += 1;
});
$("proxyPort").addEventListener("input", () => {
  app.networkPortDirty = true;
  app.networkDraftRevision += 1;
});
$("saveNetwork").addEventListener("click", () => saveNetwork());
$("saveProxyPort").addEventListener("click", saveProxyPort);
$("saveTaskNotification").addEventListener("click", () => saveTaskNotification());
$("testTaskNotification").addEventListener("click", testTaskNotification);
for (const id of ["taskNotificationEnabled", "taskNotificationWebhookUrl", "taskNotificationIdleGraceSeconds", "taskNotificationRequestTimeoutSeconds", "taskNotificationMaxAttempts", ...Object.values(taskNotificationEventInputs)]) $(id).addEventListener("input", markTaskNotificationDirty);
$("taskNotificationEnabled").addEventListener("change", markTaskNotificationDirty);
for (const id of Object.values(taskNotificationEventInputs)) $(id).addEventListener("change", markTaskNotificationDirty);
$("dogeConnectionAction").addEventListener("click", async () => {
  const doge = app.state?.doge || {};
  const button = $("dogeConnectionAction");
  const binding = !doge.bound;
  let bound = false;
  if (doge.bound && !(await showConfirmDialog("解除绑定会清除本地二狗子目录，确定继续吗？", { title: "解除二狗子绑定", danger: true }))) return;
  if (binding) {
    app.localDogeSyncing = true;
    app.dogeRemoteSyncing = false;
    showDogeSyncToast("base");
    startDogeSyncProgressPolling();
    renderShell(); renderProfiles(); renderConnection(); renderAnnouncements();
  }
  setButtonLoading(button, true, binding ? "绑定中..." : "解除中...");
  try {
    if (doge.bound) {
      await UnbindDoge();
      await loadState();
      toast("二狗子已解除绑定");
    } else {
      await BindDoge($("dogeAccessToken").value);
      bound = true;
      toast("二狗子已验证并绑定");
    }
  } catch (error) {
    toast(errorMessage(error), true);
  } finally {
    if (binding) {
      app.localDogeSyncing = false;
      stopDogeSyncProgressPolling();
      app.dogeRemoteSyncing = false;
      await loadState();
      if (bound) openDogeCategoryDialog(true);
    }
    setButtonLoading(button, false);
    renderConnection();
  }
});
$("dogeSyncInterval").addEventListener("change", async () => {
  try { await SetDogeSyncInterval(Number($("dogeSyncInterval").value)); await loadState(); toast("自动同步间隔已更新"); } catch (error) { toast(errorMessage(error), true); }
});
$("closeDogeCategoryModal").addEventListener("click", closeDogeCategoryDialog);
$("saveDogeCategories").addEventListener("click", saveDogeCategoryAssignments);
$("confirmCancel").addEventListener("click", () => closeConfirmDialog(false));
$("confirmAccept").addEventListener("click", () => closeConfirmDialog(true));
document.querySelectorAll("[data-copy]").forEach((button) => button.addEventListener("click", () => copyText($(button.dataset.copy).textContent)));
document.addEventListener("keydown", (event) => {
  if (event.key !== "Escape") return;
  if (!$("onboardingModal").classList.contains("hidden")) return;
  if (!$("confirmModal").classList.contains("hidden")) { closeConfirmDialog(false); return; }
  setDogeQuotaPopover(false);
  setAnnouncementPanel(false);
  if (!$("dogeTopupModal").classList.contains("hidden")) closeDogeTopupModal();
  if (!$("dogeCategoryModal").classList.contains("hidden")) closeDogeCategoryDialog();
  if (!$("clientSetupModal").classList.contains("hidden")) closeClientSetupModal();
});
function restoreDefaultViewFilter() {
  applyDefaultViewFilter(true);
  renderShell();
  renderProfiles();
}

document.addEventListener("visibilitychange", () => {
  if (document.hidden) return;
  if (app.state?.preferences?.restoreViewMode === "default") restoreDefaultViewFilter();
  loadState();
});
// 托盘后台同步完成后由原生窗口发出状态事件；隐藏窗口可能不触发浏览器 visibilitychange，因此恢复时直接读取最新快照。
wails.Events.On("relay-state-changed", () => { if (app.view !== "editor") loadState(); });
wails.Events.On("relay-restore-default-view", () => {
  if (app.state) restoreDefaultViewFilter();
});
wails.Events.On("wails:updater:download-started", () => {
  app.update.installing = true;
  app.update.phase = "正在下载更新";
  renderUpdateStatus();
});
wails.Events.On("wails:updater:download-progress", (event) => {
  const progress = event?.data || event || {};
  app.update.written = Number(progress.written || 0);
  app.update.total = Number(progress.total || 0);
  renderUpdateStatus();
});
wails.Events.On("wails:updater:verifying", () => {
  app.update.phase = "正在校验更新文件";
  renderUpdateStatus();
});
wails.Events.On("wails:updater:installing", () => {
  app.update.phase = "正在准备替换程序";
  renderUpdateStatus();
});
wails.Events.On("wails:updater:update-ready", () => {
  app.update.phase = "更新已校验，正在重启";
  renderUpdateStatus();
});

loadState();
setInterval(() => { if (!document.hidden && app.view !== "editor") loadState(); }, 3000);
