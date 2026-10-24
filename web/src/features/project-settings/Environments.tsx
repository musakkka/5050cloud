// SPDX-License-Identifier: AGPL-3.0-only

import { useState } from "react";
import { Check, Copy, MoreHorizontal, Pencil, Plus, ShieldOff, ShieldCheck, Trash2 } from "lucide-react";
import type * as T from "../../api/types.gen";
import { Avatar, Button, ExternalLink, Field, IconButton, Input, Menu, Modal, RelTime, Select, Switch, cx, readPref, toast, writePref } from "../../kit";
import { v1 } from "../settings/client";
import { checkEnvName } from "../settings/forms";
import TypedConfirm from "./TypedConfirm";
import { BlockDivider, InlineError, SectionHead, useAsync } from "./ui";

export default function Environments({ project, current, onProject, admin }: { project: T.Project; current?: T.Environment; onProject: (p: T.Project) => void; admin: boolean }) {
  const envs = project.environments;
  const [creating, setCreating] = useState(false);
  const [rename, setRename] = useState<T.Environment | null>(null);
  const [del, setDel] = useState<T.Environment | null>(null);
  const setEnvs = (list: T.Environment[]) => onProject({ ...project, environments: list });
  const toggleEgress = async (e: T.Environment) => {
    const next = e.egress === "blocked" ? "allowed" : "blocked";
    const prev = envs;
    setEnvs(envs.map((x) => (x.id === e.id ? { ...x, egress: next } : x)));
    try { await v1.updateEnvironment(e.id, { egress: next }); toast(next === "blocked" ? `Outbound traffic blocked in ${e.name}` : `Outbound traffic allowed in ${e.name}`, "success"); }
    catch (er: any) { setEnvs(prev); toast(er?.message || String(er), "error"); }
  };
  return (
    <>
      <section>
        <SectionHead title="Environments" description={<>Environments keep deployments for each stage of your project apart. <ExternalLink href="https://github.com/5050cloud/5050cloud#environments" className="text-fg-2 underline underline-offset-2 hover:text-foreground">Read the docs</ExternalLink></>} />
        <div className="overflow-hidden rounded-lg border border-line">
          {envs.map((e, i) => (
            <div key={e.id} className={cx("group flex h-[60px] items-center gap-3 px-4 transition-colors hover:bg-tint-hover", i > 0 && "border-t border-surface-hover")}>
              <span className="flex w-4 shrink-0 justify-center text-accent-muted">{current?.id === e.id && <Check size={16} aria-label="Current environment" />}</span>
              <span className="min-w-0 flex-1 truncate text-lead text-foreground">{e.name}</span>
              {e.egress === "blocked" && <span className="hidden items-center gap-1 text-caption text-warn sm:inline-flex"><ShieldOff size={12} />No outbound</span>}
              <span className="hidden text-body text-fg-3 sm:inline">Created <RelTime ts={e.createdAt} /></span>
              <Avatar name={project.name} size={16} />
              <Menu width={220} trigger={<IconButton label={`Actions for ${e.name}`} size={34}><MoreHorizontal size={16} /></IconButton>}
                items={[
                  { label: "Rename", icon: <Pencil size={14} />, onClick: () => setRename(e) },
                  ...(admin ? [{ label: e.egress === "blocked" ? "Allow outbound traffic" : "Block outbound traffic", icon: e.egress === "blocked" ? <ShieldCheck size={14} /> : <ShieldOff size={14} />, onClick: () => void toggleEgress(e) }] : []),
                  ...(admin && envs.length > 1 && i > 0 ? ["separator" as const, { label: "Delete environment", icon: <Trash2 size={14} />, danger: true, onClick: () => setDel(e) }] : []),
                ]} />
            </div>
          ))}
        </div>
        <Button variant="primary" className="mt-4" icon={<Plus size={16} />} onClick={() => setCreating(true)}>New Environment</Button>
      </section>
      <BlockDivider />
      <PREnvironments project={project} envs={envs} />
      {creating && <NewEnvironment project={project} onClose={() => setCreating(false)} onCreated={(e) => { setEnvs([...envs, e]); toast(`Environment ${e.name} created`, "success"); }} />}
      {rename && <RenameEnv env={rename} taken={envs.filter((x) => x.id !== rename.id).map((x) => x.name)} onClose={() => setRename(null)}
        onSaved={(e) => { setEnvs(envs.map((x) => (x.id === e.id ? e : x))); toast("Environment renamed", "success"); }} />}
      {del && <TypedConfirm title="Delete Environment" name={del.name} action="Delete"
        body={<>You are <b className="text-danger-text">deleting</b> the environment <b>{del.name}</b>. Every service in it stops and its volumes are deleted after 48 hours.</>}
        onClose={() => setDel(null)} onConfirm={async (c) => { await v1.deleteEnvironment(del.id, c); setEnvs(envs.filter((x) => x.id !== del.id)); toast("Environment deleted", "success"); }} />}
    </>
  );
}

