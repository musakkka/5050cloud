// SPDX-License-Identifier: AGPL-3.0-only
import { describe, expect, it } from "vitest";
import { imageError, imageName, imageRegistry, repoRef } from "./image";

describe("image names", () => {
  it("accepts common references", () => {
    for (const r of ["nginx", "nginx:1.27", "traefik/whoami:v1.10.1", "ghcr.io/owner/app:latest", "localhost:5000/x", "redis@sha256:" + "a".repeat(64)]) expect(imageError(r)).toBe("");
  });
  it("rejects bad ones with a sentence", () => {
    expect(imageError("")).toMatch(/Enter/);
    expect(imageError("my image")).toMatch(/spaces/);
    expect(imageError("Nginx")).toMatch(/lowercase/);
    expect(imageError("nginx::1")).not.toBe("");
  });
  it("derives registry and name", () => {
    expect(imageRegistry("ghcr.io/o/r:1")).toBe("ghcr.io");
    expect(imageRegistry("traefik/whoami")).toBe("docker.io");
    expect(imageName("ghcr.io/o/web-app:1")).toBe("web-app");
  });
  it("normalises repo URLs", () => {
    expect(repoRef("https://github.com/a/b.git")).toBe("https://github.com/a/b");
    expect(repoRef("github.com/a/b/tree/main")).toBe("https://github.com/a/b");
    expect(repoRef("a/b")).toBe("a/b");
  });
});
