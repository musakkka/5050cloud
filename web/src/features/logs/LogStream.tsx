// SPDX-License-Identifier: AGPL-3.0-only

// The log viewer (docs/SPEC.md 13.5): filter bar, time range, live tail over SSE, histogram,
// table, row drawer, View in Context, layout settings, dropped-lines notice and host notices.
// Used by the Log Explorer (environment-wide) and reusable for deployment-scoped views.

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { AlertTriangle, ArrowDown, ArrowLeft, ArrowRight, ArrowUp, Download, ExternalLink, Pause, Play } from "lucide-react";
import type { LogLine, LogsResponse } from "../../api/types.gen";
import { IconButton, Tooltip, cx } from "../../kit";
import { get, qs } from "../service/client";
import FilterBar, { ALL_TAGS, type TagDef, type ValueOption } from "./FilterBar";
import Histogram from "./Histogram";
import LogDrawer from "./LogDrawer";
import { DEFAULT_WIDTHS, HttpHeader, HttpRows, Rows, TableHeader, type RowCtx, type Widths } from "./LogTable";
import SettingsPopover from "./SettingsPopover";
import TimeRange from "./TimeRange";
import { LogState, LogStyles } from "./states";
import { useRange, useViewOpts } from "./prefs";
import { addFilter, compileFilter, flatten, fmtStamp, fmtStampMs, levelOf, mergeLines, rangeBounds } from "./query";

export type LogSource =
  | { kind: "logs"; environment: string; services?: string[]; deployment?: string; type?: string }
  | { kind: "build"; deployment: string };

function parseSSE(data: string): LogLine[] {
  try {
    const v = JSON.parse(data);
    return Array.isArray(v) ? v : v && typeof v === "object" ? [v] : [];
  } catch { return []; }
}

function baseParams(src: LogSource, q: string) {
  if (src.kind === "build") return {};
  return { environment: src.environment, services: src.services?.join(","), deployment: src.deployment, type: src.type, q };
}

const mocksOn = import.meta.env.VITE_MOCKS === "1";
const mocksReady = (): Promise<unknown> => (mocksOn ? import("./mocks").then((m) => m.installLogMocks()).catch(() => undefined) : Promise.resolve());

function useNarrow() {
  const [n, setN] = useState(() => window.matchMedia("(max-width: 639px)").matches);
  useEffect(() => {
    const m = window.matchMedia("(max-width: 639px)");
    const h = () => setN(m.matches);
    m.addEventListener("change", h);
    return () => m.removeEventListener("change", h);
  }, []);
  return n;
}

