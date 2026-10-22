// SPDX-License-Identifier: AGPL-3.0-only

import { useCallback, useEffect, useRef, useState } from "react";
import { eventsUrl } from "./client";
import type { Event } from "./types.gen";

/** Loads data once per deps change; `reload` fetches again, `set` applies an optimistic value. */
export function useLoad<R>(fn: () => Promise<R>, deps: unknown[]) {
  const [data, setData] = useState<R | undefined>(undefined);
  const [error, setError] = useState<Error | undefined>(undefined);
  const [loading, setLoading] = useState(true);
  const seq = useRef(0);
  const run = useCallback(() => {
    const my = ++seq.current;
    setLoading(true);
    return fn().then(
      (d) => { if (my === seq.current) { setData(d); setError(undefined); setLoading(false); } },
      (e) => { if (my === seq.current) { setError(e as Error); setLoading(false); } },
    );
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps);
  useEffect(() => { void run(); }, [run]);
  return { data, error, loading, reload: run, set: setData };
}

/**
 * Subscribes to the environment's SSE stream (/api/v1/events). The browser
 * reconnects on its own; a 30 s fallback reload runs while the stream is down.
 */
export function useEvents(eid: string | undefined, onEvent: (e: Event) => void, onFallback?: () => void) {
  const cb = useRef(onEvent);
  cb.current = onEvent;
  const fb = useRef(onFallback);
  fb.current = onFallback;
  useEffect(() => {
    if (!eid || typeof EventSource === "undefined") return;
    let open = false;
    const es = new EventSource(eventsUrl(eid), { withCredentials: true });
    es.onopen = () => { open = true; };
    es.onerror = () => { open = false; };
    es.onmessage = (m) => {
      try { cb.current(JSON.parse(m.data) as Event); } catch { /* a keep-alive or a malformed frame */ }
    };
    const t = setInterval(() => { if (!open) fb.current?.(); }, 30000);
    return () => { clearInterval(t); es.close(); };
  }, [eid]);
}
