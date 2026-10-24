// SPDX-License-Identifier: AGPL-3.0-only

// Cmd+K command palette: a 500px card over an 80% dim, a flat command tree with sub-menus
// (tag chips above the card, 16px slide), whole-tree search under breadcrumb headers, keycap
// chips on rows, and a "report a missing command" fallback row in place of an empty state.
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useLocation, useNavigate } from "react-router-dom";
import { ArrowUpRight, ChevronRight, Folder, MessageSquarePlus, Star } from "lucide-react";
import { api, type Project, type SearchResult } from "../../api/client";
import { useMaybeProject } from "../../app/project";
import { canEdit, isAdmin, useMe } from "../../app/session";
import { Button, ErrorLine, Field, Input, Modal, ServiceIcon, cx, isMac, readPref, toastError, writePref } from "../../kit";
import { isTyping } from "../canvas/Changes";
import { ACTIVITY_KEY } from "../activity/bridge";
import { buildCommands } from "./commands";
import { back, missingCommandUrl, push, rank, searchGroups, type Cmd, type Level } from "./model";

export const PALETTE_EVENT = "5050:palette";
export const openPalette = () => window.dispatchEvent(new Event(PALETTE_EVENT));
/** Opens the palette directly on the sub-level whose command id is `id` (for example "search:projects"). */
export const openPaletteAt = (id: string) => window.dispatchEvent(new CustomEvent(PALETTE_EVENT, { detail: { open: id } }));

const FAVOURITES_PREF = "5050.favourites";

