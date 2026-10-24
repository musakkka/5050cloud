// SPDX-License-Identifier: AGPL-3.0-only

// Richer MSW fixtures for the service panel and the deployment drawer, used
// only by `npm run dev:mocks` (service/client.ts installs them lazily). The
// generated fixtures stay untouched: these handlers answer only for the ids
// below (services "svc-*", deployments "dep-*") and pass everything else on.
//
// Open them at /project/<any>/service/svc-hub (active), svc-api (failed with a
// diagnosis), svc-web (no deployments yet), svc-postgres (database),
// svc-worker (building now) and svc-nightly (cron).

import { http, HttpResponse, type HttpHandler } from "msw";
import type { Deployment, DeploymentDetail, DeployStep, LogLine, Service, ServiceConfig } from "../../api/types.gen";

const MIN = 60_000, HOUR = 60 * MIN, DAY = 24 * HOUR;
const now = Date.now();
const ENV = "env-production";

function cfg(name: string, p: Partial<ServiceConfig> & { repo?: string; image?: string }): ServiceConfig {
  const { repo, image, ...rest } = p;
  return {
    name, icon: "", kind: rest.kind,
    source: repo ? { type: "github", repo, branch: "main", rootDir: "/", autoDeploy: true } : image ? { type: "image", image } : { type: "empty" },
    build: { builder: repo ? "dockerfile" : undefined, dockerfilePath: repo ? "Dockerfile" : undefined, watchPaths: repo ? ["src/**", "package.json"] : undefined },
    deploy: { startCommand: repo ? "node dist/server.js" : undefined, healthcheckPath: repo ? "/health" : undefined, restartPolicy: "on_failure", restartMaxRetries: 10, replicas: 1, placement: { roles: ["app"] } },
    networking: { port: 8080, privateName: name },
    ...rest,
  };
}

function svc(id: string, c: ServiceConfig, p: Partial<Service>): Service {
  return {
    id, projectId: "proj-demo", environmentId: ENV, config: c, kind: c.kind || "web", status: "online", statusText: "Online",
    domains: [], privateDomain: `${c.networking.privateName}.internal`, tcpProxies: [], replicas: [], listeningPorts: [8080],
    outboundIps: ["203.0.113.7"], processes: 1, monitorFiring: false, createdAt: now - 40 * DAY, ...p,
  };
}

const replica = (id: string, dep: string, host = "fra-1") => ({ id, deploymentId: dep, hostId: `host-${host}`, hostName: host, state: "running", restarts: 0, startedAt: now - 3 * DAY, singleton: false });

function steps(t0: number, plan: [string, number, DeployStep["state"]?, string?][]): DeployStep[] {
  let t = t0;
  return plan.map(([name, ms, state = "ok", detail]) => {
    const s: DeployStep = { name, state, startedAt: state === "pending" ? undefined : t, finishedAt: state === "ok" || state === "failed" ? t + ms : undefined, detail };
    t += ms;
    return s;
  });
}

const OK_STEPS = (t0: number) => steps(t0, [["queued", 400], ["fetch", 1800], ["build", 98_000], ["push", 4200], ["pre_deploy", 3000], ["start", 7000], ["healthcheck", 3200], ["cutover", 900], ["drain", 30_000]]);

/** A stable uuid-looking id from a readable fixture name. */
function hexId(name: string): string {
  let h1 = 0x811c9dc5, h2 = 0x1234567;
  for (const c of name) { h1 = Math.imul(h1 ^ c.charCodeAt(0), 16777619) >>> 0; h2 = Math.imul(h2 + c.charCodeAt(0), 2654435761) >>> 0; }
  const x = (h1.toString(16).padStart(8, "0") + h2.toString(16).padStart(8, "0")).repeat(2);
  return `${x.slice(0, 8)}-${x.slice(8, 12)}-4${x.slice(13, 16)}-a${x.slice(17, 20)}-${x.slice(20, 32)}`;
}

