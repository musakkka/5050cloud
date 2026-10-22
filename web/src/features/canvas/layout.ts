// SPDX-License-Identifier: AGPL-3.0-only

import type { Group as ApiGroup, Service } from "../../api/client";

/** What the layout needs to know about a card. */
export interface CanvasNode { id: string; name: string; kind: string; volume: boolean; domain: boolean; svcKind?: string; group: string | null }
export interface Group { name: string; icon?: string | null; parent: string | null }

const DB_KIND: Record<string, string> = { postgres: "postgres", postgresql: "postgres", mysql: "mysql", redis: "redis", mongodb: "mongodb", mongo: "mongodb" };

/** Maps API services and groups onto layout input. */
export function layoutInput(services: Service[], groups: ApiGroup[]): { nodes: CanvasNode[]; groups: Record<string, Group> } {
  const gmap: Record<string, Group> = {};
  const member: Record<string, string> = {};
  for (const g of groups) { gmap[g.id] = { name: g.name, icon: g.icon || null, parent: g.parent || null }; for (const s of g.services) member[s] = g.id; }
  const nodes = services.map((s) => {
    const c = s.staged || s.config;
    const db = s.kind === "database" ? DB_KIND[c.template || c.icon] || "postgres" : "app";
    return { id: s.id, name: c.name, kind: db, volume: !!s.volume, domain: s.domains.length > 0, svcKind: s.kind, group: member[s.id] || null };
  });
  return { nodes, groups: gmap };
}

// Card geometry: 288x144 cards on a 24px grid, a volume as a strip under the card.
export const W = 288;
export const H = 144;
export const VOL = 40;
export const GRID = 24;
const GAP_X = 72;
const GAP_Y = 72;
const PAD = 36;        // group frame padding
const HEAD = 48;       // room for the group's name inside its frame
const BLOCK_GAP = 120; // between groups

export const nodeH = (n: Pick<CanvasNode, "volume">) => H + (n.volume ? VOL : 0);

const DATA_KINDS = new Set(["postgres", "mysql", "redis", "mongodb"]);
const dataRank = (n: CanvasNode) => ({ postgres: 1, mysql: 2, mongodb: 3, redis: 4 } as Record<string, number>)[n.kind] ?? 5;
const appRank = (n: CanvasNode) => (n.domain ? 0 : n.svcKind === "cron" ? 2 : 1);
const byName = (a: CanvasNode, b: CanvasNode) => a.name.localeCompare(b.name);

export type Pos = Record<string, [number, number]>;
export interface Frame { id: string; name: string; icon?: string | null; x: number; y: number; w: number; h: number; depth: number }

interface Box { w: number; h: number; place: (x: number, y: number, out: Pos) => void }

const parentOf = (groups: Record<string, Group>, gid: string) => {
  const p = groups[gid]?.parent;
  return p && groups[p] ? p : null;
};
const groupOf = (groups: Record<string, Group>, n: CanvasNode) => (n.group && groups[n.group] ? n.group : null);

/** Apps in a grid on the left, the data they lean on (databases, nested groups) in a column on the right. */
function content(nodes: CanvasNode[], subs: Box[]): Box {
  let apps = nodes.filter((n) => !DATA_KINDS.has(n.kind)).sort((a, b) => appRank(a) - appRank(b) || byName(a, b));
  let data = nodes.filter((n) => DATA_KINDS.has(n.kind)).sort((a, b) => dataRank(a) - dataRank(b) || byName(a, b));
  if (!apps.length && !subs.length && data.length > 3) [apps, data] = [data, []];   // only databases: a grid, not a tower
  const cols = Math.min(4, Math.ceil(Math.sqrt(apps.length * 1.2)));
  const rows: CanvasNode[][] = [];
  apps.forEach((n, i) => (rows[Math.floor(i / cols)] ||= []).push(n));
  const rowH = rows.map((r) => Math.max(...r.map(nodeH)));
  const appsW = apps.length ? cols * W + (cols - 1) * GAP_X : 0;
  const appsH = rowH.reduce((a, h) => a + h, 0) + Math.max(0, rows.length - 1) * GAP_Y;

  const column: ({ node: CanvasNode } | { box: Box })[] = [...data.map((node) => ({ node })), ...subs.map((box) => ({ box }))];
  const itemW = (it: (typeof column)[number]) => ("node" in it ? W : it.box.w);
  const itemH = (it: (typeof column)[number]) => ("node" in it ? nodeH(it.node) : it.box.h);
  const colW = column.length ? Math.max(...column.map(itemW)) : 0;
  const colH = column.reduce((a, it) => a + itemH(it), 0) + Math.max(0, column.length - 1) * GAP_Y;
  const colX = appsW ? appsW + GAP_X * 1.5 : 0;

  return {
    w: colX + colW,
    h: Math.max(appsH, colH),
    place(x, y, out) {
      let yy = y;
      rows.forEach((r, i) => {
        r.forEach((n, j) => (out[n.id] = [x + j * (W + GAP_X), yy]));
        yy += rowH[i] + GAP_Y;
      });
      yy = y;
      for (const it of column) {
        if ("node" in it) out[it.node.id] = [x + colX, yy];
        else it.box.place(x + colX, yy, out);
        yy += itemH(it) + GAP_Y;
      }
    },
  };
}

