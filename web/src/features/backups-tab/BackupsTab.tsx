// SPDX-License-Identifier: AGPL-3.0-only

// Backups tab: plan notice, next backup line, PITR card, the backup list (hover
// Restore, kebab, expandable details, shift-click multi-select) and the restore,
// schedule and promote dialogs. Restores always go to a new service.

import { useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { ChevronDown, Clock, HardDrive, History, Lock, MoreHorizontal, Plus, Trash2, Unlock, X } from "lucide-react";
import type { Backup, BackupSchedule, BackupsResponse, RestoreResponse, Service } from "../../api/types.gen";
import {
  Badge, Button, Checkbox, Confirm, ErrorLine, Field, Input, Menu, Modal, Notice, RelTime, Skeleton, Switch, Tooltip,
  cx, fmtBytes, fmtDateTime, plural, toastError, toastSuccess,
} from "../../kit";
import { del, get, post, put, svcPath } from "../service/client";
import { useLoad } from "../service/ui";
import { nextRuns, parseCron } from "../cron/cron";

type Cadence = "daily" | "weekly" | "monthly";
const PRESETS: Record<Cadence, { cron: string; days: number; label: string; note: string }> = {
  daily: { cron: "0 3 * * *", days: 7, label: "Daily", note: "Every day at 03:00. Kept for 7 days." },
  weekly: { cron: "0 3 * * 0", days: 30, label: "Weekly", note: "Sundays at 03:00. Kept for 30 days." },
  monthly: { cron: "0 3 1 * *", days: 90, label: "Monthly", note: "The 1st of each month at 03:00. Kept for 90 days." },
};

function cadenceOf(cron: string): Cadence {
  const f = cron.trim().split(/\s+/);
  if (f.length < 5) return "daily";
  if (f[2] !== "*") return "monthly";
  if (f[4] !== "*") return "weekly";
  return "daily";
}

function inText(ms: number) {
  const m = Math.max(1, Math.round(ms / 60000));
  if (m < 60) return `in ${plural(m, "minute")}`;
  const h = Math.round(m / 60);
  if (h < 48) return `in ${plural(h, "hour")}`;
  return `in ${plural(Math.round(h / 24), "day")}`;
}

function backupName(b: Backup, cadence: Cadence): string {
  if (b.trigger === "schedule") return `${PRESETS[cadence].label} Schedule`;
  if (b.trigger === "pre_upgrade") return "Before upgrade";
  if (b.trigger === "restore_test") return "Restore test";
  return new Date(b.startedAt).toISOString().slice(0, 16).replace("T", " ");
}

function viaText(b: Backup, cadence: Cadence): string {
  if (b.trigger === "schedule") return `via ${cadence} backups`;
  if (b.trigger === "pre_upgrade") return "via major upgrade";
  if (b.trigger === "restore_test") return "via restore test";
  return "via manual backup";
}

export default function BackupsTab({ eid, service, onOpenService }: { eid: string; service: Service; onOpenService?: (sid: string) => void }) {
  const base = svcPath(eid, service.id);
  const b = useLoad(() => get<BackupsResponse>(`${base}/backups`), [base], 30000);
  const [restore, setRestore] = useState<Backup | "pitr" | null>(null);
  const [removing, setRemoving] = useState<Backup[] | null>(null);
  const [promote, setPromote] = useState(false);
  const [schedule, setSchedule] = useState(false);
  const [pitrModal, setPitrModal] = useState<"enable" | "disable" | null>(null);
  const [restored, setRestored] = useState<RestoreResponse | null>(null);
  const [restoring, setRestoring] = useState<string | null>(null);
  const [open, setOpen] = useState<Set<string>>(new Set());
  const [sel, setSel] = useState<Set<string>>(new Set());
  const [creating, setCreating] = useState(false);
  const isPg = service.config.template === "postgres";

  const next = useMemo(() => {
    const s = b.data?.schedule;
    if (!s?.enabled) return null;
    const c = parseCron(s.cron);
    return c.ok ? nextRuns(c.cron, 1, Date.now(), s.timezone || "UTC")[0] ?? null : null;
  }, [b.data?.schedule]);

  if (!b.data) {
    return b.error ? <ErrorLine error={`Unable to list backups: ${b.error}`} /> : (
      <div className="mx-auto max-w-[799px] px-2">
        <Skeleton className="h-5 w-48" />
        <div className="mt-4 flex flex-col gap-2">{Array.from({ length: 4 }, (_, i) => <Skeleton key={i} className="h-[78px] rounded-lg" />)}</div>
      </div>
    );
  }
  const { health, backups, schedule: sched } = b.data;
  if (!health.supported) {
    return <div className="mx-auto max-w-[799px] px-2"><Notice tone="neutral">Backups are not available for this template yet. Copy data with your own client and the CLI tunnel.</Notice></div>;
  }
  const cadence = cadenceOf(sched.cron);
  const toggleSel = (id: string) => setSel((s) => { const n = new Set(s); if (n.has(id)) n.delete(id); else n.add(id); return n; });
  const toggleOpen = (id: string) => setOpen((s) => { const n = new Set(s); if (n.has(id)) n.delete(id); else n.add(id); return n; });

  const createBackup = async () => {
    setCreating(true);
    try { await post(`${base}/backups`); toastSuccess("Backup started"); await b.reload(); } catch (e) { toastError(e); } finally { setCreating(false); }
  };
  const setLock = async (x: Backup) => {
    try { await post(`/backups/${encodeURIComponent(x.id)}/lock`, { locked: !x.locked }); await b.reload(); } catch (e) { toastError(e); }
  };
  const deleteMany = async (list: Backup[]) => {
    toastSuccess(list.length === 1 ? "Deleting backup" : `Deleting ${list.length} backups`);
    try {
      for (const x of list) await del(`/backups/${encodeURIComponent(x.id)}`);
      toastSuccess(list.length === 1 ? "Backup deleted successfully" : "Backups deleted successfully");
    } catch (e) { toastError(e); }
    setSel(new Set());
    await b.reload();
  };
  const doRestore = async (x: Backup | undefined, when?: number) => {
    setRestoring(x ? x.id : "pitr");
    try {
      const r = await post<RestoreResponse>(`${base}/restore`, when ? { pointInTime: when } : { backupId: x!.id });
      setRestored(r);
      toastSuccess("Restore started");
    } finally { setRestoring(null); }
  };
  const setWal = async (on: boolean) => {
    await put(`${base}/backups/schedule`, { ...sched, continuousWal: on });
    toastSuccess(on ? "PITR enabled" : "PITR disabled");
    await b.reload();
  };

  return (
    <div className="mx-auto max-w-[799px] px-2 pb-16">
      {!health.configured && (
        <div className="mb-5 rounded-md bg-pink-50 p-3 text-body text-accent-text">
          No backup bucket is set, so new backups cannot be written. You can still review the list below.
          <div className="mt-2"><Link to="/settings/backups" className="underline underline-offset-2 hover:text-accent-muted">Set up a bucket</Link></div>
        </div>
      )}
      {health.stale && <div className="mb-5"><Notice tone="danger">{health.message || "The last good backup is older than 26 hours."}</Notice></div>}
      {restored && (
        <div className="mb-5">
          <Notice tone="ok" action={onOpenService ? <Button size="sm" onClick={() => onOpenService(restored.serviceId)}>Open the copy</Button> : undefined}>
            Restoring to a new service{restored.recoveredTo ? `, recovered to ${fmtDateTime(restored.recoveredTo)}` : ""}. Your current service is untouched.
          </Notice>
        </div>
      )}

      <div className="flex min-h-[34px] flex-wrap items-center justify-between gap-3">
        <div className="flex h-5 items-center gap-2 text-body">
          <Clock size={16} className="text-fg-3" />
          <span className="text-fg-4">{sched.enabled ? "Next backup" : "No backup schedule"}</span>
          {sched.enabled && next && <span className="font-medium text-foreground">{inText(next - Date.now())}</span>}
        </div>
        <div className="flex items-center gap-2">
          <Button onClick={() => setPromote(true)}>Promote a copy</Button>
          <Button onClick={() => setSchedule(true)}>{sched.enabled ? "Edit schedule" : "Configure schedule"}</Button>
          <Button variant="primary" icon={<Plus size={16} />} busy={creating} disabled={!health.configured} onClick={createBackup}
            title={health.configured ? undefined : "Set a backup bucket first"}>Create backup</Button>
        </div>
      </div>

      {isPg && <PitrCard sched={sched} health={health} onToggle={(m) => setPitrModal(m)} onRestore={() => setRestore("pitr")} />}

      <div className="mt-4 flex flex-col gap-2" aria-label="Backups">
        {backups.length === 0 ? (
          <div className="mt-20 flex flex-col items-center text-center">
            <History size={40} className="text-fg-3" />
            <div className="mt-4 text-lead font-semibold text-foreground">No Backups</div>
            <p className="mt-1 text-lead text-fg-2">This service's volume does not have any backups yet.</p>
          </div>
        ) : backups.map((x) => {
          const expanded = open.has(x.id);
          const selected = sel.has(x.id);
          const busyRow = restoring === x.id;
          const expires = x.locked ? "Never (locked)" : x.trigger === "schedule" ? fmtDateTime(x.startedAt + sched.retainDays * 86400000) : "Never";
          return (
            <div key={x.id} className={cx("group rounded-lg border transition-[border-color,background-color] duration-100 ease-[cubic-bezier(.4,0,.2,1)]",
              selected ? "border-accent" : "border-surface-hover")}>
              <div role="button" tabIndex={0} aria-expanded={expanded}
                onClick={(e) => { if (e.shiftKey || e.metaKey || sel.size > 0) { toggleSel(x.id); } else toggleOpen(x.id); }}
                onKeyDown={(e) => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); toggleOpen(x.id); } }}
                className={cx("flex min-h-[72px] cursor-pointer select-none items-center gap-4 rounded-md p-3 transition-colors duration-100 hover:bg-surface", busyRow && "opacity-70")}>
                <HardDrive size={20} className="ml-[3px] shrink-0 text-fg-4" />
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2 text-lead font-medium text-foreground" style={{ lineHeight: "24px" }}>
                    <span className="truncate">{backupName(x, cadence)}</span>
                    {x.locked && <Lock size={14} className="shrink-0 text-fg-3" aria-label="Locked" />}
                    {x.status === "running" && <Badge tone="info">Running</Badge>}
                    {x.status === "failed" && <Badge tone="danger">Failed</Badge>}
                  </div>
                  <div className="flex flex-wrap items-center gap-x-1.5 text-body text-fg-2">
                    <RelTime ts={x.startedAt} />
                    <span>{viaText(x, cadence)}</span>
                    <span aria-hidden>&middot;</span>
                    {busyRow ? <span>Restoring...</span> : x.status === "failed" ? <span className="text-danger-text">{x.error || "Backup failed"}</span> : <span className="tabular-nums">{fmtBytes(x.sizeBytes)}</span>}
                  </div>
                </div>
                {x.status === "ok" && (
                  <Tooltip content={<span className="block max-w-[260px] whitespace-normal">Restoring creates a new service from this backup. Your current service is not touched, and you review the copy before promoting it.</span>}>
                    <button type="button" disabled={restoring !== null}
                      onClick={(e) => { e.stopPropagation(); setRestore(x); }}
                      className="h-[34px] rounded-md border border-line px-3 text-body font-medium text-foreground opacity-0 transition-opacity duration-200 ease-[cubic-bezier(.4,0,.2,1)] hover:bg-surface-hover focus-visible:opacity-100 disabled:cursor-not-allowed disabled:opacity-0 group-hover:opacity-100 group-hover:disabled:opacity-40">
                      Restore
                    </button>
                  </Tooltip>
                )}
                <div onClick={(e) => e.stopPropagation()}>
                  <Menu align="right" trigger={(
                    <button type="button" aria-label="Backup options"
                      className="inline-flex h-8 w-8 items-center justify-center rounded-md text-fg-3 transition-colors hover:bg-surface-hover hover:text-foreground">
                      <MoreHorizontal size={16} />
                    </button>
                  )} items={[
                    { label: x.locked ? "Unlock backup" : "Lock backup", icon: x.locked ? <Unlock size={16} /> : <Lock size={16} />, onClick: () => void setLock(x) },
                    { label: "Delete backup", icon: <Trash2 size={16} />, danger: true, disabled: x.locked, onClick: () => setRemoving([x]) },
                  ]} />
                </div>
                <ChevronDown size={16} className={cx("shrink-0 text-fg-4 transition-transform duration-200", expanded && "rotate-180")} />
              </div>
              {expanded && (
                <dl className="grid animate-fade-in grid-cols-2 gap-x-6 gap-y-3 border-t border-surface-hover px-4 py-4 text-body sm:grid-cols-5">
                  <Detail k="Created" v={fmtDateTime(x.startedAt)} />
                  <Detail k="Schedule" v={x.trigger === "schedule" ? PRESETS[cadence].label : "Manual"} />
                  <Detail k="Expires" v={expires} />
                  <Detail k="Total size" v={fmtBytes(x.sizeBytes)} />
                  <Detail k="Verified" v={x.verifiedAt ? fmtDateTime(x.verifiedAt) : "Not yet"} />
                </dl>
              )}
            </div>
          );
        })}
      </div>

      {sel.size > 0 && (
        <div className="fixed bottom-8 left-1/2 z-30 flex -translate-x-1/2 animate-rise-in items-center gap-1 rounded-lg border border-line bg-surface-raised p-1.5 pl-4 shadow-popover">
          <span className="pr-3 text-body font-medium text-foreground tabular-nums">{plural(sel.size, "backup")}</span>
          <button type="button" title="Delete Backups" aria-label="Delete Backups" onClick={() => setRemoving(backups.filter((x) => sel.has(x.id)))}
            className="inline-flex h-8 w-8 items-center justify-center rounded-md text-danger-text transition-colors hover:bg-danger-bg"><Trash2 size={16} /></button>
          <button type="button" title="Dismiss selection" aria-label="Dismiss selection" onClick={() => setSel(new Set())}
            className="inline-flex h-8 w-8 items-center justify-center rounded-md text-fg-3 transition-colors hover:bg-surface-hover hover:text-foreground"><X size={16} /></button>
        </div>
      )}

      {restore && <RestoreModal backup={restore === "pitr" ? undefined : restore} latest={health.recoverableTo} engine={service.config.template || "database"}
        onClose={() => setRestore(null)} onGo={(when) => doRestore(restore === "pitr" ? undefined : restore, when)} />}
      {removing && (() => {
        const locked = removing.filter((x) => x.locked), gone = removing.filter((x) => !x.locked);
        return (
          <Confirm title={gone.length === 1 ? "Delete backup" : "Delete backups"} danger confirm="Delete forever"
            body={<>{gone.length === 0 ? "Locked backups cannot be deleted. Unlock them first." : <>This permanently removes {plural(gone.length, "backup")} from the bucket.{locked.length > 0 && ` ${plural(locked.length, "locked backup")} will be kept.`}</>}</>}
            onClose={() => setRemoving(null)} onConfirm={async () => { if (gone.length) await deleteMany(gone); }} />
        );
      })()}
      {promote && <PromoteModal eid={eid} service={service} onClose={() => setPromote(false)} />}
      {schedule && <ScheduleModal base={base} schedule={sched} onClose={() => setSchedule(false)} onSaved={() => void b.reload()} />}
      {pitrModal && (
        <Confirm title={pitrModal === "enable" ? "Enable PITR" : "Disable PITR"} danger={pitrModal === "disable"} confirm={pitrModal === "enable" ? "Enable PITR" : "Disable PITR"}
          body={pitrModal === "enable"
            ? "This turns on WAL archiving to your backup bucket. Once enabled, you can restore to any recent point in time. A restore creates a new copy, and your current data stays untouched."
            : "WAL archiving stops and point-in-time restore is no longer possible. Your volume backups are kept."}
          onClose={() => setPitrModal(null)} onConfirm={async () => { try { await setWal(pitrModal === "enable"); } catch (e) { toastError(e); throw e; } }} />
      )}
    </div>
  );
}

