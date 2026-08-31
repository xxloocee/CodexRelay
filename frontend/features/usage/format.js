const numberFormatter = new Intl.NumberFormat("zh-CN");

export function parseLogOther(value) {
  if (!value) return {};
  if (typeof value === "object") return value;
  try {
    const parsed = JSON.parse(value);
    return parsed && typeof parsed === "object" ? parsed : {};
  } catch {
    return {};
  }
}

export function formatNumber(value) {
  return numberFormatter.format(Number(value || 0));
}

export function formatCompactTokens(value) {
  const amount = Number(value || 0);
  if (Math.abs(amount) >= 1e9) return trimFixed(amount / 1e9, 2) + "B";
  if (Math.abs(amount) >= 1e6) return trimFixed(amount / 1e6, 2) + "M";
  if (Math.abs(amount) >= 1e3) return trimFixed(amount / 1e3, 2) + "K";
  return formatNumber(amount);
}

export function formatQuota(quota, quotaPerUnit) {
  const divisor = Number(quotaPerUnit) > 0 ? Number(quotaPerUnit) : 500000;
  const amount = Number(quota || 0) / divisor;
  const absolute = Math.abs(amount);
  let digits = 2;
  if (absolute > 0 && absolute < 0.01) digits = 6;
  else if (absolute < 1) digits = 4;
  return "$" + trimFixed(amount, digits);
}

export function formatPrice(value) {
  const amount = Number(value || 0);
  const digits = Math.abs(amount) < 0.01 ? 6 : 4;
  return "$" + trimFixed(amount, digits);
}

export function formatPercent(value) {
  const ratio = Number(value || 0);
  if (!Number.isFinite(ratio)) return "0%";
  const percent = ratio * 100;
  return trimFixed(percent, percent >= 10 ? 1 : 2) + "%";
}

export function formatCachePercent(cacheTokens, inputTokens) {
  const input = Number(inputTokens || 0);
  if (input <= 0) return "";
  return Math.round((Number(cacheTokens || 0) / input) * 100) + "%";
}

export function formatDateTime(timestamp) {
  const date = new Date(Number(timestamp || 0) * 1000);
  if (Number.isNaN(date.getTime())) return "-";
  const parts = [
    date.getFullYear(),
    String(date.getMonth() + 1).padStart(2, "0"),
    String(date.getDate()).padStart(2, "0"),
  ];
  const time = [
    String(date.getHours()).padStart(2, "0"),
    String(date.getMinutes()).padStart(2, "0"),
    String(date.getSeconds()).padStart(2, "0"),
  ].join(":");
  return parts.join("-") + " " + time;
}

export function formatDuration(seconds) {
  const value = Math.max(0, Number(seconds || 0));
  if (value < 1) return Math.round(value * 1000) + "ms";
  if (value < 60) return trimFixed(value, value < 10 ? 1 : 1) + "s";
  const minutes = Math.floor(value / 60);
  return minutes + "m " + Math.round(value % 60) + "s";
}

export function timingTone(seconds, firstToken = false) {
  const value = Number(seconds || 0);
  if (firstToken) {
    if (value >= 10) return "danger";
    if (value >= 5) return "warning";
    return "success";
  }
  if (value >= 60) return "danger";
  if (value >= 10) return "warning";
  return "success";
}

export function rangeTimestamps(range) {
  if (range === "all") return { startTimestamp: 0, endTimestamp: 0 };
  const end = new Date();
  end.setHours(23, 59, 59, 999);
  const start = new Date(end);
  start.setHours(0, 0, 0, 0);
  const days = range === "today" ? 1 : range === "30d" ? 30 : 7;
  start.setDate(start.getDate() - (days - 1));
  return {
    startTimestamp: Math.floor(start.getTime() / 1000),
    endTimestamp: Math.floor(end.getTime() / 1000),
  };
}

export function buildAnalyticsQuery(navigation, page = 1) {
  return {
    ...rangeTimestamps(navigation.usageRange),
    page,
    pageSize: navigation.usagePageSize,
    tokenName: navigation.usageToken,
    modelName: navigation.usageModel,
    group: navigation.usageGroup,
  };
}

export function compactMultiplierLabel(value) {
  const label = String(value || "-");
  const parts = label.split(" / ");
  if (parts[0] !== "阶梯计费") return label;
  const ratioPart = parts.find((part) => part.startsWith("分组倍率 ") || part.startsWith("专属倍率 "));
  const ratio = ratioPart?.replace("分组倍率 ", "").replace("专属倍率 ", "").trim();
  const tier = parts.length > 2 ? parts[1]?.trim() : "";
  if (tier && ratio) return tier + " · " + (ratioPart.startsWith("专属倍率 ") ? "专属 " : "") + ratio;
  return ratio ? "阶梯计费 · " + ratio : label;
}

export function formatLogDetails(log, other) {
  if (other.billing_mode === "tiered_expr") {
    const tier = other.matched_tier || "阶梯计费";
    const ratio = effectiveGroupRatio(other);
    return ratio == null ? tier : tier + " · " + trimFixed(ratio, 4) + "x";
  }
  if (Number(other.model_price) > 0) return "按次 · " + formatPrice(other.model_price);
  if (other.model_ratio != null && Number.isFinite(Number(other.model_ratio))) {
    const inputPrice = Number(other.model_ratio) * 2;
    const outputPrice = inputPrice * Number(other.completion_ratio || 1);
    return "standard · " + formatPrice(inputPrice) + " / " + formatPrice(outputPrice) + "/M";
  }
  const ratio = effectiveGroupRatio(other);
  if (ratio != null) return "分组倍率 · " + trimFixed(ratio, 4) + "x";
  return String(log.content || "-");
}

export function effectiveGroupRatio(other) {
  if (other.user_group_ratio != null) {
    const exclusive = Number(other.user_group_ratio);
    if (Number.isFinite(exclusive) && exclusive !== -1) return exclusive;
  }
  if (other.group_ratio == null) return null;
  const group = Number(other.group_ratio);
  return Number.isFinite(group) ? group : null;
}

function trimFixed(value, digits) {
  const number = Number(value || 0);
  if (!Number.isFinite(number)) return "0";
  return number.toFixed(digits).replace(/\.?0+$/, "");
}
