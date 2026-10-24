// SPDX-License-Identifier: AGPL-3.0-only

// Service Metrics tab (docs/SPEC.md 13.3): list or grid of chart cards with a synced cursor,
// range picker, live toggle, per-chart Sum/Replicas, Shift+drag zoom with a history entry,
// deployment markers, Postgres extras and monitors.

import { useCallback, useEffect, useMemo, useState } from "react";
import type { MetricsResponse, Service } from "../../api/types.gen";
import { cx } from "../../kit";
import { svcPath } from "../service/client";
import { MetricChart, CursorSync } from "./chart";
import { useServiceMetrics } from "./data";
import { Monitors, PgMetrics } from "./Extras";
import { buildModel, type MetricKind, type View } from "./model";
import { readRange, writeRange, rangeBounds, type RangeSel } from "./series";
import { LayoutToggle, LiveToggle, RangePicker, useLayout } from "./Toolbar";

const DB_TEMPLATES = ["postgres", "mysql", "redis", "mongodb", "mariadb"];

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

/** Sum / Replicas legend toggle in the CPU and Memory headers (hollow dot = off, filled = on). */
function ViewToggle({ value, onChange }: { value: View; onChange: (v: View) => void }) {
  const item = (v: View, label: string) => (
    <button type="button" aria-pressed={value === v} onClick={() => onChange(v)} className="inline-flex items-center gap-2 text-caption text-fg-menu transition-colors hover:text-foreground">
      <span className={cx("h-2 w-2 rounded-full", value === v ? "bg-accent" : "border border-foreground")} />{label}
    </button>
  );
  return <div className="flex items-center gap-3">{item("sum", "Sum")}{item("replicas", "Replicas")}</div>;
}

export default function MetricsTab({ eid, service }: { eid: string; service: Service }) {
  const base = svcPath(eid, service.id);
  const [sel, setSel] = useRangeHistory();
  const [layout, setLayout] = useLayout();
  const [live, setLive] = useState(true);
  const [views, setViews] = useState<Record<string, View>>({ cpu: "replicas", mem: "replicas" });
  const m = useServiceMetrics(base, sel, live);
  const isDb = DB_TEMPLATES.includes(service.config.template || "");
  const bounds = m.loaded ?? { ...rangeBounds(sel), data: undefined as MetricsResponse | undefined };
  const kinds: MetricKind[] = useMemo(
    () => ["cpu", "mem", ...(service.volume ? ["disk" as const] : []), "net", ...(isDb ? [] : ["req" as const, "err" as const, "lat" as const])],
    [service.volume, isDb],
  );
  const markers = (m.loaded?.data.deployments || []).map((d) => ({ ts: d.ts, label: d.commit ? `${d.commit.sha.slice(0, 7)} ${d.commit.message.split("\n")[0]}` : d.label }));
  const zoom = (from: number, until: number) => setSel({ from, until });

  return (
    <div>
      <div className="mb-8 flex flex-wrap items-center justify-end gap-2">
        <LayoutToggle value={layout} onChange={setLayout} />
        <RangePicker value={sel} onChange={setSel} />
        <LiveToggle live={live} onChange={setLive} />
      </div>
      <CursorSync>
        <div className={cx("grid grid-cols-1 gap-4", layout === "grid" && "min-[760px]:grid-cols-2 min-[760px]:gap-x-6")}>
          {kinds.map((k) => {
            const hasView = k === "cpu" || k === "mem";
            const model = m.loaded ? buildModel(k, m.loaded.data, hasView ? views[k] : "replicas") : undefined;
            return (
              <MetricChart key={k}
                title={model?.title ?? TITLES[k]}
                series={model?.series ?? []} kind={model?.kind} minMax={model?.minMax ?? 1}
                fmtAxis={model?.fmtAxis ?? String} fmtValue={model?.fmtValue ?? String} limit={model?.limit}
                from={bounds.from} until={bounds.until}
                loading={!m.loaded && !m.error} error={!m.loaded ? m.error : null} onRetry={m.reload}
                markers={k === "req" ? undefined : markers} onZoom={zoom}
                headerRight={hasView ? <ViewToggle value={views[k]} onChange={(v) => setViews((s) => ({ ...s, [k]: v }))} />
                  : k === "req" && model?.total !== undefined ? <span className="text-body text-fg-4">{Math.round(model.total).toLocaleString()} total</span> : undefined} />
            );
          })}
        </div>
      </CursorSync>
      {isDb && service.config.template === "postgres" && <PgMetrics base={base} from={bounds.from} until={bounds.until} />}
      <Monitors base={base} projectId={service.projectId} />
    </div>
  );
}

const TITLES: Record<MetricKind, string> = {
  cpu: "CPU", mem: "Memory", disk: "Disk", net: "Public Network Traffic", req: "Requests", err: "Request Error Rate", lat: "Response Time",
};