/** Fixture ids by readable name, so the screenshots can open a given deployment. */
export const DEP_IDS: Record<string, string> = {};

function dep(name: string, serviceId: string, p: Partial<Deployment>): Deployment {
  const id = hexId(name);
  DEP_IDS[name] = id;
  const createdAt = p.createdAt ?? now - DAY;
  const st = p.steps ?? OK_STEPS(createdAt + 1000);
  return {
    id, serviceId, environmentId: ENV, status: "REMOVED", label: "Removed", active: false, trigger: "github", actor: "ada",
    canRollback: true, restarts: 0, createdAt, startedAt: createdAt + 1000,
    finishedAt: st.every((s) => s.finishedAt) ? Math.max(...st.map((s) => s.finishedAt || 0)) : undefined, ...p, steps: st,
  };
}

const commit = (message: string, ago: number, author = "Ada Lovelace") => ({ sha: Math.random().toString(16).slice(2, 10) + "c0ffee12", author, message, branch: "main", time: now - ago, url: "https://github.com/acme/hub/commit/1" });

const HUB_MSGS = [
  "Add bounded retries for the webhook relay during deploy windows",
  "Keep worker heartbeats responsive under load and make links permanent",
  "Cache the session lookup and trim the auth middleware",
  "Fix flaky pagination cursor on the activity feed",
  "Move image resizing into a background queue",
  "Upgrade to Node 22 and refresh the lockfile",
  "Rate limit the public API per token instead of per IP",
  "Add structured logs with request ids",
  "Rename the billing service and update references",
  "Initial deploy",
];

const services: Record<string, Service> = {};
const deps: Record<string, Deployment[]> = {};

