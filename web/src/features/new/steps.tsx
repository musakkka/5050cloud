// SPDX-License-Identifier: AGPL-3.0-only

// The steps of the create picker: root, GitHub repository, database,
// template and Docker image. Each renders its own input and list inside the
// shared PaletteFrame; NewPage owns the URL and the step direction.
import { useEffect, useMemo, useState, type ReactNode } from "react";
import { useNavigate } from "react-router-dom";
import {
  Archive, ArrowUpRight, BadgeCheck, Boxes, CircleAlert, Database, HardDrive, Info, KeyRound, Layers, Lock, RefreshCw,
  Search, SearchX, Settings, Sparkles, SquareTerminal, TrainFront,
} from "lucide-react";
import { api, type GitHubRepo, type GitHubStatus, type TemplateSummary } from "../../api/client";
import { BrandIcon, Button, Skeleton, Spinner, cx, toastError } from "../../kit";
import { PaletteCard, PaletteInput, PaletteList, PaletteStep, useListNav, type PItem, type StepDir } from "./Palette";
import { categoryLabel, fuzzyFilter, loadGitHub, loadRepos, loadTemplates, pastedRepo, registryPage, sortDatabases, sortPopular } from "./data";
import { imageError, imageName } from "./image";
import type { Target } from "./flows";

export type StepId = "root" | "github" | "database" | "template" | "image";

/** Modals the picker can open over the canvas. */
export type PickerModal =
  | { kind: "scan"; repo: string; defaultBranch?: string }
  | { kind: "image"; image: string }
  | { kind: "compose" };

export interface StepProps {
  dir: StepDir;
  /** Text carried over from the previous step ("Search templates for ..."). */
  seed?: string;
  go: (s: StepId, seed?: string) => void;
  back: () => void;
  target: Target;
  open: (m: PickerModal) => void;
  /** False for viewers: the picker explains instead of creating. */
  canCreate: boolean;
}

const TALL_MAX = "min(536px, calc(100vh - 360px))";

/** Runs one creation at a time and remembers which row is busy. */
function useBusy() {
  const [busy, setBusy] = useState<string>();
  const run = async (key: string, fn: () => Promise<void>) => {
    if (busy) return;
    setBusy(key);
    try { await fn(); } catch (e) { toastError(e); } finally { setBusy(undefined); }
  };
  return { busy, run };
}

const spin = (on: boolean) => (on ? <Spinner size={14} className="shrink-0" /> : undefined);

function LoadingRows({ n = 4, tall }: { n?: number; tall?: boolean }) {
  return (
    <div className="px-2" aria-busy>
      {Array.from({ length: n }, (_, i) => (
        <div key={i} className={cx("flex gap-4 p-3", tall ? "items-start" : "items-center")}>
          <Skeleton w={tall ? 20 : 16} h={tall ? 20 : 16} className="rounded" />
          <div className="flex-1 space-y-2">
            <Skeleton h={12} w={`${60 - i * 7}%`} />
            {tall && <Skeleton h={10} w={`${80 - i * 5}%`} />}
          </div>
        </div>
      ))}
    </div>
  );
}

function ViewerCard() {
  return <PaletteCard icon={<Lock size={24} />} title="View access only" body="Ask an admin for member access to create projects." />;
}

// ---------------------------------------------------------------- root

/** Image references that are clearly not a search ("redis:7", "ghcr.io/o/app"). */
const looksLikeImage = (s: string) => /[:/]/.test(s.trim()) && !/^https?:/i.test(s.trim()) && !imageError(s);

