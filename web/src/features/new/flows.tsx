// SPDX-License-Identifier: AGPL-3.0-only

// Creation flows shared by /new (new project) and the canvas New menu
// (current environment): GitHub repo with the scan dialog, Docker image,
// database, Docker Compose, empty service and volume.
import { Fragment, useEffect, useMemo, useState, type ReactNode } from "react";
import { Box, Braces, Cable, CircleAlert, CircleCheck, Database, Globe, HardDrive, FileUp, KeyRound, Settings2 } from "lucide-react";
import { ApiError, api, type Import, type RegistryCredential, type ScanResult, type Service } from "../../api/client";
import { BrandIcon, Button, Checkbox, ErrorLine, Field, Input, Modal, Select, Skeleton, Spinner, Textarea, toast } from "../../kit";
import { PickerOverlay } from "./overlay";
import { imageError, imageName, imageRegistry } from "./image";

/** Where a flow creates things. /new makes a project on first use; the canvas uses its environment. */
export interface Target {
  ensure: () => Promise<{ pid: string; eid: string }>;
  /** Called after creation. `services` are the created ones; `variables` asks to open the Variables tab. */
  done: (r: { pid: string; eid: string; services: Service[]; variables?: boolean }) => void;
  /** True on the canvas: the primary action stages instead of deploying at once. */
  canvas?: boolean;
  position?: number[];
}

function useAction() {
  const [busy, setBusy] = useState<string>();
  const [error, setError] = useState<unknown>();
  const run = async (which: string, fn: () => Promise<void>) => {
    setBusy(which); setError(undefined);
    try { await fn(); } catch (e) { setError(e); } finally { setBusy(undefined); }
  };
  return { busy, error, run, setError };
}

// ---------- GitHub ----------

/** Canvas entry: the GitHub step of the create picker, floating over the canvas. */
export function GitHubFlow({ target, onClose }: { target: Target; onClose: () => void }) {
  return <PickerOverlay step="github" target={target} onClose={onClose} />;
}

