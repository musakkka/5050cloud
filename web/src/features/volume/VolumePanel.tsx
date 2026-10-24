// SPDX-License-Identifier: AGPL-3.0-only

// Volume panel (docs/SPEC.md 9.12, 13.1 /project/:pid/volume/:vid): a pane
// with Metrics (usage against the host disk, alert line, backups) and
// Settings (mount path, name, size, host, usage alert, Wipe Volume, delete).

import { useState, type ReactNode } from "react";
import { AlertTriangle, Bell, Flame, HardDrive, Info, Plug, Server, Target, X } from "lucide-react";
import type { Service, Volume, VolumeDetail } from "../../api/types.gen";
import { del, get, patch, post, svcPath } from "../service/client";
import { useLoad } from "../service/ui";
import { Button, Confirm, Modal, RelTime, Skeleton, Tooltip, cx, fmtBytes, toastError, toastSuccess } from "../../kit";
import { FilterProvider, SaveInput, SettingField, SettingsSection, Timeline, ToggleRow, type SectionDef } from "../service-settings/ui";

// `npm run dev:mocks`: stateful volume fixtures live with the settings mocks.

type Tab = "metrics" | "settings";

export default function VolumePanel({ eid, serviceId, serviceName, onClose, initialTab = "settings" }: {
  eid: string; serviceId: string; serviceName?: string; onClose?: () => void; initialTab?: Tab;
}) {
  const base = `${svcPath(eid, serviceId)}/volume`;
  const v = useLoad(() => get<VolumeDetail>(base), [base], 30000);
  const svc = useLoad(() => get<Service>(svcPath(eid, serviceId)).catch(() => undefined), [eid, serviceId]);
  const [tab, setTab] = useState<Tab>(initialTab);
  const d = v.data;

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="shrink-0 px-6 pt-12 sm:px-12">
        <div className="flex items-center gap-4">
          <HardDrive size={32} strokeWidth={1.5} className="shrink-0 text-foreground" />
          {d ? <h2 className="min-w-0 flex-1 truncate text-2xl font-semibold leading-[33px]">{d.name}</h2> : <div className="flex-1"><Skeleton w={200} h={26} /></div>}
          {onClose && (
            <button type="button" aria-label="Close" onClick={onClose}
              className="inline-flex h-8 w-8 items-center justify-center rounded-md text-fg-2 transition-colors hover:bg-surface-hover hover:text-foreground"><X size={18} /></button>
          )}
        </div>
        <div role="tablist" className="mt-8 flex gap-8">
          {(["metrics", "settings"] as const).map((t) => (
            <button key={t} type="button" role="tab" aria-selected={tab === t} onClick={() => setTab(t)}
              className={cx("relative pb-3 text-base capitalize transition-colors duration-150", tab === t ? "text-foreground" : "text-fg-3 hover:text-fg-2")}>
              {t}
              {tab === t && <span className="absolute inset-x-0 -bottom-px h-0.5 rounded-full bg-foreground" />}
            </button>
          ))}
        </div>
      </div>
      <div className="border-b border-line" />
      <div className="min-h-0 flex-1 overflow-y-auto scroll-thin px-6 py-8 sm:pl-7 sm:pr-12">
        {!d ? (v.error ? <div role="alert" className="text-sm text-danger-text">{v.error}</div> : <PaneSkeleton />) : (
          <>
            {d.hostLost && (
              <div role="alert" className="mb-6 flex items-center gap-3 rounded-md bg-danger-bg px-4 py-3 text-sm text-danger-text">
                <AlertTriangle size={16} className="shrink-0" />The host this volume was on is gone. Restore it from a backup.
              </div>
            )}
            {d.staged && (
              <div className="mb-6 flex items-center gap-3 rounded-md bg-pink-50 px-4 py-3 text-sm text-pink-700">
                <Info size={16} className="shrink-0" />
                {d.staged === "delete" ? "Deletion is staged and happens on the next deploy." : d.staged === "create" ? "This volume is staged and is created on the next deploy." : "A change to this volume is staged and applies on the next deploy."}
              </div>
            )}
            {tab === "metrics" ? <Usage d={d} /> : (
              <VolumeSettings d={d} base={base} serviceName={serviceName || svc.data?.config.name}
                required={!!svc.data && (svc.data.kind === "database" || !!svc.data.config.template)}
                onVolume={(x) => v.setData({ ...d, ...x })} reload={() => void v.reload()} />
            )}
          </>
        )}
      </div>
    </div>
  );
}

