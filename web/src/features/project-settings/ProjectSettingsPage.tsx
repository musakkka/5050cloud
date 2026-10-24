// SPDX-License-Identifier: AGPL-3.0-only

// /project/:pid/settings/:section (docs/SPEC.md 13.1).
import { useEffect } from "react";
import { NavLink, Navigate, useLocation, useParams } from "react-router-dom";
import { Box, ChartColumn, Coins, Globe, Server, Settings, TriangleAlert, Users, Webhook, type LucideIcon } from "lucide-react";
import type * as T from "../../api/types.gen";
import { useProject } from "../../app/project";
import { cx } from "../../kit";
import { v1 } from "../settings/client";
import { isAdmin } from "../settings/InstallSettings";
import ProjectMembers from "../members/ProjectMembers";
import Danger from "./Danger";
import Environments from "./Environments";
import General from "./General";
import Integrations from "./Integrations";
import SharedVariables from "./SharedVariables";
import Tokens from "./Tokens";
import Usage from "./Usage";
import Webhooks from "./Webhooks";
import { LoadError, SectionSkeleton, useLoad } from "./ui";

// Feature flags are left out on purpose: 5050cloud has no flag service yet and we do not ship dead pages.
export const PROJECT_SECTIONS: { id: string; label: string; icon: LucideIcon }[] = [
  { id: "general", label: "General", icon: Settings },
  { id: "usage", label: "Usage", icon: ChartColumn },
  { id: "environments", label: "Environments", icon: Server },
  { id: "shared-variables", label: "Shared Variables", icon: Globe },
  { id: "webhooks", label: "Webhooks", icon: Webhook },
  { id: "members", label: "Members", icon: Users },
  { id: "tokens", label: "Tokens", icon: Coins },
  { id: "integrations", label: "Integrations", icon: Box },
  { id: "danger", label: "Danger", icon: TriangleAlert },
];

export default function ProjectSettingsPage() {
  const { pid = "", section = "general" } = useParams();
  const { search } = useLocation();
  const { env, reloadProject } = useProject();
  const project = useLoad(() => v1.project(pid), [pid]);
  const me = useLoad(v1.me, []);
  useEffect(() => { document.title = "Project Settings"; }, []);
  if (!PROJECT_SECTIONS.some((s) => s.id === section)) return <Navigate to={`/project/${pid}/settings/general${search}`} replace />;
  const p = project.data;
  const admin = me.data ? isAdmin(me.data.user.role) : false;
  const current: T.Environment | undefined = p?.environments.find((e) => e.id === env?.id) || p?.environments[0];
  // keep the project switcher in step after edits made here
  const onProject = (next: T.Project) => { project.setData(next); reloadProject?.(); };
  return (
    <div className="flex h-full flex-col">
      <header className="flex h-[98px] shrink-0 items-center border-b border-line px-6 md:px-12">
        <h1 className="text-h1 text-foreground">Project Settings</h1>
      </header>
      <div className="flex min-h-0 flex-1 flex-col overflow-y-auto scroll-thin md:flex-row md:overflow-hidden">
        <nav aria-label="Project settings" className="no-scrollbar flex shrink-0 gap-2 overflow-x-auto px-4 py-3 md:block md:w-[292px] md:space-y-2 md:overflow-y-auto md:py-12 md:pl-[52px] md:pr-[52px]">
          {PROJECT_SECTIONS.map((s) => (
            <NavLink key={s.id} to={`/project/${pid}/settings/${s.id}${search}`}
              className={({ isActive }) => cx("group flex h-10 shrink-0 items-center gap-4 whitespace-nowrap rounded px-3 text-lead transition-colors md:w-[188px]",
                isActive ? "font-medium text-foreground" : "text-fg-2 hover:bg-surface-hover")}>
              {({ isActive }) => <><s.icon size={20} className={cx("shrink-0 transition-colors", isActive ? "text-accent-text" : "text-fg-4")} />{s.label}</>}
            </NavLink>
          ))}
        </nav>
        <main className="min-w-0 flex-1 px-4 pb-16 pt-6 md:overflow-y-auto md:px-0 md:pb-24 md:pt-12 scroll-thin">
          <div key={section} className="max-w-[768px] animate-fade-in md:pr-8">
            {project.error || me.error ? <LoadError error={(project.error || me.error)!} onRetry={() => { project.reload(); me.reload(); }} />
              : !p || !me.data ? <SectionSkeleton />
              : <Section id={section} p={p} current={current} admin={admin} onProject={onProject} search={search} />}
          </div>
        </main>
      </div>
    </div>
  );
}

function Section({ id, p, current, admin, onProject, search }: { id: string; p: T.Project; current?: T.Environment; admin: boolean; onProject: (p: T.Project) => void; search: string }) {
  switch (id) {
    case "general": return <General project={p} onProject={onProject} admin={admin} />;
    case "usage": return <Usage project={p} env={current} />;
    case "environments": return <Environments project={p} current={current} onProject={onProject} admin={admin} />;
    case "shared-variables": return <SharedVariables project={p} current={current} />;
    case "webhooks": return <Webhooks project={p} />;
    case "members": return <ProjectMembers project={p} admin={admin} />;
    case "tokens": return <Tokens environments={p.environments} current={current} />;
    case "integrations": return <Integrations pid={p.id} query={search} />;
    default: return <Danger project={p} env={current} admin={admin} />;
  }
}
