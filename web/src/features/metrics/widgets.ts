// SPDX-License-Identifier: AGPL-3.0-only

// Observability widget model. Boards are stored per environment in this browser until the
// control plane grows a dashboards API (see the metrics request file).

import { useCallback, useState } from "react";
import { Activity, AlertTriangle, Cpu, Globe, Gauge, HardDrive, MemoryStick, Network, ScrollText, Timer, FileText, Waypoints, type LucideIcon } from "lucide-react";
import { readPref, writePref } from "../../kit";
import type { WidgetMetric } from "./model";

export interface Widget { id: string; metric: WidgetMetric; /** Service id, empty for all services. */ service: string; title: string; description?: string }

export interface MetricType { id: WidgetMetric; label: string; icon: LucideIcon; log?: "deploy" | "http" | "dns" | "network" }

/** Order matches the picker. */
export const METRIC_TYPES: MetricType[] = [
  { id: "cpu", label: "CPU Usage", icon: Cpu },
  { id: "memory", label: "Memory Usage", icon: MemoryStick },
  { id: "egress", label: "Network Egress", icon: Network },
  { id: "disk", label: "Disk Usage", icon: HardDrive },
  { id: "logs", label: "Logs", icon: ScrollText, log: "deploy" },
  { id: "usage", label: "Project Usage", icon: Gauge },
  { id: "http", label: "HTTP Requests", icon: Globe },
  { id: "http5xx", label: "HTTP 5xx Rate", icon: AlertTriangle },
  { id: "latency", label: "HTTP Latency", icon: Timer },
  { id: "httplogs", label: "HTTP Logs", icon: FileText, log: "http" },
  { id: "dnslogs", label: "DNS Logs", icon: Waypoints, log: "dns" },
  { id: "netlogs", label: "Network Logs", icon: Activity, log: "network" },
];

export const typeOf = (id: WidgetMetric) => METRIC_TYPES.find((t) => t.id === id) ?? METRIC_TYPES[0];

export const newId = () => `w_${Math.random().toString(36).slice(2, 10)}`;

/** The "simple dashboard" starter board. */
export const STARTER: Omit<Widget, "id">[] = [
  { metric: "cpu", service: "", title: "CPU Usage" },
  { metric: "memory", service: "", title: "Memory Usage" },
  { metric: "egress", service: "", title: "Network Egress" },
  { metric: "http", service: "", title: "HTTP Requests" },
];

const key = (eid: string) => `5050.obs.widgets.${eid}`;

export function useWidgets(eid: string) {
  const [list, setList] = useState<Widget[]>(() => readPref<Widget[]>(key(eid), []));
  const commit = useCallback((next: Widget[]) => { setList(next); writePref(key(eid), next); }, [eid]);
  return {
    widgets: list,
    add: (w: Omit<Widget, "id">) => commit([...list, { ...w, id: newId() }]),
    addMany: (ws: Omit<Widget, "id">[]) => commit([...list, ...ws.map((w) => ({ ...w, id: newId() }))]),
    update: (w: Widget) => commit(list.map((x) => (x.id === w.id ? w : x))),
    remove: (id: string) => commit(list.filter((x) => x.id !== id)),
    move: (from: number, to: number) => {
      if (from === to || from < 0 || to < 0 || from >= list.length || to >= list.length) return;
      const next = [...list];
      const [m] = next.splice(from, 1);
      next.splice(to, 0, m);
      commit(next);
    },
  };
}
