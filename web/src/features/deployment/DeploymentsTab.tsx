// SPDX-License-Identifier: AGPL-3.0-only

// Deployments tab (docs/SPEC.md 13.3): service meta row, the active (or
// running, or failed) deployment with its step list, the "no active
// deployment" card with the deploy action, and the history list.

import { useEffect, useMemo, useState } from "react";
import { ChevronDown, CirclePause, EyeOff, Globe, Layers, MapPin, TriangleAlert } from "lucide-react";
import type { Deployment, Service } from "../../api/types.gen";
import { BrandIcon, Button, CopyButton, RelTime, Skeleton, cx, toast } from "../../kit";
import { get, post, svcPath } from "../service/client";
import { useEnvEvents } from "../service/events";
import { useLoad } from "../service/ui";
import { inProgress, type DepAction } from "./actions";
import { lookOf, mmss, outcomeText, titleOf, totalMs, viaLabel } from "./format";
import { AuthorAvatar, DepMenu, OUTCOME_TEXT, OutcomeIcon, StatusBadge, StepRows, useDepActions } from "./parts";

export default function DeploymentsTab({ eid, service, onOpen, onTab }: {
  eid: string; service: Service;
  /** Open the deployment drawer (optionally on a tab). */
  onOpen: (did: string, tab?: "details" | "build" | "deploy" | "network") => void;
  /** Jump to another panel tab (meta row links go to Settings). */
  onTab?: (t: "settings") => void;
}) {
  const base = svcPath(eid, service.id);
  const deps = useLoad(() => get<Deployment[]>(`${base}/deployments?limit=100`), [base]);
  const [showSkipped, setShowSkipped] = useState(true);
  const [historyOpen, setHistoryOpen] = useState(true);
  const [deploying, setDeploying] = useState(false);
  const actions = useDepActions(() => void deps.reload());

  useEnvEvents(eid, (e) => {
    if (e.type === "deployment" && e.deployment?.serviceId === service.id) {
      const nd = e.deployment;
      deps.setData((cur) => {
        if (!cur) return cur;
        const i = cur.findIndex((d) => d.id === nd.id);
        const next = i < 0 ? [nd, ...cur] : cur.map((d) => (d.id === nd.id ? nd : d));
        // Only one deployment is the active one.
        return nd.active ? next.map((d) => (d.id === nd.id ? d : { ...d, active: false })) : next;
      });
    }
  }, () => void deps.reload());

  // While something builds or deploys, poll as well so the step list moves even without the event stream.
  const busy = !!deps.data?.some(inProgress);
  const reload = deps.reload;
  useEffect(() => {
    if (!busy) return;
    const t = setInterval(() => void reload(), 2500);
    return () => clearInterval(t);
  }, [busy, reload]);

  const view = useMemo(() => {
    const list = deps.data || [];
    const running = list.filter(inProgress);
    const active = list.find((d) => d.active);
    const latest = list.find((d) => !inProgress(d));
    // A failed run, or a one-shot image that ran to completion, leads when nothing is active.
    const failed = !active && latest && (latest.status === "FAILED" || latest.status === "CRASHED" || latest.status === "COMPLETED") ? latest : undefined;
    // Cron jobs have no long-running deployment: the latest run leads instead.
    const lastRun = !active && !failed && service.kind === "cron" && latest && latest.status !== "SKIPPED" ? latest : undefined;
    const lead = [...running, ...(active ? [active] : []), ...(failed ? [failed] : []), ...(lastRun ? [lastRun] : [])];
    const history = list.filter((d) => !lead.includes(d));
    return { list, lead, active, history, skipped: history.filter((d) => d.status === "SKIPPED").length };
  }, [deps.data, service.kind]);

  const src = service.config.source;
  const deploy = async () => {
    setDeploying(true);
    try {
      await post<Deployment>(`${base}/deploy`, { ref: "latest" });
      toast(src.type === "image" ? "Deploying the image" : "Deploying the latest commit", "success");
      await deps.reload();
    } catch (e) { toast(e instanceof Error ? e.message : String(e), "error"); } finally { setDeploying(false); }
  };

  const shownHistory = showSkipped ? view.history : view.history.filter((d) => d.status !== "SKIPPED");

  return (
    <div className="pb-12">
      <ServiceNotice service={service} />
      <MetaRow service={service} onSettings={() => onTab?.("settings")} />

      {!deps.data ? (
        deps.error ? <LoadError text={deps.error} onRetry={() => void deps.reload()} /> : <ListSkeleton />
      ) : (
        <>
          <div className="space-y-2">
            {view.lead.map((d) => <LeadCard key={d.id} d={d} onOpen={onOpen} onAction={actions.request} />)}
          </div>
          {!view.lead.length && (
            <NoActiveCard service={service} busy={deploying} onDeploy={() => void deploy()} onSettings={() => onTab?.("settings")} className={view.lead.length ? "mt-2" : ""} />
          )}

          {view.history.length > 0 && (
            <section className="mt-4" aria-label="History">
              <div className="flex h-5 items-center justify-between">
                <button type="button" onClick={() => setHistoryOpen((o) => !o)} aria-expanded={historyOpen}
                  className="-ml-0.5 inline-flex items-center gap-2 text-caption font-medium uppercase text-foreground">
                  <ChevronDown size={16} className={cx("transition-transform duration-150", !historyOpen && "-rotate-90")} />History
                </button>
                <button type="button" onClick={() => setShowSkipped((s) => !s)}
                  className="text-caption text-fg-3 transition-colors duration-150 hover:text-foreground">
                  {showSkipped ? "Hide Skipped" : `Show Skipped${view.skipped ? ` (${view.skipped})` : ""}`}
                </button>
              </div>
              {historyOpen && (
                <ul className="mt-4 space-y-2">
                  {shownHistory.map((d) => (
                    <li key={d.id} style={{ contentVisibility: "auto", containIntrinsicSize: "auto 86px" }}>
                      <HistoryCard d={d} onOpen={onOpen} onAction={actions.request} />
                    </li>
                  ))}
                </ul>
              )}
            </section>
          )}
        </>
      )}
      {actions.dialog}
    </div>
  );
}

