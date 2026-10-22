// SPDX-License-Identifier: AGPL-3.0-only

// Metric helpers (docs/SPEC.md 13.3 Metrics).

export const RANGES = ["15m", "1h", "6h", "24h", "7d", "14d", "30d"] as const;
export type Range = (typeof RANGES)[number];

export interface RangeSel { range?: Range; from?: number; until?: number }

/** Join named series of [ts, value] points into rows for a chart. */
export function joinSeries(series: Record<string, number[][] | undefined>, keys: string[], scale: Record<string, number> = {}): Record<string, number>[] {
  const rows = new Map<number, Record<string, number>>();
  for (const k of keys) {
    for (const p of series[k] || []) {
      const [ts, v] = p;
      let r = rows.get(ts);
      if (!r) { r = { ts }; rows.set(ts, r); }
      r[k] = v * (scale[k] ?? 1);
    }
  }
  return [...rows.values()].sort((a, b) => a.ts - b.ts);
}

/** Read the metrics range from a URL query (mrange or mfrom/muntil). */
export function readRange(search: string): RangeSel {
  const u = new URLSearchParams(search);
  const from = Number(u.get("mfrom"));
  const until = Number(u.get("muntil"));
  if (from > 0 && until > from) return { from, until };
  const r = u.get("mrange") as Range | null;
  return { range: r && (RANGES as readonly string[]).includes(r) ? r : "1h" };
}

/** Write the range into a URL query, keeping other params. */
export function writeRange(search: string, sel: RangeSel): string {
  const u = new URLSearchParams(search);
  u.delete("mrange"); u.delete("mfrom"); u.delete("muntil");
  if (sel.from && sel.until) { u.set("mfrom", String(Math.round(sel.from))); u.set("muntil", String(Math.round(sel.until))); }
  else if (sel.range && sel.range !== "1h") u.set("mrange", sel.range);
  const s = u.toString();
  return s ? `?${s}` : "";
}

/** Parse "2 hours ago", "30 minutes ago", "3d ago" or "now" relative to `now`. */
export function parseRelative(text: string, now = Date.now()): number | null {
  const t = text.trim().toLowerCase();
  if (t === "now") return now;
  const m = t.match(/^(\d+(?:\.\d+)?)\s*(m|min|mins|minutes?|h|hrs?|hours?|d|days?|w|weeks?)\s+ago$/);
  if (!m) {
    const abs = Date.parse(text);
    return isNaN(abs) ? null : abs;
  }
  const n = Number(m[1]);
  const u = m[2][0];
  const ms = u === "m" ? 60000 : u === "h" ? 3600000 : u === "d" ? 86400000 : 604800000;
  return now - n * ms;
}

export function fmtValue(key: string, v: number): string {
  if (!isFinite(v)) return "";
  if (key === "mem" || key === "disk") return fmtBytesShort(v);
  if (key === "rx" || key === "tx") return `${fmtBytesShort(v)}/s`;
  if (key === "cpu") return `${v.toFixed(2)} vCPU`;
  if (key === "error_rate") return `${(v * 100).toFixed(1)}%`;
  if (key.startsWith("p")) return `${Math.round(v)} ms`;
  if (key.startsWith("req_")) return `${v.toFixed(2)}/s`;
  return v.toFixed(2);
}

export function fmtBytesShort(b: number): string {
  const u = ["B", "KB", "MB", "GB", "TB"];
  let i = 0;
  while (b >= 1024 && i < u.length - 1) { b /= 1024; i++; }
  return `${b.toFixed(i ? 1 : 0)} ${u[i]}`;
}
