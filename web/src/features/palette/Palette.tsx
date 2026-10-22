// SPDX-License-Identifier: AGPL-3.0-only

// Cmd+K command palette (SPEC 13.4). Mounted by the top bar, so it sees the
// open project when there is one.
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useLocation, useNavigate } from "react-router-dom";
import { ChevronRight, Search } from "lucide-react";
import { api, type SearchResult, type Service } from "../../api/client";
import { useMaybeProject } from "../../app/project";
import { canEdit, useMe } from "../../app/session";
import { Button, ErrorLine, Field, Input, Modal, cx, toast, toastError, toggleTheme } from "../../kit";
import { NEW_MENU, sendCanvas, type NewKind } from "../canvas/menus";
import { isTyping } from "../canvas/Changes";
import { back, missingCommandUrl, push, rank, redeployLabel, type Cmd, type Level } from "./model";

export const PROJECT_SETTINGS = ["general", "usage", "environments", "shared-variables", "webhooks", "members", "tokens", "danger"];
export const INSTALL_SETTINGS = ["general", "hosts", "github", "registry-credentials", "backups", "members", "api-tokens", "upgrades", "audit-log"];
const title = (s: string) => s.split("-").map((w, i) => (i ? w : w[0].toUpperCase() + w.slice(1))).join(" ");

export const PALETTE_EVENT = "5050:palette";
export const openPalette = () => window.dispatchEvent(new Event(PALETTE_EVENT));

export function Palette() {
  const [open, setOpen] = useState(false);
  const nav = useNavigate();
  const me = useMe();
  useEffect(() => {
    const k = (e: KeyboardEvent) => {
      const mod = e.metaKey || e.ctrlKey;
      if (mod && e.key.toLowerCase() === "k") { e.preventDefault(); setOpen((o) => !o); }
      if (mod && e.key === "/" && canEdit(me)) { e.preventDefault(); nav("/new"); }
    };
    const o = () => setOpen(true);
    window.addEventListener("keydown", k);
    window.addEventListener(PALETTE_EVENT, o);
    return () => { window.removeEventListener("keydown", k); window.removeEventListener(PALETTE_EVENT, o); };
  }, [nav, me]);
  useTabShortcuts();
  return open ? <PaletteDialog onClose={() => setOpen(false)} /> : null;
}

/** G D / G V / G M / G S switch the open service's tab (Deployments, Variables, Metrics, Settings). */
function useTabShortcuts() {
  const nav = useNavigate();
  const loc = useLocation();
  useEffect(() => {
    let g = 0;
    const k = (e: KeyboardEvent) => {
      if (isTyping(e.target) || e.metaKey || e.ctrlKey || e.altKey) return;
      const key = e.key.toLowerCase();
      if (key === "g") { g = Date.now(); return; }
      if (Date.now() - g > 1000) return;
      const tab = ({ d: "deployments", v: "variables", m: "metrics", s: "settings" } as Record<string, string>)[key];
      const m = /^(\/project\/[^/]+\/service\/[^/]+)/.exec(loc.pathname);
      if (tab && m) { e.preventDefault(); g = 0; nav(`${m[1]}/${tab}${loc.search}`); }
    };
    window.addEventListener("keydown", k);
    return () => window.removeEventListener("keydown", k);
  }, [nav, loc]);
}

function selectedService(pathname: string) {
  const m = /\/service\/([^/]+)/.exec(pathname);
  return m ? decodeURIComponent(m[1]) : undefined;
}

