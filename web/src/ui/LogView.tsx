// SPDX-License-Identifier: AGPL-3.0-only

import { useEffect, useMemo, useRef, useState } from "react";
import { ArrowDown, ArrowUp, Clock, Download, Pause, Play, Search } from "lucide-react";
import { api, type LogLine } from "../api";
import { MenuItem, Popover, Spinner, cx, fmtDateTime, tzLabel } from "./kit";

const BAR: Record<string, string> = { error: "bg-[rgb(182,45,43)]", warn: "bg-yellow-500", info: "bg-blue-300" };
const RANGES: [number, string][] = [[900, "Last 15 minutes"], [3600, "Last hour"], [6 * 3600, "Last 6 hours"], [86400, "Last day"], [7 * 86400, "Last week"]];

export function LogTable({ lines, showService, onService, empty, follow }: { lines: LogLine[]; showService?: boolean; onService?: (l: LogLine) => void; empty?: string; follow: boolean }) {
  const ref = useRef<HTMLDivElement>(null);
  const [atBottom, setAtBottom] = useState(true);
  useEffect(() => {
    const el = ref.current;
    if (el && follow && atBottom) el.scrollTop = el.scrollHeight;
  }, [lines, follow, atBottom]);
  const cols = showService ? "grid-cols-[188px_164px_minmax(0,1fr)]" : "grid-cols-[188px_minmax(0,1fr)]";
  return (
    <div className="flex-1 min-h-0 flex flex-col relative">
      <div className={cx("grid w-full text-left text-xs font-medium text-gray-600 select-none bg-secondaryBg border-b border-gray-200", cols)}>
        <div className="px-2 py-2.5 pl-6">Time ({tzLabel()})</div>
        {showService && <div className="px-2 py-2.5">Service</div>}
        <div className="px-2 py-2.5">Data</div>
      </div>
      <div ref={ref} className="flex-1 overflow-y-auto scroll-thin" onScroll={(e) => { const el = e.currentTarget; setAtBottom(el.scrollHeight - el.scrollTop - el.clientHeight < 40); }}>
        {lines.length === 0 && <div className="px-6 py-10 text-sm text-gray-500">{empty || "No logs in this window."}</div>}
        {lines.map((l, i) => (
          <div key={i} className={cx("grid font-mono text-xs leading-[18px] hover:bg-gray-50", cols, l.level === "error" && "bg-red-50 hover:bg-red-100")}>
            <div className="flex items-stretch pl-3 py-1">
              <span className={cx("w-[4px] rounded-full shrink-0", BAR[l.level] || BAR.info)} />
              <time className="pl-2.5 text-gray-600 tabular-nums whitespace-nowrap">{fmtDateTime(l.ts)}</time>
            </div>
            {showService && (
              <div className="px-2 py-1 text-gray-600 truncate">
                <button className="hover:underline text-left truncate max-w-full" onClick={() => onService?.(l)}>{l.service}</button>
              </div>
            )}
            <div className="px-2 py-1 text-gray-950 whitespace-pre-wrap break-all select-text">{l.msg}</div>
          </div>
        ))}
      </div>
      {!atBottom && lines.length > 0 && (
        <div className="absolute right-4 bottom-4 flex flex-col gap-1">
          <button className="w-9 h-9 rounded-md border border-gray-200 bg-secondaryBg flex items-center justify-center text-gray-600 hover:text-foreground" onClick={() => ref.current && (ref.current.scrollTop = 0)}><ArrowUp size={14} /></button>
          <button className="w-9 h-9 rounded-md border border-gray-200 bg-secondaryBg flex items-center justify-center text-gray-600 hover:text-foreground" onClick={() => ref.current && (ref.current.scrollTop = ref.current.scrollHeight)}><ArrowDown size={14} /></button>
        </div>
      )}
    </div>
  );
}

function Histogram({ lines, since, until }: { lines: LogLine[]; since: number; until: number }) {
  const N = 90;
  const buckets = useMemo(() => {
    const b = Array.from({ length: N }, () => ({ info: 0, error: 0 }));
    const span = Math.max(1, until - since);
    for (const l of lines) {
      const i = Math.min(N - 1, Math.max(0, Math.floor(((l.ts - since) / span) * N)));
      b[i][l.level === "error" ? "error" : "info"]++;
    }
    return b;
  }, [lines, since, until]);
  const max = Math.max(1, ...buckets.map((b) => b.info + b.error));
  const d = (t: number) => new Date(t * 1000).toISOString().slice(0, 10);
  return (
    <div className="px-6 pb-3">
      <div className="flex items-end h-[30px] gap-[2px]">
        {buckets.map((b, i) => (
          <div key={i} className="flex-1 flex flex-col-reverse h-full" title={`${b.info + b.error} lines`}>
            <div className="bg-blue-500 rounded-[1px]" style={{ height: `${(b.info / max) * 100}%` }} />
            <div className="bg-red-500 rounded-[1px]" style={{ height: `${(b.error / max) * 100}%` }} />
          </div>
        ))}
      </div>
      <div className="flex justify-between mt-1 font-mono text-xs text-gray-600"><span>{d(since)}</span><span>{d(until)}</span></div>
    </div>
  );
}

