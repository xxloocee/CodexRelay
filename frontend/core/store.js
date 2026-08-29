export const categoryOptions = ["codex", "claude", "gemini", "grok", "opencode", "openclaw", "hermes", "image", "other"];

export const serverState = {
  snapshot: null,
};

export const navigation = {
  view: "profiles",
  settingsTab: "general",
  selectedId: null,
  usageRange: "7d",
  usageProfile: "",
  sourceFilter: "",
  categoryFilter: "",
  viewFilterInitialized: false,
  announcementTab: "notice",
};

export const drafts = {
  isNew: false,
  dirty: false,
  editorModels: [],
  editorDefaultModel: "",
  preferencesDirty: false,
  preferencesDraftRevision: 0,
  tokenSwitchDirty: false,
  tokenSwitchDraftRevision: 0,
  dogeAlertDirty: false,
  dogeAlertDraftRevision: 0,
  dogeBaseURLDirty: false,
  networkModeDirty: false,
  networkProxyDirty: false,
  networkPortDirty: false,
  networkListenDirty: false,
  networkDraftRevision: 0,
  networkListenDraftRevision: 0,
  clientConfigDrafts: {},
  clientConfigSkipDrafts: {},
  taskNotificationDirty: false,
  taskNotificationDraftRevision: 0,
};

export const runtimeState = {
  draggingSortKey: null,
  toastTimer: null,
  toastCleanupTimer: null,
  dogeSyncPollTimer: null,
  dogeRemoteSyncing: false,
  dogeCategoryDialogSignature: "",
  localDogeSyncing: false,
  localAnnouncementSyncing: false,
  onboardingFocused: false,
  pendingActivation: null,
  clientSetupCategory: "",
  confirmResolver: null,
  updateCheckStarted: false,
  update: {
    supported: false,
    checked: false,
    checking: false,
    installing: false,
    available: false,
    latestVersion: "",
    phase: "",
    written: 0,
    total: 0,
    error: "",
  },
};

const domains = {
  server: serverState,
  navigation,
  drafts,
  runtime: runtimeState,
};

const listeners = Object.fromEntries(Object.keys(domains).map((name) => [name, new Set()]));

function domainFor(name) {
  const domain = domains[name];
  if (!domain) throw new Error(`未知状态域：${name}`);
  return domain;
}

export function subscribe(name, listener) {
  if (typeof listener !== "function") throw new TypeError("状态订阅者必须是函数");
  const domainListeners = listeners[name];
  if (!domainListeners) throw new Error(`未知状态域：${name}`);
  domainListeners.add(listener);
  return () => domainListeners.delete(listener);
}

export function notify(name) {
  const domain = domainFor(name);
  for (const listener of [...listeners[name]]) listener(domain);
}

export function patchDomain(name, values) {
  Object.assign(domainFor(name), values);
  notify(name);
}

export function setServerSnapshot(snapshot, { notifyListeners = true } = {}) {
  serverState.snapshot = snapshot;
  if (notifyListeners) notify("server");
}
