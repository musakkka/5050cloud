// SPDX-License-Identifier: AGPL-3.0-only

// Stateful MSW handlers for Settings, Networking, Cron Runs and the volume
// panel, so `npm run dev:mocks` shows real-looking services and every
// settings flow (stage, domains, TCP proxy, volume) round-trips. Prepend
// `serviceSettingsHandlers` to the generated handlers. A service id that
// contains "pg" or "postgres" answers as a Postgres database with a volume;
// one that contains "cron" answers as a scheduled job; anything else is a
// GitHub web service.

import { http, HttpResponse } from "msw";
import { canvasKnows } from "../canvas/mocks";
import type { CronRun, Domain, Host, Service, ServiceConfig, TCPProxy, VolumeDetail } from "../../api/types.gen";

const now = Date.now();
const clone = <T,>(v: T): T => JSON.parse(JSON.stringify(v)) as T;

function webConfig(name: string): ServiceConfig {
  return {
    name,
    icon: "",
    kind: "web",
    source: { type: "github", repo: "acme/storefront", autoDeploy: true },
    build: { builder: "", watchPaths: [] },
    deploy: { placement: { roles: ["app"] }, replicas: 1, restartPolicy: "on_failure", restartMaxRetries: 10 },
    networking: { port: 8080, privateName: name },
  };
}

function pgConfig(): ServiceConfig {
  return {
    name: "Postgres",
    icon: "postgresql",
    kind: "database",
    template: "postgres",
    templateVersion: "17",
    source: { type: "image", image: "ghcr.io/5050cloud/postgres:17" },
    build: {},
    deploy: { placement: { roles: ["data"] }, replicas: 1, restartPolicy: "on_failure", restartMaxRetries: 10, memoryReservationMb: 512 },
    networking: { port: 5432, privateName: "postgres", publicAccess: false },
  };
}

function cronConfig(): ServiceConfig {
  const c = webConfig("nightly-report");
  c.kind = "cron";
  c.source = { type: "image", image: "ghcr.io/acme/report-job:1.4" };
  c.deploy.cronSchedule = "0 3 * * *";
  c.deploy.cronTimezone = "Europe/Berlin";
  c.networking = { privateName: "nightly-report" };
  return c;
}

function redisConfig(): ServiceConfig {
  const c = pgConfig();
  return { ...c, name: "Redis", icon: "redis", template: "redis", templateVersion: "7", source: { type: "image", image: "redis:7.4" }, networking: { port: 6379, privateName: "redis", publicAccess: false } };
}

/** A readable service name from an id such as svc_web or svc_api. */
const nameFromId = (sid: string) => {
  const n = sid.replace(/^(svc|srv|id)[_-]?/i, "").replace(/[^a-z0-9-]/gi, "-").toLowerCase();
  return !n || n.startsWith("01jexample") ? "storefront" : n.slice(0, 32);
};

function baseService(sid: string, eid: string): Service {
  const pg = /pg|postgres/i.test(sid);
  const redis = /redis/i.test(sid);
  const cron = /cron|report/i.test(sid);
  const worker = /worker/i.test(sid);
  const config = pg ? pgConfig() : redis ? redisConfig() : cron ? cronConfig() : webConfig(nameFromId(sid));
  if (worker) { config.kind = "worker"; config.source = { type: "image", image: "node:22-alpine" }; config.icon = "nodedotjs"; }
  const name = config.networking.privateName || "svc";
  const db = pg || redis;
  return {
    id: sid,
    projectId: "id_01jexample0000000000000000",
    environmentId: eid,
    config,
    kind: worker ? "worker" : config.kind || "web",
    status: "online",
    statusText: db ? "Deployed 2 days ago" : "Deployed 3 hours ago via GitHub",
    activeDeployment: "dep_01",
    domains: [],
    privateDomain: `${name}.internal`,
    volume: db ? {
      id: `vol_${sid}`, serviceId: sid, name: pg ? "postgres-volume" : "redis-volume", mountPath: pg ? "/var/lib/postgresql/data" : "/data",
      hostId: "host_data", hostName: "data-1", usedBytes: 412_000_000, alertAtPct: 80,
    } : undefined,
    tcpProxies: [],
    replicas: [{ id: "rep_1", deploymentId: "dep_01", hostId: pg ? "host_data" : "host_app", hostName: pg ? "data-1" : "app-1", state: "running", restarts: 0, startedAt: now - 3 * 3600_000, singleton: db }],
    listeningPorts: pg ? [5432] : redis ? [6379] : cron || worker ? [] : [8080],
    outboundIps: ["203.0.113.10"],
    processes: 1,
    processesText: "Runs 1 process per replica.",
    monitorFiring: false,
    createdAt: now - 30 * 86400_000,
  };
}

