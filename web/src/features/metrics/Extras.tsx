// SPDX-License-Identifier: AGPL-3.0-only

// Extras under the charts: Postgres query statistics and threshold monitors (kept from the
// original console; restyled to the shared kit).

import { useState } from "react";
import { Plus, Trash2 } from "lucide-react";
import type { CreateMonitorRequest, DBMetricsResponse, Me, Monitor, Webhook } from "../../api/types.gen";
import { Button, Checkbox, Confirm, EmptyState, Field, IconButton, Input, Modal, Notice, Select, SectionHeader, StatusDot, cx } from "../../kit";
import { del, get, post } from "../service/client";
import { useAction, useLoad } from "../service/ui";
import { MetricChart } from "./chart";
import { demoMetrics, demoMode } from "./demo";


export function PgMetrics({ base, from, until }: { base: string; from: number; until: number }) {
  const m = useLoad(() => get<DBMetricsResponse>(`${base}/db-metrics?from=${Math.round(from)}&until=${Math.round(until)}`), [base, from, until], 60000);
  if (m.error) return <Notice tone="danger">{m.error}</Notice>;
  if (!m.data) return null;
  const s = { ...((m.data.series ?? {}) as Record<string, number[][]>) };
  const top = m.data.topQueries ?? [];
  if (demoMode() && !Object.values(s).some((x) => x.length > 3)) {
    const d = demoMetrics(from, until, 3).series;
    s.connections_active = d.p50; s.connections_idle = d.p90; s.cache_hit_ratio = d.p50.map(([t]) => [t, 0.99]);
  }
  const connKeys = Object.keys(s).filter((k) => k.startsWith("connections_"));
  const colors = ["var(--accent)", "var(--blue-500)", "var(--yellow-400)", "var(--red-300)"];
  return (
    <div className="mt-8">
      <SectionHeader title="Database" />
      <div className="mt-4 grid grid-cols-1 gap-4 min-[760px]:grid-cols-2 min-[760px]:gap-x-6">
        <MetricChart title="Connections" from={from} until={until} minMax={4} height={300} fmtAxis={(v) => String(Math.round(v))} fmtValue={(v) => String(Math.round(v))}
          series={connKeys.map((k, i) => ({ key: k, name: k.replace("connections_", ""), color: colors[i % colors.length], points: s[k] }))} />
        <MetricChart title="Cache hit ratio" from={from} until={until} minMax={1} height={300} fmtAxis={(v) => `${(v * 100).toFixed(0)}%`} fmtValue={(v) => `${(v * 100).toFixed(1)}%`}
          series={[{ key: "chr", name: "Hit ratio", color: "var(--accent)", points: s.cache_hit_ratio || [] }]} />
      </div>
      <h4 className="mb-3 mt-8 text-lead font-semibold text-foreground">Top queries</h4>
      {top.length === 0 ? <EmptyState title="No query statistics yet" body="Statistics appear once the database has served queries." /> : (
        <div className="overflow-x-auto rounded-lg border border-line">
          <table className="w-full text-caption">
            <thead className="bg-tint-header text-left text-fg-3"><tr><th className="p-3 font-medium">Query</th><th className="p-3 font-medium">Calls</th><th className="p-3 font-medium">Total ms</th><th className="p-3 font-medium">Mean ms</th></tr></thead>
            <tbody>
              {top.map((q) => (
                <tr key={q.query} className="border-t border-line text-foreground"><td className="max-w-md truncate p-3 font-mono" title={q.query}>{q.query}</td><td className="p-3 tabular-nums">{q.calls}</td><td className="p-3 tabular-nums">{q.totalMs.toFixed(0)}</td><td className="p-3 tabular-nums">{q.meanMs.toFixed(1)}</td></tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}

const METRICS = [
  { value: "cpu", label: "CPU (% of limit or host)" },
  { value: "memory", label: "Memory (% of limit)" },
  { value: "disk", label: "Disk" },
  { value: "http_5xx_rate", label: "HTTP 5xx rate (% of requests)" },
];

export function Monitors({ base, projectId }: { base: string; projectId: string }) {
  const mons = useLoad(() => get<Monitor[]>(`${base}/monitors`), [base]);
  const [adding, setAdding] = useState(false);
  const [removing, setRemoving] = useState<Monitor | null>(null);
  return (
    <div className="mt-8">
      <SectionHeader title="Monitors" actions={<Button icon={<Plus size={16} />} onClick={() => setAdding(true)}>New monitor</Button>} />
      <div className="mt-4">
        {mons.error && <Notice tone="danger">{mons.error}</Notice>}
        {!mons.data ? null : mons.data.length === 0 ? <EmptyState title="No monitors" body="Add one to get an email or webhook when a threshold is crossed." /> : (
          <div className="divide-y divide-line rounded-lg border border-line">
            {mons.data.map((mo) => (
              <div key={mo.id} className="flex items-center gap-3 px-4 py-3 text-body text-foreground">
                <StatusDot tone={mo.state === "firing" ? "bad" : "ok"} />
                <span className="flex-1">{METRICS.find((x) => x.value === mo.metric)?.label || mo.metric} above {mo.threshold}{mo.unit === "gb" ? " GB" : "%"} for {mo.forMinutes} min</span>
                <span className={cx("text-caption", mo.state === "firing" ? "text-danger-text" : "text-fg-3")}>{mo.state === "firing" ? "Firing" : "OK"}</span>
                <IconButton label="Delete monitor" size={30} onClick={() => setRemoving(mo)}><Trash2 size={16} /></IconButton>
              </div>
            ))}
          </div>
        )}
      </div>
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
      <Button variant="primary" busy={a.busy} disabled={!ok} onClick={async () => { if (await a.run(() => post(`${base}/monitors`, body))) { onDone(); onClose(); } }}>Create</Button>
    </>}>
      <div className="flex flex-col gap-4">
        <Field label="Metric"><Select variant="field" value={metric} onChange={(e) => setMetric(e.target.value)} options={METRICS} /></Field>
        <div className="flex gap-3">
          <Field label="Above" className="flex-1"><Input type="number" value={threshold} onChange={(e) => setThreshold(e.target.value)} /></Field>
          {metric === "disk" && <Field label="Unit"><Select variant="field" value={unit} onChange={(e) => setUnit(e.target.value)} options={[{ value: "pct", label: "%" }, { value: "gb", label: "GB" }]} /></Field>}
          <Field label="For minutes (1 to 60)" className="flex-1"><Input type="number" value={forMinutes} onChange={(e) => setFor(e.target.value)} /></Field>
        </div>
        <div className="flex flex-col gap-2">
          <Checkbox checked={emailMe} onChange={setEmailMe} label="Email me" />
          {(hooks.data || []).map((h) => (
            <Checkbox key={h.id} checked={hookIds.includes(h.id)} label={`Webhook ${h.url}`}
              onChange={(v) => setHookIds(v ? [...hookIds, h.id] : hookIds.filter((x) => x !== h.id))} />
          ))}
        </div>
        {a.error && <Notice tone="danger">{a.error}</Notice>}
      </div>
    </Modal>
  );
}
