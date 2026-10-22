// SPDX-License-Identifier: AGPL-3.0-only
import { describe, expect, it } from "vitest";
import { History, groupSummary } from "./history";

describe("History", () => {
  it("undoes and redoes", () => {
    const h = new History<number>();
    h.push(1); // 1 -> 2
    h.push(2); // 2 -> 3
    expect(h.undo(3)).toBe(2);
    expect(h.undo(2)).toBe(1);
    expect(h.undo(1)).toBeUndefined();
    expect(h.redo(1)).toBe(2);
    expect(h.redo(2)).toBe(3);
    expect(h.redo(3)).toBeUndefined();
  });
  it("drops redo after a new change and keeps 50", () => {
    const h = new History<number>(50);
    for (let i = 0; i < 60; i++) h.push(i);
    let n = 0, cur = 60;
    for (let s = h.undo(cur); s !== undefined; s = h.undo(cur)) { cur = s; n++; }
    expect(n).toBe(50);
    h.push(99);
    expect(h.canRedo).toBe(false);
  });
});

describe("groupSummary", () => {
  it("counts statuses", () => {
    const L: Record<string, string> = { online: "Active", crashed: "Crashed" };
    expect(groupSummary(["online", "online", "crashed", "online"], (s) => L[s])).toBe("4 services: 3 Active, 1 Crashed");
  });
});
