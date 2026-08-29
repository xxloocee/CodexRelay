import { createRefreshCoordinator } from "./refresh-coordinator.js";
import { runtimeState, serverState, setServerSnapshot, subscribe } from "./store.js";

export function createApplicationController({
  applyDefaultViewFilter,
  checkForUpdates,
  fetchState,
  onError,
  renderers,
}) {
  const renderState = () => {
    applyDefaultViewFilter();
    for (const render of renderers) render();
    if (!runtimeState.updateCheckStarted && serverState.snapshot?.updateSupported) {
      runtimeState.updateCheckStarted = true;
      setTimeout(() => checkForUpdates(false), 0);
    }
  };

  const unsubscribe = subscribe("server", renderState);
  const coordinator = createRefreshCoordinator({
    fetchState,
    applyState: setServerSnapshot,
    onError,
  });

  return {
    dispose: unsubscribe,
    isRefreshing: coordinator.isRefreshing,
    loadState: coordinator.refresh,
  };
}