export function Palette() {
  const [open, setOpen] = useState(false);
  const [initial, setInitial] = useState<string>();
  const nav = useNavigate();
  const me = useMe();
  useEffect(() => {
    const k = (e: KeyboardEvent) => {
      const mod = e.metaKey || e.ctrlKey;
      if (mod && e.key.toLowerCase() === "k") { e.preventDefault(); setInitial(undefined); setOpen((o) => !o); }
      if (mod && e.key === "/" && canEdit(me)) { e.preventDefault(); setOpen(false); nav("/new"); }
    };
    const o = (e: Event) => { setInitial((e as CustomEvent<{ open?: string } | null>).detail?.open); setOpen(true); };
    window.addEventListener("keydown", k);
    window.addEventListener(PALETTE_EVENT, o);
    return () => { window.removeEventListener("keydown", k); window.removeEventListener(PALETTE_EVENT, o); };
  }, [nav, me]);
  useTabShortcuts();
  return open ? <PaletteDialog initial={initial} onClose={() => setOpen(false)} /> : null;
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

/** A 28px keycap: "Cmd" renders as the platform modifier glyph. */
function Keycap({ children }: { children: string }) {
  const label = children === "Cmd" ? (isMac() ? "⌘" : "Ctrl") : children;
  return <kbd className="inline-flex h-7 min-w-7 items-center justify-center rounded border border-line bg-surface-hover px-2 py-1 font-sans text-caption leading-none text-fg-2">{label}</kbd>;
}

const FALLBACK = "__fallback";

function PaletteDialog({ onClose, initial }: { onClose: () => void; initial?: string }) {
  const nav = useNavigate();
  const loc = useLocation();
  const me = useMe();
  const proj = useMaybeProject();
  const [q, setQ] = useState("");
  const [cursor, setCursor] = useState(0);
  const [stack, setStack] = useState<Level[]>([{ items: [] }]);
  const [dir, setDir] = useState<"push" | "pop" | null>(null);
  const [found, setFound] = useState<SearchResult>();
  const [projects, setProjects] = useState<Project[]>();
  const [newEnv, setNewEnv] = useState(false);
  const [favs, setFavs] = useState<string[]>(() => readPref(FAVOURITES_PREF, []));
  const input = useRef<HTMLInputElement>(null);
  const listRef = useRef<HTMLUListElement>(null);
  const [activityOpen] = useState<boolean>(() => readPref(ACTIVITY_KEY, false));
  const sid = selectedService(loc.pathname);

  // hand focus back to whatever had it when the palette opened
  useEffect(() => {
    const prev = document.activeElement as HTMLElement | null;
    return () => { if (prev && document.contains(prev)) prev.focus(); };
  }, []);
  useEffect(() => { void api.projects("activity").then(setProjects, () => {}); }, []);

  const go = useCallback((path: string) => { onClose(); nav(path); }, [nav, onClose]);
  const toggleFavourite = useCallback((id: string) => setFavs((f) => { const n = f.includes(id) ? f.filter((x) => x !== id) : [...f, id]; writePref(FAVOURITES_PREF, n); return n; }), []);

  const root = useMemo(() => buildCommands({
    go, close: onClose, proj, editable: canEdit(me), admin: isAdmin(me), serviceId: sid, projects,
    newEnvironment: () => setNewEnv(true), newProject: () => go("/new"), favourites: favs, toggleFavourite, activityOpen,
  }), [go, onClose, proj, me, sid, projects, favs, toggleFavourite, activityOpen]);

  // open straight on a sub-level when asked (for example the dashboard search button)
  const opened = useRef(false);
  useEffect(() => {
    if (opened.current || !initial) return;
    const c = root.find((x) => x.id === initial && x.children);
    if (!c) return;
    opened.current = true;
    setStack([{ items: root }, { title: c.title, placeholder: c.placeholder, items: c.children!() }]);
  }, [root, initial]);

  // rebuild the open levels when the tree changes (projects arrive, favourites toggle) without losing depth
  useEffect(() => {
    setStack((s) => {
      if (s.length === 1) return [{ items: root }];
      let items = root;
      return s.map((lvl, i) => {
        if (i === 0) return { items: root };
        const parent = items.find((c) => c.title === lvl.title && c.children);
        items = parent?.children?.() || lvl.items;
        return { ...lvl, items };
      });
    });
  }, [root]);
  const level = stack[stack.length - 1];
  const atRoot = stack.length === 1;

  // server search covers what the tree cannot: services of other projects, deployments by commit message
  useEffect(() => {
    setFound(undefined);
    if (!atRoot || q.trim().length < 2) return;
    const ctl = new AbortController();
    const t = setTimeout(() => api.search(q.trim(), ctl.signal).then(setFound, () => {}), 150);
    return () => { clearTimeout(t); ctl.abort(); };
  }, [q, atRoot]);

  /** Sections of rows, each with an optional header, plus the flat row list for the cursor. */
  const { sections, rows } = useMemo(() => {
    const query = q.trim();
    let secs: { header?: string; items: Cmd[] }[] = [];
    if (!query) {
      // root: consecutive commands of one group share a section; only later groups get a header
      const keyed: { key: string; items: Cmd[] }[] = [];
      for (const c of level.items) {
        const key = atRoot ? c.group || "" : "";
        const last = keyed[keyed.length - 1];
        if (last && last.key === key) last.items.push(c); else keyed.push({ key, items: [c] });
      }
      secs = keyed.map((k, n) => ({ header: atRoot && n > 0 && k.key ? k.key : undefined, items: k.items }));
    } else if (atRoot) {
      secs = searchGroups(level.items, query).map((g) => ({ header: g.header, items: g.hits.map((h) => h.cmd) }));
      if (found) {
        const have = new Set(secs.flatMap((s) => s.items.map((c) => c.title.toLowerCase())));
        const extra: [keyof SearchResult, string][] = [["services", "Services"], ["environments", "Environments"], ["settings", "Settings"], ["deployments", "Deployments"]];
        for (const [k, label] of extra) {
          const items = (found[k] || []).filter((it) => !have.has(it.title.toLowerCase())).map((it): Cmd => ({
            id: `r:${k}:${it.id}`, title: it.title, subtitle: it.subtitle, icon: k === "services" ? <ServiceIcon size={20} /> : <Folder />, run: () => go(it.path),
          }));
          if (items.length) secs.push({ header: label, items });
        }
      }
    } else {
      const items = rank(level.items, query);
      if (items.length) secs = [{ items }];
    }
    if (!secs.length && query) {
      secs = [{ items: [{ id: FALLBACK, title: `Report a missing command "${query}"`, accent: true, run: () => { onClose(); window.open(missingCommandUrl(query), "_blank", "noopener"); } }] }];
    }
    return { sections: secs, rows: secs.flatMap((s) => s.items) };
  }, [level, q, atRoot, found, go, onClose]);

  useEffect(() => { setCursor(0); }, [q, stack]);
  useEffect(() => { listRef.current?.querySelector<HTMLElement>(`[data-i="${cursor}"]`)?.scrollIntoView({ block: "nearest" }); }, [cursor]);

  const enter = (c: Cmd) => {
    if (c.children) {
      setDir("push"); setStack((s) => push(s, c, c.children!())); setQ(""); input.current?.focus();
      return;
    }
    c.run?.();
  };
  const pop = () => { setDir("pop"); setStack(back); };

  const onKey = (e: React.KeyboardEvent) => {
    const cur = rows[cursor];
    if (e.key === "ArrowDown") { e.preventDefault(); setCursor((i) => Math.min(rows.length - 1, i + 1)); }
    else if (e.key === "ArrowUp") { e.preventDefault(); setCursor((i) => Math.max(0, i - 1)); }
    else if (e.key === "ArrowRight" && cur?.children && (e.currentTarget as HTMLInputElement).selectionStart === q.length) { e.preventDefault(); enter(cur); }
    else if (e.key === "Backspace" && !q && !atRoot) { e.preventDefault(); pop(); }
    else if (e.key === "Tab") e.preventDefault();
    else if (e.key === "Enter") { e.preventDefault(); if (cur) try { enter(cur); } catch (err) { toastError(err); } }
    else if (e.key === "Escape") { e.preventDefault(); e.stopPropagation(); if (!atRoot) pop(); else onClose(); }
  };

  if (newEnv && proj) return <NewEnvironment onClose={onClose} />;
  let index = -1;
  return createPortal(
    <div className="fixed inset-0 z-[10000] flex items-center justify-center bg-overlay p-4" onMouseDown={(e) => { if (e.target === e.currentTarget) onClose(); }}>
      <div className="relative mb-20 w-full md:w-[500px]">
        {!atRoot && (
          <div className="absolute bottom-full left-0 flex space-x-1.5 pb-3">
            {stack.slice(1).map((l, i) => <span key={i} className="rounded bg-surface-raised px-2 py-1 text-body font-medium text-foreground">{l.title}</span>)}
          </div>
        )}
        <div role="dialog" aria-modal="true" aria-label="Command palette" className="flex max-h-[420px] flex-col overflow-hidden rounded-lg bg-surface-raised">
          <input ref={input} autoFocus value={q} onChange={(e) => setQ(e.target.value)} onKeyDown={onKey} aria-label="Search commands" autoComplete="off" spellCheck={false}
            role="combobox" aria-expanded="true" aria-controls="palette-list" aria-activedescendant={rows[cursor] ? `pal-${cursor}` : undefined}
            placeholder={level.placeholder || "What can we help with?"}
            className="h-14 w-full shrink-0 rounded-t-lg bg-surface-raised px-6 text-lead text-foreground caret-white outline-none placeholder:text-fg-3 focus-visible:shadow-none" />
          <div key={stack.length} className={cx("scroll-palette min-h-0 flex-1 overflow-y-auto border-t border-line", dir === "push" && "animate-palette-push", dir === "pop" && "animate-palette-pop")}>
            <ul id="palette-list" ref={listRef} role="listbox" className="py-3">
              {sections.map((sec, si) => (
                <li key={si} role="presentation" className={cx(sec.header && si > 0 && "mt-2 border-t border-line pt-2")}>
                  {sec.header && <p className="mx-3 px-3 py-1 text-caption font-semibold text-gray-700">{sec.header}</p>}
                  <ul role="group">
                    {sec.items.map((c) => {
                      index++;
                      const i = index;
                      const on = i === cursor;
                      return <Row key={c.id + i} c={c} i={i} on={on} onHover={() => setCursor(i)} onRun={() => { try { enter(c); } catch (err) { toastError(err); } }} />;
                    })}
                  </ul>
                </li>
              ))}
            </ul>
          </div>
        </div>
      </div>
    </div>,
    document.body,
  );
}

function Row({ c, i, on, onHover, onRun }: { c: Cmd; i: number; on: boolean; onHover: () => void; onRun: () => void }) {
  const fallback = c.id === FALLBACK;
  const iconTone = c.accent ? "text-pink-500" : on ? "text-fg-2" : "text-fg-4";
  return (
    <li id={`pal-${i}`} data-i={i} role="option" aria-selected={on} onMouseMove={onHover} onClick={onRun} className="mx-3 scroll-my-3 cursor-pointer">
      <div className={cx("flex items-center justify-between rounded-lg px-3 py-3 transition-colors duration-150", on ? "bg-surface-hover text-foreground" : "text-fg-3")}>
        <span className="flex min-w-0 items-center space-x-3">
          {(c.icon || fallback) && (
            <span className={cx("flex size-5 shrink-0 items-center justify-center transition-colors duration-150 [&>img]:size-5 [&>svg]:size-5", iconTone)}>
              {fallback ? <MessageSquarePlus /> : c.icon}
            </span>
          )}
          <span className="truncate text-lead">{c.title}</span>
          {c.subtitle && <span className="truncate text-caption text-fg-4">{c.subtitle}</span>}
        </span>
        <span className="ml-3 flex shrink-0 items-center space-x-2">
          {fallback && <span className="rounded bg-tint-pill px-1.5 py-0.5 text-micro font-semibold uppercase tracking-[.25px] text-pink-500">Issue</span>}
          {fallback && <Keycap>Enter</Keycap>}
          {c.chips && <span className="flex space-x-2">{c.chips.map((k, n) => <Keycap key={n}>{k}</Keycap>)}</span>}
          {c.star && (
            <button type="button" aria-label={c.star.on ? "Remove from favourites" : "Add to favourites"} onClick={(e) => { e.stopPropagation(); c.star!.toggle(); }}
              className={cx("flex size-5 items-center justify-center rounded transition-colors duration-150 hover:text-foreground", c.star.on ? "text-warn" : "text-fg-4")}>
              <Star size={16} fill={c.star.on ? "currentColor" : "none"} />
            </button>
          )}
          {c.external && <ArrowUpRight size={16} className="text-fg-4" />}
          {c.children && <ChevronRight size={20} className="text-fg-4" />}
        </span>
      </div>
    </li>
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