/* ------------------------------------------------------------- new env */

function NewEnvironment({ project, onClose, onCreated }: { project: T.Project; onClose: () => void; onCreated: (e: T.Environment) => void }) {
  const envs = project.environments;
  const [name, setName] = useState("");
  const [mode, setMode] = useState<"duplicate" | "empty">("duplicate");
  const [from, setFrom] = useState(envs[0]?.id || "");
  const a = useAsync();
  const err = name ? checkEnvName(name, envs.map((e) => e.name)) : null;
  const ok = !!name.trim() && !err;
  const submit = async (ev: React.FormEvent) => {
    ev.preventDefault();
    if (!ok) return;
    const r = await a.run(() => v1.createEnvironment(project.id, { name: name.trim(), mode, from: mode === "duplicate" ? from : undefined }));
    if (r) { onCreated(r); onClose(); }
  };
  return (
    <Modal bare width={520} title="New Environment" onClose={onClose}>
      <form onSubmit={submit} className="p-8">
        <h3 className="text-h2 text-foreground">New Environment</h3>
        <p className="mt-2 text-body text-fg-3">Create an environment to deploy changes in isolation. You can later <b className="font-semibold text-fg-2">sync environments</b> to move changes across. <ExternalLink href="https://github.com/5050cloud/5050cloud#environments" className="underline underline-offset-2 hover:text-foreground">Learn More</ExternalLink></p>
        <div className="mt-6">
          <Input value={name} onChange={(e) => setName(e.target.value)} autoFocus placeholder="Environment name" aria-label="Environment name" aria-invalid={!!err || undefined} maxLength={32} />
          <InlineError>{err}</InlineError>
        </div>
        <div className="mt-4 space-y-2" role="radiogroup" aria-label="Start with">
          <RadioCard checked={mode === "duplicate"} onSelect={() => setMode("duplicate")} icon={<Copy size={16} />} title="Duplicate Environment">
            <div className={cx("mt-3", mode !== "duplicate" && "pointer-events-none opacity-60")} onClick={(e) => e.stopPropagation()}>
              <Select variant="field" aria-label="Environment to duplicate" value={from} onChange={(e) => setFrom(e.target.value)} options={envs.map((e) => ({ value: e.id, label: e.name }))} />
            </div>
            <p className="mt-3 text-body text-fg-2">Copy all the services, variables, and configuration from an existing environment. Sealed variables need new values.</p>
          </RadioCard>
          <RadioCard checked={mode === "empty"} onSelect={() => setMode("empty")} icon={<Plus size={16} />} title="Empty Environment">
            <p className="mt-1 text-body text-fg-2">An empty environment with no services or variables included.</p>
          </RadioCard>
        </div>
        <InlineError>{a.error}</InlineError>
        <Button type="submit" variant="primary" size="lg" block busy={a.busy} disabled={!ok} className="mt-8">Create Environment</Button>
      </form>
    </Modal>
  );
}

function RadioCard({ checked, onSelect, title, children }: { checked: boolean; onSelect: () => void; icon?: React.ReactNode; title: string; children: React.ReactNode }) {
  return (
    <div role="radio" aria-checked={checked} tabIndex={0} onClick={onSelect} onKeyDown={(e) => { if (e.key === " " || e.key === "Enter") { e.preventDefault(); onSelect(); } }}
      className={cx("flex cursor-pointer gap-4 rounded-md border p-4 transition-colors duration-150", checked ? "border-pink-200 bg-pink-50" : "border-line bg-gray-50 hover:border-fg-5")}>
      <span className={cx("mt-0.5 flex h-5 w-5 shrink-0 items-center justify-center rounded-full border transition-colors", checked ? "border-accent bg-accent" : "border-fg-5")}>
        <span className={cx("h-2 w-2 rounded-full bg-white transition-transform duration-150", checked ? "scale-100" : "scale-0")} />
      </span>
      <div className="min-w-0 flex-1"><div className="text-lead font-medium leading-5 text-foreground">{title}</div>{children}</div>
    </div>
  );
}

