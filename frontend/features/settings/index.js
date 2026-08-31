import { $ } from "../../core/dom.js";
import { navigation } from "../../core/store.js";

const panelNames = ["general", "network", "taskNotification", "connection", "advanced", "activity", "about"];

export function createSettings({ showView, onTabChange = () => {} }) {
  function setSettingsTab(tab) {
    navigation.settingsTab = tab;
    document.querySelectorAll("#settingsTabs button").forEach((button) => {
      button.classList.toggle("active", button.dataset.tab === tab);
    });
    for (const name of panelNames) $(name + "Panel").classList.toggle("hidden", name !== tab);
    onTabChange(tab);
  }

  function openSettings(tab = "general") {
    setSettingsTab(tab);
    showView("settings");
  }

  function mount() {
    $("openSettings").addEventListener("click", () => openSettings());
    $("settingsBack").addEventListener("click", () => showView("profiles"));
    document.querySelectorAll("#settingsTabs button").forEach((button) => button.addEventListener("click", () => setSettingsTab(button.dataset.tab)));
  }

  return { openSettings, mount };
}