function PaletteDialog({ onClose }: { onClose: () => void }) {
  const nav = useNavigate();
  const loc = useLocation();
  const me = useMe();
  const editable = canEdit(me);
  const proj = useMaybeProject();
  const [q, setQ] = useState("");
  const [cursor, setCursor] = useState(0);
  const [stack, setStack] = useState<Level[]>([{ items: [] }]);
  const [found, setFound] = useState<SearchResult>();
  const [newEnv, setNewEnv] = useState(false);
  const input = useRef<HTMLInputElement>(null);
  const listRef = useRef<HTMLUListElement>(null);
  const sid = selectedService(loc.pathname);

  const go = useCallback((path: string) => { onClose(); nav(path); }, [nav, onClose]);

  const root = useMemo<Cmd[]>(() => {
    const out: Cmd[] = [];
    const services = proj?.canvas?.services || [];
    const svcPath = (s: Service, tab = "") => proj ? proj.withEnv(`/project/${proj.pid}/service/${encodeURIComponent(s.id)}${tab}`) : "/";
    const sel = services.find((s) => s.id === sid);
    const name = (s: Service) => (s.staged || s.config).name;
    /** A service command: direct for the open service, otherwise a sub-menu of services. */
    const forService = (id: string, label: string, ok: (s: Service) => boolean, fn: (s: Service) => void, labelFor?: (s: Service) => string | null): Cmd | null => {
      if (sel) {
        if (!ok(sel)) return null;
        const l = labelFor ? labelFor(sel) : label;
        return l ? { id, title: l, subtitle: name(sel), group: "Service", run: () => fn(sel) } : null;
      }
      const list = services.filter(ok);
      if (!list.length) return null;
      return { id, title: label, group: "Service", children: () => list.map((s) => ({ id: `${id}:${s.id}`, title: name(s), subtitle: labelFor?.(s) || undefined, run: () => fn(s) })) };
    };
    const act = (s: Service, a: "redeploy" | "restart") => api.deploymentAction(s.activeDeployment!, a).then(() => toast(`${a === "restart" ? "Restarting" : "Redeploying"} ${name(s)}`), toastError);

    // navigation
    out.push({ id: "nav:dashboard", title: "Go to Dashboard", group: "Navigation", run: () => go("/") });
    out.push({ id: "nav:project", title: "Go to project", group: "Navigation", children: async () => (await api.projects("activity")).map((p) => ({ id: `p:${p.id}`, title: p.name, run: () => go(`/project/${p.id}`) })) });
    if (proj?.project) {
      const p = proj.project;
      if (services.length) out.push({ id: "nav:service", title: "Go to service", group: "Navigation", children: () => services.map((s) => ({ id: `s:${s.id}`, title: name(s), run: () => go(svcPath(s)) })) });
      out.push({ id: "nav:env", title: "Switch environment", group: "Navigation", children: () => p.environments.map((e) => ({ id: `e:${e.id}`, title: e.name, subtitle: e.id === proj.eid ? "current" : undefined, run: () => { onClose(); proj.setEnvironment(e.id); } })) });
      out.push({ id: "nav:psettings", title: "Project settings", group: "Navigation", children: () => PROJECT_SETTINGS.map((s) => ({ id: `ps:${s}`, title: title(s), run: () => go(proj.withEnv(`/project/${p.id}/settings/${s}`)) })) });
      out.push({ id: "nav:logs", title: "Open logs", group: "Navigation", run: () => go(proj.withEnv(`/project/${p.id}/logs`)) });
      out.push({ id: "nav:obs", title: "Open observability", group: "Navigation", run: () => go(proj.withEnv(`/project/${p.id}/observability`)) });
    }
    out.push({ id: "nav:settings", title: "Install settings", group: "Navigation", children: () => INSTALL_SETTINGS.map((s) => ({ id: `is:${s}`, title: title(s), run: () => go(`/settings/${s}`) })) });
    out.push({ id: "nav:account", title: "Account settings", group: "Navigation", children: () => ["profile", "security", "notifications"].map((s) => ({ id: `as:${s}`, title: title(s), run: () => go(`/account/${s}`) })) });

    if (editable) {
      out.push({ id: "new:project", title: "New project", shortcut: "Cmd+/", group: "Create", run: () => go("/new") });
      for (const m of NEW_MENU) {
        if (m.disabled) continue;
        out.push({ id: `new:${m.id}`, title: `New ${m.label}`, group: "Create", keywords: ["add", "create"],
          run: () => { onClose(); if (proj?.canvas) sendCanvas({ kind: "new", what: m.id as NewKind }); else nav("/new"); } });
      }
      if (proj?.project) out.push({ id: "new:env", title: "New Environment", group: "Create", run: () => setNewEnv(true) });
    }

    if (proj?.canvas && editable) {
      const cmds = [
        forService("deploy", "Deploy latest commit", (s) => s.config.source.type === "github", (s) => { onClose(); api.deploy(proj.eid, s.id).then(() => toast(`Deploying ${name(s)}`), toastError); }),
        forService("redeploy", "Redeploy", (s) => !!redeployLabel(s), (s) => { onClose(); void act(s, "redeploy"); }, redeployLabel),
        forService("restart", "Restart", (s) => !!s.activeDeployment, (s) => { onClose(); void act(s, "restart"); }),
        forService("rollback", "Rollback", (s) => !!s.activeDeployment, (s) => {
          onClose();
          api.deployments(proj.eid, s.id, 20).then((ds) => {
            const prev = ds.find((d) => !d.active && d.canRollback && d.id !== s.activeDeployment && (d.status === "SUCCESS" || d.status === "REMOVED"));
            if (!prev) { toast("No earlier deployment to roll back to."); return; }
            return api.deploymentAction(prev.id, "rollback").then(() => toast(`Rolling ${name(s)} back`));
          }).catch(toastError);
        }),
        forService("shell", "Open shell", (s) => !!s.activeDeployment, (s) => go(svcPath(s, "/shell"))),
        forService("info", "Update service info", () => true, (s) => { onClose(); sendCanvas({ kind: "update-info", serviceId: s.id }); }),
        forService("heroku", "Import variables from Heroku", (s) => s.kind !== "database", (s) => go(`${svcPath(s, "/variables")}&import=heroku`)),
      ];
      for (const c of cmds) if (c) out.push(c);
      out.push({ id: "canvas:edges", title: "Hide connections", keywords: ["show connections", "edges"], group: "Canvas", run: () => { onClose(); sendCanvas({ kind: "toggle-connections" }); } });
      out.push({ id: "canvas:tidy", title: "Tidy canvas", group: "Canvas", run: () => { onClose(); sendCanvas({ kind: "tidy" }); } });
    }
    if (proj?.canvas) {
      const c = forService("copy-private", "Copy private URL", (s) => !!s.privateDomain, (s) => { onClose(); void navigator.clipboard?.writeText(s.privateDomain).then(() => toast(`Copied ${s.privateDomain}`)); });
      if (c) out.push(c);
    }
    out.push({ id: "theme", title: "Toggle theme", keywords: ["dark", "light"], group: "Preferences", run: () => { toggleTheme(); onClose(); } });
    return out;
  }, [proj, sid, editable, go, nav, onClose]);

  useEffect(() => { setStack([{ items: root }]); }, [root]);
  const level = stack[stack.length - 1];
  const atRoot = stack.length === 1;

  // server search (projects, services, environments, settings pages, deployments by commit message)
  useEffect(() => {
    setFound(undefined);
    if (!atRoot || q.trim().length < 2) return;
    const ctl = new AbortController();
    const t = setTimeout(() => api.search(q.trim(), ctl.signal).then(setFound, () => {}), 150);
    return () => { clearTimeout(t); ctl.abort(); };
  }, [q, atRoot]);

  const items = useMemo(() => {
    const local = rank(level.items, q);
    if (!found) return local;
    const seen = new Set(local.map((c) => c.title + (c.subtitle || "")));
    const groups: [keyof SearchResult, string][] = [["projects", "Projects"], ["services", "Services"], ["environments", "Environments"], ["settings", "Settings"], ["deployments", "Deployments"]];
    const remote: Cmd[] = groups.flatMap(([k, label]) => (found[k] || []).map((it) => ({ id: `r:${k}:${it.id}`, title: it.title, subtitle: it.subtitle, group: label, run: () => go(it.path) })));
    return [...local, ...remote.filter((c) => !seen.has(c.title + (c.subtitle || "")))];
  }, [level, q, found, go]);

  useEffect(() => { setCursor(0); }, [q, stack]);
  useEffect(() => { listRef.current?.querySelector<HTMLElement>(`[data-i="${cursor}"]`)?.scrollIntoView({ block: "nearest" }); }, [cursor]);

  const enter = async (c: Cmd) => {
    if (c.children) {
      const kids = await c.children();
      setStack((s) => push(s, c, kids)); setQ(""); input.current?.focus();
      return;
    }
    c.run?.();
  };

  const onKey = (e: React.KeyboardEvent) => {
    const cur = items[cursor];
    if (e.key === "ArrowDown") { e.preventDefault(); setCursor((i) => Math.min(items.length - 1, i + 1)); }
    else if (e.key === "ArrowUp") { e.preventDefault(); setCursor((i) => Math.max(0, i - 1)); }
    else if (e.key === "ArrowRight" && cur?.children && (e.currentTarget as HTMLInputElement).selectionStart === q.length) { e.preventDefault(); void enter(cur).catch(toastError); }
    else if (e.key === "Backspace" && !q && !atRoot) { e.preventDefault(); setStack(back); }
    else if (e.key === "Enter") {
      e.preventDefault();
      if (!items.length && e.shiftKey && q.trim()) { window.open(missingCommandUrl(q.trim()), "_blank", "noopener"); onClose(); return; }
      if (cur) void enter(cur).catch(toastError);
    } else if (e.key === "Escape") { e.preventDefault(); if (!atRoot) setStack(back); else onClose(); }
  };

  if (newEnv && proj) return <NewEnvironment onClose={onClose} />;
  let lastGroup: string | undefined;
  return (
    <div className="fixed inset-0 z-[150] flex items-start justify-center bg-black/50 px-4 pt-[12vh]" onMouseDown={(e) => { if (e.target === e.currentTarget) onClose(); }}>
      <div role="dialog" aria-modal="true" aria-label="Command palette" className="w-full max-w-[600px] overflow-hidden rounded-xl border border-gray-200 bg-secondaryBg shadow-[0_20px_50px_rgba(0,0,0,.5)]">
        <div className="flex items-center gap-2 border-b border-gray-200 px-4">
          <Search size={16} className="text-gray-500" />
          {!atRoot && <span className="shrink-0 rounded bg-gray-100 px-2 text-xs text-gray-600">{level.title}</span>}
          <input ref={input} autoFocus value={q} onChange={(e) => setQ(e.target.value)} onKeyDown={onKey} aria-label="Search commands"
            role="combobox" aria-expanded="true" aria-controls="palette-list" aria-activedescendant={items[cursor] ? `pal-${cursor}` : undefined}
            placeholder={atRoot ? "Type a command or search" : "Filter"} className="h-12 flex-1 bg-transparent text-sm outline-none placeholder:text-gray-400" />
        </div>
        <ul id="palette-list" ref={listRef} role="listbox" className="max-h-[50vh] overflow-y-auto py-1 scroll-thin">
          {items.map((c, i) => {
            const head = c.group && c.group !== lastGroup ? c.group : undefined;
            lastGroup = c.group;
            return (
              <li key={c.id} role="presentation">
                {head && <div className="px-4 pb-1 pt-2 text-[11px] font-medium uppercase tracking-wider text-gray-500">{head}</div>}
                <div id={`pal-${i}`} data-i={i} role="option" aria-selected={i === cursor} onMouseMove={() => setCursor(i)} onClick={() => void enter(c).catch(toastError)}
                  className={cx("mx-1 flex cursor-pointer items-center gap-3 rounded-md px-3 py-2 text-sm", i === cursor ? "bg-gray-100 text-foreground" : "text-gray-700")}>
                  <span className="flex-1 truncate">{c.title}{c.subtitle && <span className="ml-2 text-xs text-gray-500">{c.subtitle}</span>}</span>
                  {c.shortcut && <kbd className="text-xs text-gray-500">{c.shortcut}</kbd>}
                  {c.children && <ChevronRight size={14} className="text-gray-400" />}
                </div>
              </li>
            );
          })}
          {items.length === 0 && (
            <li className="px-4 py-6 text-center text-sm text-gray-500">No results. <span className="text-gray-400">Shift+Enter: Report a missing command</span></li>
          )}
        </ul>
      </div>
    </div>
  );
}

