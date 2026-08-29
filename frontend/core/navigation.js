import { $ } from "./dom.js";
import { categoryOptions, navigation, patchDomain, serverState } from "./store.js";

export function createNavigation({ renderShell, renderProfiles }) {
  function visibleCategorySet() {
    const configured = serverState.snapshot?.preferences?.visibleCategories;
    return new Set(Array.isArray(configured) && configured.length ? configured : categoryOptions);
  }

  function isCategoryVisible(category) {
    return visibleCategorySet().has(category);
  }

  function applyDefaultViewFilter(force = false) {
    if (!serverState.snapshot || (!force && navigation.viewFilterInitialized)) return;
    const preferences = serverState.snapshot.preferences || {};
    let categoryFilter = preferences.defaultCategory || "";
    if (categoryFilter && !isCategoryVisible(categoryFilter)) categoryFilter = "";
    patchDomain("navigation", {
      sourceFilter: preferences.defaultSource || "",
      categoryFilter,
      viewFilterInitialized: true,
    });
  }

  function showView(name) {
    patchDomain("navigation", { view: name });
    $("profilesView").classList.toggle("hidden", name !== "profiles");
    $("editorView").classList.toggle("hidden", name !== "editor");
    $("settingsView").classList.toggle("hidden", name !== "settings");
    window.scrollTo(0, 0);
  }

  function restoreDefaultViewFilter() {
    applyDefaultViewFilter(true);
    renderShell();
    renderProfiles();
  }

  return {
    applyDefaultViewFilter,
    isCategoryVisible,
    restoreDefaultViewFilter,
    showView,
    visibleCategorySet,
  };
}