/** Our version of the plan notice: a host or capacity problem that stops deploys, only when it is true. */
function ServiceNotice({ service }: { service: Service }) {
  const notes = [...new Set(service.replicas.map((r) => r.note).filter(Boolean))] as string[];
  let title = "", body = "";
  if (service.status === "host_lost") { title = "Volume host is gone"; body = "The host that held this service's volume left the cluster. Restore the volume from a backup to run the service again."; }
  else if (service.status === "pending") { title = "Waiting for capacity"; body = service.statusText || "No host has room for this service right now. Add a host or lower the memory reservation."; }
  else if (notes.length) { title = "A host is offline"; body = notes.join(" "); }
  if (!title) return null;
  return (
    <div role="status" className="mb-8 flex flex-col items-center rounded-md bg-warn-bg px-6 py-8 text-center">
      <CirclePause size={48} strokeWidth={1.5} className="text-warn" />
      <div className="mt-4 text-h2 text-warn">{title}</div>
      <p className="mt-3 max-w-[440px] text-lead text-fg-2">{body}</p>
      <a href="/settings/hosts" className="mt-3 text-lead text-fg-2 underline underline-offset-4 transition-colors duration-150 hover:text-foreground">Open host settings</a>
    </div>
  );
}

function MetaRow({ service, onSettings }: { service: Service; onSettings: () => void }) {
  const online = service.status === "online" || service.status === "unhealthy" || service.status === "deploying" || service.status === "building";
  const domain = service.domains.find((d) => d.status === "active") || service.domains[0];
  const proxy = service.tcpProxies[0];
  const hosts = [...new Set(service.replicas.map((r) => r.hostName))];
  const replicas = service.config.deploy.replicas ?? Math.max(1, service.replicas.length);
  const image = service.config.source.type === "image" ? service.config.source.image : undefined;
  const item = "inline-flex items-center gap-2 whitespace-nowrap text-fg-5 transition-colors duration-150 hover:text-fg-3";
  return (
    <div className="mb-4 flex h-5 min-w-0 items-center justify-between gap-6 text-sm">
      <div className="flex min-w-0 items-center gap-2">
        {!domain && proxy && online ? (
          <>
            <Globe size={16} className="shrink-0 text-ok" />
            <span className="truncate font-mono text-foreground" title="TCP proxy">{proxy.address}</span>
            <CopyButton text={proxy.address} label="Copy address" size={20} />
          </>
        ) : !domain ? (
          <span className="inline-flex items-center gap-2 text-fg-5"><EyeOff size={16} />Unexposed service</span>
        ) : online ? (
          <>
            <Globe size={16} className="shrink-0 text-ok" />
            <a href={`https://${domain.domain}`} target="_blank" rel="noreferrer" className="truncate text-foreground transition-colors duration-150 hover:text-fg-2">{domain.domain}</a>
            <CopyButton text={`https://${domain.domain}`} label="Copy URL" size={20} />
          </>
        ) : (
          <span className="inline-flex items-center gap-2 text-fg-5"><Globe size={16} />Service offline</span>
        )}
      </div>
      <div className="hidden shrink-0 items-center gap-6 sm:flex">
        {image && <button type="button" onClick={onSettings} className={item} title="Source image"><BrandIcon name={image} size={16} fallback="box" />{image}</button>}
        <button type="button" onClick={onSettings} className={item} title="Placement"><MapPin size={16} />{hosts.length ? hosts.join(", ") : "Any host"}</button>
        <button type="button" onClick={onSettings} className={item} title="Replicas"><Layers size={16} />{replicas} Replica{replicas === 1 ? "" : "s"}</button>
      </div>
    </div>
  );
}