/** The scan dialog: detected builder, start command, config files and monorepo packages to tick. */
export function ScanDialog({ target, repo, defaultBranch, onBack, onClose }: { target: Target; repo: string; defaultBranch?: string; onBack?: () => void; onClose: () => void }) {
  const [branches, setBranches] = useState<string[]>();
  const [branch, setBranch] = useState(defaultBranch || "");
  const [rootDir, setRootDir] = useState("");
  const [scan, setScan] = useState<ScanResult>();
  const [ticked, setTicked] = useState<string[]>([]);
  const [scanErr, setScanErr] = useState<unknown>();
  const a = useAction();

  useEffect(() => {
    api.branches(repo).then((b) => { setBranches(b); setBranch((cur) => cur || b[0] || ""); }, () => setBranches([]));
  }, [repo]);
  useEffect(() => {
    if (branches === undefined) return;
    setScan(undefined); setScanErr(undefined);
    api.scan({ repo, branch: branch || undefined, rootDir: rootDir || undefined }).then(setScan, setScanErr);
  }, [repo, branch, rootDir, branches]);

  const name = repo.split("/").pop() || "app";
  const create = (deployNow: boolean) => a.run(deployNow ? "deploy" : "stage", async () => {
    const t = await target.ensure();
    const pkgs = scan?.packages?.filter((p) => ticked.includes(p.rootDir)) || [];
    const units = pkgs.length ? pkgs.map((p) => ({ name: p.name, rootDir: p.rootDir, watchPaths: p.watchPaths })) : [{ name, rootDir: rootDir || undefined, watchPaths: undefined }];
    const services: Service[] = [];
    for (const [i, u] of units.entries()) {
      const r = await api.createService(t.eid, {
        name: u.name.replace(/[^a-zA-Z0-9-_ ]/g, "-").slice(0, 32),
        source: { type: "github", repo, branch: branch || undefined, rootDir: u.rootDir },
        rootDir: u.rootDir, watchPaths: u.watchPaths, deployNow,
        position: target.position ? [target.position[0] + i * 336, target.position[1]] : undefined,
      });
      services.push(...r.services);
    }
    target.done({ ...t, services, variables: !deployNow && !target.canvas });
  });

  const short = repo.replace(/^https:\/\/github\.com\//, "");
  const rows: [string, ReactNode][] = scan ? [
    ["Builder", scan.dockerfile ? "Dockerfile" : <span className="inline-flex items-center gap-1.5">{scan.provider && <BrandIcon name={scan.provider} size={14} fallback="none" />}Railpack{scan.provider ? ` (${scan.provider})` : ""}</span>],
    ...(scan.startCommand ? [["Start command", <code key="c" className="font-mono text-[13px] text-foreground">{scan.startCommand}</code>] as [string, ReactNode]] : []),
    ...(scan.suggestedPort ? [["Port", String(scan.suggestedPort)] as [string, ReactNode]] : []),
    ...(scan.configFiles || []).map((c) => ["Config file", <>{c.path} <span className="text-fg-3">sets {c.sets.length} fields</span></>] as [string, ReactNode]),
    ...(scan.envNames.length ? [["Variables", <span key="v" className="flex flex-wrap gap-1">{scan.envNames.map((n) => <code key={n} className="rounded bg-surface-hover px-1.5 py-0.5 font-mono text-caption text-foreground">{n}</code>)}</span>] as [string, ReactNode]] : []),
  ] : [];
  return (
    <Modal title={short} onClose={onClose} width={560}
      description={<span className="inline-flex items-center gap-1.5"><BrandIcon name="github" size={14} mono />Deploy this repository{target.canvas ? " into the environment" : " in a new project"}</span>}
      footerAlign="between"
      footer={<>
        {onBack ? <Button variant="ghost" onClick={onBack}>Back</Button> : <span />}
        <div className="flex gap-2">
          <Button busy={a.busy === "stage"} disabled={!!a.busy || !scan} onClick={() => void create(false)}>{target.canvas ? "Add to canvas" : "Add Variables"}</Button>
          <Button variant="primary" busy={a.busy === "deploy"} disabled={!!a.busy || !scan} onClick={() => void create(true)}>Deploy Now</Button>
        </div>
      </>}>
      <div className="space-y-5 pb-2 pt-2">
        <div className="grid gap-3 sm:grid-cols-2">
          <Field label="Branch">
            {branches && branches.length
              ? <Select variant="field" value={branch} onChange={(e) => setBranch(e.target.value)} options={branches.map((b) => ({ value: b, label: b }))} />
              : <Input value={branch} placeholder="default branch" onChange={(e) => setBranch(e.target.value)} />}
          </Field>
          <Field label="Root directory"><Input value={rootDir} placeholder="/" mono onChange={(e) => setRootDir(e.target.value)} /></Field>
        </div>
        {!scan && !scanErr && (
          <div className="space-y-2 rounded-lg border border-line px-4 py-4">
            <div className="flex items-center gap-2 text-sm text-fg-2"><Spinner size={14} />Scanning the repository</div>
            <Skeleton h={10} w="70%" /><Skeleton h={10} w="45%" />
          </div>
        )}
        <ErrorLine error={scanErr} />
        {scan && (
          <dl className="grid grid-cols-[120px,1fr] gap-x-4 gap-y-2.5 rounded-lg border border-line px-4 py-3.5 text-sm animate-fade-in">
            {rows.map(([k, v], i) => <Fragment key={i}><dt className="text-fg-3">{k}</dt><dd className="min-w-0 break-words text-foreground">{v}</dd></Fragment>)}
          </dl>
        )}
        {scan?.packages && scan.packages.length > 0 && (
          <fieldset className="space-y-2">
            <legend className="mb-2 text-caption font-semibold text-fg-2">Packages to deploy (one service each)</legend>
            {scan.packages.map((p) => (
              <Checkbox key={p.rootDir} checked={ticked.includes(p.rootDir)} label={p.name} description={p.rootDir}
                onChange={(on) => setTicked(on ? [...ticked, p.rootDir] : ticked.filter((x) => x !== p.rootDir))} />
            ))}
          </fieldset>
        )}
        <ErrorLine error={a.error} />
      </div>
    </Modal>
  );
}

// ---------- Docker image ----------

export function RegistryCredentialPicker({ server, value, onChange }: { server: string; value: string; onChange: (id: string) => void }) {
  const [list, setList] = useState<RegistryCredential[]>();
  const [adding, setAdding] = useState(false);
  const [form, setForm] = useState({ name: "", username: "", password: "" });
  const a = useAction();
  useEffect(() => { api.registryCredentials().then(setList, () => setList([])); }, []);
  const add = () => a.run("add", async () => {
    const c = await api.addRegistryCredential({ ...form, name: form.name || server, server });
    setList((l) => [...(l || []), c]); onChange(c.id); setAdding(false);
  });
  return (
    <div className="space-y-2">
      <Field label="Registry credential" hint="Only for private images.">
        <div className="flex gap-2">
          <Select variant="field" aria-label="Registry credential" value={value} onChange={(e) => onChange(e.target.value)} className="flex-1"
            options={[{ value: "", label: "None (public image)" }, ...(list || []).map((c) => ({ value: c.id, label: `${c.name} (${c.username}@${c.server})` }))]} />
          <Button onClick={() => setAdding((x) => !x)}>Add credential</Button>
        </div>
      </Field>
      {adding && (
        <div className="space-y-2 rounded-lg border border-line bg-surface p-3 animate-rise-in">
          <div className="text-caption text-fg-3">For {server}</div>
          <Input aria-label="Credential name" placeholder="Name" value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} />
          <Input aria-label="Username" placeholder="Username" value={form.username} onChange={(e) => setForm({ ...form, username: e.target.value })} autoComplete="off" />
          <Input aria-label="Password or token" placeholder="Password or token" type="password" value={form.password} onChange={(e) => setForm({ ...form, password: e.target.value })} autoComplete="new-password" />
          <ErrorLine error={a.error} />
          <Button variant="primary" busy={a.busy === "add"} disabled={!form.username || !form.password} onClick={() => void add()}>Save credential</Button>
        </div>
      )}
    </div>
  );
}

