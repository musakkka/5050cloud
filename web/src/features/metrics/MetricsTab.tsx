// SPDX-License-Identifier: AGPL-3.0-only

// Metrics tab (docs/SPEC.md 13.3): synced cursors, deployment markers,
// Shift+drag zoom with a history entry, ranges, custom range, Sum/Replica,
// monitors, and the Postgres extras.

import { useCallback, useEffect, useMemo, useState } from "react";
import { CartesianGrid, Line, LineChart, ReferenceArea, ReferenceLine, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts";
import type { CreateMonitorRequest, DBMetricsResponse, DeployMarker, Me, MetricsResponse, Monitor, Service, Webhook } from "../../api/types.gen";
import { del, get, post, qs, svcPath } from "../service/client";
import { Button, Confirm, Empty, ErrorLine, Input, Loading, Modal, Section, cx, useAction, useLoad } from "../service/ui";
import { RANGES, fmtValue, joinSeries, parseRelative, readRange, writeRange, type RangeSel } from "./series";

const COLORS = ["#ec4899", "#3b82f6", "#22c55e", "#eab308", "#a855f7", "#14b8a6", "#f97316", "#64748b"];

function useRangeHistory(): [RangeSel, (s: RangeSel) => void] {
  const [sel, setSel] = useState<RangeSel>(() => readRange(window.location.search));
  useEffect(() => {
    const h = () => setSel(readRange(window.location.search));
    window.addEventListener("popstate", h);
    return () => window.removeEventListener("popstate", h);
  }, []);
  const push = useCallback((s: RangeSel) => {
    const url = window.location.pathname + writeRange(window.location.search, s) + window.location.hash;
    window.history.pushState(window.history.state, "", url);
    setSel(s);
  }, []);
  return [sel, push];
}

export default function MetricsTab({ eid, service }: { eid: string; service: Service }) {
  const base = svcPath(eid, service.id);
  const [sel, setSel] = useRangeHistory();
  const [view, setView] = useState<"sum" | "replica">("sum");
  const [custom, setCustom] = useState(false);
  const q = qs({ range: sel.from ? undefined : sel.range, from: sel.from, until: sel.until, view });
  const m = useLoad(() => get<MetricsResponse>(`${base}/metrics${q}`), [base, q], 60000);
  const isPg = service.config.template === "postgres";

  return (
    <div>
      <div className="mb-4 flex flex-wrap items-center gap-2">
        <div className="flex overflow-hidden rounded-md border border-gray-200 text-sm">
          {RANGES.map((r) => (
            <button key={r} onClick={() => setSel({ range: r })}
              className={cx("px-2.5 py-1", !sel.from && sel.range === r ? "bg-gray-200 text-foreground" : "text-gray-600 hover:bg-gray-100")}>{r}</button>
          ))}
          <button onClick={() => setCustom(true)} className={cx("px-2.5 py-1", sel.from ? "bg-gray-200" : "text-gray-600 hover:bg-gray-100")}>Custom</button>
        </div>
        <div className="flex overflow-hidden rounded-md border border-gray-200 text-sm">
          {(["sum", "replica"] as const).map((v) => (
            <button key={v} onClick={() => setView(v)} className={cx("px-2.5 py-1 capitalize", view === v ? "bg-gray-200" : "text-gray-600")}>{v === "sum" ? "Sum" : "Replica"}</button>
          ))}
        </div>
        {sel.from && sel.until && <span className="text-xs text-gray-500">{new Date(sel.from).toLocaleString()} to {new Date(sel.until).toLocaleString()}</span>}
        <span className="text-xs text-gray-500">Shift+drag on a chart to zoom; Back restores the range.</span>
      </div>
      {m.error && <ErrorLine>{m.error}</ErrorLine>}
      {!m.data ? (!m.error && <Loading />) : <Charts data={m.data} view={view} hasVolume={!!service.volume} onZoom={(from, until) => setSel({ from, until })} />}
      {isPg && <PgMetrics base={base} range={sel.range || "1h"} />}
      <Monitors base={base} projectId={service.projectId} />
      {custom && <CustomRange onClose={() => setCustom(false)} onApply={(from, until) => { setSel({ from, until }); setCustom(false); }} />}
    </div>
  );
}

interface ChartDef { title: string; keys: string[]; limit?: number; fmt: string }

function Charts({ data, view, hasVolume, onZoom }: { data: MetricsResponse; view: "sum" | "replica"; hasVolume: boolean; onZoom: (from: number, until: number) => void }) {
  const defs: ChartDef[] = [
    { title: `CPU${data.cpuLimit ? ` (limit ${data.cpuLimit} vCPU)` : ""}`, keys: ["cpu"], limit: data.cpuLimit, fmt: "cpu" },
    { title: `Memory${data.memLimit ? ` (limit ${fmtValue("mem", data.memLimit)})` : ""}`, keys: ["mem"], limit: data.memLimit, fmt: "mem" },
    ...(hasVolume ? [{ title: "Disk (volume)", keys: ["disk"], fmt: "disk" }] : []),
    { title: "Network in/out", keys: ["rx", "tx"], fmt: "rx" },
    { title: "HTTP requests", keys: ["req_2xx", "req_3xx", "req_4xx", "req_5xx"], fmt: "req_" },
    { title: "Error Rate", keys: ["error_rate"], fmt: "error_rate" },
    { title: "Response Time", keys: ["p50", "p90", "p95", "p99"], fmt: "p" },
  ];
  return (
    <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
      {defs.map((d) => {
        let series = data.series as Record<string, number[][]>;
        let keys = d.keys;
        if (view === "replica" && data.perReplica && (d.fmt === "cpu" || d.fmt === "mem")) {
          series = {};
          keys = [];
          for (const [rid, s] of Object.entries(data.perReplica)) {
            const k = `${d.keys[0]}:${rid.slice(0, 8)}`;
            series[k] = s[d.keys[0]] || [];
            keys.push(k);
          }
        }
        return <Chart key={d.title} def={{ ...d, keys }} series={series} markers={data.deployments} onZoom={onZoom} />;
      })}
    </div>
  );
}

function Chart({ def, series, markers, onZoom }: { def: ChartDef; series: Record<string, number[][]>; markers: DeployMarker[]; onZoom: (a: number, b: number) => void }) {
  const rows = useMemo(() => joinSeries(series, def.keys), [series, def.keys]);
  const [drag, setDrag] = useState<{ a: number; b?: number } | null>(null);
  const fmt = (v: number) => fmtValue(def.fmt === "p" ? "p50" : def.fmt === "req_" ? "req_2xx" : def.fmt, v);
  const empty = rows.length === 0;
  return (
    <div className="rounded-lg border border-gray-200 p-3">
      <div className="mb-2 text-sm font-medium">{def.title}</div>
      {empty ? <div className="flex h-40 items-center justify-center text-xs text-gray-500">No data in this range</div> : (
        <div className="h-40 select-none">
          <ResponsiveContainer width="100%" height="100%">
            <LineChart data={rows} syncId="svc-metrics" syncMethod="value"
              onMouseDown={(e: any, ev: any) => { if (ev?.shiftKey && e?.activeLabel) setDrag({ a: Number(e.activeLabel) }); }}
              onMouseMove={(e: any) => { if (drag && e?.activeLabel) setDrag({ ...drag, b: Number(e.activeLabel) }); }}
              onMouseUp={() => {
                if (drag?.b !== undefined && drag.b !== drag.a) onZoom(Math.min(drag.a, drag.b), Math.max(drag.a, drag.b));
                setDrag(null);
              }}>
              <CartesianGrid strokeDasharray="3 3" stroke="var(--gray-200)" />
              <XAxis dataKey="ts" type="number" domain={["dataMin", "dataMax"]} scale="time" tick={{ fontSize: 10 }}
                tickFormatter={(t) => new Date(t).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })} />
              <YAxis tick={{ fontSize: 10 }} width={64} tickFormatter={fmt} />
              <Tooltip labelFormatter={(t) => new Date(Number(t)).toLocaleString()} formatter={(v: number, n: string) => [fmt(v), n]}
                contentStyle={{ background: "var(--secondaryBg)", border: "1px solid var(--gray-200)", fontSize: 12 }} />
              {def.limit ? <ReferenceLine y={def.limit} stroke="#ef4444" strokeDasharray="4 4" /> : null}
              {markers.map((mk) => (
                <ReferenceLine key={mk.deploymentId + mk.ts} x={mk.ts} stroke="#a855f7" strokeDasharray="2 2"
                  label={(p: any) => (
                    <g>
                      <circle cx={p.viewBox?.x} cy={(p.viewBox?.y ?? 0) + 4} r={4} fill="#a855f7">
                        <title>{mk.commit ? `${mk.commit.sha.slice(0, 7)} ${mk.commit.message.split("\n")[0]}` : mk.label}</title>
                      </circle>
                    </g>
                  )} />
              ))}
              {def.keys.map((k, i) => <Line key={k} dataKey={k} dot={false} isAnimationActive={false} stroke={COLORS[i % COLORS.length]} strokeWidth={1.5} />)}
              {drag?.b !== undefined && <ReferenceArea x1={drag.a} x2={drag.b} fill="#ec4899" fillOpacity={0.15} />}
            </LineChart>
          </ResponsiveContainer>
        </div>
      )}
    </div>
  );
}

