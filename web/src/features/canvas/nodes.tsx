// SPDX-License-Identifier: AGPL-3.0-only

// Canvas nodes: the 288x144 service card with its modifier strips (volume, cron schedule,
// replicas), group frames with a 48px header and kebab, and the empty-project card.
import { memo, type ReactNode } from "react";
import { Handle, Position, type Node, type NodeProps } from "@xyflow/react";
import {
  AlertTriangle, CalendarDays, CircleAlert, CircleCheck, Copy, Database, HardDrive, Info, Layers, Loader2, Moon, MoreVertical, Package, Plus, SquareTerminal, Terminal,
} from "lucide-react";
import type { Service } from "../../api/client";
import { BrandIcon, ServiceIcon, cx } from "../../kit";
import { nextRuns, parseCron } from "../cron/cron";
import { COLLAPSED_H, EMPTY_H, EMPTY_W, H, W, type StripKind } from "./layout";

export type Tag = "New" | "Edited" | "Removed" | undefined;

/** The card icon: the official logo, else a terminal glyph for empty services. */
export function NodeIcon({ s, size = 24 }: { s: Service; size?: number }) {
  const cfg = s.staged || s.config;
  const src = cfg.source.type;
  if (!cfg.icon && (src === "empty" || src === "upload")) return <Terminal size={size} strokeWidth={2} className="shrink-0 text-foreground" aria-hidden />;
  return <ServiceIcon icon={cfg.icon} source={src === "github" ? "repo" : src} image={cfg.source.image} size={size} />;
}

/** "in 4 minutes", "in 2 months". */
export function inWords(ms: number): string {
  const m = Math.max(1, Math.round(ms / 60000));
  const u: [number, string][] = [[60 * 24 * 365, "year"], [60 * 24 * 30, "month"], [60 * 24 * 7, "week"], [60 * 24, "day"], [60, "hour"], [1, "minute"]];
  for (const [n, name] of u) if (m >= n) { const v = Math.round(m / n); return `${v} ${name}${v === 1 ? "" : "s"}`; }
  return "1 minute";
}

export function cronLabel(s: Service): string {
  const cfg = s.staged || s.config;
  const expr = cfg.deploy.cronSchedule;
  if (!expr) return "No schedule";
  const p = parseCron(expr);
  if (!p.ok) return expr;
  const [next] = nextRuns(p.cron, 1, Date.now(), cfg.deploy.cronTimezone || "UTC");
  return next ? `Next in ${inWords(next - Date.now())}` : expr;
}

/** The green ring dot used for "Online". */
function OnlineDot() {
  return (
    <svg width="16" height="16" viewBox="0 0 16 16" aria-hidden className="mx-auto shrink-0">
      <circle cx="8" cy="8" r="6" fill="var(--green-500)" opacity=".22" />
      <circle cx="8" cy="8" r="3" fill="var(--green-600)" />
    </svg>
  );
}

interface StatusLook { icon?: ReactNode; text: string; cls: string }

