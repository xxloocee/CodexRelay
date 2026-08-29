import { $ } from "./dom.js";
import { serverState } from "./store.js";

export function createShell({
  pendingDogeTokens,
  renderDogeQuota,
  renderUpdateStatus,
  visibleCategorySet,
}) {
  function renderPendingDogeImport() {
    const count = pendingDogeTokens().length;
    $("pendingDogeImport").classList.toggle("hidden", count === 0);
    $("pendingDogeImportCount").textContent = String(count);
  }

  function renderShell() {
    const state = serverState.snapshot;
    if (!state) return;
    $("version").textContent = "v" + state.version;
    $("aboutVersion").textContent = "版本 " + state.version;
    renderUpdateStatus();
    const visibleCategories = visibleCategorySet();
    const activeCategories = new Set((state.profiles || [])
      .filter((profile) => profile.active && visibleCategories.has(profile.category))
      .map((profile) => profile.category));
    $("activeCount").textContent = `${activeCategories.size}/${visibleCategories.size}`;
    const failoverMode = state.tokenSwitch?.mode === "auto" ? "模式：自动切换" : "模式：手动提示";
    const failoverStatus = $("failoverStatus");
    failoverStatus.textContent = failoverMode;
    failoverStatus.classList.toggle("manual", state.tokenSwitch?.mode !== "auto");
    $("taskNotificationState").textContent = state.taskNotification?.enabled ? "已开启" : "已关闭";
    $("taskNotificationSummary").classList.toggle("is-enabled", Boolean(state.taskNotification?.enabled));
    renderPendingDogeImport();
    renderDogeQuota();
  }

  return { renderPendingDogeImport, renderShell };
}
