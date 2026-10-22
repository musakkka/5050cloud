// SPDX-License-Identifier: AGPL-3.0-only
import { describe, expect, it } from "vitest";
import type { LogLine } from "../../api/types.gen";
import { addFilter, flatten, histogram, isDropped, mergeLines } from "./query";

const line = (id: string, ts: number, extra: Partial<LogLine> = {}): LogLine => ({ id, ts, msg: id, stream: "stdout", level: "info", ...extra });

describe("logs helpers", () => {
  it("flattens nested attributes", () => {
    expect(flatten({ a: 1, b: { c: "x", d: { e: true } }, f: [1, 2] })).toEqual([["a", "1"], ["b.c", "x"], ["b.d.e", "true"], ["f", "[1,2]"]]);
  });
  it("adds filters once and quotes spaces", () => {
    expect(addFilter("", "level", "error")).toBe("@level:error");
    expect(addFilter("@level:error", "level", "error")).toBe("@level:error");
    expect(addFilter("timeout", "path", "/a b")).toBe('timeout @path:"/a b"');
  });
  it("detects the dropped-lines notice", () => {
    expect(isDropped(line("x", 1, { msg: "120 log lines dropped: more than 500 lines per second", stream: "system" }))).toBe(true);
    expect(isDropped(line("x", 1))).toBe(false);
  });
  it("merges without duplicates, sorted, capped", () => {
    const a = [line("1", 1), line("2", 2)];
    expect(mergeLines(a, [line("2", 2), line("3", 3)]).map((l) => l.id)).toEqual(["1", "2", "3"]);
    expect(mergeLines(a, [line("0", 0)]).map((l) => l.id)).toEqual(["0", "1", "2"]);
    expect(mergeLines(a, [line("3", 3)], 2).map((l) => l.id)).toEqual(["2", "3"]);
  });
  it("buckets a histogram", () => {
    const h = histogram([line("1", 0), line("2", 500, { level: "error" }), line("3", 5000)], 5);
    expect(h.length).toBe(5);
    expect(h.reduce((s, b) => s + b[1], 0)).toBe(3);
    expect(h.reduce((s, b) => s + b[2], 0)).toBe(1);
  });
});
