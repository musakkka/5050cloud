// SPDX-License-Identifier: AGPL-3.0-only

// Dashboard (SPEC 13.1): project cards with a mini canvas and an online
// summary, grid or list, search, stars, sort, hosts, update and restore banners.
import { useMemo, useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { LayoutGrid, List, Plus, Search, Server, Star } from "lucide-react";
import { api, type EnvSummary, type Host, type Project } from "../../api/client";
import { useLoad } from "../../api/hooks";
import { TopBar } from "../../app/Shell";
import { useMe } from "../../app/session";
import { Button, ErrorLine, Input, RelTime, ServiceIcon, Skeleton, TONE_DOT, cx, fmtBytes, toneOf } from "../../kit";

export type Sort = "activity" | "created" | "name";
const SORTS: { id: Sort; label: string }[] = [{ id: "activity", label: "Recent Activity" }, { id: "created", label: "Creation Date" }, { id: "name", label: "A-Z" }];

function readPref<T>(key: string, fallback: T): T {
  try { const v = localStorage.getItem(key); return v ? (JSON.parse(v) as T) : fallback; } catch { return fallback; }
}
function writePref(key: string, v: unknown) { try { localStorage.setItem(key, JSON.stringify(v)); } catch { /* private window */ } }

/** Filters by name and puts starred projects first, keeping the server's order otherwise. */
export function arrange(list: Project[], query: string, stars: string[]): Project[] {
  const q = query.trim().toLowerCase();
  const hit = q ? list.filter((p) => p.name.toLowerCase().includes(q) || p.description.toLowerCase().includes(q)) : list;
  return [...hit.filter((p) => stars.includes(p.id)), ...hit.filter((p) => !stars.includes(p.id))];
}

export function onlineText(s?: EnvSummary) {
  if (!s || !s.total) return "No services";
  return `${s.online}/${s.total} online`;
}

export default function Dashboard() {
  const me = useMe();
  const nav = useNavigate();
  const [sort, setSort] = useState<Sort>(() => readPref("5050.sort", "activity"));
  const [view, setView] = useState<"grid" | "list">(() => readPref("5050.view", "grid"));
  const [stars, setStars] = useState<string[]>(() => readPref("5050.stars", []));
  const [query, setQuery] = useState("");
  const projects = useLoad(() => api.projects(sort), [sort]);
  const hosts = useLoad(() => api.hosts(), []);
  const list = useMemo(() => arrange(projects.data || [], query, stars), [projects.data, query, stars]);
  const install = me.install;
  const toggleStar = (id: string) => { const n = stars.includes(id) ? stars.filter((x) => x !== id) : [...stars, id]; setStars(n); writePref("5050.stars", n); };

  return (
    <div className="flex min-h-full flex-col">
      <TopBar left={<span className="truncate font-medium">{install.name}</span>} />
      {install.restore && (
        <div role="status" className="border-b border-yellow-300 bg-yellow-100 px-4 py-2 text-sm text-yellow-700">
          Restored from a snapshot taken <RelTime ts={install.restore.snapshotAt} />. {install.restore.unknownReplicas} unknown replicas and {install.restore.unknownHosts} unknown hosts wait for review. <Link className="underline" to="/settings/hosts">Review</Link>
        </div>
      )}
      {install.latestVersion && install.latestVersion !== install.version && (
        <div role="status" className="border-b border-blue-300 bg-blue-100 px-4 py-2 text-sm text-blue-700">
          Version {install.latestVersion} is available (you run {install.version}). <Link className="underline" to="/settings/upgrades">Upgrade</Link>
        </div>
      )}
      <main className="mx-auto w-full max-w-[1200px] flex-1 space-y-6 px-4 py-8">
        <div className="flex flex-wrap items-center gap-3">
          <h1 className="mr-auto text-2xl font-semibold">Projects</h1>
          <div className="relative w-full sm:w-64">
            <Search size={15} className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-gray-400" />
            <Input aria-label="Search projects" placeholder="Search projects" value={query} onChange={(e) => setQuery(e.target.value)} className="pl-9" />
          </div>
          <select aria-label="Sort" value={sort} onChange={(e) => { const s = e.target.value as Sort; setSort(s); writePref("5050.sort", s); }}
            className="h-[38px] rounded-md border border-gray-200 bg-background px-2 text-sm">
            {SORTS.map((s) => <option key={s.id} value={s.id}>{s.label}</option>)}
          </select>
          <div className="flex overflow-hidden rounded-md border border-gray-200">
            {(["grid", "list"] as const).map((v) => (
              <button type="button" key={v} aria-label={v === "grid" ? "Grid view" : "List view"} aria-pressed={view === v}
                onClick={() => { setView(v); writePref("5050.view", v); }} className={cx("flex h-9 w-9 items-center justify-center", view === v ? "bg-gray-100 text-foreground" : "text-gray-500")}>
                {v === "grid" ? <LayoutGrid size={15} /> : <List size={15} />}
              </button>
            ))}
          </div>
          {me.user.role !== "viewer" && <Button variant="primary" onClick={() => nav("/new")} title="New (Cmd+/)"><Plus size={15} />New</Button>}
        </div>
        <ErrorLine error={projects.error} />
        {projects.loading && !projects.data && <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">{[0, 1, 2].map((i) => <Skeleton key={i} className="h-44" />)}</div>}
        {projects.data && list.length === 0 && (
          <div className="rounded-xl border border-dashed border-gray-200 px-6 py-12 text-center">
            <p className="text-sm text-gray-600">{query ? "No project matches your search." : "No projects yet."}</p>
            {!query && me.user.role !== "viewer" && <Button variant="primary" className="mt-4" onClick={() => nav("/new")}><Plus size={15} />New project</Button>}
          </div>
        )}
        <ul className={view === "grid" ? "grid gap-4 sm:grid-cols-2 lg:grid-cols-3" : "divide-y divide-gray-200 rounded-xl border border-gray-200"}>
          {list.map((p) => <ProjectCard key={p.id} p={p} view={view} starred={stars.includes(p.id)} onStar={() => toggleStar(p.id)} />)}
        </ul>
        <HostsCard hosts={hosts.data} admin={me.user.role === "admin" || me.user.role === "owner"} />
      </main>
    </div>
  );
}

function prodSummary(p: Project) {
  const prod = p.environments.find((e) => e.name === "production") || p.environments[0];
  return p.summary?.find((s) => s.environmentId === prod?.id) || p.summary?.[0];
}

function ProjectCard({ p, view, starred, onStar }: { p: Project; view: "grid" | "list"; starred: boolean; onStar: () => void }) {
  const s = prodSummary(p);
  const star = (
    <button type="button" aria-label={starred ? "Unstar" : "Star"} aria-pressed={starred} onClick={(e) => { e.preventDefault(); onStar(); }}
      className={cx("flex h-7 w-7 items-center justify-center rounded hover:bg-gray-100", starred ? "text-yellow-500" : "text-gray-400")}>
      <Star size={15} fill={starred ? "currentColor" : "none"} />
    </button>
  );
  if (view === "list") return (
    <li>
      <Link to={`/project/${p.id}`} className="flex items-center gap-4 px-4 py-3 hover:bg-gray-50">
        <span className="min-w-0 flex-1 truncate font-medium">{p.name}</span>
        <span className="hidden text-sm text-gray-500 sm:inline">{onlineText(s)}</span>
        <span className="hidden text-xs text-gray-500 md:inline"><RelTime ts={p.lastActivityAt} /></span>
        {star}
      </Link>
    </li>
  );
  return (
    <li>
      <Link to={`/project/${p.id}`} className="flex h-full flex-col rounded-xl border border-gray-200 bg-secondaryBg transition-colors hover:border-gray-400">
        <MiniCanvas s={s} />
        <div className="flex items-start gap-2 border-t border-gray-200 px-4 py-3">
          <div className="min-w-0 flex-1">
            <div className="truncate font-medium">{p.name}</div>
            <div className="flex items-center gap-2 text-sm text-gray-500">
              <span className={cx("h-2 w-2 rounded-full", s && s.total && s.online === s.total ? "bg-green-500" : s && s.total ? "bg-yellow-500" : "bg-gray-400")} />
              {onlineText(s)}
            </div>
          </div>
          {star}
        </div>
      </Link>
    </li>
  );
}

/** A small picture of the canvas: each service as an icon at its saved position. */
function MiniCanvas({ s }: { s?: EnvSummary }) {
  const mini = s?.mini || [];
  if (!mini.length) return (
    <div className="dot-grid flex h-28 items-center justify-center gap-2 rounded-t-xl">
      {(s?.icons || []).slice(0, 5).map((c, i) => <ServiceIcon key={i} icon={c.icon} source={c.source} size={22} />)}
      {!s?.icons?.length && <span className="text-xs text-gray-500">Empty</span>}
    </div>
  );
  const xs = mini.map((m) => m.position[0] || 0), ys = mini.map((m) => m.position[1] || 0);
  const x0 = Math.min(...xs), y0 = Math.min(...ys), w = Math.max(1, Math.max(...xs) - x0), h = Math.max(1, Math.max(...ys) - y0);
  return (
    <div className="dot-grid relative h-28 overflow-hidden rounded-t-xl">
      {mini.slice(0, 24).map((m) => (
        <span key={m.serviceId} className="absolute flex h-7 w-7 items-center justify-center rounded-md border border-gray-200 bg-secondaryBg"
          style={{ left: `calc(${((m.position[0] - x0) / w) * 80 + 6}% - 14px)`, top: `calc(${((m.position[1] - y0) / h) * 60 + 20}% - 14px)` }}>
          <ServiceIcon icon={m.icon} size={16} />
          <span className={cx("absolute -right-0.5 -top-0.5 h-2 w-2 rounded-full", TONE_DOT[toneOf(m.status)])} />
        </span>
      ))}
    </div>
  );
}

function HostsCard({ hosts, admin }: { hosts?: Host[]; admin: boolean }) {
  const nav = useNavigate();
  if (!hosts) return null;
  return (
    <section aria-label="Hosts" className="rounded-xl border border-gray-200 bg-secondaryBg">
      <div className="flex items-center gap-2 border-b border-gray-200 px-4 py-3">
        <Server size={16} className="text-gray-500" /><h2 className="mr-auto font-medium">Hosts</h2>
        {admin && <Button onClick={() => nav("/settings/hosts?add=1")}><Plus size={14} />Add host</Button>}
      </div>
      <ul className="divide-y divide-gray-200">
        {hosts.map((h) => (
          <li key={h.id} className="flex flex-wrap items-center gap-x-4 gap-y-2 px-4 py-3 text-sm">
            <span className={cx("h-2 w-2 rounded-full", h.state === "online" ? "bg-green-500" : h.state === "offline" || h.state === "lost" ? "bg-red-500" : "bg-yellow-500")} title={h.state} />
            <span className="min-w-[120px] font-medium">{h.name}</span>
            <span className="text-gray-500">{h.roles.join(", ")}</span>
            <span className="text-gray-500">{h.replicas} replicas</span>
            <div className="flex flex-1 flex-wrap gap-3">
              {h.disks.map((d) => {
                const pct = d.total ? Math.round((d.used / d.total) * 100) : 0;
                return (
                  <div key={d.mount} className="min-w-[140px] flex-1" title={`${d.mount}: ${fmtBytes(d.used)} of ${fmtBytes(d.total)}`}>
                    <div className="flex justify-between text-xs text-gray-500"><span>{d.mount}</span><span>{pct}%</span></div>
                    <div className="mt-1 h-1.5 rounded bg-gray-100" role="progressbar" aria-valuenow={pct} aria-valuemin={0} aria-valuemax={100} aria-label={`Disk ${d.mount}`}>
                      <div className={cx("h-full rounded", pct >= 90 ? "bg-red-500" : pct >= 80 ? "bg-yellow-500" : "bg-green-500")} style={{ width: `${pct}%` }} />
                    </div>
                  </div>
                );
              })}
            </div>
          </li>
        ))}
        {hosts.length === 0 && <li className="px-4 py-6 text-center text-sm text-gray-500">No hosts yet.</li>}
      </ul>
    </section>
  );
}
