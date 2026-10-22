// SPDX-License-Identifier: AGPL-3.0-only

import { afterAll, beforeAll, describe, expect, it } from "vitest";
import { handlers, routes, wsHandlers } from "./handlers.gen";
import { server } from "./node";

const base = "http://localhost";

describe("generated MSW mocks", () => {
  beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
  afterAll(() => server.close());

  it("has one handler per section 11 route", () => {
    const ws = routes.filter((r) => r.stream === "ws").length;
    expect(handlers.length).toBe(routes.length - ws);
    expect(wsHandlers.length).toBe(ws);
  });

  it("answers every HTTP route with the documented status", async () => {
    for (const r of routes) {
      if (r.stream === "ws") continue;
      const path = r.path.replace(/\{[a-z]+\}/g, "x");
      const res = await fetch(base + path, { method: r.method, redirect: "manual" });
      expect([200, 204, 302], `${r.method} ${r.path}`).toContain(res.status);
    }
  }, 60_000);

  it("returns typed JSON fixtures", async () => {
    const res = await fetch(`${base}/api/v1/setup`);
    expect(await res.json()).toEqual({ needsSetup: true, version: "v0.1.0" });
  });
});
