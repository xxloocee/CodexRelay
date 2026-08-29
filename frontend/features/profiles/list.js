import { ReorderDogeTokens, ReorderFailoverProfiles, SetProfileAutoSwitch } from "../../core/desktop-api.js";
import { $, icon } from "../../core/dom.js";
import { errorMessage, setButtonLoading, toast } from "../../core/feedback.js";
import { categoryOptions, navigation, runtimeState, serverState } from "../../core/store.js";

export function createProfileList({
  loadState,
  renderShell,
  renderDogeToken,
  isDogeSyncing,
  isCategoryVisible,
  activateProfile,
  testProfile,
  openEditor,
  deleteProfile,
}) {
  function orderedProfiles() {
    const byID = new Map((serverState.snapshot?.profiles || []).map((profile) => [profile.id, profile]));
    const ordered = [];
    const seen = new Set();
    for (const category of categoryOptions) {
      const ids = serverState.snapshot?.failoverOrder?.[category] || [];
      for (const id of ids) {
        const profile = byID.get(id);
        if (profile && profile.category === category && !seen.has(id)) {
          ordered.push(profile);
          seen.add(id);
        }
      }
    }
    for (const profile of serverState.snapshot?.profiles || []) {
      if (!seen.has(profile.id)) ordered.push(profile);
    }
    return ordered;
  }

  function renderProfileRow(profile, list) {
    const row = document.createElement("article");
    row.className = "profile-row" + (profile.active ? " active" : "");
    row.dataset.profileId = profile.id;
    row.dataset.category = profile.category;
    row.dataset.sortKind = `failover-${profile.category}`;

    const dragHandle = createDragHandle();
    const mark = document.createElement("span");
    mark.className = "provider-mark";
    mark.textContent = providerInitial(profile.name);
    const tags = [];
    if (!navigation.sourceFilter) tags.push({ text: sourceLabel(profile.source), tone: "source" });
    if (!navigation.categoryFilter) tags.push({ text: categoryLabel(profile.category), tone: "category" });
    const info = createProfileInfo({ name: profile.name, tags, note: profile.note, active: profile.active });
    const actions = buildProfileActions({
      active: profile.active,
      onSwitch: (event) => activateProfile(profile.id, event.currentTarget),
      onTest: (event) => testProfile(profile.id, event.currentTarget),
      autoSwitchEnabled: !profile.skipAutoSwitch,
      onAutoSwitch: (event) => setProfileAutoSwitch(profile.id, profile.skipAutoSwitch, event.currentTarget),
      onEdit: () => openEditor(profile.id),
      onDelete: (event) => deleteProfile(profile.id, event.currentTarget),
    });
    row.append(dragHandle, mark, info, actions);
    installSortableDrag(row, dragHandle, { sortKind: `failover-${profile.category}`, keyAttribute: "profileId", persistOrder: () => persistFailoverOrder(profile.category) });
    list.appendChild(row);
  }

  function renderProfiles() {
    if (runtimeState.draggingSortKey) return;
    if (navigation.categoryFilter && !isCategoryVisible(navigation.categoryFilter)) navigation.categoryFilter = "";
    const list = $("profileList");
    list.replaceChildren();
    const allProfiles = orderedProfiles();
    const dogeTokens = serverState.snapshot?.doge?.tokens || [];
    const dogeByProfileID = new Map(dogeTokens.filter((token) => token.profileId).map((token) => [token.profileId, token]));
    const profiles = allProfiles.filter((profile) => profileMatchesFilters(profile));
    renderFilterButtons();
    for (const profile of profiles) {
      if (profile.source === "doge") {
        // 二狗子 Profile 必须以最新目录实体为准；缺失项不得退回普通可切换行。
        const token = dogeByProfileID.get(profile.id);
        if (token) renderDogeToken(token, list, { sortable: true });
      } else {
        renderProfileRow(profile, list);
      }
    }
    const dogeSelected = navigation.sourceFilter === "doge";
    const dogeVisible = !navigation.sourceFilter || dogeSelected;
    const dogeSyncing = dogeVisible && Boolean(serverState.snapshot.doge?.bound) && isDogeSyncing();
    const dogeSyncError = dogeVisible && Boolean(serverState.snapshot.doge?.bound) && Boolean(serverState.snapshot.doge?.lastSyncError);
    const dogeFailedBeforeData = dogeSyncError && !serverState.snapshot.doge?.lastSyncAt && !dogeSyncing;
    const hasRows = profiles.length > 0;
    $("emptyProfiles").classList.toggle("hidden", hasRows);
    $("emptyProfilesTitle").textContent = dogeSyncing ? "二狗子 API 同步中..." : (dogeFailedBeforeData ? "二狗子 API 同步失败，请重试" : (dogeSelected ? (serverState.snapshot.doge?.bound ? "二狗子暂无令牌" : "请在设置中绑定二狗子") : "还没有代理 API"));
    $("emptyAdd").classList.toggle("hidden", dogeSelected);
  }

  function appendProfileTag(parent, text, tone) {
    const tag = document.createElement("span");
    tag.className = `profile-kind profile-tag-${tone}`;
    tag.textContent = text;
    parent.appendChild(tag);
  }

  function createDragHandle() {
    const handle = document.createElement("button");
    handle.type = "button";
    handle.className = "drag-handle";
    handle.title = "拖动排序";
    handle.setAttribute("aria-label", "拖动排序");
    handle.appendChild(icon("grip-vertical"));
    return handle;
  }

  function createProfileInfo({ name, tags = [], note = "", active = false }) {
    const info = document.createElement("div");
    info.className = "profile-info";
    const nameLine = document.createElement("div");
    nameLine.className = "profile-name-line";
    const nameNode = document.createElement("span");
    nameNode.className = "profile-name";
    nameNode.textContent = name;
    nameLine.appendChild(nameNode);
    for (const tag of tags) appendProfileTag(nameLine, tag.text, tag.tone);
    if (active) {
      const pill = document.createElement("span");
      pill.className = "active-pill";
      pill.textContent = "使用中";
      nameLine.appendChild(pill);
    }
    info.appendChild(nameLine);
    if (note) {
      const noteNode = document.createElement("span");
      noteNode.className = "profile-note";
      noteNode.textContent = note;
      noteNode.title = note;
      info.appendChild(noteNode);
    }
    return info;
  }

  function formatDogeRatio(value) {
    const ratio = Number(value);
    return Number.isFinite(ratio) ? String(ratio) : "-";
  }

  function formatNonHomeDogeName(name, group, ratio) {
    const cleanName = String(name || "未命名令牌").trim() || "未命名令牌";
    const cleanGroup = String(group || "").trim();
    if (!cleanGroup) return cleanName;
    const numericRatio = Number(ratio);
    return numericRatio > 0
      ? `${cleanName} (${cleanGroup}·${formatDogeRatio(numericRatio)})`
      : `${cleanName} (${cleanGroup})`;
  }

  function nonHomeDogeTokenName(token) {
    return formatNonHomeDogeName(token?.name || `令牌 ${token?.id || ""}`, token?.groupDisplayName, token?.groupRatio);
  }

  // 主界面以名称、分组和倍率分开显示；其他页面统一使用完整令牌名称。
  function nonHomeProfileName(profile) {
    const name = String(profile?.name || "代理 API").trim();
    if (profile?.source === "custom") return `${name}（自定义 API）`;
    if (profile?.source !== "doge") return name;
    const token = (serverState.snapshot?.doge?.tokens || []).find((item) => Number(item.id) === Number(profile.remoteTokenId));
    return formatNonHomeDogeName(name, token?.groupDisplayName, token?.groupRatio);
  }

  function profileMatchesFilters(profile) {
    return isCategoryVisible(profile.category) &&
      (!navigation.sourceFilter || profile.source === navigation.sourceFilter) &&
      (!navigation.categoryFilter || profile.category === navigation.categoryFilter);
  }

  function renderFilterButtons() {
    const activeCategories = new Set((serverState.snapshot.profiles || []).filter((profile) => profile.active).map((profile) => profile.category));
    document.querySelectorAll(".filter-options").forEach((group) => {
      const isCategoryGroup = group.dataset.filterGroup === "category";
      const value = isCategoryGroup ? navigation.categoryFilter : navigation.sourceFilter;
      group.querySelectorAll(".filter-option").forEach((button) => {
        const categoryHidden = isCategoryGroup && button.dataset.filterValue && !isCategoryVisible(button.dataset.filterValue);
        button.classList.toggle("hidden", categoryHidden);
        const active = button.dataset.filterValue === value;
        button.classList.toggle("active", active);
        const hasActiveProfile = isCategoryGroup && activeCategories.has(button.dataset.filterValue);
        button.classList.toggle("has-active", hasActiveProfile);
        button.setAttribute("aria-selected", String(active));
        button.title = hasActiveProfile ? `${categoryLabel(button.dataset.filterValue)} 已启用代理 API` : "";
      });
    });
  }

  function setFilter(group, value) {
    if (group === "category" && value && !isCategoryVisible(value)) return;
    if (group === "source") navigation.sourceFilter = value;
    if (group === "category") navigation.categoryFilter = value;
    navigation.viewFilterInitialized = true;
    renderShell();
    renderProfiles();
  }

  function categoryLabel(category) {
    return { codex: "Codex", claude: "Claude", gemini: "Gemini", grok: "Grok", opencode: "OpenCode", openclaw: "OpenClaw", hermes: "Hermes", image: "生图", other: "其他" }[category] || "未分类";
  }

  function sourceLabel(source) {
    return source === "doge" ? "二狗子" : "自定义";
  }

  // 统一自定义 API 与二狗子令牌的鼠标、键盘排序行为；稳定身份字段和保存回调由调用方提供。
  function installSortableDrag(row, handle, { sortKind, keyAttribute, persistOrder }) {
    const readKey = () => row.dataset[keyAttribute] || "";
    const dataAttribute = `data-${keyAttribute.replace(/[A-Z]/g, (letter) => "-" + letter.toLowerCase())}`;
    const findRow = (key) => document.querySelector(`[${dataAttribute}="${CSS.escape(key)}"]`);
    row.draggable = true;
    handle.draggable = true;
    handle.addEventListener("keydown", async (event) => {
      if (event.key !== "ArrowUp" && event.key !== "ArrowDown") return;
      event.preventDefault();
      const sibling = event.key === "ArrowUp" ? previousSortableSibling(row) : nextSortableSibling(row);
      if (!sibling) return;
      moveSortableRow(row.parentElement, row, sibling, event.key === "ArrowDown");
      await persistOrder();
      findRow(readKey())?.querySelector(".drag-handle")?.focus();
    });
    row.addEventListener("dragstart", (event) => {
      if (!event.target.closest(".drag-handle")) {
        event.preventDefault();
        return;
      }
      runtimeState.draggingSortKey = readKey();
      row.classList.add("dragging");
      event.dataTransfer.effectAllowed = "move";
      event.dataTransfer.setData("text/plain", readKey());
    });
    row.addEventListener("dragover", (event) => {
      if (!runtimeState.draggingSortKey || row.dataset.sortKind !== sortKind || runtimeState.draggingSortKey === readKey()) return;
      event.preventDefault();
      event.dataTransfer.dropEffect = "move";
      const dragged = findRow(runtimeState.draggingSortKey);
      if (!dragged) return;
      const bounds = row.getBoundingClientRect();
      const insertAfter = event.clientY > bounds.top + bounds.height / 2;
      moveSortableRow(row.parentElement, dragged, row, insertAfter);
    });
    row.addEventListener("drop", (event) => event.preventDefault());
    row.addEventListener("dragend", async () => {
      row.classList.remove("dragging");
      runtimeState.draggingSortKey = null;
      await persistOrder();
    });
  }

  function previousSortableSibling(row) {
    let sibling = row.previousElementSibling;
    while (sibling && sibling.dataset.sortKind !== row.dataset.sortKind) sibling = sibling.previousElementSibling;
    return sibling;
  }

  function nextSortableSibling(row) {
    let sibling = row.nextElementSibling;
    while (sibling && sibling.dataset.sortKind !== row.dataset.sortKind) sibling = sibling.nextElementSibling;
    return sibling;
  }

  function moveSortableRow(list, dragged, target, insertAfter) {
    if (insertAfter && target.nextElementSibling === dragged) return;
    if (!insertAfter && target.previousElementSibling === dragged) return;
    const positions = new Map(Array.from(list.children).map((row) => [row, row.getBoundingClientRect().top]));
    list.insertBefore(dragged, insertAfter ? target.nextElementSibling : target);
    for (const row of list.children) {
      if (row === dragged) continue;
      const delta = positions.get(row) - row.getBoundingClientRect().top;
      if (!delta) continue;
      row.animate(
        [{ transform: `translateY(${delta}px)` }, { transform: "translateY(0)" }],
        { duration: 170, easing: "cubic-bezier(.2,.8,.2,1)" },
      );
    }
  }

  async function persistFailoverOrder(category) {
    const sortKind = `failover-${category}`;
    const ids = Array.from($("profileList").children).filter((row) => row.dataset.sortKind === sortKind).map((row) => row.dataset.profileId).filter(Boolean);
    if (!ids.length) return;
    const current = [...(serverState.snapshot.failoverOrder?.[category] || [])];
    const visible = new Set(ids);
    const next = current.slice();
    let cursor = 0;
    for (let index = 0; index < next.length; index += 1) {
      if (visible.has(next[index])) next[index] = ids[cursor++];
    }
    while (cursor < ids.length) next.push(ids[cursor++]);
    if (next.every((id, index) => id === current[index]) && next.length === current.length) return;
    await persistOrder(() => ReorderFailoverProfiles(category, next), "令牌切换顺序已更新");
  }

  async function persistDogeTokenOrder() {
    const ids = Array.from($("profileList").children).filter((row) => row.dataset.sortKind === "doge").map((row) => row.dataset.dogeOrderKey).filter(Boolean);
    await persistOrder(() => ReorderDogeTokens(ids), "二狗子令牌排序已更新");
  }

  // 统一排序后的刷新、成功提示和失败恢复；排序数据本身由各类别保存函数整理。
  async function persistOrder(save, successMessage) {
    try {
      await save();
      await loadState();
      toast(successMessage);
    } catch (error) {
      await loadState();
      toast(errorMessage(error), true);
    }
  }

  function buildProfileActions({ active, switchDisabled = false, onSwitch, onTest, autoSwitchEnabled = true, onAutoSwitch, onEdit, onDelete, editTitle = "编辑代理 API", deleteDisabled = false }) {
    const actions = document.createElement("div");
    actions.className = "profile-actions";
    const buttons = [
      actionButton(active ? "当前" : "切换", active ? "use-button current" : "use-button", "切换当前代理 API", active ? "check" : "play", onSwitch, active || switchDisabled),
      actionButton("", "row-icon-button", "测试令牌 API", "activity", onTest),
      actionButton("", "row-icon-button", editTitle, "edit", onEdit),
      actionButton("", "row-icon-button danger", "删除本地代理 API", "trash-2", onDelete, deleteDisabled),
    ];
    if (serverState.snapshot?.tokenSwitch?.mode === "auto" && onAutoSwitch) buttons.splice(2, 0, autoSwitchButton(autoSwitchEnabled, onAutoSwitch));
    actions.append(...buttons);
    return actions;
  }

  function autoSwitchButton(enabled, handler) {
    const button = document.createElement("button");
    button.type = "button";
    button.className = "row-icon-button";
    button.title = enabled ? "参与自动切换，点击后跳过此令牌" : "已跳过此令牌，点击后恢复自动切换";
    button.setAttribute("aria-label", button.title);
    const image = document.createElement("img");
    image.className = "profile-auto-switch-icon";
    image.src = enabled ? "/icons/auto.svg" : "/icons/skip.svg";
    image.alt = "";
    button.appendChild(image);
    button.addEventListener("click", handler);
    return button;
  }

  async function setProfileAutoSwitch(id, currentlySkipped, button) {
    setButtonLoading(button, true);
    try {
      await SetProfileAutoSwitch(id, currentlySkipped);
      await loadState();
      toast(currentlySkipped ? "已恢复参与自动切换" : "自动切换将跳过此令牌");
    } catch (error) {
      toast(errorMessage(error), true);
      setButtonLoading(button, false);
    }
  }

  function actionButton(label, className, title, iconName, handler, disabled = false) {
    const button = document.createElement("button");
    button.type = "button";
    button.className = className;
    button.appendChild(icon(iconName));
    if (label) {
      const text = document.createElement("span");
      text.textContent = label;
      text.dataset.buttonLabel = "";
      button.appendChild(text);
    }
    button.title = title;
    button.setAttribute("aria-label", title);
    button.disabled = disabled;
    button.addEventListener("click", handler);
    return button;
  }

  function providerInitial(name) {
    const first = Array.from((name || "C").trim())[0] || "C";
    return /^[A-Za-z0-9]$/.test(first) ? first.toUpperCase() : "API";
  }

  return {
    renderProfiles,
    setFilter,
    categoryLabel,
    createDragHandle,
    createProfileInfo,
    formatDogeRatio,
    nonHomeDogeTokenName,
    nonHomeProfileName,
    installSortableDrag,
    persistFailoverOrder,
    persistDogeTokenOrder,
    buildProfileActions,
    setProfileAutoSwitch,
  };
}
