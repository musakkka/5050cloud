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

import { useLocation, useNavigate, useParams } from "react-router-dom";
import type { Canvas, Service } from "../../api/types.gen";
import { get, svcPath } from "./client";
import { ErrorLine, Loading, useLoad } from "./ui";
import ServicePanel, { type PanelTab } from "./ServicePanel";
import DeploymentView, { type DeploymentTab } from "../deployment/DeploymentView";
import VolumePanel from "../volume/VolumePanel";
import Observability from "../metrics/Observability";

export { default as LogExplorer } from "../logs/LogExplorer";
export { default as ServicePanel } from "./ServicePanel";
export { AttachVolumeModal } from "../volume/VolumePanel";

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

export function ServiceRoute({ eid }: { eid: string }) {
  const { pid = "", sid = "", tab, did } = useParams();
  const go = useGo();
  const base = `/project/${pid}/service/${sid}`;
  if (did) {
    return (
      <div className="absolute inset-0 z-20 flex flex-col border-l border-gray-200 bg-secondaryBg p-4 shadow-xl sm:left-auto sm:w-[min(1040px,80%)] sm:rounded-l-xl sm:p-8">
        <DepWithName eid={eid} sid={sid} did={did} tab={DEP_TABS.includes(tab as DeploymentTab) ? (tab as DeploymentTab) : undefined}
          onTab={(t) => go(`${base}/deployment/${did}/${t}`)} onBack={() => go(base)}
          onOpen={(nd) => go(`${base}/deployment/${nd}`)} />
      </div>
    );
  }
  return (
    <ServicePanel eid={eid} sid={sid} tab={(tab as PanelTab) || "deployments"}
      onTab={(t) => go(t === "deployments" ? base : `${base}/${t}`)}
      onClose={() => go(`/project/${pid}`)}
      onOpenDeployment={(d) => go(`${base}/deployment/${d}`)}
      onOpenService={(s) => go(`/project/${pid}/service/${s}`)} />
  );
}

function DepWithName({ eid, sid, ...p }: { eid: string; sid: string; did: string; tab?: DeploymentTab; onTab: (t: DeploymentTab) => void; onBack: () => void; onOpen: (d: string) => void }) {
  const svc = useLoad(() => get<Service>(svcPath(eid, sid)).catch(() => undefined), [eid, sid]);
  return <DeploymentView serviceName={svc.data?.config.name} {...p} />;
}

export function VolumeRoute({ eid }: { eid: string }) {
  const { pid = "", vid = "" } = useParams();
  const go = useGo();
  const c = useLoad(() => get<Canvas>(`/environments/${encodeURIComponent(eid)}/canvas`), [eid]);
  if (!c.data) return c.error ? <ErrorLine>{c.error}</ErrorLine> : <Loading />;
  const owner = c.data.services.find((s) => s.volume?.id === vid);
  return (
    <div className="absolute inset-0 z-20 overflow-y-auto border-l border-gray-200 bg-secondaryBg shadow-xl sm:left-auto sm:w-[min(720px,80%)] sm:rounded-l-xl">
      {owner ? <VolumePanel eid={eid} serviceId={owner.id} serviceName={owner.config.name} onClose={() => go(`/project/${pid}`)} />
        : <div className="p-6"><ErrorLine>This volume is not in the current environment.</ErrorLine></div>}
    </div>
  );
}

export function ObservabilityRoute({ eid }: { eid: string }) {
  const { pid = "" } = useParams();
  const go = useGo();
  return <Observability environmentId={eid} onOpenService={(s) => go(`/project/${pid}/service/${s}/metrics`)} />;
}
