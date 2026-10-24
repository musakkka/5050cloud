// SPDX-License-Identifier: AGPL-3.0-only

// Settings > Source: the repo or image row, root directory, the branch this
// environment follows, auto deploy, and the registry credential for images.

import { useState } from "react";
import { Box, Folder, GitBranch, Pencil, RefreshCw, Zap, ZapOff } from "lucide-react";
import type { GitHubRepo, GitHubStatus, RegistryCredential } from "../../api/types.gen";
import { get, post, qs } from "../service/client";
import { useLoad } from "../service/ui";
import { BrandIcon, Button, Confirm, IconButton, Input, Modal, Tooltip, cx, toastSuccess } from "../../kit";
import { useFieldState, useSettings } from "./context";
import { normaliseRepo, validImage } from "./patch";
import { DocsLink, DropField, INPUT, SettingField, TextButton, ToggleRow, docs } from "./ui";

export default function SourceSection() {
  const { cfg } = useSettings();
  const type = cfg.source.type || "empty";
  if (type === "image") return <ImageSource />;
  if (type === "github") return <RepoSource />;
  return <EmptySource />;
}

/* ---------- empty ---------- */

function EmptySource() {
  const [mode, setMode] = useState<"repo" | "image" | null>(null);
  return (
    <SettingField title="Source" keywords="source repo image github docker connect"
      description="Connect a GitHub repository or a container image to deploy this service from.">
      {mode === null && (
        <div className="flex gap-2">
          <Button icon={<BrandIcon name="github" size={16} />} onClick={() => setMode("repo")}>Connect Repo</Button>
          <Button icon={<BrandIcon name="docker" size={16} />} onClick={() => setMode("image")}>Connect Image</Button>
        </div>
      )}
      {mode === "repo" && <RepoPicker onDone={() => setMode(null)} />}
      {mode === "image" && <ImagePicker onDone={() => setMode(null)} />}
    </SettingField>
  );
}

/* ---------- repo ---------- */

const ROW = "flex items-center gap-3 rounded-md border border-surface-hover bg-surface-hover";

