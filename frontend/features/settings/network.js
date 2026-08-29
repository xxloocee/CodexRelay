import { SetDogeBaseURL, SetNetwork, SetProxyListenAllInterfaces, SetProxyPort } from "../../core/desktop-api.js";
import { $ } from "../../core/dom.js";
import { errorMessage, setButtonLoading, toast } from "../../core/feedback.js";
import { drafts, serverState } from "../../core/store.js";

export function createNetwork({ loadState }) {
  function renderNetworkDraftState() {
    const preserveDraft = drafts.networkModeDirty || drafts.networkProxyDirty || drafts.networkPortDirty || drafts.networkListenDirty;
    if (!preserveDraft) {
      document.querySelectorAll("#networkModes button").forEach((button) => {
        button.classList.toggle("active", button.dataset.mode === serverState.snapshot.network.mode);
      });
      $("manualProxyRow").classList.toggle("hidden", serverState.snapshot.network.mode !== "manual");
      $("manualProxy").value = serverState.snapshot.network.proxyUrl || "";
      $("proxyPort").value = String(serverState.snapshot.proxyPort || 8765);
      $("listenOnAllInterfaces").checked = Boolean(serverState.snapshot.listenOnAllInterfaces);
    }
  }

  function renderNetwork() {
    renderNetworkDraftState();
    const system = serverState.snapshot.systemProxy;
    $("systemProxyState").textContent = system.enabled ? "已检测到 Windows 系统代理" : "使用 Windows 当前路由";
    $("networkNote").textContent = system.note || "";
    $("listenScopeNote").textContent = serverState.snapshot.listenOnAllInterfaces
      ? "当前监听范围：所有网卡。WSL2 请使用 Windows 主机地址和上方端口访问。"
      : "当前监听范围：仅 Windows 本机回环地址。";
  }

  async function setNetworkMode(mode) {
    drafts.networkModeDirty = true;
    drafts.networkDraftRevision += 1;
    const proxyUrl = mode === "manual" ? $("manualProxy").value.trim() : "";
    if (mode === "manual" && !proxyUrl) {
      document.querySelectorAll("#networkModes button").forEach((button) => button.classList.toggle("active", button.dataset.mode === mode));
      $("manualProxyRow").classList.remove("hidden");
      $("manualProxy").focus();
      return;
    }
    document.querySelectorAll("#networkModes button").forEach((button) => button.classList.toggle("active", button.dataset.mode === mode));
    $("manualProxyRow").classList.toggle("hidden", mode !== "manual");
    await saveNetwork(mode, proxyUrl);
  }

  async function saveNetwork(mode = "manual", proxyUrl = $("manualProxy").value.trim()) {
    const draftRevision = drafts.networkDraftRevision;
    const button = $("saveNetwork");
    setButtonLoading(button, true, "保存中...");
    try {
      await SetNetwork({ mode, proxyUrl });
      if (draftRevision === drafts.networkDraftRevision) {
        drafts.networkModeDirty = false;
        drafts.networkProxyDirty = false;
      }
      await loadState();
      toast("网络出口设置已保存");
    } catch (error) {
      toast(errorMessage(error), true);
    } finally {
      setButtonLoading(button, false);
    }
  }

  async function saveProxyPort() {
    const input = $("proxyPort");
    const button = $("saveProxyPort");
    const port = Number(input.value);
    if (!Number.isInteger(port) || port < 1 || port > 65535) {
      toast("监听端口必须是 1 到 65535 之间的整数", true);
      input.focus();
      return;
    }
    const draftRevision = drafts.networkDraftRevision;
    setButtonLoading(button, true, "保存中...");
    try {
      await SetProxyPort(port);
      if (draftRevision === drafts.networkDraftRevision) drafts.networkPortDirty = false;
      await loadState();
      toast("监听端口已更新");
    } catch (error) {
      toast(errorMessage(error), true);
    } finally {
      setButtonLoading(button, false);
    }
  }

  async function saveDogeBaseURL() {
    const input = $("dogeBaseURL");
    const baseURL = input.value.trim();
    if (!baseURL) {
      toast("二狗子 API 地址不能为空", true);
      input.focus();
      return;
    }
    const button = $("saveDogeBaseURL");
    setButtonLoading(button, true, "保存中...");
    try {
      await SetDogeBaseURL(baseURL);
      drafts.dogeBaseURLDirty = false;
      await loadState();
      toast("二狗子 API 地址已更新；已绑定账户请手动刷新一次");
    } catch (error) {
      toast(errorMessage(error), true);
    } finally {
      setButtonLoading(button, false);
    }
  }

  async function setProxyListenAllInterfaces(enabled) {
    const input = $("listenOnAllInterfaces");
    const draftRevision = drafts.networkListenDraftRevision + 1;
    drafts.networkListenDraftRevision = draftRevision;
    drafts.networkListenDirty = true;
    input.disabled = true;
    try {
      await SetProxyListenAllInterfaces(enabled);
      if (draftRevision === drafts.networkListenDraftRevision) drafts.networkListenDirty = false;
      await loadState();
      toast(enabled ? "已允许 WSL2 访问" : "已恢复仅本机访问");
    } catch (error) {
      if (draftRevision === drafts.networkListenDraftRevision) {
        drafts.networkListenDirty = false;
        input.checked = Boolean(serverState.snapshot?.listenOnAllInterfaces);
      }
      await loadState();
      toast(errorMessage(error), true);
    } finally {
      input.disabled = false;
    }
  }

  function mount() {
    document.querySelectorAll("#networkModes button").forEach((button) => button.addEventListener("click", () => setNetworkMode(button.dataset.mode)));
    $("manualProxy").addEventListener("input", () => {
      drafts.networkProxyDirty = true;
      drafts.networkDraftRevision += 1;
    });
    $("proxyPort").addEventListener("input", () => {
      drafts.networkPortDirty = true;
      drafts.networkDraftRevision += 1;
    });
    $("listenOnAllInterfaces").addEventListener("change", () => setProxyListenAllInterfaces($("listenOnAllInterfaces").checked));
    $("saveNetwork").addEventListener("click", () => saveNetwork());
    $("saveProxyPort").addEventListener("click", saveProxyPort);
    $("dogeBaseURL").addEventListener("input", () => { drafts.dogeBaseURLDirty = true; });
    $("saveDogeBaseURL").addEventListener("click", saveDogeBaseURL);
  }

  return { renderNetwork, mount };
}