function baseDomains(svc: Service): Domain[] {
  if (svc.kind !== "web") return [];
  const n = svc.config.networking.privateName || "app";
  return [
    { id: `dom_${svc.id}_1`, serviceId: svc.id, domain: `${n}-7f3a.up.example.net`, kind: "generated", port: 8080, dnsOk: true, certOk: true, status: "active", certExpiresAt: now + 74 * 86400_000 },
    {
      id: `dom_${svc.id}_2`, serviceId: svc.id, domain: "shop.acme.dev", kind: "custom", port: 8080, dnsOk: true, certOk: true, status: "active", cloudflare: true,
      certExpiresAt: now + 61 * 86400_000,
      message: "Cloudflare proxy detected: keep SSL/TLS mode on Full (strict).",
    },
  ];
}

interface State { svc: Service; domains: Domain[]; volume?: VolumeDetail; runs: CronRun[] }
const db = new Map<string, State>();

function state(eid: string, sid: string): State {
  const key = `${eid}/${sid}`;
  let s = db.get(key);
  if (!s) {
    const svc = baseService(sid, eid);
    const domains = baseDomains(svc);
    svc.domains = domains;
    const volume: VolumeDetail | undefined = svc.volume ? {
      ...svc.volume, hostFreeBytes: 38_400_000_000, hostTotalBytes: 50_000_000_000,
      backups: { configured: true, supported: true, stale: false, lastOk: now - 5 * 3600_000 },
    } : undefined;
    const runs: CronRun[] = svc.kind === "cron" ? [
      { id: "run_3", serviceId: sid, status: "completed", trigger: "schedule", exitCode: 0, scheduledAt: now - 7 * 3600_000, startedAt: now - 7 * 3600_000, finishedAt: now - 7 * 3600_000 + 41_000 },
      { id: "run_2", serviceId: sid, status: "failed", trigger: "manual", exitCode: 1, message: "Exited with code 1", scheduledAt: now - 26 * 3600_000, startedAt: now - 26 * 3600_000, finishedAt: now - 26 * 3600_000 + 12_000 },
      { id: "run_1", serviceId: sid, status: "missed", trigger: "schedule", message: "Missed while the control plane was down", scheduledAt: now - 31 * 3600_000 },
    ] : [];
    s = { svc, domains, volume, runs };
    db.set(key, s);
  }
  return s;
}

/** RFC 7396 merge patch. */
function merge(target: any, patch: any): any {
  if (patch === null || typeof patch !== "object" || Array.isArray(patch)) return patch;
  const out = target && typeof target === "object" && !Array.isArray(target) ? { ...target } : {};
  for (const [k, v] of Object.entries(patch)) {
    if (v === null) delete out[k];
    else out[k] = merge(out[k], v);
  }
  return out;
}

function snapshot(s: State): Service {
  s.svc.domains = s.domains;
  return clone(s.svc);
}

