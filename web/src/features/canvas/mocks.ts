// SPDX-License-Identifier: AGPL-3.0-only

// Stateful MSW handlers for the canvas, so `npm run dev:mocks` shows a realistic project and
// every canvas flow works end to end: drag and group (positions, groups), Config > Name/Icon,
// start command, duplicate, delete (staged), Details with per-change discard, Deploy and Discard.
// Pick a scenario with localStorage "5050.mock.canvas" = "full" (default) | "single" | "empty".
import { http, HttpResponse, type RequestHandler } from "msw";
import type { Canvas, Change, Group, Project, Service, ServiceConfig } from "../../api/client";

const PID = "id_01jexample0000000000000000";
const EID = "id_01jexample0000000000000000";
const now = Date.now();

type CfgOver = Omit<Partial<ServiceConfig>, "deploy"> & { deploy?: Partial<ServiceConfig["deploy"]> };
function cfg(name: string, over: CfgOver = {}): ServiceConfig {
  const { deploy, ...rest } = over;
  return { name, icon: "", source: { type: "empty" }, build: {}, networking: {}, ...rest, deploy: { placement: {}, ...deploy } };
}
function svc(id: string, c: ServiceConfig, over: Partial<Service> = {}): Service {
  return {
    id, projectId: PID, environmentId: EID, config: c, kind: c.kind || "web", status: "online", statusText: "Deployed 2h ago via GitHub",
    domains: [], privateDomain: `${c.name}.internal`, tcpProxies: [], replicas: [], listeningPorts: [], outboundIps: [], processes: 1,
    monitorFiring: false, createdAt: now - 86400000 * 30, activeDeployment: `dep_${id}`, ...over,
  };
}
const dom = (sid: string, domain: string) => ({ id: `dom_${sid}`, serviceId: sid, domain, kind: "generated", port: 8080 }) as Service["domains"][number];
const vol = (sid: string, name: string, mountPath: string, usedBytes: number) => ({ id: `vol_${sid}`, serviceId: sid, name, mountPath, hostId: "host_1", hostName: "fra-1", usedBytes, alertAtPct: 80 });

interface Store { services: Service[]; groups: Group[]; positions: Record<string, number[]>; edges: Canvas["edges"]; changes: Change[] }

