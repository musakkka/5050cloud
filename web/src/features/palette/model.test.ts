// SPDX-License-Identifier: AGPL-3.0-only
import { describe, expect, it } from "vitest";
import { back, push, rank, redeployLabel, score, type Cmd, type Level } from "./model";

const c = (title: string, extra: Partial<Cmd> = {}): Cmd => ({ id: title, title, ...extra });

describe("ranking", () => {
  it("orders exact, prefix, word prefix, substring, then in-order letters", () => {
    const list = [c("Open shell"), c("Redeploy"), c("Deploy latest commit"), c("Toggle theme"), c("Import variables from Heroku")];
    expect(rank(list, "deploy").map((x) => x.title)).toEqual(["Deploy latest commit", "Redeploy"]);
    expect(rank(list, "theme")[0].title).toBe("Toggle theme");
    expect(rank(list, "rdp").map((x) => x.title)).toContain("Redeploy");
    expect(rank(list, "zzz")).toEqual([]);
  });
  it("keeps the given order with no query and for ties", () => {
    const list = [c("B one"), c("A one")];
    expect(rank(list, "")).toEqual(list);
    expect(rank(list, "one").map((x) => x.title)).toEqual(["B one", "A one"]);
  });
  it("matches keywords below titles", () => {
    const list = [c("Toggle theme", { keywords: ["dark", "light"] }), c("Dark mode docs")];
    expect(rank(list, "dark")[0].title).toBe("Dark mode docs");
    expect(rank(list, "light")[0].title).toBe("Toggle theme");
  });
  it("scores empty queries as a match", () => expect(score("x", "")).toBe(1));
});

describe("sub-menus", () => {
  it("pushes and goes back, never past the root", () => {
    let s: Level[] = [{ items: [c("Go to project")] }];
    s = push(s, c("Go to project"), [c("api"), c("web")]);
    expect(s).toHaveLength(2);
    expect(s[1].title).toBe("Go to project");
    s = back(s);
    expect(s).toHaveLength(1);
    expect(back(s)).toHaveLength(1);
  });
});

describe("adaptive Redeploy label", () => {
  const svc = (type: string | undefined, active?: string) => ({ activeDeployment: active, config: { source: { type } } } as never);
  it("follows the source and hides without a deployment", () => {
    expect(redeployLabel(svc("image", "d1"))).toBe("Redeploy service image");
    expect(redeployLabel(svc("empty", "d1"))).toBe("Redeploy latest deployment");
    expect(redeployLabel(svc("github", "d1"))).toBe("Redeploy");
    expect(redeployLabel(svc("github"))).toBeNull();
  });
});