function CustomRange({ onClose, onApply }: { onClose: () => void; onApply: (from: number, until: number) => void }) {
  const [from, setFrom] = useState("2 hours ago");
  const [until, setUntil] = useState("now");
  const f = parseRelative(from);
  const u = parseRelative(until);
  const ok = f !== null && u !== null && u > f;
  return (
    <Modal title="Custom range" onClose={onClose} footer={<>
      <Button onClick={onClose}>Cancel</Button>
      <Button variant="primary" disabled={!ok} onClick={() => ok && onApply(f!, u!)}>Apply</Button>
    </>}>
      <label className="mb-3 block">From<Input value={from} onChange={(e) => setFrom(e.target.value)} placeholder="2 hours ago or 2026-10-01 09:00" /></label>
      <label className="block">To<Input value={until} onChange={(e) => setUntil(e.target.value)} placeholder="now" /></label>
      <div className="mt-2 flex gap-2">
        <label className="text-xs text-gray-500">Or pick dates:
          <input type="datetime-local" className="ml-2 rounded border border-gray-200 bg-background px-1" onChange={(e) => setFrom(e.target.value)} />
          <input type="datetime-local" className="ml-2 rounded border border-gray-200 bg-background px-1" onChange={(e) => setUntil(e.target.value)} />
        </label>
      </div>
      {!ok && <div className="mt-2 text-xs text-red-600">Enter a start before the end, for example "2 hours ago" to "now".</div>}
    </Modal>
  );
}