const hosts: Host[] = [
  { id: "host_app", name: "app-1", roles: ["control", "build", "ingress", "app"], state: "online", version: "v0.1.0", arch: "amd64", os: "Ubuntu 24.04", cpus: 8, memBytes: 32_000_000_000, reserveMb: 1024, reservedMb: 2048, publicIp: "203.0.113.10", egressIp: "203.0.113.10", meshIp: "10.128.0.1", acmePrimary: true, disks: [{ mount: "/", total: 160_000_000_000, used: 41_000_000_000 }], replicas: 4, lastSeen: now, cpuUsed: 1.2, memUsed: 9_000_000_000, rebootRequired: false },
  { id: "host_data", name: "data-1", roles: ["data"], state: "online", version: "v0.1.0", arch: "amd64", os: "Ubuntu 24.04", cpus: 4, memBytes: 16_000_000_000, reserveMb: 1024, reservedMb: 512, publicIp: "203.0.113.11", egressIp: "203.0.113.11", meshIp: "10.128.0.2", acmePrimary: false, disks: [{ mount: "/", total: 50_000_000_000, used: 11_600_000_000 }], replicas: 1, lastSeen: now, cpuUsed: 0.4, memUsed: 2_000_000_000, rebootRequired: false },
];

const P = "*/api/v1/environments/:eid/services/:sid";
type Params = { eid: string; sid: string };
/** Ids the deployment mocks own ("svc-..."); their service GET answers there. */
const foreign = (sid: string) => sid.startsWith("svc-") || canvasKnows(sid);