function seed() {
  // svc-hub: active deployment from GitHub, long history.
  const hub = svc("svc-hub", cfg("hub", { repo: "acme/hub" }), {
    domains: [{ id: "dom-1", serviceId: "svc-hub", domain: "hub-production.up.5050.example", kind: "generated", port: 8080, dnsOk: true, certOk: true, status: "active" }],
    replicas: [replica("rep-1", hexId("dep-hub-1"))], activeDeployment: hexId("dep-hub-1"), statusText: "Deployed 3 days ago via GitHub",
  });
  services[hub.id] = hub;
  deps[hub.id] = HUB_MSGS.map((m, i) => dep(`dep-hub-${i + 1}`, hub.id, {
    status: i === 0 ? "SUCCESS" : i === 4 ? "SKIPPED" : "REMOVED", label: i === 0 ? "Active" : i === 4 ? "Skipped" : "Removed", active: i === 0,
    reason: i === 4 ? "No changed files matched the watch paths" : undefined,
    trigger: i % 3 === 2 ? "cli" : "github", actor: i % 3 === 2 ? "grace" : "ada",
    commit: i % 3 === 2 ? undefined : commit(m, (3 + i * 2) * DAY), message: i % 3 === 2 ? m : undefined,
    createdAt: now - (3 + i * 2) * DAY - i * HOUR,
  }));
  for (let i = 0; i < 40; i++) {
    deps[hub.id].push(dep(`dep-hub-old-${i}`, hub.id, { trigger: "github", commit: commit(`Routine change ${40 - i}: tidy handlers and tests`, (25 + i) * DAY), createdAt: now - (25 + i) * DAY, canRollback: i < 5 }));
  }

  // svc-api: the latest deploy failed its healthcheck; nothing is active.
  const api = svc("svc-api", cfg("api", { repo: "acme/api", deploy: { startCommand: "sleep infinity", healthcheckPath: "/health", restartPolicy: "on_failure", restartMaxRetries: 10, replicas: 1, placement: { roles: ["app"] } } }), {
    status: "failed", statusText: "Deploy failed 2 days ago",
    domains: [{ id: "dom-2", serviceId: "svc-api", domain: "api.acme.dev", kind: "custom", port: 8080, dnsOk: true, certOk: true, status: "active" }],
  });
  services[api.id] = api;
  const t0 = now - 2 * DAY;
  deps[api.id] = [
    dep("dep-api-1", api.id, {
      status: "FAILED", label: "Failed", trigger: "github", commit: commit("Add A/B experiments with stable assignment per account", 2 * DAY),
      createdAt: t0, canRollback: false,
      diagnosis: "The start command is set to 'sleep infinity', which replaces the image's CMD, so the app never listened on port 8080 and every /health check failed. Clear the custom start command or set it to the app's entry point, then redeploy.",
      steps: steps(t0 + 1000, [["queued", 300], ["fetch", 1200], ["build", 102_000], ["push", 3100], ["pre_deploy", 2000], ["start", 11_000], ["healthcheck", 92_000, "failed", "Healthcheck failure"], ["cutover", 0, "pending"], ["drain", 0, "pending"]]),
    }),
    ...["Split the experiments table into its own schema", "Add request tracing to the scoring endpoint", "Bump the pg driver"].map((m, i) =>
      dep(`dep-api-${i + 2}`, api.id, { commit: commit(m, (6 + i * 3) * DAY), createdAt: now - (6 + i * 3) * DAY })),
  ];

  // svc-web: a repo is connected but nothing was deployed yet.
  const web = svc("svc-web", cfg("web", { repo: "acme/storefront" }), { status: "no_deployment", statusText: "No deployment" });
  services[web.id] = web;
  deps[web.id] = [];

  // svc-postgres: database from a template, image source, volume.
  const pg = svc("svc-postgres", cfg("Postgres", { image: "postgres:17", kind: "database", template: "postgres", templateVersion: "17", icon: "postgresql" }), {
    kind: "database", replicas: [replica("rep-pg", hexId("dep-pg-1"))], activeDeployment: hexId("dep-pg-1"), privateDomain: "postgres.internal", listeningPorts: [5432],
    volume: { id: "vol-pg", serviceId: "svc-postgres", name: "postgres-volume", mountPath: "/var/lib/postgresql/data", hostId: "host-fra-1", hostName: "fra-1", usedBytes: 412_000_000, alertAtPct: 80 },
    tcpProxies: [{ id: "tcp-1", address: "203.0.113.7:41522", hostPort: 41522, targetPort: 5432 }],
  });
  services[pg.id] = pg;
  deps[pg.id] = [
    dep("dep-pg-1", pg.id, { status: "SUCCESS", label: "Active", active: true, trigger: "template", actor: "ada", image: "postgres:17", digest: "sha256:4f1a9c0e5d2b", createdAt: now - 12 * DAY,
      steps: steps(now - 12 * DAY, [["queued", 200], ["pull image", 14_000], ["start", 4000], ["healthcheck", 2000], ["cutover", 500]]) }),
  ];

  // svc-worker: a build is running right now; the previous one is active.
  const worker = svc("svc-worker", cfg("worker", { repo: "acme/jobs" }), {
    status: "building", statusText: "Building", replicas: [replica("rep-w", hexId("dep-w-1"), "fra-2")], activeDeployment: hexId("dep-w-1"),
  });
  services[worker.id] = worker;
  const tb = now - 70_000;
  deps[worker.id] = [
    dep("dep-w-2", worker.id, { status: "BUILDING", label: "Building", trigger: "github", commit: commit("Retry failed emails with exponential backoff", 70_000), createdAt: tb,
      steps: steps(tb, [["queued", 400], ["fetch", 1500], ["build", 0, "running"], ["push", 0, "pending"], ["start", 0, "pending"], ["healthcheck", 0, "pending"], ["cutover", 0, "pending"]]) }),
    dep("dep-w-1", worker.id, { status: "SUCCESS", label: "Active", active: true, commit: commit("Send digests in batches of 500", 5 * DAY), createdAt: now - 5 * DAY }),
  ];

  // svc-nightly: cron job.
  const cron = svc("svc-nightly", cfg("nightly-report", { image: "ghcr.io/acme/reports:2.4", kind: "cron", deploy: { cronSchedule: "0 3 * * *", cronTimezone: "UTC", replicas: 1, placement: { roles: ["app"] } } }), {
    kind: "cron", status: "completed", statusText: "Next run in 4h",
  });
  services[cron.id] = cron;
  deps[cron.id] = [dep("dep-cron-1", cron.id, { status: "COMPLETED", label: "Completed", trigger: "cron", image: "ghcr.io/acme/reports:2.4", createdAt: now - 20 * HOUR,
    steps: steps(now - 20 * HOUR, [["queued", 300], ["pull image", 3000], ["start", 41_000]]) })];
}
seed();

