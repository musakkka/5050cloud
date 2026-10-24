// SPDX-License-Identifier: AGPL-3.0-only

import { useEffect, useRef, useState } from "react";
import { AlertCircle, Check, ChevronDown, Info, Plus, Sparkles, Trash2, Webhook as WebhookIcon, X } from "lucide-react";
import type * as T from "../../api/types.gen";
import { Badge, Button, Checkbox, Confirm, IconButton, Input, Switch, cx, toast } from "../../kit";
import { v1 } from "../settings/client";
import { CRITICAL_EVENTS, WEBHOOK_EVENTS, checkWebhook, webhookBody } from "../settings/forms";
import { CopyBox } from "./CopyBox";
import { FormLabel, InlineError, LoadError, SectionHead, SectionSkeleton, useAsync, useLoad } from "./ui";

type Tone = "red" | "blue" | "amber";
interface EventMeta { label: string; group: string; tone: Tone }
const META: Record<string, EventMeta> = {
  "deployment.success": { label: "Deployed", group: "Deployment", tone: "blue" },
  "deployment.failed": { label: "Failed", group: "Deployment", tone: "amber" },
  "deployment.crashed": { label: "Crashed", group: "Deployment", tone: "red" },
  "replica.oom_killed": { label: "Oom Killed", group: "Service", tone: "red" },
  "service.unhealthy": { label: "Unhealthy", group: "Service", tone: "amber" },
  "volume.high": { label: "Volume High", group: "Storage", tone: "red" },
  "disk.high": { label: "Disk High", group: "Storage", tone: "red" },
  "backup.failed": { label: "Backup Failed", group: "Storage", tone: "red" },
  "monitor.triggered": { label: "Triggered", group: "Monitor", tone: "red" },
  "monitor.resolved": { label: "Resolved", group: "Monitor", tone: "blue" },
  "host.offline": { label: "Host Offline", group: "Host", tone: "amber" },
};
const meta = (ev: string): EventMeta => META[ev] || { label: ev, group: "Other", tone: "blue" };
const GROUPS = ["Deployment", "Service", "Storage", "Monitor", "Host"].map((g) => ({ group: g, events: WEBHOOK_EVENTS.filter((e) => meta(e).group === g) })).filter((g) => g.events.length);
const TONE: Record<Tone, string> = { red: "bg-danger-bg text-danger-soft border-danger-line", blue: "bg-blue-50 text-blue-700 border-blue-200", amber: "bg-warn-bg text-warn border-warn-line" };

export default function Webhooks({ project }: { project: T.Project }) {
  const { data, error, setData, reload } = useLoad(() => v1.webhooks(project.id), [project.id]);
  const [creating, setCreating] = useState(false);
  const [secret, setSecret] = useState<string | null>(null);
  const [del, setDel] = useState<T.Webhook | null>(null);
  if (error) return <LoadError error={error} onRetry={reload} />;
  if (!data) return <SectionSkeleton />;
  const showForm = creating || data.length === 0;
  return (
    <section>
      <SectionHead title="Webhooks" description="Get a signed JSON post when something happens in this project. Check the X-5050-Signature header with the secret. Discord and Slack URLs are formatted automatically."
        actions={data.length > 0 && !showForm ? <Button variant="accent-outline" icon={<Plus size={16} />} onClick={() => setCreating(true)}>Create Webhook</Button> : undefined} />
      {secret && (
        <div className="mb-6 animate-rise-in rounded-lg border border-line p-4">
          <p className="mb-2 flex items-center gap-2 text-body text-ok-soft"><Check size={16} />Copy the signing secret now. It is not shown again.</p>
          <CopyBox value={secret} />
          <Button variant="ghost" size="sm" className="mt-2" onClick={() => setSecret(null)}>Done</Button>
        </div>
      )}
      {data.length > 0 && <WebhookList data={data} setData={setData} onDelete={setDel} />}
      {showForm && <NewWebhook project={project} first={data.length === 0}
        onCancel={data.length ? () => setCreating(false) : undefined}
        onCreated={(w) => { setData([w, ...data]); setSecret(w.secret || null); setCreating(false); toast("Webhook created", "success"); }} />}
      {del && <Confirm title="Delete webhook" danger confirm="Delete" body={<>Stop sending events to <b className="break-all">{del.url}</b>?</>} onClose={() => setDel(null)}
        onConfirm={async () => { await v1.deleteWebhook(del.id); setData(data.filter((x) => x.id !== del.id)); toast("Webhook deleted", "success"); }} />}
    </section>
  );
}

