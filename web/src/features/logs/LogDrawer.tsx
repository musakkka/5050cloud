// SPDX-License-Identifier: AGPL-3.0-only

// Row detail: prev/next, expand, View in Context, kebab, close; Attributes and Raw Data tabs.

import { useEffect, useMemo, useState } from "react";
import { ChevronDown, ChevronRight, ChevronUp, Check, Copy, Filter, Maximize2, MoreVertical, Minimize2, X } from "lucide-react";
import { Menu, cx, toast } from "../../kit";
import type { LogLine } from "../../api/types.gen";
import { Ansi } from "./ansi";
import { LEVEL_PILL, fmtStamp, flatten, levelOf, type Level } from "./query";

const LEVEL_NAME: Record<Level, string> = { debug: "Debug", info: "Info", warn: "Warning", error: "Error" };

export function rawOf(l: LogLine, service: string) {
  return {
    message: l.msg,
    severity: l.level,
    attributes: { level: l.level, ...(l.attrs || {}) },
    tags: { service: service, serviceId: l.serviceId, deployment: l.deploymentId, replica: l.replica, stream: l.stream },
    timestamp: new Date(l.ts).toISOString(),
  };
}

function useCopy() {
  const [done, setDone] = useState<string | null>(null);
  return [done, (key: string, text: string) => {
    void navigator.clipboard?.writeText(text);
    setDone(key);
    window.setTimeout(() => setDone((d) => (d === key ? null : d)), 1400);
  }] as const;
}

export function CopyGlyph({ done, onClick, label, className }: { done: boolean; onClick: () => void; label: string; className?: string }) {
  return (
    <button type="button" aria-label={label} onClick={(e) => { e.stopPropagation(); onClick(); }}
      className={cx("flex h-[26px] w-[26px] items-center justify-center rounded-md text-fg-3 transition-colors hover:bg-surface-hover hover:text-foreground", className)}>
      {done ? <Check size={14} className="text-ok-text" /> : <Copy size={14} />}
    </button>
  );
}

export interface DrawerProps {
  line: LogLine;
  serviceName: string;
  position: number;
  total: number;
  expanded: boolean;
  onExpand: () => void;
  onPrev: () => void;
  onNext: () => void;
  onClose: () => void;
  onFilter: (key: string, value: string) => void;
  onContext?: () => void;
  permalink: string;
}

export default function LogDrawer(p: DrawerProps) {
  const { line: l } = p;
  const [tab, setTab] = useState<"attrs" | "raw">("attrs");
  const lvl = levelOf(l);
  const err = lvl === "error";
  const [done, copy] = useCopy();
  const raw = useMemo(() => JSON.stringify(rawOf(l, p.serviceName), null, 2), [l, p.serviceName]);
  const rows: [string, string][] = [["level", l.level], ...flatten(l.attrs).filter(([k]) => k !== "level")];
  const meta: [string, string][] = [["serviceId", l.serviceId ?? ""], ["deploymentId", l.deploymentId ?? ""], ["replicaId", l.replica ?? ""], ["stream", l.stream], ["lineId", l.id], ["timestamp", new Date(l.ts).toISOString()]].filter(([, v]) => v) as [string, string][];
  const [metaOpen, setMetaOpen] = useState(false);

  useEffect(() => setTab("attrs"), [l.id]);

  return (
    <div className="flex h-full min-w-[420px] flex-col overflow-y-auto scroll-thin" data-testid="log-drawer">
      <div className="flex shrink-0 items-center justify-between gap-3 p-6 pb-0">
        <div className="flex items-center gap-3">
          <div className="flex h-[34px] w-[70px] overflow-hidden rounded-md border border-line">
            <button type="button" aria-label="Previous log" disabled={p.position <= 0} onClick={p.onPrev}
              className="flex h-full w-[34px] items-center justify-center bg-surface-hover text-fg-3 transition-colors hover:text-foreground disabled:opacity-40"><ChevronUp size={16} /></button>
            <button type="button" aria-label="Next log" disabled={p.position >= p.total - 1} onClick={p.onNext}
              className="flex h-full w-[34px] items-center justify-center text-fg-3 transition-colors hover:bg-surface-hover hover:text-foreground disabled:opacity-40"><ChevronDown size={16} /></button>
          </div>
          <button type="button" aria-label={p.expanded ? "Collapse drawer" : "Expand drawer"} onClick={p.onExpand}
            className="flex h-[34px] w-[34px] items-center justify-center rounded-md border border-line text-fg-3 transition-[background-color,color,transform] hover:bg-surface-hover hover:text-foreground active:scale-95">
            {p.expanded ? <Minimize2 size={16} /> : <Maximize2 size={16} />}
          </button>
        </div>
        <div className="flex items-center gap-3">
          {p.onContext && (
            <button type="button" onClick={p.onContext}
              className="h-[34px] rounded-md border border-line px-3 text-sm text-foreground transition-[background-color,transform] hover:bg-surface-hover active:scale-95">View in Context</button>
          )}
          <Menu align="right" minWidth={220} items={[
            { label: "Copy log as JSON", icon: <Copy size={16} />, onClick: () => { void navigator.clipboard?.writeText(raw); toast("Log copied as JSON", "success"); } },
            { label: "Copy permalink", icon: <Copy size={16} />, onClick: () => { void navigator.clipboard?.writeText(p.permalink); toast("Permalink copied", "success"); } },
          ]} trigger={
            <button type="button" aria-label="Log actions" className="flex h-[34px] w-[34px] items-center justify-center rounded-md border border-line text-fg-3 transition-[background-color,color,transform] hover:bg-surface-hover hover:text-foreground active:scale-95"><MoreVertical size={16} /></button>
          } />
          <button type="button" aria-label="Close details" onClick={p.onClose}
            className="flex h-[34px] w-[34px] items-center justify-center rounded-md border border-line text-fg-3 transition-[background-color,color,transform] hover:bg-surface-hover hover:text-foreground active:scale-95"><X size={16} /></button>
        </div>
      </div>

      <div className="px-6 pt-8">
        <div className="text-caption font-medium text-[rgb(220,220,224)]">{p.serviceName}</div>
        <div className="mt-1 flex items-center gap-3">
          <h2 className="text-h3 font-medium tabular-nums text-foreground">{fmtStamp(l.ts)}</h2>
          <span className={cx("rounded-full border px-2.5 py-px text-caption font-medium", LEVEL_PILL[lvl])}>{LEVEL_NAME[lvl]}</span>
        </div>
      </div>

      <div role="tablist" className="mt-6 flex gap-6 border-b border-line px-6">
        {([["attrs", "Attributes"], ["raw", "Raw Data"]] as const).map(([id, label]) => (
          <button key={id} role="tab" aria-selected={tab === id} type="button" onClick={() => setTab(id)}
            className={cx("-mb-px h-[41px] border-b-2 text-sm outline-none transition-colors", tab === id ? "border-foreground text-foreground" : "border-transparent text-fg-3 hover:text-foreground")}>{label}</button>
        ))}
      </div>

      {tab === "attrs" ? (
        <div className="flex flex-col gap-6 p-6">
          <div className={cx("relative rounded-md border p-4", err ? "border-danger-line bg-danger-bg text-[rgb(242,241,243)]" : "border-line bg-surface text-foreground")}>
            <pre className="whitespace-pre-wrap break-words pr-6 font-mono text-sm leading-5"><Ansi text={l.msg} /></pre>
            <CopyGlyph label="Copy message" done={done === "msg"} onClick={() => copy("msg", l.msg)} className="absolute right-3 top-3" />
          </div>
          <AttrTable rows={rows} onFilter={p.onFilter} copy={copy} done={done} />
          <div>
            <button type="button" aria-expanded={metaOpen} onClick={() => setMetaOpen((o) => !o)}
              className="flex items-center gap-2 text-caption font-medium text-fg-2 transition-colors hover:text-foreground">
              <ChevronRight size={16} className={cx("transition-transform duration-200", metaOpen && "rotate-90")} />Deployment metadata
            </button>
            {metaOpen && <div className="mt-3 animate-collapsible-down overflow-hidden"><AttrTable rows={meta} onFilter={p.onFilter} copy={copy} done={done} noFilter /></div>}
          </div>
        </div>
      ) : (
        <div className="relative p-6">
          <pre className="overflow-x-auto rounded-md border border-line bg-surface p-4 font-mono text-sm leading-[22px]"><Json text={raw} /></pre>
          <CopyGlyph label="Copy raw data" done={done === "raw"} onClick={() => copy("raw", raw)} className="absolute right-9 top-9" />
        </div>
      )}
    </div>
  );
}

