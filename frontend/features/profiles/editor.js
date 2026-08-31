import { DeleteProfile, FetchProfileModels, SaveProfile, TestProfile } from "../../core/desktop-api.js";
import { $, icon } from "../../core/dom.js";
import { errorMessage, setButtonLoading, toast } from "../../core/feedback.js";
import { showConfirmDialog } from "../../core/modal.js";
import { drafts, navigation, serverState } from "../../core/store.js";

export function createProfileEditor({ loadState, beginActivation, nonHomeProfileName, showView }) {
  function openEditor(id = null) {
    navigation.selectedId = id;
    drafts.isNew = !id;
    drafts.dirty = false;
    const profile = id ? serverState.snapshot.profiles.find((item) => item.id === id) : null;
    $("editorTitle").textContent = profile ? "编辑代理 API" : "新建代理 API";
    $("profileCategory").value = profile?.category || "codex";
    $("profileName").value = profile?.name || "";
    $("profileNote").value = profile?.note || "";
    $("baseUrl").value = profile?.baseUrl || "";
    const dogeToken = profile?.source === "doge"
      ? serverState.snapshot?.doge?.tokens?.find((token) => Number(token.id) === Number(profile.remoteTokenId))
      : null;
    const dogeGroupField = $("dogeProfileGroupField");
    dogeGroupField.classList.toggle("hidden", !dogeToken);
    if (dogeToken) {
      const groupSelect = $("dogeProfileGroup");
      groupSelect.replaceChildren();
      for (const group of serverState.snapshot?.doge?.groups || []) {
        const displayName = String(serverState.snapshot?.doge?.groupDisplayNames?.[group] || group).trim();
        groupSelect.appendChild(new Option(displayName || group, group));
      }
      if (!Array.from(groupSelect.options).some((option) => option.value === dogeToken.group)) {
        groupSelect.appendChild(new Option(dogeToken.groupDisplayName || dogeToken.group, dogeToken.group));
      }
      groupSelect.value = dogeToken.group;
    }
    // 完整密钥不再从状态快照返回。已有 Profile 留空提交表示保留后端密钥，
    // 新建 Profile 仍要求填写；避免把脱敏提示误当成真实密钥保存。
    const keyConfigured = Boolean(profile?.apiKeyConfigured);
    const keyHint = profile?.apiKeyHint || "";
    $("apiKey").value = "";
    $("apiKey").required = !keyConfigured;
    $("apiKey").placeholder = keyConfigured ? `已配置${keyHint ? `（${keyHint}）` : ""}，留空保持不变` : "sk-...";
    $("apiKey").setAttribute("data-key-configured", String(keyConfigured));
    $("copyApiKey").disabled = true;
    $("fetchModels").disabled = !keyConfigured;
    const dogeKey = profile?.source === "doge";
    $("apiKey").readOnly = dogeKey;
    $("apiKey").setAttribute("aria-readonly", String(dogeKey));
    $("apiKey").title = dogeKey ? "二狗子令牌密钥由远端管理，不能修改" : (keyConfigured ? "已配置密钥不会显示；留空表示保持不变" : "");
    $("headers").value = profile && Object.keys(profile.headers || {}).length ? JSON.stringify(profile.headers, null, 2) : "";
    drafts.editorModels = (profile?.models || []).map((model) => ({
      id: model.id || "", name: model.name || model.id || "", ownedBy: model.ownedBy || "", contextWindow: Number(model.contextWindow) > 0 ? Number(model.contextWindow) : 0,
    }));
    drafts.editorDefaultModel = profile?.defaultModel || "";
    setModelManagerStatus("");
    renderModelManager();
    $("activeBadge").classList.toggle("hidden", !profile?.active);
    const activateButton = $("activateProfile");
    activateButton.disabled = !profile;
    const activateLabel = activateButton.querySelector("[data-button-label]");
    if (activateLabel) activateLabel.textContent = profile?.active ? "重新配置" : "设为当前";
    $("testProfile").disabled = !profile;
    $("deleteProfile").disabled = !profile;
    updatePreview();
    showView("editor");
    setTimeout(() => $("profileName").focus(), 0);
  }

  function readHeadersField() {
    let headers = {};
    const headerText = $("headers").value.trim();
    if (!headerText) return headers;
    try {
      headers = JSON.parse(headerText);
      if (!headers || Array.isArray(headers) || typeof headers !== "object") throw new Error();
      if (!Object.values(headers).every((value) => typeof value === "string")) throw new Error();
    } catch {
      throw new Error("额外请求头必须是值为字符串的 JSON 对象");
    }
    return headers;
  }

  function renderModelManager() {
    const rows = $("modelRows");
    const empty = $("modelEmpty");
    if (!rows || !empty) return;
    rows.replaceChildren();
    const models = Array.isArray(drafts.editorModels) ? drafts.editorModels : [];
    empty.classList.toggle("hidden", models.length > 0);
    for (const [index, model] of models.entries()) {
      const row = document.createElement("div");
      row.className = "model-row";
      const fields = [
        ["id", "模型 ID", "text"],
        ["name", "显示名称", "text"],
        ["ownedBy", "归属（可选）", "text"],
        ["contextWindow", "可选", "number"],
      ];
      for (const [field, placeholder, type] of fields) {
        const input = document.createElement("input");
        input.type = type;
        input.value = model[field] || "";
        input.placeholder = placeholder;
        input.dataset.modelField = field;
        input.setAttribute("aria-label", `${placeholder} ${index + 1}`);
        if (type === "number") {
          input.min = "0";
          input.step = "1";
          input.inputMode = "numeric";
        }
        input.addEventListener("input", () => {
          const previousID = drafts.editorModels[index][field];
          drafts.editorModels[index][field] = type === "number" ? (input.value ? Number(input.value) : 0) : input.value;
          if (field === "id" && drafts.editorDefaultModel === previousID) drafts.editorDefaultModel = input.value;
          drafts.dirty = true;
        });
        row.appendChild(input);
      }
      const defaultInput = document.createElement("input");
      defaultInput.type = "radio";
      defaultInput.name = "defaultModel";
      defaultInput.className = "model-default";
      defaultInput.checked = drafts.editorDefaultModel === model.id && Boolean(model.id);
      defaultInput.title = "设为默认模型";
      defaultInput.setAttribute("aria-label", `将 ${model.id || `模型 ${index + 1}`} 设为默认`);
      defaultInput.addEventListener("change", () => {
        if (defaultInput.checked) {
          drafts.editorDefaultModel = drafts.editorModels[index].id.trim();
          drafts.dirty = true;
        }
      });
      const remove = document.createElement("button");
      remove.type = "button";
      remove.className = "model-delete";
      remove.title = "删除模型";
      remove.setAttribute("aria-label", `删除 ${model.id || `模型 ${index + 1}`}`);
      remove.appendChild(icon("trash-2"));
      remove.addEventListener("click", () => {
        const deletedID = drafts.editorModels[index]?.id;
        drafts.editorModels.splice(index, 1);
        if (deletedID && drafts.editorDefaultModel === deletedID) drafts.editorDefaultModel = drafts.editorModels[0]?.id || "";
        drafts.dirty = true;
        renderModelManager();
      });
      row.append(defaultInput, remove);
      rows.appendChild(row);
    }
  }

  function setModelManagerStatus(message, error = false) {
    const node = $("modelManagerStatus");
    if (!node) return;
    node.textContent = message || "";
    node.classList.toggle("is-error", error);
    node.classList.toggle("is-success", Boolean(message) && !error);
  }

  function addEditorModel() {
    drafts.editorModels.push({ id: "", name: "", ownedBy: "", contextWindow: 0 });
    drafts.dirty = true;
    setModelManagerStatus("");
    renderModelManager();
    const last = $("modelRows").lastElementChild?.querySelector("input[data-model-field=\"id\"]");
    last?.focus();
  }

  async function fetchEditorModels(button = null) {
    let headers;
    try {
      headers = readHeadersField();
    } catch (error) {
      toast(errorMessage(error), true);
      return;
    }
    setButtonLoading(button, true, "获取中...");
    try {
      const models = await FetchProfileModels({
        id: drafts.isNew ? "" : navigation.selectedId,
        baseUrl: $("baseUrl").value.trim(),
        apiKey: $("apiKey").value.trim(),
        headers,
      });
      drafts.editorModels = (models || []).map((model) => ({ id: model.id || "", name: model.name || model.id || "", ownedBy: model.ownedBy || "", contextWindow: Number(model.contextWindow) > 0 ? Number(model.contextWindow) : 0 }));
      if (!drafts.editorModels.some((model) => model.id === drafts.editorDefaultModel)) drafts.editorDefaultModel = drafts.editorModels[0]?.id || "";
      drafts.dirty = true;
      renderModelManager();
      setModelManagerStatus(`已获取 ${drafts.editorModels.length} 个模型，保存后用于客户端配置`);
    } catch (error) {
      setModelManagerStatus(errorMessage(error), true);
      toast(errorMessage(error), true);
    } finally {
      setButtonLoading(button, false);
    }
  }

  async function leaveEditor() {
    if (drafts.dirty && !(await showConfirmDialog("当前修改尚未保存，确定返回吗？", { title: "未保存的修改" }))) return;
    drafts.dirty = false;
    showView("profiles");
  }

  function updatePreview() {
    const category = $("profileCategory").value || "codex";
    $("previewUrl").textContent = "请求地址：" + (serverState.snapshot?.proxyUrls?.[category] || serverState.snapshot?.proxyUrl || "-");
    $("previewToken").textContent = "密钥：" + (serverState.snapshot?.localAccessToken ? "********" : "-");
  }

  async function saveProfile(event) {
    event.preventDefault();
    let headers;
    try {
      headers = readHeadersField();
    } catch (error) {
      toast(errorMessage(error), true);
      return;
    }
    const models = (drafts.editorModels || []).map((model) => ({
      id: String(model.id || "").trim(), name: String(model.name || "").trim(), ownedBy: String(model.ownedBy || "").trim(), contextWindow: Number(model.contextWindow) > 0 ? Number(model.contextWindow) : 0,
    }));
    if (models.some((model) => !model.id)) {
      toast("模型 ID 不能为空", true);
      return;
    }
    const modelIDs = new Set();
    for (const model of models) {
      if (modelIDs.has(model.id)) {
        toast(`模型 ID 重复：${model.id}`, true);
        return;
      }
      modelIDs.add(model.id);
    }
    if (drafts.editorDefaultModel && !modelIDs.has(drafts.editorDefaultModel)) {
      toast("默认模型不在模型目录中", true);
      return;
    }
    const oldIDs = new Set(serverState.snapshot.profiles.map((profile) => profile.id));
    const existingProfile = serverState.snapshot.profiles.find((profile) => profile.id === navigation.selectedId);
    const existingDogeToken = existingProfile?.source === "doge"
      ? serverState.snapshot?.doge?.tokens?.find((token) => Number(token.id) === Number(existingProfile.remoteTokenId))
      : null;
    const selectedDogeGroup = $("dogeProfileGroup").value;
    const payload = {
      id: drafts.isNew ? "" : navigation.selectedId,
      source: existingProfile?.source || "custom",
      category: $("profileCategory").value,
      dogeGroup: existingDogeToken && selectedDogeGroup !== existingDogeToken.group ? selectedDogeGroup : "",
      name: $("profileName").value.trim(),
      note: $("profileNote").value.trim(),
      baseUrl: $("baseUrl").value.trim(),
      apiKey: $("apiKey").value.trim(),
      headers,
      models,
      defaultModel: drafts.editorDefaultModel,
    };
    const button = $("profileForm").querySelector("button[type=submit]");
    setButtonLoading(button, true, "保存中...");
    try {
      await SaveProfile(payload);
      drafts.dirty = false;
      await loadState();
      if (!payload.id) {
        navigation.selectedId = serverState.snapshot.profiles.find((profile) => !oldIDs.has(profile.id))?.id || null;
        drafts.isNew = false;
      }
      toast("代理 API 配置已保存");
      showView("profiles");
    } catch (error) {
      toast(errorMessage(error), true);
    } finally {
      setButtonLoading(button, false);
    }
  }

  async function activateProfile(id, button = null) {
    const profile = serverState.snapshot?.profiles?.find((item) => item.id === id);
    if (!profile) {
      toast("代理 API 不存在", true);
      return;
    }
    await beginActivation({ category: profile.category, profileId: id, tokenId: 0, button });
  }

  async function testProfile(id, button = null) {
    setButtonLoading(button, true, "测试中...");
    try {
      const result = await TestProfile(id);
      const message = result.ok
        ? "连接成功 · HTTP " + result.status + " · " + result.durationMs + " ms"
        : "已连接，上游返回 HTTP " + result.status;
      toast(message, !result.ok);
    } catch (error) {
      toast(errorMessage(error), true);
    } finally {
      setButtonLoading(button, false);
    }
  }

  async function deleteProfile(id, button = null) {
    const profile = serverState.snapshot.profiles.find((item) => item.id === id);
    if (!profile || !(await showConfirmDialog("确定删除“" + nonHomeProfileName(profile) + "”吗？", { title: "删除代理 API", danger: true }))) return;
    setButtonLoading(button, true, "删除中...");
    try {
      await DeleteProfile(id);
      drafts.dirty = false;
      await loadState();
      showView("profiles");
      toast("代理 API 已删除");
    } catch (error) {
      toast(errorMessage(error), true);
    } finally {
      setButtonLoading(button, false);
    }
  }

  return {
    openEditor,
    addEditorModel,
    fetchEditorModels,
    leaveEditor,
    updatePreview,
    saveProfile,
    activateProfile,
    testProfile,
    deleteProfile,
  };
}