function WebhookList({ data, setData, onDelete }: { data: T.Webhook[]; setData: (d: T.Webhook[]) => void; onDelete: (w: T.Webhook) => void }) {
  const [tests, setTests] = useState<Record<string, T.WebhookTestResponse | "busy">>({});
  const [open, setOpen] = useState<string | null>(null);
  const toggle = async (w: T.Webhook, on: boolean) => {
    const prev = data;
    setData(data.map((x) => (x.id === w.id ? { ...x, enabled: on } : x)));
    try { await v1.updateWebhook(w.id, { enabled: on }); } catch (e: any) { setData(prev); toast(e?.message || String(e), "error"); }
  };
  const test = async (w: T.Webhook) => {
    setTests({ ...tests, [w.id]: "busy" });
    try { const r = await v1.testWebhook(w.id); setTests((t) => ({ ...t, [w.id]: r })); }
    catch (e: any) { setTests((t) => { const { [w.id]: _x, ...rest } = t; return rest; }); toast(e?.message || String(e), "error"); }
  };
  return (
    <div className="mb-8 overflow-hidden rounded-lg border border-line">
      {data.map((w, i) => {
        const t = tests[w.id];
        return (
          <div key={w.id} className={cx(i > 0 && "border-t border-surface-hover")}>
            <div className="flex min-h-[60px] flex-wrap items-center gap-3 px-4 py-2 transition-colors hover:bg-tint-hover">
              <WebhookIcon size={18} className="shrink-0 text-fg-3" />
              <button type="button" onClick={() => setOpen(open === w.id ? null : w.id)} className="min-w-0 flex-1 text-left">
                <div className="truncate font-mono text-caption text-foreground">{w.url}</div>
                <div className="text-caption text-fg-3">{w.preset === "critical" ? "Critical events" : `${w.events.length} ${w.events.length === 1 ? "event" : "events"}`}</div>
              </button>
              <Switch size="sm" checked={w.enabled} onChange={(on) => toggle(w, on)} label={`${w.enabled ? "Disable" : "Enable"} webhook`} />
              <Button size="sm" busy={t === "busy"} onClick={() => test(w)}>Test</Button>
              <IconButton label="Delete webhook" size={30} onClick={() => onDelete(w)}><Trash2 size={16} /></IconButton>
            </div>
            {t && t !== "busy" && (
              <div className={cx("px-4 pb-3 text-caption", t.status === "ok" ? "text-ok-soft" : "text-danger-text")}>
                {t.status === "ok" ? `Test delivered${t.responseCode ? ` (HTTP ${t.responseCode})` : ""}.` : `Test failed${t.responseCode ? ` (HTTP ${t.responseCode})` : ""}${t.error ? `: ${t.error}` : "."}`}
              </div>
            )}
            {open === w.id && (
              <div className="animate-fade-in border-t border-surface-hover bg-tint-header px-4 py-3">
                <div className="mb-3 flex flex-wrap gap-2">{w.events.map((ev) => <EventChip key={ev} ev={ev} small />)}</div>
                {w.deliveries?.length ? w.deliveries.map((d) => (
                  <div key={d.id} className="flex gap-3 py-0.5 text-caption text-fg-2"><span className="w-40 shrink-0 text-fg-3">{new Date(d.createdAt).toLocaleString()}</span><span className="flex-1 truncate">{d.event}</span><Badge kind={d.status}>{d.status}{d.responseCode ? ` ${d.responseCode}` : ""}</Badge></div>
                )) : <p className="text-caption text-fg-3">No deliveries yet.</p>}
              </div>
            )}
          </div>
        );
      })}
    </div>
  );
}

