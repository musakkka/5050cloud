// SPDX-License-Identifier: AGPL-3.0-only

// Settings > Networking (docs/SPEC.md 13.3): public domains with status,
// DNS records and certificate state, TCP proxies, the private domain,
// outbound IP and egress, metadata access, and for databases the public
// access switch. Domain and proxy changes are immediate; config fields are
// staged.

import { useState, type ReactNode } from "react";
import {
  AlertTriangle, ArrowRight, ChevronRight, CircleCheck, Clock, Cloud, CloudAlert, Copy, Check, Globe, Info, Pencil, Plus, RotateCw, Terminal, Trash2, Waypoints, Zap,
} from "lucide-react";
import type { Canvas, Domain, TCPProxy } from "../../api/types.gen";
import { del, get, patch, post } from "../service/client";
import { useLoad } from "../service/ui";
import { Button, Confirm, IconButton, Tooltip, cx, toastError } from "../../kit";
import { isDatabase, useFieldState, useSettings } from "./context";
import { Code, InlinePanel, PANEL_INPUT, Pill, SettingField, ToggleRow } from "./ui";

const DOMAIN_STATUS: Record<string, string> = {
  pending_dns: "Waiting for DNS", issuing: "Issuing certificate", active: "Active", error: "Error",
};

export default function Networking() {
  const { svc } = useSettings();
  const db = isDatabase(svc);
  return (
    <>
      {db ? <PublicAccess /> : <PublicNetworking />}
      <PrivateNetworking />
      {db && <ConnectPrivately />}
      <Outbound />
      <MetadataAccess />
    </>
  );
}

/* ---------- public domains ---------- */

type Adding = null | "generate" | "custom" | "tcp";

function PublicNetworking() {
  const { svc, base } = useSettings();
  const domains = useLoad(() => get<Domain[]>(`${base}/domains`), [base], 15000);
  const [proxies, setProxies] = useState<TCPProxy[]>(svc.tcpProxies || []);
  const [adding, setAdding] = useState<Adding>(null);
  const [removing, setRemoving] = useState<Domain | null>(null);
  const [removingProxy, setRemovingProxy] = useState<TCPProxy | null>(null);
  const defaultPort = svc.config.networking.port || svc.listeningPorts[0] || 8080;
  const list = domains.data ?? svc.domains ?? [];
  const update = (d: Domain) => domains.setData((cur) => cur?.map((x) => (x.id === d.id ? d : x)));

  return (
    <SettingField title="Public Networking" keywords="public domain custom domain generate tcp proxy port dns certificate https"
      description="Reach this service from the internet over HTTPS on these domains.">
      <div className="flex flex-col gap-2">
        {list.map((d) => <DomainRow key={d.id} d={d} onChange={update} onDelete={() => setRemoving(d)} />)}
        {proxies.map((p) => <ProxyRow key={p.id} p={p} onDelete={() => setRemovingProxy(p)} />)}
        {domains.error && <div role="alert" className="text-sm text-danger-text">{domains.error}</div>}
        {adding === "generate" && (
          <PortPanel title="Generate a domain" submit="Generate Domain" initial={String(defaultPort)} onCancel={() => setAdding(null)}
            hint="The port your app listens on"
            onSubmit={async (port) => { const d = await post<Domain>(`${base}/domains`, { kind: "generated", port }); domains.setData((c) => [...(c || []), d]); setAdding(null); }} />
        )}
        {adding === "custom" && <CustomDomainPanel defaultPort={defaultPort} onCancel={() => setAdding(null)}
          onCreate={async (domain, port) => { const d = await post<Domain>(`${base}/domains`, { kind: "custom", domain, port }); domains.setData((c) => [...(c || []), d]); setAdding(null); }} />}
        {adding === "tcp" && (
          <PortPanel title="Add TCP Proxy" submit="Add Proxy" placeholder={String(svc.listeningPorts[0] || 5432)} onCancel={() => setAdding(null)}
            hint="The port your app listens on for raw TCP"
            onSubmit={async (port) => { const p = await post<TCPProxy>(`${base}/tcp-proxies`, { targetPort: port }); setProxies((c) => [...c, p]); setAdding(null); }} />
        )}
      </div>
      {adding === null && (
        <div className={cx("flex flex-wrap gap-2", (list.length > 0 || proxies.length > 0) && "mt-4")}>
          <Button variant="accent-outline" icon={<Zap size={16} />} onClick={() => setAdding("generate")}>Generate Domain</Button>
          <Button variant="accent-outline" icon={<Plus size={16} />} onClick={() => setAdding("custom")}>Custom Domain</Button>
          <Button variant="accent-outline" icon={<Plus size={16} />} onClick={() => setAdding("tcp")}>TCP Proxy</Button>
        </div>
      )}
      {removing && (
        <Confirm title="Remove domain" danger confirm="Delete"
          body={<>Stop serving this service on <b className="text-foreground">{removing.domain}</b>? Visitors get an error until you add it again.</>}
          onClose={() => setRemoving(null)}
          onConfirm={async () => { await del(`/domains/${encodeURIComponent(removing.id)}`); domains.setData((c) => c?.filter((x) => x.id !== removing.id)); }} />
      )}
      {removingProxy && (
        <Confirm title="Remove TCP proxy" danger confirm="Delete"
          body={<>Close <b className="font-mono text-foreground">{removingProxy.address}</b>? Clients using it lose their connection.</>}
          onClose={() => setRemovingProxy(null)}
          onConfirm={async () => { await del(`${base}/tcp-proxies/${encodeURIComponent(removingProxy.id)}`); setProxies((c) => c.filter((x) => x.id !== removingProxy.id)); }} />
      )}
    </SettingField>
  );
}