function PaneSkeleton() {
  return (
    <div aria-busy className="flex flex-col gap-12">
      {[0, 1].map((i) => (
        <div key={i} className="flex gap-[22px]">
          <Skeleton circle w={40} h={40} />
          <div className="flex flex-1 flex-col gap-3 pt-2"><Skeleton w={140} h={20} /><Skeleton w={300} h={14} className="mt-4" /><Skeleton w={536} h={42} className="max-w-full" /></div>
        </div>
      ))}
    </div>
  );
}

/* ---------- metrics ---------- */

function Usage({ d }: { d: VolumeDetail }) {
  const total = d.hostTotalBytes || 0;
  const hostUsed = total ? total - d.hostFreeBytes : 0;
  const usedPct = total ? (d.usedBytes / total) * 100 : 0;
  const otherPct = total ? Math.max(0, ((hostUsed - d.usedBytes) / total) * 100) : 0;
  return (
    <div className="flex flex-col gap-4">
      <div className="rounded-lg border border-line p-4 sm:p-6">
        <div className="flex items-baseline justify-between gap-4">
          <h3 className="text-lg font-semibold text-foreground">Volume Usage</h3>
          <span className="text-sm text-fg-3">on {d.hostName}</span>
        </div>
        <div className="mt-6 flex items-baseline gap-2">
          <span className="text-[32px] font-semibold leading-none tracking-tight text-foreground">{fmtBytes(d.usedBytes, 2)}</span>
          <span className="text-sm text-fg-2">used{total ? ` of ${fmtBytes(total, 0)} host disk` : ""}</span>
        </div>
        <div className="relative mt-6 h-3 overflow-hidden rounded-full bg-surface-hover" role="meter" aria-label="Volume usage" aria-valuenow={Math.round(usedPct)} aria-valuemin={0} aria-valuemax={100}>
          <div className="absolute inset-y-0 left-0 bg-pink-500 transition-[width] duration-500" style={{ width: `${usedPct}%` }} />
          <div className="absolute inset-y-0 bg-fg-5 transition-[width] duration-500" style={{ left: `${usedPct}%`, width: `${otherPct}%` }} />
          <Tooltip content={`Alert at ${d.alertAtPct}% of the host disk`}>
            <span tabIndex={0} className="absolute inset-y-0 w-0.5 bg-warn" style={{ left: `calc(${Math.min(d.alertAtPct, 100)}% - 1px)` }} />
          </Tooltip>
        </div>
        <div className="mt-3 flex flex-wrap gap-x-5 gap-y-1 text-caption text-fg-2">
          <Legend cls="bg-pink-500">This volume {fmtBytes(d.usedBytes)}</Legend>
          <Legend cls="bg-fg-5">Other data on the host {fmtBytes(Math.max(0, hostUsed - d.usedBytes))}</Legend>
          <Legend cls="bg-surface-hover border border-line">Free {fmtBytes(d.hostFreeBytes)}</Legend>
          <Legend cls="bg-warn">Alert at {d.alertAtPct}%</Legend>
        </div>
      </div>
      {d.backups && (
        <div className="flex items-center gap-3 rounded-lg border border-line px-4 py-3 text-sm">
          <span className={cx("h-2 w-2 shrink-0 rounded-full", !d.backups.configured ? "bg-fg-5" : d.backups.stale || d.backups.lastError ? "bg-warn" : "bg-ok")} />
          <span className="flex-1 text-fg-2">
            {!d.backups.configured ? "No backup bucket is set for this install." : d.backups.lastOk ? <>Last good backup <RelTime ts={d.backups.lastOk} className="text-foreground" /></> : "No backup yet."}
            {d.backups.message && <span className="block text-warn">{d.backups.message}</span>}
          </span>
        </div>
      )}
    </div>
  );
}

