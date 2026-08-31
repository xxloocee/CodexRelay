import { GetDogeUsageLogs } from "../../core/desktop-api.js";
import { $ } from "../../core/dom.js";
import { errorMessage } from "../../core/feedback.js";
import { navigation } from "../../core/store.js";
import {
  buildAnalyticsQuery,
  effectiveGroupRatio,
  formatCachePercent,
  formatDateTime,
  formatDuration,
  formatLogDetails,
  formatNumber,
  formatQuota,
  parseLogOther,
  timingTone,
} from "./format.js";

export function createUsageLogs({ onLoading, onCountChange, groupName }) {
  let data = { page: 1, page_size: navigation.usagePageSize, total: 0, items: [], quota_per_unit: 500000 };
  let loading = false;
  let requestSequence = 0;

  async function load(page = navigation.usagePage) {
    const sequence = ++requestSequence;
    navigation.usagePage = Math.max(1, Number(page) || 1);
    loading = true;
    setState("正在加载使用日志...");
    onCountChange("正在加载使用日志...");
    setPaginationLoading(true);
    $("usageLogsView").setAttribute("aria-busy", "true");
    onLoading(true);
    try {
      const result = await GetDogeUsageLogs(buildAnalyticsQuery(navigation, navigation.usagePage));
      if (sequence !== requestSequence) return;
      const pageSize = normalizePageSize(result.page_size, navigation.usagePageSize);
      const total = Math.max(0, Number(result.total || 0));
      const pages = Math.max(1, Math.ceil(total / pageSize));
      const resultPage = Math.max(1, Number(result.page) || navigation.usagePage);
      if (total > 0 && resultPage > pages) return load(pages);
      navigation.usagePage = Math.min(resultPage, pages);
      navigation.usagePageSize = pageSize;
      data = { ...result, page: navigation.usagePage, page_size: pageSize, total };
      render();
    } catch (error) {
      if (sequence !== requestSequence) return;
      data = { page: navigation.usagePage, page_size: navigation.usagePageSize, total: 0, items: [], quota_per_unit: 500000 };
      renderRows();
      renderPagination();
      setState(errorMessage(error));
      onCountChange("使用日志加载失败");
    } finally {
      if (sequence === requestSequence) {
        loading = false;
        $("usageLogsView").setAttribute("aria-busy", "false");
        onLoading(false);
        renderPagination();
      }
    }
  }

  function render() {
    renderRows();
    renderPagination();
    const count = Number(data.total || 0);
    onCountChange(formatNumber(count) + " 条消费日志");
    setState((data.items || []).length ? "" : "当前筛选范围暂无消费日志");
  }

  function renderRows() {
    const rows = $("usageLogRows");
    rows.replaceChildren();
    for (const log of data.items || []) rows.appendChild(renderLogRow(log));
  }

  function renderLogRow(log) {
    const row = document.createElement("tr");
    const other = parseLogOther(log.other);
    row.append(
      timeCell(log),
      tokenCell(log, other),
      modelCell(log),
      streamCell(log, other),
      tokensCell(log, other),
      costCell(log),
      timingCell(log, other),
      detailsCell(log, other),
    );
    return row;
  }

  function timeCell(log) {
    const stack = document.createElement("div");
    stack.className = "usage-cell-stack";
    stack.append(textSpan(formatDateTime(log.created_at), "usage-mono"), textSpan("消耗", "usage-log-status"));
    return cell(stack);
  }

  function tokenCell(log, other) {
    const stack = document.createElement("div");
    stack.className = "usage-cell-stack usage-token-cell";
    const name = textSpan(log.token_name || "-", "usage-key-badge");
    name.title = log.token_name || "";
    stack.appendChild(name);
    const group = groupName(log.group || other.group || "");
    const ratio = effectiveGroupRatio(other);
    if (group || ratio != null) {
      const detail = [group, ratio == null ? "" : ratio + "x"].filter(Boolean).join(" ");
      stack.appendChild(textSpan(detail, "usage-cell-note usage-group-note"));
    }
    return cell(stack);
  }

  function modelCell(log) {
    const badge = textSpan(log.model_name || "-", "usage-model-badge");
    badge.title = log.model_name || "";
    return cell(badge);
  }

  function streamCell(log) {
    const stack = document.createElement("div");
    stack.className = "usage-cell-stack";
    stack.appendChild(textSpan(log.is_stream ? "流" : "非流", log.is_stream ? "usage-stream-label" : "usage-cell-note"));
    const seconds = Number(log.use_time || 0);
    const tps = seconds > 0 ? Number(log.completion_tokens || 0) / seconds : 0;
    stack.appendChild(textSpan(tps > 0 ? Math.round(tps) + " t/s" : "-", "usage-cell-note usage-mono"));
    return cell(stack);
  }

  function tokensCell(log, other) {
    const stack = document.createElement("div");
    stack.className = "usage-cell-stack";
    const input = Number(log.prompt_tokens || 0);
    const output = Number(log.completion_tokens || 0);
    stack.appendChild(textSpan(input || output ? formatNumber(input) + " / " + formatNumber(output) : "-", "usage-token-pair usage-mono"));

    const cacheRead = Number(other.cache_tokens || 0);
    const splitWrite = Number(other.cache_creation_tokens_5m || 0) + Number(other.cache_creation_tokens_1h || 0);
    const cacheWrite = splitWrite || Number(other.cache_creation_tokens || 0);
    if (cacheRead > 0 || cacheWrite > 0) {
      const parts = [];
      if (cacheRead > 0) {
        const percentage = formatCachePercent(cacheRead, input);
        parts.push("缓存↓ " + formatNumber(cacheRead) + (percentage ? " (" + percentage + ")" : ""));
      }
      if (cacheWrite > 0) parts.push("↑ " + formatNumber(cacheWrite));
      stack.appendChild(textSpan(parts.join(" · "), "usage-cell-note usage-cache-note"));
    }
    return cell(stack);
  }

  function costCell(log) {
    return cell(textSpan(formatQuota(log.quota, data.quota_per_unit), "usage-cost-badge usage-mono"));
  }

  function timingCell(log, other) {
    const group = document.createElement("div");
    group.className = "usage-timing-group";
    const duration = Number(log.use_time || 0);
    group.appendChild(timingBadge(formatDuration(duration), timingTone(duration), "耗时"));
    const firstTokenSeconds = Number(other.frt || 0) / 1000;
    if (log.is_stream) group.appendChild(timingBadge(firstTokenSeconds > 0 ? formatDuration(firstTokenSeconds) : "-", firstTokenSeconds > 0 ? timingTone(firstTokenSeconds, true) : "neutral", "首字"));
    return cell(group);
  }

  function detailsCell(log, other) {
    const details = formatLogDetails(log, other);
    const span = textSpan(details, "usage-details");
    span.title = details;
    return cell(span, "usage-details-cell");
  }

  function renderPagination() {
    const page = Math.max(1, Number(data.page || navigation.usagePage));
    const pageSize = normalizePageSize(data.page_size, navigation.usagePageSize);
    const total = Math.max(0, Number(data.total || 0));
    const pages = Math.max(1, Math.ceil(total / pageSize));
    $("usagePageSummary").textContent = "共 " + formatNumber(total) + " 条 · 第 " + page + " / " + pages + " 页";
    $("usagePageSize").value = String(pageSize);
    $("usagePageSize").disabled = loading;
    $("usagePageInput").value = String(page);
    $("usagePageInput").max = String(pages);
    $("usagePageInput").disabled = loading || pages <= 1;
    $("usageJumpPage").disabled = loading || pages <= 1;
    $("usageFirstPage").disabled = loading || page <= 1;
    $("usagePreviousPage").disabled = loading || page <= 1;
    $("usageNextPage").disabled = loading || page >= pages;
    $("usageLastPage").disabled = loading || page >= pages;
    renderPageNumbers(page, pages);
  }

  function renderPageNumbers(page, pages) {
    const container = $("usagePageNumbers");
    container.replaceChildren();
    for (const item of paginationItems(page, pages)) {
      if (item === "...") {
        const ellipsis = textSpan("…", "usage-page-ellipsis");
        ellipsis.setAttribute("aria-hidden", "true");
        container.appendChild(ellipsis);
        continue;
      }
      const button = document.createElement("button");
      button.type = "button";
      button.className = "secondary-button usage-page-number";
      button.textContent = String(item);
      button.title = "第 " + item + " 页";
      button.setAttribute("aria-label", "第 " + item + " 页");
      button.disabled = loading;
      if (item === page) {
        button.classList.add("active");
        button.setAttribute("aria-current", "page");
      }
      button.addEventListener("click", () => goToPage(item));
      container.appendChild(button);
    }
  }

  function setPaginationLoading(value) {
    $("usagePaginationControls").querySelectorAll("button, input, select").forEach((control) => {
      control.disabled = value;
    });
  }

  function goToPage(requestedPage) {
    const pages = Math.max(1, Math.ceil(Math.max(0, Number(data.total || 0)) / normalizePageSize(data.page_size, navigation.usagePageSize)));
    const nextPage = Math.min(Math.max(1, Number(requestedPage) || 1), pages);
    $("usagePageInput").value = String(nextPage);
    if (nextPage === navigation.usagePage) return;
    load(nextPage);
  }

  function jumpToPage() {
    const rawPage = Number($("usagePageInput").value);
    if (!Number.isInteger(rawPage)) {
      $("usagePageInput").value = String(navigation.usagePage);
      return;
    }
    goToPage(rawPage);
  }

  function changePageSize() {
    const pageSize = normalizePageSize($("usagePageSize").value, navigation.usagePageSize);
    if (pageSize === navigation.usagePageSize) return;
    navigation.usagePageSize = pageSize;
    navigation.usagePage = 1;
    load(1);
  }

  function setState(message) {
    const state = $("usageLogsState");
    state.textContent = message;
    state.classList.toggle("hidden", !message);
  }

  function mount() {
    $("usagePageSize").addEventListener("change", changePageSize);
    $("usageFirstPage").addEventListener("click", () => goToPage(1));
    $("usagePreviousPage").addEventListener("click", () => goToPage(navigation.usagePage - 1));
    $("usageNextPage").addEventListener("click", () => goToPage(navigation.usagePage + 1));
    $("usageLastPage").addEventListener("click", () => goToPage(Number.MAX_SAFE_INTEGER));
    $("usagePageInput").addEventListener("keydown", (event) => {
      if (event.key !== "Enter") return;
      event.preventDefault();
      jumpToPage();
    });
    $("usageJumpPage").addEventListener("click", jumpToPage);
    renderPagination();
  }

  return { load, mount, render };
}

function normalizePageSize(value, fallback = 20) {
  const pageSize = Number(value);
  return [10, 20, 30, 40, 50, 100].includes(pageSize) ? pageSize : fallback;
}

function paginationItems(currentPage, totalPages) {
  if (totalPages <= 4) return Array.from({ length: totalPages }, (_, index) => index + 1);
  if (currentPage <= 2) return [1, 2, "...", totalPages];
  if (currentPage >= totalPages - 1) return [1, "...", totalPages - 1, totalPages];
  return [1, "...", currentPage, "...", totalPages];
}

function cell(content, className = "") {
  const element = document.createElement("td");
  if (className) element.className = className;
  if (content instanceof Node) element.appendChild(content);
  else element.textContent = String(content ?? "");
  return element;
}

function textSpan(text, className = "") {
  const span = document.createElement("span");
  span.textContent = text;
  if (className) span.className = className;
  return span;
}

function timingBadge(value, tone, label) {
  const badge = textSpan(value, "usage-timing-badge " + tone);
  badge.title = label + "：" + value;
  badge.setAttribute("aria-label", label + "：" + value);
  return badge;
}