function Detail({ k, v }: { k: string; v: string }) {
  return <div><dt className="text-caption text-fg-3">{k}</dt><dd className="mt-0.5 text-foreground">{v}</dd></div>;
}

function PitrCard({ sched, health, onToggle, onRestore }: { sched: BackupSchedule; health: BackupsResponse["health"]; onToggle: (m: "enable" | "disable") => void; onRestore: () => void }) {
  const on = sched.continuousWal;
  const status = !health.configured ? { tone: "warn" as const, text: "Needs bucket" } : !on ? { tone: "neutral" as const, text: "Disabled" }
    : health.walArchiveOk === false ? { tone: "danger" as const, text: "Failed" } : health.walArchiveOk ? { tone: "ok" as const, text: "Archiving" } : { tone: "info" as const, text: "Verifying" };
  return (
    <div className="mt-4 rounded-lg border border-surface-hover p-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="min-w-0">
          <div className="flex items-center gap-2 text-lead font-medium text-foreground">Point-in-time recovery <Badge tone={status.tone} upper>{status.text}</Badge></div>
          <p className="mt-1 text-body text-fg-2">
            {on && health.recoverableTo ? <>WAL archiving is on. Recoverable to <span className="text-foreground">{fmtDateTime(health.recoverableTo)}</span>.</>
              : "Archive the write-ahead log to restore to any recent moment, not just to a backup."}
          </p>
          {health.walArchiveOk === false && health.message && <p className="mt-1 text-body text-danger-text">{health.message}</p>}
        </div>
        <div className="flex items-center gap-3">
          {on && <Button disabled={!health.configured} onClick={onRestore}>Restore to a point in time</Button>}
          <Switch checked={on} label="Point-in-time recovery" disabled={!health.configured} onChange={(v) => onToggle(v ? "enable" : "disable")} />
        </div>
      </div>
    </div>
  );
}

