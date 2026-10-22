// SPDX-License-Identifier: AGPL-3.0-only

// Deployments tab (docs/SPEC.md 13.3).

import { useState } from "react";
import { ChevronDown, ChevronRight, GitCommit } from "lucide-react";
import type { Deployment, Service } from "../../api/types.gen";
import { get, post, svcPath } from "../service/client";
import { useEnvEvents } from "../service/events";
import { Button, Confirm, CopyButton, Empty, ErrorLine, LabelBadge, Loading, Menu, When, cx, useAction, useLoad } from "../service/ui";
import { ACTION_LABEL, actionsFor, inProgress, runAction, shortId, type DepAction } from "./actions";

const STEP_COLOR: Record<string, string> = { ok: "text-green-700", failed: "text-red-600", running: "text-blue-600", skipped: "text-gray-400", pending: "text-gray-400" };

export default function DeploymentsTab({ eid, service, onOpen }: {
  eid: string; service: Service; onOpen: (did: string) => void;
}) {
  const base = svcPath(eid, service.id);
  const deps = useLoad(() => get<Deployment[]>(`${base}/deployments?limit=50`), [base]);
  const [showSkipped, setShowSkipped] = useState(false);
  const [confirm, setConfirm] = useState<{ d: Deployment; a: DepAction } | null>(null);
  const act = useAction();
  useEnvEvents(eid, (e) => {
    if (e.type === "deployment" && e.deployment?.serviceId === service.id) {
      const nd = e.deployment;
      deps.setData((cur) => {
        if (!cur) return cur;
        const i = cur.findIndex((d) => d.id === nd.id);
        if (i < 0) return [nd, ...cur];
        const next = cur.slice();
        next[i] = nd;
        // Only one deployment is the active one.
        return nd.active ? next.map((d) => (d.id === nd.id ? d : { ...d, active: false })) : next;
      });
    }
  }, () => void deps.reload());

  const doAction = (d: Deployment, a: DepAction) => {
    if (a === "rollback" || a === "remove" || a === "abort") { setConfirm({ d, a }); return; }
    void act.run(async () => { await runAction(d.id, a); await deps.reload(); });
  };

  if (!deps.data) return deps.error ? <ErrorLine>{deps.error}</ErrorLine> : <Loading />;
  const list = deps.data;
  const active = list.find((d) => d.active);
  const running = list.filter((d) => inProgress(d));
  const history = list.filter((d) => d !== active && !running.includes(d) && (showSkipped || d.status !== "SKIPPED"));
  const skipped = list.filter((d) => d.status === "SKIPPED").length;
  const src = service.config.source;

  const deployLatest = () => act.run(async () => { await post<Deployment>(`${base}/deploy`, { ref: "latest" }); await deps.reload(); });

  return (
    <div>
      <div className="mb-4 flex justify-end gap-2">
        {(src.type === "github" || src.type === "image") && (
          <Button variant="primary" disabled={act.busy} onClick={() => void deployLatest()}>
            {src.type === "github" ? "Deploy latest commit" : "Deploy image"}
          </Button>
        )}
      </div>
      <ErrorLine>{act.error}</ErrorLine>

      {list.length === 0 ? (
        src.type === "empty" || !src.type ? (
          <Empty>
            <div className="mb-2 font-medium text-foreground">Connect a repo or image</div>
            Set a source in Settings, Source, or deploy this folder from your machine:
            <div className="mt-3 inline-flex items-center gap-2 rounded bg-gray-100 px-3 py-1 font-mono text-xs">5050 up --service {service.config.name}<CopyButton text={`5050 up --service ${service.config.name}`} /></div>
          </Empty>
        ) : (
          <Empty>
            No deployments yet.
            <div className="mt-3"><Button variant="primary" onClick={() => void deployLatest()}>Deploy now</Button></div>
          </Empty>
        )
      ) : null}

      {running.map((d) => <DepCard key={d.id} d={d} onOpen={onOpen} onAction={doAction} expandable />)}
      {active && <DepCard d={active} onOpen={onOpen} onAction={doAction} highlight />}

      {history.length > 0 && <h4 className="mb-2 mt-6 text-sm font-medium text-gray-600">History</h4>}
      {history.map((d) => <DepCard key={d.id} d={d} onOpen={onOpen} onAction={doAction} />)}
      {skipped > 0 && (
        <button className="mt-2 text-sm text-gray-600 underline" onClick={() => setShowSkipped((s) => !s)}>
          {showSkipped ? "Hide Skipped" : `Show Skipped (${skipped})`}
        </button>
      )}

      {confirm && (
        <Confirm
          title={`${ACTION_LABEL[confirm.a]} deployment`}
          body={confirm.a === "rollback" ? "Restores the image and the variables this deployment ran with." :
            confirm.a === "remove" ? "Stops this deployment. The service shows No deployment until you deploy again." :
            "Stops this deployment before it finishes. The current active deployment keeps serving."}
          confirm={ACTION_LABEL[confirm.a]} danger={confirm.a !== "rollback"}
          onClose={() => setConfirm(null)}
          onConfirm={async () => { await runAction(confirm.d.id, confirm.a); await deps.reload(); }} />
      )}
    </div>
  );
}