function Legend({ cls, children }: { cls: string; children: ReactNode }) {
  return <span className="inline-flex items-center gap-1.5"><span className={cx("h-2 w-2 rounded-sm", cls)} />{children}</span>;
}

/* ---------- settings ---------- */

const SECTIONS: Record<string, SectionDef> = {
  connection: { id: "connection", label: "Connection", icon: <Plug /> },
  size: { id: "size", label: "Size", icon: <Target /> },
  host: { id: "host", label: "Host", icon: <Server /> },
  alerts: { id: "alerts", label: "Alerts", icon: <Bell /> },
  wipe: { id: "wipe", label: "Wipe", icon: <Flame />, danger: true },
};

function VolumeSettings({ d, base, serviceName, required, onVolume, reload }: {
  d: VolumeDetail; base: string; serviceName?: string; required: boolean; onVolume: (v: Partial<VolumeDetail>) => void; reload: () => void;
}) {
  const [wipe, setWipe] = useState(false);
  const [remove, setRemove] = useState(false);
  const save = async (body: Record<string, unknown>) => {
    try { onVolume(await patch<Volume>(base, body)); } catch (e) { toastError(e); throw e; }
  };
  const alertsOn = d.alertAtPct > 0 && d.alertAtPct < 100;
  return (
    <FilterProvider value={{ q: "", sectionMatch: false }}>
      <Timeline>
        <SettingsSection prefix="volume" def={SECTIONS.connection}>
          <SettingField title="Mount Path" staged={d.staged === "update"}
            description={<>Directory this volume is mounted at in <b className="font-semibold text-foreground">{serviceName || "its service"}</b>. Changing it is staged and restarts the service on deploy.</>}>
            <SaveInput label="Mount path" mono value={d.mountPath} placeholder="/data"
              validate={(x) => (x.startsWith("/") && !/\s/.test(x) ? null : "Use an absolute path, for example /data")} onSave={(x) => save({ mountPath: x })} />
          </SettingField>
          <SettingField title="Volume Name" description="Shown on the canvas card and in backups.">
            <SaveInput label="Volume name" value={d.name} validate={(x) => (/^[a-zA-Z0-9][a-zA-Z0-9 _-]{0,31}$/.test(x) ? null : "1 to 32 letters, digits, spaces, dashes or underscores")} onSave={(x) => save({ name: x })} />
          </SettingField>
        </SettingsSection>

        <SettingsSection prefix="volume" def={SECTIONS.size}>
          <SettingField title="Volume Size" description="Volumes share their host's disk and grow as data is written, up to the free space on the host.">
            <div className="flex gap-4">
              <div className="flex h-11 min-w-0 flex-1 items-center gap-2 rounded-md bg-surface-hover px-4 text-base text-foreground">
                {fmtBytes(d.usedBytes + d.hostFreeBytes, 2)}<span className="truncate text-sm text-fg-3">available, {fmtBytes(d.usedBytes, 2)} used</span>
              </div>
              <Tooltip content="Not in v0.1: volumes grow until the host disk is full">
                <span tabIndex={0} className="inline-flex rounded-md"><Button size="lg" disabled>Live resize</Button></span>
              </Tooltip>
            </div>
          </SettingField>
        </SettingsSection>

        <SettingsSection prefix="volume" def={SECTIONS.host}>
          <SettingField title="Volume Host" description="The host that stores this volume. The service always runs on it.">
            <div className="flex h-11 items-center gap-3 rounded-md bg-surface-hover px-4 text-base text-foreground">
              <Server size={16} className="text-fg-3" />{d.hostName}
            </div>
          </SettingField>
        </SettingsSection>

        <SettingsSection prefix="volume" def={SECTIONS.alerts}>
          <SettingField title="Usage Alert" description="When the host disk passes this share, the project's members get an email and its webhooks are called.">
            <ToggleRow checked={alertsOn} label="Enable alerts" onChange={(on) => void save({ alertAtPct: on ? 80 : 100 }).catch(() => undefined)} />
            {alertsOn && (
              <SaveInput className="mt-2" label="Alert threshold" type="number" value={String(d.alertAtPct)} suffix="% of host disk"
                validate={(x) => (Number(x) >= 1 && Number(x) <= 99 ? null : "1 to 99")} onSave={(x) => save({ alertAtPct: Number(x) })} />
            )}
          </SettingField>
        </SettingsSection>

        <SettingsSection prefix="volume" def={SECTIONS.wipe}>
          <SettingField danger title="Wipe Volume"
            description={<>Wiping deletes <b>all data</b> on this volume in this environment right away and restarts the service. <b>Backups</b> are kept.</>}>
            <Button variant="danger" className="font-medium" onClick={() => setWipe(true)}>Wipe volume</Button>
          </SettingField>
          <SettingField danger title="Delete Volume"
            description={<>Deleting the volume <b>permanently deletes</b> all data in it and stops the service's deploys until a new volume is attached.</>}>
            {required && (
              <div className="mb-4 flex items-start gap-3 rounded-md bg-blue-50 px-4 py-3 text-sm leading-6 text-blue-600">
                <Info size={16} className="mt-1 shrink-0" />
                The database this volume belongs to needs it. To remove the volume, delete the service.
              </div>
            )}
            <Button variant="danger" className="font-medium" disabled={required || d.staged === "delete"} onClick={() => setRemove(true)}>Delete Volume</Button>
          </SettingField>
        </SettingsSection>
      </Timeline>

      {wipe && <Confirm title="Wipe Volume" danger confirm="Wipe" typeName={d.name}
        body="Deletes every file on this volume now and restarts the service. This cannot be undone."
        onClose={() => setWipe(false)} onConfirm={async () => { await post(`${base}/wipe`, { confirm: d.name }); toastSuccess("Volume wiped"); reload(); }} />}
      {remove && <Confirm title="Delete Volume" danger confirm="Delete" typeName={d.name}
        body="Stages deletion of this volume and its data. It happens when you deploy."
        onClose={() => setRemove(false)} onConfirm={async () => { await del(base, { confirm: d.name }); reload(); }} />}
    </FilterProvider>
  );
}

/** Attach Volume: asks for the mount path; the volume is staged (9.12). */
export function AttachVolumeModal({ eid, serviceId, onClose, onDone }: { eid: string; serviceId: string; onClose: () => void; onDone?: (v: Volume) => void }) {
  const [mountPath, setMountPath] = useState("/data");
  const [name, setName] = useState("");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const ok = mountPath.startsWith("/") && !/\s/.test(mountPath);
  const submit = async () => {
    if (!ok) return;
    setBusy(true); setErr(null);
    try { const v = await post<Volume>(`${svcPath(eid, serviceId)}/volume`, { mountPath, name: name || undefined }); onDone?.(v); onClose(); } catch (e) { setErr((e as Error).message); setBusy(false); }
  };
  const field = "h-[42px] w-full rounded-md border border-line bg-transparent px-3 text-base text-foreground outline-none transition-colors focus:border-accent-muted focus:shadow-none";
  return (
    <Modal title="Attach Volume" onClose={onClose} footer={<>
      <Button onClick={onClose}>Cancel</Button>
      <Button variant="primary" busy={busy} disabled={!ok} onClick={() => void submit()}>Attach</Button>
    </>}>
      <form className="flex flex-col gap-4" onSubmit={(e) => { e.preventDefault(); void submit(); }}>
        <label className="block space-y-2">
          <span className="block text-caption font-semibold text-fg-2">Mount path</span>
          <input autoFocus value={mountPath} onChange={(e) => setMountPath(e.target.value)} className={cx(field, "font-mono")} aria-invalid={!ok || undefined} />
          {!ok && <span className="block text-caption text-danger-text">Use an absolute path, for example /data</span>}
        </label>
        <label className="block space-y-2">
          <span className="block text-caption font-semibold text-fg-2">Name <span className="font-normal text-fg-4">(optional)</span></span>
          <input value={name} onChange={(e) => setName(e.target.value)} className={field} />
        </label>
        <p className="text-caption text-fg-3">A service with a volume runs one replica, and its deploys stop the old container before starting the new one.</p>
        {err && <div role="alert" className="text-sm text-danger-text">{err}</div>}
      </form>
    </Modal>
  );
}