function toLocalInput(ts: number) {
  const d = new Date(ts - new Date(ts).getTimezoneOffset() * 60000);
  return d.toISOString().slice(0, 16);
}

function RestoreModal({ backup, latest, engine, onClose, onGo }: {
  backup?: Backup; latest?: number; engine: string; onClose: () => void; onGo: (when?: number) => Promise<void>;
}) {
  const [when, setWhen] = useState(toLocalInput(latest || Date.now()));
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const pitr = !backup;
  const go = async () => {
    setBusy(true); setErr(null);
    try { await onGo(pitr ? new Date(when).getTime() : undefined); onClose(); } catch (e) { setErr(e instanceof Error ? e.message : String(e)); setBusy(false); }
  };
  return (
    <Modal title={pitr ? "Restore to point in time" : "Restore backup"} bare width={548} onClose={onClose} className="focus-visible:shadow-none">
      <div className="p-6 text-foreground">
        <h2 className="pb-4 pr-8 text-h2" style={{ lineHeight: "30px" }}>{pitr ? "Restore to point in time" : "Restore backup"}</h2>
        <p className="text-lead" style={{ lineHeight: "24px" }}>
          {pitr ? `Restoring creates a new standalone ${engine} service recovered to the moment you pick. The current service is left untouched.`
            : "Restoring creates a new service with the data from this backup. Your current service keeps running and is not changed."}
        </p>
        <p className="mt-4 text-lead" style={{ lineHeight: "24px" }}>Nothing replaces your current data until you review the copy and promote it.</p>
        <div className="mt-5 text-body font-medium text-fg-2">{pitr ? "Restore target" : "Backup that will be restored"}</div>
        {pitr ? (
          <div className="mt-2"><Input type="datetime-local" aria-label="Restore target" value={when} onChange={(e) => setWhen(e.target.value)} /></div>
        ) : (
          <div className="mt-2 flex items-center gap-3 rounded-xl border border-surface-hover bg-surface-raised px-4 py-[10px]">
            <HardDrive size={20} className="text-fg-3" />
            <span className="text-body font-medium tabular-nums">{new Date(backup.startedAt).toISOString().slice(0, 16).replace("T", " ")}</span>
            <span className="ml-auto text-lead text-fg-3 tabular-nums">{fmtBytes(backup.sizeBytes)}</span>
          </div>
        )}
        {err && <ErrorLine error={err} className="mt-4" />}
        <div className="mt-6 flex justify-end gap-4">
          <Button size="lg" onClick={onClose}>Cancel</Button>
          <Button size="lg" variant="primary" busy={busy} onClick={go}>Restore</Button>
        </div>
      </div>
    </Modal>
  );
}