/** Canvas entry: the Docker image step; with `initial` it opens the full form (registry credentials). */
export function ImageFlow({ target, onClose, initial }: { target: Target; onClose: () => void; initial?: string }) {
  if (initial !== undefined) return <ImageForm target={target} onClose={onClose} initial={initial} />;
  return <PickerOverlay step="image" target={target} onClose={onClose} />;
}

/** The image form: reference, registry credential, then stage or deploy. */
export function ImageForm({ target, onClose, initial = "" }: { target: Target; onClose: () => void; initial?: string }) {
  const [image, setImage] = useState(initial);
  const [cred, setCred] = useState("");
  const [touched, setTouched] = useState(!!initial);
  const a = useAction();
  const err = imageError(image);
  const create = (deployNow: boolean) => a.run(deployNow ? "deploy" : "stage", async () => {
    const t = await target.ensure();
    const r = await api.createService(t.eid, { name: imageName(image), source: { type: "image", image: image.trim(), registryCredential: cred || undefined }, deployNow, position: target.position });
    target.done({ ...t, services: r.services, variables: !deployNow && !target.canvas });
  });
  return (
    <Modal title="Deploy a Docker image" onClose={onClose} width={520}>
      <form className="space-y-4 pb-2" onSubmit={(e) => { e.preventDefault(); if (!err) void create(!target.canvas); }}>
        <Field label="Image" error={touched && image ? err : undefined} hint="Examples: nginx:1.27, ghcr.io/owner/app:latest">
          <Input autoFocus value={image} onChange={(e) => { setImage(e.target.value); setTouched(true); }} placeholder="traefik/whoami:v1.10.1" aria-invalid={!!(touched && err)} />
        </Field>
        <RegistryCredentialPicker server={imageRegistry(image || "docker.io/x")} value={cred} onChange={setCred} />
        <ErrorLine error={a.error} />
        <div className="flex justify-end gap-2 py-2">
          <Button busy={a.busy === "stage"} disabled={!!err || !!a.busy} onClick={() => void create(false)}>{target.canvas ? "Add to canvas" : "Add Variables"}</Button>
          <Button variant="primary" busy={a.busy === "deploy"} disabled={!!err || !!a.busy} onClick={() => void create(true)}>Deploy Now</Button>
        </div>
      </form>
    </Modal>
  );
}

