// SPDX-License-Identifier: AGPL-3.0-only

// /project/:pid/observability (docs/SPEC.md 13.1): per-service CPU and
// memory sparklines, HTTP error rate, and the hosts this environment uses.

import { Line, LineChart, ResponsiveContainer } from "recharts";
import type { Canvas, MetricsResponse, Service } from "../../api/types.gen";
import { get, svcPath } from "../service/client";
import { Empty, ErrorLine, Loading, NotInV01, useLoad } from "../service/ui";
import { fmtValue, joinSeries } from "./series";

function Spark({ points, color }: { points: number[][] | undefined; color: string }) {
  const rows = joinSeries({ v: points }, ["v"]);
  if (!rows.length) return <div className="h-8 text-xs text-gray-400">No data</div>;
  return (
    <div className="h-8 w-full">
      <ResponsiveContainer width="100%" height="100%">
        <LineChart data={rows}><Line dataKey="v" dot={false} isAnimationActive={false} stroke={color} strokeWidth={1.5} /></LineChart>
      </ResponsiveContainer>
    </div>
  );
}

const last = (p?: number[][]) => (p && p.length ? p[p.length - 1][1] : undefined);

function ServiceRow({ eid, s, onOpen }: { eid: string; s: Service; onOpen?: (sid: string) => void }) {
  const m = useLoad(() => get<MetricsResponse>(`${svcPath(eid, s.id)}/metrics?range=1h`), [eid, s.id], 60000);
  const sr = m.data?.series as Record<string, number[][]> | undefined;
  const cols: [string, string, string][] = [["cpu", "CPU", "#ec4899"], ["mem", "Memory", "#3b82f6"], ["error_rate", "HTTP error rate", "#ef4444"]];
  return (
    <div className="grid grid-cols-1 items-center gap-3 border-b border-gray-200 px-4 py-3 last:border-0 sm:grid-cols-4">
      <button className="truncate text-left text-sm font-medium hover:underline" onClick={() => onOpen?.(s.id)}>{s.config.name}</button>
      {cols.map(([k, label, color]) => (
        <div key={k}>
          <div className="flex justify-between text-xs text-gray-500"><span>{label}</span><span>{last(sr?.[k]) !== undefined ? fmtValue(k, last(sr?.[k])!) : ""}</span></div>
          {m.error ? <div className="text-xs text-red-600">{m.error}</div> : <Spark points={sr?.[k]} color={color} />}
        </div>
      ))}
    </div>
  );
}

export default function Observability({ environmentId, onOpenService }: { environmentId: string; onOpenService?: (sid: string) => void }) {
  const c = useLoad(() => get<Canvas>(`/environments/${encodeURIComponent(environmentId)}/canvas`), [environmentId], 60000);
  if (!c.data) return c.error ? <div className="p-8"><ErrorLine>{c.error}</ErrorLine></div> : <Loading />;
  const hosts = new Map<string, { name: string; services: Set<string> }>();
  for (const s of c.data.services) for (const r of s.replicas) {
    const h = hosts.get(r.hostId) || { name: r.hostName, services: new Set<string>() };
    h.services.add(s.config.name);
    hosts.set(r.hostId, h);
  }
  return (
    <div className="p-4 sm:p-8">
      <h1 className="mb-4 text-2xl font-semibold">Observability</h1>
      {c.data.services.length === 0 ? <Empty>No services in this environment yet.</Empty> : (
        <div className="rounded-lg border border-gray-200">
          {c.data.services.map((s) => <ServiceRow key={s.id} eid={environmentId} s={s} onOpen={onOpenService} />)}
        </div>
      )}
      <h2 className="mb-2 mt-8 text-lg font-semibold">Hosts</h2>
      {hosts.size === 0 ? <Empty>No replicas are running.</Empty> : (
        <div className="divide-y divide-gray-200 rounded-lg border border-gray-200">
          {[...hosts.entries()].map(([id, h]) => (
            <div key={id} className="flex justify-between px-4 py-2 text-sm"><span>{h.name}</span><span className="text-gray-500">{[...h.services].join(", ")}</span></div>
          ))}
        </div>
      )}
      <div className="mt-8"><NotInV01 label="Widget board" instead="use the per-service Metrics tab" /></div>
    </div>
  );
}