export function RootStep({ dir, go, target, open, canCreate, seed }: StepProps) {
  const nav = useNavigate();
  const [q, setQ] = useState(seed || "");
  const { busy, run } = useBusy();
  const repo = pastedRepo(q);

  const base: PItem[] = [
    { key: "github", icon: <BrandIcon name="github" size={16} mono />, label: "GitHub Repository", chevron: true, onPick: () => go("github") },
    { key: "database", icon: <Database size={16} />, label: "Database", chevron: true, onPick: () => go("database") },
    { key: "template", icon: <Layers size={16} />, label: "Template", chevron: true, onPick: () => go("template") },
    { key: "image", icon: <BrandIcon name="docker" size={16} mono />, label: "Docker Image", chevron: true, onPick: () => go("image") },
    { key: "bucket", icon: <Archive size={16} />, label: "Bucket", onPick: () => nav("/new/template/minio") },
    {
      key: "empty", icon: <SquareTerminal size={16} />, label: "Empty Project", right: spin(busy === "empty"),
      onPick: () => void run("empty", async () => { const t = await target.ensure(); target.done({ ...t, services: [] }); }),
    },
    { key: "compose", icon: <Boxes size={16} />, label: "Docker Compose", divider: true, onPick: () => open({ kind: "compose" }) },
    { key: "import", icon: <TrainFront size={16} />, label: "Import a project", onPick: () => nav("/new/import") },
  ];
  const text = q.trim();
  const items: PItem[] = repo ? [] : text ? [
    looksLikeImage(text)
      ? { key: "suggest-image", icon: <BrandIcon name="docker" size={16} />, label: <>Deploy image <span className="font-mono text-[13px]">{text}</span></>, enter: true, onPick: () => go("image", text) }
      : { key: "suggest-template", icon: <Sparkles size={16} />, accent: true, label: <>Search templates for <span className="text-foreground">&ldquo;{text}&rdquo;</span></>, enter: true, onPick: () => go("template", text) },
    ...fuzzyFilter(base, text, (i) => String(i.label)).map((i) => ({ ...i, divider: false })),
  ] : base;

  const deployRepo = () => repo && open({ kind: "scan", repo: `https://github.com/${repo}` });
  const navi = useListNav(items, () => (q ? setQ("") : nav("/")));
  return (
    <PaletteStep dir={dir}>
      <PaletteInput value={q} onChange={setQ} label="Describe what to create" placeholder="Search, or paste a GitHub repo or image link"
        icon={<Sparkles size={16} />} listId="new-root" activeId={`new-root-${navi.active}`}
        onKeyDown={(e) => { if (repo && e.key === "Enter") { e.preventDefault(); deployRepo(); } else navi.onKeyDown(e); }} />
      {!canCreate ? <ViewerCard /> : repo ? (
        <PaletteCard icon={<BrandIcon name="github" size={32} mono />} title={repo} body="Deploy this repository in a new project."
          action={<Button variant="primary" onClick={deployRepo}>Deploy Repo</Button>} />
      ) : (
        <PaletteList id="new-root" items={items} active={navi.active} onHover={navi.hover} byKey={navi.byKey} maxHeight={TALL_MAX} />
      )}
    </PaletteStep>
  );
}

// ---------------------------------------------------------------- GitHub

