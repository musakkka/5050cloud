// SPDX-License-Identifier: AGPL-3.0-only

// Build Logs and Deploy Logs in the deployment drawer: filter box with tag
// suggestions, download, open in the Log Explorer, a time/message table with
// a severity bar, grouped build steps, view options, a line detail pane, the
// live tail with up/down controls and the end-of-range pill.

import { forwardRef, useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { Link } from "react-router-dom";
import {
  ArrowDown, ArrowRight, ArrowUp, Check, ChevronDown, ChevronRight, ChevronUp, Download, EllipsisVertical, ExternalLink, Minimize2, Search, Settings, X,
} from "lucide-react";
import type { DeploymentDetail, LogLine, LogsResponse } from "../../api/types.gen";
import { Checkbox, CopyButton, IconButton, Kbd, MenuItem, Popover, Skeleton, Tooltip, cx, readPref, toast, writePref } from "../../kit";
import { LINKS } from "../../app/links";
import { get, qs } from "../service/client";
import { inProgress } from "./actions";
import { fmtSecond, matches, parseQuery, shortDuration, toMs, tzLabel } from "./format";

export type LogKind = "build" | "deploy";

function parseSSE(data: string): LogLine[] {
  try {
    const v = JSON.parse(data);
    return Array.isArray(v) ? v : v && typeof v === "object" ? [v] : [];
  } catch { return []; }
}

/** Merge lines by id, keep them sorted by time, cap the window. */
export function mergeLines(cur: LogLine[], add: LogLine[], max = 5000): LogLine[] {
  if (!add.length) return cur;
  const seen = new Set(cur.map((l) => l.id));
  const fresh = add.filter((l) => !seen.has(l.id));
  if (!fresh.length) return cur;
  const all = cur.concat(fresh);
  all.sort((a, b) => a.ts - b.ts);
  return all.length > max ? all.slice(all.length - max) : all;
}

/** Loads history, then follows the live stream while `live` is on. */
export function useLogLines(path: string, streamUrl: string | null, limit = 1000) {
  const [lines, setLines] = useState<LogLine[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [truncated, setTruncated] = useState(false);
  const [notices, setNotices] = useState<string[]>([]);
  useEffect(() => {
    let off = false;
    setLoading(true); setError(null);
    get<LogsResponse>(path)
      .then((r) => { if (!off) { setLines(mergeLines([], r.lines || [])); setTruncated(!!r.truncated); setNotices(r.notices || []); } })
      .catch((e) => { if (!off) setError(e instanceof Error ? e.message : String(e)); })
      .finally(() => { if (!off) setLoading(false); });
    return () => { off = true; };
  }, [path, limit]);
  useEffect(() => {
    if (!streamUrl) return;
    const es = new EventSource(streamUrl, { withCredentials: true });
    const on = (ev: MessageEvent) => { const add = parseSSE(ev.data); if (add.length) setLines((cur) => mergeLines(cur, add)); };
    es.onmessage = on;
    es.addEventListener("lines", on as EventListener);
    es.addEventListener("line", on as EventListener);
    es.addEventListener("notice", ((ev: MessageEvent) => setNotices((n) => [...n, String(ev.data)])) as EventListener);
    return () => es.close();
  }, [streamUrl]);
  return { lines, loading, error, truncated, notices };
}

/** Focus a filter box with "/" (not while typing elsewhere). */
export function useSlashFocus(ref: React.RefObject<HTMLInputElement>) {
  useEffect(() => {
    const k = (e: KeyboardEvent) => {
      if (e.key !== "/" || e.metaKey || e.ctrlKey || e.altKey) return;
      const t = e.target as HTMLElement | null;
      if (t && (t.tagName === "INPUT" || t.tagName === "TEXTAREA" || t.isContentEditable)) return;
      if (document.querySelector("[aria-modal='true']")) return;
      e.preventDefault();
      ref.current?.focus();
    };
    document.addEventListener("keydown", k);
    return () => document.removeEventListener("keydown", k);
  }, [ref]);
}

const TAGS: { tag: string; help: string }[] = [
  { tag: "@level", help: "Filter by severity" },
  { tag: "@replica", help: "Filter by replica" },
  { tag: "@deployment", help: "Filter by deployment" },
];
const LEVELS = ["debug", "info", "warn", "error"];
const LEVEL_BAR: Record<string, string> = { error: "bg-danger", warn: "bg-yellow-500", info: "bg-blue-500", debug: "bg-fg-5" };

/** The 34px filter input with "/" hint, clear button and the tag dropdown. */
export const FilterBox = forwardRef<HTMLInputElement, {
  value: string; onChange: (v: string) => void; onSubmit: (v: string) => void; placeholder: string;
  suggest?: { replicas?: string[]; deployment?: string }; tags?: boolean; className?: string;
}>(function FilterBox({ value, onChange, onSubmit, placeholder, suggest, tags = true, className }, ref) {
  const [focus, setFocus] = useState(false);
  const inner = useRef<HTMLInputElement | null>(null);
  const token = value.split(/\s+/).pop() || "";
  const m = /^-?@(\w+):(.*)$/.exec(token);
  let heading = "Tags";
  let rows: { label: string; help?: string; bar?: string; insert: string }[] = [];
  if (m) {
    const key = m[1], part = m[2].toLowerCase();
    const vals = key === "level" ? LEVELS : key === "replica" ? suggest?.replicas || [] : key === "deployment" && suggest?.deployment ? [suggest.deployment] : [];
    heading = key === "level" ? "Log Severity" : key === "replica" ? "Replicas" : "Values";
    rows = vals.filter((v) => v.toLowerCase().startsWith(part)).map((v) => ({ label: v, bar: key === "level" ? LEVEL_BAR[v] : undefined, insert: `${token.startsWith("-") ? "-" : ""}@${key}:${v}` }));
  } else {
    const part = token.replace(/^-/, "").toLowerCase();
    rows = TAGS.filter((t) => !part || t.tag.startsWith(part.startsWith("@") ? part : `@${part}`))
      .map((t) => ({ label: t.tag, help: t.help, insert: `${token.startsWith("-") ? "-" : ""}${t.tag}:` }));
  }
  const pick = (ins: string) => {
    const parts = value.split(/\s+/);
    parts[parts.length - 1] = ins;
    const next = parts.join(" ");
    onChange(ins.endsWith(":") ? next : `${next} `);
    if (!ins.endsWith(":")) onSubmit(next);
    inner.current?.focus();
  };
  const open = tags && focus && (rows.length > 0 || !value);
  return (
    <div className={cx("relative min-w-0 flex-1", className)}>
      <div className={cx("flex h-[34px] items-center gap-2 rounded-md border bg-background pl-3 pr-1.5 transition-colors duration-150",
        focus ? "border-accent" : "border-line hover:border-fg-5", open && "rounded-b-none")}>
        <Search size={16} className="shrink-0 text-fg-3" />
        <input ref={(n) => { inner.current = n; if (typeof ref === "function") ref(n); else if (ref) ref.current = n; }}
          value={value} placeholder={placeholder} spellCheck={false} aria-label={placeholder}
          onChange={(e) => onChange(e.target.value)} onFocus={() => setFocus(true)} onBlur={() => setTimeout(() => setFocus(false), 120)}
          onKeyDown={(e) => {
            if (e.key === "Enter") { e.preventDefault(); onSubmit(value.trim()); setFocus(false); }
            if (e.key === "Escape") { e.preventDefault(); e.stopPropagation(); (e.target as HTMLInputElement).blur(); }
          }}
          className={cx("h-full min-w-0 flex-1 bg-transparent text-sm text-foreground outline-none placeholder:text-fg-2 focus-visible:shadow-none", value && "font-mono")} />
        {value ? (
          <button type="button" aria-label="Clear filter" onClick={() => { onChange(""); onSubmit(""); inner.current?.focus(); }}
            className="inline-flex h-6 w-6 items-center justify-center rounded text-foreground transition-colors hover:bg-surface-hover"><X size={16} /></button>
        ) : <Kbd className="mr-0.5 bg-transparent">/</Kbd>}
      </div>
      {open && (
        <div data-popover role="listbox" aria-label={heading}
          className="absolute inset-x-0 top-full z-30 animate-pop-in overflow-hidden rounded-b-md border border-t-0 border-line-popover bg-surface shadow-popover">
          <div className="px-4 pb-2 pt-3 text-caption font-medium text-fg-2">{heading}</div>
          {rows.map((r) => (
            <button key={r.label} type="button" role="option" aria-selected={false} onMouseDown={(e) => { e.preventDefault(); pick(r.insert); }}
              className="flex h-9 w-full items-center gap-3 px-4 text-left transition-colors duration-150 hover:bg-surface-hover">
              {r.bar && <span className={cx("h-4 w-1 rounded-full", r.bar)} />}
              <span className="flex-1 font-mono text-sm font-light text-foreground">{r.label}</span>
              {r.help && <span className="text-caption text-fg-2">{r.help}</span>}
            </button>
          ))}
          <div className="flex flex-wrap items-center gap-x-6 gap-y-2 border-t border-line px-4 py-2.5 text-caption text-fg-2">
            <span className="flex items-center gap-1.5">Tags:{TAGS.map((t) => <Chip key={t.tag} onClick={() => pick(`${t.tag}:`)}>{t.tag}</Chip>)}</span>
            <span className="flex items-center gap-1.5">Operators:{["-", "AND", "OR"].map((o) => <Chip key={o}>{o}</Chip>)}</span>
            <a href={LINKS.docs} target="_blank" rel="noreferrer" className="ml-auto underline underline-offset-2 transition-colors hover:text-foreground">Documentation</a>
          </div>
        </div>
      )}
    </div>
  );
});

function Chip({ children, onClick }: { children: ReactNode; onClick?: () => void }) {
  return (
    <span role={onClick ? "button" : undefined} onMouseDown={onClick ? (e) => { e.preventDefault(); onClick(); } : undefined}
      className={cx("inline-flex h-[22px] items-center rounded-md border border-line bg-background px-1.5 font-mono text-caption font-medium text-accent-text", onClick && "cursor-pointer hover:border-fg-5")}>{children}</span>
  );
}

/** A 34px toolbar icon button with a tooltip. */
export function ToolButton({ label, onClick, children, to }: { label: string; onClick?: () => void; children: ReactNode; to?: string }) {
  const cls = "inline-flex h-[34px] w-[34px] shrink-0 items-center justify-center rounded-md border border-line bg-background text-fg-2 transition-colors duration-150 hover:bg-surface-hover hover:text-foreground active:scale-[.96]";
  return (
    <Tooltip content={label} side="bottom">
      {to ? <Link to={to} aria-label={label} className={cls}>{children}</Link> : <button type="button" aria-label={label} onClick={onClick} className={cls}>{children}</button>}
    </Tooltip>
  );
}

export interface ViewOpts { severity: boolean; data: boolean; attrs: boolean; json: boolean; wrap: boolean; replica: boolean }
const DEFAULT_OPTS: ViewOpts = { severity: true, data: true, attrs: true, json: false, wrap: true, replica: false };

export function useViewOpts(): [ViewOpts, (o: ViewOpts) => void] {
  const [o, set] = useState<ViewOpts>(() => ({ ...DEFAULT_OPTS, ...readPref<Partial<ViewOpts>>("5050.logs.view", {}) }));
  return [o, (n) => { set(n); writePref("5050.logs.view", n); }];
}

export function ViewOptions({ opts, onChange }: { opts: ViewOpts; onChange: (o: ViewOpts) => void }) {
  const row = (k: keyof ViewOpts, label: string) => (
    <Checkbox key={k} checked={opts[k]} onChange={(v) => onChange({ ...opts, [k]: v })} label={<span className="text-caption font-light text-fg-menu">{label}</span>} className="h-6 items-center !gap-3" />
  );
  return (
    <Popover align="right" width={286} role="dialog" wrapperClassName="relative shrink-0"
      trigger={(open) => <IconButton label="View options" size={26} active={open}><Settings size={16} /></IconButton>}>
      {() => (
        <div className="px-3 py-3">
          <div className="mb-2 text-caption font-medium text-fg-2">View options</div>
          {row("severity", "Show severity level")}
          {row("data", "Show data column")}
          {row("attrs", "Show attributes in data column")}
          {row("json", "Display data column as JSON")}
          {row("wrap", "Wrap long lines")}
          <div className="mb-2 mt-4 text-caption font-medium text-fg-2">Tag columns</div>
          {row("replica", "Replica")}
        </div>
      )}
    </Popover>
  );
}

function download(name: string, lines: LogLine[]) {
  const text = lines.map((l) => `${new Date(toMs(l.ts)).toISOString()} ${l.level.toUpperCase().padEnd(5)} ${l.replica ? `[${l.replica}] ` : ""}${l.msg}`).join("\n");
  const url = URL.createObjectURL(new Blob([text + "\n"], { type: "text/plain" }));
  const a = document.createElement("a");
  a.href = url; a.download = name;
  document.body.appendChild(a); a.click(); a.remove();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}

/** Empty state with our own small illustration. */
export function LogsEmpty({ title, body }: { title: string; body: string }) {
  return (
    <div className="flex h-full min-h-[280px] flex-col items-center justify-center px-6 text-center font-sans">
      <svg width="88" height="64" viewBox="0 0 88 64" fill="none" aria-hidden className="mb-5">
        <rect x="14" y="6" width="60" height="52" rx="8" className="fill-surface-hover stroke-line" />
        <rect x="24" y="18" width="4" height="8" rx="2" className="fill-blue-500" />
        <rect x="32" y="20" width="30" height="4" rx="2" className="fill-line" />
        <rect x="24" y="30" width="4" height="8" rx="2" className="fill-fg-5" />
        <rect x="32" y="32" width="22" height="4" rx="2" className="fill-line" />
        <rect x="24" y="42" width="4" height="8" rx="2" className="fill-fg-5" />
        <rect x="32" y="44" width="26" height="4" rx="2" className="fill-line" />
      </svg>
      <div className="text-lead font-semibold text-foreground">{title}</div>
      <div className="mt-1 text-caption text-fg-2">{body}</div>
    </div>
  );
}

function RowsSkeleton() {
  return (
    <div className="space-y-2 px-6 py-3" aria-busy="true" aria-label="Loading logs">
      {Array.from({ length: 14 }, (_, i) => (
        <div key={i} className="flex h-[18px] items-center gap-6">
          <Skeleton w={150} h={12} /><Skeleton w={`${30 + ((i * 37) % 55)}%`} h={12} />
        </div>
      ))}
    </div>
  );
}

const attrText = (l: LogLine) => Object.entries(l.attrs || {}).filter(([k]) => !["step", "cmd", "durationMs", "cached", "level"].includes(k))
  .map(([k, v]) => `${k}=${typeof v === "object" ? JSON.stringify(v) : String(v)}`).join(" ");

/** Rows for one view: plain lines, or build steps with their output folded under them. */
type Row = { line: LogLine; step?: boolean; child?: boolean; hasKids?: boolean };

function buildRows(lines: LogLine[], collapsed: Set<string>): Row[] {
  const kids = new Map<string, LogLine[]>();
  const heads = new Set<string>();
  for (const l of lines) {
    const s = l.attrs?.step;
    if (s === undefined) continue;
    const k = String(s);
    if (l.attrs?.cmd !== undefined) heads.add(k);
    else { if (!kids.has(k)) kids.set(k, []); kids.get(k)!.push(l); }
  }
  const out: Row[] = [];
  const done = new Set<string>();
  for (const l of lines) {
    const k = l.attrs?.step === undefined ? undefined : String(l.attrs.step);
    if (k !== undefined && l.attrs?.cmd !== undefined) {
      const ks = done.has(k) ? [] : kids.get(k) || [];
      done.add(k);
      out.push({ line: l, step: true, hasKids: ks.length > 0 });
      if (!collapsed.has(k)) for (const c of ks) out.push({ line: c, child: true });
    } else if (k !== undefined && heads.has(k)) continue;
    else out.push({ line: l });
  }
  return out;
}

export default function LogView({ kind, d, pid }: { kind: LogKind; d: DeploymentDetail; pid?: string }) {
  const did = d.id;
  const live = kind === "build" ? d.status === "BUILDING" || d.status === "QUEUED" : d.active || inProgress(d);
  const path = kind === "build" ? `/deployments/${encodeURIComponent(did)}/build-logs`
    : `/logs${qs({ environment: d.environmentId, deployment: did, type: "deploy", limit: 1000 })}`;
  const stream = !live ? null : kind === "build" ? `/api/v1/deployments/${encodeURIComponent(did)}/build-logs?follow=1`
    : `/api/v1/logs/stream${qs({ environment: d.environmentId, deployment: did, type: "deploy" })}`;
  const { lines, loading, error, notices } = useLogLines(path, stream);
  const [input, setInput] = useState("");
  const [q, setQ] = useState("");
  const [opts, setOpts] = useViewOpts();
  const [sel, setSel] = useState<string | null>(null);
  const [follow, setFollow] = useState(true);
  const [flash, setFlash] = useState<string | null>(null);
  const [collapsed, setCollapsed] = useState<Set<string>>(new Set());
  const box = useRef<HTMLInputElement>(null);
  const scroller = useRef<HTMLDivElement>(null);
  useSlashFocus(box);

  // Typing filters as you go (debounced); Enter applies at once.
  useEffect(() => { const t = setTimeout(() => setQ(input.trim()), 250); return () => clearTimeout(t); }, [input]);
  const terms = useMemo(() => parseQuery(q), [q]);
  const shown = useMemo(() => (terms.length ? lines.filter((l) => matches(l, terms)) : lines), [lines, terms]);
  const rows = useMemo(() => (kind === "build" && !terms.length ? buildRows(shown, collapsed) : shown.map((line) => ({ line }) as Row)), [shown, kind, terms.length, collapsed]);
  const replicas = useMemo(() => [...new Set(lines.map((l) => l.replica).filter(Boolean))] as string[], [lines]);

  useLayoutEffect(() => {
    const el = scroller.current;
    if (el && follow) el.scrollTop = el.scrollHeight;
  }, [rows, follow, loading]);
  const onScroll = useCallback(() => {
    const el = scroller.current;
    if (el) setFollow(el.scrollHeight - el.scrollTop - el.clientHeight < 24);
  }, []);

  const selIdx = sel ? shown.findIndex((l) => l.id === sel) : -1;
  const selected = selIdx >= 0 ? shown[selIdx] : null;
  const viewInContext = (l: LogLine) => {
    setSel(null); setInput(""); setQ(""); setFollow(false); setFlash(l.id);
    requestAnimationFrame(() => requestAnimationFrame(() => scroller.current?.querySelector(`[data-line-id="${CSS.escape(l.id)}"]`)?.scrollIntoView({ block: "center" })));
    setTimeout(() => setFlash(null), 1400);
  };

  const tz = tzLabel();
  const empty = !loading && !error && rows.length === 0;
  const lastTs = lines.length ? lines[lines.length - 1].ts : d.finishedAt || d.createdAt;

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="flex items-center gap-3 border-b border-line px-4 pb-2 pt-2 sm:px-6">
        <FilterBox ref={box} value={input} onChange={setInput} onSubmit={(v) => { setInput(v); setQ(v); }}
          placeholder={kind === "build" ? "Search build logs" : "Filter and search logs"} tags={kind === "deploy"}
          suggest={{ replicas, deployment: did }} />
        <ToolButton label="Download logs" onClick={() => { download(`deployment-${did.slice(0, 8)}-${kind}-logs.log`, shown); toast("Download started", "success"); }}><Download size={16} /></ToolButton>
        {kind === "deploy" && pid && <ToolButton label="Open in Log Explorer" to={`/project/${pid}/logs?deployment=${encodeURIComponent(did)}&environmentId=${encodeURIComponent(d.environmentId)}`}><ExternalLink size={16} /></ToolButton>}
      </div>
      {notices.map((n) => <div key={n} className="border-b border-line bg-warn-bg px-6 py-2 text-caption text-warn">{n}</div>)}
      {d.status === "FAILED" && kind === "build" && d.diagnosis && (
        <div className="flex items-center gap-3 border-b border-line bg-pink-50 px-6 py-3 text-sm text-accent-text">
          <span className="min-w-0 flex-1 truncate">Diagnosis: we looked into the failure. The Details tab has what we found.</span>
        </div>
      )}

      <div className="flex h-[38px] shrink-0 items-center border-b border-line pl-6 pr-4 text-caption font-medium text-fg-2">
        <span className="w-[180px] shrink-0">Time ({tz})</span>
        {opts.replica && <span className="w-[110px] shrink-0">Replica</span>}
        <span className="flex-1">{opts.data ? (kind === "build" ? "Message" : "Data") : ""}</span>
        <ViewOptions opts={opts} onChange={setOpts} />
      </div>

      <div className="relative min-h-0 flex-1">
        <div ref={scroller} onScroll={onScroll} data-testid="log-lines"
          className={cx("scroll-thin absolute inset-0 overflow-y-auto font-mono text-caption", !opts.wrap && "overflow-x-auto")}>
          {loading && !lines.length ? <RowsSkeleton /> : error ? (
            <div role="alert" className="m-6 rounded-md border border-danger-line bg-danger-bg px-4 py-3 font-sans text-sm text-danger-text">Could not load logs. {error}</div>
          ) : empty ? (
            q ? <LogsEmpty title="No matching lines" body="Nothing in this range matches the filter." />
              : kind === "build" ? <LogsEmpty title="No build logs" body="No build logs were found for this deployment." />
              : <LogsEmpty title="No deploy logs" body={live ? "Waiting for the first lines from the replicas." : "No deploy logs were found for this deployment."} />
          ) : (
            <div className="pb-16 pt-1">
              {rows.map((r) => (
                <LogRow key={r.line.id} r={r} opts={opts} selected={sel === r.line.id} flash={flash === r.line.id}
                  collapsed={collapsed.has(String(r.line.attrs?.step))}
                  onToggle={() => setCollapsed((c) => { const n = new Set(c); const k = String(r.line.attrs?.step); if (n.has(k)) n.delete(k); else n.add(k); return n; })}
                  onClick={() => setSel(r.line.id)} />
              ))}
              {!live && !q && (
                <div className="sticky bottom-3 ml-8 mt-3 inline-flex h-[26px] items-center gap-2 rounded-full border border-line bg-background px-3 font-sans text-caption text-fg-2">
                  You reached the end of the range <ArrowRight size={12} /> <span className="font-mono">{fmtSecond(lastTs).slice(0, 16)}</span>
                </div>
              )}
            </div>
          )}
        </div>

        {!empty && !error && (
          <div className="absolute bottom-4 right-6 z-[5] flex flex-col gap-1">
            <IconButton label="Scroll to top" size={34} variant="secondary" onClick={() => { setFollow(false); scroller.current?.scrollTo({ top: 0, behavior: "smooth" }); }}><ArrowUp size={16} /></IconButton>
            <IconButton label={live ? "Follow the live tail" : "Scroll to bottom"} size={34} variant="secondary" disabled={follow}
              onClick={() => { setFollow(true); const el = scroller.current; if (el) el.scrollTop = el.scrollHeight; }}><ArrowDown size={16} /></IconButton>
          </div>
        )}

        {selected && (
          <LineDetail line={selected} kind={kind} onClose={() => setSel(null)}
            onPrev={selIdx > 0 ? () => setSel(shown[selIdx - 1].id) : undefined}
            onNext={selIdx < shown.length - 1 ? () => setSel(shown[selIdx + 1].id) : undefined}
            onContext={() => viewInContext(selected)}
            onFilter={(k, v) => { const t = `@${k}:${/\s/.test(v) ? `"${v}"` : v}`; const nq = q.includes(t) ? q : `${q} ${t}`.trim(); setInput(nq); setQ(nq); setSel(null); }} />
        )}
      </div>
    </div>
  );
}

function LogRow({ r, opts, selected, flash, collapsed, onToggle, onClick }: {
  r: Row; opts: ViewOpts; selected: boolean; flash: boolean; collapsed: boolean; onToggle: () => void; onClick: () => void;
}) {
  const l = r.line;
  const err = l.level === "error";
  const ms = typeof l.attrs?.durationMs === "number" ? (l.attrs.durationMs as number) : undefined;
  const attrs = opts.attrs && !r.step ? attrText(l) : "";
  const msg = opts.json ? JSON.stringify({ msg: l.msg, level: l.level, ...(l.attrs || {}) }) : l.msg;
  return (
    <div data-line-id={l.id} onClick={onClick}
      className={cx("relative flex min-h-[26px] cursor-pointer items-start py-1 pl-[26px] pr-6 transition-colors duration-100",
        err ? "bg-red-50 hover:bg-danger-bg-hover" : "hover:bg-tint-hover", selected && "!bg-tint-active", flash && "animate-flash")}>
      {opts.severity && <span aria-hidden className={cx("absolute bottom-1 left-3 top-1 w-1 rounded-full", LEVEL_BAR[l.level] || "bg-blue-500")} />}
      <span className="w-[180px] shrink-0 whitespace-nowrap leading-[18px] text-fg-2" title={new Date(toMs(l.ts)).toISOString()}>{fmtSecond(l.ts)}</span>
      {opts.replica && <span className="w-[110px] shrink-0 truncate leading-[18px] text-fg-3">{l.replica || ""}</span>}
      {opts.data && (r.step ? (
        <span className="flex min-w-0 flex-1 items-start leading-[18px]">
          <span className="flex w-[108px] shrink-0 items-center gap-1.5 text-fg-2"><Check size={12} className="text-fg-3" />{String(l.attrs?.step)}</span>
          {r.hasKids && (
            <button type="button" aria-label={collapsed ? "Show output" : "Hide output"} onClick={(e) => { e.stopPropagation(); onToggle(); }}
              className="-ml-5 mr-1 mt-px inline-flex h-4 w-4 shrink-0 items-center justify-center rounded-full bg-surface-hover text-fg-2 hover:text-foreground">
              {collapsed ? <ChevronRight size={11} /> : <ChevronDown size={11} />}
            </button>
          )}
          <span className={cx("min-w-0 flex-1 text-fg-menu", opts.wrap ? "whitespace-pre-wrap break-all" : "whitespace-pre")}>
            {String(l.attrs?.cmd ?? l.msg)}{l.attrs?.cached ? <span className="ml-2 text-fg-4">cached</span> : null}
          </span>
        </span>
      ) : (
        <span className={cx("min-w-0 flex-1 leading-[18px]", r.child && "pl-[108px] font-semibold", err ? "text-fg-menu" : "text-fg-menu", opts.wrap ? "whitespace-pre-wrap break-all" : "whitespace-pre")}>
          {msg}{attrs && <span className="ml-3 text-fg-3">{attrs}</span>}
        </span>
      ))}
      {ms !== undefined && <span className="ml-4 shrink-0 leading-[18px] text-fg-2">{shortDuration(ms)}</span>}
    </div>
  );
}

function LineDetail({ line, kind, onClose, onPrev, onNext, onContext, onFilter }: {
  line: LogLine; kind: LogKind; onClose: () => void; onPrev?: () => void; onNext?: () => void; onContext: () => void; onFilter: (k: string, v: string) => void;
}) {
  const [tab, setTab] = useState<"attrs" | "raw">("attrs");
  const [meta, setMeta] = useState(false);
  const ref = useRef<HTMLDivElement>(null);
  useLayoutEffect(() => {
    if (window.matchMedia?.("(prefers-reduced-motion: reduce)").matches) return;
    ref.current?.animate?.([{ transform: "translateX(24px)", opacity: 0 }, { transform: "none", opacity: 1 }], { duration: 200, easing: "cubic-bezier(0,0,.2,1)" });
  }, []);
  useEffect(() => {
    const k = (e: KeyboardEvent) => {
      if ((e.target as HTMLElement)?.tagName === "INPUT") return;
      if (e.key === "Escape" && !document.querySelector("[data-popover], [aria-modal='true']")) { e.preventDefault(); onClose(); return; }
      if (e.key === "ArrowUp" && onPrev) { e.preventDefault(); onPrev(); }
      if (e.key === "ArrowDown" && onNext) { e.preventDefault(); onNext(); }
    };
    document.addEventListener("keydown", k);
    return () => document.removeEventListener("keydown", k);
  }, [onPrev, onNext, onClose]);
  const err = line.level === "error";
  const attrs: [string, string][] = [["level", line.level], ...Object.entries(line.attrs || {}).filter(([k]) => k !== "level").map(([k, v]) => [k, typeof v === "object" ? JSON.stringify(v) : String(v)] as [string, string])];
  const platform: [string, string][] = [["stream", line.stream], ...(line.replica ? [["replica", line.replica] as [string, string]] : []),
    ...(line.deploymentId ? [["deployment", line.deploymentId] as [string, string]] : []), ...(line.serviceId ? [["service", line.serviceId] as [string, string]] : []), ["line id", line.id]];
  return (
    <div ref={ref} role="dialog" aria-label="Log line" data-escape-layer
      className="absolute inset-y-0 left-[25px] right-0 z-10 flex flex-col overflow-hidden border-l border-line bg-background">
      <div className="flex items-center gap-3 px-6 pt-6">
        <div className="inline-flex">
          <button type="button" aria-label="Previous line" disabled={!onPrev} onClick={onPrev} className="inline-flex h-[34px] w-[34px] items-center justify-center rounded-l-md border border-line text-fg-2 transition-colors hover:bg-surface-hover hover:text-foreground disabled:opacity-40"><ChevronUp size={16} /></button>
          <button type="button" aria-label="Next line" disabled={!onNext} onClick={onNext} className="-ml-px inline-flex h-[34px] w-[34px] items-center justify-center rounded-r-md border border-line text-fg-2 transition-colors hover:bg-surface-hover hover:text-foreground disabled:opacity-40"><ChevronDown size={16} /></button>
        </div>
        <IconButton label="Collapse" size={34} variant="outline" onClick={onClose}><Minimize2 size={16} /></IconButton>
        <span className="flex-1" />
        {kind === "deploy" && <button type="button" onClick={onContext} className="inline-flex h-[34px] items-center rounded-md border border-line px-3 text-sm text-foreground transition-colors hover:bg-surface-hover active:scale-[.97]">View in Context</button>}
        <Popover align="right" minWidth={200} wrapperClassName="relative shrink-0"
          trigger={(open) => <IconButton label="More" size={34} variant="outline" active={open}><EllipsisVertical size={16} /></IconButton>}>
          {(close) => (
            <>
              <MenuItem onClick={() => { close(); void navigator.clipboard?.writeText(line.msg); toast("Copied", "success"); }}>Copy message</MenuItem>
              <MenuItem onClick={() => { close(); void navigator.clipboard?.writeText(JSON.stringify(line, null, 2)); toast("Copied", "success"); }}>Copy as JSON</MenuItem>
              <MenuItem onClick={() => { close(); void navigator.clipboard?.writeText(line.id); toast("Copied", "success"); }}>Copy line id</MenuItem>
            </>
          )}
        </Popover>
        <IconButton label="Close" size={34} variant="outline" onClick={onClose}><X size={16} /></IconButton>
      </div>
      <div className="flex items-center gap-3 px-6 pt-6">
        <span className="font-mono text-h2 font-normal text-foreground">{fmtSecond(line.ts)}</span>
        <span className={cx("inline-flex h-6 items-center rounded px-2 text-caption font-medium capitalize",
          err ? "bg-danger-bg text-danger-text" : line.level === "warn" ? "bg-warn-bg text-warn" : "bg-blue-50 text-blue-600")}>{line.level}</span>
      </div>
      <div role="tablist" className="mt-5 flex gap-8 border-b border-line px-6">
        {([["attrs", "Attributes"], ["raw", "Raw Data"]] as const).map(([id, label]) => (
          <button key={id} type="button" role="tab" aria-selected={tab === id} onClick={() => setTab(id)}
            className={cx("relative pb-3 text-sm transition-colors", tab === id ? "text-foreground" : "text-fg-3 hover:text-fg-2")}>
            {label}{tab === id && <span className="absolute inset-x-0 -bottom-px h-0.5 bg-foreground" />}
          </button>
        ))}
      </div>
      <div className="scroll-thin min-h-0 flex-1 space-y-6 overflow-y-auto px-6 py-6">
        {tab === "attrs" ? (
          <>
            <div className={cx("relative rounded-md border px-4 py-3 pr-12", err ? "border-danger-line bg-red-50" : "border-line bg-surface")}>
              <pre className="whitespace-pre-wrap break-all font-mono text-sm leading-6 text-fg-menu">{line.msg}</pre>
              <CopyButton text={line.msg} label="Copy message" className="absolute right-3 top-3" />
            </div>
            <KvTable rows={attrs} onFilter={kind === "deploy" ? onFilter : undefined} />
            <div>
              <button type="button" onClick={() => setMeta((m) => !m)} aria-expanded={meta} className="inline-flex items-center gap-2 text-caption font-medium text-fg-2 hover:text-foreground">
                <ChevronRight size={14} className={cx("transition-transform", meta && "rotate-90")} />Platform metadata
              </button>
              {meta && <div className="mt-3"><KvTable rows={platform} onFilter={kind === "deploy" ? onFilter : undefined} /></div>}
            </div>
          </>
        ) : (
          <div className="relative rounded-md bg-surface-hover">
            <pre className="scroll-thin overflow-x-auto p-4 pr-12 font-mono text-sm leading-6 text-fg-menu">{JSON.stringify(line, null, 2)}</pre>
            <CopyButton text={JSON.stringify(line, null, 2)} label="Copy raw data" className="absolute right-3 top-3" />
          </div>
        )}
      </div>
    </div>
  );
}

function KvTable({ rows, onFilter }: { rows: [string, string][]; onFilter?: (k: string, v: string) => void }) {
  return (
    <div className="overflow-hidden rounded-md border border-line">
      <div className="grid grid-cols-[minmax(0,2fr)_minmax(0,3fr)] border-b border-line bg-tint-header text-sm text-fg-2">
        <span className="border-r border-line px-4 py-2">Name</span><span className="px-4 py-2">Value</span>
      </div>
      {rows.map(([k, v]) => (
        <div key={k} className="grid grid-cols-[minmax(0,2fr)_minmax(0,3fr)] border-b border-line font-mono text-sm last:border-0">
          <span className="truncate border-r border-line px-4 py-2 text-fg-2" title={k}>{k}</span>
          {onFilter ? (
            <button type="button" title="Filter by this value" onClick={() => onFilter(k === "line id" ? "id" : k, v)}
              className="break-all px-4 py-2 text-left text-foreground transition-colors hover:bg-tint-hover">{v}</button>
          ) : <span className="break-all px-4 py-2 text-foreground">{v}</span>}
        </div>
      ))}
    </div>
  );
}
