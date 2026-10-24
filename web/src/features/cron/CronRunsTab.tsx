// SPDX-License-Identifier: AGPL-3.0-only

// Cron Runs tab (docs/SPEC.md 13.3): the schedule and next runs, Run now, and
// the run history with missed rows.

import { useMemo, useState } from "react";
import { CalendarClock, CircleAlert, CircleCheck, CircleDashed, CircleSlash, Clock, Loader2, Play, TimerOff } from "lucide-react";
import type { CronRun, Service } from "../../api/types.gen";
import { get, post, svcPath } from "../service/client";
import { useEnvEvents } from "../service/events";
import { useLoad } from "../service/ui";
import { Button, EmptyState, RelTime, Skeleton, Tooltip, cx, toastError } from "../../kit";
import { nextRuns, parseCron } from "./cron";

const LOOK: Record<string, { icon: JSX.Element; text: string; label: string }> = {
  completed: { icon: <CircleCheck size={16} />, text: "text-ok-text", label: "Completed" },
  failed: { icon: <CircleAlert size={16} />, text: "text-danger-text", label: "Failed" },
  timeout: { icon: <TimerOff size={16} />, text: "text-danger-text", label: "Timed out" },
  missed: { icon: <CircleDashed size={16} />, text: "text-warn", label: "Missed" },
  running: { icon: <Loader2 size={16} className="spin" />, text: "text-blue-600", label: "Running" },
  skipped: { icon: <CircleSlash size={16} />, text: "text-fg-3", label: "Skipped" },
};

const dur = (ms: number) => (ms < 60_000 ? `${(ms / 1000).toFixed(1)}s` : `${Math.floor(ms / 60_000)}m ${Math.round((ms % 60_000) / 1000)}s`);

export default function CronRunsTab({ eid, service }: { eid: string; service: Service }) {
  const base = svcPath(eid, service.id);
  const runs = useLoad(() => get<CronRun[]>(`${base}/cron-runs`), [base]);
  const [busy, setBusy] = useState(false);
  useEnvEvents(eid, (e) => { if (e.serviceId === service.id && (e.type === "deployment" || e.type === "service")) void runs.reload(); }, () => void runs.reload());
  const cfg = service.staged ?? service.config;
  const tz = cfg.deploy.cronTimezone || "UTC";
  const next = useMemo(() => {
    const p = cfg.deploy.cronSchedule ? parseCron(cfg.deploy.cronSchedule) : null;
    return p?.ok ? nextRuns(p.cron, 3, Date.now(), tz) : [];
  }, [cfg.deploy.cronSchedule, tz]);
  const runNow = async () => {
    setBusy(true);
    try { await post(`${base}/cron-runs`); await runs.reload(); } catch (e) { toastError(e); } finally { setBusy(false); }
  };

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-wrap items-center gap-4 rounded-lg border border-line px-4 py-4">
        <span className="inline-flex h-10 w-10 shrink-0 items-center justify-center rounded-full border border-line text-fg-3"><CalendarClock size={18} /></span>
        <div className="min-w-0 flex-1">
          {cfg.deploy.cronSchedule ? (
            <>
              <div className="flex items-center gap-2 text-base text-foreground">
                <code className="font-mono tracking-[.15em]">{cfg.deploy.cronSchedule}</code>
                <span className="text-sm text-fg-3">{tz}</span>
              </div>
              <div className="mt-1 flex flex-wrap items-center gap-1.5 text-caption text-fg-2">
                <Clock size={12} />Next
                {next.map((t, i) => (
                  <Tooltip key={t} content={new Date(t).toUTCString()}>
                    <span tabIndex={0} className={cx("rounded px-1.5 py-px", i === 0 ? "bg-pink-50 text-pink-700" : "bg-surface-hover")}>
                      {new Date(t).toLocaleString(undefined, { weekday: "short", hour: "2-digit", minute: "2-digit", timeZone: tz })}
                    </span>
                  </Tooltip>
                ))}
              </div>
            </>
          ) : (
            <div className="text-sm text-fg-2">No schedule set. Add one in Settings, Deploy, Cron Schedule.</div>
          )}
        </div>
        <Button variant="primary" icon={<Play size={14} />} busy={busy} onClick={() => void runNow()}>Run now</Button>
      </div>

      {runs.error && <div role="alert" className="text-sm text-danger-text">{runs.error}</div>}
      {!runs.data ? (!runs.error && (
        <div className="flex flex-col gap-2">{[0, 1, 2].map((i) => <Skeleton key={i} h={52} className="w-full rounded-lg" />)}</div>
      )) : runs.data.length === 0 ? (
        <EmptyState icon={<CalendarClock />} title="No runs yet" body="Runs show up here when the schedule fires or you press Run now." />
      ) : (
        <div className="overflow-hidden rounded-lg border border-line">
          {runs.data.map((r, i) => {
            const look = LOOK[r.status] || LOOK.skipped;
            return (
              <div key={r.id} className={cx("flex flex-wrap items-center gap-x-4 gap-y-1 px-4 py-3 text-sm transition-colors hover:bg-surface-hover", i > 0 && "border-t border-line")}>
                <span className={cx("flex w-32 items-center gap-2 font-medium", look.text)}>{look.icon}{look.label}</span>
                <span className="text-fg-2">{r.trigger === "manual" ? "Run now" : "Scheduled"}</span>
                <RelTime ts={r.startedAt || r.scheduledAt} className="text-fg-2" />
                {r.message && <span className="min-w-0 flex-1 truncate text-fg-3">{r.message}</span>}
                <span className="ml-auto flex items-center gap-3 font-mono text-caption text-fg-3">
                  {r.exitCode !== undefined && <span>exit {r.exitCode}</span>}
                  {r.startedAt && r.finishedAt && <span>{dur(r.finishedAt - r.startedAt)}</span>}
                </span>
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}
