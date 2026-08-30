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
