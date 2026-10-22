// SPDX-License-Identifier: AGPL-3.0-only

// /settings/:section, the install-wide settings (docs/SPEC.md 13.1).
import { Navigate, useParams } from "react-router-dom";
import BackupsSettings from "../backups-settings/BackupsSettings";
import HostsSettings from "../hosts/HostsSettings";
import MembersSettings from "../members/MembersSettings";
import TokensPanel from "../account/TokensPanel";
import AuditLog from "./AuditLog";
import GeneralSettings from "./GeneralSettings";
import GitHubSettings from "./GitHubSettings";
import RegistrySettings from "./RegistrySettings";
import UpgradesSettings from "./UpgradesSettings";
import { v1 } from "./client";
import { Loading, Notice, Page, useLoad } from "./ui";

export const INSTALL_SECTIONS = [
  { id: "general", label: "General", admin: true },
  { id: "hosts", label: "Hosts", admin: false },
  { id: "github", label: "GitHub", admin: false },
  { id: "registry-credentials", label: "Registry credentials", admin: false },
  { id: "backups", label: "Backups", admin: true },
  { id: "members", label: "Members", admin: true },
  { id: "api-tokens", label: "API tokens", admin: false },
  { id: "upgrades", label: "Upgrades", admin: false },
  { id: "audit-log", label: "Audit log", admin: true },
];

export const isAdmin = (role?: string) => role === "owner" || role === "admin";

export default function InstallSettings() {
  const { section = "general" } = useParams();
  const me = useLoad(v1.me, []);
  if (!me.data) return <Page title="Settings" nav={[]}><Loading error={me.error} /></Page>;
  const { user, install } = me.data;
  const admin = isAdmin(user.role);
  const visible = INSTALL_SECTIONS.filter((s) => admin || !s.admin);
  if (!visible.some((s) => s.id === section)) return <Navigate to={`/settings/${visible[0].id}`} replace />;
  return (
    <Page title="Settings" nav={visible.map((s) => ({ to: `/settings/${s.id}`, label: s.label }))}>
      {install.readOnly && <Notice kind="error">The control host disk is full, so settings are read-only until space is freed.</Notice>}
      {section === "general" && <GeneralSettings />}
      {section === "hosts" && <HostsSettings />}
      {section === "github" && <GitHubSettings />}
      {section === "registry-credentials" && <RegistrySettings />}
      {section === "backups" && <BackupsSettings isOwner={user.role === "owner"} />}
      {section === "members" && <MembersSettings me={user} install={install} onInstall={(i) => me.setData({ user, install: i })} />}
      {section === "api-tokens" && <TokensPanel />}
      {section === "upgrades" && <UpgradesSettings />}
      {section === "audit-log" && <AuditLog />}
    </Page>
  );
}
