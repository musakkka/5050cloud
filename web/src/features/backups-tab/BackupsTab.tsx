// SPDX-License-Identifier: AGPL-3.0-only

// Backups tab (docs/SPEC.md 13.3, 9.16): health, Back up now, schedule,
// lock, delete, restore to a copy (backup or point in time), promote.

import { useState } from "react";
import { Lock, Unlock } from "lucide-react";
import type { Backup, BackupSchedule, BackupsResponse, RestoreResponse, Service } from "../../api/types.gen";
import { del, get, post, put, svcPath } from "../service/client";
import { Button, Confirm, Empty, ErrorLine, Input, Loading, Modal, Section, When, cx, fmtBytes, useAction, useLoad } from "../service/ui";
import { parseCron } from "../cron/cron";

export default function BackupsTab({ eid, service, onOpenService }: { eid: string; service: Service; onOpenService?: (sid: string) => void }) {
  const base = svcPath(eid, service.id);
  const b = useLoad(() => get<BackupsResponse>(`${base}/backups`), [base], 30000);
  const a = useAction();
  const [restore, setRestore] = useState<Backup | "pitr" | null>(null);
  const [removing, setRemoving] = useState<Backup | null>(null);
  const [promote, setPromote] = useState(false);
  const [restored, setRestored] = useState<RestoreResponse | null>(null);

  if (!b.data) return b.error ? <ErrorLine>{b.error}</ErrorLine> : <Loading />;
  const { health, backups, schedule } = b.data;
  const isPg = service.config.template === "postgres";

  if (!health.supported) return <Empty>Backups are not available for this template in v0.1.</Empty>;
  return (
    <div>
      {!health.configured && <div className="mb-4 rounded-md border border-yellow-300 bg-yellow-100 px-4 py-2 text-sm text-yellow-700">No backup bucket is set. Add one in Settings, Backups.</div>}
      {health.stale && <div className="mb-4 rounded-md border border-red-300 bg-red-100 px-4 py-2 text-sm text-red-700">{health.message || "The last good backup is older than 26 hours."}</div>}
      <div className="mb-6 flex flex-wrap items-center gap-3 text-sm">
        <span>Last good backup: {health.lastOk ? <When ts={health.lastOk} /> : "never"}</span>
        {isPg && health.recoverableTo && <span className="text-gray-600">Recoverable to {new Date(health.recoverableTo).toLocaleString()}</span>}
        {isPg && health.walArchiveOk !== undefined && <span className={health.walArchiveOk ? "text-green-700" : "text-red-600"}>WAL archiving {health.walArchiveOk ? "OK" : "failing"}</span>}
        <div className="ml-auto flex gap-2">
          {isPg && <Button disabled={!health.configured} onClick={() => setRestore("pitr")}>Restore to a point in time</Button>}
          <Button onClick={() => setPromote(true)}>Promote a copy</Button>
          <Button variant="primary" disabled={a.busy || !health.configured} onClick={() => void a.run(async () => { await post(`${base}/backups`); await b.reload(); })}>Back up now</Button>
        </div>
      </div>
      <ErrorLine>{a.error}</ErrorLine>
      {restored && (
        <div className="mb-4 rounded-md border border-green-300 bg-green-100 px-4 py-2 text-sm text-green-700">
          Restoring to a new service{restored.recoveredTo ? `, recovered to ${new Date(restored.recoveredTo).toLocaleString()}` : ""}.
          {onOpenService && <button className="ml-2 underline" onClick={() => onOpenService(restored.serviceId)}>Open the copy</button>}
        </div>
      )}

      <Section title="Backups">
        {backups.length === 0 ? <Empty>No backups yet.</Empty> : (
          <div className="divide-y divide-gray-200 rounded-lg border border-gray-200">
            {backups.map((x) => (
              <div key={x.id} className="flex flex-wrap items-center gap-3 px-4 py-2 text-sm">
                <span className={cx("w-16 capitalize", x.status === "ok" ? "text-green-700" : x.status === "failed" ? "text-red-600" : "text-blue-600")}>{x.status}</span>
                <span><When ts={x.startedAt} /></span>
                <span className="text-gray-500">{fmtBytes(x.sizeBytes)}</span>
                <span className="text-xs text-gray-500">{x.trigger.replace("_", " ")}</span>
                {x.verifiedAt && <span className="text-xs text-green-700">Restore tested <When ts={x.verifiedAt} /></span>}
                {x.error && <span className="text-xs text-red-600">{x.error}</span>}
                <div className="ml-auto flex items-center gap-1">
                  <button aria-label={x.locked ? "Unlock backup" : "Lock backup"} title={x.locked ? "Locked: kept past retention" : "Lock to keep past retention"}
                    onClick={() => void a.run(async () => { await post(`/backups/${encodeURIComponent(x.id)}/lock`, { locked: !x.locked }); await b.reload(); })}>
                    {x.locked ? <Lock size={14} /> : <Unlock size={14} className="text-gray-400" />}
                  </button>
                  {x.status === "ok" && <Button variant="ghost" onClick={() => setRestore(x)}>Restore</Button>}
                  <Button variant="ghost" onClick={() => setRemoving(x)}>Delete</Button>
                </div>
              </div>
            ))}
          </div>
        )}
      </Section>
      <ScheduleForm base={base} schedule={schedule} isPg={isPg} onSaved={() => void b.reload()} />

      {restore && <RestoreModal base={base} backup={restore === "pitr" ? undefined : restore} pitr={restore === "pitr"} latest={health.recoverableTo}
        onClose={() => setRestore(null)} onDone={(r) => { setRestored(r); setRestore(null); }} />}
      {removing && <Confirm title="Delete backup" danger confirm="Delete" body="This backup is deleted from the bucket." onClose={() => setRemoving(null)}
        onConfirm={async () => { await del(`/backups/${encodeURIComponent(removing.id)}`); await b.reload(); }} />}
      {promote && <PromoteModal eid={eid} service={service} onClose={() => setPromote(false)} />}
    </div>
  );
}

