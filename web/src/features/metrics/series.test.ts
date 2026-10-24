// SPDX-License-Identifier: AGPL-3.0-only
import { describe, expect, it } from "vitest";
import { bucketPoints, fmtBytesDec, fmtValue, joinSeries, niceTicks, parseRelative, rangeBounds, readRange, valueAt, writeRange } from "./series";

describe("metrics helpers", () => {
  it("joins series by timestamp", () => {
    expect(joinSeries({ a: [[1, 2], [2, 3]], b: [[2, 5]] }, ["a", "b"], { b: 10 })).toEqual([{ ts: 1, a: 2 }, { ts: 2, a: 3, b: 50 }]);
  });
  it("round-trips the range through the URL", () => {
    expect(readRange(writeRange("?x=1", { range: "6h" }))).toEqual({ range: "6h" });
    expect(readRange(writeRange("", { from: 100, until: 200 }))).toEqual({ from: 100, until: 200 });
    expect(readRange("")).toEqual({ range: "7d" });
    expect(writeRange("?x=1&mrange=6h", { range: "7d" })).toBe("?x=1");
  });
  it("parses relative times", () => {
    const now = 1_000_000_000;
    expect(parseRelative("now", now)).toBe(now);
    expect(parseRelative("2 hours ago", now)).toBe(now - 7_200_000);
    expect(parseRelative("30 minutes ago", now)).toBe(now - 1_800_000);
    expect(parseRelative("1d ago", now)).toBe(now - 86_400_000);
    expect(parseRelative("7d ago", now)).toBe(now - 7 * 86_400_000);
    expect(parseRelative("garbage", now)).toBeNull();
  });
  it("formats values", () => {
    expect(fmtValue("error_rate", 0.123)).toBe("12.3%");
    expect(fmtValue("mem", 1536)).toBe("1.5 KB");
    expect(fmtValue("p90", 12.4)).toBe("12 ms");
    expect(fmtBytesDec(193e6)).toBe("193 MB");
    expect(fmtBytesDec(0, 300e6)).toBe("0 B");
    expect(fmtBytesDec(200e6, 600e6)).toBe("200 MB");
  });
  it("builds nice ticks", () => {
    expect(niceTicks(0.14, 3)).toEqual([0, 0.05, 0.1, 0.15]);
    expect(niceTicks(0)).toEqual([0, 1]);
  });
  it("resolves bounds, buckets and looks up values", () => {
    expect(rangeBounds({ range: "1h" }, 10_000_000)).toEqual({ from: 10_000_000 - 3_600_000, until: 10_000_000 });
    expect(bucketPoints([[0, 1], [1, 3], [10, 5]], 0, 10, 2, "sum")).toEqual([[0, 4], [5, 5]]);
    expect(valueAt([[1, 2], [5, 6]], 4)).toBe(2);
    expect(valueAt([[1, 2]], 0)).toBeUndefined();
  });
});
