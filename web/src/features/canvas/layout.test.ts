// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it } from "vitest";
import { GRID, H, HEAD, PAD, W, autoLayout, frames, freeSpot, ranks, repairOverlaps, type CanvasNode } from "./layout";

const node = (id: string, kind = "app", group: string | null = null, strips = 0): CanvasNode => ({ id, name: id, kind, strips, domain: false, group });

describe("canvas layout", () => {
  it("ranks referrers left of what they reference", () => {
    const r = ranks(["web", "api", "db"], [{ source: "web", target: "api" }, { source: "api", target: "db" }]);
    expect([r.get("web"), r.get("api"), r.get("db")]).toEqual([0, 1, 2]);
  });

  it("survives reference cycles", () => {
    const r = ranks(["a", "b"], [{ source: "a", target: "b" }, { source: "b", target: "a" }]);
    expect(r.size).toBe(2);
  });

  it("lays linked services out left to right on the grid", () => {
    const p = autoLayout([node("web"), node("api"), node("db", "postgres")], {}, [{ source: "web", target: "api" }, { source: "api", target: "db" }]);
    expect(p.web[0]).toBeLessThan(p.api[0]);
    expect(p.api[0]).toBeLessThan(p.db[0]);
    for (const [x, y] of Object.values(p)) { expect(x % GRID).toBe(0); expect(y % GRID).toBe(0); }
  });

  it("frames a group with 24px sides and a 48px header", () => {
    const nodes = [node("a", "app", "g"), node("b", "app", "g", 1)];
    const groups = { g: { name: "G", parent: null } };
    const p = autoLayout(nodes, groups);
    const [f] = frames(nodes, groups, p);
    expect(f.x).toBe(Math.min(p.a[0], p.b[0]) - PAD);
    expect(f.y).toBe(Math.min(p.a[1], p.b[1]) - HEAD);
    expect(f.w).toBeGreaterThanOrEqual(W + 2 * PAD);
  });

  it("collapses a group to 288x264", () => {
    const nodes = [node("a", "app", "g")];
    const [f] = frames(nodes, { g: { name: "G", parent: null } }, { a: [0, 0] }, ["g"]);
    expect([f.w, f.h]).toEqual([288, 264]);
  });

  it("repairs overlaps and moves a dropped card to a free slot", () => {
    const nodes = [node("a"), node("b")];
    const r = repairOverlaps(nodes, { a: [0, 0], b: [24, 24] });
    expect(r.moved).toEqual(["b"]);
    const [bx, by] = r.pos.b;
    expect(bx >= W + GRID || by >= H + GRID).toBe(true);
    const spot = freeSpot("b", nodes, { a: [0, 0], b: [0, 0] });
    expect(spot).not.toEqual([0, 0]);
  });
});
