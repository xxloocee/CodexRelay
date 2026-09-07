import { CheckForUpdate, InstallUpdate, RestartUpdate } from "../core/desktop-api.js";
import { $ } from "../core/dom.js";
import { errorMessage, toast } from "../core/feedback.js";
import { showConfirmDialog } from "../core/modal.js";
import { runtimeState, serverState } from "../core/store.js";

export function createUpdates() {
  let restartWatchdog = null;

  function clearRestartWatchdog() {
    if (restartWatchdog !== null) clearTimeout(restartWatchdog);
    restartWatchdog = null;
  }

  function renderUpdateStatus() {
    const section = $("windowsUpdate");
    const supported = Boolean(serverState.snapshot?.updateSupported);
    runtimeState.update.supported = supported;
    section.classList.toggle("hidden", !supported);
    if (!supported) return;

    const title = $("updateTitle");
    const status = $("updateStatus");
    const button = $("updateAction");
    const buttonIcon = button.querySelector(".icon");
    const buttonLabel = button.querySelector("[data-button-label]");
    const progress = $("updateProgress");
    const total = Number(runtimeState.update.total || 0);
    const written = Number(runtimeState.update.written || 0);
    const percent = total > 0 ? Math.max(0, Math.min(100, Math.round(written / total * 100))) : 0;

    status.classList.toggle("error", Boolean(runtimeState.update.error));
    progress.classList.toggle("hidden", !runtimeState.update.installing || total <= 0);
    progress.setAttribute("aria-valuenow", String(percent));
    $("updateProgressBar").style.width = percent + "%";
    button.disabled = runtimeState.update.checking || runtimeState.update.installing || runtimeState.update.restarting;

    if (runtimeState.update.restarting) {
      title.textContent = "更新已校验，正在重启";
      status.textContent = "正在退出当前程序并替换文件";
      button.className = "primary-button compact-button";
      buttonIcon.className = "icon icon-load spin";
      buttonLabel.textContent = "重启中";
      return;
    }
    if (runtimeState.update.installing) {
      title.textContent = runtimeState.update.phase || "正在准备更新";
      status.textContent = total > 0 ? `已下载 ${percent}%` : "请保持程序运行";
      button.className = "primary-button compact-button";
      buttonIcon.className = "icon icon-load spin";
      buttonLabel.textContent = "更新中";
      return;
    }
    if (runtimeState.update.checking) {
      title.textContent = "正在检查新版本";
      status.textContent = "正在连接 GitHub Releases";
      button.className = "secondary-button compact-button";
      buttonIcon.className = "icon icon-load spin";
      buttonLabel.textContent = "检查中";
      return;
    }
    if (runtimeState.update.error) {
      if (runtimeState.update.errorStage === "restart") title.textContent = "无法自动重启";
      else if (runtimeState.update.errorStage === "install") title.textContent = "更新安装失败";
      else title.textContent = "无法检查更新";
      status.textContent = runtimeState.update.error;
    } else if (runtimeState.update.ready) {
      title.textContent = "更新文件已准备完成";
      status.textContent = "可以重新尝试退出并安装";
    } else if (runtimeState.update.available) {
      title.textContent = `发现新版本 v${runtimeState.update.latestVersion}`;
      status.textContent = "下载完成并校验通过后将自动重启";
    } else if (runtimeState.update.checked) {
      title.textContent = "当前已是最新版本";
      status.textContent = `当前版本 v${serverState.snapshot.version}`;
    } else {
      title.textContent = "检查 Windows 新版本";
      status.textContent = "尚未检查";
    }
    const primaryAction = runtimeState.update.available || runtimeState.update.ready;
    button.className = primaryAction ? "primary-button compact-button" : "secondary-button compact-button";
    buttonIcon.className = runtimeState.update.available && !runtimeState.update.ready ? "icon icon-download" : "icon icon-refresh";
    buttonLabel.textContent = runtimeState.update.ready ? "重启安装" : (runtimeState.update.available ? "下载并重启" : "检查更新");
  }

  async function checkForUpdates(manual = true) {
    if (!serverState.snapshot?.updateSupported || runtimeState.update.checking || runtimeState.update.installing || runtimeState.update.restarting || runtimeState.update.ready) return;
    runtimeState.update.checking = true;
    runtimeState.update.error = "";
    runtimeState.update.errorStage = "";
    renderUpdateStatus();
    try {
      const info = await CheckForUpdate();
      runtimeState.update.checked = true;
      runtimeState.update.available = Boolean(info.available);
      runtimeState.update.ready = false;
      runtimeState.update.latestVersion = info.latestVersion || "";
      if (runtimeState.update.available) toast(`发现新版本 v${runtimeState.update.latestVersion}`);
      else if (manual) toast("当前已是最新版本");
    } catch (error) {
      runtimeState.update.checked = true;
      runtimeState.update.available = false;
      runtimeState.update.error = errorMessage(error);
      runtimeState.update.errorStage = "check";
      if (manual) toast(runtimeState.update.error, true);
    } finally {
      runtimeState.update.checking = false;
      renderUpdateStatus();
    }
  }

  async function restartWindowsUpdate() {
    clearRestartWatchdog();
    runtimeState.update.installing = false;
    runtimeState.update.restarting = true;
    runtimeState.update.ready = true;
    runtimeState.update.error = "";
    runtimeState.update.errorStage = "";
    renderUpdateStatus();
    try {
      await RestartUpdate();
      restartWatchdog = setTimeout(() => {
        restartWatchdog = null;
        if (!runtimeState.update.restarting) return;
        runtimeState.update.restarting = false;
        runtimeState.update.errorStage = "restart";
        runtimeState.update.error = "程序未能正常退出。请再次点击重启安装；如仍失败，请关闭程序后重新打开并重新下载更新。";
        renderUpdateStatus();
      }, 25000);
    } catch (error) {
      runtimeState.update.restarting = false;
      runtimeState.update.errorStage = "restart";
      runtimeState.update.error = errorMessage(error);
      toast(runtimeState.update.error, true);
      renderUpdateStatus();
    }
  }

  async function runWindowsUpdate() {
    if (!runtimeState.update.available) {
      await checkForUpdates(true);
      return;
    }
    const confirmed = await showConfirmDialog(`下载并安装 CodexRelay v${runtimeState.update.latestVersion}？程序将在更新后自动重启。`, { title: "安装 Windows 更新" });
    if (!confirmed) return;
    runtimeState.update.installing = true;
    runtimeState.update.restarting = false;
    runtimeState.update.ready = false;
    runtimeState.update.phase = "正在下载更新";
    runtimeState.update.written = 0;
    runtimeState.update.total = 0;
    runtimeState.update.error = "";
    runtimeState.update.errorStage = "";
    renderUpdateStatus();
    try {
      await InstallUpdate();
      runtimeState.update.installing = false;
      runtimeState.update.ready = true;
      await restartWindowsUpdate();
    } catch (error) {
      runtimeState.update.installing = false;
      runtimeState.update.ready = false;
      runtimeState.update.available = false;
      runtimeState.update.error = errorMessage(error);
      runtimeState.update.errorStage = "install";
      toast(runtimeState.update.error, true);
      renderUpdateStatus();
    }
  }

  function mount() {
    $("updateAction").addEventListener("click", () => {
      if (runtimeState.update.ready) restartWindowsUpdate();
      else if (runtimeState.update.available) runWindowsUpdate();
      else checkForUpdates(true);
    });
  }

  return { renderUpdateStatus, checkForUpdates, mount };
}