function ScheduleModal({ base, schedule, onClose, onSaved }: { base: string; schedule: BackupSchedule; onClose: () => void; onSaved: () => void }) {
  const [sel, setSel] = useState<Set<Cadence>>(new Set(schedule.enabled ? [cadenceOf(schedule.cron)] : []));
  const [cron, setCron] = useState(schedule.cron);
  const [tz, setTz] = useState(schedule.timezone || "UTC");
  const [adv, setAdv] = useState(false);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const parsed = parseCron(cron);
  const order: Cadence[] = ["daily", "weekly", "monthly"];
  const toggle = (c: Cadence, on: boolean) => {
    const n = new Set(sel);
    if (on) n.add(c); else n.delete(c);
    setSel(n);
    const top = order.find((o) => n.has(o));
    if (top) setCron(PRESETS[top].cron);
  };
  const save = async () => {
    setBusy(true); setErr(null);
    try {
      const days = sel.size ? Math.max(...[...sel].map((c) => PRESETS[c].days)) : schedule.retainDays;
      await put(`${base}/backups/schedule`, { enabled: sel.size > 0, cron, timezone: tz, retainDays: days, continuousWal: schedule.continuousWal } satisfies BackupSchedule);
      toastSuccess("Schedule saved");
      onSaved(); onClose();
    } catch (e) { setErr(e instanceof Error ? e.message : String(e)); toastError(e); setBusy(false); }
  };
  return (
    <Modal title="Configure backup schedule" description="Run volume backups on a schedule." width={520} onClose={onClose}
      footer={<><Button onClick={onClose}>Cancel</Button><Button variant="primary" busy={busy} disabled={sel.size > 0 && !parsed.ok} onClick={save}>Save schedule</Button></>}>
      <div className="flex flex-col gap-4 pb-2">
        <div>
          <div className="mb-2 text-body font-medium text-foreground">Available schedules</div>
          <div className="divide-y divide-surface-hover rounded-md border border-line">
            {order.map((c) => (
              <div key={c} className="px-3 py-3">
                <Checkbox checked={sel.has(c)} onChange={(v) => toggle(c, v)} label={PRESETS[c].label} description={PRESETS[c].note} />
              </div>
            ))}
          </div>
        </div>
        {sel.size === 0 && <Notice tone="warn">No schedules selected. Scheduled backups will be turned off.</Notice>}
        {sel.size > 1 && <p className="text-caption text-fg-3">Backups run on the most frequent schedule you picked and are kept for the longest retention.</p>}
        <button type="button" onClick={() => setAdv(!adv)} className="flex items-center gap-1 self-start text-body text-fg-2 hover:text-foreground">
          <ChevronDown size={14} className={cx("transition-transform", !adv && "-rotate-90")} />Advanced
        </button>
        {adv && (
          <div className="flex flex-col gap-3">
            <Field label="Cron expression" error={!parsed.ok ? parsed.error : undefined}><Input mono value={cron} onChange={(e) => setCron(e.target.value)} aria-invalid={!parsed.ok} /></Field>
            <Field label="Time zone"><Input value={tz} onChange={(e) => setTz(e.target.value)} /></Field>
          </div>
        )}
        {err && <ErrorLine error={err} />}
      </div>
    </Modal>
  );
}

