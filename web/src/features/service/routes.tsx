// SPDX-License-Identifier: AGPL-3.0-only

// Route components for the WP8b screens (docs/SPEC.md 13.1). The shell
// (WP8a) mounts them under its project layout and passes the current
// environment id:
//
//   /project/:pid/service/:sid/:tab?                     <ServiceRoute eid />
//   /project/:pid/service/:sid/deployment/:did/:tab?     <ServiceRoute eid />
//   /project/:pid/volume/:vid                            <VolumeRoute eid />
//   /project/:pid/logs                                   <LogExplorer environmentId />
//   /project/:pid/observability                          <ObservabilityRoute eid />

import { lazy, Suspense, useEffect, useLayoutEffect, useRef, type ReactNode } from "react";
import { useLocation, useNavigate, useParams } from "react-router-dom";
import type { Canvas, Service } from "../../api/types.gen";
import { cx } from "../../kit";
import { get, svcPath } from "./client";
import { ErrorLine, Loading, useLoad } from "./ui";
import ServicePanel, { TAB_ALIAS, type PanelTab } from "./ServicePanel";
import DeploymentView, { type DeploymentTab } from "../deployment/DeploymentView";

// Other packages load on demand, so a problem in one of them never blanks the service panel.
const VolumePanel = lazy(() => import("../volume/VolumePanel"));
const Observability = lazy(() => import("../metrics/Observability"));
const LogExplorerLazy = lazy(() => import("../logs/LogExplorer"));
export function LogExplorer(p: { environmentId: string }) {
  return <Suspense fallback={<Loading />}><LogExplorerLazy {...p} /></Suspense>;
}
export const AttachVolumeModal = lazy(() => import("../volume/VolumePanel").then((m) => ({ default: m.AttachVolumeModal })));
export { default as ServicePanel } from "./ServicePanel";

const DEP_TABS: DeploymentTab[] = ["details", "build", "deploy", "network"];

function useGo() {
  const nav = useNavigate();
  const loc = useLocation();
  // Keep the query (environment selection) but drop metrics range params when leaving a tab.
  return (path: string) => {
    const q = new URLSearchParams(loc.search);
    q.delete("mrange"); q.delete("mfrom"); q.delete("muntil");
    const s = q.toString();
    nav(path + (s ? `?${s}` : ""));
  };
}

const reduced = () => typeof window !== "undefined" && !!window.matchMedia?.("(prefers-reduced-motion: reduce)").matches;

/** Slides a layer in from the right (200ms ease-out) the first time it mounts for a given key. */
const shown = new Set<string>();
function useSlideIn(ref: React.RefObject<HTMLElement>, key: string) {
  useLayoutEffect(() => {
    if (shown.has(key)) return;
    shown.add(key);
    if (!reduced()) ref.current?.animate?.([{ transform: "translateX(48px)", opacity: 0 }, { transform: "none", opacity: 1 }], { duration: 200, easing: "cubic-bezier(0,0,.2,1)" });
  }, [ref, key]);
  useEffect(() => () => { setTimeout(() => shown.delete(key), 400); }, [key]);
}

/**
 * One layer of the side panel stack: 903px wide at desktop, anchored to the right edge of the
 * canvas card with a 24px top gap, 12px top-left radius, hairline border and no scrim.
 * A layer under the deployment drawer sits 25px left and 24px lower, dimmed.
 */
function Layer({ children, under, id, onUnderClick }: { children: ReactNode; under?: boolean; id: string; onUnderClick?: () => void }) {
  const ref = useRef<HTMLDivElement>(null);
  useSlideIn(ref, id);
  return (
    <div ref={ref}
      className={cx("pointer-events-auto absolute bottom-0 right-0 top-0 w-full overflow-hidden bg-surface-raised md:top-6 md:w-[min(903px,calc(100%-56px))] md:rounded-tl-panel md:border-l md:border-t md:border-line",
        "transition-[transform,filter] duration-200 ease-out", under && "md:-translate-x-[25px] md:translate-y-6 md:brightness-75")}>
      {children}
      {under && <button type="button" aria-label="Back to the service" tabIndex={-1} onClick={onUnderClick} className="absolute inset-0 z-20 cursor-default" />}
    </div>
  );
}

