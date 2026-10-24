// SPDX-License-Identifier: AGPL-3.0-only

// The Metrics tab of a host: CPU, memory and network charts from /hosts/{id}/metrics, with per-replica
// lines, a one or two column layout, a time range, and a live button that polls every 5 seconds.
import { useCallback, useEffect, useRef, useState } from "react";
import { Clock, LayoutGrid, Radio, Rows3 } from "lucide-react";
import { Button, IconButton, Listbox, Notice, SegmentedControl, Skeleton, cx, fmtBytes, readPref, writePref, type ListboxOption } from "../../kit";
import type * as T from "../../api/types.gen";
import { hostsApi, messageOf, type HostMetricsRange } from "./client";
import { LineChart, type ChartSeries } from "./Chart";

const RANGES: ListboxOption<HostMetricsRange>[] = [
  { value: "1h", label: "Last hour", icon: <Clock size={16} /> },
  { value: "6h", label: "Last 6 hours", icon: <Clock size={16} /> },
  { value: "24h", label: "Last day", icon: <Clock size={16} /> },
  { value: "7d", label: "Last week", icon: <Clock size={16} /> },
];
const RANGE_VALUES = RANGES.map((r) => r.value);

const ACCENT = "var(--accent)";
const BLUE = "var(--blue-500)";
const REPLICA_COLORS = [BLUE, "var(--ok)", "var(--warn)", "var(--fg-3)", "var(--danger-text)"];
const RANGE_KEY = "5050.hosts.range";
const LAYOUT_KEY = "5050.hosts.metricsLayout";

export function HostMetrics({ hostId, cpus, memBytes }: { hostId: string; cpus: number; memBytes: number }) {
  const [range, setRange] = useState<HostMetricsRange>(() => {
    const v = readPref<string>(RANGE_KEY, "24h");
    return (RANGE_VALUES as string[]).includes(v) ? (v as HostMetricsRange) : "24h";
  });
  const [layout, setLayout] = useState<"list" | "grid">(() => (readPref<string>(LAYOUT_KEY, "grid") === "list" ? "list" : "grid"));
  const [live, setLive] = useState(false);
  const [data, setData] = useState<T.MetricsResponse | null>(null);
  const [error, setError] = useState<string | null>(null);
  const seq = useRef(0);

  const load = useCallback(async () => {
    const id = ++seq.current;
    try {
      const d = await hostsApi.metrics(hostId, range, true);
      if (id === seq.current) { setData(d); setError(null); }
    } catch (e) {
      if (id === seq.current) setError(messageOf(e));
    }
  }, [hostId, range]);

  useEffect(() => { setData(null); void load(); }, [load]);
  useEffect(() => {
    if (!live) return;
    const t = window.setInterval(() => void load(), 5000);
    return () => clearInterval(t);
  }, [live, load]);
  useEffect(() => writePref(RANGE_KEY, range), [range]);
  useEffect(() => writePref(LAYOUT_KEY, layout), [layout]);

  const ready = data !== null;
  const replicaLines = (key: string): ChartSeries[] => {
    if (!data?.perReplica) return [];
    return Object.keys(data.perReplica).map((r, i) => ({
      name: r, color: REPLICA_COLORS[i % REPLICA_COLORS.length], points: data.perReplica?.[r]?.[key] ?? [],
    }));
  };
  const cpuSeries: ChartSeries[] = data ? [{ name: "Sum", color: ACCENT, points: data.series.cpu ?? [], area: true }, ...replicaLines("cpu")] : [];
  const memSeries: ChartSeries[] = data ? [{ name: "Sum", color: ACCENT, points: data.series.mem ?? [], area: true }, ...replicaLines("mem")] : [];
  const netSeries: ChartSeries[] = data ? [
    { name: "Received", color: ACCENT, points: data.series.rx ?? [], area: true },
    { name: "Sent", color: BLUE, points: data.series.tx ?? [] },
  ] : [];

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-end gap-2">
        <SegmentedControl<"list" | "grid">
          label="Chart layout"
          size={34}
          value={layout}
          onChange={setLayout}
          options={[
            { value: "list", label: "One column", icon: <Rows3 size={16} /> },
            { value: "grid", label: "Two columns", icon: <LayoutGrid size={16} /> },
          ]}
        />
        <Listbox<HostMetricsRange> label="Time range" value={range} onChange={setRange} options={RANGES} width={180} />
        <IconButton label={live ? "Stop live updates" : "Live updates"} size={34} active={live} onClick={() => setLive((v) => !v)}
          className={cx(live && "border-green-200 text-ok-soft")}>
          <Radio size={16} className={cx(live && "animate-pulse")} />
        </IconButton>
      </div>
      {error && (
        <Notice tone="danger" title="Metrics unavailable" action={<Button size="xs" variant="outline" onClick={() => void load()}>Retry</Button>}>
          {error}
        </Notice>
      )}
      <div className={cx("grid gap-4", layout === "grid" && "md:grid-cols-2")}>
        <MetricCard title="CPU" series={cpuSeries} ready={ready} yMax={cpus}
          format={(v) => `${v.toFixed(2)} vCPU`} axis={(v) => `${v.toFixed(2)} vCPU`} />
        <MetricCard title="Memory" series={memSeries} ready={ready} yMax={memBytes}
          format={(v) => fmtBytes(v)} axis={(v) => fmtBytes(v, 0)} />
        <MetricCard title="Network" series={netSeries} ready={ready}
          format={(v) => `${fmtBytes(v)}/s`} axis={(v) => `${fmtBytes(v, 0)}/s`} />
      </div>
    </div>
  );
}

function MetricCard({ title, series, ready, yMax, format, axis }: {
  title: string; series: ChartSeries[]; ready: boolean; yMax?: number; format: (v: number) => string; axis: (v: number) => string;
}) {
  const first = series[0]?.points ?? [];
  const latest = first.length ? first[first.length - 1][1] : null;
  return (
    <section className="rounded-lg border border-line bg-surface p-4 pb-3">
      <header className="mb-3 flex items-center justify-between gap-3">
        <h3 className="text-lead font-semibold text-foreground">{title}</h3>
        {latest !== null && <span className="text-caption tabular-nums text-fg-3">{format(latest)} now</span>}
      </header>
      {ready ? (
        <>
          <LineChart series={series} yMax={yMax} format={axis} />
          <ul className="mt-3 flex flex-wrap gap-x-4 gap-y-1.5 text-caption text-fg-menu">
            {series.map((s) => (
              <li key={s.name} className="flex items-center gap-1.5">
                <span aria-hidden className={cx("h-2 w-2 rounded-full", s.area && "border-2")}
                  style={s.area ? { borderColor: s.color } : { background: s.color }} />
                {s.name}
              </li>
            ))}
          </ul>
        </>
      ) : (
        <Skeleton w="100%" h={168} />
      )}
    </section>
  );
}
