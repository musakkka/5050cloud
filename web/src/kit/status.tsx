// SPDX-License-Identifier: AGPL-3.0-only

// One StatusGlyph everywhere (SPEC 13.4): cards, panels, the palette, activity.
import { AlertTriangle, CircleAlert, CircleCheck, CircleDashed, CircleSlash, Loader2, Moon } from "lucide-react";

/** Service statuses (api.Status) and deployment labels map onto five looks. */
export type Tone = "ok" | "busy" | "bad" | "warn" | "idle";

export function toneOf(status: string): Tone {
  switch (status.toLowerCase()) {
    case "online": case "active": case "success": case "completed":
      return "ok";
    case "deploying": case "building": case "initializing": case "waiting": case "pending": case "removing":
      return "busy";
    case "crashed": case "failed": case "host_lost":
      return "bad";
    case "unhealthy":
      return "warn";
    default:
      return "idle";
  }
}

export const STATUS_TEXT: Record<string, string> = {
  online: "Online", deploying: "Deploying", building: "Building", crashed: "Crashed", unhealthy: "Unhealthy", failed: "Failed",
  pending: "Pending", sleeping: "Sleeping", no_deployment: "No deployment", removed: "Removed", completed: "Completed", host_lost: "Host lost",
};

export const TONE_TEXT: Record<Tone, string> = { ok: "text-gray-600", busy: "text-blue-600", bad: "text-red-600", warn: "text-yellow-600", idle: "text-gray-500" };
export const TONE_DOT: Record<Tone, string> = { ok: "bg-green-500", busy: "bg-blue-500", bad: "bg-red-500", warn: "bg-yellow-500", idle: "bg-gray-400" };

export function StatusGlyph({ status, size = 18 }: { status: string; size?: number }) {
  const label = STATUS_TEXT[status] || status;
  const p = { size, strokeWidth: 1.75, "aria-label": label, role: "img" as const };
  if (status === "sleeping") return <Moon {...p} className="shrink-0 text-gray-500" />;
  if (status === "pending") return <CircleDashed {...p} className="shrink-0 text-blue-600" />;
  switch (toneOf(status)) {
    case "ok": return <CircleCheck {...p} className="shrink-0 text-success" />;
    case "busy": return <Loader2 {...p} className="shrink-0 spin text-blue-600" />;
    case "bad": return <CircleAlert {...p} className="shrink-0 text-red-600" />;
    case "warn": return <AlertTriangle {...p} className="shrink-0 text-yellow-600" />;
    default: return <CircleSlash {...p} className="shrink-0 text-gray-500" />;
  }
}