function findDep(did: string): Deployment | undefined {
  for (const list of Object.values(deps)) { const d = list.find((x) => x.id === did); if (d) return d; }
  return undefined;
}

function detail(d: Deployment): DeploymentDetail {
  const s = services[d.serviceId];
  return {
    ...d, config: s.config, replicas: d.active ? s.replicas : [],
    variableNames: s.kind === "database" ? ["PGDATA", "POSTGRES_DB", "POSTGRES_PASSWORD", "POSTGRES_USER", "DATABASE_URL"] : ["DATABASE_URL", "NODE_ENV", "PORT", "REDIS_URL", "SESSION_SECRET", "SENTRY_DSN"],
    source: s.config.source.type === "github" ? { "build.dockerfilePath": "5050.json:4" } : undefined,
    configFile: s.config.source.type === "github" ? { path: "5050.json", format: "json" } : undefined,
  };
}

// A new deployment walks through its steps on a timer, like the real one.
let seq = 100;
function startDeployment(serviceId: string, p: Partial<Deployment>): Deployment {
  const s = services[serviceId];
  const t = Date.now();
  const d = dep(`dep-new-${seq++}`, serviceId, {
    status: "QUEUED", label: "Queued", trigger: "ui", actor: "ada", createdAt: t, startedAt: t,
    commit: s.config.source.type === "github" ? commit("Deploy the latest commit on main", 0) : undefined,
    image: s.config.source.image, ...p,
    steps: ["queued", "fetch", "build", "push", "start", "healthcheck", "cutover"].map((name) => ({ name, state: "pending" })),
  });
  deps[serviceId] = [d, ...(deps[serviceId] || [])];
  let i = 0;
  const tick = () => {
    const st = d.steps;
    if (i > 0) { st[i - 1].state = "ok"; st[i - 1].finishedAt = Date.now(); }
    if (i >= st.length) {
      d.status = "SUCCESS"; d.label = "Active"; d.finishedAt = Date.now();
      for (const o of deps[serviceId]) if (o !== d && o.active) { o.active = false; o.status = "REMOVED"; o.label = "Removed"; }
      d.active = true; s.activeDeployment = d.id; s.status = "online"; s.statusText = "Deployed just now via Console";
      if (!s.replicas.length) s.replicas = [replica("rep-new", d.id)];
      return;
    }
    st[i].state = "running"; st[i].startedAt = Date.now();
    d.status = i < 4 ? (i === 0 ? "QUEUED" : "BUILDING") : "DEPLOYING";
    d.label = d.status.charAt(0) + d.status.slice(1).toLowerCase();
    i++;
    setTimeout(tick, i === 3 ? 4000 : 1500);
  };
  setTimeout(tick, 600);
  return d;
}

// Logs ---------------------------------------------------------------------

let lineSeq = 0;
const line = (ts: number, msg: string, p: Partial<LogLine> = {}): LogLine => ({ id: `ln-${lineSeq++}`, ts, msg, stream: "stdout", level: "info", replica: "rep-1", ...p });

