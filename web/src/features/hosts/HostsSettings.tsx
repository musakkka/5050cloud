// SPDX-License-Identifier: AGPL-3.0-only

import { useEffect, useState } from "react";
import type * as T from "../../api/types.gen";
import { Badge, Button, Confirm, Empty, Menu, Modal, fmtBytes } from "../../ui/kit";
import { v1 } from "../settings/client";
import { CopyField, Field, Input, Loading, Notice, Section, Toggle, fmtDate, optimistic, useAction, useLoad } from "../settings/ui";

export const ROLES = ["control", "build", "ingress", "app", "data"];

export function toggleRole(roles: string[], role: string, on: boolean): string[] {
  const next = on ? [...new Set([...roles, role])] : roles.filter((r) => r !== role);
  return ROLES.filter((r) => next.includes(r));
}

const pct = (used: number, total: number) => (total > 0 ? Math.round((used / total) * 100) : 0);

export default function HostsSettings() {
  const { data, error, setData, reload } = useLoad(v1.hosts, []);
  const [adding, setAdding] = useState(false);
  const [edit, setEdit] = useState<T.Host | null>(null);
  const [remove, setRemove] = useState<T.Host | null>(null);
  const [err, setErr] = useState<string | null>(null);
  useEffect(() => { const t = setInterval(reload, 15000); return () => clearInterval(t); }, [reload]);
  if (!data) return <Loading error={error} />;
  const replace = (h: T.Host) => setData(data.map((x) => (x.id === h.id ? h : x)));
  const act = async (h: T.Host, patch: Partial<T.Host>, send: () => Promise<T.Host>) => {
    const cur = data;
    await optimistic(cur, data.map((x) => (x.id === h.id ? { ...x, ...patch } : x)), setData, async () => replace(await send()), setErr);
  };
  return (
    <Section title="Hosts" description="Every machine in this install." actions={<Button variant="primary" onClick={() => setAdding(true)}>Add host</Button>}>
      {err && <Notice kind="error">{err}</Notice>}
      {data.length === 0 ? <Empty>No hosts.</Empty> : data.map((h) => (
        <div key={h.id} className="rounded-xl border border-gray-200 p-4 mb-3 text-sm">
          <div className="flex items-start gap-3">
            <div className="flex-1 min-w-0">
              <div className="flex items-center gap-2 flex-wrap">
                <span className="font-semibold">{h.name}</span>
                <Badge kind={h.state === "online" ? "online" : h.state === "offline" || h.state === "lost" ? "crashed" : "deploying"}>{h.state}</Badge>
                {h.roles.map((r) => <span key={r} className="text-xs rounded border border-gray-200 px-1.5 py-0.5">{r}</span>)}
              </div>
              <div className="text-xs text-gray-500 mt-1">
                {h.publicIp} · {h.os} {h.arch} · {h.version} · {h.replicas} replica{h.replicas === 1 ? "" : "s"} · seen {fmtDate(h.lastSeen)}
              </div>
            </div>
            <Menu trigger={<Button variant="ghost" aria-label={`Actions for ${h.name}`}>Manage</Button>} items={[
              { label: "Edit name and roles", onClick: () => setEdit(h) },
              { label: h.state === "maintenance" ? "End maintenance" : "Start maintenance", onClick: () => act(h, { state: h.state === "maintenance" ? "online" : "maintenance" }, () => v1.maintenance(h.id, h.state !== "maintenance")) },
              { label: h.state === "draining" ? "Stop draining" : "Drain", onClick: () => act(h, { state: h.state === "draining" ? "online" : "draining" }, () => v1.updateHost(h.id, { drain: h.state !== "draining" })) },
              ...(h.state === "unknown" ? [{ label: "Adopt again", onClick: () => act(h, {}, () => v1.readopt(h.id)) }] : []),
              { label: "Remove host", danger: true, onClick: () => setRemove(h) },
            ]} />
          </div>
          <div className="grid grid-cols-1 md:grid-cols-3 gap-3 mt-3">
            <Bar label="CPU" used={h.cpuUsed} total={h.cpus} text={`${h.cpuUsed.toFixed(1)} of ${h.cpus} vCPU`} />
            <Bar label="Memory" used={h.memUsed} total={h.memBytes} text={`${fmtBytes(h.memUsed)} of ${fmtBytes(h.memBytes)}, ${h.reservedMb} MB reserved`} />
            {h.disks.map((d) => <Bar key={d.mount} label={`Disk ${d.mount}`} used={d.used} total={d.total} text={`${fmtBytes(d.used)} of ${fmtBytes(d.total)}`} />)}
          </div>
          {h.rebootRequired && <div className="mt-3"><Notice>A kernel update waits for a reboot. Start maintenance, then reboot the machine.</Notice></div>}
          {h.warnings?.map((w) => <div key={w} className="mt-2"><Notice>{w}</Notice></div>)}
          {!!h.unreachable?.length && <div className="mt-2"><Notice kind="error">Cannot reach {h.unreachable.map((id) => data.find((x) => x.id === id)?.name || id).join(", ")} over the private network.</Notice></div>}
          {!!h.conflicts?.length && (
            <div className="mt-2">
              <Notice kind="error">
                {h.conflicts.length} other machine{h.conflicts.length === 1 ? "" : "s"} presented this host's identity: {h.conflicts.map((c) => `${c.ip} (seen ${fmtDate(c.seenAt)})`).join(", ")}.
              </Notice>
              <Button onClick={() => act(h, { conflicts: [] }, () => v1.clearConflicts(h.id))}>Clear conflicts</Button>
            </div>
          )}
        </div>
      ))}
      {adding && <AddHost onClose={() => { setAdding(false); reload(); }} />}
      {edit && <EditHost host={edit} onClose={() => setEdit(null)} onSaved={(h) => { replace(h); setEdit(null); }} />}
      {remove && <RemoveHost host={remove} onClose={() => setRemove(null)} onDone={() => setData(data.filter((x) => x.id !== remove.id))} />}
    </Section>
  );
}

