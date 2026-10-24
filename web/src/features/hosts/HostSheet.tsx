// SPDX-License-Identifier: AGPL-3.0-only

// The machine detail sheet: an Overview with capacity, notices, the facts about the machine and its
// actions, and a Metrics tab with per-host charts.
import { useState, type ReactNode } from "react";
import { ArrowDownToLine, Pencil, Trash2, Wrench } from "lucide-react";
import { Badge, Button, Sheet, Tabs, fmtBytes, fmtDateTime } from "../../kit";
import type * as T from "../../api/types.gen";
import { Capacity, HostNotices, StateBadge, type HostActions } from "./HostCard";
import { HostMetrics } from "./HostMetrics";
import { roleIcon } from "./model";

type Tab = "overview" | "metrics";

export function HostSheet({ host, nameOf, actions, onClose }: {
  host: T.Host; nameOf: (id: string) => string; actions: HostActions; onClose: () => void;
}) {
  const [tab, setTab] = useState<Tab>("overview");
  const Icon = roleIcon(host.roles);
  const facts: [string, ReactNode, boolean][] = [
    ["Public address", host.publicIp, true],
    ["Egress address", host.egressIp, true],
    ["Private network", host.meshIp, true],
    ["Version", host.version, true],
    ["Architecture", host.arch, false],
    ["Operating system", host.os, false],
    ["vCPUs", String(host.cpus), false],
    ["Memory", fmtBytes(host.memBytes), false],
    ["Kept free for the system", `${host.reserveMb} MB`, false],
    ["Memory reserved by replicas", `${host.reservedMb} MB`, false],
    ["Replicas", String(host.replicas), false],
    ["Last seen", host.lastSeen ? fmtDateTime(host.lastSeen) : "Never", false],
  ];
  return (
    <Sheet onClose={onClose} width={720} label={`${host.name} details`}
      title={<span className="flex min-w-0 items-center gap-2.5"><Icon size={18} className="shrink-0 text-fg-2" /><span className="truncate">{host.name}</span></span>}>
      <div className="px-5">
        <Tabs tabs={[{ id: "overview", label: "Overview" }, { id: "metrics", label: "Metrics" }]} value={tab} onChange={(id) => setTab(id as Tab)} />
      </div>
      <div className="space-y-6 p-5">
        {tab === "overview" ? (
          <>
            <div className="flex flex-wrap items-center gap-2">
              <StateBadge state={host.state} />
              {host.roles.map((r) => <Badge key={r}>{r}</Badge>)}
              {host.acmePrimary && <Badge tone="accent">Certificates</Badge>}
            </div>
            <HostNotices host={host} nameOf={nameOf} actions={actions} />
            <section className="rounded-lg border border-line bg-surface p-5">
              <Capacity host={host} />
            </section>
            <dl className="grid grid-cols-1 gap-x-8 gap-y-4 sm:grid-cols-2">
              {facts.map(([label, value, mono]) => (
                <div key={label} className="min-w-0 border-b border-line pb-3">
                  <dt className="text-caption text-fg-3">{label}</dt>
                  <dd className={`mt-1 truncate text-body text-foreground ${mono ? "font-mono text-[13px]" : "tabular-nums"}`}>{value}</dd>
                </div>
              ))}
            </dl>
            <div className="flex flex-wrap gap-2">
              <Button icon={<Pencil size={16} />} onClick={() => actions.edit(host)}>Edit name and roles</Button>
              <Button icon={<Wrench size={16} />} onClick={() => actions.maintenance(host)}>
                {host.state === "maintenance" ? "End maintenance" : "Start maintenance"}
              </Button>
              <Button icon={<ArrowDownToLine size={16} />} onClick={() => actions.drain(host)}>
                {host.state === "draining" ? "Stop draining" : "Drain replicas"}
              </Button>
              <Button variant="danger-outline" icon={<Trash2 size={16} />} className="ml-auto" onClick={() => actions.remove(host)}>Remove host</Button>
            </div>
          </>
        ) : (
          <HostMetrics hostId={host.id} cpus={host.cpus} memBytes={host.memBytes} />
        )}
      </div>
    </Sheet>
  );
}