function buildLines(d: Deployment): LogLine[] {
  if (!d.commit && d.image) return [];
  const t = toMsTs(d.startedAt || d.createdAt);
  const st = (n: string, cmd: string, ms: number, off: number, extra: Partial<LogLine["attrs"]> = {}) =>
    line(t + off, cmd, { stream: "build", attrs: { step: n, cmd, durationMs: ms, ...extra } });
  return [
    line(t, "Using Detected Dockerfile", { stream: "build" }),
    line(t + 200, "context: 7q2x-9b1d", { stream: "build" }),
    st("internal", "load build definition from Dockerfile", 1, 300),
    st("internal", "load metadata for docker.io/library/node:22-slim", 840, 400),
    st("internal", "load build context", 1, 1300),
    st("1", "FROM docker.io/library/node:22-slim", 3, 1400, { cached: true }),
    st("2", "WORKDIR /app", 41, 1500, { cached: true }),
    st("3", "COPY package.json package-lock.json ./", 985, 1600),
    line(t + 2700, "scheduling build on builder fra-1", { stream: "build" }),
    st("4", "RUN npm ci --omit=dev", 34_000, 2800),
    line(t + 30_000, "npm notice New minor version of npm available! 10.8.2 -> 10.9.0", { stream: "build", attrs: { step: "4" } }),
    line(t + 36_000, "added 412 packages, and audited 413 packages in 31s", { stream: "build", attrs: { step: "4" } }),
    st("5", "COPY . .", 587, 37_000),
    st("6", "RUN npm run build", 21_000, 38_000),
    line(t + 52_000, "> hub@1.4.0 build", { stream: "build", attrs: { step: "6" } }),
    line(t + 58_000, "dist/server.js  182.4kb", { stream: "build", attrs: { step: "6" } }),
    line(t + 60_000, "exporting to docker image format", { stream: "build", attrs: { durationMs: 6000 } }),
    line(t + 66_000, "containerimage.digest: sha256:cd675e98d38a434137f5e5b9efae3421f8d87b070256dfdf3a407ef9920a5d66", { stream: "build" }),
    line(t + 66_100, "image push 109.6 MB", { stream: "build", attrs: { step: "push", cmd: "image push 109.6 MB", durationMs: 7000 } }),
  ];
}

const toMsTs = (ts: number) => (ts > 1e12 ? ts : ts * 1000);
const PATHS = ["/", "/api/users", "/api/session", "/favicon.ico", "/robots.txt", "/health", "/api/agent/poll", "/wp-admin/install.php", "/.well-known/security.txt", "/sitemap.xml"];

function deployLines(d: Deployment): LogLine[] {
  const t = toMsTs(d.finishedAt || d.createdAt);
  const out: LogLine[] = [
    line(t - 9000, "Starting Container", { stream: "system" }),
    line(t - 8000, "> hub@1.4.0 start", { replica: "rep-1" }),
    line(t - 7800, "> node dist/server.js"),
    line(t - 7000, "Server listening on http://0.0.0.0:8080", { attrs: { port: 8080 } }),
  ];
  if (d.status === "FAILED") {
    for (let i = 0; i < 6; i++) out.push(line(t - 6000 + i * 15_000, `Attempt #${i + 1} failed with service unavailable. Continuing to retry for ${90 - i * 15}s`, { level: "error", stream: "system" }));
    out.push(line(t, "Healthcheck failed!", { level: "error", stream: "system" }));
    return out;
  }
  for (let i = 0; i < 160; i++) {
    const p = PATHS[i % PATHS.length];
    const code = p.includes("wp-admin") || p.includes("robots") || p.includes("security") || p.includes("sitemap") ? 404 : 200;
    if (i % 23 === 7) {
      out.push(line(t + i * 41_000, `Error: The session token did not match the expected format. Received "${(i * 7919).toString(16)}".`, { level: "error", stream: "stderr", attrs: { level: "error" } }));
      out.push(line(t + i * 41_000 + 5, "    at verifySession (/app/dist/server.js:1182:11)", { level: "error", stream: "stderr" }));
      continue;
    }
    out.push(line(t + i * 41_000, `INFO:     10.128.${i % 7}.${20 + (i % 30)}:0 - "${i % 4 ? "GET" : "POST"} ${p} HTTP/1.1" ${code} ${code === 200 ? "OK" : "Not Found"}`));
  }
  return out;
}

