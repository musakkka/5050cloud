// SPDX-License-Identifier: AGPL-3.0-only

// Metric helpers (docs/SPEC.md 13.3 Metrics).

export const RANGES = ["5m", "15m", "30m", "1h", "3h", "6h", "1d", "7d", "30d"] as const;
export type Range = (typeof RANGES)[number];
export const DEFAULT_RANGE: Range = "7d";

export const RANGE_MS: Record<Range, number> = {
  "5m": 300e3, "15m": 900e3, "30m": 1800e3, "1h": 3600e3, "3h": 3 * 3600e3, "6h": 6 * 3600e3,
  "1d": 86400e3, "7d": 7 * 86400e3, "30d": 30 * 86400e3,
};
export const RANGE_CHIP: Record<Range, string> = {
  "5m": "5 min", "15m": "15 min", "30m": "30 min", "1h": "1 hour", "3h": "3 hour", "6h": "6 hour", "1d": "1 day", "7d": "7 day", "30d": "30 day",
};
export const RANGE_LABEL: Record<Range, string> = {
  "5m": "Last 5 minutes", "15m": "Last 15 minutes", "30m": "Last 30 minutes", "1h": "Last hour", "3h": "Last 3 hours", "6h": "Last 6 hours",
  "1d": "Last day", "7d": "Last week", "30d": "Last month",
};

/** Resolve a selection to absolute bounds (Unix ms). Presets end at `now`. */
export function rangeBounds(sel: RangeSel, now = Date.now()): { from: number; until: number } {
  if (sel.from && sel.until) return { from: sel.from, until: sel.until };
  return { from: now - RANGE_MS[sel.range || DEFAULT_RANGE], until: now };
}

const SHORT = (t: number) => new Date(t).toLocaleDateString([], { month: "short", day: "numeric" });
export function rangeLabel(sel: RangeSel): string {
  if (sel.from && sel.until) {
    const tm = (t: number) => new Date(t).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", hour12: false });
    if (sel.until - sel.from <= 36 * 3600e3) return `${SHORT(sel.from)}, ${tm(sel.from)} to ${new Date(sel.from).getDate() === new Date(sel.until).getDate() ? "" : `${SHORT(sel.until)}, `}${tm(sel.until)}`;
    return `${SHORT(sel.from)} to ${SHORT(sel.until)}`;
  }
  return RANGE_LABEL[sel.range || DEFAULT_RANGE];
}

/** The text shown in the From/Until inputs for a selection. */
export function rangeInputs(sel: RangeSel): { from: string; until: string } {
  if (sel.from && sel.until) return { from: fmtLocalInput(sel.from), until: fmtLocalInput(sel.until) };
  const r = sel.range || DEFAULT_RANGE;
  return { from: `${r.slice(0, -1)}${r.slice(-1)} ago`, until: "now" };
}

const pad = (n: number) => String(n).padStart(2, "0");
export function fmtLocalInput(t: number): string {
  const d = new Date(t);
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

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
  return { range: r && (RANGES as readonly string[]).includes(r) ? r : DEFAULT_RANGE };
}

/** Write the range into a URL query, keeping other params. */
export function writeRange(search: string, sel: RangeSel): string {
  const u = new URLSearchParams(search);
  u.delete("mrange"); u.delete("mfrom"); u.delete("muntil");
  if (sel.from && sel.until) { u.set("mfrom", String(Math.round(sel.from))); u.set("muntil", String(Math.round(sel.until))); }
  else if (sel.range && sel.range !== DEFAULT_RANGE) u.set("mrange", sel.range);
  const s = u.toString();
  return s ? `?${s}` : "";
}

/** Parse "7d ago", "2 hours ago", "30 minutes ago", "now" or an absolute date relative to `now`. */
export function parseRelative(text: string, now = Date.now()): number | null {
  const t = text.trim().toLowerCase();
  if (t === "now") return now;
  const m = t.match(/^(\d+(?:\.\d+)?)\s*(m|min|mins|minutes?|h|hrs?|hours?|d|days?|w|weeks?)\s*ago$/);
  if (!m) {
    const abs = Date.parse(text);
    return isNaN(abs) ? null : abs;
  }
  const n = Number(m[1]);
  const u = m[2][0];
  const ms = u === "m" ? 60000 : u === "h" ? 3600000 : u === "d" ? 86400000 : 604800000;
  return now - n * ms;
}

