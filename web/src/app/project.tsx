// SPDX-License-Identifier: AGPL-3.0-only

// The open project: its environment, the live canvas and the event bus every
// panel under /project/:pid shares (WP8b and WP8c panels read it too).
import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { useParams, useSearchParams } from "react-router-dom";
import { api, type Canvas, type Environment, type Event, type Project, type Service } from "../api/client";
import { useEvents } from "../api/hooks";

export interface ProjectCtx {
  pid: string;
  project?: Project;
  env?: Environment;
  eid: string;
  canvas?: Canvas;
  error?: Error;
  /** Replaces the canvas (optimistic updates); pass a function to derive from the current one. */
  setCanvas: (f: (c: Canvas) => Canvas) => void;
  reload: () => Promise<void>;
  reloadProject: () => Promise<void>;
  /** Appends ?environment= so links keep the environment. */
  withEnv: (path: string) => string;
  setEnvironment: (eid: string) => void;
  subscribe: (fn: (e: Event) => void) => () => void;
}

const Ctx = createContext<ProjectCtx | null>(null);

/** The open project, or null outside /project/:pid (the palette is mounted everywhere). */
export const useMaybeProject = () => useContext(Ctx);

export function useProject(): ProjectCtx {
  const c = useContext(Ctx);
  if (!c) throw new Error("useProject outside ProjectProvider");
  return c;
}

/** Calls fn for every event of the open environment. */
export function useProjectEvents(fn: (e: Event) => void) {
  const { subscribe } = useProject();
  const ref = useRef(fn);
  ref.current = fn;
  useEffect(() => subscribe((e) => ref.current(e)), [subscribe]);
}

const envKey = (pid: string) => `5050.env.${pid}`;

export function pickEnvironment(p: Project, wanted?: string | null): Environment | undefined {
  return p.environments.find((e) => e.id === wanted) || p.environments.find((e) => e.name === "production") || p.environments[0];
}

/** Applies one event to the canvas; returns the same object when nothing changed. */
export function applyEvent(c: Canvas, e: Event): Canvas {
  switch (e.type) {
    case "service":
    case "deployment": {
      const s = e.service;
      if (!s) return c;
      const has = c.services.some((x) => x.id === s.id);
      const services: Service[] = has ? c.services.map((x) => (x.id === s.id ? s : x)) : [...c.services, s];
      return { ...c, services };
    }
    case "changes":
      return e.changesCount === undefined ? c : { ...c, changesCount: e.changesCount };
    case "positions":
      return e.positions ? { ...c, positions: { ...c.positions, ...e.positions } } : c;
    case "presence":
      return e.presence ? { ...c, presence: e.presence } : c;
    case "groups":
      return e.groups ? { ...c, groups: e.groups } : c;
    default:
      return c;
  }
}

export function ProjectProvider({ children }: { children: ReactNode }) {
  const { pid = "" } = useParams();
  const [params, setParams] = useSearchParams();
  const [project, setProject] = useState<Project>();
  const [canvas, setCanvasState] = useState<Canvas>();
  const [error, setError] = useState<Error>();
  const listeners = useRef(new Set<(e: Event) => void>());

  const reloadProject = useCallback(() => api.project(pid).then(setProject, setError), [pid]);
  useEffect(() => {
    setProject(undefined); setCanvasState(undefined); setError(undefined);
    void reloadProject();
  }, [reloadProject]);

  let stored: string | null = null;
  try { stored = localStorage.getItem(envKey(pid)); } catch { /* private window */ }
  const env = project ? pickEnvironment(project, params.get("environment") || stored) : undefined;
  const eid = env?.id || "";

  const reload = useCallback(async () => {
    if (!eid) return;
    try { setCanvasState(await api.canvas(eid)); setError(undefined); } catch (e) { setError(e as Error); }
  }, [eid]);
  useEffect(() => { setCanvasState(undefined); void reload(); }, [reload]);

  // staged changes alter many fields at once: fetch the canvas again shortly after a "changes" event
  const pending = useRef<ReturnType<typeof setTimeout>>();
  useEvents(eid, (e) => {
    setCanvasState((c) => (c ? applyEvent(c, e) : c));
    if (e.type === "changes" || e.type === "service" || e.type === "deployment" || e.type === "resync") { clearTimeout(pending.current); pending.current = setTimeout(() => void reload(), 250); }
    listeners.current.forEach((f) => f(e));
  }, () => void reload());

  const setCanvas = useCallback((f: (c: Canvas) => Canvas) => setCanvasState((c) => (c ? f(c) : c)), []);
  const subscribe = useCallback((fn: (e: Event) => void) => { listeners.current.add(fn); return () => { listeners.current.delete(fn); }; }, []);
  const withEnv = useCallback((path: string) => (eid ? `${path}${path.includes("?") ? "&" : "?"}environment=${encodeURIComponent(eid)}` : path), [eid]);
  const setEnvironment = useCallback((id: string) => {
    try { localStorage.setItem(envKey(pid), id); } catch { /* private window */ }
    const n = new URLSearchParams(params); n.set("environment", id); setParams(n);
  }, [pid, params, setParams]);

  const value = useMemo<ProjectCtx>(() => ({ pid, project: canvas?.project || project, env, eid, canvas, error, setCanvas, reload, reloadProject, withEnv, setEnvironment, subscribe }),
    [pid, project, env, eid, canvas, error, setCanvas, reload, reloadProject, withEnv, setEnvironment, subscribe]);
  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}