export function statusLook(s: Service, tag: Tag, changes: number): StatusLook {
  if (tag === "Removed") return { icon: <CircleAlert size={18} strokeWidth={1.75} className="mx-auto text-red-600" />, text: "Will be deleted", cls: "text-red-600" };
  if (tag === "New") return { icon: <Plus size={18} strokeWidth={1.75} className="mx-auto text-green-600" />, text: "Will be created", cls: "text-green-600" };
  if (tag === "Edited") {
    const n = Math.max(1, changes);
    return { icon: <Info size={18} strokeWidth={1.75} className="mx-auto text-blue-400" />, text: `${n} Change${n === 1 ? "" : "s"}`, cls: "text-blue-600" };
  }
  const spin = (c: string) => <Loader2 size={16} strokeWidth={2} className={cx("spin mx-auto", c)} />;
  switch (s.status) {
    case "online": return { icon: <OnlineDot />, text: "Online", cls: "text-green-600" };
    case "deploying": return { icon: spin("text-blue-500"), text: "Deploying", cls: "text-blue-600" };
    case "building": return { icon: spin("text-blue-500"), text: "Building", cls: "text-blue-600" };
    case "pending": return { icon: spin("text-gray-500"), text: "Queued", cls: "text-fg-2" };
    case "crashed": return { icon: <CircleAlert size={18} strokeWidth={1.75} className="mx-auto text-red-600" />, text: "Crashed", cls: "text-red-600" };
    case "failed": return { icon: <CircleAlert size={18} strokeWidth={1.75} className="mx-auto text-red-600" />, text: "Failed", cls: "text-red-600" };
    case "host_lost": return { icon: <CircleAlert size={18} strokeWidth={1.75} className="mx-auto text-red-600" />, text: "Host lost", cls: "text-red-600" };
    case "unhealthy": return { icon: <AlertTriangle size={18} strokeWidth={1.75} className="mx-auto text-yellow-600" />, text: "Unhealthy", cls: "text-yellow-600" };
    case "sleeping": return { icon: <Moon size={16} strokeWidth={1.75} className="mx-auto text-fg-3" />, text: "Sleeping", cls: "text-fg-2" };
    case "completed": return { icon: <CircleCheck size={18} strokeWidth={1.75} className="mx-auto text-green-600" />, text: "Completed", cls: "text-fg-2" };
    default: return { text: "Service is offline", cls: "text-fg-2" };
  }
}

const TAG_LOOK: Record<Exclude<Tag, undefined>, { card: string; badge: string }> = {
  Edited: { card: "border-blue-200 bg-blue-50", badge: "bg-blue-300" },
  New: { card: "border-green-300 bg-green-100", badge: "bg-green-500" },
  Removed: { card: "border-red-300 bg-red-50", badge: "bg-red-500" },
};

export type CardData = {
  s: Service; tag: Tag; changes: number; selected: boolean; strips: StripKind[]; showPos?: [number, number];
  onStrip: (s: Service, k: StripKind) => void;
} & Record<string, unknown>;

/** The card body shared by the canvas node and the phone list. */
export function CardBody({ s, tag, changes }: { s: Service; tag: Tag; changes: number }) {
  const cfg = s.staged || s.config;
  const domain = s.domains[0]?.domain;
  const st = statusLook(s, tag, changes);
  return (
    <>
      <div className={cx("grid grid-cols-[24px_1fr] gap-3", domain ? "items-start" : "items-center")}>
        <span className={cx("flex h-6 w-6 items-center justify-center", domain && "mt-1")}><NodeIcon s={s} /></span>
        <div className="flex min-w-0 flex-col pr-8">
          <div className="flex min-w-0 items-center gap-2">
            <p className={cx("truncate text-base font-semibold leading-6 text-foreground", tag === "Removed" && "line-through decoration-red-600")}>{cfg.name}</p>
            {s.monitorFiring && <span className="h-1.5 w-1.5 shrink-0 rounded-full bg-red-500" title="A monitor is in alarm" />}
          </div>
          {domain && <p className="truncate text-sm leading-5 text-gray-700">{domain}</p>}
        </div>
      </div>
      <div title={s.statusText || undefined} className={cx("text-sm leading-5", st.icon ? "grid grid-cols-[24px_1fr] items-center gap-3" : "", st.cls)}>
        {st.icon && <span className="flex h-5 items-center">{st.icon}</span>}
        <span className="truncate">{st.text}</span>
      </div>
    </>
  );
}

