// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it } from "vitest";
import type { EnvSummary, Project } from "../../api/client";
import { arrange, onlineText, primarySummary, sortProjects, summaryTone, tileLayout, tilesOf, toggleIn } from "./model";
import { hostIssue } from "./Banners";

const env = (id: string, name: string) => ({ id, projectId: "p", name, egress: "allowed", createdAt: 0 });
const proj = (id: string, name: string, created: number, activity: number, summary?: EnvSummary[]): Project => ({
  id, name, description: "", publicRead: false, createdAt: created, lastActivityAt: activity,
  environments: [env(`${id}-stg`, "staging"), env(`${id}-prod`, "production")], summary,
});

describe("dashboard model", () => {
  const a = proj("a", "beta", 1, 30), b = proj("b", "Alpha", 3, 10), c = proj("c", "gamma", 2, 20);

  it("sorts by activity, creation date and name", () => {
    expect(sortProjects([a, b, c], "activity").map((p) => p.id)).toEqual(["a", "c", "b"]);
    expect(sortProjects([a, b, c], "created").map((p) => p.id)).toEqual(["b", "c", "a"]);
    expect(sortProjects([a, b, c], "name").map((p) => p.id)).toEqual(["b", "a", "c"]);
  });

  it("puts favourites first and keeps order otherwise", () => {
    expect(arrange([a, b, c], ["c"]).map((p) => p.id)).toEqual(["c", "a", "b"]);
    expect(arrange([a, b, c], [], "alp").map((p) => p.id)).toEqual(["b"]);
    expect(toggleIn(["x"], "x")).toEqual([]);
    expect(toggleIn([], "x")).toEqual(["x"]);
  });

  it("summarises the production environment", () => {
    const s: EnvSummary[] = [
      { environmentId: "a-stg", total: 1, online: 0, icons: [] },
      { environmentId: "a-prod", total: 2, online: 2, icons: [] },
    ];
    expect(primarySummary(proj("a", "x", 0, 0, s))?.environmentId).toBe("a-prod");
    expect(onlineText(s[1])).toBe("2/2 services online");
    expect(onlineText({ environmentId: "e", total: 1, online: 1, icons: [] })).toBe("1/1 service online");
    expect(onlineText(undefined)).toBe("No services");
  });

  it("picks a health tone", () => {
    const mini = (status: string) => ({ serviceId: status, icon: "", status, position: [0, 0] });
    expect(summaryTone({ environmentId: "e", total: 2, online: 2, icons: [] })).toBe("ok");
    expect(summaryTone({ environmentId: "e", total: 2, online: 1, icons: [] })).toBe("warn");
    expect(summaryTone({ environmentId: "e", total: 1, online: 0, icons: [], mini: [mini("crashed")] })).toBe("bad");
    expect(summaryTone({ environmentId: "e", total: 0, online: 0, icons: [] })).toBe("idle");
  });

  it("orders tiles by canvas position and keeps the source kind", () => {
    const s: EnvSummary = {
      environmentId: "e", total: 2, online: 2,
      icons: [{ icon: "", kind: "repo", source: "repo" }, { icon: "postgresql", kind: "image", source: "image" }],
      mini: [
        { serviceId: "web", icon: "", status: "online", position: [400, 0] },
        { serviceId: "db", icon: "postgresql", status: "online", position: [0, 0] },
      ],
    };
    expect(tilesOf(s).map((t) => [t.serviceId, t.source])).toEqual([["db", "image"], ["web", "repo"]]);
  });

  it("caps the tile block", () => {
    expect(tileLayout(1)).toEqual({ shown: 1, more: 0, cols: 1 });
    expect(tileLayout(7)).toEqual({ shown: 7, more: 0, cols: 5 });
    expect(tileLayout(42)).toEqual({ shown: 14, more: 28, cols: 5 });
  });

  it("reports the first host problem", () => {
    const h = (id: string, state: string, used = 1, total = 10) => ({ id, name: id, state, disks: [{ mount: "/", used, total }] }) as unknown as import("../../api/client").Host;
    expect(hostIssue([h("a", "online")])).toBeNull();
    expect(hostIssue([h("a", "online", 95, 100)])?.text).toBe("Disk / on a is 95% full.");
    expect(hostIssue([h("a", "online", 95, 100), h("b", "offline")])?.text).toBe("Host b is offline.");
  });
});
