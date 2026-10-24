// SPDX-License-Identifier: AGPL-3.0-only

// The command palette's data model (SPEC 13.4), kept free of React so the
// ranking, sub-menus and adaptive labels have unit tests.
import type { ReactNode } from "react";
import type { Service } from "../../api/types.gen";

export interface Cmd {
  id: string;
  title: string;
  subtitle?: string;
  group?: string;
  keywords?: string[];
  shortcut?: string;
  /** 20px leading icon. Sub-menus such as the theme list have none. */
  icon?: ReactNode;
  /** Create rows (New ...) keep the accent colour on their plus icon. */
  accent?: boolean;
  /** Keycap chips on the right ("G", "D"; "Cmd", "/"). */
  chips?: string[];
  /** Trailing arrow for rows that leave the console. */
  external?: boolean;
  /** Trailing control for project rows. */
  star?: { on: boolean; toggle: () => void };
  /** Input placeholder while this command's sub-menu is open. */
  placeholder?: string;
  /** Runs the command; the palette closes afterwards. */
  run?: () => void;
  /** Items with children open a sub-menu (Arrow Right or Enter). */
  children?: () => Cmd[];
}

/** One search result: the command and the breadcrumb path of the group it sits in. */
export interface Hit { cmd: Cmd; path: string[]; score: number }
export interface HitGroup { header: string; hits: Hit[] }

/** Flattens the whole command tree so a query can match every level at once. */
export function flatten(cmds: Cmd[], parents?: string[]): Hit[] {
  const out: Hit[] = [];
  for (const c of cmds) {
    const path = parents || [c.group || "General"];
    out.push({ cmd: c, path, score: 0 });
    if (c.children) out.push(...flatten(c.children(), [...path, c.title]));
  }
  return out;
}

/** Scores, filters and groups hits under breadcrumb headers; the group of the best hit comes first. */
export function searchGroups(cmds: Cmd[], query: string): HitGroup[] {
  const q = query.trim();
  const groups = new Map<string, HitGroup & { best: number; i: number }>();
  flatten(cmds).forEach((h, i) => {
    const c = h.cmd;
    const s = Math.max(
      score(c.title, q),
      ...(c.keywords || []).map((k) => score(k, q) - 25),
      c.subtitle ? score(c.subtitle, q) - 20 : 0,
      ...h.path.slice(1).map((p) => score(p, q) - 30),
    );
    if (s <= 0) return;
    const header = h.path.join(" › ");
    const g = groups.get(header) || { header, hits: [], best: 0, i };
    g.hits.push({ ...h, score: s });
    g.best = Math.max(g.best, s);
    groups.set(header, g);
  });
  return [...groups.values()]
    .sort((a, b) => b.best - a.best || a.i - b.i)
    .map((g) => ({ header: g.header, hits: g.hits.sort((a, b) => b.score - a.score) }));
}

/** Scores how well a query matches text: higher is better, 0 means no match. */
export function score(text: string, query: string): number {
  const t = text.toLowerCase(), q = query.trim().toLowerCase();
  if (!q) return 1;
  if (t === q) return 100;
  if (t.startsWith(q)) return 80;
  if (t.split(/[\s/._-]+/).some((w) => w.startsWith(q))) return 60;
  if (t.includes(q)) return 40;
  // in order, not adjacent: "rdp" matches "Redeploy"
  let i = 0;
  for (const ch of t) if (ch === q[i]) i++;
  return i === q.length ? 10 : 0;
}

/** Filters and orders commands for a query; ties keep the given order. */
export function rank(cmds: Cmd[], query: string): Cmd[] {
  if (!query.trim()) return cmds;
  return cmds
    .map((c, i) => ({ c, i, s: Math.max(score(c.title, query), ...(c.keywords || []).map((k) => score(k, query) - 25), c.subtitle ? score(c.subtitle, query) - 20 : 0) }))
    .filter((x) => x.s > 0)
    .sort((a, b) => b.s - a.s || a.i - b.i)
    .map((x) => x.c);
}

/** A sub-menu stack: the palette shows the top level. */
export interface Level { title?: string; placeholder?: string; items: Cmd[] }

export function push(stack: Level[], cmd: Cmd, items: Cmd[]): Level[] {
  return [...stack, { title: cmd.title, placeholder: cmd.placeholder, items }];
}

/** Backspace on an empty query goes back one level (never past the root). */
export function back(stack: Level[]): Level[] {
  return stack.length > 1 ? stack.slice(0, -1) : stack;
}

/**
 * The Redeploy command's label: "Redeploy service image" for image services,
 * "Redeploy latest deployment" when there is no source, hidden (null) when
 * there is no deployment.
 */
export function redeployLabel(s: Pick<Service, "activeDeployment" | "config">): string | null {
  if (!s.activeDeployment) return null;
  const t = s.config.source?.type;
  if (t === "image") return "Redeploy service image";
  if (!t || t === "empty" || t === "upload") return "Redeploy latest deployment";
  return "Redeploy";
}

/** The GitHub issue link for "Report a missing command". */
export const missingCommandUrl = (q: string) =>
  `https://github.com/musakkka/5050cloud/issues/new?labels=palette&title=${encodeURIComponent(`Missing command: ${q}`)}`;
