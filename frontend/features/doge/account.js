import {
  BindDoge,
  GetState,
  OpenDogeProfile,
  OpenDogeTopup,
  RedeemDoge,
  SetDogeSyncInterval,
  SyncDoge,
  UnbindDoge,
} from "../../core/desktop-api.js";
import { $, icon } from "../../core/dom.js";
import { errorMessage, setButtonLoading, setLoadingText, toast } from "../../core/feedback.js";
import { showConfirmDialog, syncModalBody } from "../../core/modal.js";
import { runtimeState, serverState, setServerSnapshot } from "../../core/store.js";
import { createDogeAnnouncements } from "./announcements.js";
import { createDogeOnboarding } from "./onboarding.js";

export function createDogeAccount({
  loadState,
  renderShell,
  renderProfiles,
  renderConnection,
  openDogeCategoryDialog,
  closeDogeCategoryDialog,
  saveDogeCategoryAssignments,
}) {
  const {
    renderAnnouncements,
    setPanel: setAnnouncementPanel,
    mount: mountAnnouncements,
  } = createDogeAnnouncements({ loadState });
  const {
    renderOnboarding,
    mount: mountOnboarding,
  } = createDogeOnboarding({ loadState, openDogeCategoryDialog });

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
    const doge = serverState.snapshot?.doge || {};
    const waitingForFirstSync = Boolean(doge.bound && !doge.lastSyncAt && !doge.lastSyncError);
    return Boolean(runtimeState.localDogeSyncing || doge.syncing || waitingForFirstSync);
  }

  function showDogeSyncToast(phase = "base") {
    const message = phase === "keys" ? "令牌密钥同步中..." : "基础数据同步中...";
    const node = $("toast");
    node.replaceChildren(icon("load", "spin"), Object.assign(document.createElement("span"), { textContent: message }));
    node.className = "toast show";
    clearTimeout(runtimeState.toastTimer);
    clearTimeout(runtimeState.toastCleanupTimer);
    runtimeState.toastTimer = null;
    runtimeState.toastCleanupTimer = null;
  }

  function renderDogeSyncToast() {
    const doge = serverState.snapshot?.doge || {};
    const remoteSyncing = Boolean(doge.syncing);
    if (remoteSyncing) {
      showDogeSyncToast(doge.syncPhase || "base");
    } else if (!runtimeState.localDogeSyncing && runtimeState.dogeRemoteSyncing) {
      toast("数据同步完成");
    }
    runtimeState.dogeRemoteSyncing = remoteSyncing;
  }

  // 手动同步期间短轮询本地状态，只读取阶段标记，不重复请求上游；同步结束后由主请求统一刷新完整页面。
  function startDogeSyncProgressPolling() {
    if (runtimeState.dogeSyncPollTimer) return;
    const generation = runtimeState.dogeSyncPollGeneration + 1;
    runtimeState.dogeSyncPollGeneration = generation;
    const poll = async () => {
      runtimeState.dogeSyncPollTimer = null;
      if (!runtimeState.localDogeSyncing || generation !== runtimeState.dogeSyncPollGeneration) return;
      try {
        const snapshot = await GetState();
        if (!runtimeState.localDogeSyncing || generation !== runtimeState.dogeSyncPollGeneration) return;
        setServerSnapshot(snapshot, { notifyListeners: false });
        renderShell();
        renderConnection();
        renderDogeSyncToast();
      } catch {
        // 主同步请求负责报告错误，阶段轮询失败时保持当前提示，避免覆盖原始错误。
      }
      if (runtimeState.localDogeSyncing) runtimeState.dogeSyncPollTimer = setTimeout(poll, 350);
    };
    runtimeState.dogeSyncPollTimer = setTimeout(poll, 120);
  }

  function stopDogeSyncProgressPolling() {
    runtimeState.dogeSyncPollGeneration += 1;
    if (runtimeState.dogeSyncPollTimer) clearTimeout(runtimeState.dogeSyncPollTimer);
    runtimeState.dogeSyncPollTimer = null;
  }

  function renderDogeQuota() {
    const doge = serverState.snapshot?.doge || {};
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
      const amountNode = document.createElement("strong");
      amountNode.textContent = formatDogeUSD(subscription.remainingUsd);
      row.append(label, amountNode);
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
    const doge = serverState.snapshot?.doge || {};
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

  function openDogeTopupModal() {
    if (!serverState.snapshot?.doge?.bound) {
      toast("请先在设置中绑定二狗子 API", true);
      return;
    }
    $("dogeTopupError").dataset.submission = "";
    renderDogeTopupState();
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

  function setDogeQuotaPopover(open) {
    const popover = $("dogeQuotaPopover");
    popover.classList.toggle("hidden", !open);
    $("dogeQuotaSummary").setAttribute("aria-expanded", String(open));
  }

  async function syncDogeNow(button) {
    if (runtimeState.localDogeSyncing || serverState.snapshot?.doge?.syncing) return;
    setButtonLoading(button, true);
    runtimeState.localDogeSyncing = true;
    runtimeState.dogeRemoteSyncing = false;
    showDogeSyncToast("base");
    startDogeSyncProgressPolling();
    renderShell(); renderProfiles(); renderConnection(); renderAnnouncements();
    let synced = false;
    try {
      runtimeState.dogeCategoryDialogSignature = "";
      await SyncDoge();
      synced = true;
      toast("数据同步完成");
    } catch (error) {
      toast(errorMessage(error), true);
    } finally {
      runtimeState.localDogeSyncing = false;
      stopDogeSyncProgressPolling();
      runtimeState.dogeRemoteSyncing = false;
      await loadState();
      if (synced) openDogeCategoryDialog(true);
      setButtonLoading(button, false);
    }
  }

  async function toggleDogeConnection() {
    const doge = serverState.snapshot?.doge || {};
    const button = $("dogeConnectionAction");
    const binding = !doge.bound;
    let bound = false;
    if (doge.bound && !(await showConfirmDialog("解除绑定会清除本地二狗子目录，确定继续吗？", { title: "解除二狗子绑定", danger: true }))) return;
    if (binding) {
      runtimeState.localDogeSyncing = true;
      runtimeState.dogeRemoteSyncing = false;
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
        runtimeState.localDogeSyncing = false;
        stopDogeSyncProgressPolling();
        runtimeState.dogeRemoteSyncing = false;
        await loadState();
        if (bound) openDogeCategoryDialog(true);
      }
      setButtonLoading(button, false);
      renderConnection();
    }
  }

  async function updateSyncInterval() {
    try {
      await SetDogeSyncInterval(Number($("dogeSyncInterval").value));
      await loadState();
      toast("自动同步间隔已更新");
    } catch (error) {
      toast(errorMessage(error), true);
    }
  }

  function mount() {
    $("dogeQuotaSummary").addEventListener("click", () => setDogeQuotaPopover($("dogeQuotaPopover").classList.contains("hidden")));
    $("dogeQuotaWrap").addEventListener("mouseenter", () => setDogeQuotaPopover(true));
    $("dogeQuotaWrap").addEventListener("mouseleave", () => setDogeQuotaPopover(false));
    $("dogeQuotaWrap").addEventListener("focusin", () => setDogeQuotaPopover(true));
    $("dogeQuotaWrap").addEventListener("focusout", (event) => {
      if (!$("dogeQuotaWrap").contains(event.relatedTarget)) setDogeQuotaPopover(false);
    });
    mountAnnouncements();
    $("openDogeTopup").addEventListener("click", openDogeTopupModal);
    $("closeDogeTopupModal").addEventListener("click", closeDogeTopupModal);
    $("openDogeProfile").addEventListener("click", openDogeProfile);
    mountOnboarding();
    $("dogeTopupPurchase").addEventListener("click", openDogePurchase);
    $("submitDogeTopup").addEventListener("click", redeemDoge);
    $("refreshDoge").addEventListener("click", () => syncDogeNow($("refreshDoge")));
    $("syncDogeSettings").addEventListener("click", (event) => syncDogeNow(event.currentTarget));
    $("pendingDogeImport").addEventListener("click", () => openDogeCategoryDialog(true));
    $("dogeConnectionAction").addEventListener("click", toggleDogeConnection);
    $("dogeSyncInterval").addEventListener("change", updateSyncInterval);
    $("closeDogeCategoryModal").addEventListener("click", closeDogeCategoryDialog);
    $("saveDogeCategories").addEventListener("click", saveDogeCategoryAssignments);
  }

  return {
    formatDogeUSD,
    isDogeSyncing,
    renderDogeSyncToast,
    renderDogeQuota,
    renderAnnouncements,
    setAnnouncementPanel,
    renderOnboarding,
    closeDogeTopupModal,
    setDogeQuotaPopover,
    mount,
  };
}