function Strip({ kind, s, i, n, onClick }: { kind: StripKind; s: Service; i: number; n: number; onClick: () => void }) {
  const cfg = s.staged || s.config;
  const icon = kind === "volume" ? <HardDrive size={16} /> : kind === "cron" ? <CalendarDays size={16} /> : <Copy size={16} />;
  const label = kind === "volume" ? (s.volume?.name || s.volume?.mountPath || "volume")
    : kind === "cron" ? cronLabel(s)
      : `${cfg.deploy.replicas ?? 1} Replicas`;
  const staged = kind === "volume" && !!s.volume?.staged;
  return (
    <div role="button" tabIndex={-1} aria-label={kind === "volume" ? `Volume ${label}` : kind === "cron" ? `Schedule: ${label}` : label}
      onClick={(e) => { e.stopPropagation(); onClick(); }}
      style={{ zIndex: 4 - i, height: 72 }}
      className={cx("cv-strip nodrag relative -mt-6 overflow-hidden rounded-xl border pt-6 transition-colors duration-100",
        staged ? "border-blue-200 bg-blue-50" : "border-line bg-secondaryBg", i === n - 1 && "")}>
      {kind === "volume" && <span aria-hidden className="absolute bottom-0 left-0 top-0 w-1.5 bg-gray-200 opacity-30" />}
      <div className="grid h-full w-full cursor-pointer grid-cols-[24px_1fr] items-center gap-3 px-6">
        <span className={cx("mx-auto flex", staged ? "text-blue-600" : "text-fg-3")}>{icon}</span>
        <p className={cx("truncate whitespace-nowrap text-sm leading-5", staged ? "text-blue-600" : "text-fg-3")}>{label}</p>
      </div>
    </div>
  );
}

export const ServiceNode = memo(({ data: d }: NodeProps<Node<CardData>>) => {
  const look = d.tag ? TAG_LOOK[d.tag] : undefined;
  return (
    <div style={{ width: W }} className={cx("cv-node", d.tag === "Removed" && "opacity-70")} data-testid="service-card" data-name={(d.s.staged || d.s.config).name}>
      <Handle type="target" position={Position.Left} /><Handle type="source" position={Position.Right} />
      <div style={{ height: H }}
        className={cx("cv-card relative z-[5] flex flex-col justify-between rounded-xl border p-6 shadow-[0_1px_2px_rgba(0,0,0,.05)]",
          look ? cx(look.card, "cv-staged") : "border-line bg-secondaryBg",
          d.selected && "cv-selected !border-pink-400")}>
        {d.tag && <span className={cx("absolute right-2.5 top-2.5 rounded px-1.5 py-0.5 text-[10px] font-semibold leading-4 text-background", look!.badge)}>{d.tag}</span>}
        <CardBody s={d.s} tag={d.tag} changes={d.changes} />
        {d.showPos && <span className="absolute bottom-2 right-3 font-mono text-[10px] text-fg-4">{d.showPos[0]}, {d.showPos[1]}</span>}
      </div>
      {d.strips.map((k, i) => <Strip key={k} kind={k} s={d.s} i={i} n={d.strips.length} onClick={() => d.onStrip(d.s, k)} />)}
    </div>
  );
});

/** Group colours: frame tint and border from one family. */
export const GROUP_COLOURS = ["gray", "blue", "green", "pink", "yellow"] as const;
export type GroupColour = (typeof GROUP_COLOURS)[number];
export const COLOUR_DOT: Record<GroupColour, string> = { gray: "var(--gray-500)", blue: "var(--blue-500)", green: "var(--green-500)", pink: "var(--pink-500)", yellow: "var(--yellow-500)" };
function frameStyle(c: GroupColour | undefined): React.CSSProperties {
  if (!c || c === "gray") return { background: "rgba(135,133,147,.05)", borderColor: "var(--gray-200)" };
  return { background: `color-mix(in srgb, var(--${c}-500) 6%, transparent)`, borderColor: `var(--${c}-200)` };
}

export type FrameData = {
  id: string; name: string; icon?: string | null; w: number; h: number; depth: number;
  collapsed: boolean; colour?: GroupColour; summary: string; memberIcons: Service[];
  editable: boolean;
  onKebab: (id: string, el: HTMLElement) => void;
  onRename: (id: string) => void;
} & Record<string, unknown>;