export const serviceSettingsHandlers = [
  http.get<Params>(P, ({ params }) => (foreign(params.sid) ? undefined : HttpResponse.json(snapshot(state(params.eid, params.sid))))),

  http.patch<Params>(`${P}/config`, async ({ params, request }) => {
    if (canvasKnows(params.sid)) return undefined;
    const s = state(params.eid, params.sid);
    const body = (await request.json()) as { patch?: Record<string, unknown> };
    const next = merge(clone(s.svc.staged ?? s.svc.config), body.patch ?? {}) as ServiceConfig;
    if (JSON.stringify(next) === JSON.stringify(s.svc.config)) { delete s.svc.staged; delete s.svc.stagedAction; } else { s.svc.staged = next; s.svc.stagedAction = s.svc.stagedAction || "update"; }
    return HttpResponse.json(snapshot(s));
  }),

  http.delete<Params>(P, ({ params }) => {
    const s = state(params.eid, params.sid);
    s.svc.stagedAction = "delete";
    return new HttpResponse(null, { status: 204 });
  }),

  http.get<Params>(`${P}/domains`, ({ params }) => HttpResponse.json(clone(state(params.eid, params.sid).domains))),

  http.post<Params>(`${P}/domains`, async ({ params, request }) => {
    const s = state(params.eid, params.sid);
    const b = (await request.json()) as { kind: string; domain?: string; port?: number };
    const name = s.svc.config.networking.privateName || "svc";
    const custom = b.kind === "custom";
    const d: Domain = custom ? {
      id: `dom_${Date.now()}`, serviceId: s.svc.id, domain: b.domain || "app.example.com", kind: "custom", port: b.port || 8080, dnsOk: false, certOk: false, status: "pending_dns",
      message: "Waiting for DNS: add one of the records below.",
      dns: [
        { type: "A", name: b.domain || "", host: (b.domain || "").split(".")[0], value: "203.0.113.10" },
        { type: "CNAME", name: b.domain || "", host: (b.domain || "").split(".")[0], value: s.domains[0]?.domain || `${name}.up.example.net` },
      ],
    } : {
      id: `dom_${Date.now()}`, serviceId: s.svc.id, domain: `${name}-${Math.random().toString(16).slice(2, 6)}.up.example.net`, kind: "generated", port: b.port || 8080, dnsOk: true, certOk: false, status: "issuing",
    };
    s.domains = [...s.domains, d];
    return HttpResponse.json(d);
  }),

  http.patch<{ id: string }>("*/api/v1/domains/:id", async ({ params, request }) => {
    const b = (await request.json()) as Partial<Domain>;
    for (const s of db.values()) {
      const i = s.domains.findIndex((d) => d.id === params.id);
      if (i >= 0) { s.domains[i] = { ...s.domains[i], ...b }; return HttpResponse.json(s.domains[i]); }
    }
    return HttpResponse.json({ code: "not_found", message: "No such domain" }, { status: 404 });
  }),

  http.delete<{ id: string }>("*/api/v1/domains/:id", ({ params }) => {
    for (const s of db.values()) s.domains = s.domains.filter((d) => d.id !== params.id);
    return new HttpResponse(null, { status: 204 });
  }),

  http.post<{ id: string }>("*/api/v1/domains/:id/:action", ({ params }) => {
    for (const s of db.values()) {
      const d = s.domains.find((x) => x.id === params.id);
      if (d) return HttpResponse.json(d);
    }
    return HttpResponse.json({ code: "not_found", message: "No such domain" }, { status: 404 });
  }),

  http.post<Params>(`${P}/tcp-proxies`, async ({ params, request }) => {
    const s = state(params.eid, params.sid);
    const b = (await request.json()) as { targetPort: number };
    const hostPort = 20000 + Math.floor(Math.random() * 9999);
    const p: TCPProxy = { id: `tcp_${Date.now()}`, targetPort: b.targetPort, hostPort, address: `proxy.example.net:${hostPort}` };
    s.svc.tcpProxies = [...s.svc.tcpProxies, p];
    return HttpResponse.json(p);
  }),

  http.delete<Params & { id: string }>(`${P}/tcp-proxies/:id`, ({ params }) => {
    const s = state(params.eid, params.sid);
    s.svc.tcpProxies = s.svc.tcpProxies.filter((p) => p.id !== params.id);
    return new HttpResponse(null, { status: 204 });
  }),

  http.get<Params>(`${P}/volume`, ({ params }) => {
    const v = state(params.eid, params.sid).volume;
    return v ? HttpResponse.json(clone(v)) : HttpResponse.json({ code: "not_found", message: "This service has no volume" }, { status: 404 });
  }),

  http.patch<Params>(`${P}/volume`, async ({ params, request }) => {
    const s = state(params.eid, params.sid);
    if (!s.volume) return HttpResponse.json({ code: "not_found", message: "This service has no volume" }, { status: 404 });
    const b = (await request.json()) as Partial<VolumeDetail>;
    s.volume = { ...s.volume, ...b, staged: b.mountPath && b.mountPath !== s.volume.mountPath ? "update" : s.volume.staged };
    return HttpResponse.json(clone(s.volume));
  }),

  http.post<Params>(`${P}/volume/wipe`, ({ params }) => {
    const s = state(params.eid, params.sid);
    if (s.volume) s.volume.usedBytes = 0;
    return new HttpResponse(null, { status: 204 });
  }),

  http.delete<Params>(`${P}/volume`, ({ params }) => {
    const s = state(params.eid, params.sid);
    if (s.volume) s.volume.staged = "delete";
    return new HttpResponse(null, { status: 204 });
  }),

  http.get<Params>(`${P}/cron-runs`, ({ params }) => HttpResponse.json(clone(state(params.eid, params.sid).runs))),

  http.post<Params>(`${P}/cron-runs`, ({ params }) => {
    const s = state(params.eid, params.sid);
    const r: CronRun = { id: `run_${Date.now()}`, serviceId: params.sid, status: "running", trigger: "manual", scheduledAt: Date.now(), startedAt: Date.now() };
    s.runs = [r, ...s.runs];
    return HttpResponse.json(r);
  }),
];

/** Two realistic hosts (an app host and a data host) for the Scale section's limits. */
export const serviceSettingsHostHandlers = [http.get("*/api/v1/hosts", () => HttpResponse.json(clone(hosts)))];

/** Registers the handlers on the running MSW worker (mocks mode only). */
export async function installSettingsMocks(): Promise<void> {
  const { worker } = await import("../../api/mocks/browser");
  worker.use(...serviceSettingsHandlers);
}
