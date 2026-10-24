// SPDX-License-Identifier: AGPL-3.0-only

// The deployment drawer (docs/SPEC.md 13.5): a second layer over the service
// panel with the service / id breadcrumb, status pill, actions, timestamp,
// domain line, and the Details, Build Logs, Deploy Logs and Network Logs tabs.

import { useEffect, useRef, useState } from "react";
import { useParams } from "react-router-dom";
import { ArrowUpRight, X } from "lucide-react";
import type { DeploymentDetail, Service } from "../../api/types.gen";
import { IconButton, ServiceIcon, Skeleton, Tooltip, cx, toast } from "../../kit";
import { get } from "../service/client";
import { useEnvEvents } from "../service/events";
import { useLoad } from "../service/ui";
import { defaultTab, inProgress } from "./actions";
import { fmtMinute, tzLabel } from "./format";
import { DepMenu, StatusPill, useDepActions } from "./parts";
import Details from "./Details";
import LogView from "./LogView";
import NetworkLogs from "./NetworkLogs";

export type DeploymentTab = "details" | "build" | "deploy" | "network";

const TABS: { id: DeploymentTab; label: string }[] = [
  { id: "details", label: "Details" }, { id: "build", label: "Build Logs" }, { id: "deploy", label: "Deploy Logs" }, { id: "network", label: "Network Logs" },
];

