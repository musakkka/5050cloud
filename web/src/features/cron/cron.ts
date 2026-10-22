// SPDX-License-Identifier: AGPL-3.0-only

// Cron schedule validation and next run times for Settings > Deploy
// (docs/SPEC.md 13.3): 5-field crontab or @hourly/@daily/@weekly/@monthly.

export const CRON_PRESETS: { label: string; value: string }[] = [
  { label: "Every hour", value: "0 * * * *" },
  { label: "Every day", value: "0 0 * * *" },
  { label: "Every week", value: "0 0 * * 0" },
  { label: "Every month", value: "0 0 1 * *" },
];

const MACROS: Record<string, string> = {
  "@hourly": "0 * * * *",
  "@daily": "0 0 * * *",
  "@midnight": "0 0 * * *",
  "@weekly": "0 0 * * 0",
  "@monthly": "0 0 1 * *",
  "@yearly": "0 0 1 1 *",
  "@annually": "0 0 1 1 *",
};

const MONTHS = ["JAN", "FEB", "MAR", "APR", "MAY", "JUN", "JUL", "AUG", "SEP", "OCT", "NOV", "DEC"];
const DAYS = ["SUN", "MON", "TUE", "WED", "THU", "FRI", "SAT"];

export interface Cron {
  minute: Set<number>;
  hour: Set<number>;
  dom: Set<number>;
  month: Set<number>;
  dow: Set<number>;
  domStar: boolean;
  dowStar: boolean;
}

export type ParseCron = { ok: true; cron: Cron } | { ok: false; error: string };

function field(src: string, name: string, min: number, max: number, names?: string[]): Set<number> | string {
  const out = new Set<number>();
  for (const part of src.split(",")) {
    const [rangeRaw, stepRaw, extra] = part.split("/");
    if (extra !== undefined) return `${name}: "${part}" has more than one /`;
    const step = stepRaw === undefined ? 1 : Number(stepRaw);
    if (!Number.isInteger(step) || step < 1) return `${name}: step "${stepRaw}" must be a positive number`;
    const val = (s: string): number | null => {
      const up = s.toUpperCase();
      if (names) { const i = names.indexOf(up); if (i >= 0) return i + min; }
      if (!/^\d+$/.test(s)) return null;
      return Number(s);
    };
    let lo: number;
    let hi: number;
    if (rangeRaw === "*") { lo = min; hi = max; } else {
      const [a, b, more] = rangeRaw.split("-");
      if (more !== undefined) return `${name}: "${rangeRaw}" is not a range`;
      const av = val(a);
      const bv = b === undefined ? av : val(b);
      if (av === null || bv === null) return `${name}: "${rangeRaw}" is not a number`;
      lo = av; hi = bv;
      if (stepRaw !== undefined && b === undefined) hi = max;
    }
    if (lo < min || hi > max || lo > hi) return `${name}: ${rangeRaw} is outside ${min}-${max}`;
    for (let i = lo; i <= hi; i += step) out.add(i);
  }
  return out;
}

export function parseCron(expr: string): ParseCron {
  const raw = expr.trim();
  if (!raw) return { ok: false, error: "Enter a schedule" };
  const e = MACROS[raw.toLowerCase()] ?? raw;
  if (e.startsWith("@")) return { ok: false, error: `Unknown schedule ${raw}` };
  const parts = e.split(/\s+/);
  if (parts.length !== 5) return { ok: false, error: "A schedule has 5 fields: minute hour day-of-month month day-of-week" };
  const minute = field(parts[0], "Minute", 0, 59);
  const hour = field(parts[1], "Hour", 0, 23);
  const dom = field(parts[2], "Day of month", 1, 31);
  const month = field(parts[3], "Month", 1, 12, MONTHS);
  const dowRaw = field(parts[4], "Day of week", 0, 7, DAYS);
  for (const f of [minute, hour, dom, month, dowRaw]) if (typeof f === "string") return { ok: false, error: f };
  const dow = new Set([...(dowRaw as Set<number>)].map((d) => (d === 7 ? 0 : d)));
  return {
    ok: true,
    cron: {
      minute: minute as Set<number>, hour: hour as Set<number>, dom: dom as Set<number>, month: month as Set<number>, dow,
      domStar: parts[2] === "*", dowStar: parts[4] === "*",
    },
  };
}

/** Offset of `tz` from UTC in ms at instant `t`. */
const fmts = new Map<string, Intl.DateTimeFormat>();
function tzOffset(t: number, tz: string): number {
  if (tz === "UTC") return 0;
  let f = fmts.get(tz);
  if (!f) {
    f = new Intl.DateTimeFormat("en-US", {
      timeZone: tz, hourCycle: "h23", year: "numeric", month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit", second: "2-digit",
    });
    fmts.set(tz, f);
  }
  const p: Record<string, number> = {};
  for (const x of f.formatToParts(new Date(t))) if (x.type !== "literal") p[x.type] = Number(x.value);
  const wall = Date.UTC(p.year, p.month - 1, p.day, p.hour === 24 ? 0 : p.hour, p.minute, p.second);
  return wall - Math.floor(t / 1000) * 1000;
}

function dayMatches(c: Cron, d: Date): boolean {
  const domOk = c.dom.has(d.getUTCDate());
  const dowOk = c.dow.has(d.getUTCDay());
  if (c.domStar && c.dowStar) return true;
  if (c.domStar) return dowOk;
  if (c.dowStar) return domOk;
  return domOk || dowOk;
}

/** The next `n` run times (ms) after `from`, in time zone `tz`. */
export function nextRuns(c: Cron, n: number, from = Date.now(), tz = "UTC"): number[] {
  const out: number[] = [];
  const mins = [...c.minute].sort((a, b) => a - b);
  const hours = [...c.hour].sort((a, b) => a - b);
  // Work on wall-clock time in tz, represented as a UTC Date.
  const wallFrom = from + tzOffset(from, tz);
  const start = new Date(Math.floor(wallFrom / 60000) * 60000 + 60000);
  const day = new Date(Date.UTC(start.getUTCFullYear(), start.getUTCMonth(), start.getUTCDate()));
  for (let i = 0; i < 366 * 5 && out.length < n; i++, day.setUTCDate(day.getUTCDate() + 1)) {
    if (!c.month.has(day.getUTCMonth() + 1) || !dayMatches(c, day)) continue;
    for (const h of hours) {
      for (const m of mins) {
        const wall = day.getTime() + h * 3600000 + m * 60000;
        if (wall < start.getTime()) continue;
        // Convert wall time back to an instant.
        let t = wall - tzOffset(wall, tz);
        t = wall - tzOffset(t, tz);
        if (t > from && (out.length === 0 || t > out[out.length - 1])) out.push(t);
        if (out.length >= n) return out;
      }
    }
  }
  return out;
}

export function validTimeZone(tz: string): boolean {
  try { new Intl.DateTimeFormat("en-US", { timeZone: tz }); return true; } catch { return false; }
}
