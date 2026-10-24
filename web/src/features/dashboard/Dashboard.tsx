// SPDX-License-Identifier: AGPL-3.0-only

// Projects dashboard (SPEC 13.1): title row with the project search (opens the palette) and New,
// a toolbar with the count, sort and grid/list toggle, then project cards (mini canvas well with
// service tiles and a production status line) or rows. Favourites float first. Install notices
// go to the shell banner row; New opens the create flow, or a limit dialog when the install
// genuinely cannot take a new project yet (disk full, no host online).
import { useCallback, useEffect, useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";
import { LayoutGrid, LayoutTemplate, List, Plus, RotateCw } from "lucide-react";
import { api, type Host, type Project } from "../../api/client";
import { useLoad } from "../../api/hooks";
import { canEdit, isAdmin, useMe } from "../../app/session";
import { useViewportWidth } from "../../app/Shell";
import { PALETTE_EVENT } from "../palette/Palette";
import {
  BrandTile, Button, EmptyState, ErrorLine, SearchButton, SegmentedControl, Select, VDivider, cx, plural, readPref, writePref,
} from "../../kit";
import { DashboardBanners } from "./Banners";
import { LimitDialog, type Limit } from "./LimitDialog";
import { CardSkeleton, ProjectCard, ProjectRow, RowSkeleton } from "./ProjectCard";
import { PREF, SORTS, arrange, isSort, isView, sortProjects, toggleIn, type Sort, type View } from "./model";

// `npm run dev:mocks`: richer dashboard fixtures, registered before the first fetch.
const mocksReady: Promise<unknown> = import.meta.env.VITE_MOCKS === "1"
  ? import("./mocks").then((m) => m.installDashboardMocks()).catch(() => undefined)
  : Promise.resolve();

/** Opens the command palette on its project search level (falls back to the root level). */
export function openProjectSearch() {
  window.dispatchEvent(new CustomEvent(PALETTE_EVENT, { detail: { open: "search:projects" } }));
}

function readFavourites(): string[] {
  const fav = readPref<string[] | null>(PREF.favourites, null);
  if (Array.isArray(fav)) return fav;
  // carry over stars saved before favourites were shared with the palette
  const legacy = readPref<string[]>(PREF.legacyStars, []);
  return Array.isArray(legacy) ? legacy : [];
}

export default function Dashboard() {
  const me = useMe();
  const nav = useNavigate();
  const editable = canEdit(me);
  const admin = isAdmin(me);
  const [sort, setSortState] = useState<Sort>(() => { const v = readPref<unknown>(PREF.sort, "activity"); return isSort(v) ? v : "activity"; });
  const [view, setViewState] = useState<View>(() => { const v = readPref<unknown>(PREF.view, "grid"); return isView(v) ? v : "grid"; });
  const [favourites, setFavourites] = useState<string[]>(readFavourites);
  const [limit, setLimit] = useState<Limit | null>(null);
  const narrow = useViewportWidth() < 640;

  const projects = useLoad(() => mocksReady.then(() => api.projects(sort)), [sort]);
  const hosts = useLoad<Host[] | undefined>(() => (admin ? mocksReady.then(() => api.hosts()).catch(() => undefined) : Promise.resolve(undefined)), [admin]);

  useEffect(() => { document.title = "Projects | 5050cloud"; return () => { document.title = "5050cloud"; }; }, []);

  // keep favourites in step with the palette's star command and other tabs
  useEffect(() => {
    const f = (e: StorageEvent) => { if (e.key === PREF.favourites) setFavourites(readFavourites()); };
    const focus = () => setFavourites(readFavourites());
    window.addEventListener("storage", f);
    window.addEventListener("focus", focus);
    return () => { window.removeEventListener("storage", f); window.removeEventListener("focus", focus); };
  }, []);

  const setSort = (s: Sort) => {
    setSortState(s);
    writePref(PREF.sort, s);
    // re-order what is on screen at once; the server answer replaces it
    if (projects.data) projects.set(sortProjects(projects.data, s));
  };
  const setView = (v: View) => { setViewState(v); writePref(PREF.view, v); };
  const toggleFavourite = useCallback((id: string) => {
    setFavourites((f) => { const n = toggleIn(f, id); writePref(PREF.favourites, n); return n; });
  }, []);

  const list = useMemo(() => arrange(projects.data || [], favourites), [projects.data, favourites]);
  const favSet = useMemo(() => new Set(favourites), [favourites]);

  /** New: the create flow, unless the install cannot take a project right now. */
  const startNew = useCallback(() => {
    if (me.install.readOnly) { setLimit("read-only"); return; }
    const h = hosts.data;
    if (h && h.length > 0 && !h.some((x) => x.state === "online")) { setLimit("no-hosts"); return; }
    if (h && h.length === 0) { setLimit("no-hosts"); return; }
    nav("/new");
  }, [me.install.readOnly, hosts.data, nav]);

  const loading = projects.loading && !projects.data;
  const count = projects.data?.length ?? 0;

  return (
    <div className="flex flex-col gap-8">
      <DashboardBanners install={me.install} hosts={hosts.data} admin={admin} />

      <div className="flex flex-wrap items-center gap-3">
        <h1 className="min-w-0 flex-1 truncate text-title text-foreground">Projects</h1>
        <SearchButton label="Search projects..." onClick={openProjectSearch} shortcut={narrow ? null : "K"} className="order-last w-full sm:order-none sm:w-56" />
        {editable && (
          <Button variant="primary" icon={<Plus size={16} />} onClick={startNew} title="New project" aria-keyshortcuts="Meta+/ Control+/">New</Button>
        )}
      </div>

      <div className="flex flex-col gap-8">
        <div className="flex items-center justify-between gap-3">
          <div className="flex min-w-0 items-center gap-3">
            <span className="hidden items-center gap-1.5 whitespace-nowrap text-sm text-foreground sm:inline-flex" aria-live="polite">
              <LayoutGrid size={16} className="shrink-0 text-fg-4" />
              {loading ? <span className="inline-block h-4 w-[72px] animate-skeleton rounded bg-skeleton" /> : plural(count, "Project")}
            </span>
            <VDivider className="hidden !bg-line sm:inline-block" />
            <label className="sr-only" htmlFor="dash-sort">Project sort</label>
            <Select id="dash-sort" variant="flat" value={sort} onChange={(e) => setSort(e.target.value as Sort)} className="w-auto [&>select]:pr-6">
              {SORTS.map((s) => <option key={s.value} value={s.value} title={s.title} className="bg-surface-raised text-foreground">{s.label}</option>)}
            </Select>
          </div>
          <SegmentedControl<View> label="View display" value={view} onChange={setView}
            options={[{ value: "grid", label: "Grid view", icon: <LayoutGrid size={16} /> }, { value: "list", label: "List view", icon: <List size={16} /> }]} />
        </div>

        {projects.error && (
          <div className="flex items-center justify-between gap-4 rounded-lg border border-danger-line bg-danger-bg px-4 py-3">
            <ErrorLine error={projects.error} className="!m-0" />
            <Button size="sm" icon={<RotateCw size={14} />} onClick={() => void projects.reload()}>Retry</Button>
          </div>
        )}

        {loading && (
          <ul aria-busy="true" aria-label="Loading projects" className={view === "grid" ? "grid grid-cols-1 gap-4 md:grid-cols-2 lg:grid-cols-3" : "grid gap-1.5"}>
            {Array.from({ length: view === "grid" ? 6 : 8 }, (_, i) => (view === "grid" ? <CardSkeleton key={i} /> : <RowSkeleton key={i} />))}
          </ul>
        )}

        {projects.data && list.length === 0 && <EmptyDashboard editable={editable} onNew={startNew} />}

        {list.length > 0 && (
          <ul aria-label="Projects" className={cx("animate-fade-in", view === "grid" ? "grid grid-cols-1 gap-4 md:grid-cols-2 lg:grid-cols-3" : "grid gap-1.5")}>
            {list.map((p: Project) => view === "grid"
              ? <ProjectCard key={p.id} p={p} favourite={favSet.has(p.id)} onFavourite={toggleFavourite} />
              : <ProjectRow key={p.id} p={p} favourite={favSet.has(p.id)} onFavourite={toggleFavourite} />)}
          </ul>
        )}
      </div>

      {limit && (
        <LimitDialog limit={limit} admin={admin} onClose={() => setLimit(null)} onCreateAnyway={() => { setLimit(null); nav("/new"); }} />
      )}
    </div>
  );
}

const QUICK_STARTS = [
  { to: "/new/github", label: "GitHub Repository", tile: <BrandTile name="github" /> },
  { to: "/new/database", label: "Database", tile: <BrandTile name="postgresql" /> },
  { to: "/new/templates", label: "Template", tile: <span className="inline-flex h-10 w-10 items-center justify-center rounded-lg border border-line-popover bg-surface-hover text-fg-2"><LayoutTemplate size={20} /></span> },
  { to: "/new/image", label: "Docker Image", tile: <BrandTile name="docker" /> },
];

/** No projects yet: the dashed empty card with the four ways to start. */
function EmptyDashboard({ editable, onNew }: { editable: boolean; onNew: () => void }) {
  const nav = useNavigate();
  return (
    <EmptyState
      className="animate-rise-in px-6 py-10"
      icon={<LayoutGrid />}
      title="No projects yet"
      body={editable
        ? "A project groups the services, databases and environments of one app. Start from a repository, a database, a template or an image."
        : "Projects you are added to will show up here."}
      action={editable && (
        <div className="flex flex-col items-center gap-6">
          <Button variant="primary" icon={<Plus size={16} />} onClick={onNew}>New Project</Button>
          <ul className="grid grid-cols-2 gap-3 sm:grid-cols-4">
            {QUICK_STARTS.map((q) => (
              <li key={q.to}>
                <button type="button" onClick={() => nav(q.to)}
                  className="group flex w-40 flex-col items-center gap-2 whitespace-nowrap rounded-lg border border-line-popover bg-surface px-3 py-4 text-sm text-fg-2 transition-colors duration-150 hover:bg-surface-hover hover:text-foreground">
                  <span className="transition-transform duration-150 group-hover:-translate-y-0.5">{q.tile}</span>
                  {q.label}
                </button>
              </li>
            ))}
          </ul>
        </div>
      )}
    />
  );
}
