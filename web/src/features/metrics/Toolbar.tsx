// SPDX-License-Identifier: AGPL-3.0-only

// Metrics toolbar: list/grid layout toggle, range picker popover (quick chips plus From/Until
// with relative or absolute input) and the live-updates toggle.

import { useCallback, useRef, useState } from "react";
import { CalendarDays, Clock, LayoutGrid, Pause, Play, Rows2 } from "lucide-react";
import { Popover, Tooltip, cx, readPref, writePref } from "../../kit";
import { RANGES, RANGE_CHIP, fmtLocalInput, parseRelative, rangeInputs, rangeLabel, type RangeSel } from "./series";

export type Layout = "list" | "grid";
const LAYOUT_KEY = "5050.metrics.layout";

/** Layout choice, remembered across services and visits. */
export function useLayout(): [Layout, (l: Layout) => void] {
  const [l, setL] = useState<Layout>(() => (readPref<string>(LAYOUT_KEY, "list") === "grid" ? "grid" : "list"));
  const set = useCallback((v: Layout) => { setL(v); writePref(LAYOUT_KEY, v); }, []);
  return [l, set];
}

export function LayoutToggle({ value, onChange }: { value: Layout; onChange: (l: Layout) => void }) {
  const seg = (v: Layout, label: string, icon: React.ReactNode, round: string) => (
    <button type="button" aria-label={label} aria-pressed={value === v} title={label} onClick={() => onChange(v)}
      className={cx("inline-flex h-[34px] w-[46px] items-center justify-center border transition-colors duration-150 active:scale-[.97]", round,
        value === v ? "border-fg-5 bg-surface-raised text-accent-text" : "border-line text-fg-5 hover:text-fg-3")}>
      {icon}
    </button>
  );
  return (
    <div role="group" aria-label="Chart layout" className="inline-flex">
      {seg("list", "List layout", <Rows2 size={20} strokeWidth={1.75} />, "rounded-l-md")}
      {seg("grid", "Grid layout", <LayoutGrid size={20} strokeWidth={1.75} />, "-ml-px rounded-r-md")}
    </div>
  );
}

export function LiveToggle({ live, onChange }: { live: boolean; onChange: (v: boolean) => void }) {
  return (
    <Tooltip content={live ? "Pause live updates" : "Resume live updates"} side="bottom">
      <button type="button" aria-label={live ? "Pause live updates" : "Resume live updates"} aria-pressed={live} onClick={() => onChange(!live)}
        className={cx("inline-flex h-[34px] w-[34px] items-center justify-center rounded-md border transition-all duration-150 active:scale-[.97]",
          live ? "border-green-200 text-ok-soft hover:bg-green-100" : "border-line text-foreground hover:bg-surface-hover")}>
        {live ? <Pause size={16} className="text-ok" strokeWidth={2} /> : <Play size={16} fill="currentColor" strokeWidth={1.5} />}
      </button>
    </Tooltip>
  );
}

export function RangePicker({ value, onChange, align = "right" }: { value: RangeSel; onChange: (s: RangeSel) => void; align?: "left" | "right" }) {
  const [open, setOpen] = useState(false);
  return (
    <Popover open={open} onOpenChange={setOpen} align={align} width={346} role="dialog" offset={6}
      trigger={(o) => (
        <button type="button" aria-haspopup="dialog" aria-expanded={o} aria-label={`Time range: ${rangeLabel(value)}`}
          className={cx("inline-flex h-[34px] items-center gap-2 whitespace-nowrap rounded-md border px-3 text-body text-foreground transition-colors duration-150 hover:bg-surface-hover active:scale-[.97]", o ? "border-fg-5" : "border-line")}>
          <Clock size={16} className="text-fg-3" />{rangeLabel(value)}
        </button>
      )}>
      {() => <RangeForm value={value} onApply={(s) => { onChange(s); setOpen(false); }} />}
    </Popover>
  );
}

function RangeForm({ value, onApply }: { value: RangeSel; onApply: (s: RangeSel) => void }) {
  const init = rangeInputs(value);
  const [from, setFrom] = useState(init.from);
  const [until, setUntil] = useState(init.until);
  const [touched, setTouched] = useState(false);
  const f = parseRelative(from);
  const u = parseRelative(until);
  const bad = touched && (f === null || u === null || u <= f);
  const apply = () => {
    setTouched(true);
    if (f !== null && u !== null && u > f) onApply({ from: Math.round(f), until: Math.round(u) });
  };
  return (
    <div className="flex flex-col gap-4 p-4">
      <div>
        <div className="mb-3 text-caption font-medium text-fg-2">Quick range</div>
        <div className="grid grid-cols-3 gap-3">
          {RANGES.map((r) => (
            <button key={r} type="button" onClick={() => onApply({ range: r })} aria-pressed={!value.from && value.range === r}
              className={cx("h-[26px] rounded-md border border-line px-1.5 text-caption transition-colors duration-150 hover:bg-surface-hover", !value.from && value.range === r ? "bg-surface-hover text-foreground" : "text-fg-menu")}>
              {RANGE_CHIP[r]}
            </button>
          ))}
        </div>
      </div>
      <TimeField label="From" value={from} onChange={setFrom} invalid={bad && f === null} onCommit={apply} placeholder="7d ago, 2023-12-25, now" />
      <TimeField label="Until" value={until} onChange={setUntil} invalid={bad && (u === null || (f !== null && u <= f))} onCommit={apply} placeholder="1h ago, 2023-12-25, now" />
      {bad && <div role="alert" className="-mt-2 text-caption text-danger-text">Use a start before the end, such as "7d ago" to "now".</div>}
    </div>
  );
}

function TimeField({ label, value, onChange, onCommit, invalid, placeholder }: { label: string; value: string; onChange: (v: string) => void; onCommit: () => void; invalid: boolean; placeholder: string }) {
  const native = useRef<HTMLInputElement>(null);
  const id = `mr-${label.toLowerCase()}`;
  return (
    <div>
      <label htmlFor={id} className="mb-2 block text-caption font-medium text-fg-2">{label}</label>
      <div className={cx("relative flex h-8 items-center rounded-md border bg-transparent transition-colors duration-150 focus-within:border-fg-4", invalid ? "border-danger" : "border-line")}>
        <input id={id} value={value} placeholder={placeholder} onChange={(e) => onChange(e.target.value)} aria-invalid={invalid}
          onKeyDown={(e) => { if (e.key === "Enter") { e.preventDefault(); onCommit(); } }}
          className="h-full min-w-0 flex-1 bg-transparent px-2.5 text-caption text-foreground outline-none focus-visible:shadow-none placeholder:text-fg-4" />
        <button type="button" aria-label={`Pick ${label.toLowerCase()} date`} onClick={() => native.current?.showPicker?.()}
          className="mr-1 inline-flex h-6 w-6 items-center justify-center rounded text-fg-3 transition-colors hover:text-foreground">
          <CalendarDays size={14} />
        </button>
        <input ref={native} type="datetime-local" tabIndex={-1} aria-hidden className="pointer-events-none absolute right-0 top-full h-0 w-0 opacity-0"
          onChange={(e) => { if (e.target.value) onChange(fmtLocalInput(new Date(e.target.value).getTime())); }} />
      </div>
    </div>
  );
}
