// SPDX-License-Identifier: AGPL-3.0-only

import type { Deployment } from "../../api/types.gen";
import { post } from "../service/client";

export type DepAction = "redeploy" | "rollback" | "restart" | "abort" | "remove";

export const runAction = (did: string, a: DepAction) => post<Deployment>(`/deployments/${encodeURIComponent(did)}/${a}`);

const IN_PROGRESS = new Set(["QUEUED", "WAITING", "BUILDING", "DEPLOYING"]);
export const inProgress = (d: Deployment) => IN_PROGRESS.has(d.status);

/** Which actions the three-dot menu offers for a deployment in this state (13.3). */
export function actionsFor(d: Deployment): DepAction[] {
  if (inProgress(d)) return ["abort"];
  const out: DepAction[] = [];
  if (d.active) {
    out.push("restart", "redeploy", "remove");
  } else {
    out.push("redeploy");
    if (d.canRollback && (d.status === "SUCCESS" || d.status === "REMOVED" || d.status === "COMPLETED")) out.push("rollback");
  }
  if (d.status === "CRASHED" && !out.includes("restart")) out.unshift("restart");
  return out;
}

export const ACTION_LABEL: Record<DepAction, string> = {
  redeploy: "Redeploy", rollback: "Rollback", restart: "Restart", abort: "Abort", remove: "Remove",
};

/** The tab the deployment drawer opens on: Details for a failure (steps and diagnosis), Build Logs while building, else Deploy Logs. */
export function defaultTab(d: Deployment): "details" | "build" | "deploy" {
  if (d.status === "FAILED") return "details";
  if (d.status === "BUILDING" || d.status === "QUEUED" || d.status === "WAITING") return "build";
  return "deploy";
}

export const shortId = (id: string) => id.slice(0, 8);