function StatusIcon({ d }: { d: Domain }) {
  const icon = d.status === "active" ? <Cloud size={20} className="text-ok-text" />
    : d.status === "error" ? <CloudAlert size={20} className="text-danger-text" />
    : <Clock size={20} className="text-warn" />;
  return <span role="img" aria-label={DOMAIN_STATUS[d.status] || d.status} className="inline-flex shrink-0">{icon}</span>;
}

/** Hover-revealed 26px icon actions at the top right of a card. */
function CardActions({ children }: { children: ReactNode }) {
  return <div className="flex shrink-0 items-center gap-1 opacity-0 transition-opacity duration-150 focus-within:opacity-100 group-hover:opacity-100">{children}</div>;
}

function CopyIcon({ text, label = "Copy" }: { text: string; label?: string }) {
  const [done, setDone] = useState(false);
  return (
    <IconButton label={label} size={26} onClick={() => { void navigator.clipboard?.writeText(text).then(() => { setDone(true); setTimeout(() => setDone(false), 1200); }); }}>
      {done ? <Check size={15} className="text-ok-text" /> : <Copy size={15} />}
    </IconButton>
  );
}

/** One public domain: status icon, name, port and state line; copy, edit port and delete on hover. */
export function DomainRow({ d, onChange, onDelete }: { d: Domain; onChange: (d: Domain) => void; onDelete: () => void }) {
  const [editing, setEditing] = useState(false);
  const [busy, setBusy] = useState<string | null>(null);
  const call = async (path: string) => {
    setBusy(path);
    try { onChange(await post<Domain>(`/domains/${encodeURIComponent(d.id)}/${path}`)); } catch (e) { toastError(e); } finally { setBusy(null); }
  };
  const showRecords = d.kind === "custom" && !!d.dns?.length && !d.dnsOk;
  const retry = d.status === "error" || d.status === "issuing";
  if (editing) {
    return (
      <PortPanel title={`Update target port for ${d.domain}`} submit="Update" initial={String(d.port)} hint="Update your target port"
        onCancel={() => setEditing(false)}
        onSubmit={async (port) => { onChange(await patch<Domain>(`/domains/${encodeURIComponent(d.id)}`, { port })); setEditing(false); }} />
    );
  }
  return (
    <div className="group rounded-lg border border-line transition-colors duration-150 hover:border-fg-5">
      <div className="flex min-h-[74px] items-center gap-4 px-4 py-3">
        <StatusIcon d={d} />
        <div className="min-w-0 flex-1">
          <a href={`https://${d.domain}`} target="_blank" rel="noreferrer" className="block truncate text-sm text-foreground hover:underline">{d.domain}</a>
          <div className="mt-1 flex flex-wrap items-center gap-x-1 text-caption font-medium text-fg-2">
            <ArrowRight size={13} className="shrink-0" />
            <span>Port <span className="font-mono">{d.port}</span></span>
            {d.status !== "active" && (
              <><span>·</span><span className={d.status === "error" ? "text-red-600" : "text-warn"}>{DOMAIN_STATUS[d.status] || d.status}</span></>
            )}
            {d.status === "active" && d.certExpiresAt && (
              <><span>·</span><Tooltip content={`Certificate valid until ${new Date(d.certExpiresAt).toLocaleDateString()}`}><span tabIndex={0} className="text-pink-600">Certificate valid</span></Tooltip></>
            )}
            {d.cloudflare && <><span>·</span><Tooltip content={d.message || "Keep SSL/TLS on Full (strict)"}><span tabIndex={0} className="text-pink-600 underline-offset-2 hover:underline">Cloudflare proxy detected</span></Tooltip></>}
          </div>
        </div>
        <CardActions>
          <CopyIcon text={`https://${d.domain}`} />
          <IconButton label={`Edit port for ${d.domain}`} title="Edit port" size={26} onClick={() => setEditing(true)}><Pencil size={15} /></IconButton>
          <IconButton label={`Delete ${d.domain}`} title="Delete" size={26} onClick={onDelete}><Trash2 size={15} /></IconButton>
        </CardActions>
      </div>
      {(d.message && !d.cloudflare) || showRecords || retry || d.kind === "custom" && d.status !== "active" ? (
        <div className="border-t border-line px-4 py-3 text-caption">
          {d.message && !d.cloudflare && <div className={cx("mb-2", d.status === "error" ? "text-red-600" : "text-fg-2")}>{d.message}</div>}
          {showRecords && (
            <>
              <div className="mb-2 text-fg-2">Add an A record to each address, or a CNAME where your DNS provider allows it. Use one kind of record per name.</div>
              <div className="overflow-hidden rounded-md border border-line">
                <div className="grid grid-cols-[70px_1fr_2fr] bg-tint-header px-3 py-1.5 font-medium text-fg-3"><span>Type</span><span>Name</span><span>Value</span></div>
                {d.dns!.map((r) => (
                  <div key={r.type + r.name + r.value} className="grid grid-cols-[70px_1fr_2fr] items-start border-t border-line px-3 py-1.5 text-fg-2">
                    <span>{r.type}</span>
                    <span className="flex min-w-0 items-center gap-1 font-mono" title={r.name}><span className="truncate">{r.host || r.name}</span> <InlineCopy text={r.host || r.name} /></span>
                    <span className="min-w-0 font-mono">
                      <span className="flex items-center gap-1"><span className="truncate">{r.value}</span> <InlineCopy text={r.value} /></span>
                      {r.note && <span className="block font-sans text-warn">{r.note}</span>}
                    </span>
                  </div>
                ))}
              </div>
            </>
          )}
          <div className={cx("flex flex-wrap gap-2", (showRecords || (d.message && !d.cloudflare)) && "mt-2")}>
            {d.kind === "custom" && d.status !== "active" && <Button size="xs" busy={busy === "check"} icon={<RotateCw size={12} />} onClick={() => void call("check")}>Check DNS</Button>}
            {retry && <Button size="xs" busy={busy === "retry-certificate"} onClick={() => void call("retry-certificate")}>Retry certificate</Button>}
          </div>
        </div>
      ) : null}
    </div>
  );
}