function PgMetrics({ base, range }: { base: string; range: string }) {
  const m = useLoad(() => get<DBMetricsResponse>(`${base}/db-metrics?range=${encodeURIComponent(range)}`), [base, range], 60000);
  if (m.error) return <ErrorLine>{m.error}</ErrorLine>;
  if (!m.data) return null;
  const s = m.data.series as Record<string, number[][]>;
  const connKeys = Object.keys(s).filter((k) => k.startsWith("connections_"));
  return (
    <div className="mt-6">
      <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
        <Chart def={{ title: "Connections", keys: connKeys, fmt: "n" }} series={s} markers={[]} onZoom={() => undefined} />
        <Chart def={{ title: "Cache hit ratio", keys: ["cache_hit_ratio"], fmt: "error_rate" }} series={s} markers={[]} onZoom={() => undefined} />
      </div>
      <h4 className="mb-2 mt-6 text-sm font-medium">Top queries</h4>
      {m.data.topQueries.length === 0 ? <Empty>No query statistics yet.</Empty> : (
        <div className="overflow-x-auto rounded border border-gray-200">
          <table className="w-full text-xs">
            <thead className="text-left text-gray-500"><tr><th className="p-2">Query</th><th className="p-2">Calls</th><th className="p-2">Total ms</th><th className="p-2">Mean ms</th></tr></thead>
            <tbody>
              {m.data.topQueries.map((q) => (
                <tr key={q.query} className="border-t border-gray-200"><td className="max-w-md truncate p-2 font-mono" title={q.query}>{q.query}</td><td className="p-2">{q.calls}</td><td className="p-2">{q.totalMs.toFixed(0)}</td><td className="p-2">{q.meanMs.toFixed(1)}</td></tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}

const METRICS = [
  { id: "cpu", label: "CPU (% of limit or host)" },
  { id: "memory", label: "Memory (% of limit)" },
  { id: "disk", label: "Disk" },
  { id: "http_5xx_rate", label: "HTTP 5xx rate (% of requests)" },
];

function Monitors({ base, projectId }: { base: string; projectId: string }) {
  const mons = useLoad(() => get<Monitor[]>(`${base}/monitors`), [base]);
  const [adding, setAdding] = useState(false);
  const [removing, setRemoving] = useState<Monitor | null>(null);
  return (
    <div className="mt-8">
      <Section title="Monitors" actions={<Button onClick={() => setAdding(true)}>New monitor</Button>}>
        {mons.error && <ErrorLine>{mons.error}</ErrorLine>}
        {!mons.data ? null : mons.data.length === 0 ? <Empty>No monitors. Add one to get an email or webhook when a threshold is crossed.</Empty> : (
          <div className="divide-y divide-gray-200 rounded-lg border border-gray-200">
            {mons.data.map((mo) => (
              <div key={mo.id} className="flex items-center gap-3 px-4 py-2 text-sm">
                <span className={cx("h-2 w-2 rounded-full", mo.state === "firing" ? "bg-red-500" : "bg-green-500")} />
                <span className="flex-1">{METRICS.find((x) => x.id === mo.metric)?.label || mo.metric} above {mo.threshold}{mo.unit === "gb" ? " GB" : "%"} for {mo.forMinutes} min</span>
                <span className="text-xs text-gray-500">{mo.state === "firing" ? "Firing" : "OK"}</span>
                <Button variant="ghost" onClick={() => setRemoving(mo)}>Delete</Button>
              </div>
            ))}
          </div>
        )}
      </Section>
      {adding && <NewMonitor base={base} projectId={projectId} onClose={() => setAdding(false)} onDone={() => void mons.reload()} />}
      {removing && <Confirm title="Delete monitor" body="Stop watching this threshold?" confirm="Delete" danger onClose={() => setRemoving(null)}
        onConfirm={async () => { await del(`/monitors/${encodeURIComponent(removing.id)}`); await mons.reload(); }} />}
    </div>
  );
}

function NewMonitor({ base, projectId, onClose, onDone }: { base: string; projectId: string; onClose: () => void; onDone: () => void }) {
  const me = useLoad(() => get<Me>("/me"), []);
  const hooks = useLoad(() => get<Webhook[]>(`/projects/${encodeURIComponent(projectId)}/webhooks`).catch(() => [] as Webhook[]), [projectId]);
  const [emailMe, setEmailMe] = useState(true);
  const [hookIds, setHookIds] = useState<string[]>([]);
  const [metric, setMetric] = useState("cpu");
  const [threshold, setThreshold] = useState("80");
  const [unit, setUnit] = useState("pct");
  const [forMinutes, setFor] = useState("5");
  const a = useAction();
  const body: CreateMonitorRequest = { metric, threshold: Number(threshold), unit: metric === "disk" ? unit : "pct", forMinutes: Number(forMinutes),
    channels: { users: emailMe && me.data ? [me.data.user.id] : [], webhooks: hookIds } };
  const ok = body.threshold > 0 && body.forMinutes >= 1 && body.forMinutes <= 60;
  return (
    <Modal title="New monitor" onClose={onClose} footer={<>
      <Button onClick={onClose}>Cancel</Button>
      <Button variant="primary" disabled={!ok || a.busy} onClick={async () => { if (await a.run(() => post(`${base}/monitors`, body))) { onDone(); onClose(); } }}>Create</Button>
    </>}>
      <label className="mb-3 block">Metric
        <select value={metric} onChange={(e) => setMetric(e.target.value)} className="mt-1 h-[34px] w-full rounded-md border border-gray-200 bg-background px-2">
          {METRICS.map((x) => <option key={x.id} value={x.id}>{x.label}</option>)}
        </select>
      </label>
      <div className="flex gap-3">
        <label className="flex-1">Above<Input type="number" value={threshold} onChange={(e) => setThreshold(e.target.value)} /></label>
        {metric === "disk" && (
          <label>Unit<select value={unit} onChange={(e) => setUnit(e.target.value)} className="mt-1 h-[34px] rounded-md border border-gray-200 bg-background px-2"><option value="pct">%</option><option value="gb">GB</option></select></label>
        )}
        <label className="flex-1">For minutes (1 to 60)<Input type="number" value={forMinutes} onChange={(e) => setFor(e.target.value)} /></label>
      </div>
      <div className="mt-3 flex flex-col gap-1">
        <label className="flex items-center gap-2"><input type="checkbox" checked={emailMe} onChange={(e) => setEmailMe(e.target.checked)} />Email me</label>
        {(hooks.data || []).map((h) => (
          <label key={h.id} className="flex items-center gap-2"><input type="checkbox" checked={hookIds.includes(h.id)}
            onChange={(e) => setHookIds(e.target.checked ? [...hookIds, h.id] : hookIds.filter((x) => x !== h.id))} />Webhook {h.url}</label>
        ))}
      </div>
      <ErrorLine>{a.error}</ErrorLine>
    </Modal>
  );
}
