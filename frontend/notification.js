/*
 * @Author        : 顾青离
 * @Url           : sucaijun.com
 * @Email         : Ricky@LiHai.La
 * @Project       : CodexRelay
 * @Description   : 右下角提醒窗口状态读取与确认交互
 * @File          : 独立提醒窗口脚本
 * @Read me       : 感谢使用 CodexRelay，源码注释齐全，支持二次开发。
 * @Remind        : 二次开发请保留原版权信息，谢谢。
 */
import { DismissDogeNotification, DismissDogeTokenSwitch, GetState, SwitchToken } from "./api.js";
import { renderAnnouncementMarkdown } from "./announcement-markdown.js";
import { registerExternalLinkHandler } from "./external-links.js";
import * as wails from "/wails/runtime.js";

const search = new URLSearchParams(window.location.search);
const kind = search.get("kind") || "announcement";
const category = search.get("category") || "";
const $ = (id) => document.getElementById(id);
let switchPrompt = null;

registerExternalLinkHandler((error) => {
  const message = error?.message || String(error || "外部链接打开失败");
  const status = $("notificationStatus");
  status.textContent = message;
  status.hidden = false;
});

function setButtonLoading(button, loading, label = "") {
  if (!button) return;
  let iconNode = button.querySelector(".icon");
  const labelNode = button.querySelector("[data-button-label]");
  if (loading) {
    if (button.dataset.loading !== "true") {
      button.dataset.loading = "true";
      if (!iconNode) {
        iconNode = document.createElement("span");
        iconNode.className = "icon icon-load spin";
        iconNode.dataset.loadingCreated = "true";
        button.prepend(iconNode);
      }
      button.dataset.loadingIcon = iconNode.className;
      if (labelNode) button.dataset.loadingLabel = labelNode.textContent;
    }
    button.disabled = true;
    iconNode = button.querySelector(".icon");
    if (iconNode) iconNode.className = "icon icon-load spin";
    if (labelNode && label) labelNode.textContent = label;
    return;
  }
  if (button.dataset.loading !== "true") return;
  if (iconNode?.dataset.loadingCreated === "true") iconNode.remove();
  else if (iconNode) iconNode.className = button.dataset.loadingIcon || "icon icon-load";
  if (labelNode) labelNode.textContent = button.dataset.loadingLabel || labelNode.textContent;
  delete button.dataset.loading;
  delete button.dataset.loadingIcon;
  delete button.dataset.loadingLabel;
  button.disabled = false;
}

function renderTokenSwitch(prompt) {
  switchPrompt = prompt || null;
  const panel = $("tokenSwitchPanel");
  const rows = $("notificationRows");
  const dismiss = $("dismissNotification");
  const actions = $("tokenSwitchActions");
  const title = $("notificationTitle");
  const auto = prompt?.mode === "auto" || Boolean(prompt?.switchedToName);
  const label = $("tokenSwitchLabel");
  const select = $("tokenSwitchCandidates");
  const status = $("tokenSwitchStatus");
  const message = $("tokenSwitchMessage");
  const stopPanel = $("tokenStopPanel");
  const stopMessage = $("tokenStopMessage");
  const historyPanel = $("tokenSwitchHistory");
  const historyRows = $("tokenSwitchHistoryRows");
  setButtonLoading($("cancelTokenSwitch"), false);
  setButtonLoading($("confirmTokenSwitch"), false);
  setButtonLoading(dismiss, false);
  $("cancelTokenSwitch").disabled = false;
  dismiss.disabled = false;
  panel.hidden = !prompt;
  rows.hidden = true;
  const stopped = auto && Boolean(prompt?.stopped);
  dismiss.hidden = !prompt || (!auto && !stopped);
  actions.hidden = !prompt || auto || stopped;
  title.textContent = stopped ? "自动切换已停止" : (auto ? "已自动切换令牌" : "令牌切换提醒");
  stopPanel.hidden = !stopped;
  stopMessage.textContent = prompt?.stopMessage || "当前类别暂无可用令牌，已停止自动切换。";
  label.hidden = auto || stopped;
  select.hidden = auto || stopped;
  status.hidden = auto || stopped;
  select.replaceChildren();
  message.textContent = prompt?.message || "当前令牌连续异常，建议尝试切换其他令牌。";
  status.textContent = "";
  historyRows.replaceChildren();
  const history = prompt?.switchHistory || [];
  historyPanel.hidden = history.length === 0;
  for (const item of history) {
    const row = document.createElement("article");
    row.className = "token-switch-history-row";
    const route = document.createElement("strong");
    const hasTarget = Boolean(String(item.toName || "").trim());
    route.textContent = hasTarget ? `${item.fromName || "当前令牌"} → ${item.toName}` : (item.fromName || "当前令牌");
    const date = document.createElement("span");
    date.textContent = `${hasTarget ? "切换时间" : "故障时间"}：${item.switchedAt || "未知"}`;
    const error = document.createElement("span");
    error.textContent = `错误信息：${item.failureMessage || "上游请求异常"}`;
    row.append(route, date, error);
    historyRows.appendChild(row);
  }
  const candidates = (prompt?.candidates || []).filter((candidate) => candidate.selectable !== false);
  const placeholder = document.createElement("option");
  placeholder.value = "";
  placeholder.textContent = "请选择";
  placeholder.selected = true;
  select.appendChild(placeholder);
  for (const candidate of candidates) {
    const option = document.createElement("option");
    option.value = String(candidate.profileId || "");
    const name = candidate.name || "候选代理 API";
    option.textContent = name;
    select.appendChild(option);
  }
  select.disabled = auto || stopped || candidates.length === 0;
  $("confirmTokenSwitch").disabled = stopped || !String(select.value || "").trim();
  if (!auto && prompt && candidates.length === 0) {
    status.textContent = "当前类别没有可用的其他令牌";
  }
}

