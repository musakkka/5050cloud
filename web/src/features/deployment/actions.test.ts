// SPDX-License-Identifier: AGPL-3.0-only
import { describe, expect, it } from "vitest";
import type { Deployment } from "../../api/types.gen";
import { actionsFor, defaultTab } from "./actions";

const dep = (p: Partial<Deployment>): Deployment => ({
  id: "d1", serviceId: "s", environmentId: "e", status: "SUCCESS", label: "Active", active: false, trigger: "ui",
  steps: [], canRollback: true, restarts: 0, createdAt: 0, ...p,
});

describe("deployment actions", () => {
  it("offers abort while building", () => expect(actionsFor(dep({ status: "BUILDING" }))).toEqual(["abort"]));
  it("active can restart, redeploy, remove", () => expect(actionsFor(dep({ active: true }))).toEqual(["restart", "redeploy", "remove"]));
  it("an old success can roll back", () => expect(actionsFor(dep({}))).toEqual(["redeploy", "rollback"]));
  it("no rollback when the image is gone", () => expect(actionsFor(dep({ canRollback: false }))).toEqual(["redeploy"]));
  it("crashed active offers restart first", () => expect(actionsFor(dep({ status: "CRASHED", active: true }))[0]).toBe("restart"));
  it("opens build logs for a failed build and deploy logs for a crash", () => {
    expect(defaultTab(dep({ status: "FAILED", steps: [{ name: "build", state: "failed" }] }))).toBe("build");
    expect(defaultTab(dep({ status: "FAILED", steps: [{ name: "build", state: "ok" }, { name: "healthcheck", state: "failed" }] }))).toBe("deploy");
    expect(defaultTab(dep({ status: "CRASHED" }))).toBe("deploy");
  });
});
