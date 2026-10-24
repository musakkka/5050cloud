// SPDX-License-Identifier: AGPL-3.0-only

// One machine on the Hosts page: the list row, the grid card, the capacity meters, the notices
// (reboot, warnings, unreachable peers, identity conflicts) and the actions menu.
import type { ReactNode } from "react";
import { ArrowDownToLine, Clock, Cpu, Eraser, Globe, Info, Layers, MoreHorizontal, Pencil, RotateCw, Tag, Trash2, Wrench } from "lucide-react";
import { Badge, Button, IconButton, Menu, Notice, RelTime, StatusDot, cx, fmtBytes, plural, type MenuEntry } from "../../kit";
import type * as T from "../../api/types.gen";
import { capacityTone, isDimmed, pct, roleIcon, stateOf } from "./model";

/** Every action a machine can take. The page owns the requests and the optimistic updates. */
export interface HostActions {
  open: (h: T.Host) => void;
  edit: (h: T.Host) => void;
  maintenance: (h: T.Host) => void;
  drain: (h: T.Host) => void;
  readopt: (h: T.Host) => void;
  clearConflicts: (h: T.Host) => void;
  remove: (h: T.Host) => void;
}

export interface HostView {
  host: T.Host;
  /** Name of another machine by id, for unreachable peers. */
  nameOf: (id: string) => string;
  actions: HostActions;
}

export function StateBadge({ state }: { state: string }) {
  const s = stateOf(state);
  return (
    <Badge tone={s.tone}>
      <StatusDot tone={s.dot} />
      {s.label}
    </Badge>
  );
}

/** One capacity bar: 8px track, accent fill that turns yellow at 75 percent and red at 90 percent. */
export function Meter({ label, used, total, value, note }: { label: string; used: number; total: number; value: string; note?: string }) {
  const p = pct(used, total);
  const tone = capacityTone(p);
  return (
    <div className="min-w-0">
      <div className="flex items-baseline justify-between gap-3">
        <span className="text-body font-medium text-fg-2">{label}</span>
        <span className="text-caption tabular-nums text-fg-3">{p}%</span>
      </div>
      <div role="meter" aria-label={label} aria-valuemin={0} aria-valuemax={100} aria-valuenow={p} className="mt-2 h-2 overflow-hidden rounded-full bg-line">
        <div className={cx("h-full rounded-full transition-[width] duration-500 ease-out",
          tone === "danger" ? "bg-danger-text" : tone === "warn" ? "bg-warn" : "bg-accent")}
          style={{ width: `${p}%` }} />
      </div>
      <div className="mt-2 text-body tabular-nums text-fg-2">{value}</div>
      {note && <div className="text-caption text-fg-3">{note}</div>}
    </div>
  );
}

/** CPU, memory and one meter per disk. `compact` keeps only CPU and memory for the grid card. */
export function Capacity({ host, compact }: { host: T.Host; compact?: boolean }) {
  return (
    <div className={cx("grid gap-x-6 gap-y-5", compact ? "grid-cols-2" : "sm:grid-cols-3")}>
      <Meter label="CPU" used={host.cpuUsed} total={host.cpus} value={`${host.cpuUsed.toFixed(1)} of ${plural(host.cpus, "vCPU", "vCPU")}`} />
      <Meter label="Memory" used={host.memUsed} total={host.memBytes} value={`${fmtBytes(host.memUsed)} of ${fmtBytes(host.memBytes)}`}
        note={compact ? undefined : `${host.reservedMb} MB reserved for replicas`} />
      {!compact && host.disks.map((d) => (
        <Meter key={d.mount} label={`Disk ${d.mount}`} used={d.used} total={d.total} value={`${fmtBytes(d.used)} of ${fmtBytes(d.total)}`} />
      ))}
    </div>
  );
}

