// SPDX-License-Identifier: AGPL-3.0-only

// The Cron Schedule setting (Settings > Deploy): "+ Add Schedule" turns into
// a preset dropdown with the expression underneath and a Customize link to
// type any 5-field expression; then the next runs, time zone, overlap rule
// and run timeout.

import { useEffect, useMemo, useState } from "react";
import { Clock, Plus } from "lucide-react";
import { Button, Tooltip, cx } from "../../kit";
import { nextRuns, parseCron, validTimeZone } from "./cron";

export const SCHEDULE_PRESETS = [
  { value: "*/15 * * * *", label: "Every 15 minutes" },
  { value: "0 * * * *", label: "Hourly" },
  { value: "0 0 * * *", label: "Daily" },
  { value: "0 0 * * 0", label: "Weekly" },
  { value: "0 0 1 * *", label: "Monthly" },
];

const INPUT = "h-[42px] w-full rounded-md border border-line bg-transparent px-3 text-base text-foreground outline-none transition-colors duration-150 placeholder:text-fg-4 hover:border-fg-5 focus:border-accent-muted focus:shadow-none aria-[invalid=true]:border-danger";

export interface CronValue { schedule: string; timezone: string; overlap: string; timeoutSec?: number }

/** Renders nothing about layout outside its own block; the caller wraps it in a field. */
export default function CronSchedule({ value, onStage, disabled, renderDropdown }: {
  value: CronValue;
  onStage: (patch: Partial<Record<"cronSchedule" | "cronTimezone" | "cronOverlap" | "cronTimeoutSec", unknown>>) => Promise<unknown>;
  disabled?: boolean;
  /** The settings dropdown component, injected so this folder does not depend on Settings. */
  renderDropdown: (p: { value: string; options: { value: string; label: string }[]; onChange: (v: string) => void; label: string }) => JSX.Element;
}) {
  const expr = value.schedule;
  const preset = SCHEDULE_PRESETS.find((p) => p.value === expr);
  const [custom, setCustom] = useState(!!expr && !preset);
  const [text, setText] = useState(expr);
  useEffect(() => setText(expr), [expr]);
  const parsed = useMemo(() => (text ? parseCron(text) : null), [text]);
  const tz = value.timezone || "UTC";
  const runs = useMemo(() => {
    const p = expr ? parseCron(expr) : null;
    return p?.ok && validTimeZone(tz) ? nextRuns(p.cron, 3, Date.now(), tz) : [];
  }, [expr, tz]);

  if (!expr && !custom) {
    return <Button icon={<Plus size={16} />} disabled={disabled} onClick={() => void onStage({ cronSchedule: "0 0 * * *" })}>Add Schedule</Button>;
  }

  const saveExpr = () => {
    const t = text.trim();
    if (!t || !parsed?.ok || t === expr) return;
    void onStage({ cronSchedule: t }).catch(() => setText(expr));
  };

  return (
    <div className="flex flex-col gap-3">
      {custom ? (
        <div>
          <input aria-label="Cron expression" autoFocus={!expr} value={text} disabled={disabled} placeholder="*/5 * * * *" aria-invalid={parsed && !parsed.ok ? true : undefined}
            onChange={(e) => setText(e.target.value)} onBlur={saveExpr}
            onKeyDown={(e) => { if (e.key === "Enter") (e.target as HTMLInputElement).blur(); if (e.key === "Escape") { e.stopPropagation(); setText(expr); } }}
            className={cx(INPUT, "font-mono text-[15px]")} />
          <div className="mt-1.5 flex items-center justify-between gap-3 text-caption">
            <span className={cx(parsed && !parsed.ok ? "text-danger-text" : "text-fg-3")}>
              {parsed && !parsed.ok ? parsed.error : "minute hour day-of-month month day-of-week, or @hourly, @daily, @weekly"}
            </span>
            <button type="button" className="shrink-0 text-fg-2 transition-colors hover:text-foreground" onClick={() => { setCustom(false); if (!preset) void onStage({ cronSchedule: "0 0 * * *" }); }}>Use a preset</button>
          </div>
        </div>
      ) : (
        <div>
          {renderDropdown({ value: expr, options: SCHEDULE_PRESETS, onChange: (v) => void onStage({ cronSchedule: v }), label: "Schedule" })}
          <div className="mt-1.5 flex items-center justify-between font-mono text-caption text-fg-2">
            <span className="tracking-[.2em]">{expr}</span>
            <button type="button" className="font-sans text-fg-2 transition-colors hover:text-foreground" onClick={() => setCustom(true)}>Customize</button>
          </div>
        </div>
      )}

      {runs.length > 0 && (
        <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-sm text-fg-2">
          <Clock size={14} className="text-fg-3" />
          <span>Next runs</span>
          {runs.map((t, i) => (
            <Tooltip key={t} content={new Date(t).toUTCString()}>
              <span tabIndex={0} className={cx("rounded px-1.5 py-0.5 text-caption", i === 0 ? "bg-pink-50 text-pink-700" : "bg-surface-hover text-fg-2")}>
                {new Date(t).toLocaleString(undefined, { weekday: "short", hour: "2-digit", minute: "2-digit", timeZone: tz })}
              </span>
            </Tooltip>
          ))}
        </div>
      )}

      <div className="grid grid-cols-[1fr_1fr] gap-2">
        <span className="text-caption text-fg-3">Time zone</span>
        <span className="text-caption text-fg-3">If the last run is still going</span>
        <TzInput value={value.timezone} onSave={(v) => onStage({ cronTimezone: v })} />
        {renderDropdown({
          value: value.overlap === "queue" ? "queue" : "skip", label: "When a run is still going",
          options: [{ value: "skip", label: "Skip overlapping runs" }, { value: "queue", label: "Queue overlapping runs" }],
          onChange: (v) => void onStage({ cronOverlap: v === "skip" ? "" : v }),
        })}
      </div>
      <div className="flex items-center justify-between text-caption">
        <TimeoutInput value={value.timeoutSec} onSave={(n) => onStage({ cronTimeoutSec: n })} />
        <button type="button" className="text-fg-3 transition-colors hover:text-danger-text"
          onClick={() => { setCustom(false); void onStage({ cronSchedule: "", cronTimezone: "", cronOverlap: "", cronTimeoutSec: "" }); }}>Remove schedule</button>
      </div>
    </div>
  );
}

