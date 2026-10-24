// SPDX-License-Identifier: AGPL-3.0-only

// Canvas geometry on a 24px grid: 288x144 cards, 48px modifier strips (volume, cron schedule,
// replicas), group frames with 24px sides and a 48px header, a left-to-right dependency layout,
// overlap repair and snapping.
import type { Edge as ApiEdge, Group as ApiGroup, Service } from "../../api/client";

/** What the layout needs to know about a card. */
export interface CanvasNode { id: string; name: string; kind: string; strips: number; domain: boolean; svcKind?: string; group: string | null }
export interface Group { name: string; icon?: string | null; parent: string | null }

const DB_KIND: Record<string, string> = { postgres: "postgres", postgresql: "postgres", mysql: "mysql", redis: "redis", mongodb: "mongodb", mongo: "mongodb" };

export const GRID = 24;
export const W = 12 * GRID;           // 288
export const H = 6 * GRID;            // 144
export const STRIP = 2 * GRID;        // 48 visible per modifier strip
export const TUCK = GRID;             // each strip hides 24px under the card above it
export const PAD = GRID;              // group frame sides and bottom
export const HEAD = 2 * GRID;         // group frame header band
export const EMPTY_W = 17 * GRID;     // 408: the empty-project card
export const EMPTY_H = 15 * GRID;     // 360
export const COLLAPSED_H = 11 * GRID; // 264: a group with its contents hidden
const GAP = 3 * GRID;                 // 72 between ranks and between cards in a rank
const BLOCK_GAP = 5 * GRID;           // 120 between disconnected clusters and groups

/** The modifier strips a service shows under its card, in stacking order. */
export type StripKind = "volume" | "cron" | "replicas";
export function stripsOf(s: Service): StripKind[] {
  const cfg = s.staged || s.config;
  const out: StripKind[] = [];
  if (s.volume) out.push("volume");
  if (s.kind === "cron" || cfg.deploy.cronSchedule) out.push("cron");
  if ((cfg.deploy.replicas ?? 1) > 1) out.push("replicas");
  return out;
}

/** Maps API services and groups onto layout input. */
export function layoutInput(services: Service[], groups: ApiGroup[]): { nodes: CanvasNode[]; groups: Record<string, Group> } {
  const gmap: Record<string, Group> = {};
  const member: Record<string, string> = {};
  for (const g of groups) { gmap[g.id] = { name: g.name, icon: g.icon || null, parent: g.parent || null }; for (const s of g.services) member[s] = g.id; }
  const nodes = services.map((s) => {
    const c = s.staged || s.config;
    const db = s.kind === "database" ? DB_KIND[c.template || c.icon] || "postgres" : "app";
    return { id: s.id, name: c.name, kind: db, strips: stripsOf(s).length, domain: s.domains.length > 0, svcKind: s.kind, group: member[s.id] || null };
  });
  return { nodes, groups: gmap };
}

export const nodeH = (n: Pick<CanvasNode, "strips">) => H + n.strips * STRIP;
export const snap = (v: number) => Math.round(v / GRID) * GRID;

export type Pos = Record<string, [number, number]>;
export interface Frame { id: string; name: string; icon?: string | null; x: number; y: number; w: number; h: number; depth: number }
interface Box { w: number; h: number; place: (x: number, y: number, out: Pos) => void }

const parentOf = (groups: Record<string, Group>, gid: string) => {
  const p = groups[gid]?.parent;
  return p && groups[p] ? p : null;
};
const groupOf = (groups: Record<string, Group>, n: CanvasNode) => (n.group && groups[n.group] ? n.group : null);
const DATA = new Set(["postgres", "mysql", "redis", "mongodb"]);

/** Something the layout places: a card, or a group block (frame plus contents). */
interface Item { id: string; name: string; data: boolean; domain: boolean; w: number; h: number; place: (x: number, y: number, out: Pos) => void }
interface Link { source: string; target: string }

