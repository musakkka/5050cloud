// SPDX-License-Identifier: AGPL-3.0-only

import { useEffect, useMemo, useState } from "react";
import type * as T from "../../api/types.gen";
import { Select, Skeleton, cx, fmtBytes } from "../../kit";
import { call, v1 } from "../settings/client";
import { usageRange } from "../settings/forms";
import { LoadError, SectionHead, useLoad } from "./ui";

const PERIODS = [{ value: "month", label: "This month" }, { value: "last-month", label: "Last month" }, { value: "30d", label: "Last 30 days" }];
// Public list prices used to turn quantities into a comparable figure.
const RATE = { mem: 0.01386, cpu: 0.02778, egress: 0.05, volume: 0.15 };
const money = (x: number, d = 2) => x.toLocaleString(undefined, { style: "currency", currency: "USD", minimumFractionDigits: d, maximumFractionDigits: d });
const num = (x: number, d = 2) => x.toLocaleString(undefined, { minimumFractionDigits: d, maximumFractionDigits: d });

interface Line { key: string; label: string; unit: string; qty: number; rate: number; per: string; cost: number }
function lines(u: T.UsageRow): Line[] {
  const l = (key: string, label: string, unit: string, qty: number, rate: number, per: string): Line => ({ key, label, unit, qty, rate, per, cost: qty * rate });
  return [
    l("mem", "Memory", "GB hours", u.memGbHours, RATE.mem, "GB / hour"),
    l("cpu", "CPU", "vCPU hours", u.vcpuHours, RATE.cpu, "vCPU / hour"),
    l("egress", "Egress", "GB", u.egressGb, RATE.egress, "GB"),
    l("volume", "Volume", "GB months", u.volumeGbMonths, RATE.volume, "GB / month"),
  ];
}

export default function Usage({ project, env }: { project: T.Project; env?: T.Environment }) {
  const [period, setPeriod] = useState("month");
  const range = useMemo(() => usageRange(period, new Date(Math.floor(Date.now() / 60000) * 60000)), [period]);
  const { data, error, reload } = useLoad(() => v1.usage(project.id, range.from, range.until), [project.id, range.from, range.until]);
  return (
    <section>
      <SectionHead title="Current Usage" description="What this project has used in the chosen period, with the figure the same usage would cost on a hosted platform."
        actions={<Select aria-label="Period" value={period} onChange={(e) => setPeriod(e.target.value)} options={PERIODS} />} />
      {error ? <LoadError error={error} onRetry={reload} /> : !data ? <UsageSkeleton /> : <UsageBody u={data} range={range} env={env} />}
    </section>
  );
}

