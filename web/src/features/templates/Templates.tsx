// SPDX-License-Identifier: AGPL-3.0-only

// Template gallery (/new/template) and template page (/new/template/:id),
// SPEC 10.2. With ?project=&environment= a template deploys into that
// environment; otherwise into a new project.
import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { Link, useNavigate, useParams, useSearchParams } from "react-router-dom";
import {
  ArrowDown, ArrowUpRight, BadgeCheck, BookOpen, CircleCheck, Database, Eye, EyeOff, Flag, Folder, Globe, HardDrive,
  KeyRound, Layers, LayoutGrid, Package, Search, Sparkles,
} from "lucide-react";
import { api, type TemplateDetail, type TemplateSummary } from "../../api/client";
import { LINKS, TopBar, useShellOptions } from "../../app/Shell";
import { canEdit, useMe } from "../../app/session";
import { BrandIcon, Button, ErrorLine, Field, Input, Logo, NotFound, Select, Sheet, Skeleton, cx, toastSuccess } from "../../kit";
import { categoryLabel, fuzzyFilter, loadTemplate, loadTemplates, sortPopular } from "../new/data";

function useInto() {
  const [p] = useSearchParams();
  const pid = p.get("project"), eid = p.get("environment");
  return pid && eid ? { pid, eid, q: `?project=${encodeURIComponent(pid)}&environment=${encodeURIComponent(eid)}` } : null;
}

const CAT_ICON: Record<string, ReactNode> = { database: <Database size={16} />, storage: <HardDrive size={16} />, app: <Package size={16} /> };

// ---------------------------------------------------------------- shared pieces

/** A gallery card: logo, name, two-line description, publisher and service count. */
export function TemplateCard({ t, to }: { t: TemplateSummary; to: string }) {
  const n = t.services.length || 1;
  return (
    <Link to={to} className="group flex h-[222px] flex-col rounded-lg border border-line bg-surface-raised p-6 outline-none transition-[border-color,background-color,box-shadow,transform] duration-150 hover:border-fg-5 hover:bg-surface hover:shadow-card focus-visible:border-accent active:scale-[.99]">
      <BrandIcon name={t.icon || t.id} size={32} fallback="box" className="transition-transform duration-200 group-hover:scale-105" />
      <div className="mt-4 truncate text-lead font-semibold text-foreground">{t.name}</div>
      <p className="line-clamp-2 text-sm text-fg-3">{t.tagline || t.description}</p>
      <div className="mt-auto flex items-center justify-between pt-4 text-caption text-fg-3">
        <span className="inline-flex items-center gap-1.5"><span className="text-foreground"><Logo size={14} /></span>5050cloud</span>
        <span className="inline-flex items-center gap-1.5"><Layers size={12} />{n} {n === 1 ? "service" : "services"}</span>
      </div>
      <span className="sr-only">View template</span>
    </Link>
  );
}

function CardSkeleton() {
  return (
    <div className="flex h-[222px] flex-col rounded-lg border border-line bg-surface-raised p-6">
      <Skeleton w={32} h={32} className="rounded-md" />
      <Skeleton h={14} w="50%" className="mt-5" />
      <Skeleton h={12} w="85%" className="mt-3" />
      <Skeleton h={12} w="60%" className="mt-2" />
    </div>
  );
}

// ---------------------------------------------------------------- gallery

type Tab = "popular" | "database" | "app";
const TABS: { id: Tab; label: string }[] = [{ id: "popular", label: "Popular" }, { id: "database", label: "Databases" }, { id: "app", label: "Apps" }];

