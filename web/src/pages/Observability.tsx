// SPDX-License-Identifier: AGPL-3.0-only

import { useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { Area, AreaChart, ResponsiveContainer } from "recharts";
import { api } from "../api";
import { ResourceIcon, Spinner, StatusGlyph, cx, fmtBytes, usePoll } from "../ui/kit";
import { useProject } from "./ProjectLayout";
import { MetricChart, fmtCpu, fmtMem } from "./ServicePanel";

const RANGES: [number, string][] = [[3600, "Last hour"], [6 * 3600, "Last 6 hours"], [86400, "Last day"], [7 * 86400, "Last week"]];

export default function ObservabilityPage() {
  const { canvas, pid, withEnv } = useProject();
  const [range, setRange] = useState(3600);
  const nodes = useMemo(() => (canvas?.nodes || []).filter((n) => n.kind === "service"), [canvas]);
  const keys = nodes.map((n) => `svc:${n.id.slice(8)}`);
  const hosts = usePoll(api.hosts, [], 30000).data;
  const hostKeys = (hosts || []).map((h) => `host:${h.role}`);
  const { data } = usePoll(() => (keys.length ? api.seriesMany([...keys, ...hostKeys], range) : Promise.resolve(null)), [keys.join(","), hostKeys.join(","), range], 30000);
  if (!canvas) return <div className="h-full flex items-center justify-center"><Spinner /></div>;
  const last = (k: string, i: number) => { const s = data?.series[k]; return s && s.length ? s[s.length - 1][i] : 0; };
  const sorted = [...nodes].sort((a, b) => last(`svc:${b.id.slice(8)}`, 2) - last(`svc:${a.id.slice(8)}`, 2));
  return (
    <div className="h-full overflow-y-auto scroll-thin">
      <div className="flex items-center justify-between px-6 py-4 border-b border-gray-200 sticky top-0 bg-background z-10">
        <h2 className="font-semibold">Resource usage</h2>
        <div className="flex rounded-md border border-gray-200 p-0.5">
          {RANGES.map(([v, l]) => <button key={v} onClick={() => setRange(v)} className={cx("px-3 h-7 rounded text-sm", v === range ? "bg-gray-100 text-foreground" : "text-gray-500 hover:text-foreground")}>{l}</button>)}
        </div>
      </div>
      <div className="p-6 space-y-6">
        <div className="grid grid-cols-1 xl:grid-cols-2 gap-4">
          {(hosts || []).map((h) => (
            <div key={h.role} className="space-y-4">
              <MetricChart title={`${h.name} CPU`} right={`${h.cpus || "?"} vCPU · ${h.containers} containers`} points={(data?.series[`host:${h.role}`] || []).map((p) => [p[0], p[1] * 100 / 100, p[2], 0, 0])} idx={1} fmt={(v) => `${(v * 100).toFixed(0)}%`} height={150} />
              <div className="grid grid-cols-3 gap-3 text-sm">
                <Stat label="Memory" value={`${fmtBytes(last(`host:${h.role}`, 2), 1)} / ${fmtBytes(h.mem_total, 0)}`} frac={last(`host:${h.role}`, 2) / (h.mem_total || 1)} />
                {h.disks?.map((d: any) => <Stat key={d.mount} label={`Disk ${d.mount}`} value={`${fmtBytes(d.used, 0)} / ${fmtBytes(d.size, 0)}`} frac={d.used / d.size} />)}
              </div>
            </div>
          ))}
        </div>
        <div className="rounded-xl border border-gray-200 overflow-hidden">
          <div className="grid grid-cols-[minmax(0,1fr)_200px_200px_90px] gap-4 px-5 py-2.5 bg-secondaryBg text-xs font-medium text-gray-600 border-b border-gray-200">
            <span>Service</span><span>CPU</span><span>Memory</span><span className="text-right">Host</span>
          </div>
          {sorted.map((n) => {
            const k = `svc:${n.id.slice(8)}`;
            const s = data?.series[k] || [];
            return (
              <Link key={n.id} to={withEnv(`/project/${pid}/service/${encodeURIComponent(n.id)}/metrics`)} className="grid grid-cols-[minmax(0,1fr)_200px_200px_90px] gap-4 items-center px-5 py-2.5 border-b border-gray-100 last:border-b-0 hover:bg-gray-50">
                <span className="flex items-center gap-3 min-w-0"><StatusGlyph status={n.status} size={16} /><ResourceIcon icon={n.icon} kind={n.kind} source={n.source} size={16} /><span className="truncate text-sm">{n.name}</span></span>
                <Spark points={s} idx={1} label={fmtCpu(last(k, 1))} color="hsl(270, 70%, 65%)" />
                <Spark points={s} idx={2} label={fmtMem(last(k, 2))} color="hsl(220, 80%, 65%)" />
                <span className="text-right text-xs text-gray-500 truncate">{n.host}</span>
              </Link>
            );
          })}
        </div>
      </div>
    </div>
  );
}

function Stat({ label, value, frac }: { label: string; value: string; frac: number }) {
  return (
    <div className="rounded-lg border border-gray-200 px-3 py-2.5">
      <div className="text-xs text-gray-500">{label}</div>
      <div className="text-sm mt-0.5">{value}</div>
      <div className="mt-2 h-1 rounded bg-gray-100 overflow-hidden"><div className={cx("h-full", frac > 0.85 ? "bg-red-500" : frac > 0.75 ? "bg-yellow-500" : "bg-pink-500")} style={{ width: `${Math.min(100, frac * 100)}%` }} /></div>
    </div>
  );
}

function Spark({ points, idx, label, color }: { points: number[][]; idx: number; label: string; color: string }) {
  const d = points.map((p) => ({ v: p[idx] }));
  return (
    <div className="flex items-center gap-3">
      <div className="w-[90px] h-[26px]">
        {d.length > 1 && <ResponsiveContainer><AreaChart data={d}><Area dataKey="v" stroke={color} fill={color} fillOpacity={0.12} strokeWidth={1.2} isAnimationActive={false} /></AreaChart></ResponsiveContainer>}
      </div>
      <span className="text-xs text-gray-700 tabular-nums">{label}</span>
    </div>
  );
}