$("tokenSwitchCandidates").addEventListener("change", () => {
  const prompt = switchPrompt;
  const auto = prompt?.mode === "auto" || Boolean(prompt?.switchedToName);
  const stopped = auto && Boolean(prompt?.stopped);
  $("confirmTokenSwitch").disabled = auto || stopped || !String($("tokenSwitchCandidates").value || "").trim();
});

function render(state) {
  const notificationStatus = $("notificationStatus");
  notificationStatus.textContent = "";
  notificationStatus.hidden = true;
  if (kind === "token-switch") {
    const prompts = state?.doge?.tokenSwitches || {};
    renderTokenSwitch((category ? prompts[category] : null) || (!category ? state?.doge?.tokenSwitch : null) || null);
    return;
  }
  switchPrompt = null;
  $("tokenSwitchPanel").hidden = true;
  $("notificationRows").hidden = false;
  $("dismissNotification").hidden = false;
  $("tokenSwitchActions").hidden = true;
  const alerts = state?.doge?.notifications?.alerts || [];
  const filtered = alerts.filter((alert) => alert.kind === kind);
  const labels = { balance: "余额提醒", subscription: "套餐提醒", announcement: "系统公告" };
  $("notificationTitle").textContent = labels[kind] || "CodexRelay 提醒";
  const rows = $("notificationRows");
  rows.replaceChildren();
  for (const alert of filtered) {
    const row = document.createElement("div");
    row.className = `notification-row ${kind}`;
    const title = document.createElement("strong");
    title.textContent = alert.title || labels[kind] || "提醒";
    const announcement = kind === "announcement"
      ? (state?.doge?.notifications?.announcements || []).find((item) => Number(item.id) === Number(alert.announcementId))
      : null;
    const message = document.createElement(kind === "announcement" ? "div" : "span");
    if (announcement) {
      message.className = "notification-markdown";
      message.append(renderAnnouncementMarkdown(announcement.content || alert.message));
    } else {
      message.textContent = alert.message || "请查看账户状态";
    }
    row.append(title, message);
    rows.appendChild(row);
  }
}

$("cancelTokenSwitch").addEventListener("click", async () => {
  if (!switchPrompt?.key) return;
  const button = $("cancelTokenSwitch");
  setButtonLoading(button, true, "取消中...");
  $("confirmTokenSwitch").disabled = true;
  try {
    await DismissDogeTokenSwitch(switchPrompt.key);
  } catch (error) {
    $("tokenSwitchStatus").textContent = error?.message || String(error || "取消失败");
    setButtonLoading(button, false);
    $("confirmTokenSwitch").disabled = false;
  }
});

$("confirmTokenSwitch").addEventListener("click", async () => {
  if (!switchPrompt?.key) return;
  const profileID = String($("tokenSwitchCandidates").value || "").trim();
  if (!profileID) return;
  const button = $("confirmTokenSwitch");
  setButtonLoading(button, true, "切换中...");
  $("cancelTokenSwitch").disabled = true;
  $("tokenSwitchStatus").textContent = "切换中...";
  try {
    await SwitchToken(switchPrompt.key, profileID);
    await loadState();
  } catch (error) {
    $("tokenSwitchStatus").textContent = error?.message || String(error || "切换失败");
    setButtonLoading(button, false);
    $("cancelTokenSwitch").disabled = false;
  }
});

async function loadState() {
  try { render(await GetState()); } catch { /* 主窗口会继续负责显示同步错误。 */ }
}

$("dismissNotification").addEventListener("click", async () => {
  const button = $("dismissNotification");
  setButtonLoading(button, true, "关闭中...");
  try {
    if (kind === "token-switch" && switchPrompt?.key) await DismissDogeTokenSwitch(switchPrompt.key);
    else await DismissDogeNotification(kind);
  } finally { setButtonLoading(button, false); }
});
wails.Events.On("notification-state-changed", loadState);
loadState();