function Bar({ label, used, total, text }: { label: string; used: number; total: number; text: string }) {
  const p = pct(used, total);
  return (
    <div>
      <div className="flex justify-between text-xs text-gray-600 mb-1"><span>{label}</span><span>{p}%</span></div>
      <div className="h-1.5 rounded bg-gray-100 overflow-hidden"><div className={p >= 90 ? "h-full bg-red-500" : p >= 75 ? "h-full bg-yellow-500" : "h-full bg-green-500"} style={{ width: `${Math.min(100, p)}%` }} /></div>
      <div className="text-xs text-gray-500 mt-1">{text}</div>
    </div>
  );
}

function AddHost({ onClose }: { onClose: () => void }) {
  const [roles, setRoles] = useState<string[]>(["app"]);
  const [name, setName] = useState("");
  const [tok, setTok] = useState<T.JoinToken | null>(null);
  const a = useAction();
  return (
    <Modal title="Add host" onClose={onClose} width={620} footer={tok ? <Button onClick={onClose}>Done</Button> : <>
      <Button onClick={onClose}>Cancel</Button>
      <Button variant="primary" disabled={!roles.length || a.busy} onClick={async () => { const r = await a.run(() => v1.joinToken({ roles, name: name.trim() || undefined })); if (r) setTok(r); }}>Create join command</Button>
    </>}>
      {tok ? (
        <>
          <p className="mb-2">Run this on the new machine as root. It works once and expires {fmtDate(tok.expiresAt)}.</p>
          <CopyField value={tok.command} />
          <p className="mt-3 text-xs text-gray-500">The host appears here when it connects.</p>
        </>
      ) : (
        <>
          <Field label="Name" hint="Optional. The machine's host name is used when empty."><Input value={name} onChange={(e) => setName(e.target.value)} /></Field>
          <div className="text-sm font-medium mb-2">Roles</div>
          {ROLES.filter((r) => r !== "control").map((r) => <Toggle key={r} label={r} checked={roles.includes(r)} onChange={(on) => setRoles(toggleRole(roles, r, on))} />)}
          {a.error && <Notice kind="error">{a.error}</Notice>}
        </>
      )}
    </Modal>
  );
}

function EditHost({ host, onClose, onSaved }: { host: T.Host; onClose: () => void; onSaved: (h: T.Host) => void }) {
  const [name, setName] = useState(host.name);
  const [roles, setRoles] = useState(host.roles);
  const [reserve, setReserve] = useState(String(host.reserveMb));
  const a = useAction();
  const reserveOk = /^\d+$/.test(reserve);
  return (
    <Modal title={`Edit ${host.name}`} onClose={onClose} footer={<>
      <Button onClick={onClose}>Cancel</Button>
      <Button variant="primary" disabled={!name.trim() || !roles.length || !reserveOk || a.busy} onClick={async () => {
        const r = await a.run(() => v1.updateHost(host.id, { name: name.trim(), roles, reserveMb: Number(reserve) }));
        if (r) onSaved(r);
      }}>Save</Button>
    </>}>
      <Field label="Name"><Input value={name} onChange={(e) => setName(e.target.value)} /></Field>
      <Field label="Memory kept free for the system (MB)" error={reserveOk ? null : "Enter a whole number."}><Input inputMode="numeric" value={reserve} onChange={(e) => setReserve(e.target.value)} /></Field>
      <div className="text-sm font-medium mb-2">Roles</div>
      {ROLES.map((r) => <Toggle key={r} label={r} disabled={r === "control"} checked={roles.includes(r)} onChange={(on) => setRoles(toggleRole(roles, r, on))} />)}
      {a.error && <Notice kind="error">{a.error}</Notice>}
    </Modal>
  );
}

function RemoveHost({ host, onClose, onDone }: { host: T.Host; onClose: () => void; onDone: () => void }) {
  const [force, setForce] = useState(false);
  return (
    <Confirm title="Remove host" danger confirm="Remove" onClose={onClose}
      onConfirm={async () => { await v1.removeHost(host.id, force); onDone(); }}
      body={<>
        <p>Remove <b>{host.name}</b> from this install?</p>
        {host.replicas > 0 && <><p className="mt-2">It still runs {host.replicas} replica{host.replicas === 1 ? "" : "s"}. Drain it first, or remove it anyway.</p>
          <Toggle label="Remove it anyway" checked={force} onChange={setForce} /></>}
      </>} />
  );
}