function InlineCopy({ text }: { text: string }) {
  const [done, setDone] = useState(false);
  return (
    <button type="button" aria-label="Copy" title="Copy" className="inline-flex h-5 w-5 shrink-0 items-center justify-center rounded text-fg-3 transition-colors hover:bg-surface-hover hover:text-foreground"
      onClick={() => { void navigator.clipboard?.writeText(text).then(() => { setDone(true); setTimeout(() => setDone(false), 1200); }); }}>
      {done ? <Check size={12} className="text-ok-text" /> : <Copy size={12} />}
    </button>
  );
}

function ProxyRow({ p, onDelete }: { p: TCPProxy; onDelete: () => void }) {
  return (
    <div className="group flex min-h-[74px] items-center gap-4 rounded-lg border border-line px-4 py-3 transition-colors duration-150 hover:border-fg-5">
      <Waypoints size={20} className="shrink-0 text-ok-text" />
      <div className="min-w-0 flex-1">
        <div className="truncate font-mono text-sm text-foreground">{p.address}</div>
        <div className="mt-1 flex items-center gap-1 text-caption font-medium text-fg-2"><ArrowRight size={13} />TCP to port <span className="font-mono">{p.targetPort}</span></div>
      </div>
      <CardActions>
        <CopyIcon text={p.address} />
        <IconButton label="Delete proxy" size={26} onClick={onDelete}><Trash2 size={15} /></IconButton>
      </CardActions>
    </div>
  );
}

