// SPDX-License-Identifier: AGPL-3.0-only

// Service panel (docs/SPEC.md 13.3): icon, click-to-rename name, close, and
// the tab strip. Routing and the panel frame live in routes.tsx; this
// component takes explicit props so the shell can mount it.

import { lazy, Suspense, useRef, useState } from "react";
import { X } from "lucide-react";
import type { Service } from "../../api/types.gen";
import { IconButton, ServiceIcon, Skeleton, cx, toast } from "../../kit";
import { get, patch, svcPath } from "./client";
import { useEnvEvents } from "./events";
import { ErrorLine, Loading, useLoad } from "./ui";
import DeploymentsTab from "../deployment/DeploymentsTab";

// The other tabs belong to other packages and load on demand, so one failing module never blanks the panel.
const VariablesTab = lazy(() => import("../variables/VariablesTab"));
const MetricsTab = lazy(() => import("../metrics/MetricsTab"));
const SettingsTab = lazy(() => import("../service-settings/SettingsTab"));
const BackupsTab = lazy(() => import("../backups-tab/BackupsTab"));
const DataTab = lazy(() => import("../data/DataTab"));
const CronRunsTab = lazy(() => import("../cron/CronRunsTab"));
const ShellTab = lazy(() => import("../shell/ShellTab"));

export type PanelTab = "deployments" | "variables" | "metrics" | "settings" | "backups" | "data" | "cron" | "console";

/** URL segments people may type or older links use, mapped to our tab ids. */
export const TAB_ALIAS: Record<string, PanelTab> = { database: "data", shell: "console", schedule: "cron", "cron-runs": "cron" };

/** Tab order by kind: cron runs first for cron jobs, Database then Backups for databases, Backups after Deployments with a volume. */
export function panelTabs(s: Service): { id: PanelTab; label: string }[] {
  const db = s.kind === "database";
  const cfg = s.staged ?? s.config;
  return [
    ...(s.kind === "cron" || cfg.deploy?.cronSchedule ? [{ id: "cron" as const, label: "Cron Runs" }] : []),
    { id: "deployments", label: "Deployments" },
    ...(db ? [{ id: "data" as const, label: "Database" }] : []),
    ...(db || s.volume ? [{ id: "backups" as const, label: "Backups" }] : []),
    { id: "variables", label: "Variables" },
    { id: "metrics", label: "Metrics" },
    { id: "console", label: "Console" },
    { id: "settings", label: "Settings" },
  ];
}

export { PANEL_WIDTH } from "./panel";

const sourceKind = (s: Service) => (s.kind === "cron" ? "cron" : s.config.source.type === "image" ? "image" : s.config.source.type === "github" ? "github" : "cli");

