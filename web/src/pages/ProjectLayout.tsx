// SPDX-License-Identifier: AGPL-3.0-only

import { createContext, useContext, useMemo, useState } from "react";
import { Link, NavLink, Outlet, useNavigate, useParams, useSearchParams } from "react-router-dom";
import { Activity as ActivityIcon, ChartNoAxesColumn, ChevronDown, FileText, Network, Settings } from "lucide-react";
import { api, type Activity, type Canvas } from "../api";
import { MenuItem, Popover, Spinner, StatusGlyph, cx, timeAgo, usePoll } from "../ui/kit";
import Logo from "../ui/Logo";

interface Ctx { canvas: Canvas | null; reload: () => void; env: string; pid: string; withEnv: (path: string) => string }
const ProjectCtx = createContext<Ctx>(null as unknown as Ctx);
export const useProject = () => useContext(ProjectCtx);

export default function ProjectLayout() {
  const { pid = "" } = useParams();
  const [sp] = useSearchParams();
  const env = sp.get("env") || "production";
  const { data: canvas, reload } = usePoll(() => api.canvas(pid, env), [pid, env], 8000);
  const [showActivity, setShowActivity] = useState(() => { try { return localStorage.getItem("5050.activity") !== "0"; } catch { return true; } });
  const toggleActivity = () => { const v = !showActivity; setShowActivity(v); try { localStorage.setItem("5050.activity", v ? "1" : "0"); } catch { /* ignore */ } };
  const withEnv = (path: string) => `${path}${path.includes("?") ? "&" : "?"}env=${encodeURIComponent(env)}`;
  const ctx = useMemo(() => ({ canvas, reload, env, pid, withEnv }), [canvas, env, pid]); // eslint-disable-line react-hooks/exhaustive-deps

  return (
    <ProjectCtx.Provider value={ctx}>
      <div className="h-full flex flex-col">
        <TopBar onActivity={toggleActivity} activityOn={showActivity} />
        <div className="flex-1 min-h-0 flex gap-2 pr-2 pb-2">
          <Rail />
          <div className="flex-1 min-w-0 relative rounded-xl border border-gray-200 bg-background overflow-hidden">
            <Outlet />
          </div>
          {showActivity && <ActivityPanel />}
        </div>
      </div>
    </ProjectCtx.Provider>
  );
}

function TopBar({ onActivity, activityOn }: { onActivity: () => void; activityOn: boolean }) {
  const { canvas, env, pid } = useProject();
  const projects = usePoll(api.projects, [], 0).data;
  const nav = useNavigate();
  const me = usePoll(api.me, [], 0).data;
  return (
    <header className="h-[56px] shrink-0 flex items-center justify-between px-4">
      <div className="flex items-center gap-3 min-w-0">
        <Link to="/" className="text-foreground"><Logo size={26} /></Link>
        <span className="w-px h-5 bg-gray-200 mx-1" />
        <Dropdown
          label={<span className="truncate max-w-[220px]">{canvas?.project.name || pid}</span>}
          items={(projects || []).map((p) => ({ label: p.name, active: p.id === pid, onClick: () => nav(`/project/${p.id}?env=${p.envs.find((e) => e.name === "production")?.name || p.envs[0]?.name}`) }))}
        />
        <span className="text-gray-300 text-lg">/</span>
        <Dropdown
          label={<span>{env}</span>}
          items={(canvas?.project.envs || [env]).map((e) => ({ label: e, active: e === env, onClick: () => nav(`/project/${pid}?env=${e}`) }))}
        />
      </div>
      <div className="flex items-center gap-2">
        <button onClick={onActivity} title="Activity"
          className={cx("w-[30px] h-[30px] rounded-md flex items-center justify-center", activityOn ? "bg-gray-100 text-gray-800" : "text-gray-500 hover:text-foreground")}>
          <ActivityIcon size={16} />
        </button>
        <span className="w-px h-5 bg-gray-200 mx-1" />
        <div title={me?.email || ""} className="w-7 h-7 rounded-full bg-gray-200 flex items-center justify-center text-xs">{(me?.email || "?")[0].toUpperCase()}</div>
      </div>
    </header>
  );
}

function Dropdown({ label, items }: { label: React.ReactNode; items: { label: string; active?: boolean; onClick: () => void }[] }) {
  return (
    <Popover minWidth={240} className="max-h-[60vh]" trigger={() => (
      <button className="flex items-center gap-2 h-[30px] px-2 rounded-md border border-transparent hover:border-gray-200 text-sm font-medium min-w-0">
        {label}<ChevronDown size={14} className="text-gray-500 shrink-0" />
      </button>
    )}>
      {(close) => items.map((it) => <MenuItem key={it.label} active={it.active} onClick={() => { close(); it.onClick(); }}>{it.label}</MenuItem>)}
    </Popover>
  );
}

