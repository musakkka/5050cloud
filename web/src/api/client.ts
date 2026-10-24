// SPDX-License-Identifier: AGPL-3.0-only

// Typed client for the 5050d REST API (docs/SPEC.md 11). Every call goes to
// /api/v1 with the session cookie; mutations carry the CSRF header.
import type * as T from "./types.gen";

export const BASE = "/api/v1";

/** A failed API call with the server's one-sentence message and code. */
export class ApiError extends Error {
  status: number;
  code: string;
  field?: string;
  constructor(status: number, body: Partial<T.Error>, fallback: string) {
    super(body.error || fallback);
    this.status = status;
    this.code = body.code || "internal";
    this.field = body.field;
  }
}

type Listener = (e: ApiError) => void;
const unauthorizedListeners = new Set<Listener>();
/** Called when any request answers 401, so the shell can send the user to /login. */
export function onUnauthorized(fn: Listener): () => void {
  unauthorizedListeners.add(fn);
  return () => unauthorizedListeners.delete(fn);
}

export interface ReqInit { method?: string; body?: unknown; raw?: BodyInit; contentType?: string; signal?: AbortSignal }

export async function request<R>(path: string, init: ReqInit = {}): Promise<R> {
  const method = init.method || (init.body !== undefined || init.raw !== undefined ? "POST" : "GET");
  const headers: Record<string, string> = { accept: "application/json" };
  if (method !== "GET" && method !== "HEAD") headers["X-5050-CSRF"] = "1";
  let body: BodyInit | undefined;
  if (init.raw !== undefined) { body = init.raw; headers["content-type"] = init.contentType || "text/plain"; }
  else if (init.body !== undefined) { body = JSON.stringify(init.body); headers["content-type"] = "application/json"; }
  let r: Response;
  try {
    r = await fetch(BASE + path, { method, headers, body, credentials: "same-origin", signal: init.signal });
  } catch (e) {
    if ((e as Error).name === "AbortError") throw e;
    throw new ApiError(0, { code: "unavailable" }, "Could not reach the server. Check your connection.");
  }
  if (r.status === 204) return undefined as R;
  const text = await r.text();
  let data: unknown = undefined;
  if (text) { try { data = JSON.parse(text); } catch { data = text; } }
  if (!r.ok) {
    const err = new ApiError(r.status, (data && typeof data === "object" ? data : {}) as Partial<T.Error>, r.statusText || `Request failed (${r.status})`);
    if (r.status === 401) unauthorizedListeners.forEach((fn) => fn(err));
    throw err;
  }
  return data as R;
}

const enc = encodeURIComponent;
const q = (o: Record<string, string | number | boolean | undefined>) => {
  const s = Object.entries(o).filter(([, v]) => v !== undefined && v !== "").map(([k, v]) => `${k}=${enc(String(v))}`).join("&");
  return s ? `?${s}` : "";
};
const svc = (eid: string, sid: string) => `/environments/${enc(eid)}/services/${enc(sid)}`;

