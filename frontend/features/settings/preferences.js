import { SetDogeAlertSettings, SetPreferences, SetTokenSwitchSettings } from "../../core/desktop-api.js";
import { $ } from "../../core/dom.js";
import { errorMessage, toast } from "../../core/feedback.js";
import { categoryOptions, drafts, serverState } from "../../core/store.js";

export function createPreferences({ loadState, visibleCategorySet, categoryLabel }) {
  function renderPreferences() {
    const preferences = serverState.snapshot.preferences || {};
    const preserveDraft = drafts.preferencesDirty;
    if (!preserveDraft) {
      $("closeToTray").checked = preferences.closeToTray;
      $("launchAtStartup").checked = preferences.launchAtStartup;
      $("startHidden").checked = preferences.startHidden;
    }
    if (!drafts.tokenSwitchDirty) renderTokenSwitchSettings();
    if (!drafts.dogeAlertDirty) renderDogeAlertSettings();
    const visible = preserveDraft
      ? new Set(Array.from(document.querySelectorAll("#visibleCategories input[data-category]:checked"), (input) => input.dataset.category))
      : visibleCategorySet();
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

  function markPreferencesDirty() {
    drafts.preferencesDirty = true;
    drafts.preferencesDraftRevision += 1;
  }

  function markTokenSwitchDirty() {
    drafts.tokenSwitchDirty = true;
    drafts.tokenSwitchDraftRevision += 1;
  }

  function markDogeAlertDirty() {
    drafts.dogeAlertDirty = true;
    drafts.dogeAlertDraftRevision += 1;
  }

  function renderTokenSwitchSettings() {
    const settings = serverState.snapshot?.tokenSwitch || {};
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
    const doge = serverState.snapshot?.doge || {};
    $("balanceAlertEnabled").checked = doge.balanceAlertEnabled !== false;
    $("subscriptionAlertEnabled").checked = doge.subscriptionAlertEnabled !== false;
    $("balanceAlertThresholdUSD").value = Number(doge.balanceAlertThresholdUsd || 1).toFixed(2);
    $("subscriptionAlertThresholdUSD").value = Number(doge.subscriptionAlertThresholdUsd || 1).toFixed(2);
  }

  async function saveTokenSwitchSettings() {
    const draftRevision = drafts.tokenSwitchDraftRevision;
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
      if (draftRevision === drafts.tokenSwitchDraftRevision) drafts.tokenSwitchDirty = false;
      await loadState();
      toast("令牌异常处理设置已保存");
    } catch (error) {
      await loadState();
      toast(errorMessage(error), true);
    }
  }

  async function saveDogeAlertSettings() {
    const draftRevision = drafts.dogeAlertDraftRevision;
    const payload = {
      balanceEnabled: $("balanceAlertEnabled").checked,
      balanceThresholdUsd: Number($("balanceAlertThresholdUSD").value),
      subscriptionEnabled: $("subscriptionAlertEnabled").checked,
      subscriptionThresholdUsd: Number($("subscriptionAlertThresholdUSD").value),
    };
    try {
      await SetDogeAlertSettings(payload);
      if (draftRevision === drafts.dogeAlertDraftRevision) drafts.dogeAlertDirty = false;
      await loadState();
      toast("余额和套餐提醒设置已保存");
    } catch (error) {
      await loadState();
      toast(errorMessage(error), true);
    }
  }

  async function savePreferences() {
    const draftRevision = drafts.preferencesDraftRevision;
    const visibleCategories = Array.from(document.querySelectorAll("#visibleCategories input[data-category]:checked"), (input) => input.dataset.category);
    if (!visibleCategories.length) {
      drafts.preferencesDirty = false;
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
      if (draftRevision === drafts.preferencesDraftRevision) drafts.preferencesDirty = false;
      await loadState();
      toast("通用设置已保存");
    } catch (error) {
      await loadState();
      toast(errorMessage(error), true);
    }
  }

  function mount() {
    for (const id of ["closeToTray", "launchAtStartup", "startHidden", "defaultSource", "defaultCategory", "restoreViewMode"]) {
      $(id).addEventListener("change", () => {
        markPreferencesDirty();
        savePreferences();
      });
    }
    document.querySelectorAll("#tokenSwitchMode button").forEach((button) => button.addEventListener("click", async () => {
      document.querySelectorAll("#tokenSwitchMode button").forEach((item) => item.classList.toggle("active", item === button));
      markTokenSwitchDirty();
      await saveTokenSwitchSettings();
    }));
    const tokenSwitchInputs = ["failoverLoop", "trigger401", "trigger403", "trigger5xx", "triggerNetwork", "triggerDirectoryInvalid", "triggerDirectoryMissing", "authFailureThreshold", "upstreamFailureThreshold", "upstreamFailureWindowMinutes"];
    for (const id of tokenSwitchInputs) $(id).addEventListener("change", () => {
      markTokenSwitchDirty();
      saveTokenSwitchSettings();
    });
    for (const id of ["authFailureThreshold", "upstreamFailureThreshold", "upstreamFailureWindowMinutes"]) $(id).addEventListener("input", markTokenSwitchDirty);
    for (const id of ["balanceAlertThresholdUSD", "subscriptionAlertThresholdUSD"]) $(id).addEventListener("input", markDogeAlertDirty);
    for (const id of ["balanceAlertEnabled", "balanceAlertThresholdUSD", "subscriptionAlertEnabled", "subscriptionAlertThresholdUSD"]) {
      $(id).addEventListener("change", () => {
        markDogeAlertDirty();
        saveDogeAlertSettings();
      });
    }
  }

  return { renderPreferences, mount };
}