/** Notices under a machine: a pending reboot, the server's warnings, unreachable peers and identity conflicts. */
export function HostNotices({ host, nameOf, actions }: HostView) {
  const items: ReactNode[] = [];
  if (host.rebootRequired) {
    items.push(
      <Notice key="reboot" tone="warn" title="Reboot pending"
        action={host.state !== "maintenance" && <Button size="xs" variant="outline" onClick={() => actions.maintenance(host)}>Start maintenance</Button>}>
        A kernel update waits for a reboot. Start maintenance, then reboot the machine.
      </Notice>,
    );
  }
  host.warnings?.forEach((w, i) => items.push(<Notice key={`w${i}`} tone="warn">{w}</Notice>));
  if (host.unreachable?.length) {
    items.push(
      <Notice key="unreachable" tone="danger" title="Peer unreachable">
        Cannot reach {host.unreachable.map(nameOf).join(", ")} over the private network.
      </Notice>,
    );
  }
  if (host.conflicts?.length) {
    const n = host.conflicts.length;
    items.push(
      <Notice key="conflicts" tone="danger" title="Identity conflict"
        action={<Button size="xs" variant="outline" icon={<Eraser size={14} />} onClick={() => actions.clearConflicts(host)}>Clear conflicts</Button>}>
        {plural(n, "other machine", "other machines")} presented this host's identity: {host.conflicts.map((c) => c.ip).join(", ")}.
      </Notice>,
    );
  }
  if (!items.length) return null;
  return <div className="space-y-2">{items}</div>;
}

/** The actions menu for one machine: details, edit, maintenance, drain, adopt, remove. */
export function HostMenu({ host, actions, label = `Actions for ${host.name}` }: { host: T.Host; actions: HostActions; label?: string }) {
  const maintenance = host.state === "maintenance";
  const draining = host.state === "draining";
  const items: MenuEntry[] = [
    { label: "Open details", icon: <Info size={16} />, onClick: () => actions.open(host) },
    { label: "Edit name and roles", icon: <Pencil size={16} />, onClick: () => actions.edit(host) },
    "separator",
    { label: maintenance ? "End maintenance" : "Start maintenance", icon: <Wrench size={16} />, onClick: () => actions.maintenance(host) },
    { label: draining ? "Stop draining" : "Drain replicas", icon: <ArrowDownToLine size={16} />, onClick: () => actions.drain(host) },
    ...(host.state === "unknown" ? [{ label: "Adopt again", icon: <RotateCw size={16} />, onClick: () => actions.readopt(host) }] : []),
    "separator",
    { label: "Remove host", icon: <Trash2 size={16} />, danger: true, onClick: () => actions.remove(host) },
  ];
  return (
    <Menu align="right" minWidth={208} items={items}
      trigger={<IconButton label={label} size={34}><MoreHorizontal size={16} /></IconButton>} />
  );
}

function Mono({ children }: { children: ReactNode }) {
  return <span className="font-mono text-[13px] leading-5">{children}</span>;
}

/** The meta line: public address, size, replicas, version and last check-in. Dims when the machine is down. */
export function HostMeta({ host }: { host: T.Host }) {
  const items: [ReactNode, ReactNode][] = [
    [<Globe key="i" size={14} />, <Mono key="ip">{host.publicIp}</Mono>],
    [<Layers key="i" size={14} />, plural(host.replicas, "replica")],
    [<Cpu key="i" size={14} />, <>{host.cpus} vCPU · {host.arch}</>],
    [<Tag key="i" size={14} />, <Mono key="m">{host.version}</Mono>],
    [<Clock key="i" size={14} />, host.lastSeen ? <>Seen <RelTime ts={host.lastSeen} /></> : "Never seen"],
  ];
  return (
    <div className={cx("mt-2 flex flex-wrap items-center gap-x-4 gap-y-1 text-body", isDimmed(host.state) ? "text-fg-5" : "text-fg-3")}>
      {items.map(([icon, text], i) => (
        <span key={i} className="inline-flex items-center gap-1.5 whitespace-nowrap">
          <span className="inline-flex shrink-0">{icon}</span>
          {text}
        </span>
      ))}
    </div>
  );
}