/** Escape closes the topmost layer: an open menu or dialog first, then the drawer, then the panel. */
function useEscape(onEscape: () => void) {
  const cb = useRef(onEscape);
  cb.current = onEscape;
  useEffect(() => {
    const k = (e: KeyboardEvent) => {
      if (e.key !== "Escape" || e.defaultPrevented) return;
      const t = e.target as HTMLElement | null;
      if (t && (t.tagName === "INPUT" || t.tagName === "TEXTAREA" || t.tagName === "SELECT" || t.isContentEditable)) return;
      if (document.querySelector("[data-popover], [aria-modal='true'], [data-escape-layer]")) return;
      cb.current();
    };
    document.addEventListener("keydown", k);
    return () => document.removeEventListener("keydown", k);
  }, []);
}

export function ServiceRoute({ eid }: { eid: string }) {
  const { pid = "", sid = "", tab, did } = useParams();
  const go = useGo();
  const base = `/project/${pid}/service/${sid}`;
  const panelTab = ((tab && TAB_ALIAS[tab]) || tab || "deployments") as PanelTab;
  const closeDrawer = () => go(base);
  useEscape(() => (did ? closeDrawer() : go(`/project/${pid}`)));
  return (
    <div className="pointer-events-none absolute inset-0 z-20">
      <Layer id={`svc:${sid}`} under={!!did} onUnderClick={closeDrawer}>
        <ServicePanel eid={eid} sid={sid} tab={did ? "deployments" : panelTab}
          onTab={(t) => go(t === "deployments" ? base : `${base}/${t}`)}
          onClose={() => go(`/project/${pid}`)}
          onOpenDeployment={(d, t) => go(`${base}/deployment/${d}${t ? `/${t}` : ""}`)}
          onOpenService={(s) => go(`/project/${pid}/service/${s}`)} />
      </Layer>
      {did && (
        <Layer id={`dep:${did}`}>
          <DepWithService eid={eid} sid={sid} did={did} tab={DEP_TABS.includes(tab as DeploymentTab) ? (tab as DeploymentTab) : undefined}
            onTab={(t) => go(`${base}/deployment/${did}/${t}`)} onBack={closeDrawer}
            onOpen={(nd) => go(`${base}/deployment/${nd}`)} onSettings={() => go(`${base}/settings`)} />
        </Layer>
      )}
    </div>
  );
}

function DepWithService({ eid, sid, ...p }: { eid: string; sid: string; did: string; tab?: DeploymentTab; onTab: (t: DeploymentTab) => void; onBack: () => void; onOpen: (d: string) => void; onSettings: () => void }) {
  const svc = useLoad(() => get<Service>(svcPath(eid, sid)).catch(() => undefined), [eid, sid]);
  return <DeploymentView service={svc.data} {...p} />;
}

export function VolumeRoute({ eid }: { eid: string }) {
  const { pid = "", vid = "" } = useParams();
  const go = useGo();
  const c = useLoad(() => get<Canvas>(`/environments/${encodeURIComponent(eid)}/canvas`), [eid]);
  if (!c.data) return c.error ? <ErrorLine>{c.error}</ErrorLine> : <Loading />;
  const owner = c.data.services.find((s) => s.volume?.id === vid);
  return (
    <div className="pointer-events-none absolute inset-0 z-20">
      <Layer id={`vol:${vid}`}>
        <div className="flex h-full min-h-0 flex-col">
          {owner ? <Suspense fallback={<Loading />}><VolumePanel eid={eid} serviceId={owner.id} serviceName={owner.config.name} onClose={() => go(`/project/${pid}`)} /></Suspense>
            : <div className="p-6"><ErrorLine>This volume is not in the current environment.</ErrorLine></div>}
        </div>
      </Layer>
    </div>
  );
}

export function ObservabilityRoute({ eid }: { eid: string }) {
  const { pid = "" } = useParams();
  const go = useGo();
  return <Suspense fallback={<Loading />}><Observability environmentId={eid} onOpenService={(s) => go(`/project/${pid}/service/${s}/metrics`)} /></Suspense>;
}