const validPort = (s: string) => /^\d+$/.test(s) && Number(s) >= 1 && Number(s) <= 65535;

function PortPanel({ title, submit, initial = "", placeholder = "8080", hint, onSubmit, onCancel }: {
  title: string; submit: string; initial?: string; placeholder?: string; hint: string; onSubmit: (port: number) => Promise<void>; onCancel: () => void;
}) {
  const [port, setPort] = useState(initial);
  const [busy, setBusy] = useState(false);
  return (
    <InlinePanel title={title} submit={submit} valid={validPort(port)} busy={busy} onCancel={onCancel}
      hint={<><Info size={16} className="shrink-0" />{hint}</>}
      onSubmit={async () => { setBusy(true); try { await onSubmit(Number(port)); } catch (e) { toastError(e); } finally { setBusy(false); } }}>
      <input autoFocus aria-label="Port" inputMode="numeric" value={port} placeholder={placeholder} onChange={(e) => setPort(e.target.value.replace(/\D/g, ""))} className={cx(PANEL_INPUT, "font-mono")} />
    </InlinePanel>
  );
}

function CustomDomainPanel({ defaultPort, onCreate, onCancel }: { defaultPort: number; onCreate: (domain: string, port: number) => Promise<void>; onCancel: () => void }) {
  const [domain, setDomain] = useState("");
  const [port, setPort] = useState("");
  const [busy, setBusy] = useState(false);
  const d = domain.trim().toLowerCase();
  const wildcard = d.includes("*");
  const okDomain = /^[a-z0-9-]+(\.[a-z0-9-]+)*\.[a-z]{2,}$/.test(d);
  const p = port || String(defaultPort);
  const hint = wildcard ? "Not in v0.1: wildcard domains. Add each name instead." : !d ? "Enter your custom domain" : !okDomain ? "That does not look like a domain name" : `You will get DNS records for ${d}`;
  return (
    <InlinePanel title="Add Custom Domain" submit="Add Domain" valid={okDomain && !wildcard && validPort(p)} busy={busy} onCancel={onCancel}
      hint={<><Info size={16} className="shrink-0" /><span className={cx("truncate", (wildcard || (d && !okDomain)) && "text-warn")}>{hint}</span></>}
      onSubmit={async () => { setBusy(true); try { await onCreate(d, Number(p)); } catch (e) { toastError(e); } finally { setBusy(false); } }}>
      <input autoFocus aria-label="Domain" value={domain} placeholder="example.com" onChange={(e) => setDomain(e.target.value)} className={PANEL_INPUT} />
      <div>
        <div className="mb-2 text-sm text-fg-2">Enter the port your app is listening on</div>
        <input aria-label="Port" inputMode="numeric" value={port} placeholder={String(defaultPort)} onChange={(e) => setPort(e.target.value.replace(/\D/g, ""))} className={cx(PANEL_INPUT, "font-mono")} />
      </div>
    </InlinePanel>
  );
}

/* ---------- databases: public access ---------- */

