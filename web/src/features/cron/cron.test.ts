// SPDX-License-Identifier: AGPL-3.0-only
import { describe, expect, it } from "vitest";
import { nextRuns, parseCron, validTimeZone } from "./cron";

const at = (s: string) => Date.parse(s);
const iso = (ts: number[]) => ts.map((t) => new Date(t).toISOString());
function cron(e: string) {
  const r = parseCron(e);
  if (!r.ok) throw new Error(r.error);
  return r.cron;
}

describe("parseCron", () => {
  it("accepts macros, names, ranges, steps and lists", () => {
    for (const e of ["@hourly", "@daily", "@weekly", "@monthly", "*/5 * * * *", "0 9-17 * * MON-FRI", "0 0 1,15 jan,jul *", "30 2 * * 7"]) {
      expect(parseCron(e).ok).toBe(true);
    }
  });
  it("rejects bad input with a sentence", () => {
    const bad = ["", "* * * *", "60 * * * *", "* 24 * * *", "* * 0 * *", "* * * 13 *", "*/0 * * * *", "@sometimes", "a * * * *"];
    for (const e of bad) {
      const r = parseCron(e);
      expect(r.ok).toBe(false);
      if (!r.ok) expect(r.error.length).toBeGreaterThan(5);
    }
  });
});

describe("nextRuns", () => {
  const from = at("2026-10-22T10:17:30Z");
  it("every hour", () => {
    expect(iso(nextRuns(cron("@hourly"), 3, from))).toEqual(["2026-10-22T11:00:00.000Z", "2026-10-22T12:00:00.000Z", "2026-10-22T13:00:00.000Z"]);
  });
  it("every 15 minutes", () => {
    expect(iso(nextRuns(cron("*/15 * * * *"), 2, from))).toEqual(["2026-10-22T10:30:00.000Z", "2026-10-22T10:45:00.000Z"]);
  });
  it("weekly on Sunday (2026-10-25 is a Sunday)", () => {
    expect(iso(nextRuns(cron("@weekly"), 1, from))).toEqual(["2026-10-25T00:00:00.000Z"]);
  });
  it("monthly and day-of-month OR day-of-week", () => {
    expect(iso(nextRuns(cron("@monthly"), 2, from))).toEqual(["2026-11-01T00:00:00.000Z", "2026-12-01T00:00:00.000Z"]);
    expect(iso(nextRuns(cron("0 0 1 * MON"), 2, from))).toEqual(["2026-10-26T00:00:00.000Z", "2026-11-01T00:00:00.000Z"]);
  });
  it("honours the time zone", () => {
    // 09:00 in New York is 13:00 UTC during daylight time (until 2026-11-01).
    expect(iso(nextRuns(cron("0 9 * * *"), 1, from, "America/New_York"))).toEqual(["2026-10-22T13:00:00.000Z"]);
    expect(iso(nextRuns(cron("0 9 * * *"), 1, at("2026-11-05T00:00:00Z"), "America/New_York"))).toEqual(["2026-11-05T14:00:00.000Z"]);
  });
  it("finds Feb 29", () => {
    expect(iso(nextRuns(cron("0 0 29 2 *"), 1, from))).toEqual(["2028-02-29T00:00:00.000Z"]);
  });
  it("validates time zones", () => {
    expect(validTimeZone("Europe/Berlin")).toBe(true);
    expect(validTimeZone("Mars/Base")).toBe(false);
  });
});
