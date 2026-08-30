const appearanceStorageKey = "codexrelay.appearance";

export const themeOptions = Object.freeze([
  "classic-blue",
  "energy-orange",
  "future-pink",
  "tech-purple",
  "deep-blue",
  "light-speed-cyan",
  "nebula-gradient",
  "aurora-gradient",
]);
export const colorModeOptions = Object.freeze(["light", "dark"]);

export function normalizeAppearance(preferences = {}) {
  return {
    theme: themeOptions.includes(preferences.theme) ? preferences.theme : "tech-purple",
    colorMode: colorModeOptions.includes(preferences.colorMode) ? preferences.colorMode : "light",
  };
}

export function applyAppearance(preferences) {
  const appearance = normalizeAppearance(preferences);
  document.documentElement.dataset.theme = appearance.theme;
  document.documentElement.dataset.colorMode = appearance.colorMode;
  try {
    localStorage.setItem(appearanceStorageKey, JSON.stringify(appearance));
  } catch {
    // The server-side preference remains authoritative when local storage is unavailable.
  }
  return appearance;
}