export function GitHubStep({ dir, back, open, canCreate }: StepProps) {
  const [q, setQ] = useState("");
  const [status, setStatus] = useState<GitHubStatus>();
  const [repos, setRepos] = useState<GitHubRepo[]>();
  const [refreshing, setRefreshing] = useState(false);

  useEffect(() => { loadGitHub().then(setStatus, () => setStatus({ appConfigured: false, installations: [] })); }, []);
  const load = (refresh = false) => {
    setRefreshing(refresh);
    return loadRepos(refresh).then(setRepos, (e) => { toastError(e); setRepos((r) => r || []); }).finally(() => setRefreshing(false));
  };
  useEffect(() => { if (status?.appConfigured) void load(); }, [status]);

  const connect = async () => {
    try {
      const r = await api.githubApp(location.pathname + location.search);
      const f = document.createElement("form");
      f.method = "post"; f.action = r.formUrl;
      const i = document.createElement("input");
      i.type = "hidden"; i.name = "manifest"; i.value = typeof r.manifest === "string" ? r.manifest : JSON.stringify(r.manifest);
      f.appendChild(i); document.body.appendChild(f); f.submit();
    } catch (e) { toastError(e); }
  };

  const pasted = pastedRepo(q);
  const known = pasted ? repos?.find((r) => r.fullName.toLowerCase() === pasted.toLowerCase()) : undefined;
  const pick = (r: GitHubRepo) => open({ kind: "scan", repo: r.fullName, defaultBranch: r.defaultBranch });
  const list = pasted ? (known ? [known] : []) : fuzzyFilter(repos || [], q, (r) => r.fullName);

  const items: PItem[] = [];
  if (status?.appConfigured) {
    items.push({
      key: "configure", icon: <Settings size={16} />, label: "Configure GitHub App",
      onPick: () => { if (status.configureUrl) window.open(status.configureUrl, "_blank", "noopener"); },
      right: (
        <button type="button" onClick={(e) => { e.stopPropagation(); void load(true); }} disabled={refreshing}
          className="-my-1 inline-flex h-[26px] shrink-0 items-center gap-1.5 rounded-md border border-line px-1.5 text-caption text-gray-700 transition-colors hover:bg-tint-active hover:text-foreground disabled:opacity-60">
          <RefreshCw size={12} className={cx(refreshing && "spin")} />Refresh
        </button>
      ),
    });
  } else if (status) {
    items.push({
      key: "connect", icon: <Settings size={16} />, label: "Connect a GitHub App", sub: "Deploy private repositories and redeploy on push.", onPick: () => void connect(),
      right: <Button size="xs" variant="primary" tabIndex={-1} onClick={(e) => { e.stopPropagation(); void connect(); }}>Connect</Button>,
    });
  }
  for (const r of list) {
    items.push({
      key: r.fullName, icon: <BrandIcon name="github" size={16} mono />, label: r.fullName, chevron: true, onPick: () => pick(r),
      right: r.private ? <Lock size={12} className="shrink-0 opacity-70" aria-label="Private" /> : undefined,
    });
  }
  if (q.trim() && !pasted && repos && list.length === 0) {
    items.push({ key: "none", inert: true, icon: <SearchX size={16} />, label: "No repositories found. Try a different search." });
  }
  const navi = useListNav(items, back);
  const deployPasted = () => pasted && open({ kind: "scan", repo: `https://github.com/${pasted}` });

  return (
    <PaletteStep dir={dir}>
      <PaletteInput value={q} onChange={setQ} label="Search repositories" placeholder="Search repositories, or paste a URL..." onBack={back}
        listId="new-gh" activeId={`new-gh-${navi.active}`}
        onKeyDown={(e) => { if (pasted && !known && e.key === "Enter") { e.preventDefault(); deployPasted(); } else navi.onKeyDown(e); }} />
      {!canCreate ? <ViewerCard /> : pasted && !known && repos ? (
        <PaletteCard icon={<BrandIcon name="github" size={32} mono />} title={pasted}
          body={status?.appConfigured ? "Not in the app's repositories. Public repositories deploy as is; give the app access for private ones." : "Public repositories deploy as is. Connect a GitHub App for private ones."}
          action={
            <div className="flex items-center justify-center gap-2">
              {status?.installUrl && <Button onClick={() => window.open(status.installUrl, "_blank", "noopener")}>Grant access</Button>}
              <Button variant="primary" onClick={deployPasted}>Deploy Repo</Button>
            </div>
          } />
      ) : (
        <PaletteList id="new-gh" items={items} active={navi.active} onHover={navi.hover} byKey={navi.byKey} maxHeight={TALL_MAX}>
          {(!status || (status.appConfigured && !repos)) && <LoadingRows n={status ? 5 : 3} />}
        </PaletteList>
      )}
    </PaletteStep>
  );
}

// ---------------------------------------------------------------- Database

function useTemplates() {
  const [list, setList] = useState<TemplateSummary[]>();
  const [error, setError] = useState<unknown>();
  useEffect(() => { loadTemplates().then(setList, setError); }, []);
  return { list, error };
}

