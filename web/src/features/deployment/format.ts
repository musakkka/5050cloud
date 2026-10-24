// SPDX-License-Identifier: AGPL-3.0-only

// Pure helpers for the deployment views: status looks, step groups,
// durations and timestamps. No React here so they are easy to test.

import type { Deployment, DeployStep } from "../../api/types.gen";

/** Epoch seconds or milliseconds, normalised to milliseconds. */
export const toMs = (ts: number) => (ts > 1e12 ? ts : ts * 1000);

const pad = (n: number) => String(n).padStart(2, "0");

/** "GMT+3" style label for the viewer's time zone. */
export function tzLabel(at = Date.now()): string {
  try {
    const parts = new Intl.DateTimeFormat("en-US", { timeZoneName: "shortOffset" } as Intl.DateTimeFormatOptions).formatToParts(new Date(at));
    const z = parts.find((p) => p.type === "timeZoneName")?.value;
    if (z) return z === "GMT" ? "GMT+0" : z;
  } catch { /* older engines */ }
  const off = -new Date(at).getTimezoneOffset() / 60;
  return `GMT${off >= 0 ? "+" : ""}${off}`;
}

/** "2026-10-20 18:48" in local time. */
export function fmtMinute(ts: number): string {
  const d = new Date(toMs(ts));
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

/** "2026-10-20 18:48:02" in local time (log rows). */
export function fmtSecond(ts: number): string {
  const d = new Date(toMs(ts));
  return `${fmtMinute(ts)}:${pad(d.getSeconds())}`;
}

/** "01:42" (mm:ss), hours folded into minutes. */
export function mmss(ms: number): string {
  const s = Math.max(0, Math.round(ms / 1000));
  return `${pad(Math.floor(s / 60))}:${pad(s % 60)}`;
}

/** "1ms", "985ms", "34s", "2m 5s" for build step durations. */
export function shortDuration(ms: number): string {
  if (ms < 1000) return `${Math.max(0, Math.round(ms))}ms`;
  const s = Math.round(ms / 1000);
  if (s < 60) return `${s}s`;
  return `${Math.floor(s / 60)}m ${s % 60}s`;
}

export type Look = "ok" | "bad" | "busy" | "wait" | "idle";

/** The look of a deployment: green active, red failed or crashed, blue in progress, grey history. */
export function lookOf(d: Pick<Deployment, "status" | "active">): Look {
  switch (d.status) {
    case "SUCCESS": return d.active ? "ok" : "idle";
    case "COMPLETED": return "ok";
    case "FAILED": case "CRASHED": return "bad";
    case "BUILDING": case "DEPLOYING": case "REMOVING": return "busy";
    case "QUEUED": case "WAITING": return "wait";
    default: return "idle";
  }
}

/** The word on the badge. The server sends it (label); fall back to the status. */
export function labelOf(d: Pick<Deployment, "label" | "status" | "active">): string {
  if (d.label) return d.label;
  if (d.status === "SUCCESS") return d.active ? "Active" : "Removed";
  return d.status.charAt(0) + d.status.slice(1).toLowerCase();
}

/** Square list badge: 26px tall, caps text, no border. */
export const BADGE: Record<Look, string> = {
  ok: "bg-green-200 text-ok-soft",
  bad: "bg-danger-line text-danger-soft",
  busy: "bg-blue-100 text-blue-700",
  wait: "bg-yellow-100 text-yellow-600",
  idle: "bg-line text-fg-3",
};

/** Rounded drawer pill: 1px border, tinted fill. */
export const PILL: Record<Look, string> = {
  ok: "border-green-300 bg-green-50 text-ok-text",
  bad: "border-red-300 bg-danger-bg text-danger-text",
  busy: "border-blue-200 bg-blue-50 text-blue-600",
  wait: "border-yellow-300 bg-yellow-50 text-yellow-600",
  idle: "border-line bg-surface text-fg-2",
};

const TRIGGER: Record<string, string> = {
  github: "GitHub", cli: "CLI", ui: "Console", commit: "Console", redeploy: "Redeploy", rollback: "Rollback",
  template: "Template", cron: "Schedule", restore: "Restore", image: "Docker image", api: "API",
};

/** "GitHub", "CLI", ... for "3 days ago via GitHub". */
export const viaLabel = (trigger: string) => TRIGGER[trigger] ?? (trigger ? trigger.charAt(0).toUpperCase() + trigger.slice(1) : "Console");

/** The one-line title of a deployment card. */
export function titleOf(d: Pick<Deployment, "commit" | "message" | "image" | "id" | "trigger" | "rollbackOf">): string {
  const first = (s?: string) => (s || "").split("\n")[0].trim();
  return first(d.commit?.message) || first(d.message) || (d.rollbackOf ? `Rollback to ${d.rollbackOf.slice(0, 8)}` : "") || d.image || `Deployment ${d.id.slice(0, 8)}`;
}

export type StepState = "ok" | "failed" | "running" | "pending" | "skipped";

export interface StepGroup {
  key: string;
  label: string;
  state: StepState;
  ms?: number;
  /** The reason line under a failed group (monospace, red). */
  detail?: string;
}

const GROUPS: { key: string; label: string; steps: string[] }[] = [
  { key: "init", label: "Initialization", steps: ["queued", "fetch", "snapshot"] },
  { key: "build", label: "Build", steps: ["build", "push"] },
  { key: "deploy", label: "Deploy", steps: ["pull image", "pull", "pre_deploy", "start"] },
  { key: "network", label: "Network", steps: ["healthcheck"] },
  { key: "post", label: "Post-deploy", steps: ["cutover", "drain"] },
];

function groupOf(step: string): string {
  const n = step.toLowerCase();
  return GROUPS.find((g) => g.steps.includes(n))?.key ?? "deploy";
}

/** Fold the server's fine-grained steps into the five rows the cards show. */
export function groupSteps(steps: DeployStep[]): StepGroup[] {
  const out: StepGroup[] = [];
  for (const g of GROUPS) {
    const mine = steps.filter((s) => groupOf(s.name) === g.key);
    if (!mine.length) {
      // The network row only shows when the deployment has a healthcheck step.
      if (g.key === "network") continue;
      out.push({ key: g.key, label: g.label, state: "pending" });
      continue;
    }
    const failed = mine.find((s) => s.state === "failed");
    const state: StepState = failed ? "failed"
      : mine.some((s) => s.state === "running") ? "running"
      : mine.every((s) => s.state === "pending") ? "pending"
      : mine.every((s) => s.state === "skipped") ? "skipped"
      : mine.some((s) => s.state === "pending") ? "running" : "ok";
    const starts = mine.map((s) => s.startedAt).filter((v): v is number => !!v);
    const ends = mine.map((s) => s.finishedAt).filter((v): v is number => !!v);
    const ms = starts.length && (ends.length || state === "running")
      ? toMs(ends.length ? Math.max(...ends) : Date.now()) - toMs(Math.min(...starts)) : undefined;
    let label = g.label;
    if (g.key === "network") label = "Network › Healthcheck";
    else if (failed && mine.length > 1) label = `${g.label} › ${failed.name.replace(/_/g, "-").replace(/^./, (c) => c.toUpperCase())}`;
    out.push({ key: g.key, label, state, ms, detail: failed?.detail });
  }
  // Nothing after a failure ran.
  const fi = out.findIndex((g) => g.state === "failed");
  if (fi >= 0) for (let i = fi + 1; i < out.length; i++) if (out[i].state !== "ok") out[i] = { ...out[i], state: "pending", ms: undefined };
  return out;
}

/** Total time from the first step start to the end (or now while running). */
export function totalMs(d: Pick<Deployment, "startedAt" | "finishedAt" | "createdAt" | "steps">): number | undefined {
  const start = d.startedAt ?? d.steps.map((s) => s.startedAt).filter((v): v is number => !!v).sort((a, b) => a - b)[0];
  if (!start) return undefined;
  const end = d.finishedAt ?? d.steps.map((s) => s.finishedAt).filter((v): v is number => !!v).sort((a, b) => b - a)[0];
  return toMs(end ?? Date.now()) - toMs(start);
}

/** The sentence in the status bar under a card or at the top of Details. */
export function outcomeText(d: Pick<Deployment, "status" | "steps" | "active">): string {
  const groups = groupSteps(d.steps);
  switch (d.status) {
    case "FAILED": {
      const g = groups.find((x) => x.state === "failed");
      return g ? `Deployment failed during the ${g.label.split(" ›")[0].toLowerCase()} process` : "Deployment failed";
    }
    case "CRASHED": return "Deployment crashed after it started";
    case "QUEUED": case "WAITING": return "Waiting for a build slot";
    case "BUILDING": return "Building the image";
    case "DEPLOYING": {
      const g = groups.find((x) => x.state === "running");
      return g ? `Deploying: ${g.label.toLowerCase()}` : "Deploying";
    }
    case "REMOVING": return "Removing the deployment";
    case "CANCELLED": return "Deployment cancelled";
    case "SKIPPED": return "Deployment skipped";
    case "COMPLETED": return "Run completed";
    default: return "Deployment successful";
  }
}

/** Filter query parsing for the log views: `@key:value`, `-@key:value`, `-word`, quoted phrases, plain words. */
export interface Term { key?: string; value: string; neg: boolean }

export function parseQuery(q: string): Term[] {
  const out: Term[] = [];
  const re = /(-?)(?:@([\w.-]+):)?(?:"((?:[^"\\]|\\.)*)"|(\S+))/g;
  let m: RegExpExecArray | null;
  while ((m = re.exec(q))) {
    const value = (m[3] ?? m[4] ?? "").replace(/\\"/g, '"');
    if (!value && !m[2]) continue;
    if (!m[2] && (value === "AND" || value === "OR")) continue;
    out.push({ neg: m[1] === "-", key: m[2], value });
  }
  return out;
}

/** True when a line passes every term. Keys match level, replica, deployment, stream or an attribute. */
export function matches(line: { msg: string; level: string; replica?: string; deploymentId?: string; stream: string; attrs?: Record<string, unknown> }, terms: Term[]): boolean {
  for (const t of terms) {
    let hit: boolean;
    const v = t.value.toLowerCase();
    if (t.key) {
      const k = t.key;
      const raw = k === "level" ? line.level : k === "replica" ? line.replica : k === "deployment" ? line.deploymentId : k === "stream" ? line.stream : k === "message" || k === "msg" ? line.msg : line.attrs?.[k];
      const s = raw === undefined || raw === null ? "" : String(raw).toLowerCase();
      hit = v.endsWith("*") ? s.startsWith(v.slice(0, -1)) : s === v || (k === "path" && s.startsWith(v)) || (k === "deployment" && s.startsWith(v));
    } else {
      hit = line.msg.toLowerCase().includes(v);
    }
    if (hit === t.neg) return false;
  }
  return true;
}
