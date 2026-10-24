// SPDX-License-Identifier: AGPL-3.0-only

// Config sub-tab: engine summary, Postgres extensions (install), credentials.

import { useState } from "react";
import { Search } from "lucide-react";
import type { DataResponse, Extension, Service } from "../../api/types.gen";
import { Badge, Button, Confirm, Input, Skeleton, toastError, toastSuccess } from "../../kit";
import { get, post, svcPath } from "../service/client";
import { useLoad } from "../service/ui";
import type { Engine } from "./engine";
import { Panel } from "./parts";

export default function ConfigView({ eid, service, engine }: { eid: string; service: Service; engine: Engine }) {
  const base = svcPath(eid, service.id);
  const data = useLoad(() => get<DataResponse>(`${base}/data`), [base]);
  const [regen, setRegen] = useState(false);
  return (
    <div className="flex flex-col gap-6">
      <Panel title="Engine">
        <dl className="grid grid-cols-[140px_1fr] gap-y-2 px-4 text-body">
          <dt className="text-fg-3">Engine</dt><dd className="text-foreground">{engine.label}</dd>
          <dt className="text-fg-3">Version</dt><dd className="text-foreground tabular-nums">{data.data?.version ?? (data.error ? "unknown" : "...")}</dd>
          <dt className="text-fg-3">Port</dt><dd className="font-mono text-foreground">{engine.port}</dd>
          <dt className="text-fg-3">Private host</dt><dd className="font-mono text-foreground">{service.privateDomain}</dd>
        </dl>
      </Panel>
      {engine.key === "postgres" && <Extensions base={base} />}
      <Panel title="Credentials" hint="Make a new password. Services that reference this database redeploy with it." danger
        actions={<Button variant="danger-outline" onClick={() => setRegen(true)}>Regenerate credentials</Button>}>
        <div className="px-4 text-body text-fg-3">The old password stops working as soon as the new one is set.</div>
      </Panel>
      {regen && (
        <Confirm title="Regenerate credentials" danger confirm="Regenerate" typeName={service.config.name}
          body="The old password stops working. Services that use this database through references redeploy with the new one."
          onClose={() => setRegen(false)}
          onConfirm={async () => { try { await post(`${base}/regenerate-credentials`); toastSuccess("New credentials are set and dependent services are redeploying."); } catch (e) { toastError(e); throw e; } }} />
      )}
    </div>
  );
}

function Extensions({ base }: { base: string }) {
  const ext = useLoad(() => get<Extension[]>(`${base}/extensions`), [base]);
  const [q, setQ] = useState("");
  const [busy, setBusy] = useState<string | null>(null);
  const install = async (x: Extension) => {
    setBusy(x.name);
    try {
      const r = await post<Extension>(`${base}/extensions`, { name: x.name });
      ext.setData((cur) => cur?.map((e) => (e.name === r.name ? r : e)));
      toastSuccess("Extension installed");
    } catch (e) { toastError(e); } finally { setBusy(null); }
  };
  const rows = (ext.data || []).filter((x) => x.name.toLowerCase().includes(q.toLowerCase()));
  return (
    <Panel title="Extensions" hint="Install extensions that ship with this image."
      actions={<div className="w-[200px]"><Input size="sm" aria-label="Filter extensions" placeholder="Filter" icon={<Search size={14} />} value={q} onChange={(e) => setQ(e.target.value)} /></div>}>
      {ext.error ? <p className="px-4 text-body text-danger-text">{ext.error}</p> : !ext.data ? (
        <div className="flex flex-col gap-2 px-4"><Skeleton className="h-9" /><Skeleton className="h-9" /><Skeleton className="h-9" /></div>
      ) : rows.length === 0 ? <p className="px-4 text-body text-fg-3">No extensions match.</p> : (
        <div className="max-h-[420px] overflow-y-auto scroll-thin">
          {rows.map((x) => (
            <div key={x.name} className="flex items-center gap-3 border-t border-surface-hover px-4 py-2 first:border-t-0">
              <span className="w-40 shrink-0 truncate font-mono text-body text-foreground">{x.name}</span>
              <span className="min-w-0 flex-1 truncate text-caption text-fg-3" title={x.comment}>{x.version}{x.comment ? ` · ${x.comment}` : ""}</span>
              {x.installed ? <Badge tone="ok">Installed</Badge> : <Button size="xs" busy={busy === x.name} onClick={() => void install(x)}>Install</Button>}
            </div>
          ))}
        </div>
      )}
    </Panel>
  );
}