/**
 * Ranks for a left-to-right layout: an item sits one rank left of everything it references,
 * so apps come first and the databases they lean on end up on the right.
 */
export function ranks(ids: string[], edges: Link[]): Map<string, number> {
  const set = new Set(ids);
  const out = new Map<string, string[]>();
  for (const e of edges) if (set.has(e.source) && set.has(e.target) && e.source !== e.target) (out.get(e.source) || out.set(e.source, []).get(e.source)!).push(e.target);
  const height = new Map<string, number>();
  const visiting = new Set<string>();
  const h = (id: string): number => {
    if (height.has(id)) return height.get(id)!;
    if (visiting.has(id)) return 0;   // cycles: cut here
    visiting.add(id);
    let v = 0;
    for (const t of out.get(id) || []) v = Math.max(v, h(t) + 1);
    visiting.delete(id);
    height.set(id, v);
    return v;
  };
  const max = Math.max(0, ...ids.map(h));
  const r = new Map<string, number>();
  for (const id of ids) r.set(id, max - h(id));
  return r;
}

const byItemName = (a: Item, b: Item) => a.name.localeCompare(b.name);

/** Lays items out: linked items as rank columns (left to right), unlinked ones as a wide grid. */
function arrange(items: Item[], edges: Link[]): Box {
  const ids = new Set(items.map((i) => i.id));
  const links = edges.filter((e) => ids.has(e.source) && ids.has(e.target) && e.source !== e.target);
  let cols: Item[][];
  if (links.length) {
    const r = ranks(items.map((i) => i.id), links);
    const n = Math.max(0, ...r.values()) + 1;
    cols = Array.from({ length: n }, () => [] as Item[]);
    // order inside a rank: follow the order of what points at it, so lines cross less
    const order = new Map<string, number>();
    for (let k = 0; k < n; k++) {
      const col = items.filter((i) => (r.get(i.id) || 0) === k);
      const score = (i: Item) => {
        const from = links.filter((e) => e.target === i.id).map((e) => order.get(e.source)).filter((v): v is number => v !== undefined);
        return from.length ? from.reduce((a, b) => a + b, 0) / from.length : Number.MAX_SAFE_INTEGER;
      };
      col.sort((a, b) => score(a) - score(b) || Number(a.data) - Number(b.data) || byItemName(a, b));
      col.forEach((i, j) => order.set(i.id, j));
      cols[k] = col;
    }
  } else {
    const all = [...items].sort((a, b) => Number(a.data) - Number(b.data) || Number(b.domain) - Number(a.domain) || byItemName(a, b));
    const per = Math.max(1, Math.ceil(Math.sqrt(all.length / 1.6)));   // rows per column, aiming at a wide shape
    cols = [];
    all.forEach((it, i) => (cols[Math.floor(i / per)] ||= []).push(it));
  }
  cols = cols.filter((c) => c.length);
  const colW = cols.map((c) => Math.max(...c.map((i) => i.w)));
  const colH = cols.map((c) => c.reduce((a, i) => a + i.h, 0) + (c.length - 1) * GAP);
  const w = colW.reduce((a, b) => a + b, 0) + Math.max(0, cols.length - 1) * GAP;
  const h = Math.max(0, ...colH);
  return {
    w, h,
    place(x, y, out) {
      let xx = x;
      cols.forEach((c, k) => {
        let yy = y + snap((h - colH[k]) / 2);   // centre each rank on the tallest one
        for (const it of c) { it.place(xx, yy, out); yy += it.h + GAP; }
        xx += colW[k] + GAP;
      });
    },
  };
}

const cardItem = (n: CanvasNode): Item => ({ id: n.id, name: n.name, data: DATA.has(n.kind), domain: n.domain, w: W, h: nodeH(n), place: (x, y, out) => { out[n.id] = [x, y]; } });
const framed = (inner: Box): Box => ({ w: inner.w + 2 * PAD, h: inner.h + HEAD + PAD, place: (x, y, out) => inner.place(x + PAD, y + HEAD, out) });