function AttrTable({ rows, onFilter, copy, done, noFilter }: { rows: [string, string][]; onFilter: (k: string, v: string) => void; copy: (k: string, t: string) => void; done: string | null; noFilter?: boolean }) {
  return (
    <div className="overflow-hidden rounded-md border border-surface-hover text-sm">
      <div className="grid h-[37px] grid-cols-[minmax(150px,2fr)_3fr] items-center border-b border-surface-hover px-0 font-medium text-fg-2">
        <div className="px-4">Name</div><div className="border-l border-surface-hover px-4">Value</div>
      </div>
      {rows.map(([k, v]) => (
        <div key={k} className="group/r grid min-h-[36px] grid-cols-[minmax(150px,2fr)_3fr] items-center border-b border-surface-hover last:border-0">
          <div className="truncate px-4 font-mono text-fg-2" title={k}>{k}</div>
          <div className="flex min-w-0 items-center gap-2 border-l border-surface-hover px-4 py-1.5">
            <span className="min-w-0 flex-1 break-all font-mono text-foreground">{v}</span>
            <span className="flex shrink-0 gap-0.5 opacity-0 transition-opacity duration-150 group-hover/r:opacity-60 focus-within:opacity-100">
              {!noFilter && <button type="button" aria-label={`Filter ${k}`} title="Add as a filter" onClick={() => onFilter(k, v)} className="flex h-6 w-6 items-center justify-center rounded hover:bg-surface-hover hover:text-foreground"><Filter size={12} /></button>}
              <button type="button" aria-label={`Copy ${k}`} title="Copy" onClick={() => copy(`a:${k}`, v)} className="flex h-6 w-6 items-center justify-center rounded hover:bg-surface-hover hover:text-foreground">{done === `a:${k}` ? <Check size={12} /> : <Copy size={12} />}</button>
            </span>
          </div>
        </div>
      ))}
    </div>
  );
}

function Json({ text }: { text: string }) {
  const parts = text.split(/("(?:\\.|[^"\\])*"(?:\s*:)?|\b(?:true|false|null)\b|-?\d+(?:\.\d+)?)/g);
  return (
    <>
      {parts.map((s, i) => {
        if (i % 2 === 0) return <span key={i} className="text-fg-3">{s}</span>;
        if (s.endsWith(":")) return <span key={i} className="text-green-600">{s.slice(0, -1)}<span className="text-fg-3">:</span></span>;
        if (s.startsWith('"')) return <span key={i} className="text-green-500">{s}</span>;
        return <span key={i} className="text-blue-500">{s}</span>;
      })}
    </>
  );
}
