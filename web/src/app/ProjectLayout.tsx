// SPDX-License-Identifier: AGPL-3.0-only

// /project/:pid: header with the logo, project switcher and environment switcher, then the
// Activity toggle, bell and install status; a 56px rail (Architecture, Observability, Logs,
// Settings) with a sliding active pill and tooltips; the canvas as an r12 card, and the Activity
// feed docked beside it as a matching card (the canvas shrinks with a 200ms transition).
import { lazy, Suspense, useEffect, useMemo, useState } from "react";
import { Link, Outlet, useLocation, useNavigate } from "react-router-dom";
import { Activity as ActivityIcon, ChartNoAxesColumn, ChevronDown, FileText, Network, Plus, RefreshCw, Settings } from "lucide-react";
import { api, type Project } from "../api/client";
import { NewEnvironment, Palette } from "../features/palette/Palette";
import { Bell } from "../features/activity/Bell";
import { ActivityPanel } from "../features/activity/ActivityPanel";
import { ACTIVITY_EVENT, ACTIVITY_KEY } from "../features/activity/bridge";
import { ProjectProvider, useProject } from "./project";
import { DOCK_BELOW, InstallStatus, ShellSlotsProvider, UserMenu, useViewportWidth } from "./Shell";
import { fullPage } from "./routes";
import { useMe } from "./session";
import {
  Avatar, ErrorLine, IconButton, Logo, MenuItem, MenuLabel, MenuSeparator, NotFound, Popover, Skeleton, Spinner, Tooltip, VDivider,
  cx, readPref, toast, toastError, writePref,
} from "../kit";

const CanvasView = lazy(() => import("../features/canvas/Canvas"));

export default function ProjectLayout() {
  return <ProjectProvider><Layout /></ProjectProvider>;
}

/** Breadcrumb button: 30px, 14px/500 white label, muted chevron. */
function Crumb({ children, open, label }: { children: React.ReactNode; open: boolean; label: string }) {
  return (
    <button type="button" aria-label={label} aria-expanded={open}
      className={cx("flex h-[30px] max-w-[220px] items-center gap-1.5 rounded-md px-2 text-sm font-medium text-foreground transition-colors duration-150 hover:bg-surface-hover", open && "bg-surface-hover")}>
      {children}
      <ChevronDown size={16} className="shrink-0 text-fg-5" />
    </button>
  );
}

function ProjectSwitcher() {
  const me = useMe();
  const { project, pid } = useProject();
  const nav = useNavigate();
  const [list, setList] = useState<Project[]>();
  return (
    <Popover width={260} onOpenChange={(o) => { if (o && !list) api.projects("activity").then(setList, toastError); }}
      trigger={(open) => (
        <Crumb open={open} label="Switch project">
          <Avatar name={me.install.name} size={16} />
          <span className="truncate">{project?.name ?? <Skeleton w={80} h={14} />}</span>
        </Crumb>
      )}>
      {(close) => (
        <>
          <MenuLabel icon={<Avatar name={me.install.name} size={16} />}>{me.install.name} projects</MenuLabel>
          <div className="max-h-[60vh] overflow-y-auto scroll-thin">
            {!list && <div className="flex justify-center py-3"><Spinner size={16} /></div>}
            {list?.map((p) => (
              <MenuItem key={p.id} active={p.id === pid} checkSide="left" onClick={() => { close(); if (p.id !== pid) nav(`/project/${p.id}`); }}>{p.name}</MenuItem>
            ))}
          </div>
          <MenuSeparator />
          <MenuItem icon={<Plus />} accentIcon onClick={() => { close(); nav("/new"); }}>New project</MenuItem>
        </>
      )}
    </Popover>
  );
}

function EnvSwitcher() {
  const { project, eid, env, setEnvironment, reload } = useProject();
  const [creating, setCreating] = useState(false);
  if (!project) return <Skeleton w={90} h={16} />;
  const others = project.environments.filter((e) => e.id !== eid);
  const sync = async (from: string) => {
    try {
      const r = await api.syncEnvironment(eid, from);
      await reload();
      toast(`${r.changes.length} change${r.changes.length === 1 ? "" : "s"} staged from the sync. Review them in Details.`);
    } catch (e) { toastError(e); }
  };
  return (
    <>
      <Popover width={260} trigger={(open) => <Crumb open={open} label="Environment"><span className="truncate">{env?.name}</span></Crumb>}>
        {(close) => (
          <>
            <MenuLabel>Environments</MenuLabel>
            {project.environments.map((e) => (
              <MenuItem key={e.id} active={e.id === eid} onClick={() => { close(); setEnvironment(e.id); }}>{e.name}</MenuItem>
            ))}
            {others.length > 0 && (
              <>
                <MenuSeparator />
                <MenuLabel>Sync into {env?.name}</MenuLabel>
                {others.map((e) => <MenuItem key={`s${e.id}`} icon={<RefreshCw />} onClick={() => { close(); void sync(e.id); }}>From {e.name}</MenuItem>)}
              </>
            )}
            <MenuSeparator />
            <MenuItem icon={<Plus />} accentIcon onClick={() => { close(); setCreating(true); }}>New environment</MenuItem>
          </>
        )}
      </Popover>
      {creating && <NewEnvironment onClose={() => setCreating(false)} />}
    </>
  );
}

const RAIL_STEP = 44;   // 40px button + 4px gap

