import { ClearUsage } from "../core/desktop-api.js";
import { $ } from "../core/dom.js";
import { errorMessage, setButtonLoading, toast } from "../core/feedback.js";
import { showConfirmDialog } from "../core/modal.js";
import { navigation, serverState } from "../core/store.js";

export function createUsage({ loadState, nonHomeProfileName }) {
  function renderRequests() {
    const rows = $("requestRows");
    rows.replaceChildren();
    renderUsageProfileOptions();
    const allRequests = serverState.snapshot.requests || [];
    const requests = allRequests.filter((request) => usageRequestMatches(request));
    $("requestCount").textContent = requests.length === allRequests.length
      ? requests.length + " 条请求"
      : requests.length + " / " + allRequests.length + " 条请求";
    $("emptyRequests").textContent = allRequests.length ? "当前范围暂无请求" : "暂无请求";
    $("emptyRequests").classList.toggle("hidden", requests.length > 0);
    renderUsageSummary();
    for (const request of requests) {
      const row = document.createElement("tr");
      const reported = request.usageStatus === "reported";
      row.append(
        cell(formatRequestTime(request.startedAt)),
        cell(nonHomeProfileName((serverState.snapshot.profiles || []).find((profile) => profile.id === request.profileId) || { name: request.profile || "-" })),
        cell(request.model || "-"),
        cell(request.method + " " + request.path, "path"),
        cell(reported ? formatTokens(request.inputTokens) : "-", "token-value"),
        cell(reported ? formatTokens(request.outputTokens) : "-", "token-value"),
        cell(reported ? formatTokens(request.cachedTokens) : "-", "token-value"),
        cell(reported ? formatTokens(request.totalTokens) : "未上报", reported ? "token-value total-token" : "usage-missing"),
        cell(String(request.status), request.status >= 200 && request.status < 400 ? "status-ok" : "status-error"),
        cell(request.durationMs + " ms"),
      );
      rows.appendChild(row);
    }
  }

  function renderUsageProfileOptions() {
    const select = $("usageProfile");
    const names = new Map((serverState.snapshot.profiles || []).map((profile) => [profile.id, nonHomeProfileName(profile)]));
    for (const request of serverState.snapshot.requests || []) {
      if (request.profileId && !names.has(request.profileId)) names.set(request.profileId, request.profile || "已删除中转站");
    }
    const current = navigation.usageProfile;
    select.replaceChildren(new Option("全部代理 API", ""));
    for (const [id, name] of names) select.appendChild(new Option(name, id));
    if (current && names.has(current)) select.value = current;
    else navigation.usageProfile = "";
  }

  function usageRequestMatches(request) {
    if (navigation.usageProfile && request.profileId !== navigation.usageProfile) return false;
    const days = usageRangeDays(navigation.usageRange);
    if (!days) return true;
    const start = new Date();
    start.setHours(0, 0, 0, 0);
    start.setDate(start.getDate() - (days - 1));
    return new Date(request.startedAt) >= start;
  }

  function usageRangeDays(range) {
    if (range === "today") return 1;
    if (range === "7d") return 7;
    if (range === "30d") return 30;
    return 0;
  }

  function renderUsageSummary() {
    const usage = serverState.snapshot.usage || { total: {}, profiles: {}, days: {} };
    const aggregate = emptyUsageAggregate();
    const days = usageRangeDays(navigation.usageRange);
    if (!days) {
      addUsageAggregate(aggregate, navigation.usageProfile ? usage.profiles?.[navigation.usageProfile] : usage.total);
    } else {
      const date = new Date();
      date.setHours(12, 0, 0, 0);
      for (let offset = 0; offset < days; offset++) {
        const key = localDateKey(date);
        const day = usage.days?.[key];
        addUsageAggregate(aggregate, navigation.usageProfile ? day?.profiles?.[navigation.usageProfile] : day?.total);
        date.setDate(date.getDate() - 1);
      }
    }
    $("usageTotal").textContent = formatTokens(aggregate.totalTokens);
    $("usageInput").textContent = formatTokens(aggregate.inputTokens);
    $("usageOutput").textContent = formatTokens(aggregate.outputTokens);
    $("usageCached").textContent = formatTokens(aggregate.cachedTokens);
    $("usageRequests").textContent = formatTokens(aggregate.requests);
    $("usageReported").textContent = aggregate.requests && aggregate.reportedRequests !== aggregate.requests
      ? aggregate.reportedRequests + " 条已上报"
      : "";
  }

  function emptyUsageAggregate() {
    return { requests: 0, reportedRequests: 0, inputTokens: 0, outputTokens: 0, cachedTokens: 0, totalTokens: 0 };
  }

  function addUsageAggregate(target, source) {
    if (!source) return;
    for (const key of Object.keys(target)) target[key] += Number(source[key] || 0);
  }

  function localDateKey(date) {
    const year = date.getFullYear();
    const month = String(date.getMonth() + 1).padStart(2, "0");
    const day = String(date.getDate()).padStart(2, "0");
    return `${year}-${month}-${day}`;
  }

  function formatTokens(value) {
    return new Intl.NumberFormat("zh-CN").format(Number(value || 0));
  }

  function formatRequestTime(value) {
    const date = new Date(value);
    const days = usageRangeDays(navigation.usageRange);
    return days === 1
      ? date.toLocaleTimeString("zh-CN", { hour12: false })
      : date.toLocaleString("zh-CN", { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit", hour12: false });
  }

  async function clearUsage() {
    if (!(await showConfirmDialog("确定清空全部 Token 统计和最近请求明细吗？此操作无法恢复。", { title: "清空用量统计", danger: true }))) return;
    const button = $("clearUsage");
    setButtonLoading(button, true, "清空中...");
    try {
      await ClearUsage();
      await loadState();
      toast("用量统计已清空");
    } catch (error) {
      toast(errorMessage(error), true);
    } finally {
      setButtonLoading(button, false);
    }
  }

  function cell(text, className = "") {
    const td = document.createElement("td");
    td.textContent = text;
    td.title = text;
    if (className) td.className = className;
    return td;
  }

  function mount() {
    document.querySelectorAll("#usageRanges button").forEach((button) => button.addEventListener("click", () => {
      navigation.usageRange = button.dataset.range;
      document.querySelectorAll("#usageRanges button").forEach((item) => item.classList.toggle("active", item === button));
      renderRequests();
    }));
    $("usageProfile").addEventListener("change", () => {
      navigation.usageProfile = $("usageProfile").value;
      renderRequests();
    });
    $("clearUsage").addEventListener("click", clearUsage);
  }

  return { renderRequests, mount };
}
