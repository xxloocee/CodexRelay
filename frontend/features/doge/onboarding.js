import { BindDoge, CompleteOnboarding } from "../../core/desktop-api.js";
import { $ } from "../../core/dom.js";
import { errorMessage, setButtonLoading, toast } from "../../core/feedback.js";
import { syncModalBody } from "../../core/modal.js";
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

  function mount() {
    $("bindOnboarding").addEventListener("click", bindOnboarding);
    $("skipOnboarding").addEventListener("click", skipOnboarding);
  }

  return { renderOnboarding, mount };
}
