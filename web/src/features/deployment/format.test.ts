// SPDX-License-Identifier: AGPL-3.0-only
import { describe, expect, it } from "vitest";
import { groupSteps, matches, mmss, outcomeText, parseQuery, shortDuration } from "./format";

const T = 1767225600000;
const line = (msg: string, p: Record<string, unknown> = {}) => ({ msg, level: "info", stream: "stdout", ...p });

describe("deployment format helpers", () => {
  it("folds steps into the five card rows", () => {
    const g = groupSteps([
      { name: "queued", state: "ok", startedAt: T, finishedAt: T + 1000 },
      { name: "build", state: "ok", startedAt: T + 1000, finishedAt: T + 61000 },
      { name: "start", state: "ok", startedAt: T + 61000, finishedAt: T + 70000 },
      { name: "healthcheck", state: "failed", startedAt: T + 70000, finishedAt: T + 160000, detail: "Healthcheck failure" },
      { name: "cutover", state: "pending" },
    ]);
    expect(g.map((x) => x.label)).toEqual(["Initialization", "Build", "Deploy", "Network › Healthcheck", "Post-deploy"]);
    expect(g[3]).toMatchObject({ state: "failed", detail: "Healthcheck failure" });
    expect(g[4].state).toBe("pending");
    expect(mmss(g[1].ms!)).toBe("01:00");
  });
  it("hides the network row without a healthcheck", () => {
    expect(groupSteps([{ name: "build", state: "ok" }]).some((x) => x.key === "network")).toBe(false);
  });
  it("says where a deploy failed", () => {
    expect(outcomeText({ status: "FAILED", active: false, steps: [{ name: "build", state: "failed" }] })).toBe("Deployment failed during the build process");
    expect(outcomeText({ status: "SUCCESS", active: true, steps: [] })).toBe("Deployment successful");
  });
  it("formats durations", () => {
    expect(shortDuration(985)).toBe("985ms");
    expect(shortDuration(34000)).toBe("34s");
    expect(shortDuration(125000)).toBe("2m 5s");
  });
  it("parses and applies filters", () => {
    const t = parseQuery('@level:error -timeout "two words"');
    expect(t).toEqual([{ neg: false, key: "level", value: "error" }, { neg: true, key: undefined, value: "timeout" }, { neg: false, key: undefined, value: "two words" }]);
    expect(matches(line("two words here", { level: "error" }), t)).toBe(true);
    expect(matches(line("two words timeout", { level: "error" }), t)).toBe(false);
    expect(matches(line("x", { attrs: { httpStatus: 404 } }), parseQuery("@httpStatus:404"))).toBe(true);
    expect(matches(line("x", { attrs: { path: "/api/users/1" } }), parseQuery("@path:/api/users"))).toBe(true);
  });
});