// ---------- Database ----------

/** Canvas entry: the database step of the create picker. */
export function DatabaseFlow({ target, onClose }: { target: Target; onClose: () => void }) {
  return <PickerOverlay step="database" target={target} onClose={onClose} />;
}

// ---------- Docker Compose ----------

/** Waits for an import to finish, polling every second for up to two minutes. */
export async function waitImport(imp: Import, onTick?: (i: Import) => void): Promise<Import> {
  let cur = imp;
  for (let i = 0; cur.status === "running" && i < 120; i++) {
    await new Promise((r) => setTimeout(r, 1000));
    cur = await api.importStatus(cur.id);
    onTick?.(cur);
  }
  if (cur.status === "failed") throw new ApiError(422, { error: cur.error || "The import failed.", code: "validation" }, "The import failed.");
  return cur;
}

export function ComposeFlow({ target, onClose, initial = "" }: { target: Target; onClose: () => void; initial?: string }) {
  const [yaml, setYaml] = useState(initial);
  const [report, setReport] = useState<{ imp: Import; pid: string; eid: string }>();
  const a = useAction();
  const run = () => a.run("import", async () => {
    const t = await target.ensure();
    const imp = await waitImport(await api.importCompose(t.eid, yaml));
    setReport({ imp, ...t });
  });
  if (report) return <ImportReportModal imp={report.imp} onClose={() => { target.done({ pid: report.pid, eid: report.eid, services: [] }); onClose(); }} />;
  return (
    <Modal title="Import Docker Compose" onClose={onClose} width={600}>
      <div className="space-y-3 pb-2">
        <p className="text-sm text-fg-2">Paste a docker-compose.yml or drop the file. Services, volumes and databases are staged; nothing deploys until you press Deploy.</p>
        <label onDragOver={(e) => e.preventDefault()} onDrop={(e) => { e.preventDefault(); const f = e.dataTransfer.files?.[0]; if (f) void f.text().then(setYaml); }}
          className="flex cursor-pointer items-center gap-3 rounded-lg border border-dashed border-line px-4 py-3 text-sm text-fg-2 transition-colors hover:border-fg-5 hover:bg-tint-hover">
          <FileUp size={16} className="text-fg-3" /><span className="flex-1">Choose or drop a compose file</span><span className="text-caption text-fg-4">.yml, .yaml</span>
          <input type="file" aria-label="Compose file" accept=".yml,.yaml" className="sr-only" onChange={(e) => { const f = e.target.files?.[0]; if (f) void f.text().then(setYaml); }} />
        </label>
        <Textarea aria-label="Compose YAML" rows={12} value={yaml} onChange={(e) => setYaml(e.target.value)} placeholder={"services:\n  web:\n    image: nginx:1.27"} />
        <ErrorLine error={a.error} />
      </div>
      <div className="flex justify-end gap-2 py-4">
        <Button variant="primary" busy={a.busy === "import"} disabled={!yaml.trim()} onClick={() => void run()}>Import</Button>
      </div>
    </Modal>
  );
}

