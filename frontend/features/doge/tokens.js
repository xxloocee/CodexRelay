import { EditDogeToken, GetState, SetDogeTokenCategories } from "../../core/desktop-api.js";
import { $ } from "../../core/dom.js";
import { errorMessage, setButtonLoading, toast } from "../../core/feedback.js";
import { syncModalBody } from "../../core/modal.js";
import { categoryOptions, navigation, runtimeState, serverState } from "../../core/store.js";

export function createDogeTokens({
  loadState,
  beginActivation,
  categoryLabel,
  createDragHandle,
  createProfileInfo,
  formatDogeRatio,
  buildProfileActions,
  installSortableDrag,
  persistFailoverOrder,
  persistDogeTokenOrder,
  setProfileAutoSwitch,
  activateProfile,
  openEditor,
  deleteProfile,
  testProfile,
  nonHomeDogeTokenName,
}) {
  function pendingDogeTokens() {
    return (serverState.snapshot?.doge?.tokens || []).filter((token) => !token.profileId && !token.imported);
  }

  function openDogeCategoryDialog(force = false) {
    if (serverState.snapshot?.needsOnboarding) return;
    const pending = pendingDogeTokens();
    const modal = $("dogeCategoryModal");
    if (!pending.length) {
      modal.classList.add("hidden");
      syncModalBody();
      runtimeState.dogeCategoryDialogSignature = "";
      return;
    }
    const signature = pending.map((token) => `${token.id}:${token.orderKey || String(token.id)}`).join("|");
    if (!force && runtimeState.dogeCategoryDialogSignature === signature) return;
    runtimeState.dogeCategoryDialogSignature = signature;
    const rows = $("dogeCategoryRows");
    const selectedValues = new Map(Array.from(rows.querySelectorAll(".doge-category-select"), (select) => [select.dataset.tokenId, select.value]));
    rows.replaceChildren();
    for (const token of pending) {
      const row = document.createElement("div");
      row.className = "doge-category-row";
      const info = document.createElement("div");
      info.className = "doge-category-info";
      const name = document.createElement("strong");
      name.textContent = nonHomeDogeTokenName(token);
      const detail = document.createElement("small");
      detail.textContent = token.maskedKey || "未返回密钥";
      info.append(name, detail);
      const select = document.createElement("select");
      select.className = "sync-select doge-category-select";
      select.dataset.tokenId = String(token.id);
      select.setAttribute("aria-label", `${nonHomeDogeTokenName(token)}存放分组`);
      select.appendChild(new Option("选择分组", ""));
      for (const category of categoryOptions) select.appendChild(new Option(categoryLabel(category), category));
      if (selectedValues.has(select.dataset.tokenId)) select.value = selectedValues.get(select.dataset.tokenId);
      else if (categoryOptions.includes(token.category)) select.value = token.category;
      row.append(info, select);
      rows.appendChild(row);
    }
    modal.classList.remove("hidden");
    syncModalBody();
    setTimeout(() => rows.querySelector("select")?.focus(), 0);
  }

  function closeDogeCategoryDialog() {
    $("dogeCategoryModal").classList.add("hidden");
    syncModalBody();
  }

  async function saveDogeCategoryAssignments() {
    const assignments = Array.from(document.querySelectorAll(".doge-category-select")).map((select) => ({
      id: Number(select.dataset.tokenId),
      category: select.value,
    }));
    if (assignments.some((assignment) => !assignment.category)) {
      toast("请为每个新令牌选择存放分组", true);
      return;
    }
    const button = $("saveDogeCategories");
    setButtonLoading(button, true, "保存中...");
    try {
      await SetDogeTokenCategories(assignments);
      closeDogeCategoryDialog();
      await loadState();
      toast("二狗子令牌存放分组已保存");
    } catch (error) {
      toast(errorMessage(error), true);
    } finally {
      setButtonLoading(button, false);
    }
  }

  function renderDogeToken(token, list, { sortable = false } = {}) {
    const row = document.createElement("article");
    const unavailable = token.permitted === false;
    const localProfile = findLocalDogeProfile(token);
    const imported = Boolean(localProfile);
    // 目录可用状态只限制切换按钮；已导入 Profile 始终保留在故障顺序中，并继续支持鼠标和键盘排序。
    const sortableInFailoverOrder = sortable && imported;
    row.className = "profile-row doge-token-row" + (token.active ? " active" : "") + (token.needsCategory ? " unassigned" : "") + (unavailable ? " unavailable" : "") + (token.active && unavailable ? " active-unavailable" : "");
    row.dataset.profileId = localProfile?.id || "";
    row.dataset.category = localProfile?.category || token.category || "";
    row.dataset.sortKind = sortableInFailoverOrder ? `failover-${row.dataset.category}` : "doge";
    row.dataset.dogeOrderKey = token.orderKey || String(token.id);
    const dragHandle = createDragHandle();
    const mark = document.createElement("img");
    mark.className = "provider-mark provider-image";
    mark.src = "/logo.png";
    mark.alt = "二狗子";
    const tags = [];
    if (!navigation.sourceFilter) tags.push({ text: "二狗子", tone: "source" });
    if (!navigation.categoryFilter) tags.push({ text: token.category ? categoryLabel(token.category) : "待选择", tone: "category" });
    tags.push({ text: token.groupDisplayName || "未分组", tone: "group" });
    if (token.groupRatio > 0) tags.push({ text: `倍率：${formatDogeRatio(token.groupRatio)}`, tone: "ratio" });
    if (unavailable) tags.push({ text: "当前分组不可用", tone: "unavailable" });
    const info = createProfileInfo({ name: token.name || `令牌 ${token.id}`, tags, note: token.note || "未提供备注" });
    const actions = buildProfileActions({
      active: token.active,
      switchDisabled: token.needsCategory || unavailable,
      onSwitch: imported
        ? (event) => activateProfile(localProfile.id, event.currentTarget)
        : (event) => enableDogeToken(token, event.currentTarget),
      onTest: (event) => testDogeToken(token, event.currentTarget),
      autoSwitchEnabled: !localProfile?.skipAutoSwitch,
      onAutoSwitch: imported ? (event) => setProfileAutoSwitch(localProfile.id, localProfile.skipAutoSwitch, event.currentTarget) : null,
      onEdit: imported ? () => openEditor(localProfile.id) : token.needsCategory ? () => openDogeCategoryDialog(true) : (event) => editDogeToken(token, event.currentTarget),
      editTitle: imported ? "编辑代理 API" : "选择存放分组",
      onDelete: imported ? (event) => deleteProfile(localProfile.id, event.currentTarget) : null,
      deleteDisabled: !imported,
    });
    row.append(dragHandle, mark, info, actions);
    if (sortableInFailoverOrder) {
      installSortableDrag(row, dragHandle, { sortKind: `failover-${row.dataset.category}`, keyAttribute: "profileId", persistOrder: () => persistFailoverOrder(row.dataset.category) });
    } else {
      installSortableDrag(row, dragHandle, { sortKind: "doge", keyAttribute: "dogeOrderKey", persistOrder: persistDogeTokenOrder });
    }
    list.appendChild(row);
  }

  function findLocalDogeProfile(token) {
    const profileID = String(token?.profileId || "").trim();
    if (profileID) {
      return serverState.snapshot?.profiles?.find((profile) => profile.id === profileID && profile.source === "doge") || null;
    }
    const remoteTokenID = Number(token?.id);
    if (!Number.isFinite(remoteTokenID) || remoteTokenID <= 0) return null;
    return serverState.snapshot?.profiles?.find((profile) => profile.source === "doge" && Number(profile.remoteTokenId) === remoteTokenID) || null;
  }

  async function enableDogeToken(token, button = null) {
    let category = token.category || "";
    if (!category) {
      toast("请先为二狗子令牌选择存放类别", true);
      return;
    }
    if (!token.imported) {
      setButtonLoading(button, true, "准备中...");
      try {
        await EditDogeToken(token.id);
        const nextState = await GetState();
        const imported = nextState.doge?.tokens?.find((item) => item.id === token.id);
        if (!imported?.profileId) throw new Error("二狗子令牌尚未生成本地代理 API");
        token = imported;
        category = imported.category || category;
      } catch (error) {
        toast(errorMessage(error), true);
        setButtonLoading(button, false);
        return;
      } finally {
        setButtonLoading(button, false);
      }
    }
    await beginActivation({ category, profileId: token.profileId, tokenId: 0, button });
  }

  async function editDogeToken(token, button = null) {
    // 已有本地代理档案时直接打开编辑页，避免因同步竞态重复请求远端密钥。
    if (token?.profileId) {
      openEditor(token.profileId);
      return;
    }
    setButtonLoading(button, true, "读取中...");
    try {
      await EditDogeToken(token.id);
      await loadState();
      const imported = serverState.snapshot.doge?.tokens?.find((item) => item.id === token.id);
      if (imported?.profileId) openEditor(imported.profileId);
    } catch (error) {
      toast(errorMessage(error), true);
    } finally {
      setButtonLoading(button, false);
    }
  }

  async function testDogeToken(token, button) {
    if (token.needsCategory) {
      toast("请先为二狗子令牌选择存放类别", true);
      return;
    }
    setButtonLoading(button, true, "测试中...");
    try {
      let profileId = token.profileId;
      if (!profileId) {
        await EditDogeToken(token.id);
        const nextState = await GetState();
        profileId = nextState.doge?.tokens?.find((item) => item.id === token.id)?.profileId;
      }
      if (!profileId) throw new Error("二狗子令牌尚未生成本地代理 API");
      await testProfile(profileId, button);
      await loadState();
    } catch (error) {
      toast(errorMessage(error), true);
    } finally {
      setButtonLoading(button, false);
    }
  }

  return {
    pendingDogeTokens,
    openDogeCategoryDialog,
    closeDogeCategoryDialog,
    saveDogeCategoryAssignments,
    renderDogeToken,
  };
}
