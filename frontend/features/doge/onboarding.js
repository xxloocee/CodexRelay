import { BindDoge, CompleteOnboarding, ConfigureDetectedClients } from "../../core/desktop-api.js";
import { $ } from "../../core/dom.js";
import { errorMessage, setButtonLoading, toast } from "../../core/feedback.js";
import { showConfirmDialog, syncModalBody } from "../../core/modal.js";
import { runtimeState, serverState } from "../../core/store.js";

export function createDogeOnboarding({ loadState, openDogeCategoryDialog }) {
  function renderOnboarding() {
    const modal = $("onboardingModal");
    const open = Boolean(serverState.snapshot?.needsOnboarding);
    modal.classList.toggle("hidden", !open);
    syncModalBody();
    if (!open) {
      runtimeState.onboardingFocused = false;
      return;
    }
    if (!runtimeState.onboardingFocused) {
      runtimeState.onboardingFocused = true;
      setTimeout(() => $("onboardingToken")?.focus(), 0);
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
      let completionMessage = "二狗子已验证并绑定";
      let completionError = false;
      const detectedClients = (serverState.snapshot?.clientConfigs || [])
        .filter((client) => client.detected && client.status !== "unsupported" && !client.skipConfigReplacement
          && (!client.requiresProfile || serverState.snapshot?.activeProfiles?.[client.category]));
      if (detectedClients.length) {
        const labels = detectedClients.map((client) => client.label).join("、");
        const accepted = await showConfirmDialog(
          `检测到 ${labels} 已安装。是否现在备份原配置并配置全部客户端？`,
          { title: "配置外部客户端", confirmLabel: "配置全部" },
        );
        if (accepted) {
          try {
            await ConfigureDetectedClients();
            await loadState();
            completionMessage = "二狗子已绑定，检测到的客户端已配置";
          } catch (error) {
            completionMessage = `账户已绑定，但客户端配置失败：${errorMessage(error)}`;
            completionError = true;
          }
        }
      }
      openDogeCategoryDialog(true);
      toast(completionMessage, completionError);
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

  function mount() {
    $("bindOnboarding").addEventListener("click", bindOnboarding);
    $("skipOnboarding").addEventListener("click", skipOnboarding);
  }

  return { renderOnboarding, mount };
}