/** Connected components of items. */
function components(items: Item[], edges: Link[]): Item[][] {
  const idx = new Map(items.map((n, i) => [n.id, i]));
  const parent = items.map((_, i) => i);
  const find = (i: number): number => (parent[i] === i ? i : (parent[i] = find(parent[i])));
  for (const e of edges) { const a = idx.get(e.source), b = idx.get(e.target); if (a !== undefined && b !== undefined) parent[find(a)] = find(b); }
  const by = new Map<number, Item[]>();
  items.forEach((n, i) => (by.get(find(i)) || by.set(find(i), []).get(find(i))!).push(n));
  const comps = [...by.values()];
  const lone = comps.filter((c) => c.length === 1).flat();
  const rest = comps.filter((c) => c.length > 1).sort((a, b) => b.length - a.length);
  return lone.length ? [...rest, lone] : rest;
}

/**
 * Auto Layout: groups are compound items (their contents laid out inside the frame), linked
 * items are ranked left to right, and disconnected clusters are packed in rows aiming at 1.6:1.
 */
export function autoLayout(nodes: CanvasNode[], groups: Record<string, Group>, edges: ApiEdge[] = []): Pos {
  const memberGroup = new Map(nodes.map((n) => [n.id, groupOf(groups, n)]));
  /** The item id a service is represented by inside container `within` (null = top level). */
  const repr = (sid: string, within: string | null): string | null => {
    let g = memberGroup.get(sid) ?? null;
    if (g === within) return sid;
    while (g) { const p = parentOf(groups, g); if (p === within) return `g:${g}`; g = p; }
    return null;
  };
  const linksWithin = (within: string | null): Link[] => {
    const out: Link[] = [];
    const seen = new Set<string>();
    for (const e of edges) {
      const a = repr(e.source, within), b = repr(e.target, within);
      if (!a || !b || a === b || seen.has(`${a}>${b}`)) continue;
      seen.add(`${a}>${b}`);
      out.push({ source: a, target: b });
    }
    return out;
  };
  const itemsWithin = (within: string | null): Item[] => {
    const cards = nodes.filter((n) => groupOf(groups, n) === within).map(cardItem);
    const subs = Object.keys(groups).filter((g) => parentOf(groups, g) === within).map((g): Item | null => {
      const inner = itemsWithin(g);
      if (!inner.length) return null;
      const box = framed(arrange(inner, linksWithin(g)));
      const kids = nodes.filter((n) => groupOf(groups, n) === g);
      return { id: `g:${g}`, name: groups[g].name, data: kids.length > 0 && kids.every((k) => DATA.has(k.kind)), domain: false, ...box };
    }).filter((b): b is Item => !!b);
    return [...cards, ...subs];
  };
  const top = itemsWithin(null);
  const links = linksWithin(null);
  const all: Box[] = components(top, links).map((c) => arrange(c, links)).sort((a, b) => b.w * b.h - a.w * a.h);

  const shelve = (maxW: number) => {
    const at: [number, number][] = [];
    let x = 0, y = 0, rowH = 0, w = 0;
    for (const b of all) {
      if (x > 0 && x + b.w > maxW) { x = 0; y += rowH + BLOCK_GAP; rowH = 0; }
      at.push([x, y]);
      x += b.w + BLOCK_GAP; w = Math.max(w, x - BLOCK_GAP); rowH = Math.max(rowH, b.h);
    }
    return { at, score: Math.max(w / 1.6, y + rowH) };
  };
  const widest = Math.max(0, ...all.map((b) => b.w));
  const total = all.reduce((a, b) => a + b.w + BLOCK_GAP, 0);
  let best = shelve(widest);
  for (let mw = widest; mw <= total; mw += 240) { const s = shelve(mw); if (s.score < best.score) best = s; }
  const out: Pos = {};
  all.forEach((b, i) => b.place(best.at[i][0], best.at[i][1], out));
  for (const k of Object.keys(out)) out[k] = [snap(out[k][0]), snap(out[k][1])];
  return out;
}

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
    out[n.id] = [snap((Number.isFinite(left) ? left : 0) + (i % 4) * (W + GAP)), snap((Number.isFinite(bottom) ? bottom : 0) + BLOCK_GAP + Math.floor(i / 4) * (H + GAP))];
    i++;
  }
  return out;
}

