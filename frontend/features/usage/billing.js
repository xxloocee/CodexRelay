import { GetDogeBillingAnalysis } from "../../core/desktop-api.js";
import { $ } from "../../core/dom.js";
import { errorMessage } from "../../core/feedback.js";
import { navigation } from "../../core/store.js";
import {
  buildAnalyticsQuery,
  compactMultiplierLabel,
  formatCompactTokens,
  formatDateTime,
  formatNumber,
  formatPercent,
  formatQuota,
} from "./format.js";

const dimensionLabels = { tokens: "令牌", models: "模型", groups: "分组" };

export function createBillingAnalysis({ onLoading, onCountChange, groupName }) {
  let data = null;
  let requestSequence = 0;

  async function load() {
    const sequence = ++requestSequence;
    setState("");
    setPageLoading(true);
    $("usageBillingView").setAttribute("aria-busy", "true");
    onLoading(true);
    try {
      const result = await GetDogeBillingAnalysis(buildAnalyticsQuery(navigation, 1));
      if (sequence !== requestSequence) return;
      data = result;
      render();
    } catch (error) {
      if (sequence !== requestSequence) return;
      data = null;
      $("billingSummary").replaceChildren();
      $("billingOverview").replaceChildren();
      $("billingDimensionRows").replaceChildren();
      $("billingDimensionEmpty").classList.add("hidden");
      setState(errorMessage(error));
      onCountChange("计费分析加载失败");
    } finally {
      if (sequence === requestSequence) {
        setPageLoading(false);
        $("usageBillingView").setAttribute("aria-busy", "false");
        onLoading(false);
      }
    }
  }

  function render() {
    if (!data) return;
    setState("");
    renderSummary();
    renderOverview();
    renderDimension();
    onCountChange(formatNumber(data.summary?.request_count || 0) + " 条消费日志");
  }

  function renderSummary() {
    const container = $("billingSummary");
    container.replaceChildren();
    const summary = data.summary || {};
    const metrics = summary.token_metrics || {};
    const quotaPerUnit = data.quota_per_unit;

    container.append(
      statCard("总消费额度", formatQuota(summary.total_quota, quotaPerUnit), "blue", [
        ["官方原价总额", formatQuota(summary.original_total_quota, quotaPerUnit)],
      ]),
      statCard("余额消费", formatQuota(summary.wallet_quota, quotaPerUnit), "green", overviewDetails(summary.wallet_multiplier_overview, quotaPerUnit)),
      statCard("订阅抵扣", formatQuota(summary.subscription_quota, quotaPerUnit), "purple", overviewDetails(summary.subscription_multiplier_overview, quotaPerUnit)),
      statCard("日志 Tokens", formatCompactTokens(summary.token_count), "neutral", [
        ["输入", formatCompactTokens(metrics.input_tokens), "占比 " + formatPercent(metrics.input_share)],
        ["输出", formatCompactTokens(metrics.completion_tokens), "占比 " + formatPercent(metrics.completion_share)],
        ["缓存", formatCompactTokens(metrics.cache_tokens), "输入占比 " + formatPercent(Number(metrics.input_tokens) > 0 ? Number(metrics.cache_tokens) / Number(metrics.input_tokens) : 0)],
      ]),
      statCard("消费日志数", formatNumber(summary.request_count), "amber", [
        ["平均输入", formatNumber(Math.round(metrics.avg_input_tokens_per_request || 0))],
        ["平均输出", formatNumber(Math.round(metrics.avg_completion_tokens_per_request || 0))],
        ["平均缓存", formatNumber(Math.round(metrics.avg_cache_tokens_per_request || 0))],
      ]),
      statCard("每 1M Tokens 有效额度", formatQuota(summary.effective_quota_per_1k_tokens, quotaPerUnit), "orange", effectiveDetails(summary.multiplier_overview, quotaPerUnit)),
    );
  }

  function renderOverview() {
    const container = $("billingOverview");
    container.replaceChildren();
    const summary = data.summary || {};
    container.append(
      overviewCard("余额倍率", "余额消费", summary.wallet_quota, summary.wallet_multiplier_overview || []),
      overviewCard("订阅倍率", "订阅抵扣", summary.subscription_quota, summary.subscription_multiplier_overview || []),
      overviewCard("综合倍率", "总消费额度", summary.total_quota, summary.multiplier_overview || []),
    );
  }

  function renderDimension() {
    if (!data) return;
    const dimension = dimensionLabels[navigation.usageDimension] ? navigation.usageDimension : "tokens";
    const rows = [...(data[dimension] || [])].sort((left, right) => Number(right.total_quota || 0) - Number(left.total_quota || 0));
    $("billingDimensionLabel").textContent = dimensionLabels[dimension];
    document.querySelectorAll("#billingDimensions button").forEach((button) => {
      const active = button.dataset.dimension === dimension;
      button.classList.toggle("active", active);
      button.setAttribute("aria-selected", String(active));
    });

    const body = $("billingDimensionRows");
    body.replaceChildren();
    for (const row of rows) {
      const tableRow = document.createElement("tr");
      const rowName = dimension === "groups" ? groupName(row.key || row.name) : (row.name || row.key || "-");
      tableRow.append(
        cell(rowName, "billing-dimension-name"),
        numericCell(formatNumber(row.request_count)),
        numericCell(formatCompactTokens(row.token_count)),
        numericCell(formatQuota(row.wallet_quota, data.quota_per_unit)),
        numericCell(formatQuota(row.subscription_quota, data.quota_per_unit)),
        numericCell(formatQuota(row.total_quota, data.quota_per_unit), "billing-total-value"),
        numericCell(formatQuota(row.effective_quota_per_1k_tokens, data.quota_per_unit)),
        cell(row.last_used_at ? formatDateTime(row.last_used_at) : "-", "usage-mono"),
      );
      body.appendChild(tableRow);
    }
    $("billingDimensionEmpty").classList.toggle("hidden", rows.length > 0);
  }

  function overviewCard(title, totalLabel, totalValue, rows) {
    const article = document.createElement("article");
    article.className = "billing-overview-card";
    const heading = document.createElement("h3");
    heading.textContent = title;
    const total = document.createElement("div");
    total.className = "billing-overview-total";
    total.append(labelValue(totalLabel, formatQuota(totalValue, data.quota_per_unit)));
    const originalTotal = rows.reduce((sum, row) => sum + Number(row.original_quota || 0), 0);
    total.append(labelValue("官方原价总额", formatQuota(originalTotal, data.quota_per_unit)));

    const wrap = document.createElement("div");
    wrap.className = "billing-overview-table-wrap";
    const table = document.createElement("table");
    table.className = "billing-overview-table";
    const head = document.createElement("thead");
    const headRow = document.createElement("tr");
    [title, "额度", "官方原价", "请求数"].forEach((label) => headRow.appendChild(headerCell(label)));
    head.appendChild(headRow);
    const body = document.createElement("tbody");
    if (!rows.length) {
      const empty = document.createElement("tr");
      const emptyCell = cell("暂无数据", "billing-empty-cell");
      emptyCell.colSpan = 4;
      empty.appendChild(emptyCell);
      body.appendChild(empty);
    } else {
      for (const row of rows) {
        const tableRow = document.createElement("tr");
        tableRow.append(
          cell(compactMultiplierLabel(row.label || row.key), "billing-multiplier-label"),
          numericCell(formatQuota(row.quota, data.quota_per_unit)),
          numericCell(formatQuota(row.original_quota, data.quota_per_unit)),
          numericCell(formatNumber(row.request_count)),
        );
        body.appendChild(tableRow);
      }
    }
    table.append(head, body);
    wrap.appendChild(table);
    article.append(heading, total, wrap);
    return article;
  }

  function setState(message) {
    const state = $("usageBillingState");
    state.textContent = message;
    state.classList.toggle("hidden", !message);
  }

  function setPageLoading(loading) {
    const overlay = $("usageBillingLoading");
    overlay.classList.toggle("hidden", !loading);
    overlay.setAttribute("aria-hidden", String(!loading));
  }

  function mount() {
    document.querySelectorAll("#billingDimensions button").forEach((button) => button.addEventListener("click", () => {
      navigation.usageDimension = button.dataset.dimension;
      renderDimension();
    }));
  }

  return { load, mount, render };
}

