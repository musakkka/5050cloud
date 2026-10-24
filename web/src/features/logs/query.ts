// SPDX-License-Identifier: AGPL-3.0-only

// Pure helpers for the log views (docs/SPEC.md 13.5): the filter language, time ranges,
// timestamp formatting, histogram buckets and line merging.

import type { LogLine } from "../../api/types.gen";

// ---------------------------------------------------------------------------------------------
// Attributes

/** Flatten nested attributes into dotted keys for the side panel. */
export function flatten(obj: Record<string, unknown> | undefined, prefix = ""): [string, string][] {
  const out: [string, string][] = [];
  if (!obj) return out;
  for (const [k, v] of Object.entries(obj)) {
    const key = prefix ? `${prefix}.${k}` : k;
    if (v && typeof v === "object" && !Array.isArray(v)) out.push(...flatten(v as Record<string, unknown>, key));
    else out.push([key, Array.isArray(v) ? JSON.stringify(v) : String(v)]);
  }
  return out;
}

// ---------------------------------------------------------------------------------------------
// Filter language: @tag:value, free text, "phrases", -negation, AND, OR, ( ).

export type TokenKind = "tag" | "op" | "neg" | "paren" | "text" | "space" | "bad";
export interface Token { kind: TokenKind; raw: string; start: number; end: number; key?: string; value?: string; error?: string }

/** Splits a query into tokens that keep every character, so a highlight layer can mirror the input. */
export function tokenize(q: string): Token[] {
  const out: Token[] = [];
  let i = 0;
  const n = q.length;
  while (i < n) {
    const c = q[i];
    const start = i;
    if (/\s/.test(c)) {
      while (i < n && /\s/.test(q[i])) i++;
      out.push({ kind: "space", raw: q.slice(start, i), start, end: i });
    } else if (c === "(" || c === ")") {
      i++;
      out.push({ kind: "paren", raw: c, start, end: i });
    } else if (c === "-" && i + 1 < n && !/\s/.test(q[i + 1])) {
      i++;
      out.push({ kind: "neg", raw: c, start, end: i });
    } else if (c === "@") {
      i++;
      const ks = i;
      while (i < n && /[A-Za-z0-9_.-]/.test(q[i])) i++;
      const key = q.slice(ks, i);
      if (!key) {
        while (i < n && !/[\s()]/.test(q[i])) i++;
        out.push({ kind: "bad", raw: q.slice(start, i), start, end: i, error: "A tag needs a name, for example @level:error." });
        continue;
      }
      if (q[i] !== ":") {
        out.push({ kind: "tag", raw: q.slice(start, i), start, end: i, key });
        continue;
      }
      i++;
      let value = "";
      if (q[i] === '"') {
        i++;
        while (i < n && q[i] !== '"') { if (q[i] === "\\" && i + 1 < n) i++; value += q[i]; i++; }
        if (q[i] === '"') i++;
      } else {
        const vs = i;
        while (i < n && !/[\s()]/.test(q[i])) i++;
        value = q.slice(vs, i);
      }
      out.push({ kind: "tag", raw: q.slice(start, i), start, end: i, key, value });
    } else if (c === '"') {
      i++;
      let v = "";
      while (i < n && q[i] !== '"') { if (q[i] === "\\" && i + 1 < n) i++; v += q[i]; i++; }
      if (q[i] === '"') i++;
      out.push({ kind: "text", raw: q.slice(start, i), start, end: i, value: v });
    } else {
      while (i < n && !/[\s()]/.test(q[i])) i++;
      const raw = q.slice(start, i);
      if (raw === "AND" || raw === "OR") out.push({ kind: "op", raw, start, end: i });
      else out.push({ kind: "text", raw, start, end: i, value: raw });
    }
  }
  return out;
}

type Node =
  | { t: "and" | "or"; a: Node; b: Node }
  | { t: "not"; a: Node }
  | { t: "tag"; key: string; value: string }
  | { t: "text"; value: string };

export interface Parsed {
  /** Null when the query is empty. */
  node: Node | null;
  error?: string;
  /** Token that caused the error, for the wavy underline. */
  errorToken?: Token;
}

