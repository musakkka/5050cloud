// SPDX-License-Identifier: AGPL-3.0-only

// Host display rules shared by the list, the grid, the detail sheet and the dialogs: state labels and
// tones, capacity thresholds, role copy and icons.
import { Boxes, Database, Globe, Hammer, Server, ShieldCheck, type LucideIcon } from "lucide-react";
import type { BadgeTone, Tone } from "../../kit";

export const ROLES = ["control", "build", "ingress", "app", "data"];

/** Returns the roles with `role` added or removed, in the canonical order. */
export function toggleRole(roles: string[], role: string, on: boolean): string[] {
  const next = on ? [...new Set([...roles, role])] : roles.filter((r) => r !== role);
  return ROLES.filter((r) => next.includes(r));
}

export const ROLE_INFO: Record<string, { hint: string; icon: LucideIcon }> = {
  control: { hint: "Runs the console, API and scheduler. Stays on the first machine.", icon: ShieldCheck },
  build: { hint: "Builds images from source.", icon: Hammer },
  ingress: { hint: "Terminates HTTPS and routes public traffic.", icon: Globe },
  app: { hint: "Runs service replicas.", icon: Boxes },
  data: { hint: "Holds volumes for stateful services.", icon: Database },
};

/** The icon for a machine follows its most specific role. */
export function roleIcon(roles: string[]): LucideIcon {
  const first = ROLES.find((r) => roles.includes(r));
  return (first && ROLE_INFO[first]?.icon) || Server;
}

export const STATE: Record<string, { label: string; tone: BadgeTone; dot: Tone }> = {
  online: { label: "Online", tone: "ok", dot: "ok" },
  draining: { label: "Draining", tone: "warn", dot: "warn" },
  maintenance: { label: "Maintenance", tone: "warn", dot: "warn" },
  joining: { label: "Joining", tone: "info", dot: "busy" },
  offline: { label: "Offline", tone: "danger", dot: "bad" },
  lost: { label: "Lost", tone: "danger", dot: "bad" },
  unknown: { label: "Unknown", tone: "neutral", dot: "idle" },
};

export const stateOf = (state: string) => STATE[state] ?? { label: state, tone: "neutral" as BadgeTone, dot: "idle" as Tone };

/** Offline, lost and unknown machines dim their meta line, as the reference console dims offline services. */
export const isDimmed = (state: string) => state === "offline" || state === "lost" || state === "unknown";

/** Used / total as a whole percent, clamped to 0 to 100. */
export const pct = (used: number, total: number) => (total > 0 ? Math.min(100, Math.max(0, Math.round((used / total) * 100))) : 0);

/** Bars turn yellow at 75 percent and red at 90 percent. */
export const capacityTone = (p: number): "accent" | "warn" | "danger" => (p >= 90 ? "danger" : p >= 75 ? "warn" : "accent");

export const ROLE_OPTIONS = ROLES.filter((r) => r !== "control");
