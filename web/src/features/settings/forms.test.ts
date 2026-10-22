// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it } from "vitest";
import type * as T from "../../api/types.gen";
import {
  backupsBody, checkAlerts, checkBackups, checkEnvName, checkGeneral, checkInvite, checkPassword, checkRegistry, checkSmtp,
  checkToken, checkWebhook, CRITICAL_EVENTS, generalFromInstall, generalPatch, hasErrors, inviteBody, isCIDR, smtpBody,
  splitList, usageRange, webhookBody,
} from "./forms";

const install = {
  name: "home", version: "v0.1.0", consoleUrl: "https://console.example.com", baseDomain: "apps.example.com", publicIp: "203.0.113.7",
  acmeEmail: "ops@example.com", trustedProxies: ["10.0.0.0/8"], registryCapPct: 30, buildTimeoutMin: 30, railwayAliases: true,
  requireTotp: false, githubApp: false, backupsReady: false, smtpReady: false, readOnly: false,
} as T.Install;

describe("install general form", () => {
  it("reads the console domain from the URL", () => {
    expect(generalFromInstall(install).consoleDomain).toBe("console.example.com");
  });
  it("accepts a valid form and sends only changed fields", () => {
    const before = generalFromInstall(install);
    const f = { ...before, name: "lab", trustedProxies: "10.0.0.0/8, 2001:db8::/32" };
    expect(hasErrors(checkGeneral(f))).toBe(false);
    expect(generalPatch(f, before)).toEqual({ name: "lab", trustedProxies: ["10.0.0.0/8", "2001:db8::/32"] });
  });
  it("rejects bad domains, emails and ranges", () => {
    const e = checkGeneral({ name: " ", baseDomain: "nodot", consoleDomain: "x y", acmeEmail: "nope", trustedProxies: "10.0.0.0/40\n1.2.3.4" });
    expect(Object.keys(e).sort()).toEqual(["acmeEmail", "baseDomain", "consoleDomain", "name", "trustedProxies"]);
    expect(e.trustedProxies).toBe("Not an IP range: 10.0.0.0/40.");
  });
  it("checks CIDRs", () => {
    expect(isCIDR("192.168.1.0/24")).toBe(true);
    expect(isCIDR("256.1.1.1")).toBe(false);
    expect(isCIDR("::1/128")).toBe(true);
    expect(isCIDR("1.2.3.4/x")).toBe(false);
  });
  it("splits lists", () => expect(splitList("a, b\n\nb c")).toEqual(["a", "b", "c"]));
});

describe("smtp form", () => {
  const s: T.SMTPSettings = { host: "smtp.example.com", port: 587, tlsMode: "starttls", username: "u", password: "", from: "Ops <ops@example.com>" };
  it("accepts a named from address", () => expect(hasErrors(checkSmtp(s))).toBe(false));
  it("rejects a bad port and from", () => expect(Object.keys(checkSmtp({ ...s, port: 70000, from: "x" })).sort()).toEqual(["from", "port"]));
  it("leaves an empty password out so the stored one is kept", () => {
    expect(smtpBody(s)).not.toHaveProperty("password");
    expect(smtpBody({ ...s, password: "pw" }).password).toBe("pw");
  });
});

describe("alerts form", () => {
  it("names the bad addresses", () => expect(checkAlerts("a@example.com, bad", "").emails).toBe("Not an email address: bad."));
  it("wants an http URL", () => expect(checkAlerts("", "ftp://x").webhookUrl).toBeTruthy());
});

describe("registry form", () => {
  it("accepts ghcr.io", () => expect(hasErrors(checkRegistry({ name: "gh", server: "ghcr.io", username: "me", password: "t" }))).toBe(false));
  it("needs every field", () => expect(Object.keys(checkRegistry({ name: "", server: "", username: "", password: "" })).length).toBe(4));
});

describe("backups form", () => {
  const b: T.BackupSettings = { endpoint: "https://s3.example.com", bucket: "my-backups", region: "", prefix: "", accessKeyId: "AK", secretAccessKey: "", pathStyle: false, stateBackups: true, storeImages: false };
  it("needs a secret only on first save", () => {
    expect(checkBackups(b, false).secretAccessKey).toBeTruthy();
    expect(checkBackups(b, true).secretAccessKey).toBeUndefined();
  });
  it("drops empty secrets and read-only fields", () => {
    const body = backupsBody({ ...b, lastStateBackup: 1, litestreamLagMs: 2 });
    expect(body).not.toHaveProperty("secretAccessKey");
    expect(body).not.toHaveProperty("lastStateBackup");
  });
  it("rejects bucket names with capitals", () => expect(checkBackups({ ...b, bucket: "Bad" }, true).bucket).toBeTruthy());
});

describe("invite form", () => {
  it("builds a link invite with defaults", () => {
    const f = { role: "member", email: "", projectId: "", maxUses: "", expiresInHours: "" };
    expect(hasErrors(checkInvite(f))).toBe(false);
    expect(inviteBody(f)).toEqual({ role: "member", maxUses: 1, expiresInHours: 168 });
  });
  it("refuses the owner role and silly limits", () => {
    expect(Object.keys(checkInvite({ role: "owner", email: "x", projectId: "", maxUses: "0", expiresInHours: "99999" })).sort()).toEqual(["email", "expiresInHours", "maxUses", "role"]);
  });
});

describe("token, password, environment and webhook forms", () => {
  it("needs an environment for an environment token", () => expect(checkToken("ci", "environment", "", "0").environmentId).toBeTruthy());
  it("checks passwords", () => {
    expect(checkPassword("", "short", "other")).toEqual({ current: expect.any(String), next: expect.any(String), again: expect.any(String) });
    expect(hasErrors(checkPassword("old-password", "new-password-1", "new-password-1"))).toBe(false);
  });
  it("checks environment names", () => {
    expect(checkEnvName("staging", ["production"])).toBeNull();
    expect(checkEnvName("Production", ["production"])).toBeTruthy();
    expect(checkEnvName("has space", [])).toBeTruthy();
  });
  it("marks the critical preset", () => {
    expect(hasErrors(checkWebhook("https://example.com/h", CRITICAL_EVENTS))).toBe(false);
    expect(webhookBody("https://example.com/h", [...CRITICAL_EVENTS]).preset).toBe("critical");
    expect(webhookBody("https://example.com/h", ["deployment.success"]).preset).toBeUndefined();
    expect(checkWebhook("x", []).events).toBeTruthy();
  });
});

describe("usage periods", () => {
  const now = new Date(2026, 9, 22, 12, 0, 0);
  it("this month starts on the first", () => expect(usageRange("month", now)).toEqual({ from: new Date(2026, 9, 1).getTime(), until: now.getTime() }));
  it("last month is the whole month", () => expect(usageRange("last-month", now)).toEqual({ from: new Date(2026, 8, 1).getTime(), until: new Date(2026, 9, 1).getTime() }));
  it("last 30 days", () => expect(usageRange("30d", now).until - usageRange("30d", now).from).toBe(30 * 86400_000));
});
