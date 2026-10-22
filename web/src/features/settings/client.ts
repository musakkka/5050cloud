// SPDX-License-Identifier: AGPL-3.0-only

// Typed calls for the settings, hosts, members, account and usage screens
// (docs/SPEC.md 11.1 to 11.5). Every mutating request sends X-5050-CSRF: 1.
import type * as T from "../../api/types.gen";

export class ApiError extends Error {
  status: number;
  code: string;
  field?: string;
  constructor(status: number, body: Partial<T.Error>) {
    super(body.error || `Request failed with status ${status}.`);
    this.status = status;
    this.code = body.code || "internal";
    this.field = body.field;
  }
}

/** base is "" in the browser; tests set it to an absolute origin. */
export const config = { base: "" };

export async function call<R>(method: string, path: string, body?: unknown, raw = false): Promise<R> {
  const headers: Record<string, string> = {};
  if (method !== "GET") headers["X-5050-CSRF"] = "1";
  if (body !== undefined) headers["content-type"] = "application/json";
  const r = await fetch(`${config.base}/api/v1${path}`, {
    method,
    credentials: "include",
    headers,
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (!r.ok) {
    let parsed: Partial<T.Error> = {};
    try { parsed = await r.json(); } catch { /* not JSON */ }
    throw new ApiError(r.status, parsed);
  }
  if (r.status === 204) return undefined as R;
  if (raw) return (await r.text()) as R;
  const text = await r.text();
  return (text ? JSON.parse(text) : undefined) as R;
}

const e = encodeURIComponent;
const get = <R>(p: string) => call<R>("GET", p);
const post = <R>(p: string, b?: unknown) => call<R>("POST", p, b ?? {});
const put = <R>(p: string, b: unknown) => call<R>("PUT", p, b);
const patch = <R>(p: string, b: unknown) => call<R>("PATCH", p, b);
const del = <R = void>(p: string, b?: unknown) => call<R>("DELETE", p, b);

export const v1 = {
  // account
  me: () => get<T.Me>("/me"),
  updateMe: (b: T.UpdateMeRequest) => patch<T.User>("/me", b),
  totpSetup: () => post<T.TOTPSetupResponse>("/me/2fa/setup"),
  totpEnable: (b: T.TOTPEnableRequest) => post<T.TOTPEnableResponse>("/me/2fa/enable", b),
  totpDisable: (b: T.TOTPDisableRequest) => post<void>("/me/2fa/disable", b),
  notificationPrefs: () => get<T.NotificationPref[]>("/me/notification-prefs"),
  setNotificationPrefs: (b: T.NotificationPref[]) => put<T.NotificationPref[]>("/me/notification-prefs", b),
  logout: () => post<void>("/auth/logout"),

  // tokens
  tokens: () => get<T.APIToken[]>("/tokens"),
  createToken: (b: T.CreateTokenRequest) => post<T.CreateTokenResponse>("/tokens", b),
  deleteToken: (id: string) => del(`/tokens/${e(id)}`),

  // members and invites
  members: () => get<T.User[]>("/members"),
  updateMember: (id: string, b: T.UpdateMemberRequest) => patch<T.User>(`/members/${e(id)}`, b),
  removeMember: (id: string) => del(`/members/${e(id)}`),
  transferOwnership: (id: string, b: T.TransferOwnershipRequest) => post<void>(`/members/${e(id)}/transfer-ownership`, b),
  invites: () => get<T.Invite[]>("/invites"),
  createInvite: (b: T.CreateInviteRequest) => post<T.Invite>("/invites", b),
  deleteInvite: (id: string) => del(`/invites/${e(id)}`),
  audit: (before?: string, limit = 50) => get<T.Page<T.AuditEntry>>(`/audit?limit=${limit}${before ? `&before=${e(before)}` : ""}`),

  // install
  install: () => get<T.Install>("/install"),
  updateInstall: (b: T.UpdateInstallRequest) => patch<T.Install>("/install", b),
  backups: () => get<T.BackupSettings>("/settings/backups"),
  setBackups: (b: T.BackupSettings) => put<T.BackupSettings>("/settings/backups", b),
  testBackups: () => post<T.TestResult>("/settings/backups/test"),
  smtp: () => get<T.SMTPSettings>("/settings/smtp"),
  setSmtp: (b: T.SMTPSettings) => put<T.SMTPSettings>("/settings/smtp", b),
  testSmtp: (b: T.SMTPTestRequest) => post<T.TestResult>("/settings/smtp/test", b),
  alerts: () => get<T.InstallNotifications>("/settings/alerts"),
  setAlerts: (b: T.InstallNotifications) => put<T.InstallNotifications>("/settings/alerts", b),
  recoveryKey: (b: T.RecoveryKeyRequest) => post<T.RecoveryKeyResponse>("/settings/recovery-key", b),
  upgradeStatus: () => get<T.UpgradeStatus>("/upgrade"),
  upgrade: (b: T.UpgradeRequest) => post<T.UpgradeStatus>("/upgrade", b),
  github: () => get<T.GitHubStatus>("/github"),
  githubApp: (b: T.GitHubAppRequest) => post<T.GitHubAppResponse>("/github/app", b),

  // hosts
  hosts: () => get<T.Host[]>("/hosts"),
  joinToken: (b: T.CreateJoinTokenRequest) => post<T.JoinToken>("/hosts/join-tokens", b),
  updateHost: (id: string, b: T.UpdateHostRequest) => patch<T.Host>(`/hosts/${e(id)}`, b),
  maintenance: (id: string, on: boolean) => post<T.Host>(`/hosts/${e(id)}/maintenance`, { on }),
  readopt: (id: string) => post<T.Host>(`/hosts/${e(id)}/readopt`),
  clearConflicts: (id: string) => del<T.Host>(`/hosts/${e(id)}/conflicts`),
  removeHost: (id: string, force = false) => del(`/hosts/${e(id)}${force ? "?force=1" : ""}`),

  // registry credentials
  registryCredentials: () => get<T.RegistryCredential[]>("/registry-credentials"),
  addRegistryCredential: (b: T.CreateRegistryCredentialRequest) => post<T.RegistryCredential>("/registry-credentials", b),
  deleteRegistryCredential: (id: string) => del(`/registry-credentials/${e(id)}`),

  // projects
  project: (pid: string) => get<T.Project>(`/projects/${e(pid)}`),
  updateProject: (pid: string, b: T.UpdateProjectRequest) => patch<T.Project>(`/projects/${e(pid)}`, b),
  deleteProject: (pid: string, confirm: string) => del(`/projects/${e(pid)}`, { confirm }),
  projectMembers: (pid: string) => get<T.ProjectGrant[]>(`/projects/${e(pid)}/members`),
  setProjectMembers: (pid: string, b: T.ProjectGrant[]) => put<T.ProjectGrant[]>(`/projects/${e(pid)}/members`, b),
  usage: (pid: string, from: number, until: number) => get<T.UsageResponse>(`/projects/${e(pid)}/usage?from=${from}&until=${until}`),
  createEnvironment: (pid: string, b: T.CreateEnvironmentRequest) => post<T.Environment>(`/projects/${e(pid)}/environments`, b),
  updateEnvironment: (eid: string, b: T.UpdateEnvironmentRequest) => patch<T.Environment>(`/environments/${e(eid)}`, b),
  deleteEnvironment: (eid: string, confirm: string) => del(`/environments/${e(eid)}`, { confirm }),
  canvas: (eid: string) => get<T.Canvas>(`/environments/${e(eid)}/canvas`),
  sharedVariables: (eid: string) => get<T.VariablesResponse>(`/environments/${e(eid)}/shared-variables`),
  setSharedVariables: (eid: string, b: T.SetVariablesRequest) => put<T.VariablesResponse>(`/environments/${e(eid)}/shared-variables`, b),
  revealSharedVariables: (eid: string, b: T.RevealRequest) => post<T.RevealResponse>(`/environments/${e(eid)}/shared-variables/reveal`, b),
  deleteService: (eid: string, sid: string, confirm: string) => del(`/environments/${e(eid)}/services/${e(sid)}`, { confirm }),

  // webhooks
  webhooks: (pid: string) => get<T.Webhook[]>(`/projects/${e(pid)}/webhooks`),
  createWebhook: (pid: string, b: Partial<T.Webhook>) => post<T.Webhook>(`/projects/${e(pid)}/webhooks`, b),
  updateWebhook: (id: string, b: Partial<T.Webhook>) => patch<T.Webhook>(`/webhooks/${e(id)}`, b),
  deleteWebhook: (id: string) => del(`/webhooks/${e(id)}`),
  testWebhook: (id: string) => post<T.WebhookTestResponse>(`/webhooks/${e(id)}/test`),
};
