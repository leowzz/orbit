"use strict";

(() => {
  const key = "orbit.web.theme";
  const system = window.matchMedia("(prefers-color-scheme: dark)");
  let selected;
  try {
    const saved = localStorage.getItem(key);
    if (saved === "light" || saved === "dark") selected = saved;
  } catch {}

  function apply() {
    const theme = selected || (system.matches ? "dark" : "light");
    document.documentElement.dataset.theme = theme;
    const label = theme === "dark" ? "切换到浅色模式" : "切换到深色模式";
    document.querySelectorAll("[data-theme-toggle]").forEach((button) => {
      button.title = label;
      button.setAttribute("aria-label", label);
      button.setAttribute("aria-pressed", String(theme === "dark"));
    });
  }

  // Runs in the head so a saved preference applies before the first paint.
  apply();
  system.addEventListener("change", () => {
    if (!selected) apply();
  });
  document.addEventListener("DOMContentLoaded", () => {
    document.querySelectorAll("[data-theme-toggle]").forEach((button) => {
      button.addEventListener("click", () => {
        selected = document.documentElement.dataset.theme === "dark" ? "light" : "dark";
        try { localStorage.setItem(key, selected); } catch {}
        apply();
      });
    });
    apply();
  });
})();