function full(): Store {
  const web = svc("svc_web", cfg("web", { source: { type: "github", repo: "orbit/web", branch: "main" } }), { domains: [dom("svc_web", "web-production-4f2a.5050.app")], listeningPorts: [3000] });
  const api = svc("svc_api", cfg("api", { source: { type: "github", repo: "orbit/api", branch: "main" }, deploy: { replicas: 3 } }), { domains: [dom("svc_api", "api-production.5050.app")], listeningPorts: [8080] });
  const worker = svc("svc_worker", cfg("worker", { icon: "nodedotjs", kind: "worker", source: { type: "image", image: "node:22-alpine" } }), { kind: "worker", status: "crashed", statusText: "Crashed 4m ago" });
  const report = svc("svc_report", cfg("nightly-report", { kind: "cron", deploy: { cronSchedule: "0 3 * * *" } }), { kind: "cron", status: "completed", statusText: "Last run 9h ago" });
  const pg = svc("svc_pg", cfg("Postgres", { icon: "postgresql", kind: "database", template: "postgres", templateVersion: "17", source: { type: "image", image: "postgres:17" } }),
    { kind: "database", volume: vol("svc_pg", "postgres-volume", "/var/lib/postgresql/data", 2.4e9), listeningPorts: [5432] });
  const bouncer = svc("svc_bouncer", cfg("PgBouncer", { icon: "pgbouncer", source: { type: "image", image: "bitnami/pgbouncer:1.23" } }), { listeningPorts: [6432] });
  const redis = svc("svc_redis", cfg("Redis", { icon: "redis", kind: "database", template: "redis", source: { type: "image", image: "redis:7" } }),
    { kind: "database", volume: vol("svc_redis", "redis-volume", "/data", 1.8e8), listeningPorts: [6379] });
  const docsCfg = cfg("docs", { source: { type: "github", repo: "orbit/docs", branch: "main" } });
  const docs = svc("svc_docs", docsCfg, { staged: { ...docsCfg, source: { ...docsCfg.source, branch: "release" } }, stagedAction: "update", status: "no_deployment", activeDeployment: undefined });
  const search = svc("svc_search", cfg("search", { icon: "meilisearch", source: { type: "image", image: "getmeili/meilisearch:v1.10" } }), { status: "deploying", statusText: "Deploying" });
  return {
    services: [web, api, worker, report, pg, bouncer, redis, docs, search],
    groups: [{ id: "grp_pg", name: "Postgres + PgBouncer", icon: "postgresql", services: ["svc_bouncer", "svc_pg"] }],
    positions: {},
    edges: [
      { source: "svc_web", target: "svc_api" }, { source: "svc_api", target: "svc_bouncer" }, { source: "svc_api", target: "svc_redis" },
      { source: "svc_bouncer", target: "svc_pg" }, { source: "svc_worker", target: "svc_redis" }, { source: "svc_report", target: "svc_pg" },
      { source: "svc_docs", target: "svc_search" },
    ],
    changes: [{ id: "chg_docs", serviceId: "svc_docs", service: "docs", kind: "service.update", diff: [{ path: "source.branch", old: "main", new: "release" }], author: "Ada", createdAt: now - 60000 }],
  };
}
function single(): Store {
  const s = svc("svc_scraper", cfg("x-scraper"), { status: "no_deployment", activeDeployment: undefined, volume: vol("svc_scraper", "x-scraper-volume", "/data", 0) });
  return { services: [s], groups: [], positions: {}, edges: [], changes: [] };
}
const scenario = () => { try { return localStorage.getItem("5050.mock.canvas") || "full"; } catch { return "full"; } };
let store: Store | undefined;
let storeFor = "";
const S = () => { const k = scenario(); if (!store || storeFor !== k) { store = k === "empty" ? { services: [], groups: [], positions: {}, edges: [], changes: [] } : k === "single" ? single() : full(); storeFor = k; } return store; };

const env = (id: string, name: string) => ({ id, projectId: PID, name, egress: "allowed", createdAt: now - 86400000 * 90 });
const project = (): Project => ({
  id: PID, name: "orbit", description: "Storefront, API and data", publicRead: false, createdAt: now - 86400000 * 90, lastActivityAt: now - 3600000,
  environments: [env(EID, "production"), env("env_staging", "staging")],
});

function stageChange(s: Service, kind: string, path: string, old: unknown, next: unknown) {
  const st = S();
  let c = st.changes.find((x) => x.serviceId === s.id && x.kind !== "service.delete");
  if (!c) { c = { id: `chg_${s.id}_${Date.now()}`, serviceId: s.id, service: (s.staged || s.config).name, kind, diff: [], author: "You", createdAt: Date.now() }; st.changes.push(c); }
  c.diff = [...c.diff.filter((d) => d.path !== path), { path, old, new: next }].filter((d) => d.old !== d.new);
  if (!c.diff.length) st.changes = st.changes.filter((x) => x !== c);
}
function applyStaged() {
  const st = S();
  st.services = st.services.filter((s) => s.stagedAction !== "delete").map((s) => {
    if (!s.staged && !s.stagedAction) return s;
    return { ...s, config: s.staged || s.config, staged: undefined, stagedAction: undefined, status: "deploying", statusText: "Deploying" };
  });
  st.changes = [];
  // the deploys finish a moment later
  setTimeout(() => { for (const s of S().services) if (s.status === "deploying" && s.id !== "svc_search") { s.status = "online"; s.statusText = "Deployed just now"; } }, 2500);
}
function dropStaged(sid?: string) {
  const st = S();
  st.services = st.services.filter((s) => !(s.stagedAction === "create" && (!sid || s.id === sid))).map((s) => (!sid || s.id === sid ? { ...s, staged: undefined, stagedAction: undefined } : s));
  st.changes = sid ? st.changes.filter((c) => c.serviceId !== sid) : [];
}

