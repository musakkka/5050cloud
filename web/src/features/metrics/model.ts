// SPDX-License-Identifier: AGPL-3.0-only

// Turns an API metrics response into chart models (series, axis and tooltip formats) for the
// service Metrics tab and for Observability widgets.

import type { MetricsResponse } from "../../api/types.gen";
import { PALETTE, SERIES, type ChartSeries } from "./chart";
import { fmtBytesDec } from "./series";

export type MetricKind = "cpu" | "mem" | "net" | "disk" | "req" | "err" | "lat";
export type View = "sum" | "replicas";

export interface Model {
  title: string;
  series: ChartSeries[];
  kind: "line" | "bars";
  minMax: number;
  fmtAxis: (v: number, max: number) => string;
  fmtValue: (v: number) => string;
  limit?: { text: string };
  total?: number;
}

const pct = (v: number) => `${(v * 100).toFixed(1)}%`;
const ms = (v: number) => `${Math.round(v)}ms`;
const cpu = (v: number) => `${v.toFixed(2)} vCPU`;
const limitBytes = (v: number) => {
  const u = ["B", "KB", "MB", "GB", "TB"];
  let i = 0, x = v;
  while (x >= 1000 && i < u.length - 1) { x /= 1000; i++; }
  return `${x.toFixed(2)} ${u[i]}`;
};

const stepSecs = (d: MetricsResponse) => Math.max(1, (d.step > 1000 ? d.step : 60000) / 1000);
const scaled = (pts: number[][] | undefined, f: number) => (pts || []).map(([t, v]) => [t, v * f]);

export function buildModel(k: MetricKind, d: MetricsResponse, view: View = "replicas"): Model {
  const s = d.series as Record<string, number[][] | undefined>;
  const secs = stepSecs(d);
  switch (k) {
    case "cpu":
    case "mem": {
      const key = k;
      const per = d.perReplica && Object.keys(d.perReplica).length > 0 && view === "replicas";
      const series: ChartSeries[] = per
        ? Object.entries(d.perReplica!).map(([rid, v], i) => ({ key: rid, name: `replica-${i}`, color: PALETTE[i % PALETTE.length], points: v[key] || [] }))
        : [{ key: "sum", name: view === "sum" ? "Sum" : "replica-0", color: SERIES.accent, points: s[key] || [] }];
      return k === "cpu"
        ? { title: "CPU", series, kind: "line", minMax: 0.2, fmtAxis: cpu, fmtValue: cpu, limit: d.cpuLimit ? { text: cpu(d.cpuLimit) } : undefined }
        : { title: "Memory", series, kind: "line", minMax: 400e6, fmtAxis: (v, m) => fmtBytesDec(v, m), fmtValue: (v) => fmtBytesDec(v), limit: d.memLimit ? { text: limitBytes(d.memLimit) } : undefined };
    }
    case "disk":
      return { title: "Disk", series: [{ key: "disk", name: "Used", color: SERIES.accent, points: s.disk || [] }], kind: "line", minMax: 200e6, fmtAxis: (v, m) => fmtBytesDec(v, m), fmtValue: (v) => fmtBytesDec(v) };
    case "net":
      return {
        title: "Public Network Traffic", kind: "line", minMax: 4000,
        series: [
          { key: "tx", name: "Egress", color: SERIES.gold, points: scaled(s.tx, secs) },
          { key: "rx", name: "Ingress", color: SERIES.blue, points: scaled(s.rx, secs) },
        ],
        fmtAxis: (v, m) => fmtBytesDec(v, m), fmtValue: (v) => fmtBytesDec(v),
      };
    case "req": {
      const cls = [["req_2xx", "2xx", SERIES.blue], ["req_3xx", "3xx", SERIES.accent], ["req_4xx", "4xx", SERIES.gold], ["req_5xx", "5xx", SERIES.red]] as const;
      const series = cls.map(([key, name, color]) => ({ key, name, color, points: scaled(s[key], secs) }));
      const total = series.reduce((a, x) => a + x.points.reduce((b, p) => b + p[1], 0), 0);
      return { title: "Requests", kind: "bars", minMax: 4, series, fmtAxis: (v) => String(Math.round(v)), fmtValue: (v) => String(Math.round(v)), total };
    }
    case "err":
      return { title: "Request Error Rate", kind: "line", minMax: 0.04, series: [{ key: "error_rate", name: "Error Rate", color: SERIES.red, points: s.error_rate || [] }], fmtAxis: pct, fmtValue: pct };
    case "lat":
      return {
        title: "Response Time", kind: "line", minMax: 200,
        series: [
          { key: "p50", name: "p50 (median)", color: SERIES.blue, points: s.p50 || [] },
          { key: "p90", name: "p90", color: SERIES.gold, points: s.p90 || [] },
          { key: "p95", name: "p95", color: SERIES.accent, points: s.p95 || [] },
          { key: "p99", name: "p99", color: SERIES.red, points: s.p99 || [] },
        ],
        fmtAxis: ms, fmtValue: ms,
      };
  }
}

/** A chart with one line per service (Observability widgets). */
export function buildServicesModel(metric: WidgetMetric, entries: { name: string; color: string; data: MetricsResponse }[], title: string): Model {
  const pick = (d: MetricsResponse): { pts: number[][]; model: Model } => {
    const s = d.series as Record<string, number[][] | undefined>;
    const secs = stepSecs(d);
    switch (metric) {
      case "cpu": return { pts: s.cpu || [], model: buildModel("cpu", d, "sum") };
      case "memory":
      case "usage": return { pts: s.mem || [], model: buildModel("mem", d, "sum") };
      case "egress": return { pts: scaled(s.tx, secs), model: buildModel("net", d) };
      case "disk": return { pts: s.disk || [], model: buildModel("disk", d) };
      case "http": {
        const sum = new Map<number, number>();
        for (const k of ["req_2xx", "req_3xx", "req_4xx", "req_5xx"]) for (const [t, v] of s[k] || []) sum.set(t, (sum.get(t) || 0) + v * secs);
        return { pts: [...sum.entries()].sort((a, b) => a[0] - b[0]).map(([t, v]) => [t, v]), model: buildModel("req", d) };
      }
      case "http5xx": return { pts: s.error_rate || [], model: buildModel("err", d) };
      default: return { pts: s.p50 || [], model: buildModel("lat", d) };
    }
  };
  const first = entries[0] ? pick(entries[0].data).model : buildModel("cpu", { step: 60000, series: {}, deployments: [] });
  return {
    title, kind: "line", minMax: first.minMax, fmtAxis: first.fmtAxis, fmtValue: first.fmtValue,
    series: entries.map((e) => ({ key: e.name, name: e.name, color: e.color, points: pick(e.data).pts })),
  };
}

export type WidgetMetric = "cpu" | "memory" | "egress" | "disk" | "usage" | "http" | "http5xx" | "latency" | "logs" | "httplogs" | "dnslogs" | "netlogs";
