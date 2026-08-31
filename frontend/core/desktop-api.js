import * as desktop from "../bindings/codexrelay/internal/desktop/desktopservice.js";

function normalizeDesktopError(error) {
  if (error instanceof Error) return error;
  if (typeof error === "string") return new Error(error);
  if (error && typeof error.message === "string") return new Error(error.message);
  return new Error(String(error || "桌面服务调用失败"));
}

export function callDesktop(method, ...args) {
  const binding = desktop[method];
  if (typeof binding !== "function") throw new Error(`未知桌面服务方法：${method}`);
  let request;
  try {
    request = binding(...args);
  } catch (error) {
    return Promise.reject(normalizeDesktopError(error));
  }
  // Wails 返回 CancellablePromise；使用 then/catch 链而不是 async，保留 cancel/cancelOn 能力。
  return request.catch((error) => { throw normalizeDesktopError(error); });
}

export const ActivateProfile = (...args) => callDesktop("ActivateProfile", ...args);
export const BindDoge = (...args) => callDesktop("BindDoge", ...args);
export const CheckClientConfig = (...args) => callDesktop("CheckClientConfig", ...args);
export const CheckForUpdate = (...args) => callDesktop("CheckForUpdate", ...args);
export const ClearUsage = (...args) => callDesktop("ClearUsage", ...args);
export const ConfigureClient = (...args) => callDesktop("ConfigureClient", ...args);
export const ConfigureDetectedClients = (...args) => callDesktop("ConfigureDetectedClients", ...args);
export const CompleteOnboarding = (...args) => callDesktop("CompleteOnboarding", ...args);
export const DeleteProfile = (...args) => callDesktop("DeleteProfile", ...args);
export const DismissDogeNotification = (...args) => callDesktop("DismissDogeNotification", ...args);
export const DismissDogeTokenSwitch = (...args) => callDesktop("DismissDogeTokenSwitch", ...args);
export const EditDogeToken = (...args) => callDesktop("EditDogeToken", ...args);
export const EnableDogeToken = (...args) => callDesktop("EnableDogeToken", ...args);
export const FetchProfileModels = (...args) => callDesktop("FetchProfileModels", ...args);
export const GetState = (...args) => callDesktop("GetState", ...args);
export const InstallUpdate = (...args) => callDesktop("InstallUpdate", ...args);
export const MarkDogeAnnouncementsRead = (...args) => callDesktop("MarkDogeAnnouncementsRead", ...args);
export const OpenDogeProfile = (...args) => callDesktop("OpenDogeProfile", ...args);
export const OpenDogeTopup = (...args) => callDesktop("OpenDogeTopup", ...args);
export const OpenExternalURL = (...args) => callDesktop("OpenExternalURL", ...args);
export const RedeemDoge = (...args) => callDesktop("RedeemDoge", ...args);
export const ReorderDogeTokens = (...args) => callDesktop("ReorderDogeTokens", ...args);
export const ReorderFailoverProfiles = (...args) => callDesktop("ReorderFailoverProfiles", ...args);
export const ReorderProfiles = (...args) => callDesktop("ReorderProfiles", ...args);
export const SaveProfile = (...args) => callDesktop("SaveProfile", ...args);
export const SelectDirectory = (...args) => callDesktop("SelectDirectory", ...args);
export const SetClientConfigPath = (...args) => callDesktop("SetClientConfigPath", ...args);
export const SetClientConfigSkip = (...args) => callDesktop("SetClientConfigSkip", ...args);
export const SetClientAccessHost = (...args) => callDesktop("SetClientAccessHost", ...args);
export const SetDataDirectory = (...args) => callDesktop("SetDataDirectory", ...args);
export const SetDogeAlertSettings = (...args) => callDesktop("SetDogeAlertSettings", ...args);
export const SetDogeBaseURL = (...args) => callDesktop("SetDogeBaseURL", ...args);
export const SetDogeSyncInterval = (...args) => callDesktop("SetDogeSyncInterval", ...args);
export const SetDogeTokenCategories = (...args) => callDesktop("SetDogeTokenCategories", ...args);
export const SetNetwork = (...args) => callDesktop("SetNetwork", ...args);
export const SetPreferences = (...args) => callDesktop("SetPreferences", ...args);
export const SetProfileAutoSwitch = (...args) => callDesktop("SetProfileAutoSwitch", ...args);
export const SetProxyListenAllInterfaces = (...args) => callDesktop("SetProxyListenAllInterfaces", ...args);
export const SetProxyPort = (...args) => callDesktop("SetProxyPort", ...args);
export const SetTaskNotification = (...args) => callDesktop("SetTaskNotification", ...args);
export const SetTokenSwitchSettings = (...args) => callDesktop("SetTokenSwitchSettings", ...args);
export const SwitchDogeToken = (...args) => callDesktop("SwitchDogeToken", ...args);
export const SwitchToken = (...args) => callDesktop("SwitchToken", ...args);
export const SyncDoge = (...args) => callDesktop("SyncDoge", ...args);
export const SyncDogeAnnouncements = (...args) => callDesktop("SyncDogeAnnouncements", ...args);
export const TestProfile = (...args) => callDesktop("TestProfile", ...args);
export const TestTaskNotification = (...args) => callDesktop("TestTaskNotification", ...args);
export const UnbindDoge = (...args) => callDesktop("UnbindDoge", ...args);