function RangeButton({ value, onChange }: { value: number; onChange: (v: number) => void }) {
  return (
    <Popover align="right" trigger={() => (
      <button className="flex items-center gap-2 h-[34px] px-3 rounded-md border border-gray-200 hover:border-gray-400 text-sm whitespace-nowrap">
        <Clock size={15} className="text-gray-600" />{RANGES.find((r) => r[0] === value)?.[1]}
      </button>
    )}>
      {(close) => RANGES.map(([v, l]) => <MenuItem key={v} active={v === value} onClick={() => { onChange(v); close(); }}>{l}</MenuItem>)}
    </Popover>
  );
}

/** Log explorer: filter bar, range, live toggle, histogram, table. */
export default function LogView({ targets, version, showService, onService, histogram = true, defaultRange = 3600, live: liveAllowed = true, placeholder }: {
  targets: string[]; version?: string; showService?: boolean; onService?: (l: LogLine) => void; histogram?: boolean; defaultRange?: number; live?: boolean; placeholder?: string;
}) {
  const [q, setQ] = useState("");
  const [applied, setApplied] = useState("");
  const [range, setRange] = useState(defaultRange);
  const [live, setLive] = useState(liveAllowed);
  const [lines, setLines] = useState<LogLine[] | null>(null);
  const [now, setNow] = useState(() => Date.now() / 1000);
  const inputRef = useRef<HTMLInputElement>(null);
  const key = targets.join(",");

  useEffect(() => {
    const h = (e: KeyboardEvent) => { if (e.key === "/" && document.activeElement?.tagName !== "INPUT" && document.activeElement?.tagName !== "TEXTAREA") { e.preventDefault(); inputRef.current?.focus(); } };
    window.addEventListener("keydown", h);
    return () => window.removeEventListener("keydown", h);
  }, []);

  useEffect(() => {
    if (!targets.length) return;
    let alive = true;
    setLines(null);
    const t = Date.now() / 1000;
    setNow(t);
    api.logs(targets, { q: applied, tail: 1500, since: t - range, version }).then((r) => alive && setLines(r.lines)).catch(() => alive && setLines([]));
    return () => { alive = false; };
  }, [key, applied, range, version]); // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    if (!live || !liveAllowed || !targets.length || lines === null) return;
    const es = new EventSource(api.logStreamUrl(targets, applied));
    const buf: LogLine[] = [];
    es.onmessage = (m) => { try { buf.push(JSON.parse(m.data)); } catch { /* keepalive */ } };
    const flush = setInterval(() => {
      if (!buf.length) return;
      const add = buf.splice(0);
      const lvl = /@level:(\w+)/.exec(applied)?.[1];
      setLines((cur) => [...(cur || []), ...add.filter((l) => !lvl || l.level === lvl)].slice(-4000));
      setNow(Date.now() / 1000);
    }, 400);
    return () => { es.close(); clearInterval(flush); };
  }, [live, liveAllowed, key, applied, lines === null]); // eslint-disable-line react-hooks/exhaustive-deps

  const download = () => {
    const text = (lines || []).map((l) => `${fmtDateTime(l.ts)}\t${l.service || ""}\t${l.msg}`).join("\n");
    const a = document.createElement("a");
    a.href = URL.createObjectURL(new Blob([text], { type: "text/plain" }));
    a.download = `logs-${Date.now()}.txt`;
    a.click();
  };

  return (
    <div className="flex flex-col h-full min-h-0">
      <div className="bg-background border-b border-gray-200 flex flex-col gap-y-3.5">
        <div className="flex items-center gap-2 px-6 pt-4">
          <form className="flex-1 min-w-0" onSubmit={(e) => { e.preventDefault(); setApplied(q); }}>
            <label className="flex items-center gap-3 h-[34px] px-3 rounded-md border border-gray-200 focus-within:border-pink-400 font-mono text-sm">
              <Search size={15} className="text-gray-500 shrink-0" />
              <input ref={inputRef} value={q} onChange={(e) => setQ(e.target.value)} onBlur={() => setApplied(q)}
                placeholder={placeholder || 'Filter and search logs'} className="flex-1 min-w-0 bg-transparent outline-none font-sans placeholder:text-gray-500" />
              <kbd className="text-xs text-gray-500 border border-gray-200 rounded px-1.5">/</kbd>
            </label>
          </form>
          <button onClick={download} title="Download" className="w-[34px] h-[34px] rounded-md border border-gray-200 hover:border-gray-400 flex items-center justify-center text-gray-600"><Download size={15} /></button>
          {histogram && <RangeButton value={range} onChange={setRange} />}
          {liveAllowed && (
            <button onClick={() => setLive((l) => !l)} title={live ? "Pause live tail" : "Resume live tail"}
              className={cx("w-[34px] h-[34px] rounded-md border flex items-center justify-center", live ? "border-green-400 text-green-600 bg-green-100" : "border-gray-200 text-gray-600")}>
              {live ? <Pause size={14} /> : <Play size={14} />}
            </button>
          )}
        </div>
        {histogram ? <Histogram lines={lines || []} since={now - range} until={now} /> : <div />}
      </div>
      {lines === null ? <div className="flex-1 flex items-center justify-center"><Spinner /></div> :
        <LogTable lines={lines} showService={showService} onService={onService} follow={live} empty={applied ? `No logs match "${applied}".` : undefined} />}
    </div>
  );
}
