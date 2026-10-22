// SPDX-License-Identifier: AGPL-3.0-only

// Creation flows shared by /new (new project) and the canvas New menu
// (current environment): GitHub repo with the scan dialog, Docker image,
// database, Docker Compose, empty service and volume.
import { Fragment, useEffect, useMemo, useState } from "react";
import { ExternalLink, Github, Lock, RefreshCw } from "lucide-react";
import { ApiError, api, type GitHubRepo, type GitHubStatus, type Import, type RegistryCredential, type ScanResult, type Service, type TemplateSummary } from "../../api/client";
import { Button, ErrorLine, Field, Input, Modal, ServiceIcon, Spinner, Textarea, cx, toast } from "../../kit";
import { imageError, imageName, imageRegistry, repoRef } from "./image";

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

export function GitHubFlow({ target, onClose }: { target: Target; onClose: () => void }) {
  const [status, setStatus] = useState<GitHubStatus>();
  const [repos, setRepos] = useState<GitHubRepo[]>();
  const [q, setQ] = useState("");
  const [url, setUrl] = useState("");
  const [picked, setPicked] = useState<{ repo: string; defaultBranch?: string }>();
  const [error, setError] = useState<unknown>();
  const [refreshing, setRefreshing] = useState(false);

  useEffect(() => { api.github().then(setStatus, () => setStatus({ appConfigured: false, installations: [] })); }, []);
  const loadRepos = (refresh = false) => {
    setRefreshing(refresh);
    return api.repos(q || undefined, refresh).then(setRepos, setError).finally(() => setRefreshing(false));
  };
  useEffect(() => {
    if (!status?.appConfigured) return;
    const t = setTimeout(() => void loadRepos(), 200);
    return () => clearTimeout(t);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [status, q]);

  const connect = async () => {
    try {
      const r = await api.githubApp(location.pathname + location.search);
      // GitHub's app manifest flow: POST the manifest as a form field to formUrl
      const f = document.createElement("form");
      f.method = "post"; f.action = r.formUrl;
      const i = document.createElement("input");
      i.type = "hidden"; i.name = "manifest"; i.value = typeof r.manifest === "string" ? r.manifest : JSON.stringify(r.manifest);
      f.appendChild(i); document.body.appendChild(f); f.submit();
    } catch (e) { setError(e); }
  };

  if (picked) return <ScanDialog target={target} repo={picked.repo} defaultBranch={picked.defaultBranch} onBack={() => setPicked(undefined)} onClose={onClose} />;
  const pasted = repoRef(url);
  return (
    <Modal title="Deploy from GitHub" onClose={onClose} width={560}>
      <div className="space-y-4 pb-4">
        {!status && <Spinner />}
        {status?.appConfigured && (
          <div className="space-y-2">
            <div className="flex gap-2">
              <Input aria-label="Search repositories" placeholder="Search repositories" value={q} onChange={(e) => setQ(e.target.value)} autoFocus />
              <Button aria-label="Refresh" title="Refresh" onClick={() => void loadRepos(true)}><RefreshCw size={14} className={cx(refreshing && "spin")} />Refresh</Button>
            </div>
            <ul className="max-h-64 overflow-y-auto rounded-md border border-gray-200 scroll-thin">
              {!repos && <li className="p-3"><Spinner /></li>}
              {repos?.length === 0 && <li className="p-3 text-sm text-gray-500">No repositories. Give the GitHub App access to more of them.</li>}
              {repos?.map((r) => (
                <li key={r.fullName}>
                  <button type="button" onClick={() => setPicked({ repo: r.fullName, defaultBranch: r.defaultBranch })} className="flex w-full items-center gap-2 px-3 py-2 text-left text-sm hover:bg-gray-100">
                    <Github size={14} className="text-gray-500" /><span className="flex-1 truncate">{r.fullName}</span>{r.private && <Lock size={12} className="text-gray-500" />}
                  </button>
                </li>
              ))}
            </ul>
            {status.configureUrl && <a href={status.configureUrl} target="_blank" rel="noreferrer" className="inline-flex items-center gap-1 text-sm text-gray-500 hover:text-foreground">Configure GitHub App<ExternalLink size={12} /></a>}
          </div>
        )}
        {status && !status.appConfigured && (
          <div className="flex items-center justify-between gap-3 rounded-md border border-gray-200 px-3 py-3 text-sm">
            <span className="text-gray-600">Connect a GitHub App to deploy private repositories and deploy on push.</span>
            <Button onClick={() => void connect()}><Github size={14} />Connect GitHub</Button>
          </div>
        )}
        <form className="space-y-2" onSubmit={(e) => { e.preventDefault(); if (pasted) setPicked({ repo: pasted }); }}>
          <Field label="Paste a public repo URL">
            <div className="flex gap-2">
              <Input placeholder="https://github.com/owner/repo" value={url} onChange={(e) => setUrl(e.target.value)} />
              <Button type="submit" disabled={!/^https:\/\/github\.com\/[\w.-]+\/[\w.-]+$/.test(pasted)}>Continue</Button>
            </div>
          </Field>
        </form>
        <ErrorLine error={error} />
      </div>
    </Modal>
  );
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

  return (
    <Modal title={repo.replace(/^https:\/\/github\.com\//, "")} onClose={onClose} width={560}>
      <div className="space-y-4 pb-2">
        <div className="grid gap-3 sm:grid-cols-2">
          <Field label="Branch">
            {branches && branches.length ? (
              <select value={branch} onChange={(e) => setBranch(e.target.value)} className="h-[38px] w-full rounded-md border border-gray-200 bg-background px-2 text-sm">
                {branches.map((b) => <option key={b}>{b}</option>)}
              </select>
            ) : <Input value={branch} placeholder="default branch" onChange={(e) => setBranch(e.target.value)} />}
          </Field>
          <Field label="Root directory"><Input value={rootDir} placeholder="/" onChange={(e) => setRootDir(e.target.value)} /></Field>
        </div>
        {!scan && !scanErr && <div className="flex items-center gap-2 text-sm text-gray-500"><Spinner />Scanning the repository</div>}
        <ErrorLine error={scanErr} />
        {scan && (
          <dl className="grid grid-cols-[auto,1fr] gap-x-4 gap-y-1.5 rounded-md border border-gray-200 px-4 py-3 text-sm">
            <dt className="text-gray-500">Builder</dt><dd>{scan.dockerfile ? "Dockerfile" : `Railpack${scan.provider ? ` (${scan.provider})` : ""}`}</dd>
            {scan.startCommand && <><dt className="text-gray-500">Start command</dt><dd className="font-mono text-xs leading-5">{scan.startCommand}</dd></>}
            {scan.suggestedPort ? <><dt className="text-gray-500">Port</dt><dd>{scan.suggestedPort}</dd></> : null}
            {scan.configFiles?.map((c) => <Fragment key={c.path}><dt className="text-gray-500">Config file</dt><dd>{c.path} <span className="text-gray-500">sets {c.sets.length} fields</span></dd></Fragment>)}
            {scan.envNames.length > 0 && <><dt className="text-gray-500">Variables</dt><dd className="break-words font-mono text-xs leading-5">{scan.envNames.join(", ")}</dd></>}
          </dl>
        )}
        {scan?.packages && scan.packages.length > 0 && (
          <fieldset className="space-y-1">
            <legend className="mb-1 text-sm font-medium">Packages to deploy (one service each)</legend>
            {scan.packages.map((p) => (
              <label key={p.rootDir} className="flex items-center gap-2 text-sm">
                <input type="checkbox" checked={ticked.includes(p.rootDir)} onChange={(e) => setTicked(e.target.checked ? [...ticked, p.rootDir] : ticked.filter((x) => x !== p.rootDir))} />
                <span className="font-medium">{p.name}</span><span className="text-gray-500">{p.rootDir}</span>
              </label>
            ))}
          </fieldset>
        )}
        <ErrorLine error={a.error} />
      </div>
      <div className="flex flex-wrap justify-end gap-2 py-4">
        {onBack && <Button variant="ghost" className="mr-auto" onClick={onBack}>Back</Button>}
        <Button busy={a.busy === "stage"} disabled={!!a.busy} onClick={() => void create(false)}>{target.canvas ? "Add to canvas" : "Add Variables"}</Button>
        <Button variant="primary" busy={a.busy === "deploy"} disabled={!!a.busy} onClick={() => void create(true)}>Deploy Now</Button>
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
          <select aria-label="Registry credential" value={value} onChange={(e) => onChange(e.target.value)} className="h-[38px] flex-1 rounded-md border border-gray-200 bg-background px-2 text-sm">
            <option value="">None (public image)</option>
            {list?.map((c) => <option key={c.id} value={c.id}>{c.name} ({c.username}@{c.server})</option>)}
          </select>
          <Button onClick={() => setAdding((x) => !x)}>Add credential</Button>
        </div>
      </Field>
      {adding && (
        <div className="space-y-2 rounded-md border border-gray-200 p-3">
          <div className="text-xs text-gray-500">For {server}</div>
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

export function ImageFlow({ target, onClose }: { target: Target; onClose: () => void }) {
  const [image, setImage] = useState("");
  const [cred, setCred] = useState("");
  const [touched, setTouched] = useState(false);
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

export function DatabaseFlow({ target, onClose }: { target: Target; onClose: () => void }) {
  const [list, setList] = useState<TemplateSummary[]>();
  const a = useAction();
  useEffect(() => { api.templates().then((l) => setList(l.filter((t) => t.category === "database")), a.setError); }, [a.setError]);
  const create = (t: TemplateSummary) => a.run(t.id, async () => {
    const tgt = await target.ensure();
    const r = await api.createService(tgt.eid, { template: t.id, source: { type: "empty" }, deployNow: true, position: target.position });
    target.done({ ...tgt, services: r.services });
  });
  return (
    <Modal title="Add a database" onClose={onClose} width={480}>
      <div className="space-y-2 pb-5">
        {!list && !a.error && <Spinner />}
        {list?.map((t) => (
          <button type="button" key={t.id} disabled={!!a.busy} onClick={() => void create(t)}
            className="flex w-full items-center gap-3 rounded-md border border-gray-200 px-3 py-2.5 text-left hover:border-gray-400 disabled:opacity-50">
            <ServiceIcon icon={t.icon} size={22} />
            <span className="flex-1"><span className="block text-sm font-medium">{t.name}</span><span className="block text-xs text-gray-500">{t.tagline}</span></span>
            {a.busy === t.id && <Spinner />}
          </button>
        ))}
        <ErrorLine error={a.error} />
      </div>
    </Modal>
  );
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
        <p className="text-sm text-gray-600">Paste a docker-compose.yml or choose the file. Services, volumes and databases are staged; nothing deploys until you press Deploy.</p>
        <input type="file" aria-label="Compose file" accept=".yml,.yaml" onChange={(e) => { const f = e.target.files?.[0]; if (f) void f.text().then(setYaml); }} className="text-sm" />
        <Textarea aria-label="Compose YAML" rows={12} value={yaml} onChange={(e) => setYaml(e.target.value)} placeholder={"services:\n  web:\n    image: nginx:1.27"} />
        <ErrorLine error={a.error} />
      </div>
      <div className="flex justify-end gap-2 py-4">
        <Button variant="primary" busy={a.busy === "import"} disabled={!yaml.trim()} onClick={() => void run()}>Import</Button>
      </div>
    </Modal>
  );
}

export function ImportReport({ imp }: { imp: Import }) {
  const { landed, skipped } = imp.report || { landed: [], skipped: [] };
  return (
    <div className="space-y-4 text-sm">
      <div>
        <h3 className="mb-1 font-medium">Staged ({landed.length})</h3>
        <ul className="space-y-0.5">{landed.map((i, n) => <li key={n}><span className="text-gray-500">{i.kind}</span> {i.name}{i.detail && <span className="text-gray-500">: {i.detail}</span>}</li>)}</ul>
      </div>
      {skipped.length > 0 && (
        <div>
          <h3 className="mb-1 font-medium">Skipped ({skipped.length})</h3>
          <ul className="space-y-0.5">{skipped.map((i, n) => <li key={n}><span className="text-gray-500">{i.kind}</span> {i.name}{i.detail && <span className="text-gray-500">: {i.detail}</span>}</li>)}</ul>
        </div>
      )}
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
          <select value={sid} onChange={(e) => setSid(e.target.value)} className="h-[38px] w-full rounded-md border border-gray-200 bg-background px-2 text-sm">
            {free.map((s) => <option key={s.id} value={s.id}>{s.config.name}</option>)}
          </select>
        </Field>
        <Field label="Mount path" error={mount.startsWith("/") ? undefined : "The mount path starts with /."}><Input value={mount} onChange={(e) => setMount(e.target.value)} /></Field>
        <ErrorLine error={a.error} />
        <div className="flex justify-end py-2"><Button type="submit" variant="primary" busy={!!a.busy} disabled={!sid || !mount.startsWith("/")}>Attach Volume</Button></div>
      </form>
    </Modal>
  );
}