function PublicAccess() {
  const { svc, cfg, stage } = useSettings();
  const st = useFieldState("networking.publicAccess");
  const on = !!cfg.networking.publicAccess;
  const live = svc.tcpProxies[0];
  return (
    <SettingField title="Public Access" keywords="public access tcp proxy database url internet" staged={st.staged} locked={st.locked}
      description={<>Adds a public host and port and sets <Code>DATABASE_PUBLIC_URL</Code></>}>
      <div className="overflow-hidden rounded-md border border-line text-sm">
        <div className="flex items-center gap-3 px-4 py-2.5 text-red-600"><AlertTriangle size={16} className="shrink-0" />Anyone with the URL can connect. Use a strong password and TLS.</div>
        <div className="flex items-center gap-3 border-t border-line px-4 py-2.5 text-fg-2"><Info size={16} className="shrink-0" />Public traffic counts against your hosts' bandwidth.</div>
      </div>
      {live && on && (
        <div className="group mt-3 flex items-center gap-3 rounded-md bg-surface-hover px-4 py-3">
          <Waypoints size={18} className="text-ok-text" />
          <span className="min-w-0 flex-1 truncate font-mono text-sm text-foreground">{live.address}</span>
          <CopyIcon text={live.address} />
        </div>
      )}
      <div className="mt-4">
        {on ? <Button variant="danger-outline" onClick={() => void stage("networking.publicAccess", false)}>Remove Public Access</Button>
          : <Button variant="accent-outline" icon={<Globe size={16} />} onClick={() => void stage("networking.publicAccess", true)}>Add Public Access</Button>}
      </div>
    </SettingField>
  );
}

function ConnectPrivately() {
  const { svc } = useSettings();
  const name = svc.config.name;
  const [open, setOpen] = useState<string | null>(null);
  const cards = [
    {
      id: "machine", icon: <Terminal size={16} />, title: "From your machine",
      body: <>Open a client through an encrypted tunnel with the CLI. Nothing is exposed to the internet.<pre className="mt-2 overflow-x-auto rounded bg-secondaryBg px-3 py-2 font-mono text-caption text-foreground">5050 connect {name.includes(" ") ? `"${name}"` : name}</pre></>,
    },
    {
      id: "service", icon: <Waypoints size={16} />, title: "From another service",
      body: <>Reference this database's private URL in the other service's variables.<pre className="mt-2 overflow-x-auto rounded bg-secondaryBg px-3 py-2 font-mono text-caption text-foreground">{"DATABASE_URL=${{" + name + ".DATABASE_URL}}"}</pre></>,
    },
  ];
  return (
    <SettingField title="Connect without exposing the database" keywords="connect tunnel cli private url">
      <div className="flex flex-col gap-2">
        {cards.map((c) => (
          <div key={c.id} className="rounded-md border border-line">
            <button type="button" aria-expanded={open === c.id} onClick={() => setOpen(open === c.id ? null : c.id)}
              className="flex h-11 w-full items-center gap-3 px-4 text-left text-base text-foreground transition-colors hover:bg-surface-hover">
              <span className="text-fg-3">{c.icon}</span>
              <span className="flex-1">{c.title}</span>
              <ChevronRight size={16} className={cx("text-fg-3 transition-transform duration-200", open === c.id && "rotate-90")} />
            </button>
            {open === c.id && <div className="animate-collapsible-down px-4 pb-4 text-sm text-fg-2">{c.body}</div>}
          </div>
        ))}
      </div>
    </SettingField>
  );
}

/* ---------- private networking ---------- */

function PrivateNetworking() {
  const { svc, cfg, stage } = useSettings();
  const st = useFieldState("networking.privateName");
  const [editing, setEditing] = useState(false);
  const short = cfg.networking.privateName || svc.privateDomain.replace(/\.internal$/, "");
  const host = st.staged ? `${short}.internal` : svc.privateDomain;
  return (
    <SettingField title="Private Networking" keywords="private networking internal dns host name service discovery" staged={st.staged} locked={st.locked}
      description="Reach this service from other services in the same environment, over the encrypted mesh.">
      {editing ? (
        <PrivateNamePanel initial={short} onCancel={() => setEditing(false)}
          onSubmit={async (v) => { await stage("networking.privateName", v); setEditing(false); }} />
      ) : (
        <div className="group flex min-h-[74px] items-center gap-4 rounded-lg border border-line px-4 py-3 transition-colors duration-150 hover:border-fg-5">
          <CircleCheck size={20} className="shrink-0 text-ok-text" />
          <div className="min-w-0 flex-1">
            <div className="flex min-w-0 items-center gap-2">
              <span className="truncate text-base text-foreground">{host}</span>
              <Pill icon={<Globe size={12} />}>IPv4</Pill>
            </div>
            <div className="mt-1 text-caption font-medium text-fg-2">
              Ready for private traffic <span className="text-fg-4">·</span> <span className="text-pink-600">Other services can also use</span> <Code>{short}</Code>
            </div>
          </div>
          <CardActions>
            <CopyIcon text={host} />
            {!st.locked && <IconButton label="Edit private name" size={26} onClick={() => setEditing(true)}><Pencil size={15} /></IconButton>}
          </CardActions>
        </div>
      )}
    </SettingField>
  );
}

