// SPDX-License-Identifier: AGPL-3.0-only

// Pieces shared by the deployment list and the drawer: badges, pills, the
// author avatar with its source badge, the step list and the action menus.

import { useState, type ReactNode } from "react";
import { Check, CircleCheck, CircleX, EllipsisVertical, Loader2, X } from "lucide-react";
import type { Deployment } from "../../api/types.gen";
import { Avatar, BrandIcon, Confirm, IconButton, Popover, MenuItem, cx, toast } from "../../kit";
import { BADGE, PILL, groupSteps, labelOf, lookOf, mmss, type Look, type StepGroup } from "./format";
import { ACTION_LABEL, actionsFor, runAction, type DepAction } from "./actions";

/** The square caps badge on list cards (26px, 6px radius, no border). */
export function StatusBadge({ d }: { d: Pick<Deployment, "label" | "status" | "active"> }) {
  return (
    <span className={cx("inline-flex h-[26px] shrink-0 items-center whitespace-nowrap rounded-md px-2 text-caption font-medium uppercase", BADGE[lookOf(d)])}>
      {labelOf(d)}
    </span>
  );
}

/** The rounded pill next to the id in the drawer header. */
export function StatusPill({ d }: { d: Pick<Deployment, "label" | "status" | "active"> }) {
  const look = lookOf(d);
  return (
    <span className={cx("inline-flex h-6 shrink-0 items-center gap-1.5 whitespace-nowrap rounded-full border px-2 text-sm font-medium", PILL[look])}>
      {look === "busy" && <Loader2 size={12} className="spin" />}
      {labelOf(d)}
    </span>
  );
}

/** Who deployed it, with a small badge for where it came from (GitHub, CLI, image). */
export function AuthorAvatar({ d, size = 24, ring = "border-surface-hover" }: { d: Pick<Deployment, "actor" | "commit" | "trigger" | "image">; size?: number; ring?: string }) {
  const name = d.commit?.author || (d.actor || "").replace(/^(github|token|agent):/, "") || "5050";
  const src = d.trigger === "github" || (d.trigger === "commit" && d.commit) ? "github"
    : d.trigger === "cli" ? "cli" : d.image && !d.commit ? "docker" : d.trigger === "cron" ? "cron" : d.commit ? "github" : "console";
  return (
    <span className="relative inline-flex shrink-0" style={{ width: size + 8, height: size + 8 }}>
      <Avatar name={name} size={size} />
      <span className={cx("absolute bottom-0 right-0 inline-flex items-center justify-center rounded-full border-2 bg-surface-hover text-foreground", ring)}
        style={{ width: 20, height: 20 }} title={src === "github" ? "GitHub" : src === "cli" ? "CLI" : src === "docker" ? "Image" : "Console"}>
        {src === "github" ? <BrandIcon name="github" size={14} />
          : src === "docker" ? <BrandIcon name="docker" size={11} />
          : <BrandIcon name={null} fallback={src === "cron" ? "clock" : "terminal"} size={11} className="text-fg-2" />}
      </span>
    </span>
  );
}

const STEP_ICON: Record<StepGroup["state"], ReactNode> = {
  ok: <Check size={16} className="text-ok" />,
  failed: <X size={16} className="text-danger-text" />,
  running: <Loader2 size={16} className="spin text-blue-600" />,
  pending: <span className="block h-[15px] w-[15px] rounded-full border-[1.5px] border-fg-5" />,
  skipped: <span className="block h-[15px] w-[15px] rounded-full border-[1.5px] border-dashed border-fg-5" />,
};

/**
 * The step rows under a status bar: 44px rows, icon column, label, (mm:ss) on the right.
 * `indent` lines the icon up with the card's avatar column; `children` render after the failed row (diagnosis).
 */
export function StepRows({ d, indent = 24, labelGap = 24, after }: { d: Pick<Deployment, "steps" | "status">; indent?: number; labelGap?: number; after?: ReactNode }) {
  const groups = groupSteps(d.steps);
  const fi = groups.findIndex((g) => g.state === "failed");
  return (
    <ol className="animate-collapsible-down overflow-hidden">
      {groups.map((g, i) => (
        <li key={g.key}>
          <div className={cx("flex min-h-[44px] items-center pr-4 text-sm", g.state === "failed" && "bg-danger-bg")} style={{ paddingLeft: indent }}>
            <span className="flex w-4 shrink-0 justify-center">{STEP_ICON[g.state]}</span>
            <span className={cx("min-w-0 flex-1 truncate", g.state === "failed" ? "text-danger-text" : g.state === "pending" || g.state === "skipped" ? "text-fg-3" : "text-foreground")}
              style={{ marginLeft: labelGap }}>{g.label}</span>
            <span className={cx("shrink-0 tabular-nums", g.state === "failed" ? "text-danger-text" : "text-fg-3")}>
              {g.state === "pending" ? (fi >= 0 && i > fi ? "Not started" : d.status === "FAILED" || d.status === "CANCELLED" ? "Not started" : "Waiting")
                : g.state === "skipped" ? "Skipped" : g.ms !== undefined ? `(${mmss(g.ms)})` : ""}
            </span>
          </div>
          {g.state === "failed" && g.detail && (
            <div className="flex bg-danger-bg pb-3 pr-4" style={{ paddingLeft: indent }}>
              <span className="flex w-4 shrink-0 justify-center"><span className="block w-px self-stretch bg-danger-line" /></span>
              <span className="font-mono text-sm text-danger-text" style={{ marginLeft: labelGap }}>{g.detail}</span>
            </div>
          )}
          {i === fi && after}
        </li>
      ))}
    </ol>
  );
}

