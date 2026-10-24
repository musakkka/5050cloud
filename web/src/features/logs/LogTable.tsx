// SPDX-License-Identifier: AGPL-3.0-only

// The log table: sticky header (resizable columns), severity gutter bars, click-to-filter cells,
// wrapped mono rows, flash on new lines, and the HTTP variant.

import { memo, useCallback, useRef, type ReactNode } from "react";
import { cx } from "../../kit";
import type { LogLine } from "../../api/types.gen";
import { Ansi } from "./ansi";
import type { ViewOpts } from "./prefs";
import { LEVEL_BAR, flatten, fmtStamp, fmtStampMs, isDropped, levelOf, tzLabel } from "./query";
import { AlertTriangle } from "lucide-react";

export interface Widths { time: number; service: number }
export const DEFAULT_WIDTHS: Widths = { time: 196, service: 164 };

export interface RowCtx {
  opts: ViewOpts;
  compact: boolean;
  serviceName: (l: LogLine) => string;
  onFilter: (key: string, value: string) => void;
  onSelect: (l: LogLine) => void;
  selectedId?: string;
  flash: Set<string>;
  widths: Widths;
  /** Hide the service column regardless of settings (service-scoped views). */
  noService?: boolean;
  /** Phone width: time and data only. */
  narrow?: boolean;
}

export function columnsOf(c: Pick<RowCtx, "opts" | "compact" | "widths" | "noService" | "narrow">): { template: string; cols: string[] } {
  const { opts, compact, widths, noService, narrow } = c;
  if (narrow) return opts.data ? { template: "150px minmax(0,1fr)", cols: ["time", "data"] } : { template: "150px", cols: ["time"] };
  const cols = ["time"];
  const t = [`${widths.time}px`];
  if (opts.service && !noService) { cols.push("service"); t.push(`${widths.service}px`); }
  if (!compact) {
    if (opts.deploy) { cols.push("deploy"); t.push("120px"); }
    if (opts.replica) { cols.push("replica"); t.push("110px"); }
    for (const d of opts.dataCols) { cols.push(`data:${d}`); t.push("160px"); }
    if (opts.data) { cols.push("data"); t.push("minmax(355px,1fr)"); }
  }
  return { template: t.join(" "), cols };
}

const shortId = (s?: string) => (s ? s.replace(/^dep-/, "").slice(0, 8) : "");

function attrString(l: LogLine): [string, string][] {
  return flatten(l.attrs).filter(([k, v]) => !(k === "level" && v === l.level));
}

const Row = memo(function Row({ l, c }: { l: LogLine; c: RowCtx }) {
  const lvl = levelOf(l);
  const err = lvl === "error";
  const sel = c.selectedId === l.id;
  const { template, cols } = columnsOf(c);
  const dropped = isDropped(l);
  const text = c.opts.json
    ? JSON.stringify({ message: l.msg, level: l.level, ...(l.attrs || {}) })
    : l.msg;
  const selection = () => (window.getSelection()?.toString() ?? "") !== "";
  return (
    <div role="row" data-line-id={l.id} aria-selected={sel} tabIndex={-1}
      onClick={() => { if (!selection()) c.onSelect(l); }}
      style={{ gridTemplateColumns: template }}
      className={cx("log-row grid min-h-[26px] cursor-pointer items-start font-mono text-caption transition-colors duration-100",
        sel ? "bg-pink-50 hover:bg-pink-50" : err ? "bg-danger-bg hover:bg-danger-bg-hover" : "hover:bg-gray-50",
        c.flash.has(l.id) && (err ? "log-flash-error" : "log-flash"), dropped && "text-warn")}>
      {cols.map((col) => {
        if (col === "time") return (
          <div key={col} role="cell" className="flex min-w-0 items-stretch py-[4px] pl-3">
            {c.opts.severity ? (
              <button type="button" aria-label={`Filter level ${lvl}`} title={`Filter @level:${lvl}`} onClick={(e) => { e.stopPropagation(); c.onFilter("level", lvl); }}
                className="group/bar flex min-h-[18px] items-stretch gap-3 text-left outline-none">
                <span className={cx("w-1 shrink-0 self-stretch rounded-sm", LEVEL_BAR[lvl], lvl === "debug" && "opacity-70")} />
                <span className="whitespace-nowrap tabular-nums text-fg-2 group-hover/bar:underline" title={fmtStampMs(l.ts)}>
                  <span className="hidden md:inline">{fmtStamp(l.ts)}</span><span className="md:hidden">{fmtStamp(l.ts, false)}</span>
                </span>
              </button>
            ) : (
              <span className="whitespace-nowrap pl-4 tabular-nums text-fg-2" title={fmtStampMs(l.ts)}>{fmtStamp(l.ts)}</span>
            )}
          </div>
        );
        if (col === "service") return (
          <div key={col} role="cell" className="min-w-0 px-2 py-[4px]">
            <button type="button" onClick={(e) => { e.stopPropagation(); c.onFilter("service", c.serviceName(l)); }} title={`Filter @service:${c.serviceName(l)}`}
              className="max-w-full truncate text-left text-fg-2 outline-none hover:underline focus-visible:underline">{c.serviceName(l)}</button>
          </div>
        );
        if (col === "deploy") return <div key={col} role="cell" className="truncate px-2 py-[4px] text-fg-2">{shortId(l.deploymentId)}</div>;
        if (col === "replica") return <div key={col} role="cell" className="truncate px-2 py-[4px] text-fg-2">{l.replica}</div>;
        if (col.startsWith("data:")) {
          const v = flatten(l.attrs).find(([k]) => k === col.slice(5))?.[1];
          return <div key={col} role="cell" className="truncate px-2 py-[4px] text-foreground">{v}</div>;
        }
        const extra = c.opts.attrs && !c.opts.json ? attrString(l) : [];
        return (
          <div key={col} role="cell" className={cx("min-w-0 px-2 py-[4px] text-foreground", c.opts.wrap ? "whitespace-pre-wrap break-all" : "truncate whitespace-nowrap")}>
            <span className="inline-block select-text px-1">
              {dropped && <AlertTriangle size={12} className="mr-1 inline" />}
              <Ansi text={text} />
              {extra.map(([k, v]) => <span key={k} className="ml-2"><span className="text-fg-3">{k}=</span><span className="text-fg-2">{v}</span></span>)}
            </span>
          </div>
        );
      })}
    </div>
  );
});