/** A list row: 86px minimum, raised inner band, the name, state, roles, meta line, then the meters. */
export function HostRow({ host, nameOf, actions }: HostView) {
  const Icon = roleIcon(host.roles);
  const dim = isDimmed(host.state);
  return (
    <li className="animate-rise-in overflow-hidden rounded-lg border border-line bg-surface transition-colors duration-150 hover:border-fg-5">
      <div className="flex items-start gap-4 px-5 pt-4">
        <span className={cx("mt-0.5 inline-flex h-10 w-10 shrink-0 items-center justify-center rounded-lg border border-line-popover bg-surface-hover",
          dim ? "text-fg-5" : "text-fg-2")}>
          <Icon size={20} />
        </span>
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-x-2.5 gap-y-1.5">
            <button type="button" onClick={() => actions.open(host)}
              className="min-w-0 truncate rounded text-left text-body font-semibold text-foreground underline-offset-2 transition-colors duration-150 hover:underline">
              {host.name}
            </button>
            <StateBadge state={host.state} />
            {host.acmePrimary && <Badge tone="accent">Certificates</Badge>}
          </div>
          <div className="mt-2 flex flex-wrap gap-1.5">
            {host.roles.map((r) => <Badge key={r} tone={r === "control" ? "accent" : "neutral"}>{r}</Badge>)}
          </div>
          <HostMeta host={host} />
        </div>
        <HostMenu host={host} actions={actions} />
      </div>
      <div className="px-5 pb-4 pt-5">
        <Capacity host={host} />
      </div>
      <div className="px-5 pb-4">
        <HostNotices host={host} nameOf={nameOf} actions={actions} />
      </div>
    </li>
  );
}

/** A grid card: 24px padding, 16/24 title, state and roles, and the CPU and memory meters. */
export function HostTile({ host, nameOf, actions }: HostView) {
  const Icon = roleIcon(host.roles);
  const dim = isDimmed(host.state);
  return (
    <li className="animate-rise-in flex min-h-[144px] flex-col rounded-xl border border-line bg-surface p-6 transition-colors duration-150 hover:border-fg-5 hover:bg-surface-hover">
      <div className="flex items-center gap-3">
        <Icon size={24} className={cx("shrink-0", dim ? "text-fg-5" : "text-fg-2")} />
        <button type="button" onClick={() => actions.open(host)}
          className="min-w-0 flex-1 truncate rounded text-left text-lead font-semibold text-foreground underline-offset-2 hover:underline">
          {host.name}
        </button>
        <HostMenu host={host} actions={actions} />
      </div>
      <div className="mt-3 flex flex-wrap items-center gap-1.5">
        <StateBadge state={host.state} />
        {host.roles.map((r) => <Badge key={r}>{r}</Badge>)}
      </div>
      <div className="mt-5">
        <Capacity host={host} compact />
      </div>
      {(host.rebootRequired || host.conflicts?.length || host.unreachable?.length) && (
        <div className="mt-4 text-caption text-warn">
          {host.rebootRequired && "Reboot pending. "}
          {!!host.unreachable?.length && `Cannot reach ${host.unreachable.map(nameOf).join(", ")}. `}
          {!!host.conflicts?.length && "Identity conflict."}
        </div>
      )}
    </li>
  );
}

/** Placeholder rows with the same geometry as HostRow, shown while the first load is in flight. */
export function HostRowSkeleton() {
  return (
    <div className="rounded-lg border border-line bg-surface px-5 pb-4 pt-4" aria-hidden>
      <div className="flex items-start gap-4">
        <div className="h-10 w-10 shrink-0 animate-skeleton rounded-lg bg-skeleton" />
        <div className="min-w-0 flex-1 space-y-3">
          <div className="h-3.5 w-48 animate-skeleton rounded bg-skeleton" />
          <div className="h-3 w-80 max-w-full animate-skeleton rounded bg-skeleton" />
          <div className="h-3 w-96 max-w-full animate-skeleton rounded bg-skeleton" />
        </div>
      </div>
      <div className="mt-5 grid grid-cols-3 gap-6">
        {[0, 1, 2].map((i) => <div key={i} className="h-2 animate-skeleton rounded-full bg-skeleton" />)}
      </div>
    </div>
  );
}