export default function DeploymentView({ did, service, serviceName, tab, onTab, onBack, onOpen, onSettings }: {
  did: string;
  /** The service, for the icon and the domain line. */
  service?: Service;
  serviceName?: string;
  tab?: DeploymentTab;
  onTab?: (t: DeploymentTab) => void;
  /** Close the drawer (back to the service panel). */
  onBack: () => void;
  /** Open another deployment (after redeploy or rollback). */
  onOpen?: (did: string) => void;
  /** Jump to the service settings (diagnosis action). */
  onSettings?: () => void;
}) {
  const { pid } = useParams();
  const dep = useLoad(() => get<DeploymentDetail>(`/deployments/${encodeURIComponent(did)}`), [did]);
  const [localTab, setLocalTab] = useState<DeploymentTab | undefined>(tab);
  useEnvEvents(dep.data?.environmentId, (e) => {
    if (e.type === "deployment" && e.deployment?.id === did) void dep.reload();
  });
  useEffect(() => setLocalTab(tab), [tab]);
  const d = dep.data;
  const reload = dep.reload;
  const running = !!d && inProgress(d);
  useEffect(() => {
    if (!running) return;
    const t = setInterval(() => void reload(), 3000);
    return () => clearInterval(t);
  }, [running, reload]);

  const current: DeploymentTab = localTab || (d ? defaultTab(d) : "deploy");
  const setTab = (t: DeploymentTab) => { setLocalTab(t); onTab?.(t); };
  const actions = useDepActions((a, r) => {
    if (r && r.id !== did && (a === "redeploy" || a === "rollback") && onOpen) onOpen(r.id);
    else void dep.reload();
  });

  const name = service?.config.name ?? serviceName;
  const src = service?.config.source;
  const domain = service?.domains.find((x) => x.status === "active") || service?.domains[0];
  const tabRefs = useRef<(HTMLButtonElement | null)[]>([]);

  return (
    <div className="flex h-full min-h-0 flex-col">
      <header className="px-4 pt-6 sm:px-12 sm:pt-10">
        <div className="flex h-[34px] min-w-0 items-center gap-3">
          <ServiceIcon icon={service?.config.icon} image={src?.image} source={service?.kind === "cron" ? "cron" : src?.type === "image" ? "image" : src?.type === "github" ? "github" : "cli"} size={24} />
          <button type="button" onClick={onBack} title="Back to the service"
            className="max-w-[40%] truncate text-h3 font-semibold text-foreground transition-colors duration-150 hover:text-fg-2">{name || <Skeleton w={80} h={18} />}</button>
          <span aria-hidden className="text-lg font-bold text-line">/</span>
          <Tooltip content="Copy deployment id">
            <button type="button" onClick={() => { void navigator.clipboard?.writeText(did); toast("Copied deployment id", "success"); }}
              className="font-mono text-lg font-normal text-foreground transition-colors duration-150 hover:text-fg-2">{did.slice(0, 8)}</button>
          </Tooltip>
          {d && <StatusPill d={d} />}
          <span className="flex-1" />
          {d && <DepMenu d={d} onAction={actions.request} size={34} horizontal label="Deployment actions" />}
          {d && <time className="hidden whitespace-nowrap text-caption text-fg-3 md:inline" title={new Date(d.createdAt > 1e12 ? d.createdAt : d.createdAt * 1000).toString()}>{fmtMinute(d.createdAt)} {tzLabel(d.createdAt)}</time>}
          <IconButton label="Close deployment" size={34} onClick={onBack} className="text-foreground"><X size={16} /></IconButton>
        </div>
        <div className="mt-2 flex h-5 items-center gap-1 text-sm">
          {domain && (
            <a href={`https://${domain.domain}`} target="_blank" rel="noreferrer"
              className="group inline-flex min-w-0 items-center gap-1 text-foreground transition-colors duration-150 hover:text-fg-2">
              <span className="truncate">{domain.domain}</span><ArrowUpRight size={14} className="shrink-0 text-fg-3 transition-colors group-hover:text-fg-2" />
            </a>
          )}
        </div>
        <div role="tablist" aria-label="Deployment" className="no-scrollbar -mx-4 mt-8 flex gap-4 overflow-x-auto px-4 sm:-mx-12 sm:px-12"
          onKeyDown={(e) => {
            if (e.key !== "ArrowRight" && e.key !== "ArrowLeft") return;
            const i = TABS.findIndex((t) => t.id === current);
            const n = (i + (e.key === "ArrowRight" ? 1 : TABS.length - 1)) % TABS.length;
            setTab(TABS[n].id); tabRefs.current[n]?.focus();
          }}>
          {TABS.map((t, i) => (
            <button key={t.id} ref={(n) => { tabRefs.current[i] = n; }} type="button" role="tab" aria-selected={current === t.id} tabIndex={current === t.id ? 0 : -1}
              onClick={() => setTab(t.id)}
              className={cx("relative whitespace-nowrap pb-3 text-sm transition-colors duration-150", current === t.id ? "text-foreground" : "text-fg-3 hover:text-fg-2")}>
              {t.label}
              {current === t.id && <span className="absolute inset-x-0 bottom-0 h-0.5 bg-foreground" />}
            </button>
          ))}
        </div>
      </header>
      <div className="border-t border-line" />
      <div className="relative min-h-0 flex-1">
        {!d ? (
          dep.error ? (
            <div role="alert" className="m-6 flex items-center gap-3 rounded-md border border-danger-line bg-danger-bg px-4 py-3 text-sm text-danger-text">
              <span className="flex-1">Could not load this deployment. {dep.error}</span>
              <button type="button" onClick={() => void dep.reload()} className="underline underline-offset-2">Retry</button>
            </div>
          ) : (
            <div className="space-y-6 px-12 py-8" aria-busy="true">
              <Skeleton h={44} className="rounded-md" /><Skeleton h={52} className="rounded-lg" /><Skeleton h={80} className="rounded-md" />
            </div>
          )
        ) : current === "details" ? (
          <div className="scroll-thin absolute inset-0 overflow-y-auto">
            <Details d={d} onSettings={d.status === "FAILED" || d.status === "CRASHED" ? onSettings : undefined}
              onRedeploy={d.status === "FAILED" || d.status === "CRASHED" ? () => actions.request(d, "redeploy") : undefined} />
          </div>
        ) : current === "network" ? (
          <NetworkLogs d={d} />
        ) : (
          <LogView key={current} kind={current} d={d} pid={pid} />
        )}
      </div>
      {actions.dialog}
    </div>
  );
}