function Layout() {
  const { pid, project, error, withEnv } = useProject();
  const loc = useLocation();
  const vw = useViewportWidth();
  const docked = vw < DOCK_BELOW;
  const [activity, setActivityState] = useState<boolean>(() => readPref(ACTIVITY_KEY, false));
  const setActivity = (v: boolean) => { setActivityState(v); writePref(ACTIVITY_KEY, v); };
  useEffect(() => {
    const t = () => setActivityState((v) => { writePref(ACTIVITY_KEY, !v); return !v; });
    window.addEventListener(ACTIVITY_EVENT, t);
    return () => window.removeEventListener(ACTIVITY_EVENT, t);
  }, []);
  const rest = loc.pathname.replace(/^\/project\/[^/]+\/?/, "").split("/")[0];
  const showCanvas = !fullPage.has(rest);
  const base = `/project/${pid}`;
  const rail = useMemo(() => [
    { to: base, label: "Architecture", icon: Network, on: showCanvas },
    { to: `${base}/observability`, label: "Observability", icon: ChartNoAxesColumn, on: rest === "observability" },
    { to: `${base}/logs`, label: "Logs", icon: FileText, on: rest === "logs" },
    { to: `${base}/settings/general`, label: "Settings", icon: Settings, on: rest === "settings" },
  ], [base, showCanvas, rest]);
  const activeIdx = rail.findIndex((r) => r.on);
  const notFound = !!error && !project && /not found|404/i.test(String((error as { status?: number }).status ?? error.message));
  const panelOpen = activity && showCanvas && !docked;
  const slots = useMemo(() => ({ left: null, right: null, banner: null }), []);

  return (
    <ShellSlotsProvider slots={slots}>
      <div className="flex h-full flex-col bg-background text-foreground">
        <header className="flex h-14 shrink-0 items-center gap-3 px-4">
          <Link to="/" aria-label="Projects" className="flex shrink-0 items-center rounded-md text-foreground"><Logo size={24} /></Link>
          <span aria-hidden className="h-5 w-px shrink-0 bg-line" />
          <nav aria-label="Breadcrumb" className="flex min-w-0 flex-1 items-center gap-1">
            <ProjectSwitcher />
            <svg aria-hidden width="16" height="16" viewBox="0 0 16 16" className="shrink-0 text-fg-5"><path d="M10.5 3 5.5 13" stroke="currentColor" strokeLinecap="round" /></svg>
            <EnvSwitcher />
          </nav>
          <div className="flex shrink-0 items-center gap-3">
            {showCanvas && !docked && (
              <IconButton label={activity ? "Hide activity" : "Show activity"} active={activity} onClick={() => setActivity(!activity)}><ActivityIcon size={16} /></IconButton>
            )}
            <Bell />
            <VDivider />
            <InstallStatus compact={vw < 900} />
          </div>
        </header>

        <div className={cx("flex min-h-0 flex-1", docked && "flex-col-reverse")}>
          <nav aria-label="Project navigation"
            className={cx("relative flex shrink-0", docked ? "h-14 items-center justify-around border-t border-line px-2" : "w-rail flex-col items-center gap-1 px-2 pb-3 pt-4")}>
            {!docked && activeIdx >= 0 && (
              <span aria-hidden className="absolute left-2 top-4 h-10 w-10 rounded-lg bg-tint-pill transition-transform duration-250 ease-out"
                style={{ transform: `translateY(${activeIdx * RAIL_STEP}px)` }} />
            )}
            {rail.map((r) => (
              <Tooltip key={r.label} content={r.label} side="right" disabled={docked}>
                <Link to={withEnv(r.to)} aria-label={r.label} aria-current={r.on ? "page" : undefined}
                  className={cx("relative z-[1] flex h-10 w-10 items-center justify-center rounded-lg transition-colors duration-150",
                    r.on ? "text-foreground" : "text-fg-3 hover:text-foreground", docked && r.on && "bg-tint-pill")}>
                  <r.icon size={16} />
                </Link>
              </Tooltip>
            ))}
            {!docked && <div className="mt-auto"><UserMenu variant="avatar" placement="right" /></div>}
          </nav>

          <div className={cx("relative min-h-0 min-w-0 flex-1", docked ? "mx-2 mt-0" : "mb-2 mr-2")}>
            <main className="absolute inset-y-0 left-0 overflow-hidden rounded-panel border border-line bg-background transition-[right] duration-200 ease-out"
              style={{ right: panelOpen ? 328 : 0 }}>
              {notFound ? <NotFound title="This project does not exist" body="It may have been deleted, or you may not have access to it." /> : (
                <>
                  {error && !project && <div className="p-6"><ErrorLine error={error} /></div>}
                  {showCanvas && project && <Suspense fallback={<div className="flex h-full items-center justify-center"><Spinner /></div>}><CanvasView /></Suspense>}
                  {!showCanvas ? (
                    <div className="h-full overflow-y-auto scroll-thin"><Suspense fallback={null}><Outlet /></Suspense></div>
                  ) : <Suspense fallback={null}><Outlet /></Suspense>}
                </>
              )}
            </main>
            {panelOpen && project && (
              // The transform makes this card the containing block for the panel's fixed positioning.
              <div className="absolute inset-y-0 right-0 w-[320px] overflow-hidden rounded-panel border border-line bg-background animate-sheet-in [transform:translateZ(0)]">
                <ActivityPanel onClose={() => setActivity(false)} />
              </div>
            )}
          </div>
        </div>
        <Palette />
      </div>
    </ShellSlotsProvider>
  );
}