export function TableHeader({ c, onResize, gear }: { c: Pick<RowCtx, "opts" | "compact" | "widths" | "noService" | "narrow">; onResize: (col: keyof Widths, w: number) => void; gear?: ReactNode }) {
  const { template, cols } = columnsOf(c);
  const label = (col: string) => col === "time" ? `Time (${tzLabel()})` : col === "service" ? "Service" : col === "deploy" ? "Deploy" : col === "replica" ? "Replica" : col === "data" ? "Data" : col.slice(5);
  return (
    <div role="row" style={{ gridTemplateColumns: template }}
      className="sticky top-0 z-10 grid h-[38px] items-center border-b border-line bg-secondaryBg text-caption font-medium text-fg-2">
      {cols.map((col, i) => (
        <div key={col} role="columnheader" className={cx("group/h relative flex h-full min-w-0 items-center truncate px-2", i === 0 && "pl-6")}>
          <span className="truncate">{label(col)}</span>
          {(col === "time" || col === "service") && <ResizeHandle onResize={(w) => onResize(col, w)} start={c.widths[col]} />}
          {i === cols.length - 1 && gear && <span className="absolute right-3 top-1/2 -translate-y-1/2">{gear}</span>}
        </div>
      ))}
    </div>
  );
}

function ResizeHandle({ start, onResize }: { start: number; onResize: (w: number) => void }) {
  const drag = useRef<{ x: number; w: number } | null>(null);
  const down = useCallback((e: React.PointerEvent) => {
    e.preventDefault();
    e.stopPropagation();
    (e.target as HTMLElement).setPointerCapture(e.pointerId);
    drag.current = { x: e.clientX, w: start };
  }, [start]);
  return (
    <div role="separator" aria-orientation="vertical" aria-label="Resize column" onPointerDown={down}
      onPointerMove={(e) => { if (drag.current) onResize(Math.max(80, Math.min(520, drag.current.w + e.clientX - drag.current.x))); }}
      onPointerUp={() => { drag.current = null; }}
      className="absolute -right-2 top-0 z-20 flex h-full w-4 cursor-col-resize items-center justify-center">
      <span className="h-4 w-1 rounded-full bg-gray-200 opacity-0 transition-opacity duration-150 group-hover/h:opacity-100" />
    </div>
  );
}

export function Rows({ lines, c }: { lines: LogLine[]; c: RowCtx }) {
  return <div role="rowgroup">{lines.map((l) => <Row key={l.id} l={l} c={c} />)}</div>;
}

// ---- HTTP -----------------------------------------------------------------------------------

const HTTP_COLS = "196px 80px minmax(200px,1fr) 120px 140px";
const attr = (l: LogLine, k: string) => (l.attrs?.[k] === undefined ? "" : String(l.attrs[k]));

export function HttpHeader() {
  return (
    <div role="row" style={{ gridTemplateColumns: HTTP_COLS }} className="sticky top-0 z-10 grid h-[38px] items-center border-b border-line bg-secondaryBg text-caption font-medium text-fg-2">
      <div className="pl-6">Time ({tzLabel()})</div><div className="px-2">Method</div><div className="px-2">Path</div><div className="px-2">HTTP Status</div><div className="px-2">Total Duration</div>
    </div>
  );
}

export function HttpRows({ lines, selectedId, onSelect, flash }: { lines: LogLine[]; selectedId?: string; onSelect: (l: LogLine) => void; flash: Set<string> }) {
  return (
    <div role="rowgroup">
      {lines.map((l) => {
        const st = Number(attr(l, "httpStatus"));
        const sel = selectedId === l.id;
        return (
          <div key={l.id} role="row" onClick={() => onSelect(l)} style={{ gridTemplateColumns: HTTP_COLS }}
            className={cx("log-row grid min-h-[26px] cursor-pointer items-start font-mono text-caption transition-colors duration-100", sel ? "bg-pink-50" : "hover:bg-gray-50", flash.has(l.id) && "log-flash")}>
            <div className="py-[4px] pl-6 tabular-nums text-fg-2" title={fmtStampMs(l.ts)}>{fmtStamp(l.ts)}</div>
            <div className="px-2 py-[4px]">{attr(l, "method")}</div>
            <div className="break-all px-2 py-[4px]">{attr(l, "path")}</div>
            <div className={cx("px-2 py-[4px]", st >= 500 ? "text-danger-text" : st >= 400 ? "text-warn" : "text-ok-text")}>{attr(l, "httpStatus")}</div>
            <div className="px-2 py-[4px] tabular-nums text-fg-2">{attr(l, "durationMs") ? `${attr(l, "durationMs")}ms` : ""}</div>
          </div>
        );
      })}
    </div>
  );
}
