// 统一串行化状态刷新；并发调用共享下一次刷新 Promise，等待者不会提前读到旧快照。
export function createRefreshCoordinator({ fetchState, applyState, onError }) {
  let active = null;
  let queued = null;

  const start = () => {
    const request = (async () => {
      try {
        const state = await fetchState();
        applyState(state);
      } catch (error) {
        onError(error);
      }
    })();
    active = request.finally(() => {
      active = null;
    });
    return active;
  };

  const refresh = () => {
    if (queued) return queued;
    if (!active) return start();
    queued = active.then(() => {
      queued = null;
      return start();
    });
    return queued;
  };

  return {
    refresh,
    isRefreshing: () => Boolean(active || queued),
  };
}
