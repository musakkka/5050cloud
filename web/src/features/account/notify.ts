// SPDX-License-Identifier: AGPL-3.0-only

import type * as T from "../../api/types.gen";

export type Severity = "danger" | "warn" | "neutral";

export const NOTIFY_EVENTS: { id: string; label: string; help: string; tone: Severity }[] = [
  { id: "deployment.failed", label: "A deployment failed", help: "A build or release did not finish.", tone: "warn" },
  { id: "deployment.crashed", label: "A deployment crashed", help: "A running service exited and could not be restarted.", tone: "danger" },
  { id: "backup.failed", label: "A backup failed", help: "A scheduled or manual backup did not complete.", tone: "danger" },
  { id: "disk.high", label: "A host disk is nearly full", help: "A host passed its disk usage warning level.", tone: "warn" },
  { id: "volume.high", label: "A volume is nearly full", help: "A service volume is close to its size.", tone: "warn" },
  { id: "monitor.triggered", label: "A monitor triggered", help: "A metric crossed a threshold you set.", tone: "warn" },
  { id: "host.offline", label: "A host went offline", help: "A host stopped reporting to the control plane.", tone: "danger" },
];

/** Fills in every event so the form always shows a full table. */
export function fullPrefs(p: T.NotificationPref[]): T.NotificationPref[] {
  return NOTIFY_EVENTS.map((e) => p.find((x) => x.event === e.id) || { event: e.id, email: false, inApp: true });
}

export type Delivery = "both" | "email" | "app" | "none";
export const toDelivery = (p: T.NotificationPref): Delivery => (p.email && p.inApp ? "both" : p.email ? "email" : p.inApp ? "app" : "none");
export const fromDelivery = (d: Delivery): { email: boolean; inApp: boolean } => ({ email: d === "both" || d === "email", inApp: d === "both" || d === "app" });
