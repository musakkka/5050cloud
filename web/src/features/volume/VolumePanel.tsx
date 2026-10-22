// SPDX-License-Identifier: AGPL-3.0-only

// Volume panel (docs/SPEC.md 9.12, 13.1 /project/:pid/volume/:vid):
// usage, name, mount path (staged), alert threshold, Wipe Volume and delete.

import { useState } from "react";
import { HardDrive, X } from "lucide-react";
import type { Volume, VolumeDetail } from "../../api/types.gen";
import { del, get, patch, post, svcPath } from "../service/client";
import { Button, Confirm, ErrorLine, Input, Loading, Modal, NotInV01, Section, When, fmtBytes, useAction, useLoad } from "../service/ui";

export default function VolumePanel({ eid, serviceId, serviceName, onClose }: { eid: string; serviceId: string; serviceName?: string; onClose?: () => void }) {
  const base = `${svcPath(eid, serviceId)}/volume`;
  const v = useLoad(() => get<VolumeDetail>(base), [base], 30000);
  const [wipe, setWipe] = useState(false);
  const [remove, setRemove] = useState(false);
  const a = useAction();
  if (!v.data) return v.error ? <div className="p-6"><ErrorLine>{v.error}</ErrorLine></div> : <Loading />;
  const d = v.data;
  const usedPct = d.hostTotalBytes ? (d.usedBytes / d.hostTotalBytes) * 100 : 0;
  const hostUsedPct = d.hostTotalBytes ? ((d.hostTotalBytes - d.hostFreeBytes) / d.hostTotalBytes) * 100 : 0;
  const save = (body: Record<string, unknown>) => a.run(async () => v.setData({ ...d, ...(await patch<Volume>(base, body)) }));

  return (
    <div className="p-4 sm:p-8">
      <div className="mb-6 flex items-center gap-3">
        <HardDrive size={24} />
        <h2 className="flex-1 truncate text-2xl font-semibold">{d.name}</h2>
        {onClose && <button aria-label="Close" onClick={onClose} className="text-gray-500 hover:text-foreground"><X size={20} /></button>}
      </div>
      {d.hostLost && <div className="mb-4 rounded-md border border-red-300 bg-red-100 px-4 py-2 text-sm text-red-700">The host this volume was on is gone. Restore it from a backup.</div>}
      {d.staged && <div className="mb-4 text-sm text-purple-500">Staged: {d.staged}. Deploy to apply.</div>}
      <Section title="Usage">
        <div className="mb-1 flex justify-between text-sm"><span>{fmtBytes(d.usedBytes)} used</span><span className="text-gray-500">{fmtBytes(d.hostFreeBytes)} free on {d.hostName}</span></div>
        <div className="relative h-2 overflow-hidden rounded bg-gray-200" aria-label="Volume usage">
          <div className="absolute inset-y-0 left-0 bg-gray-400" style={{ width: `${hostUsedPct}%` }} />
          <div className="absolute inset-y-0 left-0 bg-pink-500" style={{ width: `${usedPct}%` }} />
        </div>
        <div className="mt-1 text-xs text-gray-500">Volumes share the host's disk. An alert fires at {d.alertAtPct}% of it.</div>
      </Section>
      <Section title="Settings">
        <div className="flex flex-col gap-3 text-sm">
          <EditField label="Name" value={d.name} onSave={(x) => save({ name: x })} />
          <EditField label="Mount path (staged)" value={d.mountPath} onSave={(x) => save({ mountPath: x })} validate={(x) => (x.startsWith("/") ? null : "Use an absolute path, for example /data")} />
          <EditField label="Alert at (% of host disk)" value={String(d.alertAtPct)} onSave={(x) => save({ alertAtPct: Number(x) })} validate={(x) => (Number(x) >= 1 && Number(x) <= 100 ? null : "1 to 100")} />
          <div className="text-gray-600">Attached to {serviceName || "its service"} on {d.hostName}</div>
          <NotInV01 label="Live Resize" instead="volumes grow until the host disk is full" />
        </div>
        <ErrorLine>{a.error}</ErrorLine>
      </Section>
      {d.backups && (
        <Section title="Backups">
          <div className="text-sm text-gray-600">
            {!d.backups.configured ? "No backup bucket is set." : d.backups.lastOk ? <>Last good backup <When ts={d.backups.lastOk} /></> : "No backup yet."}
            {d.backups.message && <div className="text-red-600">{d.backups.message}</div>}
          </div>
        </Section>
      )}
      <Section title="Danger">
        <div className="flex flex-wrap gap-2">
          <Button variant="danger" onClick={() => setWipe(true)}>Wipe Volume</Button>
          <Button variant="danger" onClick={() => setRemove(true)}>Delete volume</Button>
        </div>
      </Section>
      {wipe && <Confirm title="Wipe Volume" danger confirm="Wipe" typeName={d.name}
        body="Deletes every file on this volume now and restarts the service. This cannot be undone."
        onClose={() => setWipe(false)} onConfirm={async () => { await post(`${base}/wipe`, { confirm: d.name }); await v.reload(); }} />}
      {remove && <Confirm title="Delete volume" danger confirm="Delete" typeName={d.name}
        body="Stages deletion of this volume and its data. It happens when you deploy."
        onClose={() => setRemove(false)} onConfirm={async () => { await del(base, { confirm: d.name }); await v.reload(); }} />}
    </div>
  );
}

function EditField({ label, value, onSave, validate }: { label: string; value: string; onSave: (v: string) => Promise<unknown>; validate?: (v: string) => string | null }) {
  const [t, setT] = useState(value);
  const err = validate ? validate(t) : null;
  return (
    <label className="flex flex-col gap-1">{label}
      <Input value={t} onChange={(e) => setT(e.target.value)} onBlur={() => { if (!err && t !== value) void onSave(t); }}
        onKeyDown={(e) => { if (e.key === "Enter") (e.target as HTMLInputElement).blur(); }} />
      {err && <span className="text-xs text-red-600">{err}</span>}
    </label>
  );
}

/** Attach Volume: asks for the mount path; the volume is staged (9.12). */
export function AttachVolumeModal({ eid, serviceId, onClose, onDone }: { eid: string; serviceId: string; onClose: () => void; onDone?: (v: Volume) => void }) {
  const [mountPath, setMountPath] = useState("/data");
  const [name, setName] = useState("");
  const a = useAction();
  const ok = mountPath.startsWith("/");
  return (
    <Modal title="Attach Volume" onClose={onClose} footer={<>
      <Button onClick={onClose}>Cancel</Button>
      <Button variant="primary" disabled={!ok || a.busy} onClick={async () => {
        if (await a.run(async () => { const v = await post<Volume>(`${svcPath(eid, serviceId)}/volume`, { mountPath, name: name || undefined }); onDone?.(v); })) onClose();
      }}>Attach</Button>
    </>}>
      <label className="mb-3 block">Mount path<Input value={mountPath} onChange={(e) => setMountPath(e.target.value)} autoFocus /></label>
      <label className="block">Name (optional)<Input value={name} onChange={(e) => setName(e.target.value)} /></label>
      <p className="mt-2 text-xs text-gray-500">A service with a volume runs one replica, and its deploys stop the old container before starting the new one.</p>
      <ErrorLine>{a.error}</ErrorLine>
    </Modal>
  );
}