export function DatabaseStep({ dir, back, go, target, canCreate }: StepProps) {
  const [q, setQ] = useState("");
  const { list, error } = useTemplates();
  const { busy, run } = useBusy();
  const dbs = useMemo(() => sortDatabases((list || []).filter((t) => t.category === "database")), [list]);
  const shown = fuzzyFilter(dbs, q, (t) => `${t.name} ${t.id}`);
  const create = (t: TemplateSummary) => run(t.id, async () => {
    const tgt = await target.ensure();
    const r = await api.createService(tgt.eid, { template: t.id, source: { type: "empty" }, deployNow: true, position: target.position });
    target.done({ ...tgt, services: r.services });
  });
  const items: PItem[] = shown.map((t) => ({
    key: t.id, icon: <BrandIcon name={t.icon || t.id} size={16} fallback="box" />, label: t.name,
    right: busy === t.id ? spin(true) : undefined, disabled: !!busy && busy !== t.id, onPick: () => void create(t),
  }));
  if (list && q.trim() && !shown.length) {
    items.push({ key: "none", inert: true, icon: <SearchX size={16} />, label: "No databases match." });
    items.push({ key: "tpl", icon: <Sparkles size={16} />, accent: true, enter: true, label: <>Search templates for <span className="text-foreground">&ldquo;{q.trim()}&rdquo;</span></>, onPick: () => go("template", q.trim()) });
  }
  if (error) items.push({ key: "err", inert: true, icon: <CircleAlert size={16} className="text-danger-text" />, label: "Could not load the database templates." });
  const navi = useListNav(items, back);
  return (
    <PaletteStep dir={dir}>
      <PaletteInput value={q} onChange={setQ} label="Choose a database" placeholder="Choose a database..." onBack={back}
        listId="new-db" activeId={`new-db-${navi.active}`} onKeyDown={navi.onKeyDown} />
      {!canCreate ? <ViewerCard /> : (
        <PaletteList id="new-db" items={items} active={navi.active} onHover={navi.hover} byKey={navi.byKey}>
          {!list && !error && <LoadingRows n={4} />}
        </PaletteList>
      )}
    </PaletteStep>
  );
}

// ---------------------------------------------------------------- Template

/** The small stats line under a template row. */
export function TemplateStats({ t, className }: { t: TemplateSummary; className?: string }) {
  const n = t.services.length || 1;
  return (
    <span className={cx("contents", className)}>
      <span className="inline-flex items-center gap-1.5"><Layers size={12} />{n} {n === 1 ? "service" : "services"}</span>
      <span className="inline-flex items-center gap-1.5"><HardDrive size={12} />{categoryLabel(t.category)}</span>
      <span className="inline-flex items-center gap-1.5 text-ok-text"><BadgeCheck size={12} />Official</span>
    </span>
  );
}

export function TemplateStep({ dir, back, seed }: StepProps) {
  const nav = useNavigate();
  const [q, setQ] = useState(seed || "");
  const { list, error } = useTemplates();
  const all = useMemo(() => sortPopular(list || []), [list]);
  const shown = fuzzyFilter(all, q, (t) => `${t.name} ${t.id} ${t.tagline}`);
  const items: PItem[] = shown.map((t) => ({
    key: t.id, icon: <BrandIcon name={t.icon || t.id} size={20} fallback="box" />, label: t.name, sub: t.tagline,
    meta: <TemplateStats t={t} />, onPick: () => nav(`/new/template/${encodeURIComponent(t.id)}`),
  }));
  if (list && q.trim() && !shown.length) {
    items.push({ key: "none", inert: true, label: "No templates found" });
    items.push({ key: "gallery", icon: <Search size={16} />, label: "Browse the template gallery", chevron: true, onPick: () => nav(`/new/template?q=${encodeURIComponent(q.trim())}`) });
  }
  if (error) items.push({ key: "err", inert: true, icon: <CircleAlert size={16} className="text-danger-text" />, label: "Could not load templates." });
  const navi = useListNav(items, back);
  return (
    <PaletteStep dir={dir}>
      <PaletteInput value={q} onChange={setQ} label="Search templates" placeholder="Search templates..." onBack={back}
        listId="new-tpl" activeId={`new-tpl-${navi.active}`} onKeyDown={navi.onKeyDown} />
      <PaletteList id="new-tpl" items={items} active={navi.active} onHover={navi.hover} byKey={navi.byKey} maxHeight={TALL_MAX}>
        {!list && !error && <LoadingRows n={4} tall />}
      </PaletteList>
    </PaletteStep>
  );
}

