// SPDX-License-Identifier: AGPL-3.0-only

// Deployment view (docs/SPEC.md 13.5): Details, Build Logs, Deploy Logs,
// Network (HTTP).

import { useEffect, useState } from "react";
import { ArrowLeft, FileCode } from "lucide-react";
import type { DeploymentDetail } from "../../api/types.gen";
import { get } from "../service/client";
import { useEnvEvents } from "../service/events";
import { Button, Confirm, CopyButton, ErrorLine, LabelBadge, Loading, Menu, Tabs, When, useAction, useLoad } from "../service/ui";
import LogStream from "../logs/LogStream";
import { ACTION_LABEL, actionsFor, defaultTab, runAction, shortId, type DepAction } from "./actions";

export type DeploymentTab = "details" | "build" | "deploy" | "network";

export default function DeploymentView({ did, serviceName, tab, onTab, onBack, onOpen }: {
  did: string;
  serviceName?: string;
  tab?: DeploymentTab;
  onTab?: (t: DeploymentTab) => void;
  onBack: () => void;
  /** Open another deployment (after redeploy or rollback). */
  onOpen?: (did: string) => void;
}) {
  const dep = useLoad(() => get<DeploymentDetail>(`/deployments/${encodeURIComponent(did)}`), [did]);
  const [localTab, setLocalTab] = useState<DeploymentTab | undefined>(tab);
  const [confirm, setConfirm] = useState<DepAction | null>(null);
  const act = useAction();
  useEnvEvents(dep.data?.environmentId, (e) => {
    if (e.type === "deployment" && e.deployment?.id === did) void dep.reload();
  });
  useEffect(() => setLocalTab(tab), [tab]);
  const d = dep.data;
  const current: DeploymentTab = localTab || (d ? defaultTab(d) : "deploy");
  const setTab = (t: DeploymentTab) => { setLocalTab(t); onTab?.(t); };

  const doAction = async (a: DepAction) => {
    const nd = await runAction(did, a);
    if (nd && nd.id !== did && onOpen) onOpen(nd.id); else await dep.reload();
  };

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="flex flex-wrap items-center gap-3 border-b border-gray-200 pb-4">
        <button aria-label="Back" onClick={onBack} className="text-gray-500 hover:text-foreground"><ArrowLeft size={18} /></button>
        <h2 className="text-lg font-semibold">{serviceName ? `${serviceName} / ` : ""}{shortId(did)}</h2>
        <CopyButton text={did} label="Copy id" />
        {d && <LabelBadge label={d.label} />}
        {d?.commit && <span className="truncate text-sm text-gray-600" title={d.commit.message}>{d.commit.sha.slice(0, 7)} {d.commit.message.split("\n")[0]}</span>}
        {d && <span className="text-xs text-gray-500">via {d.trigger} <When ts={d.createdAt} /></span>}
        <div className="ml-auto">
          {d && <Menu label="Deployment actions" items={[
            ...actionsFor(d).map((a) => ({
              label: ACTION_LABEL[a], danger: a === "remove" || a === "abort",
              onClick: () => (a === "rollback" || a === "remove" || a === "abort" ? setConfirm(a) : void act.run(() => doAction(a))),
            })),
            { label: "Copy id", onClick: () => void navigator.clipboard?.writeText(did) },
          ]} />}
        </div>
      </div>
      {d?.diagnosis && <div className="mt-3 text-sm text-red-600">{d.diagnosis}</div>}
      <ErrorLine>{act.error || dep.error}</ErrorLine>
      <Tabs className="mt-4" value={current} onChange={(t) => setTab(t as DeploymentTab)} tabs={[
        { id: "details", label: "Details" }, { id: "build", label: "Build Logs" }, { id: "deploy", label: "Deploy Logs" }, { id: "network", label: "Network" },
      ]} />
      <div className="mt-4 min-h-0 flex-1">
        {!d ? <Loading /> :
          current === "details" ? <Details d={d} /> :
          current === "build" ? <LogStream source={{ kind: "build", deployment: did }} /> :
          current === "deploy" ? <LogStream source={{ kind: "logs", environment: d.environmentId, deployment: did, type: "deploy" }} /> :
          <div className="flex h-full flex-col">
            <div className="mb-2 text-sm font-medium">HTTP</div>
            <div className="min-h-0 flex-1"><LogStream http source={{ kind: "logs", environment: d.environmentId, deployment: did, type: "http" }} /></div>
          </div>}
      </div>
      {confirm && (
        <Confirm title={`${ACTION_LABEL[confirm]} deployment`} danger={confirm !== "rollback"} confirm={ACTION_LABEL[confirm]}
          body={confirm === "rollback" ? "Restores the image and the variables this deployment ran with." :
            confirm === "remove" ? "Stops this deployment. The service shows No deployment until you deploy again." :
            "Stops this deployment before it finishes."}
          onClose={() => setConfirm(null)} onConfirm={() => doAction(confirm)} />
      )}
    </div>
  );
}

