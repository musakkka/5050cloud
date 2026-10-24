// SPDX-License-Identifier: AGPL-3.0-only

// Small helpers shared by every kit module: class joining and value formatting.

export const cx = (...a: (string | false | null | undefined | 0)[]) => a.filter(Boolean).join(" ");

/** Seconds or milliseconds since the epoch, normalised to seconds. */
export const secs = (ts: number) => (ts > 1e12 ? ts / 1000 : ts);

export function timeAgo(ts?: number | null, now = Date.now()) {
  if (!ts) return "";
  const s = Math.max(0, now / 1000 - secs(ts));
  if (s < 45) return "just now";
  const units: [number, string][] = [[60, "minute"], [3600, "hour"], [86400, "day"], [2592000, "month"], [31536000, "year"]];
  for (let i = units.length - 1; i >= 0; i--) {
    const [n, name] = units[i];
    if (s >= n * 0.92) {
      const v = Math.max(1, Math.round(s / n));
      return `${v} ${name}${v > 1 ? "s" : ""} ago`;
    }
  }
  return "just now";
}

/** Compact relative time for dense rows: "now", "5m", "2h", "3d", "2mo", "1y". */
export function timeAgoShort(ts?: number | null, now = Date.now()) {
  if (!ts) return "";
  const s = Math.max(0, now / 1000 - secs(ts));
  if (s < 45) return "now";
  if (s < 3600) return `${Math.max(1, Math.round(s / 60))}m`;
  if (s < 86400) return `${Math.round(s / 3600)}h`;
  if (s < 2592000) return `${Math.round(s / 86400)}d`;
  if (s < 31536000) return `${Math.round(s / 2592000)}mo`;
  return `${Math.round(s / 31536000)}y`;
}

export const fmtDateTime = (ts: number) => new Date(secs(ts) * 1000).toLocaleString();

export function fmtBytes(b?: number | null, digits = 1) {
  if (b == null) return "";
  const u = ["B", "KB", "MB", "GB", "TB"];
  let i = 0;
  while (b >= 1000 && i < u.length - 1) { b /= 1000; i++; }
  return `${b.toFixed(i ? digits : 0)} ${u[i]}`;
}

/** "1 service", "3 services". */
export const plural = (n: number, one: string, many = `${one}s`) => `${n} ${n === 1 ? one : many}`;

/** True on macOS and iOS, for showing the Cmd glyph instead of Ctrl. */
export const isMac = () => typeof navigator !== "undefined" && /Mac|iPhone|iPad/.test(navigator.platform || navigator.userAgent);

/** Reads a JSON preference from localStorage, tolerating private windows. */
export function readPref<T>(key: string, fallback: T): T {
  try { const v = localStorage.getItem(key); return v ? (JSON.parse(v) as T) : fallback; } catch { return fallback; }
}
export function writePref(key: string, v: unknown) {
  try { localStorage.setItem(key, JSON.stringify(v)); } catch { /* private window */ }
}
