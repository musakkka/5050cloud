// SPDX-License-Identifier: AGPL-3.0-only

// Project Settings > Usage (docs/SPEC.md 9.23).
import { useMemo, useState } from "react";
import type * as T from "../../api/types.gen";
import { fmtBytes } from "../../ui/kit";
import { v1 } from "../settings/client";
import { usageRange } from "../settings/forms";
import { Field, Input, Loading, Section, Select, useLoad } from "../settings/ui";

const PERIODS = [{ value: "month", label: "This month" }, { value: "last-month", label: "Last month" }, { value: "30d", label: "Last 30 days" }, { value: "custom", label: "Custom" }];
const n = (x: number, d = 2) => x.toLocaleString(undefined, { maximumFractionDigits: d, minimumFractionDigits: d });
export const usd = (x: number) => x.toLocaleString(undefined, { style: "currency", currency: "USD" });

export default function UsagePanel({ pid }: { pid: string }) {
  const today = new Date().toISOString().slice(0, 10);
  const [period, setPeriod] = useState("month");
  const [from, setFrom] = useState(today.slice(0, 8) + "01");
  const [until, setUntil] = useState(today);
  // Rounded to the minute so re-renders do not refetch.
  const range = useMemo(() => usageRange(period, new Date(Math.floor(Date.now() / 60000) * 60000), { from, until }), [period, from, until]);
  const { data, error } = useLoad(() => v1.usage(pid, range.from, range.until), [pid, range.from, range.until]);
  return (
    <Section title="Usage" description="What this project used in the chosen period, per service.">
      <div className="flex flex-col md:flex-row gap-3">
        <Field label="Period"><Select value={period} onChange={(e) => setPeriod(e.target.value)} options={PERIODS} /></Field>
        {period === "custom" && <>
          <Field label="From"><Input type="date" value={from} max={until} onChange={(e) => setFrom(e.target.value)} /></Field>
          <Field label="Until"><Input type="date" value={until} min={from} onChange={(e) => setUntil(e.target.value)} /></Field>
        </>}
      </div>
      {!data ? <Loading error={error} /> : <UsageTable u={data} />}
    </Section>
  );
}

function UsageTable({ u }: { u: T.UsageResponse }) {
  const row = (r: T.UsageRow, total = false) => (
    <tr key={r.serviceId || r.name} className={total ? "border-t-2 border-gray-200 font-semibold" : "border-t border-gray-100"}>
      <td className="px-4 py-2">{r.name}</td>
      <td className="px-4 py-2 text-right">{n(r.vcpuHours)}</td>
      <td className="px-4 py-2 text-right">{n(r.memGbHours)}</td>
      <td className="px-4 py-2 text-right">{n(r.egressGb)}</td>
      <td className="px-4 py-2 text-right">{n(r.volumeGbMonths, 3)}</td>
      <td className="px-4 py-2 text-right">{fmtBytes(r.backupBytes)}</td>
    </tr>
  );
  return (
    <>
      <div className="rounded-xl border border-gray-200 overflow-x-auto">
        <table className="w-full text-sm">
          <thead className="text-xs text-gray-500"><tr>
            <th className="px-4 py-2 text-left">Service</th><th className="px-4 py-2 text-right">vCPU hours</th><th className="px-4 py-2 text-right">Memory GB hours</th>
            <th className="px-4 py-2 text-right">Egress GB</th><th className="px-4 py-2 text-right">Volume GB months</th><th className="px-4 py-2 text-right">Backups</th>
          </tr></thead>
          <tbody>{u.services.map((r) => row(r))}{row({ ...u.total, name: "Total" }, true)}</tbody>
        </table>
      </div>
      <p className="text-sm text-gray-600 mt-4">
        The same usage on Railway at list prices of {u.railway.pricesDate} would cost about <b>{usd(u.railway.usd)}</b>.{" "}
        <a className="underline" href={u.railway.sourceUrl} target="_blank" rel="noreferrer">Source</a>
      </p>
    </>
  );
}
