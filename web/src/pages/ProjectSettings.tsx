// SPDX-License-Identifier: AGPL-3.0-only

import { useState } from "react";
import { Link } from "react-router-dom";
import { Copy, Layers, Server, Settings2, Shield } from "lucide-react";
import { api } from "../api";
import { ResourceIcon, Spinner, StatusGlyph, cx, fmtBytes, usePoll } from "../ui/kit";
import { useProject } from "./ProjectLayout";

const SECTIONS = [
  { id: "general", label: "General", icon: Settings2 },
  { id: "environments", label: "Environments", icon: Layers },
  { id: "hosts", label: "Hosts", icon: Server },
  { id: "access", label: "Access", icon: Shield },
];

export default function ProjectSettings() {
  const { canvas, pid, withEnv } = useProject();
  const [sec, setSec] = useState("general");
  const hosts = usePoll(api.hosts, [], 30000).data;
  const me = usePoll(api.me, [], 0).data;
  if (!canvas) return <div className="h-full flex items-center justify-center"><Spinner /></div>;
  return (
    <div className="h-full flex flex-col">
      <div className="px-12 py-8 border-b border-gray-200"><h1 className="text-2xl font-semibold">Project Settings</h1></div>
      <div className="flex-1 min-h-0 flex">
        <nav className="w-[240px] shrink-0 px-8 py-8 space-y-1">
          {SECTIONS.map((s) => (
            <button key={s.id} onClick={() => setSec(s.id)} className={cx("flex items-center gap-3 w-full h-10 text-[15px]", sec === s.id ? "text-foreground" : "text-gray-600 hover:text-foreground")}>
              <s.icon size={18} className={sec === s.id ? "text-pink-600" : ""} />{s.label}
            </button>
          ))}
        </nav>
        <div className="flex-1 min-w-0 overflow-y-auto scroll-thin px-6 py-8 max-w-[860px]">
          {sec === "general" && (
            <section>
              <h2 className="text-xl font-semibold mb-6">Project Info</h2>
              <label className="block text-sm text-gray-700 mb-1.5 font-medium">Name</label>
              <div className="h-10 px-3 flex items-center rounded-md border border-gray-200 text-[15px]">{canvas.project.name}</div>
              <label className="block text-sm text-gray-700 mb-1.5 mt-5 font-medium">Project ID</label>
              <div className="h-12 px-3 flex items-center justify-between rounded-md bg-secondaryBg font-mono text-sm">
                {canvas.project.id}<button onClick={() => navigator.clipboard.writeText(canvas.project.id)} className="text-gray-500 hover:text-foreground"><Copy size={15} /></button>
              </div>
              <h2 className="text-xl font-semibold mt-10 mb-4">Services</h2>
              <div className="rounded-xl border border-gray-200 overflow-hidden">
                {canvas.nodes.map((n) => (
                  <Link key={n.id} to={withEnv(`/project/${pid}/service/${encodeURIComponent(n.id)}/settings`)} className="flex items-center gap-3 px-4 py-2.5 border-b border-gray-100 last:border-b-0 hover:bg-gray-50 text-sm">
                    <StatusGlyph status={n.status} size={16} /><ResourceIcon icon={n.icon} kind={n.kind} source={n.source} size={16} />
                    <span className="flex-1 truncate">{n.name}</span>
                    <span className="text-xs text-gray-500 w-24 text-right truncate">{n.host}</span>
                  </Link>
                ))}
              </div>
            </section>
          )}
          {sec === "environments" && (
            <section>
              <h2 className="text-xl font-semibold mb-6">Environments</h2>
              {canvas.project.envs.map((e) => (
                <Link key={e} to={`/project/${pid}?env=${e}`} className="flex items-center justify-between rounded-lg border border-gray-200 px-4 h-12 mb-2 hover:border-gray-400">
                  <span>{e}</span>{e === canvas.env && <span className="text-xs text-pink-700">current</span>}
                </Link>
              ))}
            </section>
          )}
          {sec === "hosts" && (
            <section>
              <h2 className="text-xl font-semibold mb-6">Hosts</h2>
              {!hosts ? <Spinner /> : hosts.map((h) => (
                <div key={h.role} className="rounded-xl border border-gray-200 p-5 mb-4">
                  <div className="flex items-center justify-between"><span className="font-semibold">{h.name}</span><span className="text-sm text-gray-500">{(h.roles || []).join(", ")}</span></div>
                  <div className="mt-3 grid grid-cols-3 gap-3 text-sm text-gray-700">
                    <span>{h.cpus} vCPU</span><span>{fmtBytes(h.mem_total, 0)} memory</span><span>{h.containers} containers running</span>
                  </div>
                  {h.disks?.map((d: any) => (
                    <div key={d.mount} className="mt-3 text-sm">
                      <div className="flex justify-between text-gray-600"><span>Disk {d.mount}</span><span>{fmtBytes(d.used, 0)} of {fmtBytes(d.size, 0)}</span></div>
                      <div className="mt-1 h-1.5 rounded bg-gray-100 overflow-hidden"><div className={cx("h-full", d.used / d.size > 0.8 ? "bg-red-500" : "bg-pink-500")} style={{ width: `${(100 * d.used) / d.size}%` }} /></div>
                    </div>
                  ))}
                </div>
              ))}
            </section>
          )}
          {sec === "access" && (
            <section>
              <h2 className="text-xl font-semibold mb-6">Access</h2>
              <p className="text-sm text-gray-700 leading-6">You sign in with your email and password, plus a code when two-factor authentication is on. Every reveal of a variable, every change and every deploy is written to the audit log with the person who did it.</p>
              <div className="mt-5 rounded-lg border border-gray-200 px-4 py-3 text-sm">Signed in as <span className="font-medium">{me?.email}</span></div>
            </section>
          )}
        </div>
      </div>
    </div>
  );
}