function framed(inner: Box): Box {
  return { w: inner.w + 2 * PAD, h: inner.h + HEAD + PAD, place: (x, y, out) => inner.place(x + PAD, y + HEAD, out) };
}

/** A tidy layout for a project: each group packed on its own, groups and loose services shelved in rows. */
export function autoLayout(nodes: CanvasNode[], groups: Record<string, Group>): Pos {
  const blockOf = (gid: string): Box | null => {
    const subs = Object.keys(groups).filter((g) => parentOf(groups, g) === gid).map(blockOf).filter((b): b is Box => !!b);
    const mine = nodes.filter((n) => groupOf(groups, n) === gid);
    return mine.length || subs.length ? framed(content(mine, subs)) : null;
  };
  const blocks = Object.keys(groups).filter((g) => !parentOf(groups, g)).map(blockOf).filter((b): b is Box => !!b)
    .sort((a, b) => b.w * b.h - a.w * a.h);
  const loose = nodes.filter((n) => !groupOf(groups, n));
  if (loose.length) blocks.push(framed(content(loose, [])));   // same margins as a group, without the frame

  // shelf the blocks into rows, choosing the row width whose overall shape best fits a wide screen
  const shelve = (maxW: number) => {
    const at: [number, number][] = [];
    let x = 0, y = 0, rowH = 0, w = 0;
    for (const b of blocks) {
      if (x > 0 && x + b.w > maxW) { x = 0; y += rowH + BLOCK_GAP; rowH = 0; }
      at.push([x, y]);
      x += b.w + BLOCK_GAP; w = Math.max(w, x - BLOCK_GAP); rowH = Math.max(rowH, b.h);
    }
    return { at, score: Math.max(w / 1.6, y + rowH) };
  };
  const widest = Math.max(0, ...blocks.map((b) => b.w));
  const total = blocks.reduce((a, b) => a + b.w + BLOCK_GAP, 0);
  let best = shelve(widest);
  for (let mw = widest; mw <= total; mw += 240) { const s = shelve(mw); if (s.score < best.score) best = s; }
  const out: Pos = {};
  blocks.forEach((b, i) => b.place(best.at[i][0], best.at[i][1], out));
  for (const k of Object.keys(out)) out[k] = [snap(out[k][0]), snap(out[k][1])];
  return out;
}

export const snap = (v: number) => Math.round(v / GRID) * GRID;

/** Saved positions win; anything new since the save goes in a row under the saved layout. */
export function resolvePositions(nodes: CanvasNode[], auto: Pos, saved: Pos): Pos {
  if (!Object.keys(saved).length) return auto;
  const out: Pos = {};
  let bottom = -Infinity, left = Infinity;
  for (const n of nodes) {
    const p = saved[n.id];
    if (p) { out[n.id] = p; bottom = Math.max(bottom, p[1] + nodeH(n)); left = Math.min(left, p[0]); }
  }
  let i = 0;
  for (const n of nodes) {
    if (out[n.id]) continue;
    out[n.id] = [snap((Number.isFinite(left) ? left : 0) + (i % 4) * (W + GAP_X)), snap((Number.isFinite(bottom) ? bottom : 0) + BLOCK_GAP + Math.floor(i / 4) * (H + GAP_Y))];
    i++;
  }
  return out;
}

/** Group frames follow their members, so dragging a card resizes its group. */
export function frames(nodes: CanvasNode[], groups: Record<string, Group>, pos: Pos): Frame[] {
  const depth = (g: string): number => (parentOf(groups, g) ? 1 + depth(parentOf(groups, g)!) : 0);
  const order = Object.keys(groups).sort((a, b) => depth(b) - depth(a));
  const box: Record<string, Frame> = {};
  for (const gid of order) {
    let x0 = Infinity, y0 = Infinity, x1 = -Infinity, y1 = -Infinity;
    for (const n of nodes) {
      if (groupOf(groups, n) !== gid || !pos[n.id]) continue;
      const [x, y] = pos[n.id];
      x0 = Math.min(x0, x); y0 = Math.min(y0, y); x1 = Math.max(x1, x + W); y1 = Math.max(y1, y + nodeH(n));
    }
    for (const [cid, f] of Object.entries(box)) {
      if (parentOf(groups, cid) !== gid) continue;
      x0 = Math.min(x0, f.x); y0 = Math.min(y0, f.y); x1 = Math.max(x1, f.x + f.w); y1 = Math.max(y1, f.y + f.h);
    }
    if (!Number.isFinite(x0)) continue;
    box[gid] = { id: gid, name: groups[gid].name, icon: groups[gid].icon, x: x0 - PAD, y: y0 - HEAD, w: x1 - x0 + 2 * PAD, h: y1 - y0 + HEAD + PAD, depth: depth(gid) };
  }
  return Object.values(box).sort((a, b) => a.depth - b.depth);
}