export const api = {
  // 11.1 system and auth
  version: () => request<{ version: string; commit: string; date: string }>("/version"),
  setupStatus: () => request<T.SetupStatus>("/setup"),
  setup: (b: T.SetupRequest) => request<T.Me>("/setup", { body: b }),
  login: (b: T.LoginRequest) => request<T.LoginResponse>("/auth/login", { body: b }),
  loginTotp: (ticket: string, code: string) => request<T.Me>("/auth/login/totp", { body: { ticket, code } }),
  logout: () => request<void>("/auth/logout", { method: "POST" }),
  forgot: (email: string) => request<void>("/auth/forgot", { body: { email } }),
  reset: (token: string, password: string) => request<T.LoginResponse>("/auth/reset", { body: { token, password } }),
  verifyEmail: (token: string) => request<void>("/auth/verify-email", { body: { token } }),
  me: () => request<T.Me>("/me"),
  notifications: (before?: string) => request<T.Page<T.Notification>>(`/me/notifications${q({ before })}`),
  readNotifications: (ids: string[] | "all") => request<void>("/me/notifications/read", { body: ids === "all" ? { all: true } : { ids } }),
  cliApprove: (code: string) => request<void>("/cli/auth/approve", { body: { code } }),
  invitePreview: (token: string) => request<T.InvitePreview>(`/invites/${enc(token)}`),
  acceptInvite: (b: { token: string; email: string; name: string; password: string }) => request<T.Me>("/invites/accept", { body: b }),

  // 11.2 install and hosts
  install: () => request<T.Install>("/install"),
  hosts: () => request<T.Host[]>("/hosts"),
  registryCredentials: () => request<T.RegistryCredential[]>("/registry-credentials"),
  addRegistryCredential: (b: { name: string; server: string; username: string; password: string }) => request<T.RegistryCredential>("/registry-credentials", { body: b }),

  // 11.3 projects, environments, canvas, changes
  projects: (sort?: "activity" | "created" | "name") => request<T.Project[]>(`/projects${q({ sort })}`),
  createProject: (b: T.CreateProjectRequest = {}) => request<T.Project>("/projects", { body: b }),
  project: (pid: string) => request<T.Project>(`/projects/${enc(pid)}`),
  createEnvironment: (pid: string, b: T.CreateEnvironmentRequest) => request<T.Environment>(`/projects/${enc(pid)}/environments`, { body: b }),
  updateEnvironment: (eid: string, b: T.UpdateEnvironmentRequest) => request<T.Environment>(`/environments/${enc(eid)}`, { method: "PATCH", body: b }),
  syncEnvironment: (eid: string, from: string) => request<T.ChangesResponse>(`/environments/${enc(eid)}/sync`, { body: { from } }),
  canvas: (eid: string) => request<T.Canvas>(`/environments/${enc(eid)}/canvas`),
  savePositions: (eid: string, positions: Record<string, number[]>) => request<void>(`/environments/${enc(eid)}/positions`, { method: "PUT", body: { positions } }),
  saveGroups: (eid: string, groups: T.Group[]) => request<T.Group[]>(`/environments/${enc(eid)}/groups`, { method: "PUT", body: groups }),
  presence: (eid: string, cursor?: number[]) => request<void>(`/environments/${enc(eid)}/presence`, { body: cursor ? { cursor } : {} }),
  changes: (eid: string) => request<T.ChangesResponse>(`/environments/${enc(eid)}/changes`),
  discardChanges: (eid: string) => request<void>(`/environments/${enc(eid)}/changes`, { method: "DELETE" }),
  discardChange: (eid: string, cid: string) => request<void>(`/environments/${enc(eid)}/changes/${enc(cid)}`, { method: "DELETE" }),
  commit: (eid: string, b: T.CommitRequest) => request<T.CommitResponse>(`/environments/${enc(eid)}/changes/commit`, { body: b }),
  activity: (eid: string, o: { service?: string; kind?: string; before?: string } = {}) => request<T.Page<T.Activity>>(`/environments/${enc(eid)}/activity${q(o)}`),
  commitDetail: (id: string) => request<T.CommitDetail>(`/commits/${enc(id)}`),
  importRailway: (token: string, projectName?: string) => request<T.Import>("/imports/railway", { body: { token, projectName } }),
  importCompose: (eid: string, yaml: string) => request<T.Import>(`/environments/${enc(eid)}/imports/compose`, { raw: yaml, contentType: "application/yaml" }),
  importStatus: (id: string) => request<T.Import>(`/imports/${enc(id)}`),
  search: (text: string, signal?: AbortSignal) => request<T.SearchResult>(`/search${q({ q: text })}`, { signal }),

  // 11.4 services
  createService: (eid: string, b: T.CreateServiceRequest) => request<T.CreateServiceResponse>(`/environments/${enc(eid)}/services`, { body: b }),
  updateConfig: (eid: string, sid: string, b: T.UpdateServiceConfigRequest) => request<T.Service>(`${svc(eid, sid)}/config`, { method: "PATCH", body: b }),
  updateInfo: (eid: string, sid: string, b: T.UpdateServiceInfoRequest) => request<T.Service>(`${svc(eid, sid)}/info`, { method: "PATCH", body: b }),
  duplicate: (eid: string, sid: string) => request<T.Service>(`${svc(eid, sid)}/duplicate`, { method: "POST" }),
  deleteService: (eid: string, sid: string, confirm: string) => request<void>(svc(eid, sid), { method: "DELETE", body: { confirm } }),
  createVolume: (eid: string, sid: string, b: T.CreateVolumeRequest) => request<T.Volume>(`${svc(eid, sid)}/volume`, { body: b }),
  deploy: (eid: string, sid: string, b: T.DeployRequest = {}) => request<T.Deployment>(`${svc(eid, sid)}/deploy`, { body: b }),
  deploymentAction: (did: string, action: "redeploy" | "rollback" | "restart" | "abort" | "remove") => request<T.Deployment>(`/deployments/${enc(did)}/${action}`, { method: "POST" }),
  deployments: (eid: string, sid: string, limit = 20) => request<T.Deployment[]>(`${svc(eid, sid)}/deployments${q({ limit })}`),
  generateDomain: (eid: string, sid: string, port?: number) => request<T.Domain>(`${svc(eid, sid)}/domains`, { body: { kind: "generated", port } }),

  // 11.5 templates and GitHub
  templates: (text?: string) => request<T.TemplateSummary[]>(`/templates${q({ q: text })}`),
  template: (id: string) => request<T.TemplateDetail>(`/templates/${enc(id)}`),
  deployTemplate: (eid: string, id: string, inputs: Record<string, string>) => request<T.TemplateDeployResponse>(`/environments/${enc(eid)}/templates/${enc(id)}/deploy`, { body: { inputs } }),
  github: () => request<T.GitHubStatus>("/github"),
  githubApp: (returnTo: string) => request<{ formUrl: string; manifest: unknown }>("/github/app", { body: { returnTo } }),
  repos: (text?: string, refresh?: boolean) => request<T.GitHubRepo[]>(`/github/repos${q({ q: text, refresh: refresh ? 1 : undefined })}`),
  branches: (repo: string) => request<string[]>(`/github/branches${q({ repo })}`),
  scan: (b: T.ScanRequest) => request<T.ScanResult>("/github/scan", { body: b }),
};

/** The URL of the SSE stream for one environment. */
export const eventsUrl = (eid: string) => `${BASE}/events${q({ environment: eid })}`;

/** The icon URL for an icon key ("postgres") or path. */
export function iconUrl(icon?: string): string | undefined {
  if (!icon) return undefined;
  if (icon.startsWith("/") || icon.startsWith("http") || icon.startsWith("data:")) return icon;
  return `${BASE}/icons/${enc(icon)}`;
}

export type * from "./types.gen";