// ---------------------------------------------------------------- Docker image

const EXAMPLES = ["hello-world", "ghcr.io/owner/repo:latest", "quay.io/owner/repo:tag", "registry.gitlab.com/owner/repo:tag", "mcr.microsoft.com/owner/repo:tag"];

function Callout({ children, icon, tone = "info" }: { children: ReactNode; icon: ReactNode; tone?: "info" | "danger" }) {
  return (
    <div className={cx("mx-3 mt-2 flex items-center gap-2 rounded-lg px-3 py-3 animate-fade-in",
      tone === "info" ? "bg-blue-50 text-blue-600" : "bg-danger-bg text-danger-soft")}>
      <span className="min-w-0">{children}</span>
      <span className="shrink-0">{icon}</span>
    </div>
  );
}

export function ImageStep({ dir, back, seed, target, open, canCreate }: StepProps) {
  const [q, setQ] = useState(seed || "");
  const { busy, run } = useBusy();
  const text = q.trim();
  const err = text ? imageError(text) : "";
  const page = text && !err ? registryPage(text) : undefined;
  const deploy = () => run("deploy", async () => {
    const t = await target.ensure();
    const r = await api.createService(t.eid, { name: imageName(text), source: { type: "image", image: text }, deployNow: !target.canvas, position: target.position });
    target.done({ ...t, services: r.services });
  });
  const items: PItem[] = text && !err ? [
    { key: "deploy", icon: <BrandIcon name="docker" size={16} />, label: <>{target.canvas ? "Add" : "Deploy"} <span className="font-mono text-[13px]">{text}</span></>, enter: !busy, right: spin(busy === "deploy"), onPick: () => void deploy() },
    ...(page ? [{ key: "page", icon: <ArrowUpRight size={16} />, label: <span className="text-accent-text underline-offset-2 group-hover:underline">{page.label}</span>, onPick: () => window.open(page.href, "_blank", "noopener") }] : []),
    { key: "cred", icon: <KeyRound size={16} />, label: "Private image? Add a registry credential", onPick: () => open({ kind: "image", image: text }) },
  ] : [];
  const navi = useListNav(items, back);
  return (
    <PaletteStep dir={dir}>
      <PaletteInput value={q} onChange={setQ} label="Docker image" placeholder="traefik/whoami:v1.10" onBack={back} mono={!!q}
        listId="new-img" activeId={`new-img-${navi.active}`} onKeyDown={navi.onKeyDown} />
      {!canCreate ? <ViewerCard /> : !text ? (
        <div className="pb-1.5">
          <Callout icon={<Info size={16} />}>Enter an image from Docker Hub or any registry</Callout>
          <div className="mx-3 mb-1.5 mt-2 rounded-lg bg-surface px-4 pb-5 pt-3 animate-fade-in">
            <div className="mb-2 text-lead text-foreground">Examples</div>
            <ul className="list-disc space-y-0 pl-4 marker:text-fg-3">
              {EXAMPLES.map((x) => (
                <li key={x}>
                  <button type="button" onClick={() => setQ(x)} className="text-left text-foreground transition-colors hover:text-accent-text">{x}</button>
                </li>
              ))}
            </ul>
          </div>
        </div>
      ) : err ? (
        <div className="pb-3"><Callout tone="danger" icon={<CircleAlert size={16} />}>{err}</Callout></div>
      ) : (
        <PaletteList id="new-img" items={items} active={navi.active} onHover={navi.hover} byKey={navi.byKey} />
      )}
    </PaletteStep>
  );
}