const canvas = (): Canvas => {
  const st = S();
  return { project: project(), environment: env(EID, "production"), services: st.services, edges: st.edges, groups: st.groups, positions: st.positions, changesCount: st.changes.length, presence: [] };
};
const find = (sid: string) => S().services.find((s) => s.id === sid);
/** True for services this mock store owns, so other mock stores defer to it. */
export const canvasKnows = (sid: string) => !!find(sid);

export const canvasHandlers: RequestHandler[] = [
  http.get("*/api/v1/projects/:pid", () => HttpResponse.json(project())),
  http.get("*/api/v1/environments/:eid/canvas", () => HttpResponse.json(canvas())),
  http.get("*/api/v1/environments/:eid/services/:sid", ({ params }) => {
    const s = find(String(params.sid));
    return s ? HttpResponse.json(s) : undefined;
  }),
  http.put("*/api/v1/environments/:eid/positions", async ({ request }) => {
    const b = (await request.json()) as { positions: Record<string, number[]> };
    S().positions = b.positions || {};
    return new HttpResponse(null, { status: 204 });
  }),
  http.put("*/api/v1/environments/:eid/groups", async ({ request }) => {
    S().groups = (await request.json()) as Group[];
    return HttpResponse.json(S().groups);
  }),
  http.post("*/api/v1/environments/:eid/presence", () => new HttpResponse(null, { status: 204 })),
  http.get("*/api/v1/environments/:eid/changes", () => HttpResponse.json({ changes: S().changes })),
  http.delete("*/api/v1/environments/:eid/changes", () => { dropStaged(); return new HttpResponse(null, { status: 204 }); }),
  http.delete("*/api/v1/environments/:eid/changes/:cid", ({ params }) => {
    const c = S().changes.find((x) => x.id === params.cid);
    if (c?.serviceId) dropStaged(c.serviceId); else S().changes = S().changes.filter((x) => x.id !== params.cid);
    return new HttpResponse(null, { status: 204 });
  }),
  http.post("*/api/v1/environments/:eid/changes/commit", async ({ request }) => {
    const b = (await request.json()) as { deploy?: boolean };
    const ids = S().services.filter((s) => s.staged || s.stagedAction).map((s) => s.id);
    if (b.deploy === false) { for (const s of S().services) if (s.staged) { s.config = s.staged; s.staged = undefined; s.stagedAction = undefined; } S().changes = []; S().services = S().services.filter((s) => s.stagedAction !== "delete"); }
    else applyStaged();
    return HttpResponse.json({ commitId: `cmt_${Date.now()}`, deployments: b.deploy === false ? [] : ids.map((id) => ({ id: `dep_${id}_${Date.now()}`, serviceId: id })) });
  }),
  http.patch("*/api/v1/environments/:eid/services/:sid/info", async ({ params, request }) => {
    const s = find(String(params.sid));
    if (!s) return HttpResponse.json({ error: "No such service.", code: "not_found" }, { status: 404 });
    const b = (await request.json()) as { name?: string; icon?: string };
    const cur = s.staged || s.config;
    if (b.icon !== undefined) { s.config = { ...s.config, icon: b.icon }; if (s.staged) s.staged = { ...s.staged, icon: b.icon }; }
    if (b.name && b.name !== cur.name) {
      s.staged = { ...cur, name: b.name };
      s.stagedAction = s.stagedAction || "rename";
      stageChange(s, "service.rename", "name", s.config.name, b.name);
    }
    return HttpResponse.json(s);
  }),
  http.patch("*/api/v1/environments/:eid/services/:sid/config", async ({ params, request }) => {
    const s = find(String(params.sid));
    if (!s) return HttpResponse.json({ error: "No such service.", code: "not_found" }, { status: 404 });
    // The API body is { patch: <RFC 7396 merge patch of ServiceConfig> }; { deploy } is accepted for old callers.
    const b = (await request.json()) as { patch?: Record<string, unknown>; deploy?: Record<string, unknown> };
    const patch = b.patch ?? (b.deploy ? { deploy: b.deploy } : {});
    const cur = s.staged || s.config;
    const next = { ...cur } as unknown as Record<string, unknown>;
    for (const [sec, val] of Object.entries(patch)) {
      if (val === null || typeof val !== "object" || Array.isArray(val)) {
        const old = (s.config as unknown as Record<string, unknown>)[sec];
        if (val === null) delete next[sec]; else next[sec] = val;
        stageChange(s, "service.update", sec, old ?? null, val);
        continue;
      }
      const section = { ...((next[sec] as Record<string, unknown>) || {}) };
      for (const [k, v] of Object.entries(val as Record<string, unknown>)) {
        const old = ((s.config as unknown as Record<string, Record<string, unknown> | undefined>)[sec] || {})[k];
        if (v === null) delete section[k]; else section[k] = v;
        stageChange(s, "service.update", `${sec}.${k}`, old ?? null, v);
      }
      next[sec] = section;
    }
    s.staged = next as unknown as ServiceConfig;
    s.stagedAction = s.stagedAction || "update";
    return HttpResponse.json(s);
  }),
  http.post("*/api/v1/environments/:eid/services/:sid/duplicate", ({ params }) => {
    const s = find(String(params.sid));
    if (!s) return HttpResponse.json({ error: "No such service.", code: "not_found" }, { status: 404 });
    const name = `${(s.staged || s.config).name}-copy`.slice(0, 32);
    const id = `svc_${Math.random().toString(36).slice(2, 8)}`;
    const copy: Service = { ...s, id, config: { ...(s.staged || s.config), name }, staged: undefined, stagedAction: "create", status: "no_deployment", domains: [], volume: undefined, activeDeployment: undefined };
    S().services.push(copy);
    S().changes.push({ id: `chg_${id}`, serviceId: id, service: name, kind: "service.create", diff: [{ path: "name", old: null, new: name }], author: "You", createdAt: Date.now() });
    return HttpResponse.json(copy);
  }),
  http.delete("*/api/v1/environments/:eid/services/:sid", ({ params }) => {
    const s = find(String(params.sid));
    if (!s) return HttpResponse.json({ error: "No such service.", code: "not_found" }, { status: 404 });
    if (s.stagedAction === "create") { dropStaged(s.id); return new HttpResponse(null, { status: 204 }); }
    s.stagedAction = "delete";
    S().changes = S().changes.filter((c) => c.serviceId !== s.id);
    S().changes.push({ id: `chg_del_${s.id}`, serviceId: s.id, service: s.config.name, kind: "service.delete", diff: [], notes: ["The service stops and its volume is removed."], author: "You", createdAt: Date.now() });
    return new HttpResponse(null, { status: 204 });
  }),
  http.post("*/api/v1/environments/:eid/services", async ({ request }) => {
    const b = (await request.json()) as { name?: string; source?: ServiceConfig["source"]; template?: string; position?: number[]; deployNow?: boolean };
    const id = `svc_${Math.random().toString(36).slice(2, 8)}`;
    const name = b.name || b.template || b.source?.repo?.split("/").pop() || b.source?.image?.split(/[/:]/).slice(-2)[0] || `service-${S().services.length + 1}`;
    const c = cfg(name, { icon: b.template || "", source: b.source || { type: "empty" }, template: b.template });
    const s = svc(id, c, { status: b.deployNow ? "deploying" : "no_deployment", stagedAction: b.deployNow ? undefined : "create", activeDeployment: undefined, kind: b.template ? "database" : "web" });
    S().services.push(s);
    if (b.position && Object.keys(S().positions).length) S().positions[id] = b.position;
    if (!b.deployNow) S().changes.push({ id: `chg_${id}`, serviceId: id, service: name, kind: "service.create", diff: [{ path: "name", old: null, new: name }], author: "You", createdAt: Date.now() });
    return HttpResponse.json({ services: [s], deployments: [] });
  }),
];
