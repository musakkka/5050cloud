// SPDX-License-Identifier: AGPL-3.0-only

// Host fixtures for `npm run dev:mocks`: six machines in different states with a working join command,
// maintenance, drain, identity conflicts, removal and per-host metrics. The state lives in memory and is
// registered once from HostsSettings (VITE_MOCKS=1), so no shared mock file changes.

import { http, HttpResponse } from "msw";
import type * as T from "../../api/types.gen";

const GiB = 1024 ** 3;
const MIN = 60_000;

type Seed = Pick<T.Host, "id" | "name" | "roles" | "state"> & Partial<T.Host>;

function seed(now: number): T.Host[] {
  const base = (o: Seed): T.Host => ({
    version: "v0.9.4", arch: "amd64", os: "Ubuntu 24.04", cpus: 2, memBytes: 4 * GiB, reserveMb: 512, reservedMb: 0,
    publicIp: "203.0.113.10", egressIp: "203.0.113.10", meshIp: "10.128.0.1", acmePrimary: false,
    disks: [{ mount: "/", used: 12 * GiB, total: 40 * GiB }], replicas: 0, lastSeen: now - 4000,
    cpuUsed: 0.2, memUsed: 1.2 * GiB, rebootRequired: false, ...o,
  });
  return [
    base({
      id: "host-control", name: "control", roles: ["control"], state: "online", cpus: 4, memBytes: 8 * GiB,
      cpuUsed: 0.9, memUsed: 3.1 * GiB, reserveMb: 1024, reservedMb: 0, acmePrimary: true, rebootRequired: true,
      publicIp: "203.0.113.10", egressIp: "203.0.113.10", meshIp: "10.128.0.1", lastSeen: now - 4000,
      disks: [{ mount: "/", used: 18.4 * GiB, total: 40 * GiB }, { mount: "/data", used: 62 * GiB, total: 200 * GiB }],
      warnings: ["The root disk is above 45 percent. Free space before the next build."],
    }),
    base({
      id: "host-app-1", name: "app-1", roles: ["app", "ingress"], state: "online", cpus: 2, memBytes: 4 * GiB,
      cpuUsed: 1.7, memUsed: 2.6 * GiB, reservedMb: 1536, replicas: 6, publicIp: "203.0.113.21", meshIp: "10.128.0.2",
      egressIp: "203.0.113.21", lastSeen: now - 9000, disks: [{ mount: "/", used: 9 * GiB, total: 40 * GiB }],
    }),
    base({
      id: "host-app-2", name: "app-2", roles: ["app"], state: "draining", cpus: 2, memBytes: 4 * GiB,
      cpuUsed: 0.3, memUsed: 1.1 * GiB, reservedMb: 512, replicas: 2, publicIp: "203.0.113.22", meshIp: "10.128.0.3",
      egressIp: "203.0.113.22", version: "v0.9.3", lastSeen: now - 14_000,
    }),
    base({
      id: "host-build-1", name: "build-1", roles: ["build"], state: "maintenance", arch: "arm64", os: "Debian 12",
      cpus: 8, memBytes: 16 * GiB, cpuUsed: 0, memUsed: 2 * GiB, reservedMb: 0, replicas: 0,
      publicIp: "203.0.113.31", meshIp: "10.128.0.4", egressIp: "203.0.113.31", lastSeen: now - 3 * MIN,
    }),
    base({
      id: "host-data-1", name: "data-1", roles: ["data"], state: "online", cpus: 4, memBytes: 8 * GiB,
      cpuUsed: 2.2, memUsed: 7.6 * GiB, reservedMb: 4096, replicas: 3, publicIp: "203.0.113.41", meshIp: "10.128.0.5",
      egressIp: "203.0.113.41", lastSeen: now - 6000, disks: [{ mount: "/data", used: 150 * GiB, total: 200 * GiB }],
      unreachable: ["host-app-2"], warnings: ["Memory is nearly full. Replicas that need more memory may fail to start."],
      conflicts: [{ machineId: "m_7f2c9", wgPublicKey: "k8Xq2mN4", ip: "203.0.113.88", seenAt: now - 10 * MIN }],
    }),
    base({
      id: "host-edge-old", name: "edge-old", roles: ["ingress"], state: "lost", cpus: 2, memBytes: 2 * GiB,
      cpuUsed: 0, memUsed: 0, reservedMb: 0, replicas: 0, publicIp: "198.51.100.7", meshIp: "10.128.0.9",
      egressIp: "198.51.100.7", version: "v0.9.1", lastSeen: now - 3 * 24 * 60 * MIN,
    }),
  ];
}

const RANGE_MS: Record<string, number> = { "1h": 3_600_000, "6h": 21_600_000, "24h": 86_400_000, "7d": 604_800_000 };

/** Deterministic wave for the fixtures: a daily shape with a little texture. */
const wave = (i: number, k: number) => 0.5 + 0.35 * Math.sin(i / k) + 0.15 * Math.sin(i * 1.7 + k);