export function parseFilter(q: string): Parsed {
  const toks = tokenize(q).filter((t) => t.kind !== "space");
  if (!toks.length) return { node: null };
  const bad = toks.find((t) => t.kind === "bad");
  if (bad) return { node: null, error: bad.error, errorToken: bad };
  let p = 0;
  let fail: { msg: string; tok?: Token } | null = null;
  const err = (msg: string, tok?: Token) => { if (!fail) fail = { msg, tok }; return null; };
  const isOp = (t: Token | undefined, r: string) => t?.kind === "op" && t.raw === r;

  const atom = (): Node | null => {
    const t = toks[p];
    if (!t) return err("Expected a search term after the operator.", toks[p - 1]);
    if (t.kind === "neg") {
      p++;
      const a = atom();
      return a ? { t: "not", a } : null;
    }
    if (t.kind === "paren" && t.raw === "(") {
      p++;
      const e = expr();
      if (!e) return null;
      if (toks[p]?.kind === "paren" && toks[p].raw === ")") { p++; return e; }
      return err("Missing closing parenthesis.", t);
    }
    if (t.kind === "paren") return err("Unexpected closing parenthesis.", t);
    if (t.kind === "op") return err(`Unexpected ${t.raw}.`, t);
    p++;
    if (t.kind === "tag") {
      if (t.value === undefined) return err(`Add a value after ${t.raw}, for example ${t.raw}:value.`, t);
      return { t: "tag", key: t.key!, value: t.value };
    }
    return { t: "text", value: t.value ?? t.raw };
  };
  const and = (): Node | null => {
    let a = atom();
    while (a && p < toks.length) {
      const t = toks[p];
      if (isOp(t, "OR") || (t.kind === "paren" && t.raw === ")")) break;
      if (isOp(t, "AND")) {
        p++;
        if (p >= toks.length) return err("AND needs a term after it.", t);
      }
      const b = atom();
      if (!b) return null;
      a = { t: "and", a, b };
    }
    return a;
  };
  const expr = (): Node | null => {
    let a = and();
    while (a && isOp(toks[p], "OR")) {
      const t = toks[p];
      p++;
      if (p >= toks.length) return err("OR needs a term after it.", t);
      const b = and();
      if (!b) return null;
      a = { t: "or", a, b };
    }
    return a;
  };
  if (isOp(toks[0], "AND") || isOp(toks[0], "OR")) return { node: null, error: `Unexpected ${toks[0].raw}.`, errorToken: toks[0] };
  const node = expr();
  if (!fail && p < toks.length) err("Unexpected token.", toks[p]);
  if (fail) { const f = fail as { msg: string; tok?: Token }; return { node: null, error: f.msg, errorToken: f.tok }; }
  return { node };
}

export interface MatchCtx { serviceName?: (l: LogLine) => string }

function tagValue(l: LogLine, key: string, ctx: MatchCtx): string[] {
  switch (key) {
    case "level": return [l.level];
    case "service": return [ctx.serviceName?.(l) ?? "", l.serviceId ?? ""];
    case "deployment": return [l.deploymentId ?? ""];
    case "replica": return [l.replica ?? ""];
    case "stream": return [l.stream];
    default: {
      let v: unknown = l.attrs;
      for (const part of key.split(".")) v = v && typeof v === "object" ? (v as Record<string, unknown>)[part] : undefined;
      return v === undefined || v === null ? [] : [typeof v === "object" ? JSON.stringify(v) : String(v)];
    }
  }
}

function evalNode(n: Node, l: LogLine, ctx: MatchCtx): boolean {
  switch (n.t) {
    case "and": return evalNode(n.a, l, ctx) && evalNode(n.b, l, ctx);
    case "or": return evalNode(n.a, l, ctx) || evalNode(n.b, l, ctx);
    case "not": return !evalNode(n.a, l, ctx);
    case "text": return l.msg.toLowerCase().includes(n.value.toLowerCase());
    case "tag": {
      const want = n.value.toLowerCase();
      const attr = !["level", "service", "deployment", "replica", "stream"].includes(n.key);
      const cmp = /^(>=|<=|>|<)(-?\d+(?:\.\d+)?)$/.exec(n.value);
      if (cmp) {
        const lim = Number(cmp[2]);
        return tagValue(l, n.key, ctx).some((v) => { const x = Number(v); return !Number.isNaN(x) && (cmp[1] === ">=" ? x >= lim : cmp[1] === "<=" ? x <= lim : cmp[1] === ">" ? x > lim : x < lim); });
      }
      return tagValue(l, n.key, ctx).some((v) => { const lv = v.toLowerCase(); return lv === want || (attr && want !== "" && lv.includes(want)); });
    }
  }
}

