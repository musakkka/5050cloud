// SPDX-License-Identifier: AGPL-3.0-only

// Install-wide usage: month stepper, totals card with tiles, collapsible breakdown,
// a plan card, and a per-project accordion with per-service rows.
import { ArrowUpRight, ChevronDown, ChevronLeft, ChevronRight, CircleArrowUp } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { api } from "../../api/client";
import type * as T from "../../api/types.gen";
import { Button, ShimmerCard, cx, fmtBytes } from "../../kit";
import { v1 } from "../settings/client";
import { Loading } from "../settings/ui";

const MAX_BACK = 11;
const n = (x: number, d = 2) => x.toLocaleString(undefined, { maximumFractionDigits: d, minimumFractionDigits: d });
export const usd = (x: number) => x.toLocaleString(undefined, { style: "currency", currency: "USD", minimumFractionDigits: 2, maximumFractionDigits: x > 0 && x < 1 ? 4 : 2 });
const fmt = (d: Date) => d.toLocaleDateString(undefined, { month: "short", day: "numeric" });

interface ProjectUsage { project: T.Project; usage: T.UsageResponse }

/** The month `back` months ago: from the first until now (this month) or the first of the next month. */
export function monthRange(back: number, now = new Date()): { from: Date; until: Date; label: string } {
  const from = new Date(now.getFullYear(), now.getMonth() - back, 1);
  const end = new Date(now.getFullYear(), now.getMonth() - back + 1, 1);
  const until = back === 0 ? new Date(Math.floor(now.getTime() / 60000) * 60000) : end;
  const last = new Date(end.getTime() - 86400_000);
  return { from, until, label: `${fmt(from)} - ${fmt(back === 0 ? now : last)}` };
}

const sum = (rows: T.UsageRow[], k: keyof Omit<T.UsageRow, "name" | "serviceId">) => rows.reduce((a, r) => a + r[k], 0);

