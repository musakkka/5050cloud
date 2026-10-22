// SPDX-License-Identifier: AGPL-3.0-only
import { describe, expect, it } from "vitest";
import { parseRaw, refAt, refSuggestions, toEnv } from "./raw";

describe("parseRaw", () => {
  it("parses .env text with comments, export, quotes and multi-line values", () => {
    const r = parseRaw(`# comment
export A=1
B = "two words"  
C='single # not comment'
D=plain # trailing comment
E="line1
line2"
F="esc\\nnew"
G=\${{Postgres.DATABASE_URL}}
`);
    expect(r.errors).toEqual([]);
    expect(r.format).toBe("env");
    expect(r.vars).toEqual({
      A: "1", B: "two words", C: "single # not comment", D: "plain", E: "line1\nline2", F: "esc\nnew",
      G: "${{Postgres.DATABASE_URL}}",
    });
  });
  it("reports bad lines and names", () => {
    const r = parseRaw("1BAD=x\nnovalue\nOK=y");
    expect(r.vars).toEqual({ OK: "y" });
    expect(r.errors.length).toBe(2);
  });
  it("parses JSON objects", () => {
    const r = parseRaw(`{"A": "x", "N": 5, "B": true, "bad name": "z", "O": {}}`);
    expect(r.format).toBe("json");
    expect(r.vars).toEqual({ A: "x", N: "5", B: "true" });
    expect(r.errors.length).toBe(2);
  });
  it("reports invalid JSON", () => {
    expect(parseRaw("{nope").errors[0]).toMatch(/Not valid JSON/);
    expect(parseRaw("").format).toBe("empty");
  });
  it("round-trips through toEnv", () => {
    const vars = { A: "1", B: "has space", C: 'q"uote', D: "multi\nline", E: "" };
    expect(parseRaw(toEnv(vars)).vars).toEqual(vars);
  });
});

describe("references", () => {
  it("finds the reference being typed", () => {
    expect(refAt("x${{Pos", 7)).toEqual({ start: 1, partial: "Pos" });
    expect(refAt("${{A.B}} rest", 12)).toBeNull();
    expect(refAt("plain", 5)).toBeNull();
  });
  it("suggests services, then their variables", () => {
    const svcs = [{ name: "Postgres", vars: ["DATABASE_URL", "PGHOST"] }, { name: "api", vars: ["PORT"] }];
    expect(refSuggestions("Po", svcs)).toEqual(["Postgres."]);
    expect(refSuggestions("Postgres.DA", svcs)).toEqual(["Postgres.DATABASE_URL"]);
    expect(refSuggestions("sec", svcs)).toEqual(["secret(32)"]);
  });
});
