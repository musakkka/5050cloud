// SPDX-License-Identifier: AGPL-3.0-only

// Subscribe to the environment events stream (GET /api/v1/events) and call
// `on` for matching events. A 30 s fallback poll covers a dropped stream.

import { useEffect, useRef } from "react";
import type { Event as FiftyEvent } from "../../api/types.gen";

const TYPES = ["deployment", "service", "changes", "activity", "host"];

export function useEnvEvents(eid: string | undefined, on: (e: FiftyEvent) => void, fallback?: () => void) {
  const onRef = useRef(on);
  onRef.current = on;
  const fbRef = useRef(fallback);
  fbRef.current = fallback;
  useEffect(() => {
    if (!eid) return;
    const es = new EventSource(`/api/v1/events?environment=${encodeURIComponent(eid)}`, { withCredentials: true });
    const handle = (m: MessageEvent) => {
      try { onRef.current(JSON.parse(m.data) as FiftyEvent); } catch { /* ignore malformed frames */ }
    };
    es.onmessage = handle;
    for (const t of TYPES) es.addEventListener(t, handle as EventListener);
    const t = setInterval(() => fbRef.current?.(), 30000);
    return () => { es.close(); clearInterval(t); };
  }, [eid]);
}