function PrivateNamePanel({ initial, onSubmit, onCancel }: { initial: string; onSubmit: (v: string) => Promise<void>; onCancel: () => void }) {
  const [v, setV] = useState(initial);
  const [busy, setBusy] = useState(false);
  const ok = /^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$/.test(v) && v !== initial;
  return (
    <InlinePanel submit="Update" valid={ok} busy={busy} onCancel={onCancel}
      hint={<><Info size={16} className="shrink-0" /><span className="truncate">Lowercase letters, digits and dashes. Staged until you deploy.</span></>}
      onSubmit={async () => { setBusy(true); try { await onSubmit(v); } catch (e) { toastError(e); } finally { setBusy(false); } }}>
      <div className="relative">
        <input autoFocus aria-label="Private name" value={v} onChange={(e) => setV(e.target.value.toLowerCase())} className={cx(PANEL_INPUT, "pr-24 font-mono")} />
        <span className="pointer-events-none absolute right-3 top-1/2 -translate-y-1/2 font-mono text-sm text-fg-3">.internal</span>
      </div>
    </InlinePanel>
  );
}

/* ---------- outbound ---------- */

function Outbound() {
  const { svc, eid } = useSettings();
  const env = useLoad(() => get<Canvas>(`/environments/${encodeURIComponent(eid)}/canvas`).then((c) => c.environment).catch(() => null), [eid]);
  const blocked = env.data?.egress === "blocked";
  return (
    <>
      <SettingField title="Outbound IP" keywords="outbound ip egress firewall allowlist static"
        description="Traffic from this service leaves through its hosts' public addresses. Add them to allowlists of the APIs you call.">
        <div className="flex flex-wrap gap-2">
          {svc.outboundIps.length ? svc.outboundIps.map((ip) => (
            <span key={ip} className="inline-flex h-[34px] items-center gap-1 rounded-md bg-surface-hover pl-3 pr-1 font-mono text-sm text-foreground">{ip}<CopyIcon text={ip} label={`Copy ${ip}`} /></span>
          )) : <span className="text-sm text-fg-3">Not running on any host yet.</span>}
        </div>
        {env.data && (
          <div className="mt-3 text-sm text-fg-2">
            Egress is {blocked ? <span className="text-warn">blocked for this environment{env.data.egressAllow?.length ? `, except ${env.data.egressAllow.join(", ")}` : ""}</span> : "allowed"}. Change it in project settings.
          </div>
        )}
      </SettingField>
      <SettingField title="Outbound IPv6" keywords="ipv6 outbound" description="Let the service open connections to IPv6 destinations.">
        <ToggleRow checked={false} onChange={() => undefined} label="Enable Outbound IPv6" disabled reason="Not in v0.1: containers use IPv4" />
      </SettingField>
    </>
  );
}

function MetadataAccess() {
  const { cfg, stage } = useSettings();
  const st = useFieldState("networking.allowMetadata");
  return (
    <SettingField title="Metadata and Host Access" keywords="metadata host access admin mesh 169.254" staged={st.staged} locked={st.locked}
      description="Admins only. Lets the service reach the cloud metadata endpoint and the hosts on the mesh. Every change is audited.">
      <ToggleRow checked={!!cfg.networking.allowMetadata} label="Allow metadata and host access" disabled={!!st.locked}
        onChange={(v) => void stage("networking.allowMetadata", v)} />
    </SettingField>
  );
}