function PromoteModal({ eid, service, onClose }: { eid: string; service: Service; onClose: () => void }) {
  const canvas = useLoad(() => get<{ services: Service[] }>(`/environments/${encodeURIComponent(eid)}/canvas`), [eid]);
  const [pick, setPick] = useState("");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const copies = (canvas.data?.services || []).filter((s) => s.id !== service.id && s.config.name.startsWith(`${service.config.name}-restored-`));
  return (
    <Modal title="Promote a restored copy" width={520} onClose={onClose}
      footer={<><Button onClick={onClose}>Cancel</Button>
        <Button variant="danger" busy={busy} disabled={!pick} onClick={async () => {
          setBusy(true); setErr(null);
          try { await post(`${svcPath(eid, service.id)}/promote`, { restoredServiceId: pick }); toastSuccess("Copy promoted"); onClose(); } catch (e) { setErr(e instanceof Error ? e.message : String(e)); setBusy(false); }
        }}>Promote</Button></>}>
      <p className="mb-4 leading-6">The copy takes the name <b className="text-foreground">{service.config.name}</b>, so references resolve to it. This service is renamed and stopped, and services that use it redeploy.</p>
      {copies.length === 0 ? <div className="rounded-md border border-dashed border-line p-4 text-center text-body text-fg-3">No restored copies of this service in this environment.</div> : (
        <div className="flex flex-col gap-2">
          {copies.map((c) => (
            <label key={c.id} className={cx("flex cursor-pointer items-center gap-3 rounded-md border p-3 transition-colors", pick === c.id ? "border-accent bg-surface" : "border-line hover:bg-surface")}>
              <input type="radio" name="copy" checked={pick === c.id} onChange={() => setPick(c.id)} className="accent-[var(--pink-600,#ef6aae)]" />
              <span className="font-mono text-body text-foreground">{c.config.name}</span>
            </label>
          ))}
        </div>
      )}
      {err && <ErrorLine error={err} className="mt-4" />}
    </Modal>
  );
}