function NoActiveCard({ service, busy, onDeploy, onSettings, className }: { service: Service; busy: boolean; onDeploy: () => void; onSettings: () => void; className?: string }) {
  const src = service.config.source;
  const link = "inline-flex h-[34px] items-center gap-1.5 rounded-md px-3 text-sm text-accent-text transition-colors duration-150 hover:bg-pink-50 disabled:opacity-60";
  return (
    <div className={cx("flex min-h-[96px] flex-col items-center justify-center gap-2 rounded-md border border-dashed border-line px-6 py-4 text-center", className)}>
      <p className="text-sm text-fg-3">There is no active deployment for this service.</p>
      {src.type === "github" && src.repo ? (
        <button type="button" className={link} disabled={busy} onClick={onDeploy}>
          {busy && <span className="spin inline-block h-3.5 w-3.5 rounded-full border-2 border-current border-r-transparent" />}
          Deploy the repo <b className="font-semibold">{src.repo.replace(/^https:\/\/github\.com\//, "")}</b>
        </button>
      ) : src.type === "image" && src.image ? (
        <button type="button" className={link} disabled={busy} onClick={onDeploy}>
          {busy && <span className="spin inline-block h-3.5 w-3.5 rounded-full border-2 border-current border-r-transparent" />}
          Deploy the image <b className="font-semibold">{src.image}</b>
        </button>
      ) : (
        <div className="flex flex-wrap items-center justify-center gap-2 text-sm text-fg-3">
          <button type="button" className={cx(link, "underline underline-offset-4")} onClick={onSettings}>Connect a source to get started</button>
          <span>or run</span>
          <code className="inline-flex h-7 items-center gap-1 rounded bg-surface-hover pl-2 font-mono text-caption text-foreground">
            5050 up --service {service.config.name}<CopyButton text={`5050 up --service ${service.config.name}`} size={24} />
          </code>
        </div>
      )}
    </div>
  );
}

function Meta({ d }: { d: Deployment }) {
  return (
    <p className="truncate text-sm text-fg-3">
      <RelTime ts={d.createdAt} /> via {viaLabel(d.trigger)}
      {d.status === "SKIPPED" || d.status === "CANCELLED" ? (d.reason ? `. ${d.reason}` : "") : ""}
    </p>
  );
}

/** The active, running or failed deployment: tinted card with a status bar that opens the step list. */
function LeadCard({ d, onOpen, onAction }: { d: Deployment; onOpen: (did: string, tab?: "details" | "build" | "deploy") => void; onAction: (d: Deployment, a: DepAction) => void }) {
  const look = lookOf(d);
  const [open, setOpen] = useState(look === "busy" || look === "wait");
  const frame = look === "ok" ? "border-green-200 bg-green-50" : look === "bad" ? "border-danger-line bg-red-50" : "border-blue-200 bg-blue-50";
  const bar = look === "ok" ? "bg-green-100 hover:bg-green-200" : look === "bad" ? "bg-danger-bg hover:bg-danger-bg-hover" : "bg-blue-100 hover:bg-blue-200";
  const total = totalMs(d);
  const ring = look === "ok" ? "border-green-50" : look === "bad" ? "border-red-50" : "border-blue-50";
  return (
    <div className={cx("overflow-hidden rounded-md border p-1", frame)} data-deployment-id={d.id}>
      <div role="link" tabIndex={0} onClick={() => onOpen(d.id)} onKeyDown={(e) => { if (e.key === "Enter") onOpen(d.id); }}
        className="flex h-[76px] cursor-pointer items-center rounded-md px-3 outline-none">
        <span className="min-w-24 shrink-0 pr-2"><StatusBadge d={d} /></span>
        <span className="w-[52px] shrink-0"><AuthorAvatar d={d} ring={ring} /></span>
        <div className="min-w-0 flex-1 pr-4">
          <p className="truncate text-sm text-foreground" title={titleOf(d)}>{titleOf(d)}</p>
          <Meta d={d} />
        </div>
        <div className="flex shrink-0 items-center gap-3" onClick={(e) => e.stopPropagation()}>
          <Button variant={look === "bad" ? "danger-outline" : look === "ok" ? "success-outline" : "outline"}
            className={look === "ok" ? "!border-green-200 hover:!bg-green-100" : undefined}
            onClick={() => onOpen(d.id, look === "busy" || look === "wait" ? "build" : "deploy")}>View logs</Button>
          {look === "bad" && <Button variant="primary" onClick={() => onAction(d, "redeploy")}>Redeploy</Button>}
          {look === "ok" && d.status === "CRASHED" && <Button variant="primary" onClick={() => onAction(d, "restart")}>Restart</Button>}
          <DepMenu d={d} onAction={onAction} onViewLogs={() => onOpen(d.id)} />
        </div>
      </div>
      {d.steps.length > 0 && (
        <>
          <button type="button" onClick={() => setOpen((o) => !o)} aria-expanded={open}
            className={cx("flex h-9 w-full items-center gap-4 rounded-md pl-[92px] pr-3 text-left text-sm transition-colors duration-150", bar)}>
            <OutcomeIcon look={look} />
            <span className={cx("min-w-0 flex-1 truncate", OUTCOME_TEXT[look])}>{outcomeText(d)}</span>
            {look === "bad" && total !== undefined && <span className="tabular-nums text-red-300">({mmss(total)})</span>}
            <ChevronDown size={16} className={cx("shrink-0 text-foreground transition-transform duration-150", open && "rotate-180")} />
          </button>
          {open && <div className="mt-1 overflow-hidden rounded-md bg-background"><StepRows d={d} indent={92} labelGap={24} /></div>}
        </>
      )}
      {d.status === "COMPLETED" && d.diagnosis && (
        <p className="mt-1 rounded-md bg-background py-2.5 pl-[92px] pr-3 text-sm leading-[22px] text-fg-2">{d.diagnosis}</p>
      )}
    </div>
  );
}

function HistoryCard({ d, onOpen, onAction }: { d: Deployment; onOpen: (did: string) => void; onAction: (d: Deployment, a: DepAction) => void }) {
  const failed = lookOf(d) === "bad";
  return (
    <div className="group rounded-md border border-line p-1" data-deployment-id={d.id}>
      <div role="link" tabIndex={0} onClick={() => onOpen(d.id)} onKeyDown={(e) => { if (e.key === "Enter") onOpen(d.id); }}
        className="flex h-[76px] cursor-pointer items-center rounded-md bg-surface-hover px-3 outline-none transition-[filter] duration-150 hover:brightness-110 focus-visible:brightness-110">
        <span className="min-w-24 shrink-0 pr-2"><StatusBadge d={d} /></span>
        <span className="w-[52px] shrink-0"><AuthorAvatar d={d} /></span>
        <div className="min-w-0 flex-1 pr-4">
          <p className="truncate text-sm text-foreground" title={titleOf(d)}>{titleOf(d)}</p>
          <Meta d={d} />
        </div>
        <div className="flex shrink-0 items-center gap-3" onClick={(e) => e.stopPropagation()}>
          <Button variant={failed ? "danger-outline" : "outline"} onClick={() => onOpen(d.id)}
            className={cx(!failed && "opacity-0 transition-opacity duration-150 group-hover:opacity-100 group-focus-within:opacity-100")}>View logs</Button>
          <DepMenu d={d} onAction={onAction} onViewLogs={() => onOpen(d.id)} />
        </div>
      </div>
    </div>
  );
}

function ListSkeleton() {
  return (
    <div aria-busy="true" aria-label="Loading deployments">
      <div className="rounded-md border border-line p-1"><div className="flex h-[76px] items-center gap-6 px-3"><Skeleton w={64} h={26} /><Skeleton circle w={24} h={24} /><div className="flex-1 space-y-2"><Skeleton w="60%" h={14} /><Skeleton w="30%" h={14} /></div></div></div>
      <div className="mt-4 h-5" />
      <div className="mt-4 space-y-2">
        {[0, 1, 2].map((i) => (
          <div key={i} className="rounded-md border border-line p-1"><div className="flex h-[76px] items-center gap-6 rounded-md bg-surface-hover px-3"><Skeleton w={72} h={26} /><Skeleton circle w={24} h={24} /><div className="flex-1 space-y-2"><Skeleton w="55%" h={14} /><Skeleton w="25%" h={14} /></div></div></div>
        ))}
      </div>
    </div>
  );
}

function LoadError({ text, onRetry }: { text: string; onRetry: () => void }) {
  return (
    <div role="alert" className="flex items-center gap-3 rounded-md border border-danger-line bg-danger-bg px-4 py-3 text-sm text-danger-text">
      <TriangleAlert size={16} className="shrink-0" />
      <span className="min-w-0 flex-1">Could not load deployments. {text}</span>
      <Button size="sm" variant="danger-outline" onClick={onRetry}>Retry</Button>
    </div>
  );
}