function TzInput({ value, onSave }: { value: string; onSave: (v: string) => Promise<unknown> }) {
  const [t, setT] = useState(value);
  useEffect(() => setT(value), [value]);
  const bad = !!t && !validTimeZone(t);
  return (
    <div>
      <input aria-label="Time zone" value={t} placeholder="UTC" list="fifty-timezones" aria-invalid={bad || undefined}
        onChange={(e) => setT(e.target.value)} onBlur={() => { if (!bad && t !== value) void onSave(t).catch(() => setT(value)); }}
        onKeyDown={(e) => { if (e.key === "Enter") (e.target as HTMLInputElement).blur(); }}
        className={cx(INPUT, "h-11")} />
      <datalist id="fifty-timezones">{zones().map((z) => <option key={z} value={z} />)}</datalist>
      {bad && <div className="mt-1 text-caption text-danger-text">Unknown time zone, for example Europe/Berlin</div>}
    </div>
  );
}

function TimeoutInput({ value, onSave }: { value?: number; onSave: (n: number | "") => Promise<unknown> }) {
  const [t, setT] = useState(value ? String(value) : "");
  useEffect(() => setT(value ? String(value) : ""), [value]);
  return (
    <label className="flex items-center gap-2 text-fg-2">
      Stop a run after
      <input aria-label="Run timeout in seconds" inputMode="numeric" value={t} placeholder="none"
        onChange={(e) => setT(e.target.value.replace(/\D/g, ""))}
        onBlur={() => { const n = t ? Number(t) : ""; if (n !== (value || "")) void onSave(n).catch(() => setT(value ? String(value) : "")); }}
        onKeyDown={(e) => { if (e.key === "Enter") (e.target as HTMLInputElement).blur(); }}
        className="h-[30px] w-20 rounded-md border border-line bg-transparent px-2 text-center font-mono text-caption text-foreground outline-none focus:border-accent-muted focus:shadow-none" />
      seconds
    </label>
  );
}

let zoneCache: string[] | null = null;
function zones(): string[] {
  if (zoneCache) return zoneCache;
  try {
    const fn = (Intl as unknown as { supportedValuesOf?: (k: string) => string[] }).supportedValuesOf;
    zoneCache = fn ? ["UTC", ...fn("timeZone")] : ["UTC"];
  } catch { zoneCache = ["UTC"]; }
  return zoneCache;
}
