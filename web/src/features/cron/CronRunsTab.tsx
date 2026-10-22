// SPDX-License-Identifier: AGPL-3.0-only

// Cron Runs tab (docs/SPEC.md 13.3): runs with Run now and missed rows.

import { useMemo } from "react";
import type { CronRun, Service } from "../../api/types.gen";
import { get, post, svcPath } from "../service/client";
import { useEnvEvents } from "../service/events";
import { Button, Empty, ErrorLine, Loading, When, cx, useAction, useLoad } from "../service/ui";
import { nextRuns, parseCron } from "./cron";

const COLOR: Record<string, string> = {
  completed: "text-green-700", failed: "text-red-600", timeout: "text-red-600", missed: "text-yellow-700", running: "text-blue-600", skipped: "text-gray-500",
};

export default function CronRunsTab({ eid, service }: { eid: string; service: Service }) {
  const base = svcPath(eid, service.id);
  const runs = useLoad(() => get<CronRun[]>(`${base}/cron-runs`), [base]);
  const a = useAction();
  useEnvEvents(eid, (e) => { if (e.serviceId === service.id && (e.type === "deployment" || e.type === "service")) void runs.reload(); }, () => void runs.reload());
  const cfg = service.staged ?? service.config;
  const next = useMemo(() => {
    const p = cfg.deploy.cronSchedule ? parseCron(cfg.deploy.cronSchedule) : null;
    return p?.ok ? nextRuns(p.cron, 3, Date.now(), cfg.deploy.cronTimezone || "UTC") : [];
  }, [cfg.deploy.cronSchedule, cfg.deploy.cronTimezone]);

  return (
    <div>
      <div className="mb-4 flex flex-wrap items-center justify-between gap-2">
        <div className="text-sm text-gray-600">
          {cfg.deploy.cronSchedule ? <>Schedule <span className="font-mono">{cfg.deploy.cronSchedule}</span> ({cfg.deploy.cronTimezone || "UTC"}). Next: {next.map((t) => new Date(t).toLocaleString()).join(", ")}</> : "No schedule set. Set one in Settings, Deploy."}
        </div>
        <Button variant="primary" disabled={a.busy} onClick={() => void a.run(async () => { await post(`${base}/cron-runs`); await runs.reload(); })}>Run now</Button>
      </div>
      <ErrorLine>{a.error || runs.error}</ErrorLine>
      {!runs.data ? (!runs.error && <Loading />) : runs.data.length === 0 ? <Empty>No runs yet.</Empty> : (
        <div className="divide-y divide-gray-200 rounded-lg border border-gray-200">
          {runs.data.map((r) => (
            <div key={r.id} className="flex flex-wrap items-center gap-3 px-4 py-2 text-sm">
              <span className={cx("w-24 font-medium capitalize", COLOR[r.status])}>{r.status}</span>
              <span className="text-gray-600">{r.trigger === "manual" ? "Run now" : "Scheduled"} <When ts={r.startedAt || r.scheduledAt} /></span>
              {r.exitCode !== undefined && <span className="text-xs text-gray-500">exit {r.exitCode}</span>}
              {r.startedAt && r.finishedAt && <span className="text-xs text-gray-500">{((r.finishedAt - r.startedAt) / 1000).toFixed(1)}s</span>}
              {r.message && <span className="text-xs text-gray-500">{r.message}</span>}
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
