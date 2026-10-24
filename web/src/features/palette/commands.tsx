// SPDX-License-Identifier: AGPL-3.0-only

// The command tree behind the palette: every command, sub-menu and label rule. Built from the
// open project (if any), the selected service, the role and the project list.
import type { ReactNode } from "react";
import {
  Activity as ActivityIcon, Bell, BookOpen, Box, ChartColumn, ChartNoAxesColumn, CircleHelp, Coins, Copy, Eye, EyeOff, FileText, Folder, Globe,
  HardDrive, History, Info, KeyRound, LayoutGrid, Layers, Library, Lightbulb, MapPin, MessageCircle, Monitor, Network, Play, Plus, RotateCcw,
  Server, Settings, Shield, Sparkles, SquareTerminal, Store, Terminal, TriangleAlert, Undo2, User, Users, Wand2, Webhook,
} from "lucide-react";
import { api, type Project, type Service } from "../../api/client";
import type { ProjectCtx } from "../../app/project";
import { BrandIcon, ServiceIcon, readPref, setTheme, themePref, toast, toastError } from "../../kit";
import { NEW_MENU, newIcon, sendCanvas, type NewKind } from "../canvas/menus";
import { toggleActivity } from "../activity/bridge";
import { redeployLabel, type Cmd } from "./model";

const REPO = "https://github.com/musakkka/5050cloud";
const DOCS = `${REPO}/tree/main/docs`;

export const PROJECT_SETTINGS: { id: string; label: string; icon: ReactNode }[] = [
  { id: "general", label: "General", icon: <Settings /> },
  { id: "usage", label: "Usage", icon: <ChartColumn /> },
  { id: "environments", label: "Environments", icon: <Server /> },
  { id: "shared-variables", label: "Shared Variables", icon: <Globe /> },
  { id: "webhooks", label: "Webhooks", icon: <Webhook /> },
  { id: "members", label: "Members", icon: <Users /> },
  { id: "tokens", label: "Tokens", icon: <Coins /> },
  { id: "integrations", label: "Integrations", icon: <Box /> },
  { id: "danger", label: "Danger", icon: <TriangleAlert /> },
];

export const INSTALL_SETTINGS: { id: string; label: string; icon: ReactNode }[] = [
  { id: "general", label: "General", icon: <Settings /> },
  { id: "hosts", label: "Hosts", icon: <Server /> },
  { id: "github", label: "GitHub", icon: <BrandIcon name="github" size={16} mono /> },
  { id: "registry-credentials", label: "Registry credentials", icon: <KeyRound /> },
  { id: "backups", label: "Backups", icon: <HardDrive /> },
  { id: "members", label: "Members", icon: <Users /> },
  { id: "api-tokens", label: "API tokens", icon: <Coins /> },
  { id: "usage", label: "Usage", icon: <ChartColumn /> },
  { id: "upgrades", label: "Upgrades", icon: <Sparkles /> },
  { id: "audit-log", label: "Audit log", icon: <History /> },
];

const ACCOUNT_PAGES: { id: string; label: string; icon: ReactNode }[] = [
  { id: "profile", label: "Profile", icon: <User /> },
  { id: "notifications", label: "Notifications", icon: <Bell /> },
  { id: "feature-flags", label: "Feature Flags", icon: <Sparkles /> },
  { id: "tokens", label: "Tokens", icon: <KeyRound /> },
  { id: "security", label: "Security", icon: <Shield /> },
];

export interface Ctx {
  go: (path: string) => void;
  close: () => void;
  proj: ProjectCtx | null;
  editable: boolean;
  admin: boolean;
  serviceId?: string;
  projects?: Project[];
  newEnvironment: () => void;
  newProject: () => void;
  favourites: string[];
  toggleFavourite: (id: string) => void;
  activityOpen: boolean;
}

/** Copies text and toasts; falls back to a hidden textarea where the async clipboard needs a secure origin. */
const copy = (text: string, done: string) => {
  const ok = () => toast(done, "success");
  const legacy = () => {
    const t = document.createElement("textarea");
    t.value = text; t.setAttribute("readonly", ""); t.style.cssText = "position:fixed;opacity:0;pointer-events:none";
    document.body.appendChild(t); t.select();
    let good = false;
    try { good = document.execCommand("copy"); } catch { /* blocked */ }
    t.remove();
    if (good) ok(); else toast("Copy was blocked by the browser.", "error");
  };
  if (navigator.clipboard?.writeText) navigator.clipboard.writeText(text).then(ok, legacy);
  else legacy();
};

