// SPDX-License-Identifier: AGPL-3.0-only

// Time range button and popover: nine quick ranges plus free-text From / Until inputs.

import { useEffect, useState } from "react";
import { Calendar, Clock } from "lucide-react";
import { Popover, cx } from "../../kit";
import { QUICK_RANGES, fmtStamp, parseTimeInput, rangeBounds, rangeLabel, type Range } from "./query";

export default function TimeRange({ range, onChange }: { range: Range; onChange: (r: Range) => void }) {
  return (
    <Popover align="right" width={346} offset={6} role="dialog" className="!p-0"
      trigger={(open) => (
        <button type="button" aria-label="Time range" aria-expanded={open}
          className={cx("inline-flex h-[34px] items-center gap-2 rounded-md border border-line px-3 text-sm text-foreground transition-[background-color,border-color,transform] duration-150 hover:bg-surface-hover active:scale-95", open && "bg-surface-hover")}>
          <Clock size={16} className="text-fg-3" />{rangeLabel(range)}
        </button>
      )}>
      {() => <Panel range={range} onChange={onChange} />}
    </Popover>
  );
}

function Panel({ range, onChange }: { range: Range; onChange: (r: Range) => void }) {
  const q = QUICK_RANGES.find((x) => x.id === range.id);
  const [from, setFrom] = useState(range.id === "custom" ? range.from ?? "" : q ? q.id.replace(/(\d+)([a-z])/, "$1$2 ago") : "7d ago");
  const [until, setUntil] = useState(range.id === "custom" ? range.until ?? "" : "");
  const now = Date.now();
  const f = parseTimeInput(from, now, null);
  const u = parseTimeInput(until, now, now);
  const invalid = (from !== "" && f === null) || (until !== "" && u === null) || (f !== null && u !== null && f >= u);
  useEffect(() => {
    if (invalid || from === "") return;
    // keep a quick range selected when the text equals its relative form
    const quick = QUICK_RANGES.find((x) => `${x.id} ago` === from.trim().toLowerCase());
    if (quick && until === "") return;
    const t = window.setTimeout(() => onChange({ id: "custom", from, until }), 450);
    return () => window.clearTimeout(t);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [from, until]);
  const pick = (id: string) => { setFrom(`${id} ago`); setUntil(""); onChange({ id }); };
  const b = rangeBounds({ id: "custom", from, until }, now);
  return (
    <div className="p-4">
      <div className="mb-2 text-caption text-fg-2">Quick range</div>
      <div className="grid grid-cols-3 gap-2">
        {QUICK_RANGES.map((r) => (
          <button key={r.id} type="button" onClick={() => pick(r.id)} aria-pressed={range.id === r.id}
            className={cx("h-[26px] rounded-md border border-line font-mono text-caption transition-colors duration-150 active:scale-95",
              range.id === r.id ? "bg-gray-200 text-foreground" : "text-fg-2 hover:bg-surface-hover hover:text-foreground")}>{r.label}</button>
        ))}
      </div>
      <Field label="From" value={from} onChange={setFrom} placeholder="7d ago, 2023-12-25" error={from !== "" && f === null} />
      <Field label="Until" value={until} onChange={setUntil} placeholder="1h ago, 2023-12-25, now" error={until !== "" && u === null} />
      <div className={cx("mt-3 min-h-[18px] text-caption", invalid ? "text-danger-text" : "text-fg-3")}>
        {invalid ? "Use a relative time like 1h ago, a date, or now. From must be before Until." : b ? `${fmtStamp(b.from, false)} to ${fmtStamp(b.to, false)}` : ""}
      </div>
    </div>
  );
}

function Field({ label, value, onChange, placeholder, error }: { label: string; value: string; onChange: (v: string) => void; placeholder: string; error?: boolean }) {
  return (
    <label className="mt-4 block">
      <span className="mb-2 block text-caption text-fg-2">{label}</span>
      <span className="relative block">
        <input value={value} onChange={(e) => onChange(e.target.value)} placeholder={placeholder} aria-invalid={error || undefined} spellCheck={false}
          className="h-[34px] w-full rounded-md border border-line bg-transparent pl-2 pr-9 text-caption text-foreground outline-none transition-colors duration-150 placeholder:text-fg-4 hover:border-fg-5 focus:border-fg-4 aria-[invalid=true]:border-danger" />
        <Calendar size={16} className="pointer-events-none absolute right-3 top-1/2 -translate-y-1/2 text-fg-3" />
      </span>
    </label>
  );
}