function Rail() {
  const { pid, withEnv } = useProject();
  const items = [
    { to: `/project/${pid}`, icon: <Network size={16} />, label: "Architecture", end: false, match: (p: string) => !/\/(logs|observability|settings)$/.test(p) },
    { to: `/project/${pid}/observability`, icon: <ChartNoAxesColumn size={16} />, label: "Observability" },
    { to: `/project/${pid}/logs`, icon: <FileText size={16} />, label: "Logs" },
    { to: `/project/${pid}/settings`, icon: <Settings size={16} />, label: "Settings" },
  ];
  return (
    <nav className="w-[56px] shrink-0 flex flex-col items-center gap-1 pt-4">
      {items.map((it) => (
        <NavLink key={it.label} to={withEnv(it.to)} title={it.label} end={!it.match}
          className={({ isActive }) => {
            const active = it.match ? it.match(window.location.pathname) : isActive;
            return cx("w-10 h-10 rounded-lg flex items-center justify-center transition-colors", active ? "bg-gray-100 text-foreground" : "text-gray-500 hover:text-foreground");
          }}>
          {it.icon}
        </NavLink>
      ))}
    </nav>
  );
}

const TONE: Record<string, { name: string; text: string; dot: string }> = {
  success: { name: "text-green-800", text: "text-green-700", dot: "bg-green-500" },
  failed: { name: "text-red-800", text: "text-red-700", dot: "bg-red-500" },
  crashed: { name: "text-red-800", text: "text-red-700", dot: "bg-red-500" },
  warning: { name: "text-yellow-800", text: "text-yellow-700", dot: "bg-yellow-500" },
  progress: { name: "text-blue-800", text: "text-blue-700", dot: "bg-blue-500" },
  restarted: { name: "text-foreground", text: "text-gray-700", dot: "bg-blue-500" },
};

function ActivityPanel() {
  const { canvas, pid, withEnv } = useProject();
  const targets = (canvas?.nodes || []).map((n) => n.id);
  const [filter, setFilter] = useState<string>("");
  const [kind, setKind] = useState<string>("");
  const { data } = usePoll(() => (targets.length ? api.activity(targets) : new Promise<Activity[]>(() => {})), [targets.join(",")], 10000);
  const items = (data || []).filter((i) => (!filter || i.target === filter) && (!kind || (kind === "deploys" ? !!i.historyId : !i.historyId)));
  const names = Object.fromEntries((canvas?.nodes || []).map((n) => [n.id, n.name]));
  return (
    <aside className="w-[320px] shrink-0 rounded-xl border border-gray-200 bg-background flex flex-col overflow-hidden">
      <div className="px-5 pt-5 pb-3"><p className="font-semibold text-base">Activity</p></div>
      <div className="flex items-center gap-2 px-4 pb-2">
        <Select value={kind} onChange={setKind} options={[["", "All activity"], ["deploys", "Deployments"], ["events", "Container events"]]} />
        <Select value={filter} onChange={setFilter} options={[["", "All services"], ...Object.entries(names).sort((a, b) => a[1].localeCompare(b[1]))]} />
      </div>
      <div className="flex-1 overflow-y-auto scroll-thin px-2 pb-2">
        {!data && <div className="flex justify-center py-10"><Spinner /></div>}
        {data && items.length === 0 && <p className="px-4 py-6 text-sm text-gray-500">Nothing yet. Deploys, crashes and restarts show up here.</p>}
        {items.map((i, k) => {
          const t = TONE[i.kind] || TONE.restarted;
          const href = i.historyId ? `/project/${pid}/service/${encodeURIComponent(i.target)}/deployment/${i.historyId}` : `/project/${pid}/service/${encodeURIComponent(i.target)}`;
          return (
            <Link key={k} to={withEnv(href)} className="px-4 py-4 grid items-center gap-4 grid-cols-[auto_1fr] w-full text-left rounded-md hover:bg-gray-50">
              <StatusGlyph status={i.kind} />
              <div className="grid gap-2 min-w-0">
                <p className={cx("text-sm font-semibold truncate flex items-center", t.name)}><span className={cx("inline-block size-2 rounded-full shrink-0 mr-2", t.dot)} />{names[i.target] || i.name}</p>
                <p className={cx("text-sm", t.text)}>{i.text}</p>
                <p className="text-xs text-gray-500"><time>{timeAgo(i.ts)}</time></p>
              </div>
            </Link>
          );
        })}
      </div>
    </aside>
  );
}

export function Select({ value, onChange, options }: { value: string; onChange: (v: string) => void; options: [string, string][] }) {
  const cur = options.find((o) => o[0] === value)?.[1] || options[0][1];
  return (
    <Popover minWidth={200} trigger={(open) => (
      <button className={cx("flex items-center gap-2 border rounded-md h-[34px] px-2 hover:border-gray-400", open ? "border-gray-400" : "border-gray-200")}>
        <span className="text-xs text-gray-600 font-medium max-w-[110px] truncate">{cur}</span><ChevronDown size={12} className="text-gray-500" />
      </button>
    )}>
      {(close) => options.map(([v, l]) => <MenuItem key={v} active={v === value} onClick={() => { onChange(v); close(); }}>{l}</MenuItem>)}
    </Popover>
  );
}
