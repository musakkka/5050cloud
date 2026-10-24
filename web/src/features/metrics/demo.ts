// SPDX-License-Identifier: AGPL-3.0-only

// Deterministic demo series used only when the API runs on mocks (VITE_MOCKS=1) and returns
// too few points to draw. Never used against a real control plane.

import type { MetricsResponse } from "../../api/types.gen";

function rng(seed: number) {
  let s = seed >>> 0;
  return () => { s = (s * 1664525 + 1013904223) >>> 0; return s / 4294967296; };
}

export const demoMode = () => import.meta.env.VITE_MOCKS === "1";

export function needsDemo(m: MetricsResponse): boolean {
  return demoMode() && !Object.values(m.series).some((s) => s.length > 3);
}

/** A smooth-ish synthetic metrics response over [from, until]. */
export function demoMetrics(from: number, until: number, seed = 7): MetricsResponse {
  const n = 96;
  const step = Math.round((until - from) / n);
  const r = rng(seed);
  const pts = (f: (i: number) => number) => Array.from({ length: n + 1 }, (_, k) => [from + k * step, f(k)]);
  const wave = (base: number, amp: number) => pts((i) => Math.max(0, base + Math.sin(i / 7) * amp + (r() - 0.5) * amp * 0.6));
  const cpuA = wave(0.06, 0.04);
  const cpuB = wave(0.03, 0.02);
  const memA = wave(180e6, 40e6);
  const memB = wave(95e6, 20e6);
  const sum = (a: number[][], b: number[][]) => a.map((p, i) => [p[0], p[1] + b[i][1]]);
  const req = (k: number) => pts(() => (r() < 0.55 ? 0 : Math.round(r() * k)) / (step / 1000));
  return {
    step,
    cpuLimit: 8,
    memLimit: 8e9,
    deployments: [{ ts: from + Math.round((until - from) * 0.62), deploymentId: "demo", label: "Deployment successful" }],
    series: {
      cpu: sum(cpuA, cpuB),
      mem: sum(memA, memB),
      rx: wave(60e3, 40e3).map((p) => [p[0], p[1] / (step / 1000)]),
      tx: wave(120e3, 80e3).map((p) => [p[0], p[1] / (step / 1000)]),
      disk: pts((i) => 220e6 + i * 1.2e6),
      req_2xx: req(9),
      req_3xx: req(2),
      req_4xx: req(3),
      req_5xx: pts((i) => (i % 31 === 0 ? 1 / (step / 1000) : 0)),
      error_rate: pts((i) => (i % 31 === 0 ? 0.02 : 0)),
      p50: wave(42, 12),
      p90: wave(88, 20),
      p95: wave(120, 24),
      p99: wave(190, 40),
    },
    perReplica: {
      a: { cpu: cpuA, mem: memA },
      b: { cpu: cpuB, mem: memB },
    },
  };
}