function secret(len = 32) {
  const a = new Uint8Array(len);
  crypto.getRandomValues(a);
  const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789";
  return [...a].map((b) => chars[b % chars.length]).join("");
}

export function buildCommands(x: Ctx): Cmd[] {
  const { go, close, proj, editable, admin, serviceId } = x;
  const out: Cmd[] = [];
  const services = proj?.canvas?.services || [];
  const name = (s: Service) => (s.staged || s.config).name;
  const svcIcon = (s: Service) => <ServiceIcon icon={s.config.icon} image={s.config.source?.image} source={s.config.source?.type} size={20} />;
  const sel = services.find((s) => s.id === serviceId);
  const path = (rest: string) => (proj ? proj.withEnv(`/project/${proj.pid}${rest}`) : "/");
  const svcPath = (s: Service, tab = "") => path(`/service/${encodeURIComponent(s.id)}${tab}`);
  const act = (s: Service, a: "redeploy" | "restart") =>
    api.deploymentAction(s.activeDeployment!, a).then(() => toast(`${a === "restart" ? "Restarting" : "Redeploying"} ${name(s)}`, "success"), toastError);

  // ---- service open: tab shortcuts first
  if (sel) {
    const tab = (id: string, title: string, key: string, icon: ReactNode, rest: string): Cmd =>
      ({ id, title, group: "Service", icon, chips: ["G", key], run: () => go(svcPath(sel, rest)) });
    out.push(tab("svc:deployments", "Deployments", "D", <Layers />, ""));
    out.push(tab("svc:variables", "Variables", "V", <Wand2 />, "/variables"));
    out.push(tab("svc:metrics", "Metrics", "M", <ChartNoAxesColumn />, "/metrics"));
    out.push(tab("svc:settings", "Service Settings", "S", <Settings />, "/settings"));
  }

  // ---- project
  if (proj?.project) {
    const p = proj.project;
    if (editable) {
      out.push({
        id: "new:service", title: "New Service", group: "Project", icon: <Plus />, accent: true, keywords: ["add", "create"], placeholder: "What do you want to add?",
        children: () => NEW_MENU.filter((m) => !m.disabled).map((m) => ({
          id: `new:${m.id}`, title: m.label, icon: newIcon(m.id, 20), keywords: ["add", "create", "new"],
          run: () => { close(); if (proj.canvas) sendCanvas({ kind: "new", what: m.id as NewKind }); else go("/new"); },
        })),
      });
    }
    out.push({
      id: "nav:env", title: "Change environment", group: "Project", icon: <Server />, placeholder: "Switch environment...", keywords: ["switch", "staging", "production"],
      children: () => [
        ...p.environments.filter((e) => e.id !== proj.eid).map((e): Cmd => ({ id: `e:${e.id}`, title: e.name, icon: <Server />, run: () => { close(); proj.setEnvironment(e.id); } })),
        ...(editable ? [{ id: "env:new", title: "New Environment", icon: <Plus />, accent: true, run: x.newEnvironment } as Cmd] : []),
      ],
    });
    out.push({ id: "nav:activity", title: x.activityOpen ? "Close Activity" : "Open Activity", group: "Project", icon: <ActivityIcon />, run: () => { close(); toggleActivity(); } });
    out.push({ id: "nav:arch", title: "Go to Architecture", group: "Project", icon: <Network />, run: () => go(path("")) });
    out.push({ id: "nav:obs", title: "Go to Observability", group: "Project", icon: <ChartNoAxesColumn />, run: () => go(path("/observability")) });
    out.push({ id: "nav:logs", title: "Go to Logs", group: "Project", icon: <FileText />, run: () => go(path("/logs")) });
    out.push({
      id: "nav:psettings", title: "Project Settings", group: "Project", icon: <Settings />, placeholder: "Search settings...",
      children: () => PROJECT_SETTINGS.map((s) => ({ id: `ps:${s.id}`, title: s.label, icon: s.icon, run: () => go(path(`/settings/${s.id}`)) })),
    });
    if (services.length) {
      out.push({
        id: "nav:service", title: "Project Services", group: "Project", icon: <Box />, placeholder: "Search services...",
        children: () => services.map((s) => ({ id: `s:${s.id}`, title: name(s), icon: svcIcon(s), run: () => go(svcPath(s)) })),
      });
    }
    out.push({
      id: "debug", title: "Debug Info", group: "Project", icon: <Info />,
      children: () => [
        { id: "debug:pid", title: "Copy Project ID", icon: <Info />, run: () => { close(); copy(p.id, "Project ID copied to clipboard"); } },
        { id: "debug:eid", title: "Copy Environment ID", icon: <Info />, run: () => { close(); copy(proj.eid, "Environment ID copied to clipboard"); } },
        { id: "debug:cli", title: "Show CLI Setup", icon: <SquareTerminal />, run: () => go(path("/settings/tokens")) },
      ],
    });

    // service actions: direct for the open service, a service picker otherwise
    if (proj.canvas && editable) {
      const forService = (id: string, label: string, icon: ReactNode, ok: (s: Service) => boolean, fn: (s: Service) => void, labelFor?: (s: Service) => string | null): Cmd | null => {
        if (sel) {
          if (!ok(sel)) return null;
          const l = labelFor ? labelFor(sel) : label;
          return l ? { id, title: l, subtitle: name(sel), group: "Service", icon, run: () => fn(sel) } : null;
        }
        const list = services.filter(ok);
        if (!list.length) return null;
        return { id, title: label, group: "Services", icon, placeholder: `${label}...`, children: () => list.map((s) => ({ id: `${id}:${s.id}`, title: name(s), subtitle: labelFor?.(s) || undefined, icon: svcIcon(s), run: () => fn(s) })) };
      };
      const cmds = [
        forService("deploy", "Deploy latest commit", <Play />, (s) => s.config.source.type === "github", (s) => { close(); api.deploy(proj.eid, s.id).then(() => toast(`Deploying ${name(s)}`, "success"), toastError); }),
        forService("redeploy", "Redeploy", <RotateCcw />, (s) => !!redeployLabel(s), (s) => { close(); void act(s, "redeploy"); }, redeployLabel),
        forService("restart", "Restart", <RotateCcw />, (s) => !!s.activeDeployment, (s) => { close(); void act(s, "restart"); }),
        forService("rollback", "Rollback", <Undo2 />, (s) => !!s.activeDeployment, (s) => {
          close();
          api.deployments(proj.eid, s.id, 20).then((ds) => {
            const prev = ds.find((d) => !d.active && d.canRollback && d.id !== s.activeDeployment && (d.status === "SUCCESS" || d.status === "REMOVED"));
            if (!prev) { toast("No earlier deployment to roll back to."); return; }
            return api.deploymentAction(prev.id, "rollback").then(() => toast(`Rolling ${name(s)} back`, "success"));
          }).catch(toastError);
        }),
        forService("shell", "Open shell", <Terminal />, (s) => !!s.activeDeployment, (s) => go(svcPath(s, "/shell"))),
        forService("info", "Update service info", <Info />, () => true, (s) => { close(); sendCanvas({ kind: "update-info", serviceId: s.id }); }),
        forService("heroku", "Import variables from Heroku", <Wand2 />, (s) => s.kind !== "database", (s) => go(`${svcPath(s, "/variables")}&import=heroku`)),
      ];
      for (const c of cmds) if (c) out.push(c);
    }
    if (proj.canvas) {
      const c = ((): Cmd | null => {
        const ok = (s: Service) => !!s.privateDomain;
        const fn = (s: Service) => { close(); copy(s.privateDomain, "Private URL copied to clipboard"); };
        if (sel) return ok(sel) ? { id: "copy-private", title: "Copy private URL", subtitle: name(sel), group: "Service", icon: <Copy />, run: () => fn(sel) } : null;
        const list = services.filter(ok);
        return list.length ? { id: "copy-private", title: "Copy private URL", group: "Services", icon: <Copy />, children: () => list.map((s) => ({ id: `cp:${s.id}`, title: name(s), icon: svcIcon(s), run: () => fn(s) })) } : null;
      })();
      if (c) out.push(c);
      const edges = readPref("5050.canvas.connections", true);
      out.push({
        id: "canvas", title: "Canvas", group: "Canvas", icon: <LayoutGrid />,
        children: () => [
          { id: "canvas:edges", title: edges ? "Hide connections" : "Show connections", icon: edges ? <EyeOff /> : <Eye />, run: () => { close(); sendCanvas({ kind: "toggle-connections" }); } },
          { id: "canvas:refs", title: "Show variable references", keywords: ["layer", "edges"], icon: <Layers />, run: () => { close(); sendCanvas({ kind: "layer", layer: "references" }); } },
          { id: "canvas:traffic", title: "Show network traffic", keywords: ["layer"], icon: <Network />, run: () => { close(); sendCanvas({ kind: "layer", layer: "traffic" }); } },
          ...(editable ? [
            { id: "canvas:layout", title: "Auto layout", keywords: ["tidy", "arrange"], icon: <Wand2 />, run: () => { close(); sendCanvas({ kind: "auto-layout" }); } },
            { id: "canvas:repair", title: "Repair overlaps", icon: <LayoutGrid />, run: () => { close(); sendCanvas({ kind: "repair-overlaps" }); } },
            { id: "canvas:reset", title: "Reset canvas", icon: <RotateCcw />, run: () => { close(); sendCanvas({ kind: "reset" }); } },
          ] : []),
          { id: "canvas:positions", title: "Show positions", icon: <MapPin />, run: () => { close(); sendCanvas({ kind: "toggle-positions" }); } },
        ],
      });
    }
  }

  // ---- general
  if (editable) out.push({ id: "new:project", title: "New Project", group: "General", icon: <Plus />, accent: true, chips: ["Cmd", "/"], keywords: ["create"], run: x.newProject });
  out.push({ id: "nav:docs", title: "Go to Docs", group: "General", icon: <BookOpen />, external: true, run: () => { close(); window.open(DOCS, "_blank", "noopener"); } });
  out.push({ id: "nav:dashboard", title: "Go to Dashboard", group: "General", icon: <LayoutGrid />, run: () => go("/") });
  out.push({ id: "nav:templates", title: "Go to Templates", group: "General", icon: <Store />, run: () => go("/new/template") });
  out.push({ id: "nav:hosts", title: "Go to Hosts", group: "General", icon: <Server />, run: () => go("/settings/hosts") });
  if (admin) out.push({ id: "nav:people", title: "Go to People", group: "General", icon: <Users />, run: () => go("/settings/members") });

  const projects = x.projects || [];
  out.push({
    id: "search:projects", title: "Search Projects...", group: "General", icon: <Folder />, placeholder: "Search projects...",
    children: () => projects.map((p) => ({
      id: `p:${p.id}`, title: p.name, icon: <Folder />, run: () => go(`/project/${p.id}`),
      star: { on: x.favourites.includes(p.id), toggle: () => x.toggleFavourite(p.id) },
    })),
  });
  out.push({
    id: "install", title: "Install", group: "General", icon: <Library />, placeholder: "Search settings...",
    children: () => INSTALL_SETTINGS.filter((s) => admin || !["general", "backups", "audit-log"].includes(s.id)).map((s) => ({ id: `is:${s.id}`, title: s.label, icon: s.icon, run: () => go(`/settings/${s.id}`) })),
  });
  out.push({
    id: "account", title: "Account", group: "General", icon: <User />,
    children: () => ACCOUNT_PAGES.map((s) => ({ id: `as:${s.id}`, title: s.label, icon: s.icon, run: () => go(`/account/${s.id}`) })),
  });
  out.push({
    id: "community", title: "Community", group: "General", icon: <MessageCircle />,
    children: () => [
      { id: "c:idea", title: "Suggest Feature", icon: <Lightbulb />, external: true, run: () => { close(); window.open(`${REPO}/discussions/new?category=ideas`, "_blank", "noopener"); } },
      { id: "c:ask", title: "Ask a Question", icon: <CircleHelp />, external: true, run: () => { close(); window.open(`${REPO}/discussions/new?category=q-a`, "_blank", "noopener"); } },
      { id: "c:issue", title: "Report an Issue", icon: <TriangleAlert />, external: true, run: () => { close(); window.open(`${REPO}/issues/new`, "_blank", "noopener"); } },
    ],
  });
  const pref = themePref();
  out.push({
    id: "theme", title: "Change theme", group: "General", keywords: ["dark", "light", "appearance"], icon: <Monitor />, placeholder: "Pick a theme...",
    children: () => ([["dark", "Dark"], ["light", "Light"], ["system", "Sync with System"]] as const)
      .filter(([k]) => k !== pref)
      .map(([k, label]) => ({ id: `theme:${k}`, title: label, run: () => { setTheme(k); close(); } })),
  });
  out.push({
    id: "utilities", title: "Utilities", group: "General", icon: <Wand2 />,
    children: () => [
      { id: "u:secret", title: "Generate 32 character Secret", icon: <KeyRound />, run: () => { close(); copy(secret(), "Secret copied to clipboard"); } },
      { id: "u:url", title: "Copy Console URL", icon: <Copy />, run: () => { close(); copy(window.location.origin, "Console URL copied to clipboard"); } },
      ...(proj?.project ? [{ id: "u:project", title: "Copy Project ID", icon: <Copy />, run: () => { close(); copy(proj.project!.id, "Project ID copied to clipboard"); } } as Cmd] : []),
    ],
  });
  return out;
}