export default function ServicePanel({ eid, sid, tab, onTab, onClose, onOpenDeployment, onOpenService }: {
  eid: string;
  sid: string;
  tab?: PanelTab;
  onTab: (t: PanelTab) => void;
  onClose: () => void;
  onOpenDeployment: (did: string, tab?: "details" | "build" | "deploy" | "network") => void;
  onOpenService?: (sid: string) => void;
}) {
  const base = svcPath(eid, sid);
  const svc = useLoad(() => get<Service>(base), [base]);
  useEnvEvents(eid, (e) => {
    if (e.serviceId === sid && e.service) svc.setData(e.service);
    else if (e.serviceId === sid && (e.type === "deployment" || e.type === "changes")) void svc.reload();
  }, () => void svc.reload());

  const s = svc.data;
  const tabs = s ? panelTabs(s) : [];
  const current: PanelTab = tab && tabs.some((t) => t.id === tab) ? tab : "deployments";
  const tabRefs = useRef<(HTMLButtonElement | null)[]>([]);

  return (
    <div role="dialog" aria-label={s ? `Service ${s.config.name}` : "Service"} className="flex h-full min-h-0 flex-col">
      <header className="shrink-0 px-4 pt-6 sm:px-12 sm:pt-12">
        <div className="flex h-[34px] items-center gap-4">
          {s ? <ServiceIcon icon={s.config.icon} image={s.config.source.image} source={sourceKind(s)} size={32} /> : <Skeleton w={32} h={32} className="rounded-md" />}
          {s ? <Name s={s} eid={eid} onService={svc.setData} /> : <Skeleton w={160} h={24} />}
          <span className="flex-1" />
          <IconButton label="Close panel" size={34} onClick={onClose} className="text-foreground"><X size={16} /></IconButton>
        </div>
        <div className="mt-6 flex items-end justify-between gap-4">
          <div role="tablist" aria-label="Service" className="no-scrollbar -mb-px flex min-w-0 gap-8 overflow-x-auto"
            onKeyDown={(e) => {
              if (e.key !== "ArrowRight" && e.key !== "ArrowLeft") return;
              const i = tabs.findIndex((t) => t.id === current);
              const n = (i + (e.key === "ArrowRight" ? 1 : tabs.length - 1)) % tabs.length;
              onTab(tabs[n].id); tabRefs.current[n]?.focus();
            }}>
            {s ? tabs.map((t, i) => (
              <button key={t.id} ref={(n) => { tabRefs.current[i] = n; }} type="button" role="tab" aria-selected={current === t.id} tabIndex={current === t.id ? 0 : -1}
                onClick={() => onTab(t.id)}
                className={cx("relative whitespace-nowrap pb-3 text-lead transition-colors duration-150", current === t.id ? "text-foreground" : "text-fg-3 hover:text-fg-2")}>
                {t.label}
                {current === t.id && <span className="absolute inset-x-0 bottom-0 h-0.5 bg-foreground" />}
              </button>
            )) : <div className="flex gap-8 pb-3">{[96, 70, 58, 62, 62].map((w, i) => <Skeleton key={i} w={w} h={20} />)}</div>}
          </div>
        </div>
      </header>
      <div className="shrink-0 border-t border-line" />
      <div className={current === "console" ? "flex min-h-0 flex-1 flex-col overflow-hidden px-4 sm:px-12" : "scroll-thin min-h-0 flex-1 overflow-y-auto px-4 pb-12 pt-8 sm:px-12"}>
        {!s ? (svc.error ? <ErrorLine>{svc.error}</ErrorLine> : <Loading />) : (
          <Suspense fallback={<Loading />}>
            {current === "deployments" && <DeploymentsTab eid={eid} service={s} onOpen={onOpenDeployment} onTab={(t) => onTab(t)} />}
            {current === "variables" && <VariablesTab eid={eid} service={s} />}
            {current === "metrics" && <MetricsTab eid={eid} service={s} />}
            {current === "settings" && <SettingsTab eid={eid} service={s} onService={svc.setData} onDeleted={() => void svc.reload()} />}
            {current === "backups" && <BackupsTab eid={eid} service={s} onOpenService={onOpenService} />}
            {current === "data" && <DataTab eid={eid} service={s} />}
            {current === "cron" && <CronRunsTab eid={eid} service={s} />}
            {current === "console" && <ShellTab eid={eid} service={s} />}
          </Suspense>
        )}
      </div>
    </div>
  );
}

/** The name is a button: click to rename in place, Enter saves (staged), Escape cancels. */
function Name({ s, eid, onService }: { s: Service; eid: string; onService: (s: Service) => void }) {
  const shown = s.staged?.name ?? s.config.name;
  const [editing, setEditing] = useState(false);
  const [name, setName] = useState(shown);
  const renamed = !!s.staged && s.staged.name !== s.config.name;
  const save = () => {
    setEditing(false);
    const n = name.trim();
    if (!n || n === shown) return;
    patch<Service>(`${svcPath(eid, s.id)}/info`, { name: n })
      .then((ns) => { onService(ns); toast("Rename staged. Deploy the changes to apply it.", "success"); })
      .catch((e) => toast(e instanceof Error ? e.message : String(e), "error"));
  };
  if (editing) {
    return (
      <input aria-label="Service name" autoFocus value={name} maxLength={32} onChange={(e) => setName(e.target.value)} onBlur={save}
        onFocus={(e) => e.currentTarget.select()}
        onKeyDown={(e) => { if (e.key === "Enter") save(); if (e.key === "Escape") { e.preventDefault(); setName(shown); setEditing(false); } }}
        className="-ml-2 h-[34px] min-w-0 rounded-md border border-accent bg-transparent px-2 text-h1 text-foreground outline-none focus-visible:shadow-none"
        style={{ width: `${Math.max(6, name.length + 2)}ch` }} />
    );
  }
  return (
    <button type="button" onClick={() => { setName(shown); setEditing(true); }} aria-label={`Rename ${shown}`}
      className={cx("-ml-2 min-w-0 truncate rounded-md px-2 text-left text-h1 transition-colors duration-150 hover:bg-surface-hover", renamed ? "text-accent-text" : "text-foreground")}>
      {shown}
    </button>
  );
}