function statCard(label, value, tone, details) {
  const article = document.createElement("article");
  article.className = "billing-stat-card " + tone;
  article.append(textElement("span", label, "billing-stat-label"), textElement("strong", value, "billing-stat-value"));
  if (details.length) {
    const detailList = document.createElement("div");
    detailList.className = "billing-stat-details";
    for (const [detailLabel, detailValue, extra] of details) {
      const row = document.createElement("div");
      row.append(textElement("span", detailLabel), textElement("strong", detailValue));
      if (extra) row.append(textElement("small", extra));
      detailList.appendChild(row);
    }
    article.appendChild(detailList);
  }
  return article;
}

function overviewDetails(items, quotaPerUnit) {
  return (items || []).slice(0, 3).map((item) => [compactMultiplierLabel(item.label || item.key), formatQuota(item.quota, quotaPerUnit), "原价 " + formatQuota(item.original_quota, quotaPerUnit)]);
}

function effectiveDetails(items, quotaPerUnit) {
  return [...(items || [])]
    .sort((left, right) => Number(right.effective_quota_per_1k_tokens || 0) - Number(left.effective_quota_per_1k_tokens || 0))
    .slice(0, 3)
    .map((item) => [compactMultiplierLabel(item.label || item.key), formatQuota(item.effective_quota_per_1k_tokens, quotaPerUnit), "Tokens " + formatCompactTokens(item.token_count)]);
}

function labelValue(label, value) {
  const row = document.createElement("div");
  row.append(textElement("span", label), textElement("strong", value));
  return row;
}

function headerCell(text) {
  const element = document.createElement("th");
  element.textContent = text;
  return element;
}

function numericCell(text, className = "") {
  return cell(text, "billing-numeric " + className);
}

function cell(text, className = "") {
  const element = document.createElement("td");
  element.textContent = text;
  if (className.trim()) element.className = className.trim();
  element.title = text;
  return element;
}

function textElement(tag, text, className = "") {
  const element = document.createElement(tag);
  element.textContent = text;
  if (className) element.className = className;
  return element;
}
