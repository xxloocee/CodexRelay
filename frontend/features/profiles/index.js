import { $ } from "../../core/dom.js";
import { drafts, navigation, serverState } from "../../core/store.js";

// Profiles 只绑定本功能的页面事件；业务动作由组合入口显式注入。
export function mountProfiles({
  openEditor,
  setFilter,
  leaveEditor,
  saveProfile,
  fetchEditorModels,
  addEditorModel,
  updatePreview,
  copyText,
  activateProfile,
  testProfile,
  deleteProfile,
}) {
  $("addProfile").addEventListener("click", () => openEditor());
  $("emptyAdd").addEventListener("click", () => openEditor());
  document.querySelectorAll(".filter-option").forEach((button) => button.addEventListener("click", () => {
    setFilter(button.closest(".filter-options").dataset.filterGroup, button.dataset.filterValue);
  }));
  $("editorBack").addEventListener("click", leaveEditor);
  $("profileForm").addEventListener("submit", saveProfile);
  $("profileForm").addEventListener("input", () => { drafts.dirty = true; });
  $("fetchModels").addEventListener("click", (event) => fetchEditorModels(event.currentTarget));
  $("addModel").addEventListener("click", addEditorModel);
  $("baseUrl").addEventListener("input", updatePreview);
  $("profileCategory").addEventListener("change", updatePreview);
  $("copyPreviewUrl").addEventListener("click", () => {
    const category = $("profileCategory").value || "codex";
    copyText(serverState.snapshot?.proxyUrls?.[category] || serverState.snapshot?.proxyUrl || "-");
  });
  $("copyPreviewToken").addEventListener("click", () => copyText(serverState.snapshot?.localAccessToken || "-"));
  $("copyApiKey").addEventListener("click", () => copyText($("apiKey").value.trim()));
  $("apiKey").addEventListener("input", () => {
    // 已有密钥不会回填到输入框；只有用户明确输入新值时才允许复制。
    const value = $("apiKey").value.trim();
    $("copyApiKey").disabled = !value;
    const storedKeyAvailable = $("apiKey").dataset.keyConfigured === "true" && Boolean(navigation.selectedId);
    $("fetchModels").disabled = !value && !storedKeyAvailable;
  });
  $("activateProfile").addEventListener("click", (event) => activateProfile(navigation.selectedId, event.currentTarget));
  $("testProfile").addEventListener("click", () => testProfile(navigation.selectedId, $("testProfile")));
  $("deleteProfile").addEventListener("click", (event) => deleteProfile(navigation.selectedId, event.currentTarget));
}
