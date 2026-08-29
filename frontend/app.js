/*
 * @Author        : 顾青离
 * @Url           : sucaijun.com
 * @Email         : Ricky@LiHai.La
 * @Project       : CodexRelay
 * @Description   : Codex API 中转热切换桌面工具
 * @File          : 桌面界面启动与模块装配
 * @Read me       : 感谢使用 CodexRelay，源码注释齐全，支持二次开发。
 * @Remind        : 二次开发请保留原版权信息，谢谢。
 */
import { GetState } from "./core/desktop-api.js";
import { registerExternalLinkHandler } from "./external-links.js";
import { createApplicationController } from "./core/application-controller.js";
import { copyText, errorMessage, toast } from "./core/feedback.js";
import { mountGlobalEvents } from "./core/global-events.js";
import { createNavigation } from "./core/navigation.js";
import { mountRuntime } from "./core/runtime.js";
import { createShell } from "./core/shell.js";
import { createDogeAccount } from "./features/doge/account.js";
import { createDogeTokens } from "./features/doge/tokens.js";
import { createProfileActivation } from "./features/profiles/activation.js";
import { createProfileEditor } from "./features/profiles/editor.js";
import { mountProfiles } from "./features/profiles/index.js";
import { createProfileList } from "./features/profiles/list.js";
import { createUpdates } from "./features/updates.js";
import { createUsage } from "./features/usage.js";
import { createClientConfigs } from "./features/settings/client-configs.js";
import { createConnection } from "./features/settings/connection.js";
import { createSettings } from "./features/settings/index.js";
import { createNetwork } from "./features/settings/network.js";
import { createNotifications } from "./features/settings/notifications.js";
import { createPreferences } from "./features/settings/preferences.js";

registerExternalLinkHandler((error) => toast(errorMessage(error), true));

const {
  checkForUpdates,
  mount: mountUpdates,
  renderUpdateStatus,
} = createUpdates();

let connectionFeature;
let dogeAccountFeature;
let dogeTokensFeature;
let profileListFeature;
let shellFeature;

const navigationFeature = createNavigation({
  renderProfiles: () => profileListFeature.renderProfiles(),
  renderShell: () => shellFeature.renderShell(),
});
const {
  applyDefaultViewFilter,
  isCategoryVisible,
  restoreDefaultViewFilter,
  showView,
  visibleCategorySet,
} = navigationFeature;

shellFeature = createShell({
  pendingDogeTokens: () => dogeTokensFeature.pendingDogeTokens(),
  renderDogeQuota: () => dogeAccountFeature.renderDogeQuota(),
  renderUpdateStatus,
  visibleCategorySet,
});
const { renderShell } = shellFeature;

const applicationController = createApplicationController({
  applyDefaultViewFilter,
  checkForUpdates,
  fetchState: GetState,
  onError: (error) => toast(errorMessage(error), true),
  renderers: [
    () => renderShell(),
    () => profileListFeature.renderProfiles(),
    () => preferencesFeature.renderPreferences(),
    () => networkFeature.renderNetwork(),
    () => notificationsFeature.renderTaskNotification(),
    () => connectionFeature.renderConnection(),
    () => profileActivationFeature.renderDataDirectory(),
    () => clientConfigsFeature.renderClientConfigs(),
    () => usageFeature.renderRequests(),
    () => dogeAccountFeature.renderAnnouncements(),
    () => dogeAccountFeature.renderOnboarding(),
    () => dogeAccountFeature.renderDogeSyncToast(),
  ],
});
const { loadState } = applicationController;

const profileActivationFeature = createProfileActivation({
  loadState,
  categoryLabel: (category) => profileListFeature.categoryLabel(category),
});
const {
  beginActivation,
  closeClientSetupModal,
  mount: mountProfileActivation,
} = profileActivationFeature;

const profileEditorFeature = createProfileEditor({
  loadState,
  beginActivation,
  nonHomeProfileName: (profile) => profileListFeature.nonHomeProfileName(profile),
  showView,
});
const {
  activateProfile,
  addEditorModel,
  deleteProfile,
  fetchEditorModels,
  leaveEditor,
  openEditor,
  saveProfile,
  testProfile,
  updatePreview,
} = profileEditorFeature;

profileListFeature = createProfileList({
  loadState,
  renderShell,
  renderDogeToken: (...args) => dogeTokensFeature.renderDogeToken(...args),
  isDogeSyncing: (...args) => dogeAccountFeature.isDogeSyncing(...args),
  isCategoryVisible,
  activateProfile,
  testProfile,
  openEditor,
  deleteProfile,
});
const {
  buildProfileActions,
  categoryLabel,
  createDragHandle,
  createProfileInfo,
  formatDogeRatio,
  installSortableDrag,
  nonHomeDogeTokenName,
  nonHomeProfileName,
  persistDogeTokenOrder,
  persistFailoverOrder,
  setFilter,
  setProfileAutoSwitch,
} = profileListFeature;

dogeTokensFeature = createDogeTokens({
  loadState,
  beginActivation,
  categoryLabel,
  createDragHandle,
  createProfileInfo,
  formatDogeRatio,
  buildProfileActions,
  installSortableDrag,
  persistFailoverOrder,
  persistDogeTokenOrder,
  setProfileAutoSwitch,
  activateProfile,
  openEditor,
  deleteProfile,
  testProfile,
  nonHomeDogeTokenName,
});
const {
  closeDogeCategoryDialog,
  openDogeCategoryDialog,
  saveDogeCategoryAssignments,
} = dogeTokensFeature;

dogeAccountFeature = createDogeAccount({
  loadState,
  renderShell,
  renderProfiles: () => profileListFeature.renderProfiles(),
  renderConnection: (...args) => connectionFeature.renderConnection(...args),
  openDogeCategoryDialog,
  closeDogeCategoryDialog,
  saveDogeCategoryAssignments,
});
const {
  closeDogeTopupModal,
  formatDogeUSD,
  isDogeSyncing,
  mount: mountDogeAccount,
  setAnnouncementPanel,
  setDogeQuotaPopover,
} = dogeAccountFeature;

connectionFeature = createConnection({ isDogeSyncing, formatDogeUSD });
const clientConfigsFeature = createClientConfigs({ loadState });
const preferencesFeature = createPreferences({ loadState, visibleCategorySet, categoryLabel });
const networkFeature = createNetwork({ loadState });
const notificationsFeature = createNotifications({ loadState });
const { mount: mountSettings } = createSettings({ showView });
const usageFeature = createUsage({ loadState, nonHomeProfileName });

mountGlobalEvents({ copyText });
mountDogeAccount();
mountUpdates();
usageFeature.mount();
mountProfileActivation();
preferencesFeature.mount();
networkFeature.mount();
notificationsFeature.mount();
mountSettings();

mountProfiles({
  openEditor,
  setFilter,
  leaveEditor,
  saveProfile,
  fetchEditorModels,
  addEditorModel,
  updatePreview,
  copyText,
  activateProfile,
  testProfile,
  deleteProfile,
});

mountRuntime({
  loadState,
  restoreDefaultViewFilter,
  renderUpdateStatus,
  setDogeQuotaPopover,
  setAnnouncementPanel,
  closeDogeTopupModal,
  closeDogeCategoryDialog,
  closeClientSetupModal,
});
