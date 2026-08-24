/*
 * @Author        : 顾青离
 * @Url           : sucaijun.com
 * @Email         : Ricky@LiHai.La
 * @Project       : CodexRelay
 * @Description   : Codex API 中转热切换桌面工具
 * @File          : Wails 自动生成绑定的稳定前端入口
 * @Read me       : 感谢使用 CodexRelay，源码注释齐全，支持二次开发。
 * @Remind        : 二次开发请保留原版权信息，谢谢。
 */
export {
  ActivateProfile,
  BindDoge,
  CheckClientConfig,
  CheckForUpdate,
  ClearUsage,
	ConfigureClient,
  CompleteOnboarding,
  DeleteProfile,
  DismissDogeNotification,
  DismissDogeTokenSwitch,
  EditDogeToken,
  EnableDogeToken,
  GetState,
  InstallUpdate,
  OpenDogeProfile,
  OpenDogeTopup,
  OpenExternalURL,
  MarkDogeAnnouncementsRead,
  ReorderDogeTokens,
  ReorderFailoverProfiles,
  ReorderProfiles,
  RedeemDoge,
  SaveProfile,
  SetDogeTokenCategories,
  SetDogeAlertSettings,
  SetClientConfigPath,
  SetClientConfigSkip,
  SetDataDirectory,
  SelectDirectory,
  SetNetwork,
  SetDogeSyncInterval,
  SetProxyPort,
  SetPreferences,
  SetProfileAutoSwitch,
  SetTokenSwitchSettings,
  TestProfile,
  FetchProfileModels,
  SyncDoge,
  SyncDogeAnnouncements,
  SwitchDogeToken,
  SwitchToken,
  UnbindDoge,
} from "./bindings/codexrelay/internal/desktop/desktopservice.js";