function RenameEnv({ env, taken, onClose, onSaved }: { env: T.Environment; taken: string[]; onClose: () => void; onSaved: (e: T.Environment) => void }) {
  const [name, setName] = useState(env.name);
  const a = useAsync();
  const err = name.trim() === env.name ? null : checkEnvName(name, taken);
  const ok = !!name.trim() && !err && name.trim() !== env.name;
  const submit = async (ev: React.FormEvent) => {
    ev.preventDefault();
    if (!ok) return;
    const r = await a.run(() => v1.updateEnvironment(env.id, { name: name.trim() }));
    if (r) { onSaved(r); onClose(); }
  };
  return (
    <Modal bare width={480} title="Rename environment" onClose={onClose}>
      <form onSubmit={submit} className="p-6">
        <h3 className="text-h2 text-foreground">Rename environment</h3>
        <div className="mt-4"><Field label="Name" error={err || undefined}><Input value={name} onChange={(e) => setName(e.target.value)} autoFocus onFocus={(e) => e.currentTarget.select()} maxLength={32} /></Field></div>
        <InlineError>{a.error}</InlineError>
        <div className="mt-6 flex justify-end gap-2"><Button size="lg" onClick={onClose}>Cancel</Button><Button type="submit" size="lg" variant="primary" busy={a.busy} disabled={!ok}>Save</Button></div>
      </form>
    </Modal>
  );
}

/* ------------------------------------------------------------- PR envs */

interface PRPrefs { enabled: boolean; base: string; bot: boolean; focused: boolean }

function PREnvironments({ project, envs }: { project: T.Project; envs: T.Environment[] }) {
  const key = `5050.pr.${project.id}`;
  const [p, setP] = useState<PRPrefs>(() => readPref<PRPrefs>(key, { enabled: false, base: "", bot: false, focused: false }));
  const set = (n: PRPrefs) => { setP(n); writePref(key, n); };
  return (
    <section>
      <SectionHead title="PR Environments" description="Environments created for each pull request and cleaned up when the pull request is closed." />
      {!p.enabled ? (
        <Button onClick={() => { set({ ...p, enabled: true }); toast("Future pull requests will get their own environment.", "success"); }}>Enable PR Environments</Button>
      ) : (
        <div className="space-y-8">
          <Button onClick={() => { set({ ...p, enabled: false }); toast("PR environments turned off.", "info"); }}>Disable PR Environments</Button>
          <div className="animate-rise-in">
            <h3 className="text-body font-semibold text-foreground">Base Environment</h3>
            <p className="mb-4 mt-1 text-body text-fg-2">Each PR environment starts as a copy of this environment.</p>
            <Select variant="field" aria-label="Base environment" value={p.base} onChange={(e) => set({ ...p, base: e.target.value })}
              options={[{ value: "", label: "No base environment" }, ...envs.map((e) => ({ value: e.id, label: e.name }))]} />
          </div>
          <div>
            <h3 className="text-body font-semibold text-foreground">Bot PR Environments</h3>
            <p className="mb-4 mt-1 text-body text-fg-2">Also create environments for pull requests opened by bots such as dependency updaters.</p>
            <Button onClick={() => set({ ...p, bot: !p.bot })}>{p.bot ? "Disable" : "Enable"} Bot PR Environments</Button>
          </div>
          <div>
            <h3 className="text-body font-semibold text-foreground">Focused PR Environments</h3>
            <p className="mb-4 mt-1 text-body text-fg-2">Only deploy the services a pull request changed.</p>
            <Button onClick={() => set({ ...p, focused: !p.focused })}>{p.focused ? "Disable" : "Enable"} Focused PR Environments</Button>
          </div>
          <div className="flex items-center gap-3 text-caption text-fg-3"><Switch size="sm" checked={p.enabled} onChange={(v) => set({ ...p, enabled: v })} label="PR environments" />These choices are kept in this browser until the pull request service is connected.</div>
        </div>
      )}
    </section>
  );
}