function httpLines(d: Deployment): LogLine[] {
  const t = toMsTs(d.finishedAt || d.createdAt);
  return Array.from({ length: 140 }, (_, i) => {
    const p = PATHS[(i * 3) % PATHS.length];
    const code = p.includes("wp-admin") || p.includes("robots") || p.includes("security") || p.includes("sitemap") ? 404 : i % 37 === 5 ? 502 : 200;
    const method = p.startsWith("/api/") && i % 3 === 0 ? "POST" : "GET";
    return line(t + i * 97_000, `${method} ${p} ${code}`, { stream: "http", attrs: { method, path: p, httpStatus: code, durationMs: 2 + ((i * 7) % 12), host: "hub-production.up.5050.example", srcIp: `198.51.100.${i % 200}`, requestId: `req_${(i * 104729).toString(36)}` } });
  });
}

function flowLines(d: Deployment): LogLine[] {
  const t = toMsTs(d.finishedAt || d.createdAt);
  return Array.from({ length: 60 }, (_, i) => {
    const ingress = i % 2 === 0;
    const internal = i % 4 === 0;
    return line(t + i * 33_000, "flow", { stream: "flow", attrs: {
      dir: ingress ? "ingress" : "egress", proto: "TCP",
      src: ingress ? (internal ? "fd12:0:8:0:a000:c:8000" : `100.64.0.${20 + (i % 9)}:5${1000 + i}`) : "10.130.4.103:8080",
      dst: ingress ? "fd12:595a:78d:1:3000::8080" : `100.64.0.${20 + (i % 9)}:5${1000 + i}`,
      peer: internal ? "postgres" : "Internet", peerKind: internal ? "service" : "internet",
      bytes: [172, 66, 6200, 1300, 9000, 969][i % 6], latencyMs: i % 11 === 3 ? 2 : 0, status: "ok",
    } });
  });
}

function logsFor(d: Deployment, type: string): LogLine[] {
  const set = type === "http" ? httpLines(d) : type === "flow" ? flowLines(d) : type === "dns" ? [] : deployLines(d);
  return set.map((l) => ({ ...l, deploymentId: d.id, serviceId: d.serviceId }));
}

function sse(make: () => LogLine[] | null, every = 1800): HttpResponse<ReadableStream> {
  const enc = new TextEncoder();
  let timer: ReturnType<typeof setInterval> | undefined;
  const body = new ReadableStream({
    start(ctrl) {
      ctrl.enqueue(enc.encode(": ok\n\n"));
      timer = setInterval(() => {
        const add = make();
        if (add?.length) ctrl.enqueue(enc.encode(`data: ${JSON.stringify(add)}\n\n`));
      }, every);
    },
    cancel() { clearInterval(timer); },
  });
  return new HttpResponse(body, { headers: { "content-type": "text/event-stream", "cache-control": "no-cache" } });
}

const own = (id: string | readonly string[] | undefined) => typeof id === "string" && (!!services[id] || !!findDep(id));

