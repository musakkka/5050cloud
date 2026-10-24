// SPDX-License-Identifier: AGPL-3.0-only

// Database tab: sub-bar (Data, Stats, Config, Connect), the deployment checklist
// that gates the views, then the selected view. The sub-tab lives in ?state=.

import { useState } from "react";
import { useSearchParams } from "react-router-dom";
import { CheckCircle2, Loader2, Plug, XCircle } from "lucide-react";
import type { ConnectInfo, Service } from "../../api/types.gen";
import { BrandIcon, Button, ErrorLine, Notice, cx, toastError, toastSuccess } from "../../kit";
import { get, post, svcPath } from "../service/client";
import { useLoad } from "../service/ui";
import ConnectModal from "./ConnectModal";
import ConfigView from "./ConfigView";
import DataView from "./DataView";
import StatsView from "./StatsView";
import { engineOf } from "./engine";

type Sub = "data" | "stats" | "config";
const SUBS: { id: Sub; label: string }[] = [{ id: "data", label: "Data" }, { id: "stats", label: "Stats" }, { id: "config", label: "Config" }];

const BAD = new Set(["no_deployment", "crashed", "failed", "sleeping", "removed", "unhealthy", "host_lost"]);
const BUSY = new Set(["deploying", "building", "pending"]);

const BAD_TEXT: Record<string, string> = {
  no_deployment: "This service requires a deployment to be online.",
  crashed: "The database deployment has encountered an issue. It crashed and is not running.",
  failed: "The database deployment has encountered an issue. The last deployment failed.",
  unhealthy: "The database deployment has encountered an issue. It is running but failing its health check.",
  sleeping: "The database is asleep. Deploy it to wake it up.",
  removed: "The last deployment was removed. This service requires a deployment to be online.",
  host_lost: "The host that holds this database volume is gone. Restore it from a backup.",
};

export default function DataTab({ eid, service }: { eid: string; service: Service }) {
  const base = svcPath(eid, service.id);
  const [sp, setSp] = useSearchParams();
  const raw = sp.get("state");
  const sub: Sub = raw === "stats" || raw === "config" ? raw : "data";
  const [connect, setConnect] = useState(false);
  const engine = engineOf(service);
  const info = useLoad(() => get<ConnectInfo>(`${base}/connect-info`), [base, service.status], service.status === "online" ? 0 : 5000);
  const setSub = (s: Sub) => {
    const n = new URLSearchParams(sp);
    if (s === "data") n.delete("state"); else n.set("state", s);
    setSp(n, { replace: false });
  };
  const connectOk = !!info.data;

  return (
    <div>
      <div className="sticky -top-8 z-20 -mx-4 -mt-8 flex h-16 items-center justify-between bg-surface px-4 sm:-mx-12 sm:px-12">
        <div role="tablist" aria-label="Database views" className="flex gap-8">
          {SUBS.map((t) => (
            <button key={t.id} role="tab" aria-selected={sub === t.id} onClick={() => setSub(t.id)}
              className={cx("text-lead transition-colors duration-100", sub === t.id ? "text-foreground" : "text-fg-3 hover:text-foreground")}>
              {t.label}
            </button>
          ))}
        </div>
        <button type="button" aria-disabled={!connectOk} title={connectOk ? undefined : "Connection details are not available yet"}
          onClick={() => connectOk && setConnect(true)}
          className={cx("inline-flex items-center gap-2 text-body font-medium transition-colors", connectOk ? "text-accent-text hover:text-accent-muted" : "cursor-not-allowed text-fg-4")}>
          <Plug size={16} />Connect
        </button>
      </div>

      <div className="mx-auto max-w-[799px] pt-6 sm:px-2">
        {!engine ? (
          <Notice tone="warn">This database engine was not recognised, so the database view is unavailable. Use the CLI to connect.</Notice>
        ) : (
          <Gate service={service} base={base} info={info} onReload={() => info.reload()}>
            {sub === "data" && <DataView eid={eid} service={service} engine={engine} />}
            {sub === "stats" && <StatsView eid={eid} service={service} engine={engine} />}
            {sub === "config" && <ConfigView eid={eid} service={service} engine={engine} />}
          </Gate>
        )}
      </div>
      {connect && <ConnectModal base={base} onClose={() => setConnect(false)} />}
    </div>
  );
}

function Gate({ service, base, info, children }: {
  service: Service; base: string; info: ReturnType<typeof useLoad<ConnectInfo>>; onReload: () => void; children: React.ReactNode;
}) {
  const engine = engineOf(service)!;
  const st = service.status;
  const [busy, setBusy] = useState(false);
  const deploy = async () => {
    setBusy(true);
    try {
      toastSuccess("Deploying database");
      await post(`${base}/deploy`, {});
    } catch (e) { toastError(e); } finally { setBusy(false); }
  };
  if (st === "online") {
    if (info.error) {
      return (
        <Intro engine={engine}>
          <Row state="ok" title="Deployment Online" />
          <Row state="bad" title="Required Variables">
            <Inset>The connection variables for this database could not be read.</Inset>
            <ErrorLine error={info.error} className="mt-3" />
          </Row>
        </Intro>
      );
    }
    return <>{children}</>;
  }
  return (
    <Intro engine={engine}>
      {BUSY.has(st) ? (
        <Row state="busy" title={st === "building" ? "Building deployment" : "Deploying database"}>
          <p className="text-body text-fg-2">{service.statusText || "The database will be available when the deployment is online."}</p>
        </Row>
      ) : BAD.has(st) ? (
        <Row state="bad" title="Deployment Online">
          <Inset>{BAD_TEXT[st] ?? "This service requires a deployment to be online."}</Inset>
          <Button variant="accent-outline" className="mt-4" busy={busy} onClick={deploy}>{st === "no_deployment" ? "Deploy database" : "Redeploy database"}</Button>
        </Row>
      ) : (
        <Row state="busy" title="Checking deployment status..." />
      )}
      <Row state={info.data ? "ok" : info.error ? "bad" : "busy"} title="Required Variables">
        {info.data ? <p className="text-body text-fg-2">This service has all the necessary variables that this view uses to connect to the database.</p>
          : info.error ? <Inset>Some connection variables are missing. Check the Variables tab.</Inset> : null}
      </Row>
    </Intro>
  );
}

function Intro({ engine, children }: { engine: NonNullable<ReturnType<typeof engineOf>>; children: React.ReactNode }) {
  return (
    <div>
      <h2 className="flex items-center gap-3 text-h2 text-foreground" style={{ lineHeight: "27.5px" }}>
        <BrandIcon name={engine.brand} size={22} />{engine.label} DB
      </h2>
      <p className="mt-2 text-lead text-fg-2">A custom interface into your database</p>
      <ul className="mt-6 flex flex-col gap-4">{children}</ul>
    </div>
  );
}

function Row({ state, title, children }: { state: "ok" | "bad" | "busy"; title: string; children?: React.ReactNode }) {
  return (
    <li className="flex gap-4">
      <span className="mt-0.5 shrink-0">
        {state === "ok" ? <CheckCircle2 size={20} className="text-ok" /> : state === "bad" ? <XCircle size={20} className="text-danger" /> : <Loader2 size={20} className="spin text-fg-3" />}
      </span>
      <div className="min-w-0 flex-1">
        <div className="text-lead text-foreground" style={{ lineHeight: "24px" }}>{title}</div>
        {children && <div className="mt-2">{children}</div>}
      </div>
    </li>
  );
}

function Inset({ children }: { children: React.ReactNode }) {
  return <div className="rounded-md bg-surface p-3 text-body text-fg-2">{children}</div>;
}
