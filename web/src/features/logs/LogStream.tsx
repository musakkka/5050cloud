// SPDX-License-Identifier: AGPL-3.0-only

// Live log view (docs/SPEC.md 13.5): history then a live tail over SSE that
// pauses when you scroll up, column toggles, side panel with attributes,
// View in Context, the dropped-lines warning and host notices.

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { AlertTriangle, ArrowDown, X } from "lucide-react";
import type { LogLine, LogsResponse } from "../../api/types.gen";
import { get, qs } from "../service/client";
import { Button, CopyButton, ErrorLine, Modal, Spinner, cx } from "../service/ui";
import { LEVEL_COLOR, addFilter, flatten, histogram, isDropped, mergeLines } from "./query";

export type LogSource =
  | { kind: "logs"; environment: string; services?: string[]; deployment?: string; type?: string }
  | { kind: "build"; deployment: string };

type Cols = { time: boolean; replica: boolean; level: boolean; service: boolean };

function parseSSE(data: string): LogLine[] {
  try {
    const v = JSON.parse(data);
    return Array.isArray(v) ? v : v && typeof v === "object" ? [v] : [];
  } catch { return []; }
}

function baseParams(src: LogSource, q: string) {
  if (src.kind === "build") return {};
  return {
    environment: src.environment,
    services: src.services?.join(","),
    deployment: src.deployment,
    type: src.type,
    q,
  };
}