export const serviceMockHandlers: HttpHandler[] = [
  http.get("*/api/v1/environments/:eid/services/:sid", ({ params }) => {
    const s = own(params.sid) ? services[params.sid as string] : undefined;
    return s ? HttpResponse.json(s) : undefined;
  }),
  http.get("*/api/v1/environments/:eid/services/:sid/deployments", ({ params }) => {
    if (!own(params.sid)) return undefined;
    return HttpResponse.json(deps[params.sid as string] || []);
  }),
  http.post("*/api/v1/environments/:eid/services/:sid/deploy", ({ params }) => {
    if (!own(params.sid) || !services[params.sid as string]) return undefined;
    return HttpResponse.json(startDeployment(params.sid as string, {}));
  }),
  http.get("*/api/v1/deployments/:did", ({ params }) => {
    const d = own(params.did) ? findDep(params.did as string) : undefined;
    return d ? HttpResponse.json(detail(d)) : undefined;
  }),
  http.post("*/api/v1/deployments/:did/:action", ({ params }) => {
    const d = own(params.did) ? findDep(params.did as string) : undefined;
    if (!d) return undefined;
    const a = params.action as string;
    if (a === "redeploy") return HttpResponse.json(startDeployment(d.serviceId, { trigger: "redeploy", commit: d.commit, message: d.message, image: d.image }));
    if (a === "rollback") return HttpResponse.json(startDeployment(d.serviceId, { trigger: "rollback", rollbackOf: d.id, commit: d.commit, message: d.message }));
    if (a === "remove" || a === "abort") {
      d.status = a === "abort" ? "CANCELLED" : "REMOVED"; d.label = "Removed"; d.active = false;
      d.reason = a === "abort" ? "Aborted by ada" : undefined;
      const s = services[d.serviceId];
      if (s.activeDeployment === d.id) { s.activeDeployment = undefined; s.status = "removed"; s.statusText = "Service offline"; s.replicas = []; }
      return HttpResponse.json(d);
    }
    if (a === "restart") { d.restarts += 1; return HttpResponse.json(d); }
    return undefined;
  }),
  http.get("*/api/v1/deployments/:did/build-logs", ({ params, request }) => {
    const d = own(params.did) ? findDep(params.did as string) : undefined;
    if (!d) return undefined;
    if (new URL(request.url).searchParams.get("follow")) return sse(() => null, 5000);
    return HttpResponse.json({ lines: buildLines(d).map((l) => ({ ...l, deploymentId: d.id })), truncated: false });
  }),
  http.get("*/api/v1/logs", ({ request }) => {
    const u = new URL(request.url).searchParams;
    const d = own(u.get("deployment") ?? undefined) ? findDep(u.get("deployment")!) : undefined;
    if (!d) return undefined;
    const all = logsFor(d, u.get("type") || "deploy");
    const limit = Number(u.get("limit")) || 1000;
    return HttpResponse.json({ lines: all.slice(0, limit), truncated: all.length > limit });
  }),
  http.get("*/api/v1/logs/stream", ({ request }) => {
    const u = new URL(request.url).searchParams;
    const d = own(u.get("deployment") ?? undefined) ? findDep(u.get("deployment")!) : undefined;
    if (!d) return undefined;
    const type = u.get("type") || "deploy";
    if (!d.active || (type !== "deploy" && type !== "http")) return sse(() => null, 10_000);
    let i = 0;
    return sse(() => {
      i++;
      const p = PATHS[i % PATHS.length];
      const code = p.includes("wp-admin") ? 404 : 200;
      const l = type === "http"
        ? line(Date.now(), `GET ${p} ${code}`, { stream: "http", attrs: { method: "GET", path: p, httpStatus: code, durationMs: 3 + (i % 9) } })
        : line(Date.now(), `INFO:     10.128.3.${40 + (i % 9)}:0 - "GET ${p} HTTP/1.1" ${code} ${code === 200 ? "OK" : "Not Found"}`);
      return [{ ...l, deploymentId: d.id, serviceId: d.serviceId }];
    });
  }),
];

/** Registers the handlers on the running MSW worker (mocks mode only). */
export async function installServiceMocks(): Promise<void> {
  const { worker } = await import("../../api/mocks/browser");
  worker.use(...serviceMockHandlers);
}