function DepCard({ d, onOpen, onAction, highlight, expandable }: {
  d: Deployment; onOpen: (did: string) => void; onAction: (d: Deployment, a: DepAction) => void; highlight?: boolean; expandable?: boolean;
}) {
  const [open, setOpen] = useState(!!expandable);
  const actions = actionsFor(d);
  return (
    <div className={cx("mb-2 rounded-lg border px-4 py-3", highlight ? "border-green-300" : "border-gray-200")} data-deployment-id={d.id}>
      <div className="flex items-center gap-3">
        {d.steps.length > 0 && (
          <button aria-label={open ? "Hide steps" : "Show steps"} onClick={() => setOpen((o) => !o)} className="text-gray-500">
            {open ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
          </button>
        )}
        <LabelBadge label={d.label} />
        <button className="min-w-0 flex-1 truncate text-left text-sm hover:underline" onClick={() => onOpen(d.id)}>
          {d.commit ? <><GitCommit size={13} className="mr-1 inline" />{d.commit.message.split("\n")[0]}</> : d.message || d.image || `Deployment ${shortId(d.id)}`}
        </button>
        <span className="hidden whitespace-nowrap text-xs text-gray-500 sm:inline">via {d.trigger}{d.actor ? ` by ${d.actor}` : ""} <When ts={d.createdAt} /></span>
        {d.status === "CRASHED" && <Button onClick={() => onAction(d, "restart")}>Restart</Button>}
        <Menu label={`Actions for deployment ${shortId(d.id)}`} items={[
          { label: "View logs", onClick: () => onOpen(d.id) },
          ...actions.map((a) => ({ label: ACTION_LABEL[a], danger: a === "remove" || a === "abort", onClick: () => onAction(d, a) })),
        ]} />
      </div>
      {d.diagnosis && <div className="mt-2 text-sm text-red-600">{d.diagnosis}</div>}
      {d.reason && <div className="mt-1 text-xs text-gray-500">{d.reason}</div>}
      {open && d.steps.length > 0 && (
        <ol className="mt-2 space-y-0.5 pl-7 text-xs">
          {d.steps.map((s) => (
            <li key={s.name} className={cx("flex gap-2", STEP_COLOR[s.state])}>
              <span className="w-24">{s.name}</span><span>{s.state}</span>
              {s.startedAt && s.finishedAt ? <span className="text-gray-500">{((s.finishedAt - s.startedAt) / 1000).toFixed(1)}s</span> : null}
              {s.detail && <span className="text-gray-500">{s.detail}</span>}
            </li>
          ))}
        </ol>
      )}
    </div>
  );
}