function metricsFor(host: T.Host, range: string, perReplica: boolean): T.MetricsResponse {
  const span = RANGE_MS[range] ?? RANGE_MS["24h"];
  const n = 60;
  const now = Date.now();
  const at = (i: number) => now - span + (span * i) / n;
  const pts = (f: (i: number) => number) => Array.from({ length: n + 1 }, (_, i) => [at(i), Math.max(0, f(i))]);
  const replicas = Math.max(1, Math.min(3, host.replicas || 1));
  const replicaNames = Array.from({ length: replicas }, (_, r) => `${host.name}-${r}`);
  const out: T.MetricsResponse = {
    step: Math.round(span / n),
    series: {
      cpu: pts((i) => host.cpuUsed * wave(i, 9)),
      mem: pts((i) => host.memUsed * (0.92 + 0.08 * wave(i, 13))),
      rx: pts((i) => 180_000 + 90_000 * wave(i, 7)),
      tx: pts((i) => 60_000 + 35_000 * wave(i + 3, 5)),
    },
    cpuLimit: host.cpus,
    memLimit: host.memBytes,
    deployments: [],
  };
  if (perReplica && host.replicas > 0) {
    out.perReplica = Object.fromEntries(replicaNames.map((name, r) => [name, {
      cpu: pts((i) => (host.cpuUsed / replicas) * wave(i + r * 4, 9)),
      mem: pts((i) => (host.memUsed / replicas) * (0.92 + 0.08 * wave(i + r * 4, 13))),
    }]));
  }
  return out;
}

let installed: Promise<unknown> | undefined;

/** Registers the host handlers on the MSW worker once. Requests not handled here fall through to the generated mocks. */
export function installHostMocks(): Promise<unknown> {
  installed ??= import("../../api/mocks/browser").then(({ worker }) => {
    const hosts = seed(Date.now());
    const find = (id: string) => hosts.find((h) => h.id === id);
    const notFound = () => HttpResponse.json({ error: "No such host.", code: "not_found" }, { status: 404 });
    worker.use(
      http.get("*/api/v1/hosts", () => HttpResponse.json(hosts)),
      http.post("*/api/v1/hosts/join-tokens", async ({ request }) => {
        const body = (await request.json().catch(() => ({}))) as T.CreateJoinTokenRequest;
        const token = `jt_${Math.random().toString(36).slice(2, 12)}`;
        const roles = (body.roles?.length ? body.roles : ["app"]).join(",");
        return HttpResponse.json({
          command: `curl -fsSL https://5050cloud.com/install.sh | sudo sh -s -- --join https://console.example.test --token ${token} --roles ${roles}`,
          token,
          expiresAt: Date.now() + 15 * MIN,
        } satisfies T.JoinToken);
      }),
      http.patch("*/api/v1/hosts/:id", async ({ params, request }) => {
        const h = find(String(params.id));
        if (!h) return notFound();
        const b = (await request.json()) as T.UpdateHostRequest;
        if (b.name !== undefined) h.name = b.name;
        if (b.roles !== undefined) h.roles = b.roles;
        if (b.reserveMb !== undefined) h.reserveMb = b.reserveMb;
        if (b.drain !== undefined) h.state = b.drain ? "draining" : "online";
        return HttpResponse.json(h);
      }),
      http.post("*/api/v1/hosts/:id/maintenance", async ({ params, request }) => {
        const h = find(String(params.id));
        if (!h) return notFound();
        const b = (await request.json()) as T.MaintenanceRequest;
        h.state = b.on ? "maintenance" : "online";
        return HttpResponse.json(h);
      }),
      http.post("*/api/v1/hosts/:id/readopt", ({ params }) => {
        const h = find(String(params.id));
        if (!h) return notFound();
        h.state = "online";
        h.lastSeen = Date.now();
        return HttpResponse.json(h);
      }),
      http.delete("*/api/v1/hosts/:id/conflicts", ({ params }) => {
        const h = find(String(params.id));
        if (!h) return notFound();
        h.conflicts = [];
        return HttpResponse.json(h);
      }),
      http.delete("*/api/v1/hosts/:id", ({ params, request }) => {
        const i = hosts.findIndex((h) => h.id === String(params.id));
        if (i < 0) return notFound();
        const force = new URL(request.url).searchParams.get("force") === "true" || new URL(request.url).searchParams.get("force") === "1";
        if (hosts[i].replicas > 0 && !force) {
          return HttpResponse.json({ error: "This host still runs replicas. Drain it first, or remove it anyway.", code: "conflict" }, { status: 409 });
        }
        hosts.splice(i, 1);
        return new HttpResponse(null, { status: 204 });
      }),
      http.get("*/api/v1/hosts/:id/metrics", ({ params, request }) => {
        const h = find(String(params.id));
        if (!h) return notFound();
        const url = new URL(request.url);
        return HttpResponse.json(metricsFor(h, url.searchParams.get("range") ?? "24h", url.searchParams.get("view") === "replica"));
      }),
    );
  });
  return installed;
}