export default function LogStream({ source, serviceNames, http, showHistogram, defaultService }: {
  source: LogSource;
  serviceNames?: Record<string, string>;
  /** Render the HTTP table (Network > HTTP). */
  http?: boolean;
  showHistogram?: boolean;
  defaultService?: boolean;
}) {
  const [q, setQ] = useState("");
  const [qInput, setQInput] = useState("");
  const [lines, setLines] = useState<LogLine[]>([]);
  const [notices, setNotices] = useState<string[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [live, setLive] = useState(true);
  const [follow, setFollow] = useState(true);
  const [cols, setCols] = useState<Cols>({ time: true, replica: false, level: true, service: !!defaultService });
  const [selected, setSelected] = useState<LogLine | null>(null);
  const [menu, setMenu] = useState<{ x: number; y: number; line: LogLine } | null>(null);
  const [context, setContext] = useState<LogLine | null>(null);
  const scroller = useRef<HTMLDivElement>(null);
  const key = JSON.stringify(source);

  // History.
  useEffect(() => {
    let off = false;
    setLoading(true); setError(null); setLines([]);
    const path = source.kind === "build"
      ? `/deployments/${encodeURIComponent(source.deployment)}/build-logs`
      : `/logs${qs({ ...baseParams(source, q), limit: 1000 })}`;
    get<LogsResponse>(path)
      .then((r) => { if (!off) { setLines((cur) => mergeLines(mergeLines([], r.lines || []), cur)); setNotices(r.notices || []); } })
      .catch((e) => { if (!off) setError(e.message); })
      .finally(() => { if (!off) setLoading(false); });
    return () => { off = true; };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key, q]);

  // Live tail.
  useEffect(() => {
    if (!live) return;
    const url = source.kind === "build"
      ? `/api/v1/deployments/${encodeURIComponent(source.deployment)}/build-logs?follow=1`
      : `/api/v1/logs/stream${qs(baseParams(source, q))}`;
    const es = new EventSource(url, { withCredentials: true });
    const onData = (ev: MessageEvent) => {
      const add = parseSSE(ev.data);
      if (add.length) setLines((cur) => mergeLines(cur, add));
    };
    es.onmessage = onData;
    es.addEventListener("lines", onData as EventListener);
    es.addEventListener("line", onData as EventListener);
    es.addEventListener("notice", ((ev: MessageEvent) => setNotices((n) => [...n, String(ev.data)])) as EventListener);
    return () => es.close();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key, q, live]);

  // Keep the view at the bottom while following.
  useEffect(() => {
    if (follow && scroller.current) scroller.current.scrollTop = scroller.current.scrollHeight;
  }, [lines, follow]);

  const onScroll = useCallback(() => {
    const el = scroller.current;
    if (!el) return;
    const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 24;
    setFollow(atBottom);
  }, []);

  const addF = (k: string, v: string) => { const nq = addFilter(q, k, v); setQ(nq); setQInput(nq); };
  const shown = useMemo(() => (http ? lines.filter((l) => l.stream === "http") : lines), [lines, http]);
  const hist = useMemo(() => (showHistogram ? histogram(shown) : []), [shown, showHistogram]);
  const hmax = Math.max(1, ...hist.map((h) => h[1]));

  return (
    <div className="flex h-full min-h-[360px] flex-col">
      <div className="mb-2 flex flex-wrap items-center gap-2">
        {source.kind !== "build" && (
          <form className="min-w-[200px] flex-1" onSubmit={(e) => { e.preventDefault(); setQ(qInput.trim()); }}>
            <input aria-label="Filter logs" placeholder="Filter, for example @level:error timeout" value={qInput} onChange={(e) => setQInput(e.target.value)}
              className="h-[34px] w-full rounded-md border border-gray-200 bg-background px-3 font-mono text-sm outline-none" />
          </form>
        )}
        <div className="flex items-center gap-3 text-xs text-gray-600">
          {(["time", "replica", "level", "service"] as const).map((c) => (
            <label key={c} className="flex items-center gap-1 capitalize">
              <input type="checkbox" checked={cols[c]} onChange={(e) => setCols({ ...cols, [c]: e.target.checked })} />{c}
            </label>
          ))}
          <label className="flex items-center gap-1"><input type="checkbox" checked={live} onChange={(e) => setLive(e.target.checked)} />Live</label>
        </div>
      </div>

      {notices.map((n) => (
        <div key={n} className="mb-2 flex items-center gap-2 rounded-md border border-yellow-300 bg-yellow-100 px-3 py-1.5 text-xs text-yellow-700">
          <AlertTriangle size={13} />{n}
        </div>
      ))}

      {showHistogram && hist.length > 0 && (
        <div className="mb-2 flex h-12 items-end gap-px" aria-label="Log volume">
          {hist.map(([t, n, e]) => (
            <div key={t} className="flex flex-1 flex-col justify-end" title={`${new Date(t).toLocaleTimeString()}: ${n} lines, ${e} errors`}>
              <div className="bg-red-400" style={{ height: `${(e / hmax) * 100}%` }} />
              <div className="bg-gray-300" style={{ height: `${((n - e) / hmax) * 100}%` }} />
            </div>
          ))}
        </div>
      )}

      <div className="relative flex min-h-0 flex-1">
        <div ref={scroller} onScroll={onScroll} data-testid="log-lines"
          className="min-h-0 flex-1 overflow-auto rounded-md border border-gray-200 bg-background font-mono text-xs">
          {loading ? <div className="flex justify-center py-10"><Spinner /></div> :
            error ? <div className="p-3"><ErrorLine>{error}</ErrorLine></div> :
            shown.length === 0 ? <div className="p-6 text-center text-gray-500">{http ? "No HTTP requests yet." : "No logs yet."}</div> :
            http ? <HttpTable lines={shown} onPick={setSelected} /> : (
              <table className="w-full border-collapse">
                <tbody>
                  {shown.map((l) => {
                    const dropped = isDropped(l);
                    return (
                      <tr key={l.id} data-line-id={l.id} onClick={() => setSelected(l)}
                        onContextMenu={(e) => { e.preventDefault(); setMenu({ x: e.clientX, y: e.clientY, line: l }); }}
                        className={cx("cursor-pointer align-top hover:bg-gray-100", selected?.id === l.id && "bg-gray-100", dropped && "bg-yellow-100 text-yellow-700")}>
                        {cols.time && <td className="whitespace-nowrap px-2 py-0.5 text-gray-500" title={new Date(l.ts).toISOString()}>{new Date(l.ts).toLocaleTimeString()}</td>}
                        {cols.service && <td className="whitespace-nowrap px-2 py-0.5 text-gray-600">{(l.serviceId && serviceNames?.[l.serviceId]) || l.serviceId || ""}</td>}
                        {cols.replica && <td className="whitespace-nowrap px-2 py-0.5 text-gray-500">{l.replica || ""}</td>}
                        {cols.level && <td className={cx("whitespace-nowrap px-2 py-0.5 uppercase", LEVEL_COLOR[l.level])}>{l.level}</td>}
                        <td className={cx("w-full whitespace-pre-wrap break-all px-2 py-0.5", !dropped && LEVEL_COLOR[l.level])}>
                          {dropped && <AlertTriangle size={12} className="mr-1 inline" />}{l.msg}
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            )}
        </div>
        {!follow && (
          <button onClick={() => { setFollow(true); if (scroller.current) scroller.current.scrollTop = scroller.current.scrollHeight; }}
            className="absolute bottom-3 left-1/2 flex -translate-x-1/2 items-center gap-1 rounded-full bg-pink-500 px-3 py-1 text-xs text-white shadow">
            <ArrowDown size={12} />Resume
          </button>
        )}
        {selected && (
          <aside aria-label="Log line details" className="ml-2 w-72 shrink-0 overflow-y-auto rounded-md border border-gray-200 bg-secondaryBg p-3 text-xs">
            <div className="mb-2 flex items-center justify-between">
              <span className="font-semibold">Attributes</span>
              <button aria-label="Close details" onClick={() => setSelected(null)}><X size={14} /></button>
            </div>
            {[["level", selected.level], ["stream", selected.stream], ...(selected.replica ? [["replica", selected.replica]] : []),
              ...(selected.deploymentId ? [["deployment", selected.deploymentId]] : []), ...flatten(selected.attrs)].map(([k, v]) => (
              <div key={k} className="flex gap-2 border-b border-gray-200 py-1 last:border-0">
                <span className="w-24 shrink-0 break-all text-gray-500">{k}</span>
                <button className="break-all text-left font-mono hover:underline" title="Add as a filter"
                  disabled={source.kind === "build"} onClick={() => addF(k, v)}>{v}</button>
              </div>
            ))}
            <div className="mt-2 whitespace-pre-wrap break-all font-mono text-gray-700">{selected.msg}</div>
          </aside>
        )}
      </div>

      {menu && (
        <div className="fixed inset-0 z-50" onClick={() => setMenu(null)} onContextMenu={(e) => { e.preventDefault(); setMenu(null); }}>
          <div role="menu" className="absolute min-w-[160px] rounded-md border border-gray-200 bg-secondaryBg py-1 text-sm shadow-lg" style={{ left: menu.x, top: menu.y }}>
            {source.kind !== "build" && (
              <button role="menuitem" className="block w-full px-3 py-1.5 text-left hover:bg-gray-100" onClick={() => setContext(menu.line)}>View in Context</button>
            )}
            <button role="menuitem" className="block w-full px-3 py-1.5 text-left hover:bg-gray-100" onClick={() => void navigator.clipboard?.writeText(menu.line.msg)}>Copy</button>
          </div>
        </div>
      )}
      {context && source.kind === "logs" && <ContextModal source={source} line={context} onClose={() => setContext(null)} />}
    </div>
  );
}

function HttpTable({ lines, onPick }: { lines: LogLine[]; onPick: (l: LogLine) => void }) {
  const a = (l: LogLine, k: string) => (l.attrs?.[k] === undefined ? "" : String(l.attrs[k]));
  return (
    <table className="w-full border-collapse">
      <thead className="sticky top-0 bg-secondaryBg text-left text-gray-500">
        <tr>{["Time", "Method", "Path", "Status", "Time (ms)", "Request id", "Client IP"].map((h) => <th key={h} className="px-2 py-1 font-normal">{h}</th>)}</tr>
      </thead>
      <tbody>
        {lines.map((l) => {
          const st = Number(a(l, "httpStatus"));
          return (
            <tr key={l.id} onClick={() => onPick(l)} className="cursor-pointer hover:bg-gray-100">
              <td className="whitespace-nowrap px-2 py-0.5 text-gray-500">{new Date(l.ts).toLocaleTimeString()}</td>
              <td className="px-2 py-0.5">{a(l, "method")}</td>
              <td className="break-all px-2 py-0.5">{a(l, "path")}</td>
              <td className={cx("px-2 py-0.5", st >= 500 ? "text-red-600" : st >= 400 ? "text-yellow-700" : "text-green-700")}>{a(l, "httpStatus")}</td>
              <td className="px-2 py-0.5">{a(l, "durationMs")}</td>
              <td className="px-2 py-0.5">{a(l, "requestId")}</td>
              <td className="px-2 py-0.5">{a(l, "srcIp")}</td>
            </tr>
          );
        })}
      </tbody>
    </table>
  );
}

function ContextModal({ source, line, onClose }: { source: Extract<LogSource, { kind: "logs" }>; line: LogLine; onClose: () => void }) {
  const [lines, setLines] = useState<LogLine[] | null>(null);
  const [err, setErr] = useState<string | null>(null);
  useEffect(() => {
    get<LogsResponse>(`/logs${qs({ environment: source.environment, services: line.serviceId, around: line.id, limit: 100 })}`)
      .then((r) => setLines(r.lines)).catch((e) => setErr(e.message));
  }, [source.environment, line]);
  return (
    <Modal title="View in Context" width={900} onClose={onClose} footer={<Button onClick={onClose}>Close</Button>}>
      {err ? <ErrorLine>{err}</ErrorLine> : !lines ? <Spinner /> : (
        <div className="max-h-[60vh] overflow-auto rounded border border-gray-200 bg-background font-mono text-xs">
          {lines.map((l) => (
            <div key={l.id} className={cx("flex gap-2 px-2 py-0.5", l.id === line.id && "bg-pink-100")}>
              <span className="whitespace-nowrap text-gray-500">{new Date(l.ts).toLocaleTimeString()}</span>
              <span className={cx("whitespace-pre-wrap break-all", LEVEL_COLOR[l.level])}>{l.msg}</span>
            </div>
          ))}
        </div>
      )}
      <div className="mt-2 flex items-center gap-2 text-xs text-gray-500">Line id {line.id}<CopyButton text={line.id} /></div>
    </Modal>
  );
}
