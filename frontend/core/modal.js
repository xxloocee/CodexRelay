import { runtimeState } from "./store.js";
import { $ } from "./dom.js";

export function syncModalBody() {
  const modalIDs = ["onboardingModal", "dogeCategoryModal", "dogeTopupModal", "dogeTokenCreateModal", "clientSetupModal", "confirmModal"];
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

export function closeConfirmDialog(result = false) {
  const modal = $("confirmModal");
  if (modal.classList.contains("hidden")) return;
  modal.classList.add("hidden");
  syncModalBody();
  const resolver = runtimeState.confirmResolver;
  runtimeState.confirmResolver = null;
  resolver?.(result);
}

// 统一替代浏览器原生 confirm，避免 WebView 使用 wails.localhost 作为系统弹窗标题。
export function showConfirmDialog(message, options = {}) {
  if (runtimeState.confirmResolver) closeConfirmDialog(false);
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
  return new Promise((resolve) => { runtimeState.confirmResolver = resolve; });
}
