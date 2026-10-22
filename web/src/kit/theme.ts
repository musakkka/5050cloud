// SPDX-License-Identifier: AGPL-3.0-only

// Theme: "system" (default) follows the OS; the avatar menu toggles light and dark.
export type ThemePref = "system" | "light" | "dark";
const KEY = "5050.theme";

export function themePref(): ThemePref {
  try { const v = localStorage.getItem(KEY); return v === "light" || v === "dark" ? v : "system"; } catch { return "system"; }
}

export function resolvedTheme(pref = themePref()): "light" | "dark" {
  if (pref !== "system") return pref;
  return typeof matchMedia !== "undefined" && matchMedia("(prefers-color-scheme: light)").matches ? "light" : "dark";
}

export function applyTheme(pref = themePref()) {
  const t = resolvedTheme(pref);
  const el = document.documentElement;
  el.classList.toggle("light", t === "light");
  el.classList.toggle("dark", t === "dark");
  el.style.colorScheme = t;
}

export function setTheme(pref: ThemePref) {
  try { if (pref === "system") localStorage.removeItem(KEY); else localStorage.setItem(KEY, pref); } catch { /* private window */ }
  applyTheme(pref);
}

export const toggleTheme = () => setTheme(resolvedTheme() === "dark" ? "light" : "dark");

/** Follows OS changes while the preference is "system". */
export function watchSystemTheme() {
  if (typeof matchMedia === "undefined") return () => {};
  const m = matchMedia("(prefers-color-scheme: light)");
  const f = () => { if (themePref() === "system") applyTheme("system"); };
  m.addEventListener("change", f);
  return () => m.removeEventListener("change", f);
}