/** Compile a query into a predicate. Invalid queries match nothing. */
export function compileFilter(q: string, ctx: MatchCtx = {}): { match: (l: LogLine) => boolean; parsed: Parsed } {
  const parsed = parseFilter(q);
  if (parsed.error) return { match: () => false, parsed };
  const node = parsed.node;
  return { match: node ? (l) => evalNode(node, l, ctx) : () => true, parsed };
}

/** Quote bare `@tag:value` terms: `@level:error` becomes `@level:"error"`. */
export function quoteValues(q: string): string {
  return tokenize(q).map((t) => (t.kind === "tag" && t.value !== undefined && !t.raw.includes(':"') ? `@${t.key}:"${t.value.replace(/"/g, '\\"')}"` : t.raw)).join("");
}

/** Add `@key:"value"` to a filter query joined with AND; no duplicates. */
export function addFilter(q: string, key: string, value: string): string {
  const term = `@${key}:"${value.replace(/"/g, '\\"')}"`;
  const cur = q.trim();
  const has = tokenize(cur).some((t) => t.kind === "tag" && t.key === key && t.value === value);
  if (has) return cur;
  return cur ? `${cur} AND ${term}` : term;
}

// ---------------------------------------------------------------------------------------------
// Lines

/** The agent's rate-limit notice: "N log lines dropped: more than 500 lines per second". */
export const isDropped = (l: LogLine) => /log lines dropped/i.test(l.msg) && l.stream === "system";

/** Merge new lines into a sorted list, skipping ids already present, keeping at most `max`. */
export function mergeLines(cur: LogLine[], add: LogLine[], max = 5000): LogLine[] {
  if (!add.length) return cur;
  const seen = new Set(cur.map((l) => l.id));
  const fresh = add.filter((l) => !seen.has(l.id));
  if (!fresh.length) return cur;
  const all = cur.concat(fresh);
  if (fresh.some((l, i) => (i === 0 ? cur.length > 0 && l.ts < cur[cur.length - 1].ts : l.ts < fresh[i - 1].ts))) {
    all.sort((a, b) => a.ts - b.ts);
  }
  return all.length > max ? all.slice(all.length - max) : all;
}

export type Level = "debug" | "info" | "warn" | "error";
export const LEVELS: Level[] = ["debug", "info", "warn", "error"];
export const levelOf = (l: LogLine): Level => (l.level === "error" || l.stream === "stderr" ? "error" : l.level === "warn" ? "warn" : l.level === "debug" ? "debug" : "info");

/** Tailwind classes per level: gutter bar / dot, pill, row tint. */
export const LEVEL_BAR: Record<Level, string> = { debug: "bg-gray-200", info: "bg-blue-600", warn: "bg-yellow-600", error: "bg-danger" };
export const LEVEL_PILL: Record<Level, string> = {
  debug: "border-line bg-surface-hover text-fg-2",
  info: "border-blue-200 bg-blue-50 text-blue-700",
  warn: "border-warn-line bg-warn-bg text-warn",
  error: "border-danger-line bg-danger-bg text-danger-soft",
};

/** Bucket line counts for the histogram. Returns [bucketStart, total, errors][] (legacy shape). */
export function histogram(lines: LogLine[], buckets = 60): [number, number, number][] {
  if (!lines.length) return [];
  const lo = lines[0].ts;
  const hi = Math.max(lines[lines.length - 1].ts, lo + 1);
  const w = Math.max(1000, Math.ceil((hi - lo + 1) / buckets));
  const out: [number, number, number][] = [];
  for (let i = 0; i < buckets; i++) out.push([lo + i * w, 0, 0]);
  for (const l of lines) {
    const i = Math.min(buckets - 1, Math.floor((l.ts - lo) / w));
    out[i][1]++;
    if (l.level === "error") out[i][2]++;
  }
  return out;
}

export interface Bucket { t: number; end: number; counts: Record<Level, number>; total: number }