/** New Environment: empty, or a duplicate of the current one with its services staged. */
export function NewEnvironment({ onClose }: { onClose: () => void }) {
  const proj = useMaybeProject()!;
  const [name, setName] = useState("");
  const [mode, setMode] = useState<"duplicate" | "empty">("duplicate");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>();
  const create = async () => {
    setBusy(true); setError(undefined);
    try {
      const e = await api.createEnvironment(proj.pid, { name: name.trim(), mode, from: mode === "duplicate" ? proj.eid : undefined });
      await proj.reloadProject();
      proj.setEnvironment(e.id);
      onClose();
    } catch (e) { setError(e); setBusy(false); }
  };
  return (
    <Modal title="New Environment" onClose={onClose} width={440}>
      <form className="space-y-4 pb-2" onSubmit={(e) => { e.preventDefault(); void create(); }}>
        <Field label="Name"><Input autoFocus required value={name} onChange={(e) => setName(e.target.value)} placeholder="staging" /></Field>
        <fieldset className="space-y-1 text-sm">
          <label className="flex items-center gap-2"><input type="radio" checked={mode === "duplicate"} onChange={() => setMode("duplicate")} />Duplicate Environment<span className="text-gray-500">({proj.env?.name}; services arrive staged)</span></label>
          <label className="flex items-center gap-2"><input type="radio" checked={mode === "empty"} onChange={() => setMode("empty")} />Empty Environment</label>
        </fieldset>
        <ErrorLine error={error} />
        <div className="flex justify-end gap-2 py-2"><Button onClick={onClose}>Cancel</Button><Button type="submit" variant="primary" busy={busy} disabled={!name.trim()}>Create</Button></div>
      </form>
    </Modal>
  );
}