export function TemplateList() {
  useShellOptions({ width: "wide" });
  const into = useInto();
  const [params, setParams] = useSearchParams();
  const q = params.get("q") || "";
  const setQ = (v: string) => { const p = new URLSearchParams(params); if (v) p.set("q", v); else p.delete("q"); setParams(p, { replace: true }); };
  const [tab, setTab] = useState<Tab>("popular");
  const [cat, setCat] = useState("all");
  const [sort, setSort] = useState<"popular" | "name">("popular");
  const [list, setList] = useState<TemplateSummary[]>();
  const [error, setError] = useState<unknown>();
  const explore = useRef<HTMLElement>(null);
  useEffect(() => { loadTemplates().then(setList, setError); }, []);

  const all = useMemo(() => sortPopular(list || []), [list]);
  const cats = useMemo(() => Array.from(new Set(all.map((t) => t.category))).sort(), [all]);
  const link = (t: TemplateSummary) => `/new/template/${encodeURIComponent(t.id)}${into?.q || ""}`;
  const top = (tab === "popular" ? all : all.filter((t) => t.category === tab)).slice(0, 8);
  const results = fuzzyFilter(all, q, (t) => `${t.name} ${t.id} ${t.tagline}`);
  const browse = (cat === "all" ? all : all.filter((t) => t.category === cat)).slice().sort((a, b) => (sort === "name" ? a.name.localeCompare(b.name) : 0));

  return (
    <div className="mx-auto w-full max-w-[1288px]">
      <TopBar left={<span className="font-medium text-foreground">Templates</span>} />
      {into && (
        <div className="mb-6 flex items-center gap-2 rounded-lg border border-line bg-surface-raised px-4 py-3 text-sm text-fg-2 animate-rise-in">
          <Sparkles size={16} className="shrink-0 text-accent-text" />
          <span className="flex-1">Templates deploy into the environment you came from.</span>
          <Link to={`/project/${into.pid}?environment=${encodeURIComponent(into.eid)}`} className="text-foreground underline-offset-2 hover:underline">Back to the canvas</Link>
        </div>
      )}
      <section className="flex flex-col items-center pb-16 pt-14 text-center">
        <h1 className="max-w-[940px] font-serif text-[36px] font-medium leading-[1.35] text-foreground md:text-[56px] md:leading-[91px] animate-rise-in">
          Deploy open-source apps and databases in one click
        </h1>
        <p className="mt-4 text-[20px] leading-[30px] text-fg-2">Pick a template, fill in a few values, and it runs on your own servers.</p>
        <label className="relative mt-12 block w-full max-w-[768px]">
          <span className="sr-only">Search templates</span>
          <input value={q} onChange={(e) => setQ(e.target.value)} placeholder="Search Postgres, n8n, Ghost..." autoComplete="off"
            className="h-[42px] w-full rounded-md border border-line bg-black/25 py-2 pl-3 pr-10 text-lead text-foreground outline-none transition-colors placeholder:text-fg-3 hover:border-fg-5 focus:border-accent focus-visible:shadow-none" />
          <Search size={18} className="pointer-events-none absolute right-3 top-1/2 -translate-y-1/2 text-fg-3" />
        </label>
      </section>

      <ErrorLine error={error} />

      {q ? (
        <section className="animate-fade-in">
          <div className="mb-6 flex items-center justify-between border-b border-line pb-4">
            <h2 className="text-lead text-foreground">{list ? `${results.length} ${results.length === 1 ? "result" : "results"} for "${q}"` : "Searching"}</h2>
            <button type="button" onClick={() => setQ("")} className="text-sm text-fg-3 transition-colors hover:text-foreground">Clear search</button>
          </div>
          {list && results.length === 0 ? (
            <div className="flex flex-col items-center rounded border border-dashed border-line px-6 py-12 text-center">
              <LayoutGrid size={24} className="text-fg-3" />
              <div className="mt-4 text-[18px] leading-[27px] text-foreground">No templates found</div>
              <p className="mt-1 text-sm text-fg-2">Try another name, or <Link to="/new/image" className="text-accent-text hover:underline">deploy a Docker image</Link> directly.</p>
            </div>
          ) : (
            <div className="grid grid-cols-1 gap-[18px] sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
              {(list ? results : []).map((t) => <TemplateCard key={t.id} t={t} to={link(t)} />)}
              {!list && Array.from({ length: 8 }, (_, i) => <CardSkeleton key={i} />)}
            </div>
          )}
        </section>
      ) : (
        <>
          <div className="mb-10 flex items-end justify-between border-b border-line">
            <div role="tablist" className="flex gap-8">
              {TABS.map((x) => (
                <button key={x.id} role="tab" type="button" aria-selected={tab === x.id} onClick={() => setTab(x.id)}
                  className={cx("relative h-14 text-lead transition-colors", tab === x.id ? "font-medium text-foreground" : "text-fg-3 hover:text-fg-2")}>
                  {x.label}
                  <span className={cx("absolute inset-x-0 -bottom-px h-px bg-foreground transition-opacity", tab === x.id ? "opacity-100" : "opacity-0")} />
                </button>
              ))}
            </div>
            <button type="button" onClick={() => explore.current?.scrollIntoView({ behavior: "smooth", block: "start" })}
              className="mb-4 hidden items-center gap-1 text-lead text-fg-3 transition-colors hover:text-foreground sm:inline-flex">
              All templates ({all.length})<ArrowDown size={16} />
            </button>
          </div>
          <div key={tab} className="grid grid-cols-1 gap-[18px] animate-fade-in sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
            {list ? top.map((t) => <TemplateCard key={t.id} t={t} to={link(t)} />) : Array.from({ length: 8 }, (_, i) => <CardSkeleton key={i} />)}
          </div>

          <section ref={explore} className="scroll-mt-6 pt-24">
            <div className="mb-8 flex flex-wrap items-center justify-between gap-4">
              <h2 className="font-serif text-[30px] font-medium leading-[40px] text-foreground">Explore all templates</h2>
              <Select variant="flat" aria-label="Sort" value={sort} onChange={(e) => setSort(e.target.value as "popular" | "name")}
                options={[{ value: "popular", label: "Sort by popularity" }, { value: "name", label: "Sort by name" }]} />
            </div>
            <div className="flex flex-col gap-8 md:flex-row">
              <nav className="w-full shrink-0 md:w-[239px]" aria-label="Filter templates">
                <div className="mb-2 px-3 text-caption font-semibold uppercase tracking-wide text-fg-4">Templates</div>
                <FilterLink on={cat === "all"} onClick={() => setCat("all")} icon={<LayoutGrid size={16} />}>All</FilterLink>
                <div className="mb-2 mt-6 px-3 text-caption font-semibold uppercase tracking-wide text-fg-4">Category</div>
                {cats.map((c) => (
                  <FilterLink key={c} on={cat === c} onClick={() => setCat(c)} icon={CAT_ICON[c] || <Folder size={16} />}>
                    {categoryLabel(c)}<span className="ml-auto text-caption text-fg-4">{all.filter((t) => t.category === c).length}</span>
                  </FilterLink>
                ))}
              </nav>
              <div key={`${cat}-${sort}`} className="grid min-w-0 flex-1 grid-cols-1 gap-[18px] animate-fade-in sm:grid-cols-2 xl:grid-cols-3">
                {list ? browse.map((t) => <TemplateCard key={t.id} t={t} to={link(t)} />) : Array.from({ length: 6 }, (_, i) => <CardSkeleton key={i} />)}
              </div>
            </div>
          </section>
        </>
      )}
    </div>
  );
}

