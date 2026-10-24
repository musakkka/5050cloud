// SPDX-License-Identifier: AGPL-3.0-only

// Mock log data for the Log Explorer (`npm run dev:mocks`). Deployment-scoped requests are left to
// the deployment mocks; environment-wide requests get a week of lines from four services.

import { http, HttpResponse, type HttpHandler } from "msw";
import type { LogLine } from "../../api/types.gen";

let seq = 0;
const SERVICES = [
  { id: "svc-scanner", name: "queue-worker" },
  { id: "svc-pg", name: "Postgres" },
  { id: "svc-web", name: "web" },
  { id: "svc-redis", name: "Redis" },
];

const mk = (ts: number, svc: number, msg: string, level = "info", stream = "stdout", attrs?: Record<string, unknown>): LogLine => ({
  id: `ex-${String(seq++).padStart(6, "0")}`, ts, msg, level, stream, serviceId: SERVICES[svc].id,
  replica: `rep-${svc + 1}`, deploymentId: `dep-ex-${svc}`, attrs: attrs ?? (level === "info" ? undefined : { level }),
});

const WARN = (n: number) => `WARNING worker.http upstream request failed, attempt ${n}/3: SSLError: HTTPSConnectionPool(host='api.example.com', port=443): Max retries exceeded with url: /v2/orders/9b1f0c2e7d4a4e1f8c3b6a5d2e9f0a1b/sync (Caused by SSLError(SSLError(1, '[SSL: TLSV1_ALERT_INTERNAL_ERROR] tlsv1 alert internal error (_ssl.c:1020)')))`;

function build(): LogLine[] {
  const now = Date.now();
  const week = 7 * 86_400_000;
  const out: LogLine[] = [];
  let r = 7;
  const rnd = () => { r = (r * 1103515245 + 12345) & 0x7fffffff; return r / 0x7fffffff; };
  for (let t = now - week; t < now - 20_000; t += 400_000 + rnd() * 900_000) {
    const k = rnd();
    if (k < 0.35) out.push(mk(t, 0, `batch processed=${Math.floor(rnd() * 30)} failed=0 retried=0 queued=${Math.floor(rnd() * 20)} backlog=${Math.floor(rnd() * 4)}`));
    else if (k < 0.5) out.push(mk(t, 0, `order 0852fef1-363e-4b92-9c08-4dd07e2e09f4: webhook delivery timed out, will retry`));
    else if (k < 0.62) { for (let a = 1; a <= 3; a++) out.push(mk(t + a * 40, 0, WARN(a), "error", "stderr")); }
    else if (k < 0.72) out.push(mk(t, 1, `${new Date(t).toISOString().replace("T", " ").replace("Z", " UTC")} [125] LOG:  checkpoint starting: time`));
    else if (k < 0.8) out.push(mk(t, 1, `checkpoint complete: wrote 3 buffers (0.0%), wrote 1 SLRU buffers; 0 WAL file(s) added, 0 removed, 0 recycled; write=0.310 s, sync=0.026 s, total=0.432 s`, "error", "stderr"));
    else if (k < 0.9) out.push(mk(t, 2, `GET /api/health 200 in ${2 + Math.floor(rnd() * 9)}ms`, "info", "stdout", { method: "GET", path: "/api/health", httpStatus: 200 }));
    else if (k < 0.95) out.push(mk(t, 2, `slow query detected: SELECT * FROM orders WHERE id = $1 took ${400 + Math.floor(rnd() * 900)}ms`, "warn"));
    else out.push(mk(t, 3, `Background saving started by pid ${100 + Math.floor(rnd() * 800)}`, "debug"));
  }
  out.push(mk(now - 9 * 60_000, 0, "Traceback (most recent call last):\n  File \"/app/worker/run.py\", line 88, in process\n    raise RuntimeError(\"upstream unavailable\")\nRuntimeError: upstream unavailable", "error", "stderr"));
  out.push(mk(now - 6 * 60_000, 1, "sending signal SIGTERM to container", "info", "system"));
  out.push(mk(now - 3 * 60_000, 1, "Stopping Container", "info", "system"));
  return out.sort((a, b) => a.ts - b.ts);
}

let cache: LogLine[] | null = null;
const all = () => (cache ??= build());
const own = (d: string | null) => !!d && (d.startsWith("dep-") && !d.startsWith("dep-ex-"));

function sse(make: () => LogLine[] | null, every: number): HttpResponse<ReadableStream> {
  const enc = new TextEncoder();
  let timer: ReturnType<typeof setInterval> | undefined;
  return new HttpResponse(new ReadableStream({
    start(c) {
      c.enqueue(enc.encode(": ok\n\n"));
      timer = setInterval(() => { const a = make(); if (a?.length) c.enqueue(enc.encode(`data: ${JSON.stringify(a)}\n\n`)); }, every);
    },
    cancel() { clearInterval(timer); },
  }), { headers: { "content-type": "text/event-stream", "cache-control": "no-cache" } });
}

export const logExplorerHandlers: HttpHandler[] = [
  http.get("*/api/v1/logs", ({ request }) => {
    const u = new URL(request.url).searchParams;
    if (own(u.get("deployment"))) return undefined;
    let rows = all();
    const around = u.get("around");
    if (around) {
      const i = rows.findIndex((l) => l.id === around);
      return HttpResponse.json({ lines: rows.slice(Math.max(0, i - 20), i + 21), truncated: false });
    }
    const from = Number(u.get("since")) || 0, until = Number(u.get("until")) || Infinity;
    const svcs = u.get("services")?.split(",").filter(Boolean);
    rows = rows.filter((l) => l.ts >= from && l.ts <= until && (!svcs?.length || svcs.includes(l.serviceId || "")));
    const limit = Number(u.get("limit")) || 1000;
    return HttpResponse.json({ lines: rows.slice(-limit), truncated: rows.length > limit });
  }),
  http.get("*/api/v1/logs/stream", ({ request }) => {
    const u = new URL(request.url).searchParams;
    if (own(u.get("deployment"))) return undefined;
    let i = 0;
    return sse(() => {
      i++;
      const bad = i % 4 === 0;
      return [mk(Date.now(), i % 2, bad ? WARN(1 + (i % 3)) : `batch processed=${i} failed=0 retried=0 queued=16 backlog=2`, bad ? "error" : "info", bad ? "stderr" : "stdout")];
    }, 2500);
  }),
];

export const EXPLORER_SERVICES = SERVICES;

/** Registers the handlers on the MSW worker once (mocks mode only). */
let ready: Promise<void> | null = null;
export function installLogMocks(): Promise<void> {
  return (ready ??= import("../../api/mocks/browser").then(({ worker }) => { worker.use(...logExplorerHandlers); }));
}
