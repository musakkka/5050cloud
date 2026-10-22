// SPDX-License-Identifier: AGPL-3.0-only

import { http, HttpResponse } from "msw";
import { afterAll, afterEach, beforeAll, describe, expect, it } from "vitest";
import { server } from "../../api/mocks/node";
import { toggleRole } from "../hosts/HostsSettings";
import { lagText } from "../backups-settings/BackupsSettings";
import { fullPrefs } from "../account/AccountPage";
import { ApiError, config, v1 } from "./client";

describe("settings client", () => {
  beforeAll(() => { config.base = "http://localhost"; server.listen({ onUnhandledRequest: "error" }); });
  afterEach(() => server.resetHandlers());
  afterAll(() => server.close());

  it("reads typed fixtures from /api/v1", async () => {
    const i = await v1.install();
    expect(typeof i.baseDomain).toBe("string");
    expect(Array.isArray(await v1.hosts())).toBe(true);
    expect(Array.isArray((await v1.audit()).items)).toBe(true);
  });

  it("sends the CSRF header and JSON body on mutations", async () => {
    let seen: { csrf: string | null; body: unknown } | null = null;
    server.use(http.put("*/api/v1/settings/smtp", async ({ request }) => {
      seen = { csrf: request.headers.get("x-5050-csrf"), body: await request.json() };
      return HttpResponse.json(seen.body as Record<string, unknown>);
    }));
    await v1.setSmtp({ host: "smtp.example.com", port: 587, tlsMode: "tls", username: "", from: "a@example.com" });
    expect(seen).toEqual({ csrf: "1", body: { host: "smtp.example.com", port: 587, tlsMode: "tls", username: "", from: "a@example.com" } });
  });

  it("turns api.Error into an ApiError with the sentence", async () => {
    server.use(http.patch("*/api/v1/install", () => HttpResponse.json({ error: "The base domain does not resolve to this install.", code: "validation", field: "baseDomain" }, { status: 400 })));
    const err = await v1.updateInstall({ baseDomain: "x.example.com" }).catch((e) => e);
    expect(err).toBeInstanceOf(ApiError);
    expect(err.message).toBe("The base domain does not resolve to this install.");
    expect(err.field).toBe("baseDomain");
  });

  it("handles 204 responses", async () => {
    server.use(http.delete("*/api/v1/tokens/:id", () => new HttpResponse(null, { status: 204 })));
    await expect(v1.deleteToken("tok_1")).resolves.toBeUndefined();
  });

  it("sends the typed confirm on deletes", async () => {
    let body: unknown;
    server.use(http.delete("*/api/v1/projects/:pid", async ({ request }) => { body = await request.json(); return new HttpResponse(null, { status: 204 }); }));
    await v1.deleteProject("prj_1", "shop");
    expect(body).toEqual({ confirm: "shop" });
  });
});

describe("small helpers", () => {
  it("keeps roles in a fixed order", () => expect(toggleRole(["app"], "build", true)).toEqual(["build", "app"]));
  it("removes roles", () => expect(toggleRole(["build", "app"], "app", false)).toEqual(["build"]));
  it("describes Litestream lag", () => {
    expect(lagText(undefined)).toBe("Not streaming");
    expect(lagText(200)).toBe("Up to date");
    expect(lagText(30_000)).toBe("30 seconds behind");
  });
  it("fills missing notification prefs", () => {
    const p = fullPrefs([{ event: "deployment.failed", email: true, inApp: false }]);
    expect(p).toHaveLength(7);
    expect(p[0]).toEqual({ event: "deployment.failed", email: true, inApp: false });
  });
});
