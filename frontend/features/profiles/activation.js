import {
  ActivateProfile,
  CheckClientConfig,
  SelectDirectory,
  SetDataDirectory,
} from "../../core/desktop-api.js";
import { $ } from "../../core/dom.js";
import { errorMessage, setButtonLoading, toast } from "../../core/feedback.js";
import { syncModalBody } from "../../core/modal.js";
import { runtimeState, serverState } from "../../core/store.js";

export function createProfileActivation({ loadState, categoryLabel }) {
  function clientConfigFor(category) {
    return (serverState.snapshot?.clientConfigs || []).find((item) => item.category === category) || null;
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
    input.value = serverState.snapshot?.dataDirectory || "";
  }

  function openClientSetupModal(category, pending) {
    runtimeState.clientSetupCategory = category;
    runtimeState.pendingActivation = pending;
    $("clientSetupHeading").textContent = clientCategoryLabel(category) + " 配置";
    $("clientSetupQuestion").textContent = `当前 ${clientCategoryLabel(category)} 未使用 CodexRelay 配置信息，是否一键配置？`;
    $("clientSetupURL").textContent = serverState.snapshot?.proxyUrls?.[category] || serverState.snapshot?.proxyUrl || "-";
    $("clientSetupKey").textContent = serverState.snapshot?.localAccessToken || "-";
    $("clientSetupModal").classList.remove("hidden");
    syncModalBody();
  }

  function closeClientSetupModal() {
    $("clientSetupModal").classList.add("hidden");
    runtimeState.pendingActivation = null;
    runtimeState.clientSetupCategory = "";
    syncModalBody();
  }

  async function performActivation(pending) {
    const button = pending?.button || null;
    setButtonLoading(button, true, "切换中...");
    try {
      // configureClient 只由用户确认或已明确接管的切换路径设置；跳过时
      // 传 false，后端不会触碰外部客户端文件。
      await ActivateProfile(pending.profileId, pending.configureClient === true);
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
      await performActivation({ ...pending, configureClient: false });
      return;
    }
    let configured = clientIsConfigured(pending.category);
    try {
      const status = await CheckClientConfig(pending.category);
      configured = !status || status.status === "configured" || status.status === "unsupported";
      const cached = serverState.snapshot?.clientConfigs?.find((item) => item.category === pending.category);
      if (cached && status) Object.assign(cached, status);
    } catch (error) {
      toast(errorMessage(error), true);
      return;
    }
    if (configured) {
      // 当前客户端已经由 CodexRelay 接管，切换 Profile 时同步模型等变更。
      await performActivation({ ...pending, configureClient: true });
      return;
    }
    openClientSetupModal(pending.category, pending);
  }

  async function resolveClientSetup(configure) {
    const pending = runtimeState.pendingActivation;
    const category = runtimeState.clientSetupCategory;
    if (!pending || !category) {
      closeClientSetupModal();
      return;
    }
    const button = $("clientSetupConfigure");
    closeClientSetupModal();
    if (configure) setButtonLoading(button, true, "配置并启用中...");
    try {
      await performActivation({ ...pending, configureClient: Boolean(configure), button: pending.button || (configure ? button : null) });
    } finally {
      if (configure) setButtonLoading(button, false);
    }
  }

  async function chooseDataDirectory(event) {
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
  }

  function mount() {
    $("clientSetupSkip").addEventListener("click", () => resolveClientSetup(false));
    $("clientSetupConfigure").addEventListener("click", () => resolveClientSetup(true));
    $("closeClientSetupModal").addEventListener("click", closeClientSetupModal);
    $("chooseDataDirectory").addEventListener("click", chooseDataDirectory);
  }

  return {
    renderDataDirectory,
    beginActivation,
    closeClientSetupModal,
    mount,
  };
}