function FilterLink({ on, onClick, icon, children }: { on: boolean; onClick: () => void; icon: ReactNode; children: ReactNode }) {
  return (
    <button type="button" onClick={onClick} aria-current={on || undefined}
      className={cx("flex h-[42px] w-full items-center gap-3 rounded-md px-3 text-left text-sm transition-colors",
        on ? "bg-tint-active text-foreground" : "text-fg-3 hover:bg-tint-hover hover:text-foreground")}>
      <span className={cx("shrink-0", on && "text-accent-text")}>{icon}</span>{children}
    </button>
  );
}

// ---------------------------------------------------------------- template page

/** The service diagram: one node per service with its image, status and volume. */
function Diagram({ t }: { t: TemplateDetail }) {
  return (
    <div className="dot-grid relative flex min-h-[520px] items-center justify-center overflow-hidden rounded-lg border border-line-popover bg-surface-raised p-4">
      <div className="flex flex-wrap items-start justify-center gap-8 py-8">
        {t.serviceInfo.map((s, i) => (
          <div key={s.name} className="w-[288px] max-w-full animate-rise-in" style={{ animationDelay: `${i * 60}ms`, animationFillMode: "both" }}>
            <div className={cx("rounded-xl border border-line bg-surface-raised", "transition-[border-color,transform] duration-200 hover:-translate-y-0.5 hover:border-fg-5")}>
              <div className="rounded-xl border-b border-line bg-surface-hover p-6">
                <div className="flex items-center gap-3">
                  <BrandIcon name={s.image} size={24} fallback="box" />
                  <span className="truncate text-lead font-semibold text-foreground">{s.name}</span>
                </div>
                <div className="mt-1 truncate pl-9 font-mono text-[13px] leading-5 text-fg-3" title={s.image}>{s.image}</div>
                <div className="mt-5 flex items-center gap-3 text-sm text-fg-2">
                  <CircleCheck size={16} className="text-ok-text" />Ready to deploy
                  {s.domain && <span className="ml-auto inline-flex items-center gap-1 text-caption text-fg-3"><Globe size={12} />Public</span>}
                </div>
              </div>
              {s.volume ? (
                <div className="flex h-[48px] items-center gap-3 px-6 text-sm text-fg-3"><HardDrive size={16} /><span className="truncate font-mono text-[13px]">{s.volume}</span></div>
              ) : <div className="h-0" />}
            </div>
            {s.dependsOn?.length ? <div className="mt-2 text-center text-caption text-fg-4">Starts after {s.dependsOn.join(", ")}</div> : null}
          </div>
        ))}
      </div>
    </div>
  );
}

