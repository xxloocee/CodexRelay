import { runtimeState } from "./store.js";
import { $, icon } from "./dom.js";

export function errorMessage(error) {
  if (typeof error === "string") return error;
  if (error?.message) return error.message;
  return String(error || "操作失败");
}

// 统一异步按钮的视觉反馈；保存原图标和文案，避免请求结束后不同操作留下不一致状态。
export function setButtonLoading(button, loading, label = "") {
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

export function setLoadingText(node, loading, text) {
  if (!node) return;
  if (!loading) {
    node.textContent = text;
    node.classList.remove("inline-loading");
    return;
  }
  node.replaceChildren(icon("load", "spin"), Object.assign(document.createElement("span"), { textContent: text }));
  node.classList.add("inline-loading");
}

export async function copyText(text) {
  if (!text || text === "-") return;
  try {
    await navigator.clipboard.writeText(text);
    toast("已复制");
  } catch {
    toast("复制失败", true);
  }
}

export function toast(message, isError = false) {
  const node = $("toast");
  clearTimeout(runtimeState.toastTimer);
  clearTimeout(runtimeState.toastCleanupTimer);
  node.replaceChildren(Object.assign(document.createElement("span"), { textContent: message }));
  node.className = "toast show" + (isError ? " error" : "");
  runtimeState.toastTimer = setTimeout(() => {
    runtimeState.toastTimer = null;
    // 淡出期间保留当前提示类型，避免错误提示在透明度过渡时短暂恢复成默认绿色。
    node.classList.remove("show");
    runtimeState.toastCleanupTimer = setTimeout(() => {
      runtimeState.toastCleanupTimer = null;
      if (!node.classList.contains("show")) node.className = "toast";
    }, 200);
  }, 3000);
}