export default function UsagePage() {
  const [back, setBack] = useState(0);
  const range = useMemo(() => monthRange(back), [back]);
  const [rows, setRows] = useState<ProjectUsage[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [open, setOpen] = useState(false);
  const from = range.from.getTime(), until = range.until.getTime();
  useEffect(() => {
    let alive = true;
    setRows(null); setError(null);
    api.projects("name")
      .then((ps) => Promise.all(ps.map((project) => v1.usage(project.id, from, until).then((usage) => ({ project, usage })))))
      .then((r) => alive && setRows(r))
      .catch((e) => alive && setError(e?.message || String(e)));
    return () => { alive = false; };
  }, [from, until]);

  const total = useMemo(() => {
    const all = (rows || []).map((r) => r.usage.total);
    return {
      vcpu: sum(all, "vcpuHours"), mem: sum(all, "memGbHours"), egress: sum(all, "egressGb"), vol: sum(all, "volumeGbMonths"), backup: sum(all, "backupBytes"),
      list: (rows || []).reduce((a, r) => a + r.usage.railway.usd, 0),
    };
  }, [rows]);

  return (
    <div className="space-y-12">
      <div className="flex items-center justify-between gap-4">
        <h2 className="text-h2">{range.label} Usage</h2>
        <div className="flex items-center gap-2">
          <Button variant="outline" aria-label="Previous month" className="!w-[34px] !px-0" disabled={back >= MAX_BACK} onClick={() => setBack(back + 1)}><ChevronLeft size={16} /></Button>
          <span className="min-w-[132px] text-center text-lead tabular-nums" aria-live="polite">{range.label}</span>
          <Button variant="outline" aria-label="Next month" className="!w-[34px] !px-0" disabled={back <= 0} onClick={() => setBack(back - 1)}><ChevronRight size={16} /></Button>
        </div>
      </div>
      {!rows ? (error ? <Loading error={error} /> : <ShimmerCard h={214} />) : (
        <>
          <section className="rounded-lg border border-line p-6">
            <div className="grid gap-6 lg:grid-cols-2 lg:gap-10">
              <dl>
                {[
                  ["vCPU hours", n(total.vcpu)], ["Memory GB hours", n(total.mem)], ["Egress", `${n(total.egress)} GB`],
                  ["Volumes", `${n(total.vol, 3)} GB months`], ["Backups", fmtBytes(total.backup)],
                ].map(([k, v]) => (
                  <div key={k} className="flex h-[37px] items-center justify-between border-b border-gray-100 text-body last:border-b-0">
                    <dt className="text-fg-2">{k}</dt><dd className="tabular-nums">{v}</dd>
                  </div>
                ))}
              </dl>
              <div className="space-y-4">
                <div className="grid grid-cols-2 gap-4">
                  <Tile label="Projects" value={String(rows.length)} />
                  <Tile label="Same usage at list prices" value={usd(total.list)} tone="ok" />
                </div>
                <Button size="lg" block aria-expanded={open} icon={<ChevronDown size={16} className={cx("transition-transform duration-150", open && "rotate-180")} />} onClick={() => setOpen(!open)}>
                  {open ? "Hide breakdown" : "Bill breakdown"}
                </Button>
              </div>
            </div>
            <Collapsible open={open}>
              <div className="mt-6 border-t border-gray-100 pt-2">
                {([["Memory", `${n(total.mem)} GB hours`], ["CPU", `${n(total.vcpu)} vCPU hours`], ["Egress", `${n(total.egress)} GB`], ["Volume", `${n(total.vol, 3)} GB months`], ["Backup", fmtBytes(total.backup)]] as const).map(([k, v]) => (
                  <div key={k} className="grid grid-cols-6 border-b border-gray-100 py-2.5 text-body">
                    <span>{k}</span><span className="col-span-3 text-fg-3 tabular-nums">{v}</span>
                  </div>
                ))}
                <div className="flex items-center justify-between pt-3"><span className="text-body font-semibold">Hosting cost</span><span className="text-h3 tabular-nums">Your own servers</span></div>
              </div>
            </Collapsible>
          </section>

          <PlanCard />

          <section className="space-y-4">
            <h2 className="text-h2">Usage by Project</h2>
            {rows.length === 0 && <p className="text-body text-fg-2">No projects yet.</p>}
            <div className="space-y-3">{rows.map((r) => <ProjectRow key={r.project.id} r={r} />)}</div>
          </section>
        </>
      )}
    </div>
  );
}

function Tile({ label, value, tone }: { label: string; value: string; tone?: "ok" }) {
  return (
    <div className="rounded-md bg-gray-50 p-5">
      <div className={cx("text-body font-medium", tone === "ok" ? "text-ok" : "text-fg-2")}>{label}</div>
      <div className="mt-1 text-title !font-semibold tabular-nums">{value}</div>
    </div>
  );
}

function Collapsible({ open, children }: { open: boolean; children: React.ReactNode }) {
  return (
    <div className={cx("grid transition-[grid-template-rows,opacity] duration-200 ease-out", open ? "grid-rows-[1fr] opacity-100" : "grid-rows-[0fr] opacity-0")} aria-hidden={!open}>
      <div className="overflow-hidden">{children}</div>
    </div>
  );
}

function PlanCard() {
  return (
    <section className="relative flex flex-col gap-4 overflow-hidden rounded-lg border border-line p-6 lg:flex-row lg:items-center lg:justify-between">
      <svg aria-hidden viewBox="0 0 240 90" className="pointer-events-none absolute bottom-0 right-[420px] hidden h-full w-[240px] text-accent opacity-25 lg:block">
        {[10, 22, 36, 52, 70].map((h, i) => <rect key={i} x={i * 44} y={90 - h} width="36" height={h} rx="3" fill="currentColor" opacity={0.35 + i * 0.13} />)}
      </svg>
      <div className="relative"><div className="text-lead font-semibold">Self-hosted</div><p className="mt-1 max-w-sm text-body text-fg-2">No metering and no usage caps. You pay your own server provider for the hosts you add.</p></div>
      <Link to="/settings/upgrades" className="relative lg:w-[360px]"><Button variant="primary" block icon={<CircleArrowUp size={20} />}>Check for upgrades</Button></Link>
    </section>
  );
}

function ProjectRow({ r }: { r: ProjectUsage }) {
  const [open, setOpen] = useState(false);
  const { project, usage } = r;
  return (
    <div className="animate-rise-in rounded-lg bg-gray-50">
      <div className="group flex min-h-[81px] items-center justify-between gap-4 rounded-lg px-6 py-4 transition-colors hover:bg-gray-100">
        <Link to={`/project/${project.id}`} className="flex min-w-0 items-center gap-2 text-h3 !font-normal transition-colors hover:text-accent-text">
          <span className="truncate">{project.name}</span><ArrowUpRight size={18} className="shrink-0 text-fg-4" />
        </Link>
        <button type="button" aria-expanded={open} aria-label={`${project.name} breakdown`} onClick={() => setOpen(!open)} className="flex items-center gap-4 text-right">
          <span><span className="block text-caption font-medium text-fg-2">Same usage at list prices</span><span className="block text-h3 tabular-nums">{usd(usage.railway.usd)}</span></span>
          <ChevronDown size={16} className={cx("text-fg-3 transition-transform duration-200", open && "rotate-180")} />
        </button>
      </div>
      <Collapsible open={open}>
        <div className="overflow-x-auto px-6 pb-4">
          <table className="w-full min-w-[520px] text-body">
            <thead className="text-caption text-fg-3"><tr><th className="py-2 text-left font-semibold">Service</th><th className="text-right font-semibold">vCPU h</th><th className="text-right font-semibold">Mem GB h</th><th className="text-right font-semibold">Egress GB</th><th className="text-right font-semibold">Volume GB mo</th><th className="text-right font-semibold">Backups</th></tr></thead>
            <tbody>
              {usage.services.map((s) => (
                <tr key={s.serviceId || s.name} className="border-t border-gray-100 tabular-nums">
                  <td className="py-2">{s.name}</td><td className="text-right">{n(s.vcpuHours)}</td><td className="text-right">{n(s.memGbHours)}</td><td className="text-right">{n(s.egressGb)}</td><td className="text-right">{n(s.volumeGbMonths, 3)}</td><td className="text-right">{fmtBytes(s.backupBytes)}</td>
                </tr>
              ))}
              {usage.services.length === 0 && <tr><td colSpan={6} className="py-3 text-fg-3">No services used resources in this period.</td></tr>}
            </tbody>
          </table>
        </div>
      </Collapsible>
    </div>
  );
}
