// SPDX-License-Identifier: AGPL-3.0-only

// Loading metrics for a service over the selected range, with optional live polling.

import { useCallback, useEffect, useRef, useState } from "react";
import type { MetricsResponse } from "../../api/types.gen";
import { errText, get, qs } from "../service/client";
import { demoMetrics, needsDemo } from "./demo";
import { rangeBounds, type RangeSel } from "./series";

export interface Loaded { data: MetricsResponse; from: number; until: number }

export const LIVE_MS = 15000;

export async function fetchMetrics(base: string, sel: RangeSel, seed = 7): Promise<Loaded> {
  const b = rangeBounds(sel);
  const from = Math.round(b.from), until = Math.round(b.until);
  let data = await get<MetricsResponse>(`${base}/metrics${qs({ from, until, view: "replica" })}`);
  if (needsDemo(data)) data = demoMetrics(from, until, seed);
  return { data, from, until };
}

/** Metrics for one service. Keeps the previous data on screen while a refresh runs. */
export function useServiceMetrics(base: string, sel: RangeSel, live: boolean) {
  const [state, setState] = useState<{ loaded?: Loaded; error?: string }>({});
  const key = base + JSON.stringify(sel);
  const run = useRef<() => Promise<void>>();
  run.current = async () => {
    try { setState({ loaded: await fetchMetrics(base, sel) }); } catch (e) { setState((s) => ({ ...s, error: errText(e) })); }
  };
  useEffect(() => {
    setState({});
    void run.current!();
  }, [key]);
  useEffect(() => {
    if (!live || (sel.from && sel.until)) return;
    const t = setInterval(() => void run.current!(), LIVE_MS);
    return () => clearInterval(t);
  }, [live, key]);
  const reload = useCallback(() => { setState((s) => ({ loaded: s.loaded })); void run.current!(); }, []);
  return { ...state, reload };
}

/** Metrics for every listed service (Observability). Keyed by service id. */
export function useEnvMetrics(eid: string, serviceIds: string[], sel: RangeSel, live: boolean) {
  const [state, setState] = useState<{ byId?: Record<string, Loaded>; error?: string }>({});
  const key = eid + serviceIds.join(",") + JSON.stringify(sel);
  const run = useRef<() => Promise<void>>();
  run.current = async () => {
    try {
      const rows = await Promise.all(serviceIds.map(async (id, i) => [id, await fetchMetrics(`/environments/${encodeURIComponent(eid)}/services/${encodeURIComponent(id)}`, sel, 7 + i * 11)] as const));
      setState({ byId: Object.fromEntries(rows) });
    } catch (e) { setState((s) => ({ ...s, error: errText(e) })); }
  };
  useEffect(() => { setState({}); void run.current!(); }, [key]);
  useEffect(() => {
    if (!live || (sel.from && sel.until)) return;
    const t = setInterval(() => void run.current!(), LIVE_MS);
    return () => clearInterval(t);
  }, [live, key]);
  const reload = useCallback(() => void run.current!(), []);
  return { ...state, reload };
}
