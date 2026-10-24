// SPDX-License-Identifier: AGPL-3.0-only

// Pure helpers for the projects dashboard: sorting, favourites, the production summary of a
// project and the service tiles drawn in each card. Kept free of React so they are easy to test.
import type { EnvSummary, MiniNode, Project } from "../../api/client";
import { toneOf, type Tone } from "../../kit";

export type Sort = "activity" | "created" | "name";
export type View = "grid" | "list";

/** Sort options in the order the control lists them. `title` keeps the short label searchable. */
export const SORTS: { value: Sort; label: string; title?: string }[] = [
  { value: "activity", label: "Sort By: Recent Activity" },
  { value: "created", label: "Sort By: Creation Date" },
  { value: "name", label: "Sort By: Alphabetical", title: "A-Z" },
];

/** Preference keys. Favourites are shared with the command palette. */
export const PREF = { sort: "5050.sort", view: "5050.view", favourites: "5050.favourites", legacyStars: "5050.stars" } as const;

export const isSort = (v: unknown): v is Sort => v === "activity" || v === "created" || v === "name";
export const isView = (v: unknown): v is View => v === "grid" || v === "list";

/** Orders a list the same way the server does, so a sort change shows at once. */
export function sortProjects(list: Project[], sort: Sort): Project[] {
  const out = [...list];
  if (sort === "name") out.sort((a, b) => a.name.localeCompare(b.name, undefined, { sensitivity: "base", numeric: true }));
  else if (sort === "created") out.sort((a, b) => b.createdAt - a.createdAt);
  else out.sort((a, b) => b.lastActivityAt - a.lastActivityAt);
  return out;
}

/** Favourites first, then the rest, each part keeping the given order. An optional query filters by name. */
export function arrange(list: Project[], favourites: string[], query = ""): Project[] {
  const q = query.trim().toLowerCase();
  const hit = q ? list.filter((p) => p.name.toLowerCase().includes(q) || (p.description || "").toLowerCase().includes(q)) : list;
  const fav = new Set(favourites);
  return [...hit.filter((p) => fav.has(p.id)), ...hit.filter((p) => !fav.has(p.id))];
}

export function toggleIn(list: string[], id: string): string[] {
  return list.includes(id) ? list.filter((x) => x !== id) : [...list, id];
}

/** The environment a card summarises: production when it exists, else the first one. */
export function primaryEnv(p: Project) {
  return p.environments.find((e) => e.name === "production") || p.environments[0];
}

export function primarySummary(p: Project): EnvSummary | undefined {
  const env = primaryEnv(p);
  return p.summary?.find((s) => s.environmentId === env?.id) || p.summary?.[0];
}

/** "1/1 service online", "0/4 services online", or "No services" for an empty environment. */
export function onlineText(s?: EnvSummary): string {
  if (!s || !s.total) return "No services";
  return `${s.online}/${s.total} ${s.total === 1 ? "service" : "services"} online`;
}

/** Health of the summarised environment: red when something failed, amber when partly up. */
export function summaryTone(s?: EnvSummary): Tone {
  if (!s || !s.total) return "idle";
  const states = (s.mini || []).map((m) => toneOf(m.status));
  if (states.includes("bad")) return "bad";
  if (states.includes("busy")) return "busy";
  if (s.online >= s.total) return "ok";
  if (s.online > 0) return "warn";
  return "idle";
}

export interface Tile {
  key: string;
  icon: string;
  source?: string;
  /** Service id when known: the tile then links to that service. */
  serviceId?: string;
  status?: string;
}

/**
 * The service tiles of a card, left to right in canvas order (by x, then y). Uses the mini canvas
 * when the server sent one (it carries service ids), otherwise the icon chips.
 */
export function tilesOf(s?: EnvSummary): Tile[] {
  if (!s) return [];
  if (s.mini?.length) {
    // the server lists icons and mini nodes from the same service list, so the source kind
    // (repo, image, cron) of node i is chip i when the lengths agree
    const chips = s.icons?.length === s.mini.length ? s.icons : [];
    const nodes = s.mini.map((m: MiniNode, i) => ({ m, source: chips[i]?.source || chips[i]?.kind }));
    nodes.sort((a, b) => (a.m.position[0] ?? 0) - (b.m.position[0] ?? 0) || (a.m.position[1] ?? 0) - (b.m.position[1] ?? 0));
    return nodes.map(({ m, source }) => ({ key: m.serviceId, icon: m.icon, source, serviceId: m.serviceId, status: m.status }));
  }
  return (s.icons || []).map((c, i) => ({ key: `${i}:${c.icon}`, icon: c.icon, source: c.source || c.kind }));
}

/** How many tiles fit a card well, and the column count: one row up to 5, then rows of 5, at most 3 rows. */
export const MAX_TILES = 15;
export function tileLayout(n: number): { shown: number; more: number; cols: number } {
  if (n <= MAX_TILES) return { shown: n, more: 0, cols: Math.max(1, Math.min(n, 5)) };
  return { shown: MAX_TILES - 1, more: n - (MAX_TILES - 1), cols: 5 };
}