const KIND_ICON: Record<string, ReactNode> = {
  service: <Box size={14} />, variable: <Braces size={14} />, domain: <Globe size={14} />, volume: <HardDrive size={14} />,
  database: <Database size={14} />, tcp_proxy: <Cable size={14} />, setting: <Settings2 size={14} />, key: <KeyRound size={14} />,
};

function ReportList({ title, items, tone }: { title: string; items: Import["report"]["landed"]; tone: "ok" | "warn" }) {
  return (
    <div>
      <div className="mb-2 flex items-center gap-2 text-caption font-semibold uppercase tracking-wide text-fg-4">
        {tone === "ok" ? <CircleCheck size={14} className="text-ok-text" /> : <CircleAlert size={14} className="text-warn-icon" />}{title} ({items.length})
      </div>
      <ul className="divide-y divide-line overflow-hidden rounded-lg border border-line">
        {items.map((i, n) => (
          <li key={n} className="flex items-start gap-3 px-3 py-2.5 text-sm animate-rise-in" style={{ animationDelay: `${Math.min(n, 12) * 25}ms`, animationFillMode: "both" }}>
            <span className="mt-0.5 shrink-0 text-fg-3">{KIND_ICON[i.kind] || <Box size={14} />}</span>
            <span className="min-w-0 flex-1">
              <span className="text-foreground">{i.name}</span>
              <span className="ml-2 text-caption text-fg-4">{i.kind.replace("_", " ")}</span>
              {i.detail && <span className="block text-caption text-fg-3">{i.detail}</span>}
            </span>
          </li>
        ))}
      </ul>
    </div>
  );
}

export function ImportReport({ imp }: { imp: Import }) {
  const { landed, skipped } = imp.report || { landed: [], skipped: [] };
  return (
    <div className="space-y-5 text-sm">
      <ReportList title="Staged" items={landed} tone="ok" />
      {skipped.length > 0 && <ReportList title="Skipped" items={skipped} tone="warn" />}
    </div>
  );
}

function ImportReportModal({ imp, onClose }: { imp: Import; onClose: () => void }) {
  return (
    <Modal title="Import report" onClose={onClose} width={560} footer={<Button variant="primary" onClick={onClose}>Review on the canvas</Button>}>
      <ImportReport imp={imp} />
    </Modal>
  );
}

// ---------- Empty service and volume (canvas) ----------

export async function createEmptyService(target: Target) {
  const t = await target.ensure();
  const r = await api.createService(t.eid, { source: { type: "empty" }, deployNow: false, position: target.position });
  target.done({ ...t, services: r.services });
}

export function VolumeFlow({ target, services, serviceId, onClose }: { target: Target; services: Service[]; serviceId?: string; onClose: () => void }) {
  const free = useMemo(() => services.filter((s) => !s.volume), [services]);
  const [sid, setSid] = useState(serviceId || free[0]?.id || "");
  const [mount, setMount] = useState("/data");
  const a = useAction();
  const create = () => a.run("create", async () => {
    const t = await target.ensure();
    await api.createVolume(t.eid, sid, { mountPath: mount.trim() });
    toast("Volume staged. Deploy to create it.");
    target.done({ ...t, services: [] });
    onClose();
  });
  return (
    <Modal title="Attach a volume" onClose={onClose} width={440}>
      <form className="space-y-4 pb-2" onSubmit={(e) => { e.preventDefault(); void create(); }}>
        <Field label="Service">
          <Select variant="field" value={sid} onChange={(e) => setSid(e.target.value)} options={free.map((s) => ({ value: s.id, label: s.config.name }))} />
        </Field>
        <Field label="Mount path" error={mount.startsWith("/") ? undefined : "The mount path starts with /."}><Input value={mount} onChange={(e) => setMount(e.target.value)} /></Field>
        <ErrorLine error={a.error} />
        <div className="flex justify-end py-2"><Button type="submit" variant="primary" busy={!!a.busy} disabled={!sid || !mount.startsWith("/")}>Attach Volume</Button></div>
      </form>
    </Modal>
  );
}
