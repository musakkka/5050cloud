// SPDX-License-Identifier: AGPL-3.0-only

// Service Settings (docs/SPEC.md 13.3): Source, Networking, Build, Deploy,
// Scale, Danger. Every field stages a merge patch of the service config and
// shows the staged colour; fields set by a config file are read-only.

import { createContext, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { FileCode } from "lucide-react";
import type { GitHubRepo, GitHubStatus, RegistryCredential, Service } from "../../api/types.gen";
import { del, get, patch, post, svcPath } from "../service/client";
import { Button, Confirm, ErrorLine, Input, Modal, NotInV01, cx, useAction, useLoad } from "../service/ui";
import { CRON_PRESETS, nextRuns, parseCron, validTimeZone } from "../cron/cron";
import Networking from "./Networking";
import { getPath, isStaged, normaliseRepo, patchFor, validImage } from "./patch";

interface Ctx {
  svc: Service;
  cfg: Service["config"];
  filter: string;
  stage: (path: string, value: unknown) => Promise<void>;
  stagePatch: (p: Record<string, unknown>) => Promise<void>;
}
const SettingsCtx = createContext<Ctx | null>(null);
const useCtx = () => useContext(SettingsCtx)!;

const SECTIONS = [
  { id: "source", label: "Source" },
  { id: "networking", label: "Networking" },
  { id: "build", label: "Build" },
  { id: "deploy", label: "Deploy" },
  { id: "scale", label: "Scale" },
  { id: "danger", label: "Danger" },
];

export default function SettingsTab({ eid, service, onService, onDeleted }: {
  eid: string; service: Service; onService: (s: Service) => void; onDeleted?: () => void;
}) {
  const base = svcPath(eid, service.id);
  const [filter, setFilter] = useState("");
  const [err, setErr] = useState<string | null>(null);
  const filterRef = useRef<HTMLInputElement>(null);
  const cfg = service.staged ?? service.config;

  useEffect(() => {
    const k = (e: KeyboardEvent) => {
      const t = e.target as HTMLElement;
      if (e.key === "/" && !["INPUT", "TEXTAREA", "SELECT"].includes(t.tagName)) { e.preventDefault(); filterRef.current?.focus(); }
    };
    window.addEventListener("keydown", k);
    return () => window.removeEventListener("keydown", k);
  }, []);

  const stagePatch = async (p: Record<string, unknown>) => {
    setErr(null);
    try { onService(await patch<Service>(`${base}/config`, { patch: p })); } catch (e) { setErr((e as Error).message); throw e; }
  };
  const ctx: Ctx = { svc: service, cfg, filter: filter.toLowerCase(), stagePatch, stage: (path, value) => stagePatch(patchFor(path, value)) };

  return (
    <SettingsCtx.Provider value={ctx}>
      <div className="flex gap-8">
        <div className="min-w-0 flex-1">
          <input ref={filterRef} aria-label="Filter settings" placeholder="Filter settings (press /)" value={filter} onChange={(e) => setFilter(e.target.value)} className="mb-6 h-[34px] w-full rounded-md border border-gray-200 bg-background px-3 text-sm outline-none" />
          {service.configFile && (
            <div className="mb-6 flex items-center gap-2 rounded-md border border-gray-200 px-3 py-2 text-sm text-gray-600">
              <FileCode size={14} />Some fields come from {service.configFile.url ? <a className="underline" href={service.configFile.url} target="_blank" rel="noreferrer">{service.configFile.path}</a> : service.configFile.path} and are read-only here.
            </div>
          )}
          <ErrorLine>{err}</ErrorLine>
          <Group id="source" title="Source"><SourceSection eid={eid} /></Group>
          <Group id="networking" title="Networking"><Networking eid={eid} service={service} field={(p) => <FieldRow {...p} />} /></Group>
          <Group id="build" title="Build"><BuildSection /></Group>
          <Group id="deploy" title="Deploy"><DeploySection /></Group>
          <Group id="scale" title="Scale"><ScaleSection /></Group>
          <Group id="danger" title="Danger"><DangerSection base={base} onDeleted={onDeleted} /></Group>
        </div>
        <nav aria-label="Settings sections" className="sticky top-0 hidden h-fit w-36 shrink-0 text-sm lg:block">
          {SECTIONS.map((s) => (
            <a key={s.id} href={`#settings-${s.id}`} className="block py-1 text-gray-600 hover:text-foreground"
              onClick={(e) => { e.preventDefault(); document.getElementById(`settings-${s.id}`)?.scrollIntoView({ behavior: "smooth" }); }}>{s.label}</a>
          ))}
        </nav>
      </div>
    </SettingsCtx.Provider>
  );
}

function Group({ id, title, children }: { id: string; title: string; children: ReactNode }) {
  return (
    <section id={`settings-${id}`} className="mb-10 scroll-mt-4">
      <h3 className="mb-3 border-b border-gray-200 pb-2 text-lg font-semibold">{title}</h3>
      <div className="flex flex-col gap-4">{children}</div>
    </section>
  );
}

export type FieldKind = "text" | "number" | "toggle" | "select" | "lines" | "textarea";
export interface FieldProps {
  label: string;
  path: string;
  kind?: FieldKind;
  options?: { value: string; label: string }[];
  placeholder?: string;
  help?: ReactNode;
  validate?: (v: string) => string | null;
  children?: ReactNode;
}

/** One config field: staged colour, config-file lock, and saving on blur or Enter. */
export function FieldRow({ label, path, kind = "text", options, placeholder, help, validate, children }: FieldProps) {
  const { svc, cfg, filter, stage } = useCtx();
  const value = getPath(cfg, path);
  const fileSrc = svc.source?.[path];
  const staged = isStaged(svc.config, svc.staged, path);
  const toText = (v: unknown) => (v === undefined || v === null ? "" : Array.isArray(v) ? v.join("\n") : String(v));
  const [text, setText] = useState(toText(value));
  const [busy, setBusy] = useState(false);
  useEffect(() => setText(toText(value)), [value]);
  if (filter && !label.toLowerCase().includes(filter) && !path.toLowerCase().includes(filter)) return null;
  const err = validate && text ? validate(text) : null;

  const save = async (raw: unknown) => {
    let v: unknown = raw;
    if (kind === "number") v = raw === "" ? "" : Number(raw);
    if (kind === "lines") v = String(raw).split("\n").map((s) => s.trim()).filter(Boolean);
    if (JSON.stringify(v) === JSON.stringify(value ?? (kind === "lines" ? [] : ""))) return;
    if (kind === "number" && v !== "" && !isFinite(v as number)) return;
    setBusy(true);
    try { await stage(path, v); } catch { setText(toText(value)); } finally { setBusy(false); }
  };

  const ro = !!fileSrc || busy;
  const cls = cx("h-[34px] w-full rounded-md border bg-background px-3 text-sm outline-none", staged ? "border-purple-400 text-purple-500" : "border-gray-200", ro && "opacity-70");
  let control: ReactNode;
  if (kind === "toggle") {
    control = (
      <input type="checkbox" aria-label={label} disabled={ro} checked={!!value} onChange={(e) => void save(e.target.checked)}
        className={cx("h-4 w-4", staged && "accent-purple-500")} />
    );
  } else if (kind === "select") {
    control = (
      <select aria-label={label} disabled={ro} value={toText(value)} onChange={(e) => void save(e.target.value)} className={cls}>
        {options?.map((o) => <option key={o.value} value={o.value}>{o.label}</option>)}
      </select>
    );
  } else if (kind === "lines" || kind === "textarea") {
    control = (
      <textarea aria-label={label} disabled={ro} rows={3} value={text} placeholder={placeholder} onChange={(e) => setText(e.target.value)}
        onBlur={() => void save(text)} className={cx(cls, "h-auto py-1.5 font-mono")} />
    );
  } else {
    control = (
      <input aria-label={label} disabled={ro} type={kind === "number" ? "number" : "text"} value={text} placeholder={placeholder}
        onChange={(e) => setText(e.target.value)} onBlur={() => { if (!err) void save(text); }}
        onKeyDown={(e) => { if (e.key === "Enter" && !err) (e.target as HTMLInputElement).blur(); }} className={cls} />
    );
  }
  return (
    <div className={cx("flex flex-col gap-1", kind === "toggle" && "flex-row items-center justify-between")}>
      <label className="flex items-center gap-2 text-sm font-medium">
        {label}
        {fileSrc && <span title={`Set by ${fileSrc}`} aria-label={`Set by ${fileSrc}`}><FileCode size={13} className="text-gray-500" /></span>}
      </label>
      {control}
      {err && <span className="text-xs text-red-600">{err}</span>}
      {help && <span className="text-xs text-gray-500">{help}</span>}
      {children}
    </div>
  );
}

function SourceSection({ eid }: { eid: string }) {
  const { svc, cfg, stagePatch } = useCtx();
  const type = cfg.source.type || "empty";
  const [mode, setMode] = useState<"github" | "image">(type === "image" ? "image" : "github");
  const [repo, setRepo] = useState(cfg.source.repo || "");
  const [image, setImage] = useState(cfg.source.image || "");
  const [addCred, setAddCred] = useState(false);
  const a = useAction();
  const gh = useLoad(() => get<GitHubStatus>("/github").catch(() => null), []);
  const repos = useLoad(() => get<GitHubRepo[] | { items: GitHubRepo[] }>("/github/repos").then((r) => (Array.isArray(r) ? r : r?.items || [])).catch(() => [] as GitHubRepo[]), []);
  const creds = useLoad(() => get<RegistryCredential[]>("/registry-credentials").catch(() => [] as RegistryCredential[]), []);
  const imgErr = mode === "image" && image ? validImage(image) : null;
  const repoNorm = normaliseRepo(repo);

  return (
    <>
      <div className="flex gap-2 text-sm">
        {(["github", "image"] as const).map((m) => (
          <button key={m} onClick={() => setMode(m)} className={cx("rounded-md border px-3 py-1", mode === m ? "border-pink-500 text-pink-600" : "border-gray-200 text-gray-600")}>
            {m === "github" ? "Repository" : "Docker image"}
          </button>
        ))}
        {type !== "empty" && (
          <Button variant="ghost" className="ml-auto" onClick={() => void a.run(() => stagePatch({ source: { type: "empty", repo: null, image: null, branch: null, registryCredential: null } }))}>Disconnect</Button>
        )}
      </div>
      {mode === "github" ? (
        <>
          <label className="text-sm font-medium">Source repo
            <div className="mt-1 flex gap-2">
              <Input list="fifty-repos" aria-label="Source repo" placeholder="owner/name or https://github.com/owner/name" value={repo} onChange={(e) => setRepo(e.target.value)} />
              <datalist id="fifty-repos">{(repos.data || []).map((r) => <option key={r.fullName} value={r.fullName} />)}</datalist>
              <Button disabled={!!repoNorm.error || a.busy || (type === "github" && repoNorm.repo === cfg.source.repo)}
                onClick={() => void a.run(() => stagePatch({ source: { type: "github", repo: repoNorm.repo, image: null, registryCredential: null } }))}>
                {type === "github" ? "Change" : "Connect"}
              </Button>
              <Button variant="ghost" onClick={() => void repos.reload()}>Refresh</Button>
            </div>
            {repo && repoNorm.error && <span className="text-xs text-red-600">{repoNorm.error}</span>}
          </label>
          {gh.data && !gh.data.appConfigured && (
            <div className="text-xs text-gray-500">Private repos need the GitHub App. Connect GitHub from New, GitHub Repository; public repos work with a pasted URL.</div>
          )}
          {gh.data?.configureUrl && <a className="text-xs underline" href={gh.data.configureUrl} target="_blank" rel="noreferrer">Configure GitHub App</a>}
          {type === "github" && <>
            <FieldRow label="Branch" path="source.branch" placeholder="default branch" />
            <FieldRow label="Root directory" path="source.rootDir" placeholder="/" />
            <FieldRow label="Auto deploy" path="source.autoDeploy" kind="toggle" help="Deploy when the branch gets a new commit." />
          </>}
        </>
      ) : (
        <>
          <label className="text-sm font-medium">Image
            <div className="mt-1 flex gap-2">
              <Input aria-label="Image" placeholder="nginx:1.27 or ghcr.io/owner/app:tag" value={image} onChange={(e) => setImage(e.target.value)} />
              <Button disabled={!!imgErr || !image || a.busy || (type === "image" && image === cfg.source.image)}
                onClick={() => void a.run(() => stagePatch({ source: { type: "image", image: image.trim(), repo: null, branch: null } }))}>
                {type === "image" ? "Change" : "Connect"}
              </Button>
            </div>
            {imgErr && <span className="text-xs text-red-600">{imgErr}</span>}
          </label>
          <label className="text-sm font-medium">Registry credential
            <div className="mt-1 flex gap-2">
              <select aria-label="Registry credential" value={cfg.source.registryCredential || ""} className="h-[34px] flex-1 rounded-md border border-gray-200 bg-background px-2 text-sm"
                onChange={(e) => void a.run(() => stagePatch({ source: { registryCredential: e.target.value || null } }))}>
                <option value="">None (public image)</option>
                {(creds.data || []).map((c) => <option key={c.id} value={c.id}>{c.name} ({c.server})</option>)}
              </select>
              <Button onClick={() => setAddCred(true)}>Add credential</Button>
            </div>
          </label>
          {type === "image" && (
            <div><Button disabled={a.busy} onClick={() => void a.run(() => post(`${svcPath(eid, svc.id)}/deploy`, { ref: "latest" }))}>Check for a newer image</Button></div>
          )}
        </>
      )}
      <FieldRow label="Config file path" path="build.configPath" placeholder="5050.yaml, railway.json or railway.toml" />
      <NotInV01 label="Wait for CI" instead="deploy from CI with 5050 up" />
      <NotInV01 label="Auto Updates" instead="use Check for a newer image" />
      <ErrorLine>{a.error}</ErrorLine>
      {addCred && <AddCredential onClose={() => setAddCred(false)} onAdded={async (c) => {
        await creds.reload();
        await stagePatch({ source: { registryCredential: c.id } });
      }} />}
    </>
  );
}

function AddCredential({ onClose, onAdded }: { onClose: () => void; onAdded: (c: RegistryCredential) => Promise<void> }) {
  const [f, setF] = useState({ name: "", server: "ghcr.io", username: "", password: "" });
  const a = useAction();
  const ok = f.name && f.server && f.username && f.password;
  return (
    <Modal title="Add registry credential" onClose={onClose} footer={<>
      <Button onClick={onClose}>Cancel</Button>
      <Button variant="primary" disabled={!ok || a.busy} onClick={async () => {
        if (await a.run(async () => onAdded(await post<RegistryCredential>("/registry-credentials", f)))) onClose();
      }}>Add</Button>
    </>}>
      {(["name", "server", "username", "password"] as const).map((k) => (
        <label key={k} className="mb-3 block capitalize">{k === "password" ? "Password or token" : k}
          <Input type={k === "password" ? "password" : "text"} value={f[k]} onChange={(e) => setF({ ...f, [k]: e.target.value })} autoComplete="off" />
        </label>
      ))}
      <ErrorLine>{a.error}</ErrorLine>
    </Modal>
  );
}

function BuildSection() {
  return (
    <>
      <FieldRow label="Builder" path="build.builder" kind="select" options={[
        { value: "", label: "Auto (Dockerfile if present, else Railpack)" },
        { value: "railpack", label: "Railpack" },
        { value: "dockerfile", label: "Dockerfile" },
        { value: "nixpacks", label: "Nixpacks (built with Railpack)" },
      ]} />
      <FieldRow label="Railpack version" path="build.railpackVersion" placeholder="current" />
      <FieldRow label="Dockerfile path" path="build.dockerfilePath" placeholder="Dockerfile" />
      <FieldRow label="Custom build command" path="build.buildCommand" />
      <FieldRow label="Watch paths" path="build.watchPaths" kind="lines" placeholder="one per line, for example src/**" help="Only commits that change these paths deploy." />
      <FieldRow label="Build without cache" path="build.noCache" kind="toggle" />
      <FieldRow label="Build timeout (minutes)" path="build.timeoutMin" kind="number" placeholder="30" />
    </>
  );
}

function CronField() {
  const { cfg, filter } = useCtx();
  const expr = cfg.deploy.cronSchedule || "";
  const tz = cfg.deploy.cronTimezone || "UTC";
  const parsed = useMemo(() => (expr ? parseCron(expr) : null), [expr]);
  const runs = useMemo(() => (parsed?.ok && validTimeZone(tz) ? nextRuns(parsed.cron, 3, Date.now(), tz) : []), [parsed, tz]);
  const { stage } = useCtx();
  if (filter && !"cron schedule".includes(filter)) return null;
  return (
    <div className="rounded-md border border-gray-200 p-3">
      <FieldRow label="Cron schedule" path="deploy.cronSchedule" placeholder="0 * * * *" validate={(v) => { const r = parseCron(v); return r.ok ? null : r.error; }}>
        <div className="flex flex-wrap gap-2">
          {CRON_PRESETS.map((p) => <Button key={p.value} variant="ghost" onClick={() => void stage("deploy.cronSchedule", p.value)}>{p.label}</Button>)}
        </div>
        {runs.length > 0 && (
          <div className="text-xs text-gray-600">Next runs: {runs.map((t) => new Date(t).toLocaleString()).join(", ")}</div>
        )}
      </FieldRow>
      {expr && <>
        <div className="mt-3"><FieldRow label="Timezone" path="deploy.cronTimezone" placeholder="UTC" validate={(v) => (validTimeZone(v) ? null : "Unknown time zone, for example Europe/Berlin")} /></div>
        <div className="mt-3"><FieldRow label="When a run is still going" path="deploy.cronOverlap" kind="select" options={[{ value: "", label: "Skip the next run" }, { value: "queue", label: "Queue it" }]} /></div>
        <div className="mt-3"><FieldRow label="Run timeout (seconds, 0 = none)" path="deploy.cronTimeoutSec" kind="number" /></div>
      </>}
    </div>
  );
}

function DeploySection() {
  const { cfg } = useCtx();
  return (
    <>
      <FieldRow label="Start command" path="deploy.startCommand" />
      <FieldRow label="Pre-deploy command" path="deploy.preDeployCommand" placeholder="for example npm run migrate" />
      {cfg.deploy.preDeployCommand && <FieldRow label="Pre-deploy timeout (seconds)" path="deploy.preDeployTimeoutSec" kind="number" placeholder="300" />}
      <CronField />
      <FieldRow label="Health check path" path="deploy.healthcheckPath" placeholder="empty = TCP check on the target port" />
      <FieldRow label="Health check timeout (seconds)" path="deploy.healthcheckTimeoutSec" kind="number" placeholder="300" />
      <h4 className="mt-2 text-sm font-semibold">Resource Limits</h4>
      <FieldRow label="vCPU per replica" path="deploy.cpuLimit" kind="number" placeholder="0 = unlimited" />
      <FieldRow label="Memory per replica (MB)" path="deploy.memoryLimitMb" kind="number" placeholder="0 = unlimited" />
      <FieldRow label="Memory reservation (MB)" path="deploy.memoryReservationMb" kind="number" placeholder="the limit, else 256" />
      <h4 className="mt-2 text-sm font-semibold">Restart Policy</h4>
      <FieldRow label="Restart policy" path="deploy.restartPolicy" kind="select" options={[
        { value: "", label: "On failure" }, { value: "always", label: "Always" }, { value: "never", label: "Never" },
      ]} />
      <FieldRow label="Max retries" path="deploy.restartMaxRetries" kind="number" placeholder="10" />
      <h4 className="mt-2 text-sm font-semibold">Teardown</h4>
      <FieldRow label="Overlap (seconds)" path="deploy.overlapSec" kind="number" placeholder="0" />
      <FieldRow label="Draining (seconds)" path="deploy.drainingSec" kind="number" placeholder="30" />
      <FieldRow label="Run as user" path="deploy.runAsUser" placeholder="image default, for example 1000:1000" />
      <FieldRow label="Shared memory size (MB)" path="deploy.shmSizeMb" kind="number" placeholder="64" />
      <NotInV01 label="Serverless" instead="set replicas to 0 by removing the deployment" />
    </>
  );
}

function ScaleSection() {
  const { svc, cfg, stage, filter } = useCtx();
  const auto = cfg.deploy.processes === "auto";
  const hosts = [...new Set(svc.replicas.map((r) => r.hostName))];
  return (
    <>
      <FieldRow label="Replicas" path="deploy.replicas" kind="number" placeholder="1" help={svc.volume ? "A service with a volume runs one replica." : undefined} />
      <FieldRow label="Max surge" path="deploy.maxSurge" placeholder="1 or 25%" />
      <FieldRow label="Singleton" path="deploy.singleton" kind="toggle" help="Never moved to another host automatically while its host is offline." />
      {(!filter || "processes use all cores".includes(filter)) && (
        <div className="flex flex-col gap-1">
          <label className="flex items-center justify-between text-sm font-medium">Use all cores
            <input type="checkbox" checked={auto} onChange={(e) => void stage("deploy.processes", e.target.checked ? "auto" : "")} />
          </label>
          {!auto && <FieldRow label="Processes" path="deploy.processes" placeholder="1" />}
          <span className="text-xs text-gray-500">
            {svc.processesText || `Runs ${svc.processes || 1} process${svc.processes === 1 ? "" : "es"} per replica.`} Node apps started with node file.js run in cluster mode; every runtime gets WEB_CONCURRENCY.
          </span>
        </div>
      )}
      <FieldRow label="Placement roles" path="deploy.placement.roles" kind="lines" placeholder="app" help="One role per line. Pinned hosts override roles." />
      <FieldRow label="Pinned hosts" path="deploy.placement.hosts" kind="lines" placeholder="host ids, one per line" />
      <div className="text-xs text-gray-600">Running now on: {hosts.length ? hosts.join(", ") : "no hosts"}</div>
    </>
  );
}

function DangerSection({ base, onDeleted }: { base: string; onDeleted?: () => void }) {
  const { svc, filter } = useCtx();
  const [open, setOpen] = useState(false);
  if (filter && !"delete service".includes(filter)) return null;
  return (
    <div className="flex items-center justify-between rounded-md border border-red-300 p-3">
      <div className="text-sm">Delete this service. The deletion is staged until you deploy.</div>
      <Button variant="danger" onClick={() => setOpen(true)}>Delete service</Button>
      {open && <Confirm title="Delete service" danger confirm="Delete" typeName={svc.config.name}
        body={<>This stages deletion of <b>{svc.config.name}</b>, its domains and its volume.</>}
        onClose={() => setOpen(false)} onConfirm={async () => { await del(base, { confirm: svc.config.name }); onDeleted?.(); }} />}
    </div>
  );
}