export default function LogStream({ source, serviceNames, http, showHistogram = true, defaultService, explorerHref, scope = "explorer" }: {
  source: LogSource;
  serviceNames?: Record<string, string>;
  /** Render the HTTP table (Network > HTTP). */
  http?: boolean;
  showHistogram?: boolean;
  defaultService?: boolean;
  /** Deployment views: link that opens the same filter in the Log Explorer. */
  explorerHref?: string;
  scope?: string;
}) {
  const build = source.kind === "build";
  const deployScoped = build || !!source.deployment;
  const [q, setQ] = useState("");
  const [range, setRange] = useRange(scope);
  const [opts, setOpts] = useViewOpts(scope);
  const [lines, setLines] = useState<LogLine[]>([]);
  const [notices, setNotices] = useState<string[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [truncated, setTruncated] = useState(false);
  const [loadingOlder, setLoadingOlder] = useState(false);
  const [live, setLive] = useState(true);
  const [follow, setFollow] = useState(true);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [expanded, setExpanded] = useState(false);
  const [dw, setDw] = useState<number | null>(null);
  const [context, setContext] = useState<LogLine | null>(null);
  const [flash, setFlash] = useState<Set<string>>(new Set());
  const [widths, setWidths] = useState<Widths>(DEFAULT_WIDTHS);
  const [tick, setTick] = useState(() => Date.now());
  const [scrollPos, setScrollPos] = useState({ top: true, bottom: true });
  const scroller = useRef<HTMLDivElement>(null);
  const body = useRef<HTMLDivElement>(null);
  const lastShown = useRef<LogLine | null>(null);
  const key = JSON.stringify(source);
  const narrow = useNarrow();

  const nameOf = useCallback((l: LogLine) => (l.serviceId && serviceNames?.[l.serviceId]) || l.serviceId || "", [serviceNames]);

  // Search text for the build view is the same language; it only ever matches the message.
  const compiled = useMemo(() => compileFilter(q, { serviceName: nameOf }), [q, nameOf]);
  const bounds = useMemo(() => {
    const open = range.id !== "custom" || !range.until?.trim();
    const b = rangeBounds(range, tick) ?? rangeBounds({ id: "7d" }, tick)!;
    return { ...b, open, valid: !!rangeBounds(range, tick) };
  }, [range, tick]);
  const boundsKey = `${range.id}|${range.from}|${range.until}`;

  // Keep the sliding window fresh.
  useEffect(() => {
    const t = window.setInterval(() => setTick(Date.now()), 15_000);
    return () => window.clearInterval(t);
  }, []);
  useEffect(() => setTick(Date.now()), [boundsKey]);

  // History.
  useEffect(() => {
    let off = false;
    setLoading(true); setError(null); setLines([]); setSelectedId(null); setContext(null);
    void mocksReady().then(() => {
      if (off) return;
      const b = rangeBounds(range, Date.now());
      const path = build
        ? `/deployments/${encodeURIComponent((source as { deployment: string }).deployment)}/build-logs`
        : `/logs${qs({ ...baseParams(source, compiled.parsed.error ? "" : q), since: b?.from, until: range.id === "custom" && range.until?.trim() ? b?.to : undefined, limit: 1000 })}`;
      return get<LogsResponse>(path)
        .then((r) => {
          if (off) return;
          setLines(mergeLines([], r.lines || []));
          setTruncated(!!r.truncated);
          setNotices(r.notices || []);
          setFollow(true);
        })
        .catch((e) => { if (!off) setError(e.message); })
        .finally(() => { if (!off) setLoading(false); });
    });
    return () => { off = true; };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key, q, boundsKey]);

  // Live tail.
  useEffect(() => {
    if (!live || !bounds.open) return;
    let es: EventSource | null = null;
    let off = false;
    const onData = (ev: MessageEvent) => {
      const add = parseSSE(ev.data);
      if (!add.length) return;
      setLines((cur) => mergeLines(cur, add));
      const ids = add.map((l) => l.id);
      setFlash((f) => new Set([...f, ...ids]));
      window.setTimeout(() => setFlash((f) => { const n = new Set(f); ids.forEach((i) => n.delete(i)); return n; }), 1300);
    };
    void mocksReady().then(() => {
      if (off) return;
      const url = build
        ? `/api/v1/deployments/${encodeURIComponent((source as { deployment: string }).deployment)}/build-logs?follow=1`
        : `/api/v1/logs/stream${qs(baseParams(source, compiled.parsed.error ? "" : q))}`;
      es = new EventSource(url, { withCredentials: true });
      es.onmessage = onData;
      es.addEventListener("lines", onData as EventListener);
      es.addEventListener("line", onData as EventListener);
      es.addEventListener("notice", ((ev: MessageEvent) => setNotices((n) => [...new Set([...n, String(ev.data)])])) as EventListener);
    });
    return () => { off = true; es?.close(); };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key, q, live, bounds.open]);

  // The visible set.
  const shown = useMemo(() => {
    if (compiled.parsed.error) return [];
    return lines.filter((l) => (!http || l.stream === "http") && l.ts >= bounds.from && (bounds.open || l.ts <= bounds.to) && compiled.match(l));
  }, [lines, http, bounds, compiled]);

  const selIndex = selectedId ? shown.findIndex((l) => l.id === selectedId) : -1;
  const selected = selIndex >= 0 ? shown[selIndex] : null;
  if (selected) lastShown.current = selected;

  // Follow the bottom while live.
  useEffect(() => {
    const el = scroller.current;
    if (follow && el) el.scrollTop = el.scrollHeight;
  }, [shown, follow, selectedId, opts.wrap]);

  const measure = useCallback(() => {
    const el = scroller.current;
    if (!el) return;
    setScrollPos({ top: el.scrollTop < 8, bottom: el.scrollHeight - el.scrollTop - el.clientHeight < 24 });
  }, []);

  const loadOlder = useCallback(() => {
    if (loadingOlder || !truncated || build || !lines.length) return;
    setLoadingOlder(true);
    const el = scroller.current;
    const prevH = el?.scrollHeight ?? 0;
    get<LogsResponse>(`/logs${qs({ ...baseParams(source, compiled.parsed.error ? "" : q), since: bounds.from, until: lines[0].ts - 1, limit: 1000 })}`)
      .then((r) => {
        const before = lines.length;
        const merged = mergeLines(lines, r.lines || [], 20000);
        setLines(merged);
        setTruncated(!!r.truncated && merged.length > before);
        requestAnimationFrame(() => { if (el) el.scrollTop += el.scrollHeight - prevH; });
      })
      .catch(() => setTruncated(false))
      .finally(() => setLoadingOlder(false));
  }, [loadingOlder, truncated, build, lines, source, compiled.parsed.error, q, bounds.from]);

  const onScroll = useCallback(() => {
    const el = scroller.current;
    if (!el) return;
    setFollow(el.scrollHeight - el.scrollTop - el.clientHeight < 24);
    measure();
    if (el.scrollTop < 60) loadOlder();
  }, [measure, loadOlder]);
  useEffect(measure, [shown, measure]);

  const jumpTop = () => { setFollow(false); scroller.current?.scrollTo({ top: 0, behavior: "smooth" }); };
  const jumpBottom = () => { setFollow(true); setLive(true); const el = scroller.current; el?.scrollTo({ top: el.scrollHeight, behavior: "smooth" }); };

  const addF = useCallback((k: string, v: string) => setQ((cur) => addFilter(cur, k, v)), []);
  const select = useCallback((l: LogLine) => { setFollow(false); setSelectedId((cur) => (cur === l.id ? null : l.id)); }, []);

  // Permalink: ?log=<id> selects a line once it is loaded.
  const linkId = useRef(new URLSearchParams(window.location.search).get("log"));
  useEffect(() => {
    if (linkId.current && lines.some((l) => l.id === linkId.current)) { setSelectedId(linkId.current); setFollow(false); linkId.current = null; }
  }, [lines]);

  // Keyboard: arrows move through the list, Esc closes the context view then the drawer.
  useEffect(() => {
    const k = (e: KeyboardEvent) => {
      const t = e.target as HTMLElement | null;
      const typing = !!t && (t.tagName === "INPUT" || t.tagName === "TEXTAREA" || t.isContentEditable);
      if (e.key === "Escape" && !typing) {
        if (context) setContext(null);
        else if (selectedId) setSelectedId(null);
        return;
      }
      if (typing || context || selIndex < 0) return;
      if (e.key === "ArrowDown" && selIndex < shown.length - 1) { e.preventDefault(); setSelectedId(shown[selIndex + 1].id); }
      if (e.key === "ArrowUp" && selIndex > 0) { e.preventDefault(); setSelectedId(shown[selIndex - 1].id); }
    };
    window.addEventListener("keydown", k);
    return () => window.removeEventListener("keydown", k);
  }, [context, selectedId, selIndex, shown]);

  // Keep the selected row in view when moving with the keyboard.
  useEffect(() => {
    if (!selectedId) return;
    scroller.current?.querySelector(`[data-line-id="${CSS.escape(selectedId)}"]`)?.scrollIntoView({ block: "nearest" });
  }, [selectedId]);

  // Values offered after `@tag:`.
  const valuesFor = useCallback((tag: string): ValueOption[] => {
    const set = new Map<string, ValueOption>();
    if (tag === "service") {
      for (const n of Object.values(serviceNames || {})) set.set(n, { value: n, icon: n });
      for (const l of lines) { const n = nameOf(l); if (n && !set.has(n)) set.set(n, { value: n, icon: n }); }
      return [...set.values()];
    }
    for (const l of lines) {
      const vals = tag === "replica" ? [l.replica] : tag === "deployment" ? [l.deploymentId] : flatten(l.attrs).filter(([k]) => k === tag).map(([, v]) => v);
      for (const v of vals) if (v && !set.has(v)) set.set(v, { value: v });
      if (set.size > 60) break;
    }
    return [...set.values()];
  }, [lines, serviceNames, nameOf]);

  const tags: TagDef[] = deployScoped ? ALL_TAGS.filter((t) => t.tag !== "service") : ALL_TAGS;

  const download = () => {
    const text = shown.map((l) => `${fmtStampMs(l.ts)} ${nameOf(l)} [${levelOf(l)}] ${l.msg}`.replace(/\s+\[/, " [")).join("\n");
    const url = URL.createObjectURL(new Blob([text + "\n"], { type: "text/plain" }));
    const a = document.createElement("a");
    a.href = url; a.download = `logs-${fmtStamp(Date.now()).replace(/[: ]/g, "-")}.log`; a.click();
    URL.revokeObjectURL(url);
  };

  const permalink = (l: LogLine) => { const u = new URL(window.location.href); u.searchParams.set("log", l.id); return u.toString(); };

  // Drawer resize.
  const dragging = useRef(false);
  const onDrag = (e: React.PointerEvent) => {
    if (!dragging.current || !body.current) return;
    const r = body.current.getBoundingClientRect();
    setDw(Math.max(380, Math.min(r.width - 260, r.right - e.clientX)));
  };

  const ctx: RowCtx = {
    opts: { ...opts, service: opts.service && (defaultService ?? true) }, compact: !!selected, serviceName: nameOf, onFilter: addF, onSelect: select, selectedId: selectedId ?? undefined,
    flash, widths, noService: deployScoped, narrow,
  };
  const wantHist = showHistogram && opts.histogram && !build && !http;
  const drawerOpen = !!selected;
  const drawerLine = selected ?? lastShown.current;
  const open = drawerOpen && !!drawerLine;

  const emptyTitle = compiled.parsed.error || q ? "No logs found for this filter" : "No logs in this time range";

  return (
    <div className="relative flex h-full min-h-[360px] flex-col overflow-hidden rounded-xl border border-line-panel bg-background" data-testid="log-stream">
      <LogStyles />
      <div className="shrink-0 border-b border-line bg-background">
        <div className="flex flex-wrap items-center gap-2 px-4 pt-4 sm:flex-nowrap sm:px-6">
          <FilterBar value={q} onApply={setQ} valuesFor={valuesFor} tags={tags} className="min-w-full flex-1 sm:min-w-0"
            placeholder={build ? "Search build logs" : http ? "Search HTTP logs e.g. @path:/api/users @httpStatus:200" : "Filter and search logs"} />
          <IconButton label="Download logs" size={34} variant="outline" onClick={download} className="text-fg-3"><Download size={16} /></IconButton>
          {explorerHref && <IconButton label="Open in Log Explorer" size={34} variant="outline" className="text-fg-3" onClick={() => { window.location.href = explorerHref; }}><ExternalLink size={16} /></IconButton>}
          {!build && (
            <>
              <TimeRange range={range} onChange={setRange} />
              <Tooltip content={live ? "Pause live updates" : "Resume live updates"} side="bottom">
                <button type="button" aria-label={live ? "Pause live updates" : "Resume live updates"} aria-pressed={live} onClick={() => { setLive((v) => !v); if (!live) setFollow(true); }}
                  className={cx("flex h-[34px] w-[34px] items-center justify-center rounded-md border transition-[background-color,border-color,transform] duration-150 active:scale-95",
                    live ? "border-[#1c362a] text-ok hover:bg-[#12261d]" : "border-line text-foreground hover:bg-surface-hover")}>
                  {live ? <Pause size={16} fill="currentColor" /> : <Play size={16} fill="currentColor" />}
                </button>
              </Tooltip>
            </>
          )}
        </div>
        {wantHist ? <Histogram lines={shown} from={truncated && shown.length ? Math.max(bounds.from, shown[0].ts) : bounds.from} to={bounds.open ? tick : bounds.to}
          onZoom={(a, b) => setRange({ id: "custom", from: new Date(a).toISOString(), until: new Date(b).toISOString() })} /> : <div className="h-4" />}
      </div>

      {notices.map((n) => (
        <div key={n} className="flex items-center gap-2 border-b border-warn-line bg-warn-bg px-6 py-1.5 text-caption text-warn"><AlertTriangle size={13} />{n}</div>
      ))}

      <div ref={body} className="relative flex min-h-0 flex-1">
        <div className="relative min-w-0 flex-1">
          <div ref={scroller} onScroll={onScroll} data-testid="log-lines" role="table" aria-label="Logs"
            className={cx("scroll-thin h-full overflow-auto transition-opacity duration-300", loading && lines.length > 0 && "opacity-60")}>
            {http ? <HttpHeader /> : <TableHeader c={ctx} onResize={(c, w) => setWidths((cur) => ({ ...cur, [c]: w }))} gear={selected ? undefined : <SettingsPopover opts={opts} onChange={setOpts} hideTags={deployScoped ? ["service"] : undefined} />} />}
            {loading && lines.length === 0 ? <LogState dim title="Your logs are loading..." body="Give us a moment to fetch your logs." /> :
              error ? <LogState title="Could not load logs" body={error} /> :
              !bounds.valid ? <LogState title="Check the time range" body="Use relative times like 1h ago, dates, or now, with From before Until." /> :
              shown.length === 0 ? (
                build ? <LogState title="No build logs" body="No build logs were found for this deployment." /> :
                http ? <LogState title="No HTTP requests yet" body="Requests will show up here as they are served." /> :
                <LogState title={emptyTitle} body={compiled.parsed.error ? compiled.parsed.error : "Logs will show up here as they are found."} />
              ) : (
                <>
                  {!truncated && (
                    <div className="flex h-9 items-center pl-8">
                      <span className="inline-flex h-6 items-center gap-2 rounded border border-line bg-background px-3 text-caption text-fg-2">
                        You reached the end of the range <ArrowRight size={12} /> <span className="tabular-nums">{fmtStamp(shown[0].ts, false)}</span>
                      </span>
                    </div>
                  )}
                  {loadingOlder && <div className="py-2 text-center text-caption text-fg-3">Loading older logs...</div>}
                  {http ? <HttpRows lines={shown} selectedId={selectedId ?? undefined} onSelect={select} flash={flash} /> : <Rows lines={shown} c={ctx} />}
                  <div className="h-3" />
                </>
              )}
          </div>
          {shown.length > 0 && (
            <div className="pointer-events-none absolute bottom-6 right-8 flex flex-col gap-1 max-sm:right-4">
              <button type="button" aria-label="Jump to top" title="Jump to top" disabled={scrollPos.top} onClick={jumpTop}
                className="pointer-events-auto flex h-[34px] w-[34px] items-center justify-center rounded-md bg-gray-100 text-fg-3 transition-[background-color,opacity,transform] hover:bg-gray-200 active:scale-95 disabled:opacity-40"><ArrowUp size={16} /></button>
              <button type="button" aria-label="Jump to bottom" title="Jump to bottom" disabled={scrollPos.bottom} onClick={jumpBottom}
                className="pointer-events-auto flex h-[34px] w-[34px] items-center justify-center rounded-md bg-gray-100 text-fg-3 transition-[background-color,opacity,transform] hover:bg-gray-200 active:scale-95 disabled:opacity-40"><ArrowDown size={16} /></button>
            </div>
          )}
        </div>

        <aside aria-label="Log line details" aria-hidden={!open}
          className={cx("relative shrink-0 overflow-hidden bg-background transition-[width] duration-300 ease-[cubic-bezier(.4,0,.2,1)] max-sm:absolute max-sm:inset-0 max-sm:z-20", open ? "border-l-2 border-line" : "pointer-events-none")}
          style={{ width: open ? (expanded ? "100%" : dw ?? "66.667%") : 0 }}>
          {open && drawerLine && (
            <>
              <div onPointerDown={(e) => { dragging.current = true; (e.target as HTMLElement).setPointerCapture(e.pointerId); }} onPointerMove={onDrag} onPointerUp={() => { dragging.current = false; }}
                className="group/rz absolute left-0 top-0 z-10 flex h-full w-4 cursor-col-resize items-center justify-center" role="separator" aria-label="Resize drawer">
                <span className="h-8 w-1 rounded-full bg-gray-200 opacity-0 transition-opacity duration-150 group-hover/rz:opacity-100" />
              </div>
              <LogDrawer line={drawerLine} serviceName={nameOf(drawerLine)} position={selIndex} total={shown.length} expanded={expanded}
                onExpand={() => setExpanded((e) => !e)} onClose={() => { setSelectedId(null); setExpanded(false); }}
                onPrev={() => selIndex > 0 && setSelectedId(shown[selIndex - 1].id)} onNext={() => selIndex < shown.length - 1 && setSelectedId(shown[selIndex + 1].id)}
                onFilter={addF} permalink={permalink(drawerLine)} onContext={build ? undefined : () => setContext(drawerLine)} />
            </>
          )}
        </aside>

        {context && source.kind === "logs" && <ContextView source={source} line={context} nameOf={nameOf} ctx={ctx} onClose={() => setContext(null)} />}
      </div>
    </div>
  );
}

function ContextView({ source, line, nameOf, ctx, onClose }: { source: Extract<LogSource, { kind: "logs" }>; line: LogLine; nameOf: (l: LogLine) => string; ctx: RowCtx; onClose: () => void }) {
  const [lines, setLines] = useState<LogLine[] | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const box = useRef<HTMLDivElement>(null);
  useEffect(() => {
    let off = false;
    void mocksReady().then(() => get<LogsResponse>(`/logs${qs({ environment: source.environment, services: line.serviceId, deployment: source.deployment, around: line.id, limit: 100 })}`))
      .then((r) => { if (!off) setLines(r.lines); }).catch((e) => { if (!off) setErr(e.message); });
    return () => { off = true; };
  }, [source.environment, source.deployment, line]);
  useEffect(() => {
    box.current?.querySelector(`[data-line-id="${CSS.escape(line.id)}"]`)?.scrollIntoView({ block: "center" });
  }, [lines, line.id]);
  const c: RowCtx = { ...ctx, compact: false, selectedId: line.id, onSelect: () => undefined, onFilter: () => undefined, opts: { ...ctx.opts, service: false, deploy: false, replica: false, dataCols: [], data: true }, flash: new Set(), noService: true };
  return (
    <div className="absolute inset-0 z-30 flex flex-col bg-background animate-fade-in" role="dialog" aria-label="Log context">
      <div className="flex shrink-0 items-center gap-3 border-b border-line px-6 py-3">
        <button type="button" onClick={onClose} className="flex h-[34px] items-center gap-2 rounded-md border border-line px-3 text-sm text-foreground transition-[background-color,transform] hover:bg-surface-hover active:scale-95"><ArrowLeft size={16} />Back to logs</button>
        <div className="min-w-0">
          <div className="truncate text-sm font-medium text-foreground">Context around {fmtStamp(line.ts)}</div>
          <div className="text-caption text-fg-3">{nameOf(line)}{line.replica ? ` / ${line.replica}` : ""}</div>
        </div>
      </div>
      <div ref={box} className="scroll-thin min-h-0 flex-1 overflow-auto">
        {err ? <LogState title="Could not load context" body={err} /> : !lines ? <LogState dim title="Your logs are loading..." body="Give us a moment to fetch your logs." /> : <Rows lines={lines} c={c} />}
      </div>
    </div>
  );
}
