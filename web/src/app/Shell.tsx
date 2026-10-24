// SPDX-License-Identifier: AGPL-3.0-only

// The signed-in shell for install-level pages: a 220px sidebar (workspace switcher, grouped
// navigation, Settings accordion, external links, user row) that collapses to a 56px rail and
// becomes a bottom dock on phones; a 56px header (bell, install status pill); and the bordered,
// independently scrolling main panel. Pages render into <Outlet/>.
import { createContext, Suspense, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { Link, NavLink, Outlet, useLocation, useNavigate } from "react-router-dom";
import {
  ArrowUpRight, BookOpen, ChevronDown, CircleArrowUp, LayoutGrid, LayoutTemplate, LogOut, MessageSquare, MessagesSquare,
  Moon, MoreHorizontal, MoreVertical, Plus, Server, Settings, Sun, UserRound, Users,
} from "lucide-react";
import { api } from "../api/client";
import {
  Avatar, Badge, Button, Logo, MenuItem, MenuSeparator, Popover, Skeleton, Spinner, Tooltip, VDivider, cx, readPref, resolvedTheme,
  toastError, toggleTheme, writePref,
} from "../kit";
import { Bell } from "../features/activity/Bell";
import { Palette } from "../features/palette/Palette";
import { isAdmin, useMe, useSession } from "./session";

import { LINKS, REPO_URL } from "./links";

export { LINKS, REPO_URL };

/** Install settings sections, in sidebar order (admin = owners and admins only). */
export const SETTINGS_NAV: { id: string; label: string; admin?: boolean }[] = [
  { id: "general", label: "General", admin: true },
  { id: "github", label: "GitHub" },
  { id: "registry-credentials", label: "Registry credentials" },
  { id: "backups", label: "Backups", admin: true },
  { id: "api-tokens", label: "API tokens" },
  { id: "upgrades", label: "Upgrades" },
  { id: "usage", label: "Usage" },
  { id: "audit-log", label: "Audit log", admin: true },
];

/* ------------------------------------------------------------------ viewport */

/** The viewport width, updated on resize. */
export function useViewportWidth() {
  const [w, setW] = useState(() => (typeof window === "undefined" ? 1512 : window.innerWidth));
  useEffect(() => {
    const f = () => setW(window.innerWidth);
    window.addEventListener("resize", f);
    return () => window.removeEventListener("resize", f);
  }, []);
  return w;
}

/** Breakpoints: full sidebar at 1200 and up, 56px rail from 600, bottom dock below 600. */
export const RAIL_BELOW = 1200;
export const DOCK_BELOW = 600;

/* ------------------------------------------------------------------ shell context */

interface ShellSlots {
  left: HTMLElement | null;
  right: HTMLElement | null;
  banner: HTMLElement | null;
}
interface ShellOptions { bleed?: boolean; width?: "content" | "narrow" | "wide" }
interface ShellCtx { slots: ShellSlots; setOptions: (o: ShellOptions) => void }
const Ctx = createContext<ShellCtx | null>(null);

/** True inside AppShell or the project shell. */
export const useInShell = () => useContext(Ctx) !== null;

/**
 * Page layout options inside AppShell: `bleed` drops the main panel padding and width cap (full
 * canvases, split views); `width` picks the content column: content 1120 (default), narrow 872, wide 1400.
 */
export function useShellOptions(o: ShellOptions) {
  const c = useContext(Ctx);
  const { bleed, width } = o;
  useEffect(() => {
    c?.setOptions({ bleed, width });
    return () => c?.setOptions({});
  }, [c, bleed, width]);
}

/** Renders children into the full-width banner row above the shell (updates, restores, outages). */
export function ShellBanner({ children }: { children: ReactNode }) {
  const c = useContext(Ctx);
  return c?.slots.banner ? createPortal(children, c.slots.banner) : null;
}

/**
 * Header content for the current page. Inside a shell it renders into the header (left: breadcrumb
 * or title; right: extra buttons). Kept for older pages; new pages should not need it.
 */
export function TopBar({ left, right }: { left?: ReactNode; right?: ReactNode }) {
  const c = useContext(Ctx);
  if (c) {
    return (
      <>
        {left && c.slots.left && createPortal(<div className="flex min-w-0 items-center gap-2 text-sm text-fg-2">{left}</div>, c.slots.left)}
        {right && c.slots.right && createPortal(right, c.slots.right)}
      </>
    );
  }
  return (
    <header className="flex h-14 shrink-0 items-center gap-3 border-b border-line bg-background px-4">
      <Link to="/" aria-label="Dashboard" className="flex shrink-0 items-center text-foreground"><Logo size={24} /></Link>
      <div className="flex min-w-0 flex-1 items-center gap-2 text-sm">{left}</div>
      <div className="flex shrink-0 items-center gap-1">{right}<Bell /><UserMenu variant="avatar" /></div>
      <Palette />
    </header>
  );
}

/* ------------------------------------------------------------------ shared header pieces */

/** The install status pill in the header: version, or "Update available" in the accent. */
export function InstallStatus({ compact }: { compact?: boolean }) {
  const me = useMe();
  const nav = useNavigate();
  const { version, latestVersion } = me.install;
  const update = !!latestVersion && latestVersion !== version;
  return (
    <Popover align="right" width={320} role="dialog" trigger={(open) => (
      <button type="button" aria-label={update ? `Update available: ${latestVersion}` : `Version ${version}`} aria-expanded={open}
        className={cx("inline-flex h-[30px] items-center gap-1.5 rounded-md border px-2 text-sm transition-colors duration-150",
          update ? "border-pink-200 bg-pink-50 text-pink-700 hover:bg-pink-100" : "border-line text-fg-2 hover:bg-surface-hover hover:text-foreground")}>
        {update ? <CircleArrowUp size={16} /> : <Server size={16} />}
        {!compact && <span className="whitespace-nowrap">{update ? "Update available" : `v${version.replace(/^v/, "")}`}</span>}
      </button>
    )}>
      {(close) => (
        <div className="p-4">
          <div className={cx("flex h-9 items-center justify-between rounded-md border px-3 text-caption font-medium",
            update ? "border-pink-200 bg-pink-50 text-pink-700" : "border-line bg-surface text-fg-2")}>
            <span>Installed version</span><span className="font-mono">{version}</span>
          </div>
          <div className="mt-4 text-base font-semibold text-foreground">{update ? `Version ${latestVersion} is ready` : "You are up to date"}</div>
          <p className="mt-1 text-sm text-fg-2">
            {update ? "Upgrades take a backup first and roll back on failure. Running services keep serving traffic." : `${me.install.name} runs the latest release.`}
          </p>
          <Button variant="primary" block className="mt-4" onClick={() => { close(); nav("/settings/upgrades"); }}>{update ? "Review upgrade" : "Upgrade settings"}</Button>
          <p className="mt-3 text-caption text-fg-3">
            Release notes on <a href={`${REPO_URL}/releases`} target="_blank" rel="noreferrer" className="underline underline-offset-2 hover:text-accent-text">GitHub</a>.
          </p>
        </div>
      )}
    </Popover>
  );
}

/** The user menu: profile card, account links, install row, docs, theme toggle and log out. */
export function UserMenu({ variant = "row", collapsed, placement }: { variant?: "row" | "avatar"; collapsed?: boolean; placement?: "top" | "bottom" | "right" }) {
  const me = useMe();
  const { setMe } = useSession();
  const nav = useNavigate();
  const [theme, setThemeState] = useState(resolvedTheme());
  const go = (to: string, close: () => void) => { close(); nav(to); };
  const roleLabel = { owner: "Owner", admin: "Admin", member: "Member", viewer: "Viewer" }[me.user.role as string] || me.user.role;
  return (
    <Popover width={280} align={variant === "avatar" ? "right" : "left"} placement={placement ?? (variant === "row" ? "top" : "bottom")}
      wrapperClassName={variant === "row" && !collapsed ? "w-full" : undefined}
      trigger={(open) => variant === "row" && !collapsed ? (
        <button type="button" aria-label="Account menu" aria-expanded={open}
          className={cx("flex h-10 w-full items-center gap-2.5 rounded-lg px-2 text-left transition-colors duration-150 hover:bg-tint-active", open && "bg-tint-active")}>
          <Avatar name={me.user.name || me.user.email} size={24} />
          <span className="min-w-0 flex-1 truncate text-sm font-medium text-foreground">{me.user.name || me.user.email}</span>
          <MoreVertical size={16} className="shrink-0 text-fg-2" />
        </button>
      ) : (
        <button type="button" aria-label="Account menu" aria-expanded={open}
          className="flex h-10 w-10 items-center justify-center rounded-lg transition-colors duration-150 hover:bg-tint-active">
          <Avatar name={me.user.name || me.user.email} size={24} />
        </button>
      )}>
      {(close) => (
        <>
          <div className="relative flex flex-col items-center rounded-md bg-surface-hover px-4 pb-5 pt-7 text-center">
            <Badge upper tone="ok" className="absolute left-2 top-2 border-transparent bg-surface">{roleLabel}</Badge>
            <Avatar name={me.user.name || me.user.email} size={48} />
            <div className="mt-3 max-w-full truncate text-sm font-medium text-foreground">{me.user.name || me.user.email}</div>
            <div className="max-w-full truncate text-caption text-fg-3">{me.user.email}</div>
          </div>
          <div className="mt-1">
            <MenuItem icon={<UserRound />} onClick={() => go("/account/profile", close)}>Account settings</MenuItem>
            <MenuItem icon={<Settings />} onClick={() => go("/settings/general", close)}>Install settings</MenuItem>
            <MenuItem icon={<Server />} onClick={() => go("/settings/hosts", close)}>Hosts</MenuItem>
          </div>
          <MenuSeparator />
          <MenuItem icon={<Avatar name={me.install.name} size={20} />} active onClick={() => go("/", close)}>{me.install.name}</MenuItem>
          <MenuSeparator />
          <MenuItem icon={<BookOpen />} hint={<ArrowUpRight size={12} />} onClick={() => { close(); window.open(LINKS.docs, "_blank", "noopener"); }}>Documentation</MenuItem>
          <MenuItem icon={<MessageSquare />} hint={<ArrowUpRight size={12} />} onClick={() => { close(); window.open(LINKS.support, "_blank", "noopener"); }}>Support</MenuItem>
          <MenuSeparator />
          <div className="flex items-center justify-between gap-2 px-1 py-1">
            <button type="button" onClick={() => { toggleTheme(); setThemeState(resolvedTheme()); }}
              className="flex h-[34px] items-center gap-2 rounded-md px-2 text-sm font-medium text-fg-2 transition-colors hover:bg-surface-hover hover:text-foreground">
              {theme === "dark" ? <Sun size={16} /> : <Moon size={16} />}{theme === "dark" ? "Light theme" : "Dark theme"}
            </button>
            <button type="button" onClick={() => { close(); api.logout().then(() => { setMe(null); nav("/login"); }, toastError); }}
              className="flex h-[34px] items-center gap-2 rounded-md border border-line px-3 text-sm font-medium text-danger-soft transition-colors hover:bg-danger-bg">
              <LogOut size={16} className="text-danger" />Log out
            </button>
          </div>
        </>
      )}
    </Popover>
  );
}

/* ------------------------------------------------------------------ sidebar */

interface NavDef { to: string; label: string; icon: typeof LayoutGrid; match: (path: string) => boolean; admin?: boolean }

const NAV_MAIN: NavDef[] = [
  { to: "/", label: "Projects", icon: LayoutGrid, match: (p) => p === "/" },
  { to: "/new/template", label: "Templates", icon: LayoutTemplate, match: (p) => p === "/new/template" || p.startsWith("/new/template/") },
];
const NAV_INSTALL: NavDef[] = [
  { to: "/settings/hosts", label: "Hosts", icon: Server, match: (p) => p.startsWith("/settings/hosts") },
  { to: "/settings/members", label: "Members", icon: Users, match: (p) => p.startsWith("/settings/members"), admin: true },
];
const NAV_EXTERNAL = [
  { href: LINKS.docs, label: "Docs", icon: BookOpen },
  { href: LINKS.community, label: "Community", icon: MessagesSquare },
  { href: LINKS.support, label: "Report an issue", icon: MessageSquare },
];

const NAV_ITEM = "group relative flex h-9 items-center rounded-lg text-sm outline-none transition-colors duration-150";

function SideLink({ def, collapsed }: { def: NavDef; collapsed: boolean }) {
  const { pathname } = useLocation();
  const on = def.match(pathname);
  const link = (
    <Link to={def.to} aria-label={collapsed ? def.label : undefined} aria-current={on ? "page" : undefined}
      className={cx(NAV_ITEM, collapsed ? "w-10 justify-center" : "gap-3 px-3",
        on ? "bg-tint-active font-medium text-foreground" : "text-fg-2 hover:bg-tint-hover hover:text-foreground")}>
      <def.icon size={16} className={cx("shrink-0", on && "text-accent-text")} />
      {!collapsed && <span className="truncate">{def.label}</span>}
    </Link>
  );
  return collapsed ? <Tooltip content={def.label} side="right">{link}</Tooltip> : link;
}

function SettingsAccordion({ collapsed }: { collapsed: boolean }) {
  const me = useMe();
  const { pathname } = useLocation();
  const inSettings = pathname.startsWith("/settings") && !NAV_INSTALL.some((n) => n.match(pathname));
  const [open, setOpen] = useState(inSettings);
  useEffect(() => { if (inSettings) setOpen(true); }, [inSettings]);
  const items = SETTINGS_NAV.filter((s) => !s.admin || isAdmin(me));
  if (collapsed) {
    return (
      <Tooltip content="Settings" side="right">
        <Link to="/settings/general" aria-label="Settings"
          className={cx(NAV_ITEM, "w-10 justify-center", inSettings ? "bg-tint-active text-foreground" : "text-fg-2 hover:bg-tint-hover hover:text-foreground")}>
          <Settings size={16} className={cx(inSettings && "text-accent-text")} />
        </Link>
      </Tooltip>
    );
  }
  return (
    <div>
      <button type="button" aria-expanded={open} onClick={() => setOpen((o) => !o)}
        className={cx(NAV_ITEM, "w-full gap-3 px-3", open || inSettings ? "bg-tint-active text-foreground" : "text-fg-2 hover:bg-tint-hover hover:text-foreground", inSettings && "font-medium")}>
        <Settings size={16} className={cx("shrink-0", inSettings && "text-accent-text")} />
        <span>Settings</span>
        <ChevronDown size={16} className={cx("shrink-0 transition-transform duration-200", open && "rotate-180")} />
      </button>
      <div className={cx("grid transition-[grid-template-rows,opacity] duration-200", open ? "grid-rows-[1fr] opacity-100" : "grid-rows-[0fr] opacity-0")}>
        <div className="overflow-hidden">
          <div className="ml-5 mt-1 flex flex-col gap-1 border-l border-line pl-2">
            {items.map((s) => (
              <NavLink key={s.id} to={`/settings/${s.id}`} tabIndex={open ? undefined : -1}
                className={({ isActive }) => cx("flex h-9 items-center rounded-lg px-3 text-sm transition-colors duration-150",
                  isActive ? "bg-tint-active font-medium text-foreground" : "text-fg-2 hover:bg-tint-hover hover:text-foreground")}>
                {s.label}
              </NavLink>
            ))}
          </div>
        </div>
      </div>
    </div>
  );
}

function ExternalLinks({ collapsed }: { collapsed: boolean }) {
  return (
    <>
      {NAV_EXTERNAL.map((l) => {
        const a = (
          <a key={l.label} href={l.href} target="_blank" rel="noreferrer" aria-label={collapsed ? `${l.label} (opens in a new tab)` : undefined}
            className={cx(NAV_ITEM, "text-fg-4 hover:bg-tint-hover hover:text-fg-2", collapsed ? "w-10 justify-center" : "gap-3 px-3")}>
            <l.icon size={16} className="shrink-0" />
            {collapsed ? <ArrowUpRight size={9} className="absolute right-1.5 top-1.5" /> : <span className="truncate">{l.label}</span>}
            {!collapsed && <ArrowUpRight size={12} className="-ml-2 shrink-0" />}
          </a>
        );
        return collapsed ? <Tooltip key={l.label} content={l.label} side="right">{a}</Tooltip> : a;
      })}
    </>
  );
}

/** The install (workspace) switcher: avatar, bold name, role label, chevron. */
function WorkspaceSwitcher({ collapsed }: { collapsed: boolean }) {
  const me = useMe();
  const nav = useNavigate();
  const role = String(me.user.role);
  return (
    <Popover width={220} trigger={(open) => collapsed ? (
      <Tooltip content={me.install.name} side="right">
        <button type="button" aria-label={`Install: ${me.install.name}`} aria-expanded={open}
          className="relative flex h-10 w-10 items-center justify-center rounded-lg transition-colors hover:bg-tint-hover">
          <Avatar name={me.install.name} size={24} />
          <span className="absolute bottom-1 right-1 flex h-3 w-3 items-center justify-center rounded-full bg-background ring-1 ring-line"><ChevronDown size={8} className="text-fg-3" /></span>
        </button>
      </Tooltip>
    ) : (
      <button type="button" aria-label="Switch install" aria-expanded={open}
        className={cx("flex h-[42px] w-full items-center gap-2 rounded-md px-2 text-left transition-colors duration-150 hover:bg-tint-hover", open && "bg-tint-hover")}>
        <Avatar name={me.install.name} size={24} />
        <span className="min-w-0 flex-1">
          <span className="block truncate text-sm font-semibold leading-5 text-foreground">{me.install.name}</span>
          <span className="block text-micro font-medium uppercase tracking-[.08em] text-ok">{role}</span>
        </span>
        <ChevronDown size={12} className="shrink-0 text-fg-3" />
      </button>
    )}>
      {(close) => (
        <>
          <MenuItem icon={<Avatar name={me.install.name} size={24} />} active onClick={() => { close(); nav("/"); }}>{me.install.name}</MenuItem>
          <MenuSeparator />
          <MenuItem icon={<Plus />} accentIcon onClick={() => { close(); nav("/new"); }}>New project</MenuItem>
          <MenuItem icon={<Settings />} onClick={() => { close(); nav("/settings/general"); }}>Install settings</MenuItem>
        </>
      )}
    </Popover>
  );
}

function SidebarHr() {
  return <div className="-mx-2 my-2 h-px self-stretch bg-line" />;
}

function Sidebar({ mode, onToggle }: { mode: "expanded" | "rail"; onToggle: () => void }) {
  const me = useMe();
  const collapsed = mode === "rail";
  const install = NAV_INSTALL.filter((n) => !n.admin || isAdmin(me));
  return (
    <aside aria-label="Sidebar"
      className={cx("relative flex h-full shrink-0 flex-col bg-background transition-[width,min-width] duration-100 ease-in-out", collapsed ? "w-rail min-w-[56px]" : "w-sidebar min-w-[220px]")}>
      <div className={cx("flex h-14 shrink-0 items-center", collapsed ? "justify-center" : "px-4")}>
        <Link to="/" aria-label="Projects" className="flex items-center rounded-md text-foreground"><Logo size={24} /></Link>
      </div>
      <div className={cx("flex min-h-0 flex-1 flex-col overflow-y-auto overflow-x-hidden scroll-thin pb-3 pt-2", collapsed ? "items-center px-2" : "px-2")}>
        <WorkspaceSwitcher collapsed={collapsed} />
        <SidebarHr />
        <nav aria-label="Main" className={cx("flex flex-col gap-1", collapsed && "items-center")}>
          {NAV_MAIN.map((d) => <SideLink key={d.to} def={d} collapsed={collapsed} />)}
        </nav>
        <SidebarHr />
        <nav aria-label="Install" className={cx("flex flex-col gap-1", collapsed && "items-center")}>
          {install.map((d) => <SideLink key={d.to} def={d} collapsed={collapsed} />)}
          <SettingsAccordion collapsed={collapsed} />
        </nav>
        <SidebarHr />
        <nav aria-label="Resources" className={cx("flex flex-col gap-1", collapsed && "items-center")}>
          <ExternalLinks collapsed={collapsed} />
        </nav>
      </div>
      <div className={cx("shrink-0 border-t border-line p-2", collapsed && "flex justify-center")}>
        <UserMenu variant="row" collapsed={collapsed} placement={collapsed ? "right" : "top"} />
      </div>
      <button type="button" aria-label={collapsed ? "Expand sidebar" : "Collapse sidebar"} onClick={onToggle}
        className="group absolute -right-[3px] top-0 z-10 flex h-full w-1.5 cursor-ew-resize items-center justify-center outline-none">
        <span className="h-8 w-0.5 rounded-full bg-tint-pill opacity-0 transition-opacity duration-150 group-hover:opacity-100 group-focus-visible:opacity-100" />
      </button>
    </aside>
  );
}

/* ------------------------------------------------------------------ mobile dock */

function MobileDock() {
  const me = useMe();
  const { pathname } = useLocation();
  const nav = useNavigate();
  const slots = [...NAV_MAIN, ...NAV_INSTALL.filter((n) => !n.admin || isAdmin(me))];
  return (
    <nav aria-label="Main" className="fixed bottom-4 left-1/2 z-[80] flex h-14 w-[min(356px,calc(100vw-32px))] -translate-x-1/2 items-center gap-1 rounded-2xl border border-line-panel bg-surface-raised px-2 shadow-modal">
      {slots.map((d) => {
        const on = d.match(pathname);
        return (
          <Link key={d.to} to={d.to} aria-current={on ? "page" : undefined}
            className={cx("flex h-11 min-w-0 flex-1 flex-col items-center justify-center gap-0.5 rounded-xl text-micro transition-colors", on ? "text-foreground" : "text-fg-2 hover:text-foreground")}>
            <d.icon size={16} className={cx(on && "text-accent-text")} />
            <span className="truncate">{d.label}</span>
          </Link>
        );
      })}
      <span aria-hidden className="mx-1 h-6 w-px bg-line" />
      <Popover align="right" placement="top" width={240} trigger={(open) => (
        <button type="button" aria-label="More" aria-expanded={open} className="flex h-11 w-11 items-center justify-center rounded-xl text-fg-2 hover:text-foreground"><MoreHorizontal size={18} /></button>
      )}>
        {(close) => (
          <>
            <MenuItem icon={<Settings />} onClick={() => { close(); nav("/settings/general"); }}>Settings</MenuItem>
            <MenuSeparator />
            {NAV_EXTERNAL.map((l) => (
              <MenuItem key={l.label} icon={<l.icon />} hint={<ArrowUpRight size={12} />} onClick={() => { close(); window.open(l.href, "_blank", "noopener"); }}>{l.label}</MenuItem>
            ))}
          </>
        )}
      </Popover>
    </nav>
  );
}

/* ------------------------------------------------------------------ app shell */

const SIDEBAR_KEY = "5050.sidebar";

/** The layout route for install-level pages: sidebar, header and the main panel around <Outlet/>. */
export function AppShell({ children }: { children?: ReactNode }) {
  const me = useMe();
  const vw = useViewportWidth();
  const [pref, setPref] = useState<"expanded" | "rail">(() => readPref(SIDEBAR_KEY, "expanded"));
  const [peek, setPeek] = useState(false);   // expanded by hand below the rail breakpoint
  const [left, setLeft] = useState<HTMLElement | null>(null);
  const [right, setRight] = useState<HTMLElement | null>(null);
  const [banner, setBanner] = useState<HTMLElement | null>(null);
  const [options, setOptions] = useState<ShellOptions>({});
  const docked = vw < DOCK_BELOW;
  const mode: "expanded" | "rail" = vw < RAIL_BELOW ? (peek ? "expanded" : "rail") : pref;
  useEffect(() => { if (vw >= RAIL_BELOW) setPeek(false); }, [vw]);
  const toggle = useCallback(() => {
    if (vw < RAIL_BELOW) { setPeek((p) => !p); return; }
    const next = pref === "expanded" ? "rail" : "expanded";
    setPref(next);
    writePref(SIDEBAR_KEY, next);
  }, [vw, pref]);
  const ctx = useMemo<ShellCtx>(() => ({ slots: { left, right, banner }, setOptions }), [left, right, banner]);
  const page = <Suspense fallback={<div className="flex justify-center py-16"><Spinner /></div>}>{children ?? <Outlet />}</Suspense>;
  const width = { content: "max-w-content", narrow: "max-w-narrow", wide: "max-w-[1400px]" }[options.width || "content"];
  return (
    <Ctx.Provider value={ctx}>
      <div className="flex h-full flex-col bg-background text-foreground">
        <div ref={setBanner} className="shrink-0 empty:hidden" />
        <div className="flex min-h-0 flex-1">
          {!docked && <Sidebar mode={mode} onToggle={toggle} />}
          <div className="flex min-w-0 flex-1 flex-col">
            <header className="flex h-14 shrink-0 items-center gap-3 px-2 sm:pl-4">
              {docked && (
                <div className="flex min-w-0 items-center gap-2">
                  <Link to="/" aria-label="Projects" className="flex shrink-0 items-center text-foreground"><Logo size={24} /></Link>
                  <span className="text-fg-5">/</span>
                  <Avatar name={me.install.name} size={20} />
                  <span className="max-w-[90px] truncate text-sm font-medium">{me.install.name}</span>
                </div>
              )}
              <div ref={setLeft} className={cx("flex min-w-0 flex-1 items-center", docked && "hidden")} />
              {docked && <div className="flex-1" />}
              <div ref={setRight} className="flex shrink-0 items-center gap-2 empty:hidden" />
              <div className="flex shrink-0 items-center gap-3">
                <Bell />
                <VDivider />
                <InstallStatus compact={docked} />
                {docked && <UserMenu variant="avatar" />}
              </div>
            </header>
            <main id="main" className={cx("relative min-h-0 flex-1 overflow-auto scroll-thin rounded-lg border border-line-panel bg-background",
              docked ? "mx-2 mb-2" : "mb-2 mr-2")}>
              {options.bleed ? page : (
                <div className={cx(docked ? "px-5 pb-28 pt-5" : "px-16 pb-24 pt-12")}>
                  <div className={cx("mx-auto w-full", width)}>
                    {page}
                  </div>
                </div>
              )}
            </main>
          </div>
        </div>
        {docked && <MobileDock />}
        <Palette />
      </div>
    </Ctx.Provider>
  );
}

/** Provides the shell context to the project layout, which draws its own header and rail. */
export function ShellSlotsProvider({ slots, children }: { slots: ShellSlots; children: ReactNode }) {
  const ctx = useMemo<ShellCtx>(() => ({ slots, setOptions: () => {} }), [slots]);
  return <Ctx.Provider value={ctx}>{children}</Ctx.Provider>;
}

/* ------------------------------------------------------------------ loading */

/** The shell-shaped loading state: same frame, pulsing blocks, no layout shift when data arrives. */
export function ShellSkeleton() {
  const vw = useViewportWidth();
  const rail = vw < RAIL_BELOW;
  const docked = vw < DOCK_BELOW;
  return (
    <div className="flex h-full bg-background" aria-busy="true" aria-label="Loading">
      {!docked && (
        <aside className={cx("flex h-full shrink-0 flex-col", rail ? "w-rail items-center" : "w-sidebar")}>
          <div className={cx("flex h-14 items-center", rail ? "justify-center" : "px-4")}><Skeleton circle w={24} h={24} /></div>
          <div className={cx("flex items-center gap-2 py-2", rail ? "justify-center" : "px-4")}>
            <Skeleton circle w={24} h={24} />
            {!rail && <div className="space-y-1"><Skeleton w={112} h={16} /><Skeleton w={32} h={10} /></div>}
          </div>
          <div className="my-2 h-px bg-line" />
          {[72, 84, 0, 56, 64, 70].map((w, i) => w === 0 ? <div key={i} className="my-2 h-px bg-line" /> : (
            <div key={i} className={cx("flex h-9 items-center gap-3", rail ? "justify-center" : "px-5")}>
              <Skeleton w={16} h={16} />{!rail && <Skeleton w={w} h={14} />}
            </div>
          ))}
          <div className="flex-1" />
          <div className={cx("flex items-center gap-2.5 border-t border-line p-4", rail && "justify-center px-0")}>
            <Skeleton circle w={24} h={24} />{!rail && <Skeleton w={80} h={16} />}
          </div>
        </aside>
      )}
      <div className="flex min-w-0 flex-1 flex-col">
        <div className="flex h-14 items-center justify-end gap-3 px-4"><Skeleton circle w={24} h={24} /><Skeleton w={96} h={24} /></div>
        <div className={cx("flex-1 rounded-lg border border-line-panel", docked ? "mx-2 mb-2" : "mb-2 mr-2")} />
      </div>
    </div>
  );
}