function EventChip({ ev, selected, onClick, small }: { ev: string; selected?: boolean; onClick?: () => void; small?: boolean }) {
  const m = meta(ev);
  const Icon = m.tone === "blue" ? Info : AlertCircle;
  const cls = cx("inline-flex items-center gap-3 rounded-md border text-body transition-[filter,box-shadow]", small ? "h-7 px-2 text-caption" : "h-9 px-3", TONE[m.tone], onClick && "hover:brightness-125", selected ? "border-current" : "border-transparent");
  const inner = <><Icon size={small ? 12 : 16} />{m.label}{selected && <Check size={14} />}</>;
  return onClick ? <button type="button" aria-pressed={selected} onClick={onClick} className={cls}>{inner}</button> : <span className={cls}>{inner}</span>;
}

type Header = { k: string; v: string };

function NewWebhook({ project, first, onCancel, onCreated }: { project: T.Project; first: boolean; onCancel?: () => void; onCreated: (w: T.Webhook) => void }) {
  const [url, setUrl] = useState("");
  const [events, setEvents] = useState<string[]>([]);
  const [headers, setHeaders] = useState<Header[]>([]);
  const [pr, setPr] = useState(false);
  const [pick, setPick] = useState(false);
  const [testRes, setTestRes] = useState<string | null>(null);
  const a = useAsync();
  const t = useAsync();
  const errs = checkWebhook(url, events);
  const valid = !errs.url && !errs.events;
  const body = (): Partial<T.Webhook> => {
    const b: Record<string, unknown> = { ...webhookBody(url, events) };
    const hs = headers.filter((h) => h.k.trim());
    if (hs.length) b.headers = Object.fromEntries(hs.map((h) => [h.k.trim(), h.v]));
    if (pr) b.includePrEnvironments = true;
    return b as Partial<T.Webhook>;
  };
  const create = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!valid) return;
    const r = await a.run(() => v1.createWebhook(project.id, body()));
    if (r) onCreated(r);
  };
  const test = async () => {
    setTestRes(null);
    const r = await t.run(async () => {
      const w = await v1.createWebhook(project.id, { ...body(), enabled: false });
      try { return await v1.testWebhook(w.id); } finally { await v1.deleteWebhook(w.id).catch(() => undefined); }
    });
    if (r) setTestRes(r.status === "ok" ? `Test delivered${r.responseCode ? ` (HTTP ${r.responseCode})` : ""}.` : `Test failed${r.responseCode ? ` (HTTP ${r.responseCode})` : ""}${r.error ? `: ${r.error}` : "."}`);
  };
  return (
    <form onSubmit={create} noValidate className="animate-rise-in overflow-hidden rounded-xl border border-surface-hover">
      <div className="space-y-6 p-[25px]">
        <h3 className="text-h2 text-foreground">New Webhook</h3>
        <div>
          <FormLabel htmlFor="wh-url">Webhook URL</FormLabel>
          <Input id="wh-url" icon={<WebhookIcon size={20} />} value={url} onChange={(e) => setUrl(e.target.value)} placeholder="https://example.com/webhook" autoComplete="off" aria-invalid={(!!url && !!errs.url) || undefined} />
          {url && errs.url ? <InlineError>{errs.url}</InlineError> : <p className="mt-2 text-caption text-fg-2">Discord and Slack URLs get a message payload automatically. Anything else gets JSON.</p>}
        </div>
        <div>
          <div className="mb-2 flex items-center justify-between">
            <span className="text-caption font-medium text-foreground">Custom headers</span>
            <Button size="xs" icon={<Plus size={14} />} onClick={() => setHeaders([...headers, { k: "", v: "" }])}>Add header</Button>
          </div>
          <p className="text-caption text-fg-2">Headers are encrypted at rest and are not shown again after saving.</p>
          {headers.map((h, i) => (
            <div key={i} className="mt-3 flex animate-rise-in items-center gap-2">
              <Input value={h.k} onChange={(e) => setHeaders(headers.map((x, j) => (j === i ? { ...x, k: e.target.value } : x)))} placeholder="Authorization" aria-label="Header name" autoFocus />
              <Input value={h.v} onChange={(e) => setHeaders(headers.map((x, j) => (j === i ? { ...x, v: e.target.value } : x)))} placeholder="Bearer ..." aria-label="Header value" />
              <IconButton label="Remove header" size={34} onClick={() => setHeaders(headers.filter((_, j) => j !== i))}><Trash2 size={16} /></IconButton>
            </div>
          ))}
        </div>
        <div>
          <FormLabel>Event Types</FormLabel>
          <EventPicker events={events} setEvents={setEvents} open={pick} setOpen={setPick} />
          {pick || !errs.events || !url ? null : <InlineError>{errs.events}</InlineError>}
          <button type="button" onClick={() => setEvents([...new Set([...events, ...CRITICAL_EVENTS])])}
            className="mt-3 inline-flex h-[34px] items-center gap-2 rounded-md border border-dashed border-line px-3 text-body text-accent-muted transition-colors hover:border-accent hover:bg-pink-50">
            <Sparkles size={16} />Add Critical events
          </button>
        </div>
        <InlineError>{a.error || t.error}</InlineError>
        {testRes && <p role="status" className={cx("text-caption", testRes.startsWith("Test delivered") ? "text-ok-soft" : "text-danger-text")}>{testRes}</p>}
      </div>
      <div className="flex flex-wrap items-center justify-between gap-3 bg-tint-header px-[25px] py-4">
        <Checkbox checked={pr} onChange={setPr} label="Include PR environments" />
        <div className="flex gap-2">
          {onCancel && <Button size="lg" variant="ghost" onClick={onCancel}>Cancel</Button>}
          <Button size="lg" busy={t.busy} disabled={!valid} onClick={test}>Test Webhook</Button>
          <Button size="lg" type="submit" variant="primary" busy={a.busy} disabled={!valid}>Create Webhook</Button>
        </div>
      </div>
      {first && <span className="sr-only">No webhooks yet</span>}
    </form>
  );
}