function ScheduleForm({ base, schedule, isPg, onSaved }: { base: string; schedule: BackupSchedule; isPg: boolean; onSaved: () => void }) {
  const [s, setS] = useState(schedule);
  const a = useAction();
  const cron = parseCron(s.cron);
  const dirty = JSON.stringify(s) !== JSON.stringify(schedule);
  return (
    <Section title="Schedule">
      <div className="flex flex-col gap-3 text-sm">
        <label className="flex items-center gap-2"><input type="checkbox" checked={s.enabled} onChange={(e) => setS({ ...s, enabled: e.target.checked })} />Scheduled backups</label>
        <label>Schedule (cron)<Input value={s.cron} onChange={(e) => setS({ ...s, cron: e.target.value })} />{!cron.ok && <span className="text-xs text-red-600">{cron.error}</span>}</label>
        <label>Timezone<Input value={s.timezone} onChange={(e) => setS({ ...s, timezone: e.target.value })} /></label>
        <label>Keep for (days)<Input type="number" value={s.retainDays} onChange={(e) => setS({ ...s, retainDays: Number(e.target.value) })} /></label>
        {isPg && <label className="flex items-center gap-2"><input type="checkbox" checked={s.continuousWal} onChange={(e) => setS({ ...s, continuousWal: e.target.checked })} />Continuous WAL archiving (point-in-time restore)</label>}
        <div><Button variant="primary" disabled={!dirty || !cron.ok || a.busy} onClick={async () => { if (await a.run(() => put(`${base}/backups/schedule`, s))) onSaved(); }}>Save schedule</Button></div>
        <ErrorLine>{a.error}</ErrorLine>
      </div>
    </Section>
  );
}

function toLocalInput(ts: number) {
  const d = new Date(ts - new Date(ts).getTimezoneOffset() * 60000);
  return d.toISOString().slice(0, 16);
}

function RestoreModal({ base, backup, pitr, latest, onClose, onDone }: {
  base: string; backup?: Backup; pitr: boolean; latest?: number; onClose: () => void; onDone: (r: RestoreResponse) => void;
}) {
  const [when, setWhen] = useState(toLocalInput(latest || Date.now()));
  const a = useAction();
  return (
    <Modal title="Restore to a copy" onClose={onClose} footer={<>
      <Button onClick={onClose}>Cancel</Button>
      <Button variant="primary" disabled={a.busy} onClick={() => void a.run(async () => {
        const body = pitr ? { pointInTime: new Date(when).getTime() } : { backupId: backup!.id };
        onDone(await post<RestoreResponse>(`${base}/restore`, body));
      })}>Restore</Button>
    </>}>
      <p className="mb-3">The restore goes to a new service next to this one. This service is not touched. Promote the copy when you have checked it.</p>
      {pitr && <label className="block">Point in time<Input type="datetime-local" value={when} onChange={(e) => setWhen(e.target.value)} /></label>}
      {!pitr && backup && <p className="text-gray-600">Backup from {new Date(backup.startedAt).toLocaleString()}.</p>}
      <ErrorLine>{a.error}</ErrorLine>
    </Modal>
  );
}

function PromoteModal({ eid, service, onClose }: { eid: string; service: Service; onClose: () => void }) {
  const canvas = useLoad(() => get<{ services: Service[] }>(`/environments/${encodeURIComponent(eid)}/canvas`), [eid]);
  const [pick, setPick] = useState("");
  const a = useAction();
  const copies = (canvas.data?.services || []).filter((s) => s.id !== service.id && s.config.name.startsWith(`${service.config.name}-restored-`));
  return (
    <Modal title="Promote a restored copy" onClose={onClose} footer={<>
      <Button onClick={onClose}>Cancel</Button>
      <Button variant="danger" disabled={!pick || a.busy} onClick={async () => {
        if (await a.run(() => post(`${svcPath(eid, service.id)}/promote`, { restoredServiceId: pick }))) onClose();
      }}>Promote</Button>
    </>}>
      <p className="mb-3">The copy takes the name <b>{service.config.name}</b>, so references resolve to it. This service is renamed and stopped, and services that use it redeploy.</p>
      {copies.length === 0 ? <Empty>No restored copies of this service in this environment.</Empty> : copies.map((c) => (
        <label key={c.id} className="flex items-center gap-2"><input type="radio" name="copy" checked={pick === c.id} onChange={() => setPick(c.id)} />{c.config.name}</label>
      ))}
      <ErrorLine>{a.error}</ErrorLine>
    </Modal>
  );
}
