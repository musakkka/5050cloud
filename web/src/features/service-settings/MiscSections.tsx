// SPDX-License-Identifier: AGPL-3.0-only

// Settings > Config-as-code, Feature-flags and Danger.

import { useState, type ReactNode } from "react";
import { AlertTriangle, FileCode } from "lucide-react";
import { del } from "../service/client";
import { Button, Modal, Tooltip, cx } from "../../kit";
import { useFieldState, useSettings } from "./context";
import { DocsLink, PlusField, SettingField, Toggle, docs, useFilter, matches } from "./ui";

/* ---------- config-as-code ---------- */

export function ConfigSection() {
  const { svc, cfg, stage } = useSettings();
  const st = useFieldState("build.configPath");
  const file = svc.configFile;
  return (
    <SettingField title="Config File" keywords="config as code file yaml json toml infrastructure" staged={st.staged}
      description={<>Keep build and deploy settings in the repo. Without a path, the root directory is searched for <code className="font-mono text-[13px] text-foreground">5050.yaml</code>, <code className="font-mono text-[13px] text-foreground">5050.json</code> and compatible JSON or TOML configs. Fields it sets are read-only here. <DocsLink href={docs("SPEC.md")} /></>}>
      {file && (
        <div className="mb-3 flex h-11 items-center gap-3 rounded-md bg-surface-hover px-4 text-sm">
          <FileCode size={16} className="text-fg-3" />
          {file.url ? <a href={file.url} target="_blank" rel="noreferrer" className="font-mono text-foreground hover:underline">{file.path}</a> : <span className="font-mono text-foreground">{file.path}</span>}
          <span className="text-fg-3">{file.format.toUpperCase()}</span>
          {file.commitSha && <span className="ml-auto font-mono text-caption text-fg-3">{file.commitSha.slice(0, 7)}</span>}
        </div>
      )}
      <PlusField label="Add File Path" value={cfg.build.configPath || ""} placeholder="/5050.yaml" mono
        onSave={(v) => stage("build.configPath", v)} />
    </SettingField>
  );
}

/* ---------- feature flags ---------- */

function FlagRow({ title, description, checked, onChange, keywords, staged, disabled }: {
  title: string; description: ReactNode; checked: boolean; onChange: (v: boolean) => void; keywords?: string; staged?: boolean; disabled?: boolean;
}) {
  const { q, sectionMatch } = useFilter();
  if (q && !sectionMatch && !matches(q, title, keywords)) return null;
  return (
    <div data-field className="flex max-w-[536px] gap-4">
      <div className="pt-[2px]"><Toggle checked={checked} onChange={onChange} label={title} disabled={disabled} /></div>
      <div className="min-w-0 flex-1 cursor-pointer" onClick={() => !disabled && onChange(!checked)}>
        <div className="flex items-center gap-2 text-base font-medium text-foreground">
          {title}
          {staged && <span className="inline-flex h-[18px] items-center rounded border border-pink-200 bg-pink-50 px-1.5 text-micro font-medium uppercase tracking-[.06em] text-pink-700">Staged</span>}
        </div>
        <div className="mt-1 text-sm text-fg-2">{description}</div>
      </div>
    </div>
  );
}

export function FlagsSection() {
  const { svc, cfg, stage } = useSettings();
  const single = useFieldState("deploy.singleton");
  const cache = useFieldState("build.noCache");
  const nix = useFieldState("build.nixpacksCompat");
  const repo = cfg.source.type === "github";
  const singletonDefault = svc.kind === "worker" || svc.kind === "cron";
  return (
    <>
      <FlagRow title="Singleton" keywords="singleton reschedule failover host offline" staged={single.staged}
        checked={cfg.deploy.singleton ?? singletonDefault} onChange={(v) => void stage("deploy.singleton", v)}
        description="Never move this service to another host on its own while its host is offline. Use it for jobs that must not run twice." />
      {repo && (
        <FlagRow title="Build Without Cache" keywords="cache no-cache clean build" staged={cache.staged}
          checked={!!cfg.build.noCache} onChange={(v) => void stage("build.noCache", v)}
          description="Start every build from scratch. Slower, but rules out a stale layer." />
      )}
      {repo && (
        <FlagRow title="Nixpacks Compatibility" keywords="nixpacks compat variables toml" staged={nix.staged}
          checked={cfg.build.nixpacksCompat !== false} onChange={(v) => void stage("build.nixpacksCompat", v ? "" : false)}
          description={<>Read <code className="font-mono text-[13px]">NIXPACKS_*</code> variables and <code className="font-mono text-[13px]">nixpacks.toml</code> and apply them to Railpack builds.</>} />
      )}
    </>
  );
}

/* ---------- danger ---------- */

export function DangerSection() {
  const { svc } = useSettings();
  const [open, setOpen] = useState(false);
  const pending = svc.stagedAction === "delete";
  return (
    <SettingField danger title="Delete Service" keywords="delete remove service danger destroy"
      description={<>Deleting this service will <b>permanently delete</b> its deployments, domains and volume and remove it from <b>this environment</b>. This cannot be undone.</>}>
      <Button variant="danger" disabled={pending} className="font-medium" onClick={() => setOpen(true)}>{pending ? "Deletion staged" : "Delete service"}</Button>
      {open && <DeleteServiceModal onClose={() => setOpen(false)} />}
    </SettingField>
  );
}

function DeleteServiceModal({ onClose }: { onClose: () => void }) {
  const { svc, base, onDeleted } = useSettings();
  const name = svc.config.name;
  const [typed, setTyped] = useState("");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const ok = typed === name;
  const run = async () => {
    if (!ok) return;
    setBusy(true); setErr(null);
    try { await del(base, { confirm: name }); onClose(); onDeleted?.(); } catch (e) { setErr((e as Error).message); setBusy(false); }
  };
  return (
    <Modal bare title="Delete Service" width={548} onClose={onClose}>
      <form className="p-6" onSubmit={(e) => { e.preventDefault(); void run(); }}>
        <div className="flex items-center gap-4">
          <AlertTriangle size={20} className="text-danger" />
          <h3 className="text-[20px] font-semibold leading-[30px] text-foreground">Delete Service</h3>
        </div>
        <p className="mt-4 text-base text-fg-2">
          You are <span className="text-danger-text">deleting</span> the service <span className="text-foreground">{name}</span> from this environment. The deletion is staged and happens when you deploy.
        </p>
        <label className="mt-4 block">
          <span className="block text-sm text-fg-2">Type <b className="font-mono font-semibold text-foreground">{name}</b> to confirm</span>
          <input autoFocus value={typed} onChange={(e) => setTyped(e.target.value)} aria-label="Service name"
            className="mt-2 h-[42px] w-full rounded-md border border-line bg-transparent px-3 text-base text-foreground outline-none transition-colors focus:border-accent-muted focus:shadow-none" />
        </label>
        {err && <div role="alert" className="mt-3 text-sm text-danger-text">{err}</div>}
        <div className="mt-6 flex justify-end gap-2">
          <Button size="lg" onClick={onClose}>Cancel</Button>
          <Tooltip content="Type the service name to confirm" disabled={ok}>
            <span tabIndex={ok ? -1 : 0} className="inline-flex rounded-md">
              <Button size="lg" type="submit" variant="danger" busy={busy} disabled={!ok} className={cx(!ok && "!opacity-50")}>Delete</Button>
            </span>
          </Tooltip>
        </div>
      </form>
    </Modal>
  );
}
