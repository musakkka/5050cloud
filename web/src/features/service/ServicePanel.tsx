// SPDX-License-Identifier: AGPL-3.0-only

// Service panel (docs/SPEC.md 13.3): header and tabs. Routing lives in
// routes.tsx; this component takes explicit props so the shell can mount it.

import { lazy, Suspense, useState } from "react";
import { Database, Globe, Server, X } from "lucide-react";
import type { Service } from "../../api/types.gen";
import { get, patch, svcPath } from "./client";
import { useEnvEvents } from "./events";
import { CopyButton, ErrorLine, Loading, Tabs, cx, useAction, useLoad } from "./ui";
import DeploymentsTab from "../deployment/DeploymentsTab";
import VariablesTab from "../variables/VariablesTab";
import MetricsTab from "../metrics/MetricsTab";
import SettingsTab from "../service-settings/SettingsTab";
import BackupsTab from "../backups-tab/BackupsTab";
import DataTab from "../data/DataTab";
import CronRunsTab from "../cron/CronRunsTab";
import ConnectModal from "../data/ConnectModal";

const ShellTab = lazy(() => import("../shell/ShellTab"));

export type PanelTab = "deployments" | "variables" | "metrics" | "settings" | "backups" | "data" | "cron" | "shell";

export function panelTabs(s: Service): { id: PanelTab; label: string }[] {
  const db = s.kind === "database";
  return [
    { id: "deployments", label: "Deployments" },
    { id: "variables", label: "Variables" },
    { id: "metrics", label: "Metrics" },
    { id: "settings", label: "Settings" },
    ...(db || s.volume ? [{ id: "backups" as const, label: "Backups" }] : []),
    ...(db ? [{ id: "data" as const, label: "Data" }] : []),
    ...(s.kind === "cron" ? [{ id: "cron" as const, label: "Cron Runs" }] : []),
    { id: "shell", label: "Shell" },
  ];
}

export default function ServicePanel({ eid, sid, tab = "deployments", onTab, onClose, onOpenDeployment, onOpenService }: {
  eid: string;
  sid: string;
  tab?: PanelTab;
  onTab: (t: PanelTab) => void;
  onClose: () => void;
  onOpenDeployment: (did: string) => void;
  onOpenService?: (sid: string) => void;
}) {
  const base = svcPath(eid, sid);
  const svc = useLoad(() => get<Service>(base), [base]);
  const [connect, setConnect] = useState(false);
  useEnvEvents(eid, (e) => {
    if (e.serviceId === sid && e.service) svc.setData(e.service);
    else if (e.serviceId === sid && (e.type === "deployment" || e.type === "changes")) void svc.reload();
  }, () => void svc.reload());

  const s = svc.data;
  const tabs = s ? panelTabs(s) : [];
  const current = tabs.some((t) => t.id === tab) ? tab : "deployments";

  return (
    <div role="dialog" aria-label={s ? `Service ${s.config.name}` : "Service"}
      className="absolute inset-0 z-20 flex flex-col border-l border-gray-200 bg-secondaryBg shadow-xl sm:left-auto sm:w-[min(1040px,80%)] sm:rounded-l-xl">
      {!s ? (svc.error ? <div className="p-6"><ErrorLine>{svc.error}</ErrorLine></div> : <Loading />) : (
        <>
          <div className="px-4 pt-6 sm:px-10">
            <div className="flex items-start justify-between gap-3">
              <Header s={s} eid={eid} onService={svc.setData} onConnect={() => setConnect(true)} />
              <button aria-label="Close panel" onClick={onClose} className="mt-1 text-gray-500 hover:text-foreground"><X size={20} /></button>
            </div>
            <Tabs className="mt-6" tabs={tabs} value={current} onChange={(t) => onTab(t as PanelTab)} />
          </div>
          <div className="min-h-0 flex-1 overflow-y-auto px-4 py-6 sm:px-10">
            {current === "deployments" && <DeploymentsTab eid={eid} service={s} onOpen={onOpenDeployment} />}
            {current === "variables" && <VariablesTab eid={eid} service={s} />}
            {current === "metrics" && <MetricsTab eid={eid} service={s} />}
            {current === "settings" && <SettingsTab eid={eid} service={s} onService={svc.setData} onDeleted={() => void svc.reload()} />}
            {current === "backups" && <BackupsTab eid={eid} service={s} onOpenService={onOpenService} />}
            {current === "data" && <DataTab eid={eid} service={s} />}
            {current === "cron" && <CronRunsTab eid={eid} service={s} />}
            {current === "shell" && <Suspense fallback={<Loading />}><ShellTab eid={eid} service={s} /></Suspense>}
          </div>
          {connect && <ConnectModal base={base} onClose={() => setConnect(false)} />}
        </>
      )}
    </div>
  );
}

function Header({ s, eid, onService, onConnect }: { s: Service; eid: string; onService: (s: Service) => void; onConnect: () => void }) {
  const [editing, setEditing] = useState(false);
  const shownName = s.staged?.name ?? s.config.name;
  const [name, setName] = useState(shownName);
  const a = useAction();
  const domain = s.domains.find((d) => d.status === "active") || s.domains[0];
  const hosts = [...new Set(s.replicas.map((r) => r.hostName))];
  const renamed = s.staged && s.staged.name !== s.config.name;
  const save = () => {
    setEditing(false);
    if (name.trim() && name.trim() !== shownName) {
      void a.run(async () => onService(await patch<Service>(`${svcPath(eid, s.id)}/info`, { name: name.trim() })));
    }
  };
  return (
    <div className="min-w-0">
      <div className="flex items-center gap-3">
        {s.kind === "database" ? <Database size={28} /> : <Server size={28} />}
        {editing ? (
          <input aria-label="Service name" autoFocus value={name} onChange={(e) => setName(e.target.value)} onBlur={save}
            onKeyDown={(e) => { if (e.key === "Enter") save(); if (e.key === "Escape") { setName(shownName); setEditing(false); } }}
            className="h-9 rounded-md border border-gray-200 bg-background px-2 text-2xl font-semibold outline-none" />
        ) : (
          <button title="Rename" onClick={() => { setName(shownName); setEditing(true); }}
            className={cx("truncate text-2xl font-semibold sm:text-[28px]", renamed && "text-purple-500")}>{shownName}</button>
        )}
        {s.kind === "database" && (
          <button onClick={onConnect} className="rounded-md border border-gray-200 px-3 py-1 text-sm hover:border-gray-400">Connect</button>
        )}
      </div>
      <div className="mt-2 flex flex-wrap items-center gap-x-4 gap-y-1 text-sm text-gray-600">
        {domain && <span className="flex items-center gap-1"><Globe size={13} /><a className="hover:underline" href={`https://${domain.domain}`} target="_blank" rel="noreferrer">{domain.domain}</a><CopyButton text={`https://${domain.domain}`} /></span>}
        <span className="flex items-center gap-1 font-mono text-xs">{s.privateDomain}<CopyButton text={s.privateDomain} label="Copy private URL" /></span>
        <span>{s.replicas.length} replica{s.replicas.length === 1 ? "" : "s"}{hosts.length ? ` on ${hosts.join(", ")}` : ""}</span>
        <span>{s.statusText}</span>
      </div>
      {s.replicas.filter((r) => r.note).map((r) => <div key={r.id} className="mt-1 text-xs text-yellow-700">{r.note}</div>)}
      <ErrorLine>{a.error}</ErrorLine>
    </div>
  );
}