function Row({ k, children }: { k: string; children: React.ReactNode }) {
  return (
    <div className="flex gap-4 border-b border-gray-200 py-2 text-sm last:border-0">
      <div className="w-40 shrink-0 text-gray-500">{k}</div>
      <div className="min-w-0 flex-1 break-all">{children}</div>
    </div>
  );
}

function flat(obj: unknown, prefix = ""): [string, string][] {
  if (obj === null || obj === undefined) return [];
  if (typeof obj !== "object" || Array.isArray(obj)) return [[prefix, Array.isArray(obj) ? obj.join(", ") : String(obj)]];
  return Object.entries(obj as Record<string, unknown>).flatMap(([k, v]) => flat(v, prefix ? `${prefix}.${k}` : k));
}

function Details({ d }: { d: DeploymentDetail }) {
  const cfg = flat(d.config).filter(([, v]) => v !== "" && v !== undefined);
  return (
    <div className="overflow-y-auto">
      {d.notices?.map((n) => <div key={n} className="mb-2 rounded-md border border-yellow-300 bg-yellow-100 px-3 py-1.5 text-xs text-yellow-700">{n}</div>)}
      <Row k="Status">{d.label}{d.reason ? `: ${d.reason}` : ""}</Row>
      {d.image && <Row k="Image"><span className="font-mono">{d.image}</span> <CopyButton text={d.image} /></Row>}
      {d.digest && <Row k="Digest"><span className="font-mono">{d.digest}</span> <CopyButton text={d.digest} /></Row>}
      {d.commit && <Row k="Commit">{d.commit.url ? <a className="underline" href={d.commit.url} target="_blank" rel="noreferrer">{d.commit.sha.slice(0, 7)}</a> : d.commit.sha.slice(0, 7)} {d.commit.message} ({d.commit.author})</Row>}
      {d.rollbackOf && <Row k="Rollback of"><span className="font-mono">{shortId(d.rollbackOf)}</span></Row>}
      {d.message && <Row k="Message">{d.message}</Row>}
      <Row k="Replicas">{d.replicas.length ? d.replicas.map((r) => `${r.hostName}: ${r.state}${r.restarts ? ` (${r.restarts} restarts)` : ""}`).join(", ") : "None"}</Row>
      <Row k="Variables">{d.variableNames.length ? <span className="font-mono">{d.variableNames.join(", ")}</span> : "None"}</Row>
      <h4 className="mb-1 mt-6 text-sm font-medium">Config{d.configFile ? ` (config file ${d.configFile.path})` : ""}</h4>
      {cfg.map(([k, v]) => (
        <Row key={k} k={k}>
          <span className="font-mono">{v}</span>
          {d.source?.[k] && <span title={`Set by ${d.source[k]}`} className="ml-2 inline-flex items-center gap-1 text-xs text-gray-500"><FileCode size={12} />{d.source[k]}</span>}
        </Row>
      ))}
      <div className="mt-4"><Button onClick={() => void navigator.clipboard?.writeText(JSON.stringify(d.config, null, 2))}>Copy config</Button></div>
    </div>
  );
}
