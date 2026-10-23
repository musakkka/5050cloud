// SPDX-License-Identifier: AGPL-3.0-only

// Settings > Networking (docs/SPEC.md 13.3): public domains, TCP proxy,
// private domain, outbound IP, egress, metadata access. Domain and proxy
// changes are immediate; config fields are staged.

import { useState, type ReactNode } from "react";
import type { Canvas, Domain, Service, TCPProxy } from "../../api/types.gen";
import { del, get, patch, post, svcPath } from "../service/client";
import { Button, Confirm, CopyButton, ErrorLine, Input, Modal, cx, useAction, useLoad } from "../service/ui";
import type { FieldProps } from "./SettingsTab";

const DOMAIN_STATUS: Record<string, string> = {
  pending_dns: "Waiting for DNS", issuing: "Issuing certificate", active: "Active", error: "Error",
};

export default function Networking({ eid, service, field }: { eid: string; service: Service; field: (p: FieldProps) => ReactNode }) {
  const base = svcPath(eid, service.id);
  const domains = useLoad(() => get<Domain[]>(`${base}/domains`), [base], 15000);
  const env = useLoad(() => get<Canvas>(`/environments/${encodeURIComponent(eid)}/canvas`).then((c) => c.environment).catch(() => null), [eid]);
  const [proxies, setProxies] = useState<TCPProxy[]>(service.tcpProxies || []);
  const [gen, setGen] = useState(false);
  const [custom, setCustom] = useState(false);
  const [removing, setRemoving] = useState<Domain | null>(null);
  const [proxyPort, setProxyPort] = useState(String(service.config.networking.port || service.listeningPorts[0] || ""));
  const a = useAction();
  const defaultPort = service.config.networking.port || service.listeningPorts[0] || 8080;

  const updateDomain = (d: Domain) => domains.setData((cur) => cur?.map((x) => (x.id === d.id ? d : x)));

  return (
    <>
      <div>
        <div className="mb-2 flex items-center justify-between">
          <span className="text-sm font-medium">Public domains</span>
          <div className="flex gap-2">
            <Button onClick={() => setGen(true)}>Generate Domain</Button>
            <Button onClick={() => setCustom(true)}>Custom Domain</Button>
          </div>
        </div>
        {domains.error && <ErrorLine>{domains.error}</ErrorLine>}
        {(domains.data || []).length === 0 ? <div className="text-xs text-gray-500">No public domain. Generate one to reach this service from the internet.</div> : (
          <div className="divide-y divide-gray-200 rounded-md border border-gray-200">
            {(domains.data || []).map((d) => (
              <DomainRow key={d.id} d={d} onChange={updateDomain} onDelete={() => setRemoving(d)} />
            ))}
          </div>
        )}
      </div>

      <div>
        <div className="mb-2 text-sm font-medium">TCP Proxy</div>
        {proxies.map((p) => (
          <div key={p.id} className="mb-1 flex items-center gap-2 text-sm">
            <span className="font-mono">{p.address}</span><CopyButton text={p.address} />
            <span className="text-gray-500">to port {p.targetPort}</span>
            <Button variant="ghost" onClick={() => void a.run(async () => { await del(`${base}/tcp-proxies/${encodeURIComponent(p.id)}`); setProxies((c) => c.filter((x) => x.id !== p.id)); })}>Delete</Button>
          </div>
        ))}
        <div className="flex gap-2">
          <Input aria-label="TCP proxy target port" type="number" className="w-32" value={proxyPort} onChange={(e) => setProxyPort(e.target.value)} placeholder="port" />
          <Button disabled={!Number(proxyPort) || a.busy} onClick={() => void a.run(async () => {
            const p = await post<TCPProxy>(`${base}/tcp-proxies`, { targetPort: Number(proxyPort) });
            setProxies((c) => [...c, p]);
          })}>Add TCP Proxy</Button>
        </div>
      </div>

      {field({ label: "Target port", path: "networking.port", kind: "number", placeholder: String(service.listeningPorts[0] || 8080), help: service.listeningPorts.length ? `Listening on ${service.listeningPorts.join(", ")}` : undefined })}
      {field({ label: "Private domain", path: "networking.privateName", help: <>Reach it as <span className="font-mono">{service.privateDomain}</span> from other services <CopyButton text={service.privateDomain} /></> })}
      <div className="text-sm">
        <span className="font-medium">Outbound IP</span>
        <div className="text-gray-600">{service.outboundIps.length ? service.outboundIps.join(", ") : "Not running"}</div>
      </div>
      {env.data && (
        <div className="text-sm">
          <span className="font-medium">Egress</span>
          <div className="text-gray-600">{env.data.egress === "blocked" ? `Blocked for this environment${env.data.egressAllow?.length ? `, except ${env.data.egressAllow.join(", ")}` : ""}` : "Allowed (set per environment in Project Settings)"}</div>
        </div>
      )}
      {field({ label: "Allow metadata and host access", path: "networking.allowMetadata", kind: "toggle", help: "Admin only. Lets the service reach the cloud metadata endpoint and the hosts." })}
      <ErrorLine>{a.error}</ErrorLine>

      {gen && <DomainModal title="Generate Domain" defaultPort={defaultPort} onClose={() => setGen(false)}
        onCreate={async (_, port) => { const d = await post<Domain>(`${base}/domains`, { kind: "generated", port }); domains.setData((c) => [...(c || []), d]); }} />}
      {custom && <DomainModal title="Custom Domain" custom defaultPort={defaultPort} onClose={() => setCustom(false)}
        onCreate={async (domain, port) => { const d = await post<Domain>(`${base}/domains`, { kind: "custom", domain, port }); domains.setData((c) => [...(c || []), d]); }} />}
      {removing && <Confirm title="Delete domain" danger confirm="Delete" body={<>Stop serving <b>{removing.domain}</b>?</>}
        onClose={() => setRemoving(null)}
        onConfirm={async () => { await del(`/domains/${encodeURIComponent(removing.id)}`); domains.setData((c) => c?.filter((x) => x.id !== removing.id)); }} />}
    </>
  );
}

