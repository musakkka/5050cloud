// SPDX-License-Identifier: AGPL-3.0-only

// /project/:pid/settings/:section (docs/SPEC.md 13.1).
import { Navigate, useParams, useSearchParams } from "react-router-dom";
import TokensPanel from "../account/TokensPanel";
import { v1 } from "../settings/client";
import { isAdmin } from "../settings/InstallSettings";
import { Loading, Page, Select, useLoad } from "../settings/ui";
import UsagePanel from "../usage/UsagePanel";
import { Danger, Environments, General, Members, SharedVariables, Webhooks } from "./sections";

export const PROJECT_SECTIONS = [
  { id: "general", label: "General" },
  { id: "usage", label: "Usage" },
  { id: "environments", label: "Environments" },
  { id: "shared-variables", label: "Shared Variables" },
  { id: "webhooks", label: "Webhooks" },
  { id: "members", label: "Members" },
  { id: "tokens", label: "Tokens" },
  { id: "danger", label: "Danger" },
];

export default function ProjectSettingsPage() {
  const { pid = "", section = "general" } = useParams();
  const [params, setParams] = useSearchParams();
  const project = useLoad(() => v1.project(pid), [pid]);
  const me = useLoad(v1.me, []);
  if (!PROJECT_SECTIONS.some((s) => s.id === section)) return <Navigate to={`/project/${pid}/settings/general`} replace />;
  const q = params.toString() ? `?${params}` : "";
  const nav = PROJECT_SECTIONS.map((s) => ({ to: `/project/${pid}/settings/${s.id}${q}`, label: s.label }));
  if (!project.data || !me.data) return <Page title="Project Settings" nav={nav}><Loading error={project.error || me.error} /></Page>;
  const p = project.data;
  const admin = isAdmin(me.data.user.role);
  const envName = params.get("env");
  const env = p.environments.find((e) => e.name === envName || e.id === envName) || p.environments[0];
  const envPicker = env && (section === "shared-variables" || section === "danger") && p.environments.length > 1 && (
    <div className="max-w-xs mb-4">
      <Select aria-label="Environment" value={env.id} options={p.environments.map((e) => ({ value: e.id, label: e.name }))}
        onChange={(e) => { const next = new URLSearchParams(params); next.set("env", p.environments.find((x) => x.id === e.target.value)!.name); setParams(next); }} />
    </div>
  );
  return (
    <Page title="Project Settings" nav={nav}>
      {envPicker}
      {section === "general" && <General project={p} onProject={project.setData} admin={admin} />}
      {section === "usage" && <UsagePanel pid={p.id} />}
      {section === "environments" && <Environments project={p} onProject={project.setData} admin={admin} />}
      {section === "shared-variables" && env && <SharedVariables env={env} />}
      {section === "webhooks" && <Webhooks project={p} />}
      {section === "members" && <Members project={p} admin={admin} />}
      {section === "tokens" && <TokensPanel environments={p.environments} />}
      {section === "danger" && env && <Danger project={p} env={env} admin={admin} />}
    </Page>
  );
}