function UsageBody({ u, range, env }: { u: T.UsageResponse; range: { from: number; until: number }; env?: T.Environment }) {
  const cur = lines(u.total);
  const span = Math.max(1, range.until - range.from);
  const monthMs = 30 * 86400000;
  const scale = Math.max(1, monthMs / span);
  const est = cur.map((x) => ({ ...x, qty: x.qty * scale, cost: x.cost * scale }));
  const total = cur.reduce((s, x) => s + x.cost, 0);
  const estTotal = est.reduce((s, x) => s + x.cost, 0);
  return (
    <>
      <UsageCard rows={cur.map((x) => [x.label + " Usage", money(x.cost)])} totalLabel="Current Usage" total={money(total)} note={`Equivalent to ${money(u.railway.usd)} on list prices of ${u.railway.pricesDate}`} />
      <h2 className="mb-6 mt-12 text-h2 text-foreground">Estimated Usage</h2>
      <UsageCard rows={est.map((x) => [x.label + " Usage", money(x.cost)])} totalLabel="Estimated 30 day usage" total={money(estTotal)} note="Projected from the usage so far" />
      <h2 className="mb-6 mt-12 text-h2 text-foreground">Details</h2>
      {env ? <Charts env={env} range={range} /> : <p className="text-body text-fg-2">Add an environment to see charts.</p>}
      <div className="mb-6 mt-12 flex items-end justify-between gap-4">
        <h2 className="text-h2 text-foreground">Project Cost</h2>
        <span className="text-body text-fg-3">{u.services.length} {u.services.length === 1 ? "service" : "services"}</span>
      </div>
      <div className="overflow-x-auto rounded-lg border border-line">
        <table className="w-full min-w-[560px] text-caption">
          <thead className="bg-tint-header text-left text-fg-2"><tr>
            <th className="px-4 py-3 font-semibold">Resource</th><th className="px-4 py-3 font-semibold">Quantity</th><th className="px-4 py-3 font-semibold">Rate</th><th className="px-4 py-3 text-right font-semibold">Cost</th>
          </tr></thead>
          <tbody>
            {cur.map((x) => (
              <tr key={x.key} className="h-[53px] border-t border-surface-hover transition-colors hover:bg-tint-hover">
                <td className="px-4 text-body text-foreground">{x.label}</td>
                <td className="px-4 text-body text-fg-2">{num(x.qty)} {x.unit}</td>
                <td className="px-4 text-body text-fg-2">{money(x.rate, 4)} / {x.per}</td>
                <td className="px-4 text-right text-body text-foreground">{money(x.cost, 4)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {u.services.length > 0 && (
        <>
          <h3 className="mb-4 mt-8 text-lead font-semibold text-foreground">Cost by Service</h3>
          <div className="overflow-x-auto rounded-lg border border-line">
            <table className="w-full min-w-[620px] text-caption">
              <thead className="bg-tint-header text-fg-2"><tr>
                <th className="px-4 py-3 text-left font-semibold">Service</th><th className="px-4 py-3 text-right font-semibold">vCPU h</th><th className="px-4 py-3 text-right font-semibold">Memory GB h</th>
                <th className="px-4 py-3 text-right font-semibold">Egress GB</th><th className="px-4 py-3 text-right font-semibold">Volume GB mo</th><th className="px-4 py-3 text-right font-semibold">Backups</th><th className="px-4 py-3 text-right font-semibold">Cost</th>
              </tr></thead>
              <tbody>
                {u.services.map((r) => (
                  <tr key={r.serviceId || r.name} className="h-[44px] border-t border-surface-hover transition-colors hover:bg-tint-hover">
                    <td className="px-4 text-body text-foreground">{r.name}</td>
                    <td className="px-4 text-right text-body text-fg-2">{num(r.vcpuHours)}</td>
                    <td className="px-4 text-right text-body text-fg-2">{num(r.memGbHours)}</td>
                    <td className="px-4 text-right text-body text-fg-2">{num(r.egressGb)}</td>
                    <td className="px-4 text-right text-body text-fg-2">{num(r.volumeGbMonths, 3)}</td>
                    <td className="px-4 text-right text-body text-fg-2">{fmtBytes(r.backupBytes)}</td>
                    <td className="px-4 text-right text-body text-foreground">{money(lines(r).reduce((s, x) => s + x.cost, 0))}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </>
      )}
      <p className="mt-4 text-caption text-fg-3">
        Hosted equivalent: <b className="text-fg-2">{money(u.railway.usd)}</b> at list prices of {u.railway.pricesDate}.{" "}
        <a className="underline underline-offset-2 hover:text-foreground" href={u.railway.sourceUrl} target="_blank" rel="noreferrer">Price source</a>
      </p>
    </>
  );
}

function UsageCard({ rows, totalLabel, total, note }: { rows: string[][]; totalLabel: string; total: string; note: string }) {
  return (
    <div className="grid gap-6 rounded-lg border border-line p-6 md:grid-cols-[1fr_340px]">
      <div>
        {rows.map(([k, v], i) => (
          <div key={k} className={cx("flex h-[37px] items-center justify-between text-body", i > 0 && "border-t border-surface-hover")}>
            <span className="text-fg-2">{k}</span><span className="text-foreground">{v}</span>
          </div>
        ))}
      </div>
      <div className="flex min-h-[107px] flex-col items-center justify-center rounded-md bg-gray-50 p-5 text-center">
        <div className="text-body font-medium text-foreground">{totalLabel}</div>
        <div className="mt-1 text-[28px] font-semibold leading-[38px] text-foreground">{total}</div>
        <div className="mt-1 text-caption text-fg-3">{note}</div>
      </div>
    </div>
  );
}

function UsageSkeleton() {
  return <div aria-busy className="space-y-6"><Skeleton h={191} className="w-full rounded-lg" /><Skeleton h={28} w={200} /><Skeleton h={191} className="w-full rounded-lg" /><Skeleton h={420} className="w-full rounded-lg" /></div>;
}

/* ------------------------------------------------------------------ charts */

type Pt = [number, number];
interface Agg { cpu: Pt[]; mem: Pt[]; net: Pt[]; disk: Pt[] }

function sumSeries(all: (Pt[] | undefined)[]): Pt[] {
  const m = new Map<number, number>();
  for (const s of all) for (const [t, v] of s || []) m.set(t, (m.get(t) || 0) + v);
  return [...m.entries()].sort((a, b) => a[0] - b[0]);
}

function Charts({ env, range }: { env: T.Environment; range: { from: number; until: number } }) {
  const [agg, setAgg] = useState<Agg | null>(null);
  const [err, setErr] = useState<string | null>(null);
  useEffect(() => {
    let live = true;
    setAgg(null); setErr(null);
    (async () => {
      const c = await v1.canvas(env.id);
      const res = await Promise.all(c.services.map((s) => call<T.MetricsResponse>("GET", `/environments/${encodeURIComponent(env.id)}/services/${encodeURIComponent(s.id)}/metrics?from=${range.from}&until=${range.until}`).catch(() => null)));
      const ok = res.filter(Boolean) as T.MetricsResponse[];
      const get = (k: string) => ok.map((r) => r.series[k] as Pt[] | undefined);
      if (live) setAgg({ cpu: sumSeries(get("cpu")), mem: sumSeries(get("mem")), net: sumSeries([...get("rx"), ...get("tx")]), disk: sumSeries(get("disk")) });
    })().catch((e) => live && setErr(e?.message || String(e)));
    return () => { live = false; };
  }, [env.id, range.from, range.until]);
  if (err) return <LoadError error={err} />;
  if (!agg) return <Skeleton h={480} className="w-full rounded-lg" />;
  const fmtMem = (v: number) => fmtBytes(v);
  return (
    <div className="divide-y divide-surface-hover rounded-lg border border-line bg-surface-raised px-4">
      <StepChart title="CPU" pts={agg.cpu} fmt={(v) => `${num(v, v < 1 ? 2 : 1)} vCPU`} />
      <StepChart title="RAM" pts={agg.mem} fmt={fmtMem} />
      <StepChart title="Network Egress" pts={agg.net} fmt={(v) => `${fmtBytes(v)}/s`} />
      <StepChart title="Volume" pts={agg.disk} fmt={fmtMem} />
    </div>
  );
}

function StepChart({ title, pts, fmt }: { title: string; pts: Pt[]; fmt: (v: number) => string }) {
  const W = 655, H = 100, pad = 4;
  const [hover, setHover] = useState<number | null>(null);
  const shape = useMemo(() => {
    if (pts.length < 2) return null;
    const t0 = pts[0][0], t1 = pts[pts.length - 1][0];
    const max = Math.max(...pts.map((p) => p[1])) * 1.15 || 1;
    const x = (t: number) => ((t - t0) / Math.max(1, t1 - t0)) * W;
    const y = (v: number) => H - pad - (v / max) * (H - pad * 2);
    let d = `M0,${y(pts[0][1])}`;
    for (let i = 1; i < pts.length; i++) d += ` H${x(pts[i][0])} V${y(pts[i][1])}`;
    return { d, area: `${d} V${H} H0 Z`, max, x, t0, t1 };
  }, [pts]);
  return (
    <div className="py-4">
      <div className="mb-2 text-body font-medium text-foreground">{title}{hover !== null && shape && pts[hover] && <span className="ml-3 font-normal text-fg-3">{fmt(pts[hover][1])}</span>}</div>
      {!shape ? <div className="flex h-[100px] items-center justify-center rounded-md bg-surface-hover text-body text-fg-3">No data in this period</div> : (
        <div className="flex gap-3">
          <svg viewBox={`0 0 ${W} ${H}`} className="h-[100px] min-w-0 flex-1" preserveAspectRatio="none" role="img" aria-label={`${title} over time`}
            onMouseMove={(e) => { const r = e.currentTarget.getBoundingClientRect(); const t = shape.t0 + ((e.clientX - r.left) / r.width) * (shape.t1 - shape.t0); let b = 0; for (let i = 0; i < pts.length; i++) if (pts[i][0] <= t) b = i; setHover(b); }}
            onMouseLeave={() => setHover(null)}>
            {[0.25, 0.5, 0.75].map((f) => <line key={f} x1={0} x2={W} y1={H * f} y2={H * f} stroke="currentColor" className="text-surface-hover" strokeWidth={1} vectorEffect="non-scaling-stroke" />)}
            <path d={shape.area} className="fill-accent" fillOpacity={0.12} />
            <path d={shape.d} fill="none" className="stroke-accent" strokeWidth={2} vectorEffect="non-scaling-stroke" />
            {hover !== null && pts[hover] && <line x1={shape.x(pts[hover][0])} x2={shape.x(pts[hover][0])} y1={0} y2={H} className="stroke-fg-4" strokeWidth={1} vectorEffect="non-scaling-stroke" />}
          </svg>
          <div className="flex w-[72px] shrink-0 flex-col justify-between text-right text-[11px] leading-none text-fg-3">
            <span>{fmt(shape.max)}</span><span>{fmt(shape.max / 2)}</span><span>0</span>
          </div>
        </div>
      )}
    </div>
  );
}