export function DomainRow({ d, onChange, onDelete }: { d: Domain; onChange: (d: Domain) => void; onDelete: () => void }) {
  const [port, setPort] = useState(String(d.port));
  const a = useAction();
  const call = (path: string) => a.run(async () => onChange(await post<Domain>(`/domains/${encodeURIComponent(d.id)}/${path}`)));
  return (
    <div className="px-3 py-2 text-sm">
      <div className="flex flex-wrap items-center gap-2">
        <a href={`https://${d.domain}`} target="_blank" rel="noreferrer" className="font-mono underline">{d.domain}</a>
        <CopyButton text={`https://${d.domain}`} />
        <span className={cx("text-xs", d.status === "active" ? "text-green-700" : d.status === "error" ? "text-red-600" : "text-yellow-700")}>{DOMAIN_STATUS[d.status] || d.status}</span>
        {d.certExpiresAt && <span className="text-xs text-gray-500">Certificate valid until {new Date(d.certExpiresAt).toLocaleDateString()}</span>}
        <div className="ml-auto flex items-center gap-1">
          <span className="text-xs text-gray-500">Port</span>
          <input aria-label={`Port for ${d.domain}`} value={port} onChange={(e) => setPort(e.target.value)} type="number"
            onBlur={() => { if (Number(port) && Number(port) !== d.port) void a.run(async () => onChange(await patch<Domain>(`/domains/${encodeURIComponent(d.id)}`, { port: Number(port) }))); }}
            className="h-7 w-20 rounded border border-gray-200 bg-background px-2 text-xs" />
          {d.kind === "custom" && <Button variant="ghost" onClick={() => void call("check")}>Check</Button>}
          {d.status === "error" || d.status === "issuing" ? <Button variant="ghost" onClick={() => void call("retry-certificate")}>Retry certificate</Button> : null}
          <Button variant="ghost" onClick={onDelete}>Delete</Button>
        </div>
      </div>
      {d.message && <div className={cx("mt-1 text-xs", d.status === "error" ? "text-red-600" : d.cloudflare ? "text-yellow-700" : "text-gray-600")}>{d.message}</div>}
      {d.kind === "custom" && d.dns && d.dns.length > 0 && !d.dnsOk && (
        <><div className="mt-2 text-xs text-gray-600">Add an A record to each address, or a CNAME where your DNS provider allows it. Use only one kind of record per name.</div>
        <table className="mt-1 w-full text-xs">
          <thead className="text-left text-gray-500"><tr><th className="py-1">Type</th><th>Host</th><th>Value</th></tr></thead>
          <tbody>
            {d.dns.map((r) => (
              <tr key={r.type + r.name + r.value} className="border-t border-gray-200">
                <td className="py-1">{r.type}</td>
                <td className="font-mono" title={r.name}>{r.host || r.name} <CopyButton text={r.host || r.name} /></td>
                <td className="font-mono">{r.value} <CopyButton text={r.value} />{r.note && <div className="font-sans text-yellow-700">{r.note}</div>}</td>
              </tr>
            ))}
          </tbody>
        </table></>
      )}
      <ErrorLine>{a.error}</ErrorLine>
    </div>
  );
}

function DomainModal({ title, custom, defaultPort, onCreate, onClose }: {
  title: string; custom?: boolean; defaultPort: number; onCreate: (domain: string, port: number) => Promise<void>; onClose: () => void;
}) {
  const [domain, setDomain] = useState("");
  const [port, setPort] = useState(String(defaultPort));
  const a = useAction();
  const wildcard = domain.includes("*");
  const ok = Number(port) > 0 && (!custom || (/^[a-z0-9.-]+\.[a-z]{2,}$/i.test(domain.trim()) && !wildcard));
  return (
    <Modal title={title} onClose={onClose} footer={<>
      <Button onClick={onClose}>Cancel</Button>
      <Button variant="primary" disabled={!ok || a.busy} onClick={async () => { if (await a.run(() => onCreate(domain.trim().toLowerCase(), Number(port)))) onClose(); }}>Add</Button>
    </>}>
      {custom && (
        <label className="mb-3 block">Domain<Input value={domain} onChange={(e) => setDomain(e.target.value)} placeholder="app.example.com" autoFocus />
          {wildcard && <span className="text-xs text-gray-500">Not in v0.1: wildcard domains. Add each name instead.</span>}
        </label>
      )}
      <label className="block">Port the app listens on<Input type="number" value={port} onChange={(e) => setPort(e.target.value)} /></label>
      <ErrorLine>{a.error}</ErrorLine>
    </Modal>
  );
}
