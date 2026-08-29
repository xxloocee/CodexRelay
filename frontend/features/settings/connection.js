import { $ } from "../../core/dom.js";
import { setLoadingText } from "../../core/feedback.js";
import { drafts, serverState } from "../../core/store.js";

export function createConnection({ isDogeSyncing, formatDogeUSD }) {
  function renderConnection() {
    const doge = serverState.snapshot.doge || {};
    const bound = Boolean(doge.bound);
    const syncing = bound && isDogeSyncing();
    const syncError = String(doge.lastSyncError || "").trim();
    const user = doge.user || {};
    const account = doge.account || {};
    const accountUnavailable = Boolean(syncError && !doge.lastSyncAt);
    const accountValue = (value, fallback = "-") => syncing ? "同步中..." : (accountUnavailable ? "同步失败" : (value || fallback));
    if (!drafts.dogeBaseURLDirty) $("dogeBaseURL").value = doge.baseUrl || "";
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
    $("dogeAccountRequests").textContent = accountValue(new Intl.NumberFormat("zh-CN").format(Number(account.requestCount || 0)), "0");
    setLoadingText($("dogeAccountState"), syncing, syncing ? "同步中... 请稍候" : (syncError ? "账户数据同步失败，当前显示缓存" : "账户信息来自最近一次同步"));
    $("dogeSyncRow").classList.toggle("hidden", !bound);
    $("dogeSyncInterval").value = String(doge.syncIntervalMinutes || 3);
    const syncButton = $("syncDogeSettings");
    if (syncButton && syncButton.dataset.loading !== "true") {
      syncButton.disabled = !bound || syncing;
      syncButton.setAttribute("aria-label", syncing ? "二狗子信息同步中" : "手动同步二狗子信息");
    }
    $("dogeSyncError").textContent = !syncing && doge.lastSyncError ? "同步失败：" + doge.lastSyncError : "";
    $("dogeSyncError").classList.toggle("hidden", syncing || !doge.lastSyncError);
  }

  return { renderConnection };
}
