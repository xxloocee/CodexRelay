import { $ } from "../../core/dom.js";

const uiScaleStorageKey = "codexrelay.ui-scale";
const defaultUiScale = 1;
const minUiScale = 0.8;
const maxUiScale = 1.5;
const uiScaleStep = 0.1;

function normalizeUiScale(value) {
  const numeric = Number(value);
  if (!Number.isFinite(numeric)) return defaultUiScale;
  const stepped = Math.round(numeric / uiScaleStep) * uiScaleStep;
  return Math.min(maxUiScale, Math.max(minUiScale, Number(stepped.toFixed(2))));
}

function readUiScale() {
  try {
    const cached = localStorage.getItem(uiScaleStorageKey);
    return cached === null || cached === "" ? defaultUiScale : normalizeUiScale(cached);
  } catch {
    return defaultUiScale;
  }
}

export function createShortcuts() {
  let uiScale = readUiScale();

  function renderUiScale() {
    const value = $("uiScaleValue");
    if (value) value.textContent = `${Math.round(uiScale * 100)}%`;
    const reset = $("resetUiScale");
    if (reset) reset.disabled = uiScale === defaultUiScale;
  }

  function applyUiScale(nextScale, { persist = true } = {}) {
    uiScale = normalizeUiScale(nextScale);
    document.documentElement.style.zoom = String(uiScale);
    renderUiScale();
    if (!persist) return;
    try {
      localStorage.setItem(uiScaleStorageKey, String(uiScale));
    } catch {
      // The current scale still applies when local storage is unavailable.
    }
  }

  function changeUiScale(delta) {
    applyUiScale(uiScale + delta);
  }

  function onKeyDown(event) {
    if (event.isComposing || !event.ctrlKey || event.metaKey || event.altKey) return;
    const isZoomOut = event.key === "-" || event.key === "_" || event.code === "Minus";
    const isZoomIn = event.key === "=" || event.key === "+" || event.code === "Equal";
    if (!isZoomOut && !isZoomIn) return;
    event.preventDefault();
    changeUiScale(isZoomIn ? uiScaleStep : -uiScaleStep);
  }

  function mount() {
    applyUiScale(uiScale, { persist: false });
    $("resetUiScale")?.addEventListener("click", () => applyUiScale(defaultUiScale));
    document.addEventListener("keydown", onKeyDown, true);
  }

  return { mount };
}
