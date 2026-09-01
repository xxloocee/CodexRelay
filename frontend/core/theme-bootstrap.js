(function restoreCachedAppearance() {
  const themes = new Set([
    "classic-blue",
    "energy-orange",
    "future-pink",
    "tech-purple",
    "deep-blue",
    "light-speed-cyan",
    "nebula-gradient",
    "aurora-gradient",
  ]);
  const colorModes = new Set(["light", "dark"]);
  try {
    const cached = JSON.parse(localStorage.getItem("codexrelay.appearance") || "null");
    if (themes.has(cached?.theme)) document.documentElement.dataset.theme = cached.theme;
    if (colorModes.has(cached?.colorMode)) document.documentElement.dataset.colorMode = cached.colorMode;
  } catch {}
}());

(function restoreCachedUiScale() {
  const minUiScale = 0.8;
  const maxUiScale = 1.5;
  try {
    const cached = localStorage.getItem("codexrelay.ui-scale");
    if (cached === null || cached === "") return;
    const value = Number(cached);
    if (!Number.isFinite(value)) return;
    const scale = Math.min(maxUiScale, Math.max(minUiScale, Math.round(value * 10) / 10));
    document.documentElement.style.zoom = String(scale);
  } catch {}
}());
