// SPDX-License-Identifier: AGPL-3.0-only

// /project/:pid: top bar with the environment switcher, Activity and the
// bell; left rail Architecture, Observability, Logs, Settings; the canvas
// under every architecture route (SPEC 13.1).
import { lazy, Suspense, useState } from "react";
import { Link, NavLink, Outlet, useLocation } from "react-router-dom";
import { Activity as ActivityIcon, ChevronDown, Network, LineChart, ScrollText, Settings } from "lucide-react";
import { api } from "../api/client";
import { NewEnvironment } from "../features/palette/Palette";
import { ProjectProvider, useProject } from "./project";
import { TopBar } from "./Shell";
import { fullPage } from "./routes";
import { Button, ErrorLine, MenuItem, Popover, Spinner, cx, toast, toastError } from "../kit";
import { ActivityPanel } from "../features/activity/ActivityPanel";

const CanvasView = lazy(() => import("../features/canvas/Canvas"));

export default function ProjectLayout() {
  return <ProjectProvider><Layout /></ProjectProvider>;
}

function EnvSwitcher() {
  const { project, eid, env, setEnvironment, reload } = useProject();
  const [creating, setCreating] = useState(false);
  if (!project) return null;
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
      <Popover minWidth={220} trigger={() => (
        <button type="button" aria-label="Environment" className="flex h-8 max-w-[180px] items-center gap-1 rounded-md border border-gray-200 px-2 text-sm hover:border-gray-400">
          <span className="truncate">{env?.name}</span><ChevronDown size={13} className="shrink-0 text-gray-500" />
        </button>
      )}>
        {(close) => (
          <>
            {project.environments.map((e) => <MenuItem key={e.id} active={e.id === eid} onClick={() => { close(); setEnvironment(e.id); }}>{e.name}</MenuItem>)}
            <div className="my-1 border-t border-gray-200" />
            <MenuItem onClick={() => { close(); setCreating(true); }}>+ New Environment</MenuItem>
            {others.map((e) => <MenuItem key={`s${e.id}`} onClick={() => { close(); void sync(e.id); }}>Sync from {e.name}</MenuItem>)}
          </>
        )}
      </Popover>
      {creating && <NewEnvironment onClose={() => setCreating(false)} />}
    </>
  );
}

function Layout() {
  const { pid, project, error, withEnv } = useProject();
  const loc = useLocation();
  const [activity, setActivity] = useState(false);
  const rest = loc.pathname.replace(/^\/project\/[^/]+\/?/, "").split("/")[0];
  const showCanvas = !fullPage.has(rest);
  const base = `/project/${pid}`;
  const rail = [
    { to: base, label: "Architecture", icon: Network, end: true, active: showCanvas },
    { to: `${base}/observability`, label: "Observability", icon: LineChart },
    { to: `${base}/logs`, label: "Logs", icon: ScrollText },
    { to: `${base}/settings/general`, label: "Settings", icon: Settings, active: rest === "settings" },
  ];
  return (
    <div className="flex h-full flex-col">
      <TopBar
        left={<>
          <Link to="/" className="hidden text-gray-500 hover:text-foreground sm:inline">Projects</Link>
          <span className="hidden text-gray-400 sm:inline">/</span>
          <span className="truncate font-medium">{project?.name || ""}</span>
          <EnvSwitcher />
        </>}
        right={<Button variant="ghost" aria-label="Activity" onClick={() => setActivity(true)}><ActivityIcon size={15} /><span className="hidden sm:inline">Activity</span></Button>}
      />
      <div className="flex min-h-0 flex-1 flex-col-reverse sm:flex-row">
        <nav aria-label="Project" className="flex shrink-0 justify-around border-t border-gray-200 bg-background sm:w-14 sm:flex-col sm:justify-start sm:gap-1 sm:border-r sm:border-t-0 sm:py-3">
          {rail.map((r) => (
            <NavLink key={r.label} to={withEnv(r.to)} end={r.end} title={r.label} aria-label={r.label}
              className={({ isActive }) => cx("mx-auto flex h-10 w-10 items-center justify-center rounded-md", (r.active ?? isActive) ? "bg-gray-100 text-foreground" : "text-gray-500 hover:text-foreground")}>
              <r.icon size={18} />
            </NavLink>
          ))}
        </nav>
        <main className="relative min-h-0 min-w-0 flex-1 overflow-hidden">
          {error && !project && <div className="p-6"><ErrorLine error={error} /></div>}
          {showCanvas && project && <Suspense fallback={<div className="flex h-full items-center justify-center"><Spinner /></div>}><CanvasView /></Suspense>}
          <Suspense fallback={null}><Outlet /></Suspense>
        </main>
      </div>
      {activity && <ActivityPanel onClose={() => setActivity(false)} />}
    </div>
  );
}
