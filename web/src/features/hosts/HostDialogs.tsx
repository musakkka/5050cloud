// SPDX-License-Identifier: AGPL-3.0-only

// The dialogs of the Hosts page: add a machine (it returns a single-use join command), edit a
// machine's name, roles and kept-free memory, and remove a machine.
import { useState } from "react";
import { Check, Plus } from "lucide-react";
import { Button, Checkbox, CopyButton, Field, Input, Modal, Notice, cx, fmtDateTime, plural, toastError, toastSuccess } from "../../kit";
import type * as T from "../../api/types.gen";
import { hostsApi, messageOf } from "./client";
import { ROLE_INFO, ROLE_OPTIONS, ROLES, toggleRole } from "./model";

/** Role chips: a checkbox per role with its icon, and the one-line meaning of each below. */
export function RoleChips({ roles, onChange, options, lockedHint }: {
  roles: string[]; onChange: (r: string[]) => void; options: string[]; lockedHint?: string;
}) {
  return (
    <div className="space-y-3">
      <div role="group" aria-label="Roles" className="flex flex-wrap gap-2">
        {options.map((r) => {
          const on = roles.includes(r);
          const locked = r === "control" && !!lockedHint;
          const Icon = ROLE_INFO[r]?.icon;
          return (
            <button key={r} type="button" role="checkbox" aria-checked={on} disabled={locked}
              onClick={() => onChange(toggleRole(roles, r, !on))}
              className={cx("inline-flex h-[30px] items-center gap-1.5 rounded-md border px-2.5 text-caption font-medium transition-colors duration-150 disabled:cursor-not-allowed disabled:opacity-50",
                on ? "border-accent-text bg-surface-hover text-foreground" : "border-line text-fg-2 hover:border-fg-5 hover:text-foreground")}>
              {on ? <Check size={14} className="text-accent-text" /> : Icon && <Icon size={14} />}
              {r}
            </button>
          );
        })}
      </div>
      <ul className="space-y-1.5">
        {options.map((r) => (
          <li key={r} className="text-caption leading-[18px] text-fg-3">
            <span className="font-mono text-fg-2">{r}</span> {r === "control" && lockedHint ? lockedHint : ROLE_INFO[r]?.hint}
          </li>
        ))}
      </ul>
    </div>
  );
}

