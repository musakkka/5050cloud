// SPDX-License-Identifier: AGPL-3.0-only

// Pure helpers for the log views (docs/SPEC.md 13.5).

import type { LogLine } from "../../api/types.gen";

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

/** Add `@key:value` to a filter query, quoting values with spaces; no duplicates. */
export function addFilter(q: string, key: string, value: string): string {
  const v = /[\s"]/.test(value) ? `"${value.replace(/"/g, '\\"')}"` : value;
  const term = `@${key}:${v}`;
  const parts = q.trim() ? q.trim().split(/\s+(?=(?:[^"]*"[^"]*")*[^"]*$)/) : [];
  if (parts.includes(term)) return q.trim();
  return [...parts, term].join(" ");
}

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

/** Bucket line counts for the histogram. Returns [bucketStart, total, errors][]. */
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

export const LEVEL_COLOR: Record<string, string> = {
  error: "text-red-600",
  warn: "text-yellow-700",
  debug: "text-gray-500",
  info: "text-foreground",
};