/** Group frames follow their members, so dragging a card resizes its group. */
export function frames(nodes: CanvasNode[], groups: Record<string, Group>, pos: Pos, collapsed: string[] = []): Frame[] {
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
    const f: Frame = { id: gid, name: groups[gid].name, icon: groups[gid].icon, x: x0 - PAD, y: y0 - HEAD, w: x1 - x0 + 2 * PAD, h: y1 - y0 + HEAD + PAD, depth: depth(gid) };
    if (collapsed.includes(gid)) { f.w = W; f.h = COLLAPSED_H; }
    box[gid] = f;
  }
  return Object.values(box).sort((a, b) => a.depth - b.depth);
}

interface Rect { x: number; y: number; w: number; h: number }
const MARGIN = GRID;
const hits = (a: Rect, b: Rect) => a.x < b.x + b.w + MARGIN && b.x < a.x + a.w + MARGIN && a.y < b.y + b.h + MARGIN && b.y < a.y + a.h + MARGIN;

/** The nearest free grid slot for one card, given the others (used after a drop). */
export function freeSpot(id: string, nodes: CanvasNode[], pos: Pos): [number, number] {
  const me = nodes.find((n) => n.id === id);
  const p = pos[id];
  if (!me || !p) return p;
  const others = nodes.filter((n) => n.id !== id && pos[n.id]).map((n) => ({ x: pos[n.id][0], y: pos[n.id][1], w: W, h: nodeH(n) }));
  const at = (x: number, y: number) => ({ x, y, w: W, h: nodeH(me) });
  if (!others.some((o) => hits(at(p[0], p[1]), o))) return p;
  for (let r = 1; r < 60; r++) {
    for (let dx = -r; dx <= r; dx++) for (const dy of [-r, r]) {
      const x = p[0] + dx * GRID, y = p[1] + dy * GRID;
      if (!others.some((o) => hits(at(x, y), o))) return [x, y];
    }
    for (let dy = -r + 1; dy < r; dy++) for (const dx of [-r, r]) {
      const x = p[0] + dx * GRID, y = p[1] + dy * GRID;
      if (!others.some((o) => hits(at(x, y), o))) return [x, y];
    }
  }
  return p;
}

/** Repair Overlaps: pushes cards that overlap an earlier one down or right; returns the moved ids too. */
export function repairOverlaps(nodes: CanvasNode[], pos: Pos): { pos: Pos; moved: string[] } {
  const out: Pos = { ...pos };
  const moved: string[] = [];
  const placed: Rect[] = [];
  const order = nodes.filter((n) => out[n.id]).sort((a, b) => out[a.id][1] - out[b.id][1] || out[a.id][0] - out[b.id][0]);
  for (const n of order) {
    let [x, y] = out[n.id];
    const r = () => ({ x, y, w: W, h: nodeH(n) });
    let guard = 0;
    let o: Rect | undefined;
    while ((o = placed.find((q) => hits(r(), q))) && guard++ < 200) {
      // move the shorter way: right of the blocker or under it
      const right = o.x + o.w + MARGIN - x, down = o.y + o.h + MARGIN - y;
      if (right <= down) x = snap(o.x + o.w + MARGIN); else y = snap(o.y + o.h + MARGIN);
    }
    if (x !== out[n.id][0] || y !== out[n.id][1]) { out[n.id] = [x, y]; moved.push(n.id); }
    placed.push(r());
  }
  return { pos: out, moved };
}
