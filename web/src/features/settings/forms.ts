// SPDX-License-Identifier: AGPL-3.0-only

// Pure form logic for the settings screens: parse what people typed, check
// it, and build the request body. Each check returns one sentence or null.
import type * as T from "../../api/types.gen";

export type Errors<K extends string = string> = Partial<Record<K, string>>;

const EMAIL = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;
const HOSTNAME = /^(?=.{1,253}$)([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+$/i;

export const isEmail = (s: string) => EMAIL.test(s.trim());
export const isHostname = (s: string) => HOSTNAME.test(s.trim());

/** Splits a textarea or comma list into trimmed, non-empty, unique items. */
export function splitList(s: string): string[] {
  return [...new Set(s.split(/[\s,]+/).map((x) => x.trim()).filter(Boolean))];
}

export function isCIDR(s: string): boolean {
  const [addr, bits, extra] = s.split("/");
  if (extra !== undefined || addr === undefined) return false;
  if (addr.includes(":")) {
    const n = bits === undefined ? 128 : Number(bits);
    return /^[0-9a-f:]+$/i.test(addr) && addr.split("::").length <= 2 && Number.isInteger(n) && n >= 0 && n <= 128 && (bits === undefined || /^\d+$/.test(bits));
  }
  const parts = addr.split(".");
  if (parts.length !== 4 || !parts.every((p) => /^\d{1,3}$/.test(p) && Number(p) <= 255)) return false;
  if (bits === undefined) return true;
  return /^\d+$/.test(bits) && Number(bits) <= 32;
}

export function isHttpUrl(s: string): boolean {
  try { const u = new URL(s); return u.protocol === "http:" || u.protocol === "https:"; } catch { return false; }
}

// Install > General
export interface GeneralForm { name: string; baseDomain: string; consoleDomain: string; acmeEmail: string; trustedProxies: string }

export function generalFromInstall(i: T.Install): GeneralForm {
  let consoleDomain = "";
  try { consoleDomain = new URL(i.consoleUrl).host; } catch { consoleDomain = i.consoleUrl; }
  return { name: i.name, baseDomain: i.baseDomain, consoleDomain, acmeEmail: i.acmeEmail || "", trustedProxies: i.trustedProxies.join("\n") };
}

export function checkGeneral(f: GeneralForm): Errors<keyof GeneralForm> {
  const e: Errors<keyof GeneralForm> = {};
  if (!f.name.trim()) e.name = "Enter a name for this install.";
  if (!isHostname(f.baseDomain)) e.baseDomain = "Enter a domain such as apps.example.com.";
  if (!isHostname(f.consoleDomain)) e.consoleDomain = "Enter a domain such as console.example.com.";
  if (f.acmeEmail.trim() && !isEmail(f.acmeEmail)) e.acmeEmail = "Enter a valid email address.";
  const bad = splitList(f.trustedProxies).filter((c) => !isCIDR(c));
  if (bad.length) e.trustedProxies = `Not an IP range: ${bad.join(", ")}.`;
  return e;
}

/** Only the fields that changed, so an unchanged console domain is never re-sent. */
export function generalPatch(f: GeneralForm, before: GeneralForm): T.UpdateInstallRequest {
  const p: T.UpdateInstallRequest = {};
  if (f.name.trim() !== before.name) p.name = f.name.trim();
  if (f.baseDomain.trim() !== before.baseDomain) p.baseDomain = f.baseDomain.trim();
  if (f.consoleDomain.trim() !== before.consoleDomain) p.consoleDomain = f.consoleDomain.trim();
  if (f.acmeEmail.trim() !== before.acmeEmail) p.acmeEmail = f.acmeEmail.trim();
  const tp = splitList(f.trustedProxies);
  if (tp.join("\n") !== splitList(before.trustedProxies).join("\n")) p.trustedProxies = tp;
  return p;
}

// Install > General > Email
export function checkSmtp(s: T.SMTPSettings): Errors<keyof T.SMTPSettings> {
  const e: Errors<keyof T.SMTPSettings> = {};
  if (!isHostname(s.host) && !isCIDR(s.host)) e.host = "Enter the mail server host name.";
  if (!Number.isInteger(s.port) || s.port < 1 || s.port > 65535) e.port = "Enter a port between 1 and 65535.";
  if (!["starttls", "tls", "none"].includes(s.tlsMode)) e.tlsMode = "Choose a TLS mode.";
  const from = s.from.trim().match(/<([^>]+)>$/)?.[1] ?? s.from.trim();
  if (!isEmail(from)) e.from = "Enter the address mail is sent from.";
  return e;
}

/** An empty password means "keep the stored one", so it is left out. */
export function smtpBody(s: T.SMTPSettings): T.SMTPSettings {
  const b: T.SMTPSettings = { host: s.host.trim(), port: s.port, tlsMode: s.tlsMode, username: s.username.trim(), from: s.from.trim() };
  if (s.password) b.password = s.password;
  return b;
}

// Install > General > Alerts
export function checkAlerts(emails: string, webhookUrl: string): Errors<"emails" | "webhookUrl"> {
  const e: Errors<"emails" | "webhookUrl"> = {};
  const bad = splitList(emails).filter((x) => !isEmail(x));
  if (bad.length) e.emails = `Not an email address: ${bad.join(", ")}.`;
  if (webhookUrl.trim() && !isHttpUrl(webhookUrl.trim())) e.webhookUrl = "Enter a URL that starts with https://.";
  return e;
}

// Registry credentials
export function checkRegistry(c: T.CreateRegistryCredentialRequest): Errors<keyof T.CreateRegistryCredentialRequest> {
  const e: Errors<keyof T.CreateRegistryCredentialRequest> = {};
  if (!c.name.trim()) e.name = "Enter a name.";
  const server = c.server.trim().replace(/^https?:\/\//, "").replace(/\/$/, "");
  if (!/^[a-z0-9.-]+(:\d+)?$/i.test(server) || !server.includes(".") && !server.startsWith("localhost")) e.server = "Enter a registry such as ghcr.io.";
  if (!c.username.trim()) e.username = "Enter the user name.";
  if (!c.password) e.password = "Enter the password or access token.";
  return e;
}

// Backups
export function checkBackups(b: T.BackupSettings, hasStoredSecret: boolean): Errors<keyof T.BackupSettings> {
  const e: Errors<keyof T.BackupSettings> = {};
  if (!isHttpUrl(b.endpoint)) e.endpoint = "Enter the bucket endpoint, such as https://s3.us-east-1.amazonaws.com.";
  if (!/^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$/.test(b.bucket)) e.bucket = "Enter a bucket name (lower case letters, digits, dots and dashes).";
  if (!b.accessKeyId.trim()) e.accessKeyId = "Enter the write key id.";
  if (!b.secretAccessKey && !hasStoredSecret) e.secretAccessKey = "Enter the write key secret.";
  if (b.retentionAccessKeyId && !b.retentionSecretAccessKey && !hasStoredSecret) e.retentionSecretAccessKey = "Enter the retention key secret.";
  return e;
}

export function backupsBody(b: T.BackupSettings): T.BackupSettings {
  const out: T.BackupSettings = {
    endpoint: b.endpoint.trim(), bucket: b.bucket.trim(), region: b.region.trim(), prefix: b.prefix.trim(),
    accessKeyId: b.accessKeyId.trim(), pathStyle: b.pathStyle, stateBackups: b.stateBackups, storeImages: b.storeImages,
  };
  if (b.secretAccessKey) out.secretAccessKey = b.secretAccessKey;
  if (b.retentionAccessKeyId?.trim()) out.retentionAccessKeyId = b.retentionAccessKeyId.trim();
  if (b.retentionSecretAccessKey) out.retentionSecretAccessKey = b.retentionSecretAccessKey;
  return out;
}

// Members > Invite
export interface InviteForm { role: string; email: string; projectId: string; maxUses: string; expiresInHours: string }

export function checkInvite(f: InviteForm): Errors<keyof InviteForm> {
  const e: Errors<keyof InviteForm> = {};
  if (!["admin", "member", "viewer"].includes(f.role)) e.role = "Choose a role.";
  if (f.email.trim() && !isEmail(f.email)) e.email = "Enter a valid email address, or leave it empty for a link.";
  const uses = Number(f.maxUses || "1");
  if (!Number.isInteger(uses) || uses < 1 || uses > 1000) e.maxUses = "Enter a number of uses between 1 and 1000.";
  const hours = Number(f.expiresInHours || "168");
  if (!Number.isInteger(hours) || hours < 1 || hours > 24 * 90) e.expiresInHours = "Enter between 1 hour and 90 days.";
  return e;
}

export function inviteBody(f: InviteForm): T.CreateInviteRequest {
  const b: T.CreateInviteRequest = { role: f.role, maxUses: Number(f.maxUses || "1"), expiresInHours: Number(f.expiresInHours || "168") };
  if (f.email.trim()) b.email = f.email.trim();
  if (f.projectId) b.projectId = f.projectId;
  return b;
}

// Tokens
export function checkToken(name: string, scope: string, environmentId: string, days: string): Errors<"name" | "environmentId" | "days"> {
  const e: Errors<"name" | "environmentId" | "days"> = {};
  if (!name.trim()) e.name = "Enter a name so you can recognise the token later.";
  if (scope === "environment" && !environmentId) e.environmentId = "Choose an environment.";
  const d = Number(days || "0");
  if (!Number.isInteger(d) || d < 0 || d > 3650) e.days = "Enter a number of days, or 0 for never.";
  return e;
}

// Account
export function checkPassword(current: string, next: string, again: string): Errors<"current" | "next" | "again"> {
  const e: Errors<"current" | "next" | "again"> = {};
  if (!current) e.current = "Enter your current password.";
  if (next.length < 10) e.next = "Use at least 10 characters.";
  if (next !== again) e.again = "The passwords do not match.";
  return e;
}

export const isTotpCode = (s: string) => /^\d{6}$/.test(s.replace(/\s/g, "")) || /^[a-z0-9]{4,}-?[a-z0-9]{4,}$/i.test(s.trim());

// Webhooks
export const CRITICAL_EVENTS = ["deployment.crashed", "replica.oom_killed", "volume.high", "disk.high", "backup.failed", "monitor.triggered"];
export const WEBHOOK_EVENTS = ["deployment.success", "deployment.failed", "deployment.crashed", "replica.oom_killed", "service.unhealthy", "volume.high", "disk.high", "backup.failed", "monitor.triggered", "monitor.resolved", "host.offline"];

export function checkWebhook(url: string, events: string[]): Errors<"url" | "events"> {
  const e: Errors<"url" | "events"> = {};
  if (!isHttpUrl(url.trim())) e.url = "Enter a URL that starts with https://.";
  if (!events.length) e.events = "Choose at least one event.";
  return e;
}

export function webhookBody(url: string, events: string[]): Partial<T.Webhook> {
  const critical = events.length === CRITICAL_EVENTS.length && CRITICAL_EVENTS.every((x) => events.includes(x));
  return { url: url.trim(), events, enabled: true, ...(critical ? { preset: "critical" } : {}) };
}

// Environments
export function checkEnvName(name: string, taken: string[]): string | null {
  const n = name.trim();
  if (!/^[a-z0-9][a-z0-9-]{0,31}$/i.test(n)) return "Use up to 32 letters, digits and dashes.";
  if (taken.some((t) => t.toLowerCase() === n.toLowerCase())) return "An environment with that name exists.";
  return null;
}

// Usage periods (9.23): this month, last month, last 30 days, custom.
export function usageRange(period: string, now = new Date(), custom?: { from: string; until: string }): { from: number; until: number } {
  const y = now.getFullYear(), m = now.getMonth();
  if (period === "last-month") return { from: new Date(y, m - 1, 1).getTime(), until: new Date(y, m, 1).getTime() };
  if (period === "30d") return { from: now.getTime() - 30 * 86400_000, until: now.getTime() };
  if (period === "custom" && custom) return { from: new Date(custom.from).getTime(), until: new Date(custom.until).getTime() + 86400_000 };
  return { from: new Date(y, m, 1).getTime(), until: now.getTime() };
}

export const hasErrors = (e: object) => Object.values(e).some(Boolean);