function MetaRow({ icon, children, tone }: { icon: ReactNode; children: ReactNode; tone?: "ok" }) {
  return <div className={cx("flex h-9 items-center gap-6 text-sm", tone === "ok" ? "text-ok-text" : "text-fg-3")}><span className="shrink-0">{icon}</span><span className="min-w-0 truncate">{children}</span></div>;
}

function DeploySheet({ t, onClose }: { t: TemplateDetail; onClose: () => void }) {
  const nav = useNavigate();
  const into = useInto();
  const [inputs, setInputs] = useState<Record<string, string>>(() => Object.fromEntries(t.inputs.map((i) => [i.name, i.default || ""])));
  const [shown, setShown] = useState<Record<string, boolean>>({});
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<unknown>();
  const missing = t.inputs.filter((i) => i.required && !inputs[i.name]?.trim());
  const deploy = async () => {
    setBusy(true); setErr(undefined);
    try {
      let pid = into?.pid, eid = into?.eid;
      if (!pid || !eid) {
        const p = await api.createProject({ name: t.name });
        pid = p.id; eid = (p.environments.find((e) => e.name === "production") || p.environments[0]).id;
      }
      await api.deployTemplate(eid, t.id, inputs);
      toastSuccess(`${t.name} is deploying`);
      nav(`/project/${pid}?environment=${encodeURIComponent(eid)}`);
    } catch (e) { setErr(e); setBusy(false); }
  };
  return (
    <Sheet overlay onClose={onClose} width={520} title={<span className="flex items-center gap-3"><BrandIcon name={t.icon || t.id} size={20} />Deploy {t.name}</span>} label={`Deploy ${t.name}`}>
      <form className="flex min-h-full flex-col" onSubmit={(e) => { e.preventDefault(); if (!missing.length) void deploy(); }}>
        <div className="flex-1 space-y-6 px-5 py-5">
          <p className="text-sm text-fg-2">{into ? "The services are added to the environment you came from and deploy right away." : "A new project is created and the services deploy right away."}</p>
          <div className="space-y-2">
            {t.serviceInfo.map((s) => (
              <div key={s.name} className="flex items-center gap-3 rounded-lg border border-line bg-surface-raised px-3 py-2.5">
                <BrandIcon name={s.image} size={18} fallback="box" />
                <span className="text-sm font-medium text-foreground">{s.name}</span>
                <span className="ml-auto truncate font-mono text-caption text-fg-3">{s.image}</span>
              </div>
            ))}
          </div>
          {t.inputs.length > 0 && (
            <div className="space-y-4">
              <div className="text-caption font-semibold uppercase tracking-wide text-fg-4">Configuration</div>
              {t.inputs.map((i) => (
                <Field key={i.name} label={i.label || i.name} hint={i.description || (i.generate ? "Generated for you. Change it if you like." : undefined)} optional={!i.required}>
                  <Input aria-label={i.label || i.name} mono type={i.secret && !shown[i.name] ? "password" : "text"} value={inputs[i.name] || ""} autoComplete="off"
                    onChange={(e) => setInputs({ ...inputs, [i.name]: e.target.value })}
                    suffix={i.secret ? (
                      <button type="button" aria-label={shown[i.name] ? "Hide value" : "Show value"} onClick={() => setShown({ ...shown, [i.name]: !shown[i.name] })}
                        className="flex h-7 w-7 items-center justify-center rounded text-fg-4 transition-colors hover:text-foreground">
                        {shown[i.name] ? <EyeOff size={14} /> : <Eye size={14} />}
                      </button>
                    ) : undefined} />
                </Field>
              ))}
            </div>
          )}
          <ErrorLine error={err} />
        </div>
        <div className="sticky bottom-0 border-t border-line bg-background px-5 py-4">
          <Button type="submit" variant="primary" size="lg" block busy={busy} disabled={missing.length > 0}>Deploy</Button>
          {missing.length > 0 && <p className="mt-2 text-center text-caption text-fg-3">Fill in {missing.map((m) => m.label || m.name).join(", ")} to deploy.</p>}
        </div>
      </form>
    </Sheet>
  );
}

