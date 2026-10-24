// SPDX-License-Identifier: AGPL-3.0-only
import { describe, expect, it } from "vitest";
import type { LogLine } from "../../api/types.gen";
import { addFilter, compileFilter, flatten, histogram, isDropped, mergeLines, parseFilter, parseTimeInput, quoteValues } from "./query";

const line = (id: string, ts: number, extra: Partial<LogLine> = {}): LogLine => ({ id, ts, msg: id, stream: "stdout", level: "info", ...extra });

describe("logs helpers", () => {
  it("flattens nested attributes", () => {
    expect(flatten({ a: 1, b: { c: "x", d: { e: true } }, f: [1, 2] })).toEqual([["a", "1"], ["b.c", "x"], ["b.d.e", "true"], ["f", "[1,2]"]]);
  });
  it("adds filters joined with AND, quoted, once", () => {
    expect(addFilter("", "level", "error")).toBe('@level:"error"');
    expect(addFilter('@level:"error"', "level", "error")).toBe('@level:"error"');
    expect(addFilter("timeout", "path", "/a b")).toBe('timeout AND @path:"/a b"');
  });
  it("quotes values on apply", () => {
    expect(quoteValues("@level:error foo")).toBe('@level:"error" foo');
  });
  it("matches the query language", () => {
    const l = line("a", 1, { level: "error", msg: "Connection timeout", serviceId: "s1" });
    const m = (q: string) => compileFilter(q, { serviceName: () => "Postgres" }).match(l);
    expect(m("@level:error")).toBe(true);
    expect(m("@level:error AND timeout")).toBe(true);
    expect(m("@level:info OR timeout")).toBe(true);
    expect(m("-timeout")).toBe(false);
    expect(m('(@service:Postgres OR @service:web) -@level:info')).toBe(true);
    expect(m('"connection timeout"')).toBe(true);
  });
  it("reports parse errors", () => {
    expect(parseFilter("@level:error AND").error).toMatch(/AND/);
    expect(parseFilter("(foo").error).toMatch(/parenthesis/);
    expect(parseFilter("@@@").error).toBeTruthy();
    expect(parseFilter("foo bar").error).toBeUndefined();
  });
  it("parses time input", () => {
    expect(parseTimeInput("1h ago", 10_000_000)).toBe(10_000_000 - 3_600_000);
    expect(parseTimeInput("now", 5)).toBe(5);
    expect(parseTimeInput("zzz", 5)).toBeNull();
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
