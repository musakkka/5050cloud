// SPDX-License-Identifier: AGPL-3.0-only

// Stats sub-tab: summary cards, series sparklines, table sizes and top queries.

import type { BackupsResponse, DBMetricsResponse, DataResponse, Service } from "../../api/types.gen";
import { Button, RelTime, Skeleton, cx, fmtBytes } from "../../kit";
import { get, svcPath } from "../service/client";
import { useLoad } from "../service/ui";
import type { Engine } from "./engine";
import { Panel } from "./parts";

const SERIES_LABEL: Record<string, string> = {
  connections_active: "Active connections",
  connections_idle: "Idle connections",
  cache_hit_ratio: "Cache hit rate",
  wal_bytes: "WAL written",
};

function Spark({ points, tone = "var(--pink-600, #ef6aae)" }: { points: number[][]; tone?: string }) {
  const ys = points.map((p) => p[1]);
  if (ys.length < 2) return <div className="h-10" />;
  const lo = Math.min(...ys), hi = Math.max(...ys), span = hi - lo || 1;
  const w = 200, h = 40;
  const d = ys.map((y, i) => `${i ? "L" : "M"}${((i / (ys.length - 1)) * w).toFixed(1)} ${(h - 3 - ((y - lo) / span) * (h - 6)).toFixed(1)}`).join(" ");
  return (
    <svg viewBox={`0 0 ${w} ${h}`} preserveAspectRatio="none" className="h-10 w-full" aria-hidden>
      <path d={`${d} L${w} ${h} L0 ${h} Z`} fill={tone} opacity=".12" />
      <path d={d} fill="none" stroke={tone} strokeWidth="1.5" vectorEffect="non-scaling-stroke" />
    </svg>
  );
}

function Stat({ label, value, sub, loading }: { label: string; value?: React.ReactNode; sub?: React.ReactNode; loading?: boolean }) {
  return (
    <div className="rounded-lg bg-surface px-4 py-4">
      <div className="text-caption text-fg-3">{label}</div>
      {loading ? <Skeleton className="mt-2 h-6 w-20" /> : <div className="mt-1 text-h2 tabular-nums text-foreground">{value ?? "n/a"}</div>}
      {sub && <div className="mt-0.5 text-caption text-fg-3">{sub}</div>}
    </div>
  );
}

export default function StatsView({ eid, service, engine }: { eid: string; service: Service; engine: Engine }) {
  const base = svcPath(eid, service.id);
  const data = useLoad(() => get<DataResponse>(`${base}/data`), [base], 15000);
  const metrics = useLoad(() => get<DBMetricsResponse>(`${base}/db-metrics`), [base], 15000);
  const backups = useLoad(() => get<BackupsResponse>(`${base}/backups`), [base]);
  const d = data.data, m = metrics.data;
  const last = (k: string) => { const s = m?.series[k]; return s && s.length ? s[s.length - 1][1] : undefined; };
  const hit = last("cache_hit_ratio");
  const act = last("connections_active"), idle = last("connections_idle");
  const seriesKeys = Object.keys(m?.series || {}).filter((k) => (m!.series[k] || []).length > 1);
  const tables = [...(d?.tables || [])].sort((a, b) => b.bytes - a.bytes).slice(0, 8);
  const maxBytes = Math.max(1, ...tables.map((t) => t.bytes));

  if (data.error) return (
    <div role="alert" className="rounded-lg border border-danger-line bg-danger-bg p-6 text-center">
      <div className="text-lead font-medium text-danger-text">Failed to load metrics</div>
      <p className="mt-1 text-body text-fg-2">{data.error}</p>
      <Button className="mt-4" onClick={() => void data.reload()}>Retry</Button>
    </div>
  );

  return (
    <div className="flex flex-col gap-6">
      <div className="grid grid-cols-2 gap-3 md:grid-cols-3 xl:grid-cols-5">
        <Stat label="Database size" loading={!d} value={d && fmtBytes(d.sizeBytes)} sub={d && `${engine.label} ${d.version}`} />
        <Stat label="Cache hit rate" loading={!m && !metrics.error} value={hit !== undefined ? `${(hit <= 1 ? hit * 100 : hit).toFixed(1)}%` : undefined} />
        <Stat label="Last backup" loading={!backups.data && !backups.error}
          value={backups.data?.health.lastOk ? <RelTime ts={backups.data.health.lastOk} /> : "Never"} sub={backups.data && !backups.data.health.configured ? "No bucket set" : undefined} />
        <Stat label="Active queries" loading={!m && !metrics.error} value={act !== undefined ? Math.round(act) : undefined} />
        <Stat label="Connections" loading={!d} value={d?.connections} sub={idle !== undefined ? `${Math.round(idle)} idle` : undefined} />
      </div>

      {metrics.error && <p className="text-body text-fg-3">Live series are not available for this database yet. Totals above still update every 15 seconds.</p>}

      {seriesKeys.length > 0 && (
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
          {seriesKeys.map((k) => (
            <Panel key={k} title={SERIES_LABEL[k] || k.replace(/_/g, " ")}>
              <div className="px-4"><Spark points={m!.series[k]} /></div>
            </Panel>
          ))}
        </div>
      )}

      <Panel title={engine.key === "mongodb" ? "Collections" : "Tables"} hint="Largest first, rows are estimates.">
        {!d ? <div className="px-4"><Skeleton className="h-24" /></div> : tables.length === 0 ? <p className="px-4 text-body text-fg-3">Nothing to show yet.</p> : (
          <div>
            {tables.map((t) => (
              <div key={`${t.schema}.${t.name}`} className="grid grid-cols-[1fr_90px_90px] items-center gap-3 border-t border-surface-hover px-4 py-2 text-body first:border-t-0">
                <div className="min-w-0">
                  <div className="truncate font-mono text-foreground">{t.name}</div>
                  <div className="mt-1 h-1 rounded-full bg-surface-hover"><div className="h-1 rounded-full bg-accent" style={{ width: `${Math.max(2, (t.bytes / maxBytes) * 100)}%` }} /></div>
                </div>
                <span className="text-right tabular-nums text-fg-2">{t.rows.toLocaleString()}</span>
                <span className="text-right tabular-nums text-fg-2">{fmtBytes(t.bytes)}</span>
              </div>
            ))}
          </div>
        )}
      </Panel>

      {m && m.topQueries.length > 0 && (
        <Panel title="Query statistics" hint="Slowest by total time.">
          <div className="overflow-x-auto">
            <div className="min-w-[560px]">
              <div className="grid grid-cols-[1fr_70px_90px_80px] gap-3 px-4 pb-2 text-caption font-medium text-fg-3"><span>Query</span><span className="text-right">Calls</span><span className="text-right">Total</span><span className="text-right">Mean</span></div>
              {m.topQueries.map((q, i) => (
                <div key={i} className={cx("grid grid-cols-[1fr_70px_90px_80px] gap-3 border-t border-surface-hover px-4 py-2 text-caption")}>
                  <code className="truncate font-mono text-foreground" title={q.query}>{q.query}</code>
                  <span className="text-right tabular-nums text-fg-2">{q.calls.toLocaleString()}</span>
                  <span className="text-right tabular-nums text-fg-2">{q.totalMs.toFixed(0)} ms</span>
                  <span className="text-right tabular-nums text-fg-2">{q.meanMs.toFixed(1)} ms</span>
                </div>
              ))}
            </div>
          </div>
        </Panel>
      )}
    </div>
  );
}