/** Tooltip date line: "2026-10-20 · 03:00 GMT+3". */
export function fmtStamp(ts: number): string {
  const d = new Date(ts);
  const off = -d.getTimezoneOffset();
  const sign = off < 0 ? "-" : "+";
  const h = Math.floor(Math.abs(off) / 60);
  const m = Math.abs(off) % 60;
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} \u00b7 ${pad(d.getHours())}:${pad(d.getMinutes())} GMT${sign}${h}${m ? `:${pad(m)}` : ""}`;
}

/** Round `max` up to a "nice" axis with `n` intervals. Returns the tick values. */
export function niceTicks(max: number, n = 4): number[] {
  const raw = max > 0 ? max / n : 1;
  const pow = Math.pow(10, Math.floor(Math.log10(raw)));
  const f = raw / pow;
  const step = (f <= 1 ? 1 : f <= 2 ? 2 : f <= 2.5 ? 2.5 : f <= 5 ? 5 : 10) * pow;
  const count = Math.max(1, Math.ceil(max / step - 1e-9));
  return Array.from({ length: count + 1 }, (_, i) => +(i * step).toPrecision(12));
}

const BYTE_UNITS = ["B", "KB", "MB", "GB", "TB"];
/** Decimal byte formatting ("193 MB", "33 KB"). `ref` picks the unit so a whole axis shares one. */
export function fmtBytesDec(v: number, ref = v): string {
  if (v === 0) return "0 B";
  let i = 0;
  let r = Math.abs(ref);
  while (r >= 1000 && i < BYTE_UNITS.length - 1) { r /= 1000; i++; }
  const x = v / Math.pow(1000, i);
  const s = Number.isInteger(+x.toFixed(2)) || Math.abs(x) >= 100 ? x.toFixed(0) : Math.abs(x) >= 10 ? x.toFixed(1) : x.toFixed(2);
  return `${+s === 0 ? "0" : s} ${BYTE_UNITS[i]}`;
}

/** Evenly spaced x ticks aligned to local time. */
export function timeTicks(from: number, until: number, maxTicks = 8): { ts: number; label: string }[] {
  const span = until - from;
  const steps = [5, 10, 15, 30, 60, 120, 180, 360, 720, 1440, 2880, 4320, 10080].map((m) => m * 60e3);
  const step = steps.find((s) => span / s <= maxTicks) ?? steps[steps.length - 1];
  const tz = -new Date(from).getTimezoneOffset() * 60e3;
  const out: { ts: number; label: string }[] = [];
  const useDate = span > 36 * 3600e3;
  for (let t = Math.ceil((from + tz) / step) * step - tz; t <= until; t += step) {
    const d = new Date(t);
    out.push({ ts: t, label: useDate ? d.toLocaleDateString([], { month: "short", day: "numeric" }) : `${pad(d.getHours())}:${pad(d.getMinutes())}` });
  }
  return out;
}

/** Average (or sum) points into at most `n` buckets. */
export function bucketPoints(points: number[][], from: number, until: number, n: number, mode: "mean" | "sum" = "mean"): number[][] {
  if (points.length <= n && mode === "mean") return points;
  const w = (until - from) / n;
  const acc = new Map<number, { s: number; c: number }>();
  for (const [ts, v] of points) {
    if (ts < from || ts > until) continue;
    const b = Math.min(n - 1, Math.floor((ts - from) / w));
    const a = acc.get(b) || { s: 0, c: 0 };
    a.s += v; a.c++;
    acc.set(b, a);
  }
  return [...acc.entries()].sort((a, b) => a[0] - b[0]).map(([b, a]) => [from + b * w, mode === "sum" ? a.s : a.s / a.c]);
}

/** Last value at or before `ts` (step-after lookup). */
export function valueAt(points: number[][], ts: number): number | undefined {
  let lo = 0, hi = points.length - 1, ans = -1;
  while (lo <= hi) {
    const mid = (lo + hi) >> 1;
    if (points[mid][0] <= ts) { ans = mid; lo = mid + 1; } else hi = mid - 1;
  }
  return ans < 0 ? undefined : points[ans][1];
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
