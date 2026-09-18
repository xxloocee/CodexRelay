import { PreviewCodexHistoryRepair, ApplyCodexHistoryRepair } from "../../core/codex-maintenance-api.js";
import { errorMessage, toast } from "../../core/feedback.js";
import { showConfirmDialog } from "../../core/modal.js";

let maintenanceElement = null;

export function appendCodexMaintenance(row) {
  if (maintenanceElement) { row.append(maintenanceElement); return; }
  const box = document.createElement("div");
  maintenanceElement = box;
  box.className = "codex-maintenance";
  const help = document.createElement("p");
  help.className = "muted";
  help.textContent = "找不到以前的会话？修复会忽略原供应商，将本机历史会话统一归入当前配置，补全可恢复的列表记录，并自动备份。请先完全退出 Codex。";
  const button = document.createElement("button");
  button.type = "button";
  button.className = "secondary-button compact-button";
  button.textContent = "修复历史会话";
  const details = document.createElement("p");
  details.className = "muted";
  details.hidden = true;
  details.setAttribute("aria-live", "polite");
  button.addEventListener("click", async () => {
    if (button.disabled) return;
    button.disabled = true;
    button.textContent = "正在查找历史会话…";
    details.hidden = true;
    try {
      const preview = await PreviewCodexHistoryRepair("all");
      if (!preview.fileCount && !preview.threadCount) {
        details.textContent = ["没有需要调整会话归属的历史记录。", preview.warning].filter(Boolean).join(" ");
        details.hidden = false;
        return;
      }
      const accepted = await showConfirmDialog(
        `已找到需要修复的历史记录，将统一归入当前账号或 API 配置，操作前自动备份。请确认已完全退出 Codex。${preview.warning ? ` ${preview.warning}` : ""}`,
        { title: "修复历史会话", confirmLabel: "开始修复" },
      );
      if (!accepted) return;
      button.textContent = "正在备份并修复…";
      const result = await ApplyCodexHistoryRepair("all", preview.token);
      details.textContent = [result.message, result.warning].filter(Boolean).join(" ");
      details.hidden = false;
      toast(result.message);
    } catch (error) {
      details.textContent = errorMessage(error);
      details.hidden = false;
      toast(details.textContent, true);
    } finally {
      button.disabled = false;
      button.textContent = "修复历史会话";
    }
  });
  box.append(help, button, details);
  row.append(box);
}