export function TemplateDetailPage() {
  const { id = "" } = useParams();
  const me = useMe();
  const into = useInto();
  const [t, setT] = useState<TemplateDetail>();
  const [err, setErr] = useState<unknown>();
  const [list, setList] = useState<TemplateSummary[]>();
  const [deploying, setDeploying] = useState(false);
  useEffect(() => { setT(undefined); setErr(undefined); loadTemplate(id).then(setT, setErr); loadTemplates().then(setList, () => setList([])); }, [id]);

  if ((err as { status?: number })?.status === 404) return <NotFound title="No such template" body="It may have been removed from this install." home="/new/template" />;
  const gallery = `/new/template${into?.q || ""}`;
  const more = (list || []).filter((x) => t && x.category === t.category && x.id !== t.id).slice(0, 3);
  const volumes = t?.serviceInfo.filter((s) => s.volume).length || 0;
  const pub = t?.serviceInfo.some((s) => s.domain);

  return (
    <div className="mx-auto w-full max-w-[1160px]">
      <TopBar left={<><Link to={gallery} className="text-fg-2 transition-colors hover:text-foreground">Templates</Link><span className="text-fg-5">/</span><span className="font-medium text-foreground">{t?.name || id}</span></>} />
      <nav aria-label="Breadcrumb" className="flex items-center gap-2 text-sm text-fg-3">
        <LayoutGrid size={18} className="text-fg-3" />
        <Link to="/" className="transition-colors hover:text-foreground">Home</Link><span className="text-fg-4">/</span>
        <Link to={gallery} className="transition-colors hover:text-foreground">Templates</Link><span className="text-fg-4">/</span>
        {t && <><span>{categoryLabel(t.category)}</span><span className="text-fg-4">/</span></>}
        <span className="text-gray-700">{t?.name || id}</span>
      </nav>
      <ErrorLine error={err} className="mt-6" />

      <header className="mt-20 flex flex-col gap-6 md:flex-row md:items-start md:justify-between">
        <div className="flex min-w-0 items-center gap-6">
          <span className="flex h-16 w-16 shrink-0 items-center justify-center rounded-lg border border-line bg-surface-raised">
            {t ? <BrandIcon name={t.icon || t.id} size={28} fallback="box" /> : <Skeleton w={28} h={28} />}
          </span>
          <div className="min-w-0">
            {t ? <h1 className="truncate font-serif text-[40px] font-medium leading-[65px] text-foreground animate-fade-in">{t.name}</h1> : <Skeleton h={36} w={260} className="my-3" />}
            {t ? <p className="text-[18px] leading-[27px] text-fg-3">{t.tagline}</p> : <Skeleton h={16} w={320} />}
          </div>
        </div>
        {canEdit(me) && <Button variant="primary" size="lg" className="w-full md:mt-2.5 md:w-[278px]" disabled={!t} onClick={() => setDeploying(true)}>Deploy Now</Button>}
      </header>

      <div className="mt-16 flex flex-col gap-12 lg:flex-row">
        <div className="min-w-0 flex-1">
          {t ? <Diagram t={t} /> : <Skeleton h={520} className="rounded-lg" />}
        </div>
        <aside className="w-full shrink-0 lg:w-[278px]">
          <div className="border-y border-line py-4">
            <div className="flex h-10 items-center gap-4 text-sm text-foreground"><Logo size={24} />5050cloud templates</div>
            {t && <>
              <MetaRow icon={<Layers size={16} />}>{t.serviceInfo.length} {t.serviceInfo.length === 1 ? "service" : "services"}</MetaRow>
              <MetaRow icon={<HardDrive size={16} />}>{volumes ? `${volumes} ${volumes === 1 ? "volume" : "volumes"}` : "No volumes"}</MetaRow>
              <MetaRow icon={<Globe size={16} />}>{pub ? "Public domain included" : "Private network only"}</MetaRow>
              <MetaRow icon={<KeyRound size={16} />}>{t.inputs.length ? `${t.inputs.length} ${t.inputs.length === 1 ? "setting" : "settings"} to review` : "Nothing to configure"}</MetaRow>
              <MetaRow icon={<BadgeCheck size={16} />} tone="ok">Maintained with 5050cloud</MetaRow>
              <MetaRow icon={<Folder size={16} />}>{categoryLabel(t.category)}</MetaRow>
            </>}
          </div>
          <div className="border-b border-line py-4">
            {t?.docs && <a href={t.docs} target="_blank" rel="noreferrer" className="flex h-9 items-center gap-6 text-sm text-fg-3 transition-colors hover:text-foreground"><BookOpen size={16} />Template docs<ArrowUpRight size={12} className="-ml-4" /></a>}
            <a href={LINKS.support} target="_blank" rel="noreferrer" className="flex h-9 items-center gap-6 text-sm text-fg-3 transition-colors hover:text-foreground"><Flag size={16} />Report a problem</a>
          </div>
        </aside>
      </div>

      {t && (
        <article className="mt-20 max-w-[834px] animate-fade-in">
          <h2 className="mb-6 font-serif text-[30px] font-medium leading-[40px] text-foreground">Deploy and host {t.name}</h2>
          <p className="text-[18px] leading-8 text-gray-700">{t.description || t.tagline}</p>
          <h3 className="mb-4 mt-12 text-[24px] font-bold leading-9 text-foreground">What you get</h3>
          <ul className="list-disc space-y-1 pl-6 text-[18px] leading-8 text-gray-700 marker:text-fg-4">
            {t.serviceInfo.map((s) => (
              <li key={s.name}>
                <span className="font-semibold text-foreground">{s.name}:</span> runs <code className="font-mono text-[16px] font-semibold text-foreground">{s.image}</code>
                {s.volume && <> with its data on a volume at <code className="font-mono text-[16px] text-foreground">{s.volume}</code></>}
                {s.domain ? <>, reachable on a public domain</> : <>, on the private network</>}
                {s.dependsOn?.length ? <>, after {s.dependsOn.join(" and ")} is up</> : null}.
              </li>
            ))}
            {t.inputs.length > 0 && <li><span className="font-semibold text-foreground">Settings:</span> {t.inputs.map((i) => i.label || i.name).join(", ")}. Secrets are generated for you.</li>}
          </ul>

          <h2 className="mb-6 mt-16 font-serif text-[30px] font-medium leading-[40px] text-foreground">Template content</h2>
          <div className="flex flex-wrap gap-3">
            {t.serviceInfo.map((s) => (
              <span key={s.name} className="inline-flex items-center gap-3 rounded-lg border border-line bg-surface-raised px-4 py-3">
                <BrandIcon name={s.image} size={20} fallback="box" />
                <span className="text-sm font-medium text-foreground">{s.name}</span>
                <span className="font-mono text-caption text-fg-3">{s.image}</span>
              </span>
            ))}
          </div>
        </article>
      )}

      {more.length > 0 && (
        <section className="mt-20">
          <h2 className="mb-6 font-serif text-[30px] font-medium leading-[40px] text-foreground">More templates in {categoryLabel(t!.category)}</h2>
          <div className="grid grid-cols-1 gap-[18px] sm:grid-cols-2 lg:grid-cols-3">
            {more.map((x) => <TemplateCard key={x.id} t={x} to={`/new/template/${encodeURIComponent(x.id)}${into?.q || ""}`} />)}
          </div>
        </section>
      )}
      {t && deploying && <DeploySheet t={t} onClose={() => setDeploying(false)} />}
    </div>
  );
}
