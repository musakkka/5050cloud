// SPDX-License-Identifier: AGPL-3.0-only

import { useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { BookOpen, ExternalLink, LayoutGrid, List, Plus, Search, Server, Star } from "lucide-react";
import { api, type ProjectSummary } from "../api";
import { Button, ResourceIcon, Spinner, cx, fmtBytes, usePoll } from "../ui/kit";
import Logo, { Wordmark } from "../ui/Logo";

export default function Dashboard() {
  const { data } = usePoll(api.projects, [], 15000);
  const hosts = usePoll(api.hosts, [], 30000).data;
  const me = usePoll(api.me, [], 0).data;
  const installName = me?.installName || "5050cloud";
  const hostCount = hosts?.length ?? 0;
  const [q, setQ] = useState("");
  const [view, setView] = useState<"grid" | "list">("grid");
  const [stars, setStars] = useState<string[]>(() => { try { return JSON.parse(localStorage.getItem("5050.stars") || "[]"); } catch { return []; } });
  const toggleStar = (id: string) => {
    const next = stars.includes(id) ? stars.filter((s) => s !== id) : [...stars, id];
    setStars(next);
    try { localStorage.setItem("5050.stars", JSON.stringify(next)); } catch { /* private mode */ }
  };
  const list = useMemo(() => {
    const all = (data || []).filter((p) => p.name.toLowerCase().includes(q.toLowerCase()));
    const weight = (p: ProjectSummary) => (stars.includes(p.id) ? 0 : 1) * 1000 - p.envs.reduce((a, e) => a + e.total, 0);
    return all.sort((a, b) => weight(a) - weight(b));
  }, [data, q, stars]);

  return (
    <div className="h-full flex flex-col">
      <header className="h-[56px] shrink-0 flex items-center justify-between px-4">
        <Link to="/" className="flex items-center gap-2.5 text-foreground"><Logo size={26} /><Wordmark /></Link>
      </header>
      <div className="flex-1 min-h-0 flex">
        <aside className="w-[220px] shrink-0 flex flex-col">
          <div className="flex items-center gap-3 px-4 py-4 border-b border-gray-200">
            <div className="min-w-0 flex-1">
              <div className="text-sm font-semibold truncate">{installName}</div>
              <div className="text-[11px] font-medium text-gray-500 tracking-wide">{hostCount === 1 ? "1 host" : `${hostCount} hosts`}</div>
            </div>
          </div>
          <nav className="p-2 space-y-1 border-b border-gray-200">
            <SideItem icon={<LayoutGrid size={16} />} label="Projects" active />
            <SideItem icon={<Server size={16} />} label="Hosts" to="/settings/hosts" />
          </nav>
          <nav className="p-2 space-y-1 border-b border-gray-200">
            <SideItem icon={<BookOpen size={16} />} label="Docs" href="https://5050cloud.com/docs" />
          </nav>
          {hosts && (
            <div className="p-4 space-y-3">
              {hosts.map((h) => (
                <div key={h.role} className="text-xs text-gray-500">
                  <div className="flex justify-between text-gray-700 font-medium"><span>{h.name}</span><span className="text-gray-500 font-normal">{h.containers} running</span></div>
                  {h.disks?.map((d: any) => (
                    <div key={d.mount} className="mt-1.5">
                      <div className="flex justify-between"><span>{d.mount}</span><span>{fmtBytes(d.used, 0)} / {fmtBytes(d.size, 0)}</span></div>
                      <div className="mt-1 h-1 rounded bg-gray-100 overflow-hidden"><div className={cx("h-full rounded", d.used / d.size > 0.85 ? "bg-red-500" : d.used / d.size > 0.75 ? "bg-yellow-500" : "bg-pink-500")} style={{ width: `${(100 * d.used) / d.size}%` }} /></div>
                    </div>
                  ))}
                </div>
              ))}
            </div>
          )}
          <div className="mt-auto flex items-center gap-3 px-4 py-4">
            <div className="w-7 h-7 rounded-full bg-gray-200 flex items-center justify-center text-xs">{(me?.email || "?")[0].toUpperCase()}</div>
            <div className="text-sm truncate">{me?.email || ""}</div>
          </div>
        </aside>
        <main className="flex-1 min-w-0 overflow-y-auto scroll-thin border border-gray-200 rounded-xl mr-2 mb-2 bg-background">
          <div className="max-w-[1120px] mx-auto px-8 py-14">
            <div className="flex items-center justify-between">
              <h1 className="text-[28px] font-medium">Projects</h1>
              <div className="flex items-center gap-3">
                <label className="flex items-center gap-2 h-[34px] w-[224px] px-3 rounded-md border border-gray-200 focus-within:border-pink-400">
                  <Search size={15} className="text-gray-500" />
                  <input value={q} onChange={(e) => setQ(e.target.value)} placeholder="Search projects..." className="bg-transparent outline-none text-sm flex-1 min-w-0 placeholder:text-gray-500" />
                </label>
                <Link to="/new">
                  <Button variant="primary"><Plus size={16} />New</Button>
                </Link>
              </div>
            </div>
            <div className="flex items-center justify-between mt-10 mb-5">
              <div className="flex items-center gap-3 text-sm text-gray-700">
                <span className="flex items-center gap-2"><LayoutGrid size={14} className="text-gray-500" />{list.length} Projects</span>
                <span className="w-px h-4 bg-gray-200" />
                <span className="text-gray-600">Sort By: Size</span>
              </div>
              <div className="flex rounded-md border border-gray-200 p-0.5">
                <button onClick={() => setView("grid")} className={cx("p-1.5 rounded", view === "grid" ? "bg-gray-100 text-foreground" : "text-gray-500")}><LayoutGrid size={14} /></button>
                <button onClick={() => setView("list")} className={cx("p-1.5 rounded", view === "list" ? "bg-gray-100 text-foreground" : "text-gray-500")}><List size={14} /></button>
              </div>
            </div>
            {!data && <div className="py-20 flex justify-center"><Spinner /></div>}
            <div className={cx(view === "grid" ? "grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4" : "flex flex-col gap-2")}>
              {list.map((p) => <ProjectCard key={p.id} p={p} starred={stars.includes(p.id)} onStar={() => toggleStar(p.id)} compact={view === "list"} />)}
            </div>
          </div>
        </main>
      </div>
    </div>
  );
}

function SideItem({ icon, label, active, to, href }: { icon: React.ReactNode; label: string; active?: boolean; to?: string; href?: string }) {
  const cls = cx("flex items-center gap-3 px-3 h-9 rounded-md text-sm", active ? "bg-gray-100 text-foreground" : "text-gray-600 hover:text-foreground hover:bg-gray-50");
  const inner = <><span className={active ? "text-pink-600" : ""}>{icon}</span>{label}{href && <ExternalLink size={11} className="text-gray-400" />}</>;
  if (href) return <a className={cls} href={href} target="_blank" rel="noreferrer">{inner}</a>;
  if (to) return <Link className={cls} to={to}>{inner}</Link>;
  return <div className={cls}>{inner}</div>;
}

function ProjectCard({ p, starred, onStar, compact }: { p: ProjectSummary; starred: boolean; onStar: () => void; compact: boolean }) {
  const prod = p.envs.find((e) => e.name === "production") || p.envs[0];
  const online = prod?.online ?? 0, total = prod?.total ?? 0;
  const footer = (
    <div className="flex items-center gap-2 text-[13px] text-gray-600">
      <span className={cx("w-1.5 h-1.5 rounded-full", online === total ? "bg-green-500" : online === 0 ? "bg-red-500" : "bg-yellow-500")} />
      <span>{prod?.name}</span><span className="text-gray-400">·</span>
      <span>{online}/{total} service{total === 1 ? "" : "s"} online</span>
    </div>
  );
  if (compact) {
    return (
      <Link to={`/project/${p.id}?env=${prod?.name}`} className="flex items-center justify-between rounded-xl border border-gray-200 bg-secondaryBg px-5 py-4 hover:border-gray-400">
        <span className="font-medium text-[15px]">{p.name}</span>{footer}
      </Link>
    );
  }
  return (
    <Link to={`/project/${p.id}?env=${prod?.name}`} className="group block rounded-xl border border-gray-200 bg-secondaryBg hover:border-gray-400 transition-colors">
      <div className="flex items-center justify-between px-4 pt-4 pb-3">
        <span className="font-medium text-[15px] truncate">{p.name}</span>
        <button onClick={(e) => { e.preventDefault(); onStar(); }} className={cx("p-1", starred ? "text-yellow-500" : "text-gray-400 hover:text-gray-700")}>
          <Star size={16} fill={starred ? "currentColor" : "none"} />
        </button>
      </div>
      <div className="mx-2 mb-2 rounded-lg border border-gray-100 bg-background dot-grid h-[220px] flex flex-col">
        <div className="flex-1 flex items-center justify-center gap-2 px-6">
          {prod?.icons.slice(0, (prod?.total ?? 0) > 5 ? 4 : 5).map((ic, i) => (
            <div key={i} className="w-10 h-10 rounded-lg border border-gray-200 bg-secondaryBg flex items-center justify-center">
              <ResourceIcon icon={ic.icon} kind={ic.kind} source={ic.source} size={20} />
            </div>
          ))}
          {(prod?.total ?? 0) > 5 && <div className="w-10 h-10 rounded-lg border border-gray-200 bg-secondaryBg flex items-center justify-center text-xs text-gray-600">+{prod!.total - 4}</div>}
        </div>
        <div className="px-3 pb-3">{footer}</div>
      </div>
    </Link>
  );
}
