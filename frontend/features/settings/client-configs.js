import { SelectDirectory, SetClientConfigPath, SetClientConfigSkip } from "../../core/desktop-api.js";
import { $, icon } from "../../core/dom.js";
import { errorMessage, setButtonLoading, toast } from "../../core/feedback.js";
import { drafts, serverState } from "../../core/store.js";

export function createClientConfigs({ loadState }) {
  function renderClientConfigs() {
    const rows = $("clientConfigRows");
    if (!rows) return;
    const focusedInput = document.activeElement?.dataset?.clientCategory || "";
    const focusedValue = focusedInput ? document.activeElement.value : "";
    rows.replaceChildren();
    for (const client of serverState.snapshot?.clientConfigs || []) {
      const row = document.createElement("div");
      row.className = "client-config-row";
      const info = document.createElement("div");
      info.className = "client-config-info";
      const title = document.createElement("strong");
      title.textContent = client.label;
      const status = document.createElement("small");
      status.textContent = client.statusText || "未检测到配置";
      status.className = `client-config-status status-${client.status || "not_detected"}`;
      info.append(title, status);
      const control = document.createElement("div");
      control.className = "client-config-control";
      const input = document.createElement("input");
      input.type = "text";
      input.value = Object.prototype.hasOwnProperty.call(drafts.clientConfigDrafts, client.category)
        ? drafts.clientConfigDrafts[client.category]
        : (client.configDir || "");
      input.placeholder = client.configDir || "未检测到默认目录";
      input.dataset.clientCategory = client.category;
      input.setAttribute("aria-label", `${client.label} 配置目录`);
      input.readOnly = client.status === "unsupported";
      input.addEventListener("input", () => {
        drafts.clientConfigDrafts[client.category] = input.value;
      });
      const choose = document.createElement("button");
      choose.type = "button";
      choose.className = "secondary-button compact-button";
      choose.append(icon("edit"), Object.assign(document.createElement("span"), { textContent: "选择路径" }));
      choose.disabled = client.status === "unsupported";
      choose.addEventListener("click", async () => {
        try {
          const selected = await SelectDirectory(input.value.trim());
          if (!selected) return;
          input.value = selected;
          drafts.clientConfigDrafts[client.category] = selected;
          setButtonLoading(choose, true, "保存中...");
          await SetClientConfigPath(client.category, selected);
          if (drafts.clientConfigDrafts[client.category] === selected) delete drafts.clientConfigDrafts[client.category];
          await loadState();
          toast(`${client.label} 配置目录已保存`);
        } catch (error) {
          toast(errorMessage(error), true);
        } finally {
          setButtonLoading(choose, false);
        }
      });
      const save = document.createElement("button");
      save.type = "button";
      save.className = "secondary-button compact-button";
      save.dataset.clientCategory = client.category;
      save.append(icon("save"), Object.assign(document.createElement("span"), { textContent: "保存" }));
      save.disabled = client.status === "unsupported";
      save.addEventListener("click", async () => {
        const directory = input.value.trim();
        drafts.clientConfigDrafts[client.category] = directory;
        setButtonLoading(save, true, "保存中...");
        try {
          await SetClientConfigPath(client.category, directory);
          if (drafts.clientConfigDrafts[client.category] === directory) delete drafts.clientConfigDrafts[client.category];
          await loadState();
          toast(`${client.label} 配置目录已保存`);
        } catch (error) {
          toast(errorMessage(error), true);
        } finally {
          setButtonLoading(save, false);
        }
      });
      control.append(input, choose, save);
      row.append(info, control);
      const skipLabel = document.createElement("label");
      skipLabel.className = "client-config-skip";
      const skip = document.createElement("input");
      skip.type = "checkbox";
      skip.checked = Object.prototype.hasOwnProperty.call(drafts.clientConfigSkipDrafts, client.category)
        ? drafts.clientConfigSkipDrafts[client.category]
        : Boolean(client.skipConfigReplacement);
      skip.disabled = client.status === "unsupported";
      skip.setAttribute("aria-label", `${client.label} 跳过配置文件替换`);
      skip.addEventListener("change", async () => {
        const value = skip.checked;
        drafts.clientConfigSkipDrafts[client.category] = value;
        try {
          await SetClientConfigSkip(client.category, value);
          if (drafts.clientConfigSkipDrafts[client.category] === value) delete drafts.clientConfigSkipDrafts[client.category];
          await loadState();
          toast(`${client.label} 的跳过配置文件替换设置已保存`);
        } catch (error) {
          delete drafts.clientConfigSkipDrafts[client.category];
          skip.checked = !skip.checked;
          toast(errorMessage(error), true);
        }
      });
      skipLabel.append(skip, Object.assign(document.createElement("span"), { textContent: "跳过配置文件替换" }));
      row.append(skipLabel);
      rows.appendChild(row);
      if (client.category === focusedInput) {
        const restored = row.querySelector("input[data-client-category]");
        if (restored) restored.value = focusedValue;
      }
    }
  }

  return { renderClientConfigs };
}