/** Fixed-width buckets over [from, to]. */
export function bucketize(lines: LogLine[], from: number, to: number, count: number): Bucket[] {
  const w = Math.max(1, (to - from) / count);
  const out: Bucket[] = Array.from({ length: count }, (_, i) => ({ t: from + i * w, end: from + (i + 1) * w, counts: { debug: 0, info: 0, warn: 0, error: 0 }, total: 0 }));
  for (const l of lines) {
    if (l.ts < from || l.ts > to) continue;
    const b = out[Math.min(count - 1, Math.floor((l.ts - from) / w))];
    b.counts[levelOf(l)]++;
    b.total++;
  }
  return out;
}

// ---------------------------------------------------------------------------------------------
// Time

const p2 = (n: number) => String(n).padStart(2, "0");
export const fmtDate = (ts: number) => { const d = new Date(ts); return `${d.getFullYear()}-${p2(d.getMonth() + 1)}-${p2(d.getDate())}`; };
export const fmtClock = (ts: number, sec = true) => { const d = new Date(ts); return `${p2(d.getHours())}:${p2(d.getMinutes())}${sec ? `:${p2(d.getSeconds())}` : ""}`; };
/** `YYYY-MM-DD HH:mm:ss` in the viewer's zone. */
export const fmtStamp = (ts: number, sec = true) => `${fmtDate(ts)} ${fmtClock(ts, sec)}`;
export const fmtStampMs = (ts: number) => `${fmtStamp(ts)}.${String(new Date(ts).getMilliseconds()).padStart(3, "0")} ${tzLabel()}`;
/** `GMT+3`, `GMT+5:30`, `GMT`. */
export function tzLabel(): string {
  const off = -new Date().getTimezoneOffset();
  if (!off) return "GMT";
  const a = Math.abs(off);
  return `GMT${off > 0 ? "+" : "-"}${Math.floor(a / 60)}${a % 60 ? `:${p2(a % 60)}` : ""}`;
}

export interface QuickRange { id: string; label: string; ms: number }
export const QUICK_RANGES: QuickRange[] = [
  { id: "5m", label: "5 min", ms: 5 * 60_000 },
  { id: "15m", label: "15 min", ms: 15 * 60_000 },
  { id: "30m", label: "30 min", ms: 30 * 60_000 },
  { id: "1h", label: "1 hour", ms: 3_600_000 },
  { id: "3h", label: "3 hour", ms: 3 * 3_600_000 },
  { id: "6h", label: "6 hour", ms: 6 * 3_600_000 },
  { id: "1d", label: "1 day", ms: 86_400_000 },
  { id: "7d", label: "7 day", ms: 7 * 86_400_000 },
  { id: "30d", label: "30 day", ms: 30 * 86_400_000 },
];

const UNIT: Record<string, number> = { s: 1000, m: 60_000, h: 3_600_000, d: 86_400_000, w: 7 * 86_400_000 };

/** Parses `7d ago`, `1h`, `now`, or a date. Empty input is `fallback`; garbage is null. */
export function parseTimeInput(s: string, now: number, fallback: number | null = null): number | null {
  const v = s.trim().toLowerCase();
  if (!v) return fallback;
  if (v === "now") return now;
  const m = /^(\d+(?:\.\d+)?)\s*(s|m|h|d|w)\s*(ago)?$/.exec(v);
  if (m) return now - Number(m[1]) * UNIT[m[2]];
  const t = Date.parse(s.trim());
  return Number.isNaN(t) ? null : t;
}

export interface Range {
  /** Quick range id, or "custom". */
  id: string;
  /** Free text for custom ranges. */
  from?: string;
  until?: string;
}

export function rangeBounds(r: Range, now: number): { from: number; to: number } | null {
  if (r.id !== "custom") {
    const q = QUICK_RANGES.find((x) => x.id === r.id) ?? QUICK_RANGES[7];
    return { from: now - q.ms, to: now };
  }
  const from = parseTimeInput(r.from ?? "", now, now - 7 * 86_400_000);
  const to = parseTimeInput(r.until ?? "", now, now);
  if (from === null || to === null || from >= to) return null;
  return { from, to };
}

export function rangeLabel(r: Range): string {
  const q = QUICK_RANGES.find((x) => x.id === r.id);
  if (!q) return r.id === "custom" ? "Custom range" : "Last week";
  const names: Record<string, string> = { "5m": "Last 5 minutes", "15m": "Last 15 minutes", "30m": "Last 30 minutes", "1h": "Last hour", "3h": "Last 3 hours", "6h": "Last 6 hours", "1d": "Last day", "7d": "Last week", "30d": "Last 30 days" };
  return names[q.id];
}