function EventPicker({ events, setEvents, open, setOpen }: { events: string[]; setEvents: (e: string[]) => void; open: boolean; setOpen: (o: boolean) => void }) {
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!open) return;
    const out = (e: MouseEvent) => { if (!ref.current?.contains(e.target as Node)) setOpen(false); };
    const esc = (e: KeyboardEvent) => { if (e.key === "Escape") { e.stopPropagation(); setOpen(false); } };
    document.addEventListener("mousedown", out); document.addEventListener("keydown", esc, true);
    return () => { document.removeEventListener("mousedown", out); document.removeEventListener("keydown", esc, true); };
  }, [open, setOpen]);
  const toggle = (ev: string) => setEvents(events.includes(ev) ? events.filter((x) => x !== ev) : [...events, ev]);
  return (
    <div ref={ref} className="relative">
      <div role="combobox" aria-expanded={open} aria-haspopup="listbox" tabIndex={0} onClick={() => setOpen(!open)}
        onKeyDown={(e) => { if (e.key === "Enter" || e.key === " " || e.key === "ArrowDown") { e.preventDefault(); setOpen(true); } }}
        className={cx("flex min-h-[42px] cursor-pointer flex-wrap items-center gap-2 rounded-md border bg-transparent px-3 py-1.5 transition-colors", open ? "border-accent" : "border-line hover:border-fg-5")}>
        {events.length === 0 && <span className="text-base text-fg-4">Choose events...</span>}
        {events.map((ev) => (
          <span key={ev} className={cx("inline-flex h-7 items-center gap-1.5 rounded px-2 text-caption", TONE[meta(ev).tone])}>
            {meta(ev).group} {meta(ev).label}
            <button type="button" aria-label={`Remove ${meta(ev).label}`} onClick={(e) => { e.stopPropagation(); toggle(ev); }} className="opacity-70 hover:opacity-100"><X size={12} /></button>
          </span>
        ))}
        <ChevronDown size={16} className={cx("ml-auto text-fg-4 transition-transform", open && "rotate-180")} />
      </div>
      {open && (
        <div role="listbox" aria-multiselectable className="absolute left-0 right-0 top-[calc(100%+4px)] z-30 max-h-[360px] animate-pop-in overflow-y-auto scroll-thin rounded-md border border-line-popover bg-background p-4 shadow-popover">
          {GROUPS.map((g, i) => (
            <div key={g.group} className={cx(i > 0 && "mt-5")}>
              <div className="mb-2 text-body font-semibold text-foreground">{g.group}</div>
              <div className="flex flex-wrap gap-2">{g.events.map((ev) => <EventChip key={ev} ev={ev} selected={events.includes(ev)} onClick={() => toggle(ev)} />)}</div>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

