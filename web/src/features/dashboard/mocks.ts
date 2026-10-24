// SPDX-License-Identifier: AGPL-3.0-only

// Dashboard fixtures for `npm run dev:mocks`: a realistic set of projects (databases, repos,
// images, a cron job, an empty project, one large project, one crashed service) and two healthy
// hosts, so the cards, tiles, sort orders and status lines can be checked against the design.
// Loaded only in mocks mode (see Dashboard.tsx); compiled out of production builds.
// Add `?mock=empty` or `?mock=slow` to the page URL to see the empty and loading states.
import { delay, http, HttpResponse } from "msw";
import type { EnvSummary, Host, Project } from "../../api/types.gen";

type Svc = [icon: string, source: string, status?: string];

const DAY = 86_400_000;
const now = Date.now();

function project(i: number, name: string, svcs: Svc[], o: { activity: number; created: number; staging?: boolean; description?: string }): Project {
  const pid = `prj_dash${String(i).padStart(2, "0")}`;
  const prod = `env_${pid}_prod`;
  const environments = [
    { id: prod, projectId: pid, name: "production", egress: "allowed", createdAt: now - o.created },
    ...(o.staging ? [{ id: `env_${pid}_stg`, projectId: pid, name: "staging", egress: "allowed", createdAt: now - o.created + DAY }] : []),
  ];
  const summary: EnvSummary = {
    environmentId: prod,
    total: svcs.length,
    online: svcs.filter((s) => (s[2] || "online") === "online").length,
    icons: svcs.map(([icon, source]) => ({ icon, kind: source, source })),
    mini: svcs.map(([icon, , status], j) => ({ serviceId: `svc_${pid}_${j}`, icon, status: status || "online", position: [j * 320, (j % 2) * 160] })),
  };
  return { id: pid, name, description: o.description || "", publicRead: false, createdAt: now - o.created, lastActivityAt: now - o.activity, environments, summary: [summary] };
}

const big: Svc[] = [
  ["", "repo"], ["", "repo"], ["", "repo"], ["postgresql", "image"], ["redis", "image"], ["", "repo"], ["", "repo"],
  ["clickhouse", "image"], ["", "repo"], ["", "cron"], ["", "repo"], ["minio", "image"], ["", "repo"], ["", "repo", "deploying"],
  ["grafana", "image"], ["prometheus", "image"], ["", "repo"], ["", "repo"],
];

export const DASH_PROJECTS: Project[] = [
  project(1, "storefront", [["", "repo"], ["postgresql", "image"], ["redis", "image"]], { activity: 4 * 60_000, created: 90 * DAY, staging: true }),
  project(2, "analytics", [["umami", "image"], ["postgresql", "image"]], { activity: 2 * 3_600_000, created: 40 * DAY }),
  project(3, "api-gateway", [["", "repo"]], { activity: 5 * 3_600_000, created: 120 * DAY }),
  project(4, "scrapers", [["", "cli", "crashed"]], { activity: DAY, created: 12 * DAY }),
  project(5, "ml-inference", [["", "repo"], ["redis", "image", "deploying"]], { activity: 2 * DAY, created: 8 * DAY }),
  project(6, "automations", [["n8n", "image"], ["postgresql", "image"]], { activity: 3 * DAY, created: 60 * DAY }),
  project(7, "platform", big, { activity: 4 * DAY, created: 200 * DAY, staging: true }),
  project(8, "ghost-blog", [["ghost", "image"], ["mysql", "image"]], { activity: 9 * DAY, created: 300 * DAY }),
  project(9, "sandbox", [], { activity: 20 * DAY, created: 21 * DAY }),
];

const GB = 1024 ** 3;
const host = (id: string, name: string, roles: string[], used: number, total: number, replicas: number): Host => ({
  id, name, roles, state: "online", version: "0.1.0", arch: "amd64", os: "ubuntu 24.04", cpus: 8, memBytes: 32 * GB, reserveMb: 1024,
  reservedMb: 6144, publicIp: "203.0.113.7", egressIp: "203.0.113.7", meshIp: "10.128.0.1", acmePrimary: id === "hst_dash1",
  disks: [{ mount: "/", used: used * GB, total: total * GB }], replicas, lastSeen: now, cpuUsed: 1.6, memUsed: 9 * GB, rebootRequired: false,
});
export const DASH_HOSTS: Host[] = [
  host("hst_dash1", "control-1", ["control", "build", "ingress", "app"], 61, 160, 14),
  host("hst_dash2", "data-1", ["data"], 212, 500, 9),
];

function sorted(sort: string | null): Project[] {
  const list = [...DASH_PROJECTS];
  if (sort === "name") list.sort((a, b) => a.name.localeCompare(b.name));
  else if (sort === "created") list.sort((a, b) => b.createdAt - a.createdAt);
  else list.sort((a, b) => b.lastActivityAt - a.lastActivityAt);
  return list;
}

/** Page query flags for checking states by hand: `?mock=empty` (no projects), `?mock=slow` (skeletons). */
const flag = (f: string) => typeof window !== "undefined" && new URLSearchParams(window.location.search).getAll("mock").includes(f);

export const dashboardHandlers = [
  http.get("*/api/v1/projects", async ({ request }) => {
    if (flag("slow")) await delay(4000);
    return HttpResponse.json(flag("empty") ? [] : sorted(new URL(request.url).searchParams.get("sort")));
  }),
  http.get("*/api/v1/hosts", () => HttpResponse.json(DASH_HOSTS)),
];

/** Registers the handlers on the running MSW worker once. */
let ready: Promise<void> | null = null;
export function installDashboardMocks(): Promise<void> {
  return (ready ??= import("../../api/mocks/browser").then(({ worker }) => { worker.use(...dashboardHandlers); }));
}
