// SPDX-License-Identifier: AGPL-3.0-only
import { describe, expect, it } from "vitest";
import { getPath, isStaged, normaliseRepo, patchFor, validImage } from "./patch";

describe("settings patches", () => {
  it("builds nested merge patches", () => {
    expect(patchFor("deploy.startCommand", "npm start")).toEqual({ deploy: { startCommand: "npm start" } });
    expect(patchFor("deploy.placement.roles", [])).toEqual({ deploy: { placement: { roles: null } } });
    expect(patchFor("deploy.replicas", "")).toEqual({ deploy: { replicas: null } });
  });
  it("reads paths and detects staged fields", () => {
    const a = { deploy: { replicas: 1 } };
    const b = { deploy: { replicas: 2 } };
    expect(getPath(a, "deploy.replicas")).toBe(1);
    expect(isStaged(a, b, "deploy.replicas")).toBe(true);
    expect(isStaged(a, undefined, "deploy.replicas")).toBe(false);
    expect(isStaged({ x: "" }, { x: undefined }, "x")).toBe(false);
  });
  it("validates images", () => {
    for (const ok of ["nginx", "nginx:1.27", "ghcr.io/o/r:tag", "localhost:5000/app", "traefik/whoami:v1.10.1"]) expect(validImage(ok)).toBeNull();
    for (const bad of ["", "Nginx", "a b", "nginx:", "ghcr.io//x"]) expect(validImage(bad)).not.toBeNull();
  });
  it("normalises repos", () => {
    expect(normaliseRepo("owner/app")).toEqual({ repo: "owner/app" });
    expect(normaliseRepo("https://github.com/owner/app.git")).toEqual({ repo: "https://github.com/owner/app" });
    expect(normaliseRepo("nope").error).toBeTruthy();
  });
});