export function AddHostDialog({ onClose }: { onClose: () => void }) {
  const [name, setName] = useState("");
  const [roles, setRoles] = useState<string[]>(["app"]);
  const [token, setToken] = useState<T.JoinToken | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const create = async () => {
    setBusy(true);
    setError(null);
    try {
      setToken(await hostsApi.joinToken({ roles, name: name.trim() || undefined }));
    } catch (e) {
      setError(messageOf(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Modal title={token ? "Run this on the new machine" : "Add host"} width={560} onClose={onClose}
      description={token ? undefined : "Installs the agent and joins the machine to this install."}
      footer={token ? (
        <Button variant="primary" onClick={onClose}>Done</Button>
      ) : (
        <>
          <Button variant="ghost" onClick={onClose}>Cancel</Button>
          <Button variant="primary" busy={busy} disabled={!roles.length} icon={<Plus size={16} />} onClick={create}>Create join command</Button>
        </>
      )}>
      {token ? (
        <div className="space-y-4">
          <p className="text-body text-fg-2">
            Run it as root on the new machine. It works once and expires {fmtDateTime(token.expiresAt)}.
            The host appears here when it connects.
          </p>
          <div className="relative rounded-lg border border-line bg-background">
            <pre className="scroll-thin overflow-x-auto whitespace-pre-wrap break-words p-3 pr-12 font-mono text-caption leading-5 text-foreground">{token.command}</pre>
            <CopyButton text={token.command} label="Copy join command" className="absolute right-2 top-2" />
          </div>
          <ol className="list-decimal space-y-1.5 pl-5 text-body text-fg-3">
            <li>Open a root shell on the new machine.</li>
            <li>Paste the command and press Enter.</li>
          </ol>
        </div>
      ) : (
        <div className="space-y-5">
          <Field label="Name" optional hint="When empty, the machine's host name is used.">
            <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="app-3" autoFocus maxLength={63} />
          </Field>
          <div className="space-y-2">
            <span className="block text-caption font-semibold text-fg-2">Roles</span>
            <RoleChips roles={roles} onChange={setRoles} options={ROLE_OPTIONS} />
          </div>
          {error && <Notice tone="danger">{error}</Notice>}
        </div>
      )}
    </Modal>
  );
}

export function EditHostDialog({ host, onClose, onSaved }: { host: T.Host; onClose: () => void; onSaved: (h: T.Host) => void }) {
  const [name, setName] = useState(host.name);
  const [roles, setRoles] = useState(host.roles);
  const [reserve, setReserve] = useState(String(host.reserveMb));
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const nameOk = name.trim().length > 0;
  const reserveOk = /^\d+$/.test(reserve);
  const valid = nameOk && reserveOk && roles.length > 0;

  const save = async () => {
    setBusy(true);
    setError(null);
    try {
      onSaved(await hostsApi.update(host.id, { name: name.trim(), roles, reserveMb: Number(reserve) }));
    } catch (e) {
      setError(messageOf(e));
      setBusy(false);
    }
  };

  return (
    <Modal title={`Edit ${host.name}`} width={560} onClose={onClose}
      footer={(
        <>
          <Button variant="ghost" onClick={onClose}>Cancel</Button>
          <Button variant="primary" busy={busy} disabled={!valid} onClick={save}>Save changes</Button>
        </>
      )}>
      <div className="space-y-5">
        <Field label="Name" error={nameOk ? undefined : "Enter a name."}>
          <Input value={name} onChange={(e) => setName(e.target.value)} autoFocus maxLength={63} />
        </Field>
        <Field label="Memory kept free for the system" hint="Replicas cannot use this memory."
          error={reserveOk ? undefined : "Enter a whole number of megabytes."}>
          <Input inputMode="numeric" value={reserve} onChange={(e) => setReserve(e.target.value)} suffix={<span className="text-caption text-fg-3">MB</span>} />
        </Field>
        <div className="space-y-2">
          <span className="block text-caption font-semibold text-fg-2">Roles</span>
          <RoleChips roles={roles} onChange={setRoles} options={ROLES} lockedHint="Stays on the first machine." />
        </div>
        {error && <Notice tone="danger">{error}</Notice>}
      </div>
    </Modal>
  );
}

export function RemoveHostDialog({ host, onClose, onRemoved }: { host: T.Host; onClose: () => void; onRemoved: () => void }) {
  const [force, setForce] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const blocked = host.replicas > 0 && !force;

  const remove = async () => {
    setBusy(true);
    setError(null);
    try {
      await hostsApi.remove(host.id, force);
      toastSuccess(`${host.name} was removed.`);
      onRemoved();
    } catch (e) {
      setError(messageOf(e));
      toastError(e);
      setBusy(false);
    }
  };

  return (
    <Modal title={`Remove ${host.name}?`} width={480} onClose={onClose}
      footer={(
        <>
          <Button variant="ghost" onClick={onClose}>Cancel</Button>
          <Button variant="danger" busy={busy} disabled={blocked} onClick={remove}>Remove host</Button>
        </>
      )}>
      <div className="space-y-4">
        <p>{host.name} leaves this install.</p>
        {host.replicas > 0 && (
          <>
            <Notice tone="warn">It still runs {plural(host.replicas, "replica")}. Drain it first, or remove it anyway.</Notice>
            <Checkbox checked={force} onChange={setForce} label="Remove it anyway" />
          </>
        )}
        {error && <Notice tone="danger">{error}</Notice>}
      </div>
    </Modal>
  );
}
