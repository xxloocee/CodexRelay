import { CreateDogeToken } from "../../core/desktop-api.js";
import { $ } from "../../core/dom.js";
import { errorMessage, setButtonLoading, toast } from "../../core/feedback.js";
import { syncModalBody } from "../../core/modal.js";
import { serverState } from "../../core/store.js";

export function createDogeTokenCreator({ loadState, openDogeCategoryDialog }) {
  let creating = false;

  function renderGroups() {
    const doge = serverState.snapshot?.doge || {};
    const select = $("dogeTokenGroup");
    const previous = select.value;
    select.replaceChildren();
    for (const group of doge.groups || []) {
      const displayName = String(doge.groupDisplayNames?.[group] || group).trim();
      select.appendChild(new Option(displayName || group, group));
    }
    if (previous && Array.from(select.options).some((option) => option.value === previous)) select.value = previous;
    select.disabled = select.options.length === 0;
  }

  function openDogeTokenModal() {
    const doge = serverState.snapshot?.doge || {};
    if (!doge.bound) {
      toast("请先在设置中绑定二狗子 API", true);
      return;
    }
    renderGroups();
    const error = $("dogeTokenCreateError");
    error.textContent = $("dogeTokenGroup").options.length ? "" : "当前账户没有可用分组，请先刷新目录";
    error.classList.toggle("hidden", !error.textContent);
    $("submitDogeTokenCreate").disabled = creating || $("dogeTokenGroup").options.length === 0;
    $("dogeTokenCreateModal").classList.remove("hidden");
    syncModalBody();
    setTimeout(() => $("dogeTokenName").focus(), 0);
  }

  function closeDogeTokenModal() {
    $("dogeTokenCreateModal").classList.add("hidden");
    syncModalBody();
  }

  async function submitDogeTokenCreate() {
    if (creating) return;
    const nameInput = $("dogeTokenName");
    const name = nameInput.value.trim();
    const group = $("dogeTokenGroup").value;
    const errorNode = $("dogeTokenCreateError");
    if (!name) {
      errorNode.textContent = "请输入 API 密钥名称";
      errorNode.classList.remove("hidden");
      nameInput.focus();
      return;
    }
    if (new TextEncoder().encode(name).length > 50) {
      errorNode.textContent = "API 密钥名称不能超过 50 字节（中文通常占 3 字节）";
      errorNode.classList.remove("hidden");
      nameInput.focus();
      return;
    }
    if (!group) {
      errorNode.textContent = "请选择 API 密钥分组";
      errorNode.classList.remove("hidden");
      $("dogeTokenGroup").focus();
      return;
    }

    const button = $("submitDogeTokenCreate");
    creating = true;
    setButtonLoading(button, true, "创建中...");
    try {
      await CreateDogeToken({ name, group });
      nameInput.value = "";
      closeDogeTokenModal();
      await loadState();
      toast("API 密钥已创建，请选择本地存放类别");
      openDogeCategoryDialog(true);
    } catch (error) {
      errorNode.textContent = errorMessage(error);
      errorNode.classList.remove("hidden");
    } finally {
      creating = false;
      setButtonLoading(button, false);
    }
  }

  function mount() {
    $("openDogeTokenCreate").addEventListener("click", openDogeTokenModal);
    $("closeDogeTokenCreateModal").addEventListener("click", closeDogeTokenModal);
    $("submitDogeTokenCreate").addEventListener("click", submitDogeTokenCreate);
    $("dogeTokenName").addEventListener("keydown", (event) => {
      if (event.key !== "Enter") return;
      event.preventDefault();
      submitDogeTokenCreate();
    });
  }

  return { closeDogeTokenModal, mount };
}