function RepoSource() {
  const { cfg, stagePatch, stage } = useSettings();
  const repo = useFieldState("source.repo");
  const root = useFieldState("source.rootDir");
  const [editing, setEditing] = useState(false);
  const [disconnect, setDisconnect] = useState(false);
  const [rootOpen, setRootOpen] = useState(!!cfg.source.rootDir);
  const name = cfg.source.repo || "";
  const href = name.startsWith("https://") ? name : `https://github.com/${name}`;
  const label = name.replace(/^https:\/\/[^/]+\//, "");

  return (
    <>
      <SettingField title="Source Repo" keywords="source repository github disconnect" staged={repo.staged} locked={repo.locked}>
        {editing ? <RepoPicker initial={name} onDone={() => setEditing(false)} /> : (
          <div className={cx(ROW, "h-[52px] pl-4 pr-4")}>
            <BrandIcon name="github" size={20} />
            <a href={href} target="_blank" rel="noreferrer" className="min-w-0 flex-1 truncate text-base text-foreground hover:underline">{label}</a>
            <IconButton label="Change repo" size={26} onClick={() => setEditing(true)}><Pencil size={15} /></IconButton>
            <Button size="xs" onClick={() => setDisconnect(true)} className="px-1.5">Disconnect</Button>
          </div>
        )}
        {!rootOpen && (
          <div className="mt-3 text-sm text-fg-2">
            <TextButton onClick={() => setRootOpen(true)}>Add Root Directory</TextButton>{" "}
            (used for build and deploy steps. <DocsLink href={docs("SPEC.md")}>Docs</DocsLink>)
          </div>
        )}
      </SettingField>

      {rootOpen && (
        <SettingField title="Root Directory" keywords="root directory monorepo folder" staged={root.staged} locked={root.locked}
          description={<>The directory to build and deploy from, for monorepos. <DocsLink href={docs("SPEC.md")} /></>}>
          <RootDirInput value={cfg.source.rootDir || ""} disabled={!!root.locked}
            onSave={async (v) => { await stage("source.rootDir", v === "/" || v === "./" ? "" : v); if (!v) setRootOpen(false); }} />
        </SettingField>
      )}

      <BranchField />

      <SettingField title="Wait for CI" keywords="wait ci github actions checks"
        description="Deploy only after the commit's GitHub checks have passed.">
        <ToggleRow checked={false} onChange={() => undefined} label="Wait for CI" disabled
          reason="Not in v0.1: deploy from CI with 5050 up, or call 5050 redeploy from a GitHub Action" />
      </SettingField>

      {disconnect && (
        <Confirm title="Disconnect repo" danger confirm="Disconnect"
          body={<>This stages removing <b className="text-foreground">{label}</b> as the source. The running deployment keeps serving until you deploy.</>}
          onClose={() => setDisconnect(false)}
          onConfirm={() => stagePatch({ source: { type: "empty", repo: null, branch: null, rootDir: null, autoDeploy: null } })} />
      )}
    </>
  );
}

function RootDirInput({ value, onSave, disabled }: { value: string; onSave: (v: string) => Promise<unknown>; disabled?: boolean }) {
  const [text, setText] = useState(value);
  const [focus, setFocus] = useState(false);
  const save = (v: string) => { if (v.trim() !== value) void onSave(v.trim()).catch(() => setText(value)); };
  const suggestions = ["./", "/"].filter((s) => !text || s.startsWith(text));
  return (
    <div className="relative">
      <input aria-label="Root directory" autoFocus={!value} value={text} placeholder="/apps/web" disabled={disabled}
        onChange={(e) => setText(e.target.value)} onFocus={() => setFocus(true)}
        onBlur={() => { setFocus(false); save(text); }}
        onKeyDown={(e) => { if (e.key === "Enter") (e.target as HTMLInputElement).blur(); if (e.key === "Escape") { e.stopPropagation(); setText(value); (e.target as HTMLInputElement).blur(); } }}
        className={cx(INPUT, "font-mono text-[15px]")} />
      {focus && suggestions.length > 0 && (
        <div role="listbox" className="absolute left-0 right-0 top-[46px] z-20 animate-pop-in rounded-md border border-line-popover bg-background p-1 shadow-popover">
          {suggestions.map((s) => (
            <button key={s} type="button" role="option" aria-selected={text === s} onMouseDown={(e) => { e.preventDefault(); setText(s); save(s); (document.activeElement as HTMLElement)?.blur(); }}
              className="flex h-9 w-full items-center gap-2.5 rounded px-3 text-left font-mono text-sm text-fg-2 transition-colors hover:bg-surface-hover hover:text-foreground">
              <Folder size={15} className="text-fg-3" />{s}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}

function BranchField() {
  const { cfg, stage, stagePatch } = useSettings();
  const st = useFieldState("source.branch");
  const ad = useFieldState("source.autoDeploy");
  const repo = cfg.source.repo || "";
  const branches = useLoad(() => get<string[]>(`/github/branches${qs({ repo })}`).catch(() => [] as string[]), [repo]);
  const branch = cfg.source.branch;
  const auto = cfg.source.autoDeploy !== false;
  const list = [...new Set([...(branch ? [branch] : []), ...(branches.data || []).filter((b) => b && b !== "example"), "main"])];

  return (
    <SettingField title="Branch connected to this environment" keywords="branch git auto deploy" staged={st.staged || ad.staged} locked={st.locked}
      description="New commits pushed to this branch deploy to this environment.">
      {!branch ? (
        <Button variant="accent-outline" onClick={() => void stage("source.branch", list[0] || "main")}>Connect Environment to Branch</Button>
      ) : (
        <div className="overflow-hidden rounded-md bg-surface-hover">
          <div className="flex h-11 items-center gap-2 border-b border-secondaryBg pl-4 pr-4">
            <GitBranch size={18} className="shrink-0 text-pink-600" />
            <DropField label="Branch" value={branch} className="flex-1 [&_button]:border-transparent [&_button]:hover:border-transparent"
              options={list.map((b) => ({ value: b, label: b }))} onChange={(b) => void stage("source.branch", b)} />
            <span aria-hidden className="h-6 w-px bg-line" />
            <Button size="xs" className="ml-2 px-1.5" onClick={() => void stagePatch({ source: { branch: null } })}>Disconnect</Button>
          </div>
          <div className="flex h-10 items-center gap-3 pl-4 pr-4 text-sm">
            {auto ? <Zap size={16} className="text-ok-text" /> : <ZapOff size={16} className="text-fg-3" />}
            <span className="flex-1 text-fg-2">Auto deploy is {auto ? "enabled" : "disabled"}</span>
            <Button size="xs" className="px-1.5" onClick={() => void stage("source.autoDeploy", !auto)}>{auto ? "Disable" : "Enable"}</Button>
          </div>
        </div>
      )}
    </SettingField>
  );
}

/** Repo input with the installed repos as suggestions. */
function RepoPicker({ initial = "", onDone }: { initial?: string; onDone: () => void }) {
  const { stagePatch } = useSettings();
  const [repo, setRepo] = useState(initial);
  const [busy, setBusy] = useState(false);
  const gh = useLoad(() => get<GitHubStatus>("/github").catch(() => null), []);
  const repos = useLoad(() => get<GitHubRepo[] | { items: GitHubRepo[] }>("/github/repos").then((r) => (Array.isArray(r) ? r : r?.items || [])).catch(() => [] as GitHubRepo[]), []);
  const norm = normaliseRepo(repo);
  const submit = async () => {
    if (norm.error) return;
    setBusy(true);
    try { await stagePatch({ source: { type: "github", repo: norm.repo, image: null, registryCredential: null } }); onDone(); } finally { setBusy(false); }
  };
  return (
    <form className="flex flex-col gap-2" onSubmit={(e) => { e.preventDefault(); void submit(); }}
      onKeyDown={(e) => { if (e.key === "Escape") { e.stopPropagation(); onDone(); } }}>
      <div className="flex gap-2">
        <Input autoFocus list="fifty-repos" aria-label="Source repo" placeholder="owner/name or https://github.com/owner/name" value={repo}
          aria-invalid={repo && norm.error ? true : undefined} onChange={(e) => setRepo(e.target.value)} icon={<BrandIcon name="github" size={16} />} />
        <datalist id="fifty-repos">{(repos.data || []).map((r) => <option key={r.fullName} value={r.fullName} />)}</datalist>
        <Button size="lg" onClick={onDone}>Cancel</Button>
        <Button size="lg" type="submit" variant="primary" busy={busy} disabled={!!norm.error || repo === initial}>Connect</Button>
      </div>
      {repo && norm.error && <div className="text-caption text-danger-text">{norm.error}</div>}
      {gh.data && !gh.data.appConfigured && (
        <div className="text-caption text-fg-3">Private repos need the GitHub App (New, GitHub Repository). Public repos work with a pasted URL.</div>
      )}
      {gh.data?.configureUrl && <a className="text-caption text-fg-2 underline underline-offset-2" href={gh.data.configureUrl} target="_blank" rel="noreferrer">Configure the GitHub App</a>}
    </form>
  );
}

/* ---------- image ---------- */

function ImageSource() {
  const { cfg, eid, svc, stagePatch, stage } = useSettings();
  const img = useFieldState("source.image");
  const cred = useFieldState("source.registryCredential");
  const [editing, setEditing] = useState(false);
  const [disconnect, setDisconnect] = useState(false);
  const [addCred, setAddCred] = useState(false);
  const [checking, setChecking] = useState(false);
  const creds = useLoad(() => get<RegistryCredential[]>("/registry-credentials").catch(() => [] as RegistryCredential[]), []);
  const image = cfg.source.image || "";
  const icon = cfg.icon || image;
  const check = async () => {
    setChecking(true);
    try { await post(`/environments/${encodeURIComponent(eid)}/services/${encodeURIComponent(svc.id)}/deploy`, { ref: "latest" }); toastSuccess("Pulling the newest image and redeploying"); } finally { setChecking(false); }
  };
  const credOptions = [{ value: "", label: "None (public image)" }, ...(creds.data || []).filter((c) => c.id !== "example").map((c) => ({ value: c.id, label: `${c.name} (${c.server})` }))];

  return (
    <>
      <SettingField title="Source Image" keywords="source image docker registry tag disconnect" staged={img.staged} locked={img.locked}>
        {editing ? <ImagePicker initial={image} onDone={() => setEditing(false)} /> : (
          <div className={cx(ROW, "min-h-[66px] py-3 pl-4 pr-4")}>
            <span className="inline-flex h-6 w-6 shrink-0 items-center justify-center">
              <BrandIcon name={icon} size={20} fallback="box" />
            </span>
            <div className="min-w-0 flex-1">
              <Tooltip content={image}><div className="truncate text-base text-foreground" tabIndex={0}>{image}</div></Tooltip>
              <button type="button" onClick={() => void check()} disabled={checking}
                className="mt-0.5 inline-flex items-center gap-1 text-caption text-fg-2 transition-colors hover:text-foreground disabled:opacity-60">
                <RefreshCw size={11} className={cx(checking && "spin")} />Check for a newer image
              </button>
            </div>
            <IconButton label="Change image" size={26} onClick={() => setEditing(true)}><Pencil size={15} /></IconButton>
            <Button size="xs" onClick={() => setDisconnect(true)} className="px-1.5">Disconnect</Button>
          </div>
        )}
      </SettingField>

      <SettingField title="Registry Credential" keywords="registry credential private image login" staged={cred.staged}
        description="For private images. Credentials are stored encrypted and shared across the install.">
        <div className="flex gap-2">
          <DropField label="Registry credential" className="flex-1" value={cfg.source.registryCredential || ""} options={credOptions}
            onChange={(v) => void stage("source.registryCredential", v)} />
          <Button size="lg" onClick={() => setAddCred(true)}>Add credential</Button>
        </div>
      </SettingField>

      <SettingField title="Auto Updates" keywords="auto updates image tag schedule"
        description="Redeploy automatically when the image tag gets a newer digest.">
        <ToggleRow checked={false} onChange={() => undefined} label="Update automatically" disabled reason="Not in v0.1: use Check for a newer image" />
      </SettingField>

      {disconnect && (
        <Confirm title="Disconnect image" danger confirm="Disconnect"
          body={<>This stages removing <b className="break-all text-foreground">{image}</b> as the source. The running deployment keeps serving until you deploy.</>}
          onClose={() => setDisconnect(false)} onConfirm={() => stagePatch({ source: { type: "empty", image: null, registryCredential: null } })} />
      )}
      {addCred && <AddCredential onClose={() => setAddCred(false)} onAdded={async (c) => { await creds.reload(); await stage("source.registryCredential", c.id); }} />}
    </>
  );
}

function ImagePicker({ initial = "", onDone }: { initial?: string; onDone: () => void }) {
  const { stagePatch } = useSettings();
  const [image, setImage] = useState(initial);
  const [busy, setBusy] = useState(false);
  const err = image ? validImage(image) : null;
  const submit = async () => {
    if (err || !image) return;
    setBusy(true);
    try { await stagePatch({ source: { type: "image", image: image.trim(), repo: null, branch: null, rootDir: null } }); onDone(); } finally { setBusy(false); }
  };
  return (
    <form className="flex flex-col gap-2" onSubmit={(e) => { e.preventDefault(); void submit(); }}
      onKeyDown={(e) => { if (e.key === "Escape") { e.stopPropagation(); onDone(); } }}>
      <div className="flex gap-2">
        <Input autoFocus aria-label="Image" placeholder="nginx:1.27 or ghcr.io/owner/app:tag" value={image} mono
          aria-invalid={err ? true : undefined} onChange={(e) => setImage(e.target.value)} icon={<Box size={16} />} />
        <Button size="lg" onClick={onDone}>Cancel</Button>
        <Button size="lg" type="submit" variant="primary" busy={busy} disabled={!image || !!err || image === initial}>Connect</Button>
      </div>
      {err && <div className="text-caption text-danger-text">{err}</div>}
    </form>
  );
}

function AddCredential({ onClose, onAdded }: { onClose: () => void; onAdded: (c: RegistryCredential) => Promise<void> }) {
  const [f, setF] = useState({ name: "", server: "ghcr.io", username: "", password: "" });
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const ok = f.name && f.server && f.username && f.password;
  const labels = { name: "Name", server: "Registry", username: "Username", password: "Password or token" } as const;
  return (
    <Modal title="Add registry credential" onClose={onClose} footer={<>
      <Button onClick={onClose}>Cancel</Button>
      <Button variant="primary" busy={busy} disabled={!ok} onClick={async () => {
        setBusy(true); setErr(null);
        try { await onAdded(await post<RegistryCredential>("/registry-credentials", f)); onClose(); } catch (e) { setErr((e as Error).message); setBusy(false); }
      }}>Add</Button>
    </>}>
      <div className="flex flex-col gap-4">
        {(["name", "server", "username", "password"] as const).map((k) => (
          <label key={k} className="block space-y-2">
            <span className="block text-caption font-semibold text-fg-2">{labels[k]}</span>
            <Input type={k === "password" ? "password" : "text"} value={f[k]} onChange={(e) => setF({ ...f, [k]: e.target.value })} autoComplete="off" />
          </label>
        ))}
        {err && <div role="alert" className="text-sm text-danger-text">{err}</div>}
      </div>
    </Modal>
  );
}