export const GroupFrame = memo(({ data: d }: NodeProps<Node<FrameData>>) => {
  const first = d.memberIcons[0];
  return (
    <div style={{ width: d.w, height: d.collapsed ? COLLAPSED_H : d.h, ...frameStyle(d.colour) }}
      className={cx("cv-group relative rounded-xl border", d.collapsed && "bg-secondaryBg")} data-testid="group-frame" data-name={d.name}>
      <div className="flex h-12 items-center gap-3 pl-6 pr-4"
        onDoubleClick={(e) => { if (!d.editable) return; e.stopPropagation(); d.onRename(d.id); }}>
        <span className="flex h-6 w-6 shrink-0 items-center justify-center">
          {d.icon ? <ServiceIcon icon={d.icon} size={24} /> : first ? <NodeIcon s={first} /> : <Layers size={20} className="text-fg-3" />}
        </span>
        <span className="min-w-0 flex-1 truncate text-base font-semibold leading-[26px] text-foreground">{d.name}</span>
        {d.editable && (
          <button type="button" aria-label={`${d.name} options`} title="Group options"
            onClick={(e) => { e.stopPropagation(); d.onKebab(d.id, e.currentTarget); }}
            onDoubleClick={(e) => e.stopPropagation()}
            className="nodrag cv-press flex h-5 w-5 shrink-0 items-center justify-center rounded-md text-fg-2 hover:bg-tint-pill hover:text-foreground">
            <MoreVertical size={16} />
          </button>
        )}
      </div>
      {d.collapsed && (
        <div className="flex flex-col gap-4 px-6 pt-2">
          <div className="flex flex-wrap gap-2">
            {d.memberIcons.slice(0, 8).map((s) => (
              <span key={s.id} title={(s.staged || s.config).name} className="flex h-10 w-10 items-center justify-center rounded-lg border border-line-popover bg-surface-hover"><NodeIcon s={s} size={20} /></span>
            ))}
          </div>
          <p className="text-sm leading-5 text-fg-2">{d.summary}</p>
        </div>
      )}
    </div>
  );
});

export type EmptyData = { editable: boolean; envName: string; onPick: (k: "github" | "database" | "template" | "image" | "empty") => void } & Record<string, unknown>;

/** The empty project: one 408x360 card with the ways to add a first service. */
export const EmptyNode = memo(({ data: d }: NodeProps<Node<EmptyData>>) => {
  const rows: { k: "github" | "database" | "template" | "image" | "empty"; label: string; icon: ReactNode }[] = [
    { k: "github", label: "GitHub Repo", icon: <BrandIcon name="github" size={16} mono /> },
    { k: "database", label: "Database", icon: <Database size={16} /> },
    { k: "template", label: "Template", icon: <Layers size={16} /> },
    { k: "image", label: "Docker Image", icon: <Package size={16} /> },
    { k: "empty", label: "Empty Service", icon: <SquareTerminal size={16} /> },
  ];
  return (
    <div style={{ width: EMPTY_W, height: EMPTY_H }} className="cv-node flex flex-col rounded-xl border border-dashed border-gray-300 bg-secondaryBg p-6" data-testid="empty-canvas">
      <div className="flex items-center gap-3">
        <span className="flex h-10 w-10 items-center justify-center rounded-lg border border-line-popover bg-surface-hover text-fg-2"><Plus size={20} /></span>
        <div className="min-w-0">
          <p className="text-base font-semibold leading-6 text-foreground">Add a service</p>
          <p className="truncate text-sm leading-5 text-fg-3">Nothing runs in {d.envName} yet.</p>
        </div>
      </div>
      <div className="mt-5 flex flex-col gap-1">
        {rows.map((r) => (
          <button key={r.k} type="button" disabled={!d.editable} onClick={(e) => { e.stopPropagation(); d.onPick(r.k); }}
            className="nodrag cv-press flex h-10 items-center gap-3 rounded-md px-3 text-left text-sm text-fg-2 hover:bg-pink-50 hover:text-pink-600 disabled:pointer-events-none disabled:opacity-40">
            <span className="flex w-4 justify-center">{r.icon}</span>{r.label}
          </button>
        ))}
      </div>
      <p className="mt-auto text-caption text-fg-4">Or right-click anywhere on the canvas.</p>
    </div>
  );
});
