// SPDX-License-Identifier: AGPL-3.0-only
import { describe, expect, it } from "vitest";
import { fmtValue, joinSeries, parseRelative, readRange, writeRange } from "./series";

describe("metrics helpers", () => {
  it("joins series by timestamp", () => {
    expect(joinSeries({ a: [[1, 2], [2, 3]], b: [[2, 5]] }, ["a", "b"], { b: 10 })).toEqual([{ ts: 1, a: 2 }, { ts: 2, a: 3, b: 50 }]);
  });
  it("round-trips the range through the URL", () => {
    expect(readRange(writeRange("?x=1", { range: "6h" }))).toEqual({ range: "6h" });
    expect(readRange(writeRange("", { from: 100, until: 200 }))).toEqual({ from: 100, until: 200 });
    expect(readRange("")).toEqual({ range: "1h" });
    expect(writeRange("?x=1&mrange=6h", { range: "1h" })).toBe("?x=1");
  });
  it("parses relative times", () => {
    const now = 1_000_000_000;
    expect(parseRelative("now", now)).toBe(now);
    expect(parseRelative("2 hours ago", now)).toBe(now - 7_200_000);
    expect(parseRelative("30 minutes ago", now)).toBe(now - 1_800_000);
    expect(parseRelative("1d ago", now)).toBe(now - 86_400_000);
    expect(parseRelative("garbage", now)).toBeNull();
  });
  it("formats values", () => {
    expect(fmtValue("error_rate", 0.123)).toBe("12.3%");
    expect(fmtValue("mem", 1536)).toBe("1.5 KB");
    expect(fmtValue("p90", 12.4)).toBe("12 ms");
  });
});
