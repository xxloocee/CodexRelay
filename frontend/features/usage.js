import { $ } from "../core/dom.js";
import { setButtonLoading } from "../core/feedback.js";
import { navigation, serverState } from "../core/store.js";
import { createBillingAnalysis } from "./usage/billing.js";
import { createUsageLogs } from "./usage/logs.js";

export function createUsage() {
  const loadingSources = new Set();
  const logs = createUsageLogs({
    onLoading: (loading) => setLoading("logs", loading),
    onCountChange: (text) => setResultCount("logs", text),
    groupName: displayGroupName,
  });
  const billing = createBillingAnalysis({
    onLoading: (loading) => setLoading("billing", loading),
    onCountChange: (text) => setResultCount("billing", text),
    groupName: displayGroupName,
  });

  function renderRequests() {
    renderFilterOptions();
    renderSection();
  }

  function activate() {
    renderRequests();
    refresh();
  }

  function refresh() {
    if (navigation.usageSection === "billing") return billing.load();
    return logs.load(navigation.usagePage);
  }

  function setSection(section, { load = true } = {}) {
    navigation.usageSection = section === "billing" ? "billing" : "logs";
    renderSection();
    if (load) refresh();
  }

  function renderSection() {
    const section = navigation.usageSection === "billing" ? "billing" : "logs";
    $("usageLogsView").classList.toggle("hidden", section !== "logs");
    $("usageBillingView").classList.toggle("hidden", section !== "billing");
    document.querySelectorAll("#usageSections button").forEach((button) => {
      const active = button.dataset.section === section;
      button.classList.toggle("active", active);
      button.setAttribute("aria-selected", String(active));
      button.tabIndex = active ? 0 : -1;
    });
  }

  function renderFilterOptions() {
    const doge = serverState.snapshot?.doge || {};
    const tokens = uniqueOptions((doge.tokens || []).map((token) => ({ value: token.name, label: token.name })));
    const groups = uniqueOptions((doge.groups || []).map((group) => ({ value: group, label: displayGroupName(group) })));
    renderSelect("usageToken", "全部令牌", tokens, navigation.usageToken);
    renderSelect("usageGroup", "全部分组", groups, navigation.usageGroup, displayGroupName(navigation.usageGroup));
    $("usageModel").value = navigation.usageModel;
    document.querySelectorAll("#usageRanges button").forEach((button) => button.classList.toggle("active", button.dataset.range === navigation.usageRange));
  }

  function displayGroupName(value) {
    const group = String(value || "").trim();
    if (!group) return "";
    const doge = serverState.snapshot?.doge || {};
    const configured = String(doge.groupDisplayNames?.[group] || "").trim();
    if (configured) return configured;
    const token = (doge.tokens || []).find((item) => item.group === group && item.groupDisplayName);
    return String(token?.groupDisplayName || group);
  }

  function renderSelect(id, placeholder, options, current, currentLabel = current) {
    const select = $(id);
    select.replaceChildren(new Option(placeholder, ""));
    const values = new Set();
    for (const option of options) {
      if (!option.value || values.has(option.value)) continue;
      values.add(option.value);
      select.appendChild(new Option(option.label, option.value));
    }
    if (current && !values.has(current)) select.appendChild(new Option(currentLabel || current, current));
    select.value = current || "";
  }

  function setLoading(source, loading) {
    if (loading) loadingSources.add(source);
    else loadingSources.delete(source);
    setButtonLoading($("refreshUsage"), loadingSources.size > 0, "刷新中...");
  }

  function setResultCount(source, text) {
    if (navigation.usageSection === source) $("usageResultCount").textContent = text;
  }

  function resetPageAndRefresh() {
    navigation.usagePage = 1;
    refresh();
  }

  function resetFilters() {
    navigation.usageRange = "today";
    navigation.usageToken = "";
    navigation.usageModel = "";
    navigation.usageGroup = "";
    navigation.usagePage = 1;
    renderFilterOptions();
    refresh();
  }

  function mount() {
    document.querySelectorAll("#usageSections button").forEach((button) => button.addEventListener("click", () => setSection(button.dataset.section)));
    document.querySelectorAll("#usageRanges button").forEach((button) => button.addEventListener("click", () => {
      navigation.usageRange = button.dataset.range;
      document.querySelectorAll("#usageRanges button").forEach((item) => item.classList.toggle("active", item === button));
      resetPageAndRefresh();
    }));
    $("usageToken").addEventListener("change", () => {
      navigation.usageToken = $("usageToken").value;
      resetPageAndRefresh();
    });
    $("usageGroup").addEventListener("change", () => {
      navigation.usageGroup = $("usageGroup").value;
      resetPageAndRefresh();
    });
    $("usageModel").addEventListener("input", () => {
      navigation.usageModel = $("usageModel").value.trim();
    });
    $("usageModel").addEventListener("keydown", (event) => {
      if (event.key !== "Enter") return;
      event.preventDefault();
      resetPageAndRefresh();
    });
    $("resetUsageFilters").addEventListener("click", resetFilters);
    $("refreshUsage").addEventListener("click", refresh);
    logs.mount();
    billing.mount();
    renderRequests();
  }

  return { activate, mount, renderRequests };
}

function uniqueOptions(options) {
  const seen = new Set();
  return options.filter((option) => {
    const value = String(option.value || "").trim();
    if (!value || seen.has(value)) return false;
    seen.add(value);
    option.value = value;
    option.label = String(option.label || value);
    return true;
  });
}
