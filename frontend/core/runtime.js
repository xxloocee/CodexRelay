import * as wails from "/wails/runtime.js";

import { $ } from "./dom.js";
import { closeConfirmDialog } from "./modal.js";
import { navigation, runtimeState, serverState } from "./store.js";

let activeRuntimeCleanup = null;

// 只负责窗口生命周期、Wails 事件和定时刷新；业务模块通过回调提供自己的渲染入口。
export function mountRuntime({
  loadState,
  restoreDefaultViewFilter,
  renderUpdateStatus,
  setDogeQuotaPopover,
  setAnnouncementPanel,
  closeDogeTopupModal,
  closeDogeTokenModal,
  closeDogeCategoryDialog,
  closeClientSetupModal,
}) {
  activeRuntimeCleanup?.();
  const onKeyDown = (event) => {
    if (event.key !== "Escape") return;
    if (!$("onboardingModal").classList.contains("hidden")) return;
    if (!$("confirmModal").classList.contains("hidden")) {
      closeConfirmDialog(false);
      return;
    }
    setDogeQuotaPopover(false);
    setAnnouncementPanel(false);
    if (!$("dogeTopupModal").classList.contains("hidden")) closeDogeTopupModal();
    if (!$("dogeTokenCreateModal").classList.contains("hidden")) closeDogeTokenModal();
    if (!$("dogeCategoryModal").classList.contains("hidden")) closeDogeCategoryDialog();
    if (!$("clientSetupModal").classList.contains("hidden")) closeClientSetupModal();
  };
  const onVisibilityChange = () => {
    if (document.hidden) return;
    if (serverState.snapshot?.preferences?.restoreViewMode === "default") restoreDefaultViewFilter();
    loadState();
  };

  document.addEventListener("keydown", onKeyDown);
  document.addEventListener("visibilitychange", onVisibilityChange);

  // 托盘后台同步完成后由原生窗口发出状态事件；隐藏窗口可能不触发 visibilitychange。
  const eventCleanups = [
    wails.Events.On("relay-state-changed", () => { if (navigation.view !== "editor") loadState(); }),
    wails.Events.On("relay-restore-default-view", () => {
      if (serverState.snapshot) restoreDefaultViewFilter();
    }),
    wails.Events.On("wails:updater:download-started", () => {
      runtimeState.update.installing = true;
      runtimeState.update.phase = "正在下载更新";
      renderUpdateStatus();
    }),
    wails.Events.On("wails:updater:download-progress", (event) => {
      const progress = event?.data || event || {};
      runtimeState.update.written = Number(progress.written || 0);
      runtimeState.update.total = Number(progress.total || 0);
      renderUpdateStatus();
    }),
    wails.Events.On("wails:updater:verifying", () => {
      runtimeState.update.phase = "正在校验更新文件";
      renderUpdateStatus();
    }),
    wails.Events.On("wails:updater:installing", () => {
      runtimeState.update.phase = "正在准备替换程序";
      renderUpdateStatus();
    }),
    wails.Events.On("wails:updater:update-ready", () => {
      runtimeState.update.phase = "更新已校验，正在重启";
      renderUpdateStatus();
    }),
  ];

  loadState();
  const refreshTimer = setInterval(() => {
    if (!document.hidden && navigation.view !== "editor") loadState();
  }, 3000);

  let disposed = false;
  const dispose = () => {
    if (disposed) return;
    disposed = true;
    clearInterval(refreshTimer);
    document.removeEventListener("keydown", onKeyDown);
    document.removeEventListener("visibilitychange", onVisibilityChange);
    for (const cleanup of eventCleanups) cleanup?.();
    if (activeRuntimeCleanup === dispose) activeRuntimeCleanup = null;
  };
  activeRuntimeCleanup = dispose;
  return dispose;
}