/** Icon and colour of a status bar (card footer, Details header). */
export function OutcomeIcon({ look, size = 16 }: { look: Look; size?: number }) {
  if (look === "bad") return <CircleX size={size} className="shrink-0 text-danger-text" />;
  if (look === "busy" || look === "wait") return <Loader2 size={size} className="spin shrink-0 text-blue-600" />;
  return <CircleCheck size={size} className="shrink-0 text-ok" />;
}

export const OUTCOME_TEXT: Record<Look, string> = { ok: "text-ok", idle: "text-ok", bad: "text-danger", busy: "text-blue-600", wait: "text-yellow-600" };

const CONFIRM_BODY: Record<DepAction, string> = {
  rollback: "Starts a new deployment from this one's image and the variables it ran with. The current deployment is replaced once it is healthy.",
  remove: "Stops this deployment and its replicas. The service has no active deployment until you deploy again.",
  abort: "Stops this deployment before it finishes. The active deployment keeps serving.",
  restart: "Restarts the replicas of this deployment with the same image and variables. Requests may fail for a few seconds.",
  redeploy: "Builds and deploys this commit again.",
};
const NEEDS_CONFIRM = new Set<DepAction>(["rollback", "remove", "abort", "restart"]);
const DONE: Record<DepAction, string> = {
  redeploy: "Redeploy started", rollback: "Rollback started", restart: "Restarting replicas", abort: "Deployment aborted", remove: "Deployment removed",
};

/**
 * Runs a deployment action, asking first for the dangerous ones. Render `dialog` once; call `request(d, a)` from menus.
 */
export function useDepActions(onDone: (a: DepAction, result?: Deployment) => void) {
  const [pending, setPending] = useState<{ d: Deployment; a: DepAction } | null>(null);
  const exec = async (d: Deployment, a: DepAction) => {
    const r = await runAction(d.id, a);
    toast(DONE[a], "success");
    onDone(a, r);
  };
  const request = (d: Deployment, a: DepAction) => {
    if (NEEDS_CONFIRM.has(a)) { setPending({ d, a }); return; }
    exec(d, a).catch((e) => toast(e instanceof Error ? e.message : String(e), "error"));
  };
  const dialog = pending ? (
    <Confirm title={`${ACTION_LABEL[pending.a]} deployment ${pending.d.id.slice(0, 8)}?`} body={CONFIRM_BODY[pending.a]}
      confirm={ACTION_LABEL[pending.a]} danger={pending.a === "remove" || pending.a === "abort"}
      onClose={() => setPending(null)} onConfirm={() => exec(pending.d, pending.a)} />
  ) : null;
  return { request, dialog };
}

/** The kebab on a card or in the drawer header: 220px menu, 36px rows, danger rows in red. */
export function DepMenu({ d, onAction, onViewLogs, label, size = 24, horizontal }: {
  d: Deployment; onAction: (d: Deployment, a: DepAction) => void; onViewLogs?: () => void; label?: string; size?: 24 | 34; horizontal?: boolean;
}) {
  const actions = actionsFor(d);
  if (!actions.length && !onViewLogs) return null;
  return (
    <Popover align="right" minWidth={220} wrapperClassName="relative shrink-0"
      trigger={(open) => (
        <IconButton label={label || `Actions for deployment ${d.id.slice(0, 8)}`} size={size} active={open} aria-haspopup="menu" aria-expanded={open}
          className={horizontal ? "text-foreground" : "text-foreground"}>
          {horizontal ? <span className="flex gap-[3px]">{[0, 1, 2].map((i) => <span key={i} className="h-[3px] w-[3px] rounded-full bg-current" />)}</span>
            : <EllipsisVertical size={18} strokeWidth={2.75} />}
        </IconButton>
      )}>
      {(close) => (
        <>
          {onViewLogs && <MenuItem onClick={() => { close(); onViewLogs(); }}>View logs</MenuItem>}
          {actions.map((a) => (
            <MenuItem key={a} danger={a === "remove" || a === "abort"} onClick={() => { close(); onAction(d, a); }}>{ACTION_LABEL[a]}</MenuItem>
          ))}
        </>
      )}
    </Popover>
  );
}
