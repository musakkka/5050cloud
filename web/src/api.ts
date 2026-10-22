// SPDX-License-Identifier: AGPL-3.0-only

export type Status = "online" | "deploying" | "crashed" | "unhealthy" | "removed" | "failed";

export interface ProjectSummary {
  id: string;
  name: string;
  envs: { name: string; total: number; online: number; icons: { icon: string | null; kind: string; source?: string }[] }[];
}

export interface CanvasNode {
  id: string;
  kind: string;
  name: string;
  icon: string | null;
  x: number;
  y: number;
  group: string | null;
  status: Status;
  host: string;
  domain?: string | null;
  source?: "github" | "image";
  svcKind?: string;
  volume?: string | null;
  since?: number;
  version?: string;
  replicas?: number;
}

export interface Group { name: string; icon?: string | null; x: number; y: number; parent: string | null }

export interface Canvas {
  project: { id: string; name: string; envs: string[] };
  env: string;
  nodes: CanvasNode[];
  groups: Record<string, Group>;
  edges: { source: string; target: string }[];
  /** where people dragged the cards (px); empty means the automatic layout */
  positions: Record<string, [number, number]>;
}

export interface Commit { sha: string; author: string; message: string; time: number }

export interface Deployment {
  id: string;
  version: string | null;
  created: number | null;
  finished?: number;
  container?: string;
  containerName?: string;
  state?: string;
  status: Status;
  badge: string;
  commit: Commit | null;
  source?: string;
  actor?: string;
  kind?: string;
  historyId?: string;
  host?: string;
  log?: boolean;
}

export interface Resource {
  id: string;
  kind: string;
  name: string;
  status: Status;
  host: string;
  hostName: string;
  region: string;
  icon?: string;
  replicas: number;
  entry?: Record<string, any>;
  domains?: string[];
  internal?: string | null;
  svcKind?: string;
  repo?: string;
  image?: string;
  lastVersion?: string;
}

export interface LogLine { ts: number; msg: string; stream: string; level: string; target?: string; service?: string }

export interface Activity { ts: number; target: string; name: string; kind: string; text: string; historyId?: string }

async function req<T>(path: string, init?: RequestInit): Promise<T> {
  const r = await fetch(path, { credentials: "include", ...init, headers: { "content-type": "application/json", ...(init?.headers || {}) } });
  if (!r.ok) {
    let msg = r.statusText;
    try { msg = (await r.json()).detail || msg; } catch { /* not json */ }
    throw new Error(msg);
  }
  return r.json();
}

const enc = encodeURIComponent;

export const api = {
  projects: () => req<ProjectSummary[]>("/api/projects"),
  canvas: (pid: string, env: string) => req<Canvas>(`/api/projects/${pid}/canvas?env=${enc(env)}`),
  savePositions: (pid: string, env: string, positions: Record<string, [number, number]>) =>
    req<{ ok: boolean }>(`/api/projects/${pid}/positions?env=${enc(env)}`, { method: "POST", body: JSON.stringify({ positions }) }),
  resource: (t: string) => req<Resource>(`/api/resources/${enc(t)}`),
  deployments: (t: string) => req<{ active: string | null; deployments: Deployment[]; lastVersion: string | null }>(`/api/resources/${enc(t)}/deployments`),
  buildLog: (t: string, hid: string) => req<{ lines: { ts: number | null; msg: string }[]; done: boolean }>(`/api/resources/${enc(t)}/build-log/${hid}`),
  logs: (targets: string[], o: { q?: string; tail?: number; since?: number; until?: number; version?: string } = {}) =>
    req<{ lines: LogLine[] }>(`/api/logs?targets=${enc(targets.join(","))}&q=${enc(o.q || "")}&tail=${o.tail || 300}&since=${o.since || 0}&until=${o.until || 0}&version=${enc(o.version || "")}`),
  logStreamUrl: (targets: string[], q = "") => `/api/logs/stream?targets=${enc(targets.join(","))}&q=${enc(q)}`,
  metrics: (t: string, range: number) => req<{ step: number; points: number[][]; memLimit: number | null; hostCpus?: number; hostMem?: number }>(`/api/resources/${enc(t)}/metrics?range=${range}`),
  seriesMany: (keys: string[], range: number) => req<{ step: number; series: Record<string, number[][]> }>(`/api/metrics?keys=${enc(keys.join(","))}&range=${range}`),
  hosts: () => req<any[]>("/api/hosts"),
  activity: (targets: string[]) => req<Activity[]>(`/api/activity?targets=${enc(targets.join(","))}`),
  variables: (t: string) => req<{ names: string[]; provided: string[] }>(`/api/resources/${enc(t)}/variables`),
  reveal: (t: string, names?: string[]) => req<Record<string, string>>(`/api/resources/${enc(t)}/variables/reveal`, { method: "POST", body: JSON.stringify({ names }) }),
  setVariables: (t: string, body: { set: Record<string, string>; unset: string[]; deploy: boolean }) =>
    req<{ ok: boolean; job: any }>(`/api/resources/${enc(t)}/variables`, { method: "POST", body: JSON.stringify(body) }),
  action: (t: string, action: string, version?: string) =>
    req<{ ok: boolean; job?: any }>(`/api/resources/${enc(t)}/actions`, { method: "POST", body: JSON.stringify({ action, version }) }),
  data: (t: string) => req<{ tables: { name: string; rows: number; bytes: number }[]; size: number | null; connections: number | null }>(`/api/resources/${enc(t)}/data`),
  me: () => req<{ email: string | null; installName?: string }>("/api/me"),
};
