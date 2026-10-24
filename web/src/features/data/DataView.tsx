// SPDX-License-Identifier: AGPL-3.0-only

// Data sub-tab: the table (or collection) card grid with search and refresh, a
// detail drawer, the terminal hint, and the Redis key browser.

import { useEffect, useMemo, useRef, useState } from "react";
import { Database, FolderClosed, KeyRound, RefreshCw, Search, Table2, Terminal, X } from "lucide-react";
import type { DataResponse, RedisKeysPage, RedisRevealResponse, Service } from "../../api/types.gen";
import { Button, Input, Kbd, Sheet, Skeleton, cx, fmtBytes, toastError } from "../../kit";
import { get, post, qs, svcPath } from "../service/client";
import { useLoad } from "../service/ui";
import type { Engine } from "./engine";
import { CodeBox, Panel } from "./parts";

export default function DataView({ eid, service, engine }: { eid: string; service: Service; engine: Engine }) {
  return engine.key === "redis" ? <RedisKeys eid={eid} service={service} engine={engine} /> : <Tables eid={eid} service={service} engine={engine} />;
}

function useSlash(ref: React.RefObject<HTMLInputElement>) {
  useEffect(() => {
    const h = (e: KeyboardEvent) => {
      const t = e.target as HTMLElement | null;
      if (e.key === "/" && !(t && /INPUT|TEXTAREA|SELECT/.test(t.tagName)) && !(t && t.isContentEditable)) {
        e.preventDefault();
        ref.current?.focus();
      }
    };
    window.addEventListener("keydown", h);
    return () => window.removeEventListener("keydown", h);
  }, [ref]);
}

function Toolbar({ title, q, setQ, placeholder, spinning, onRefresh, inputRef }: {
  title: string; q: string; setQ: (v: string) => void; placeholder: string; spinning: boolean; onRefresh: () => void; inputRef: React.RefObject<HTMLInputElement>;
}) {
  return (
    <div className="mb-4 flex items-center justify-between gap-3">
      <h3 className="text-lead font-medium text-foreground">{title}</h3>
      <div className="flex items-center gap-2">
        <div className="w-[220px]">
          <Input ref={inputRef} size="sm" aria-label={placeholder} placeholder={placeholder} value={q} onChange={(e) => setQ(e.target.value)}
            icon={<Search size={14} />}
            onKeyDown={(e) => { if (e.key === "Escape") { setQ(""); (e.target as HTMLInputElement).blur(); } }}
            suffix={q ? <button type="button" aria-label="Clear search" onClick={() => setQ("")} className="text-fg-3 hover:text-foreground"><X size={14} /></button> : <Kbd>/</Kbd>} />
        </div>
        <button type="button" aria-label="Refresh" title="Refresh" onClick={onRefresh}
          className="inline-flex h-[34px] w-[34px] items-center justify-center rounded-md border border-line text-fg-2 transition-colors hover:bg-surface-hover hover:text-foreground">
          <RefreshCw size={16} style={spinning ? { animation: "spin 1.5s linear infinite" } : undefined} />
        </button>
      </div>
    </div>
  );
}

function TerminalHint({ engine, name }: { engine: Engine; name: string }) {
  const cmd = `5050 connect ${name}`;
  return (
    <Panel className="mt-8" title={<span className="flex items-center gap-2"><Terminal size={16} className="text-fg-3" />Run queries</span>}
      hint={`Open ${engine.client} against this database through an encrypted tunnel. Nothing is exposed publicly.`}>
      <div className="px-4"><CodeBox text={cmd} /></div>
    </Panel>
  );
}

function Tables({ eid, service, engine }: { eid: string; service: Service; engine: Engine }) {
  const base = svcPath(eid, service.id);
  const data = useLoad(() => get<DataResponse>(`${base}/data`), [base]);
  const [q, setQ] = useState("");
  const [spin, setSpin] = useState(false);
  const [open, setOpen] = useState<string | null>(null);
  const ref = useRef<HTMLInputElement>(null);
  useSlash(ref);
  const key = (t: { schema?: string; name: string }) => `${t.schema || ""}.${t.name}`;
  const list = useMemo(() => (data.data?.tables || []).filter((t) => t.name.toLowerCase().includes(q.trim().toLowerCase())), [data.data, q]);
  const refresh = async () => { setSpin(true); await data.reload(); setTimeout(() => setSpin(false), 600); };
  const sel = data.data?.tables.find((t) => key(t) === open);
  const Icon = engine.key === "mongodb" ? FolderClosed : Table2;
  const cap = engine.nouns[0].toUpperCase() + engine.nouns.slice(1);

  return (
    <div>
      <Toolbar title={cap} q={q} setQ={setQ} placeholder={`Search ${engine.nouns}`} spinning={spin || (!data.data && !data.error)} onRefresh={refresh} inputRef={ref} />
      {data.error ? (
        <div role="alert" className="rounded-lg border border-danger-line bg-danger-bg p-6 text-center">
          <div className="text-lead font-medium text-danger-text">Error loading {engine.nouns}</div>
          <p className="mt-1 text-body text-fg-2">{data.error}</p>
          <Button className="mt-4" onClick={refresh}>Try again</Button>
        </div>
      ) : !data.data ? (
        <div className="grid grid-cols-[repeat(auto-fill,minmax(150px,1fr))] gap-2">{Array.from({ length: 8 }, (_, i) => <Skeleton key={i} className="h-[100px] rounded-lg" />)}</div>
      ) : data.data.tables.length === 0 ? (
        <div className="rounded-lg border border-dashed border-line px-6 py-12 text-center">
          <Database size={28} className="mx-auto text-fg-4" />
          <div className="mt-3 text-lead font-medium text-foreground">You have no {engine.nouns}</div>
          <p className="mt-1 text-body text-fg-2">Create {engine.nouns} from your application or connect with the CLI below.</p>
        </div>
      ) : list.length === 0 ? (
        <div className="rounded-lg border border-dashed border-line px-6 py-12 text-center text-body text-fg-2">No {engine.nouns} match "{q}"</div>
      ) : (
        <div className="grid grid-cols-[repeat(auto-fill,minmax(150px,1fr))] gap-x-2 gap-y-2">
          {list.map((t) => (
            <button key={key(t)} type="button" onClick={() => setOpen(key(t))} aria-pressed={open === key(t)}
              className={cx("flex min-h-[100px] flex-col items-center justify-center gap-2 rounded-lg border p-3 transition-[transform,background-color,border-color] duration-100 ease-[cubic-bezier(.4,0,.2,1)] hover:scale-105 active:scale-100",
                open === key(t) ? "border-fg-5 bg-surface-hover" : "border-line hover:border-fg-5 hover:bg-surface")}>
              <Icon size={24} className="text-blue-600" />
              <span className="w-full truncate text-center text-body text-foreground" title={t.name}>{t.schema && t.schema !== "public" ? `${t.schema}.` : ""}{t.name}</span>
              <span className="text-caption text-fg-3 tabular-nums">{t.rows.toLocaleString()} rows</span>
            </button>
          ))}
        </div>
      )}
      <TerminalHint engine={engine} name={service.config.name} />
      {sel && (
        <Sheet title={sel.name} onClose={() => setOpen(null)} width={400}>
          <dl className="grid grid-cols-[110px_1fr] gap-y-3 p-6 text-body">
            <dt className="text-fg-3">Kind</dt><dd className="text-foreground capitalize">{sel.kind}</dd>
            {sel.schema && <><dt className="text-fg-3">Schema</dt><dd className="font-mono text-foreground">{sel.schema}</dd></>}
            <dt className="text-fg-3">Rows (estimate)</dt><dd className="text-foreground tabular-nums">{sel.rows.toLocaleString()}</dd>
            <dt className="text-fg-3">Size</dt><dd className="text-foreground tabular-nums">{fmtBytes(sel.bytes)}</dd>
          </dl>
          <div className="px-6 pb-6">
            <div className="mb-2 text-caption font-medium text-fg-3">Open in your client</div>
            <CodeBox text={`5050 connect ${service.config.name}`} compact />
          </div>
        </Sheet>
      )}
    </div>
  );
}

function ttlText(ms: number) {
  if (ms <= 0) return "no expiry";
  const s = Math.round(ms / 1000);
  if (s < 90) return `${s}s`;
  if (s < 5400) return `${Math.round(s / 60)}m`;
  if (s < 129600) return `${Math.round(s / 3600)}h`;
  return `${Math.round(s / 86400)}d`;
}

const TYPE_TONE: Record<string, string> = { string: "text-blue-600", hash: "text-ok-text", list: "text-warn", set: "text-accent-text", zset: "text-accent-text", stream: "text-danger-text" };

function RedisKeys({ eid, service, engine }: { eid: string; service: Service; engine: Engine }) {
  const base = svcPath(eid, service.id);
  const [q, setQ] = useState("");
  const [match, setMatch] = useState("*");
  const [page, setPage] = useState<RedisKeysPage | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const [open, setOpen] = useState<string | null>(null);
  const ref = useRef<HTMLInputElement>(null);
  useSlash(ref);

  const load = async (m: string, cursor?: string) => {
    setLoading(true); setErr(null);
    try {
      const r = await get<RedisKeysPage>(`${base}/redis/keys${qs({ match: m, cursor })}`);
      setPage((cur) => (cursor && cur ? { keys: [...cur.keys, ...r.keys], cursor: r.cursor } : r));
    } catch (e) { setErr(e instanceof Error ? e.message : String(e)); } finally { setLoading(false); }
  };
  // eslint-disable-next-line react-hooks/exhaustive-deps
  useEffect(() => { setPage(null); void load(match); }, [base]);
  useEffect(() => {
    const t = setTimeout(() => { const m = q.trim() ? (q.includes("*") ? q.trim() : `*${q.trim()}*`) : "*"; if (m !== match) { setMatch(m); setPage(null); void load(m); } }, 250);
    return () => clearTimeout(t);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [q]);
  const sel = page?.keys.find((k) => k.key === open);

  return (
    <div>
      <Toolbar title="Keys" q={q} setQ={setQ} placeholder="Search keys" spinning={loading} onRefresh={() => { setPage(null); void load(match); }} inputRef={ref} />
      {err ? (
        <div role="alert" className="rounded-lg border border-danger-line bg-danger-bg p-6 text-center">
          <div className="text-lead font-medium text-danger-text">Error loading keys</div>
          <p className="mt-1 text-body text-fg-2">{err}</p>
        </div>
      ) : !page ? (
        <div className="flex flex-col gap-2">{Array.from({ length: 6 }, (_, i) => <Skeleton key={i} className="h-[52px] rounded-lg" />)}</div>
      ) : page.keys.length === 0 ? (
        <div className="rounded-lg border border-dashed border-line px-6 py-12 text-center">
          <KeyRound size={28} className="mx-auto text-fg-4" />
          <div className="mt-3 text-lead font-medium text-foreground">{q ? `No keys match "${q}"` : "No keys here"}</div>
        </div>
      ) : (
        <div className="overflow-hidden rounded-lg border border-line">
          <div className="grid grid-cols-[1fr_80px_90px_100px] gap-3 bg-tint-header px-4 py-2 text-caption font-medium text-fg-3">
            <span>Key</span><span>Type</span><span className="text-right">Size</span><span className="text-right">Expires</span>
          </div>
          {page.keys.map((k) => (
            <button key={k.key} type="button" onClick={() => setOpen(k.key)}
              className="grid w-full grid-cols-[1fr_80px_90px_100px] items-center gap-3 border-t border-surface-hover px-4 py-2.5 text-left text-body transition-colors duration-100 hover:bg-surface">
              <span className="truncate font-mono text-foreground" title={k.key}>{k.key}</span>
              <span className={cx("text-caption font-medium uppercase", TYPE_TONE[k.type] || "text-fg-2")}>{k.type}</span>
              <span className="text-right tabular-nums text-fg-2">{fmtBytes(k.sizeBytes)}</span>
              <span className="text-right tabular-nums text-fg-3">{ttlText(k.ttlMs)}</span>
            </button>
          ))}
        </div>
      )}
      {page?.cursor && <div className="mt-3 text-center"><Button busy={loading} onClick={() => void load(match, page.cursor)}>Load more</Button></div>}
      <TerminalHint engine={engine} name={service.config.name} />
      {sel && <KeySheet base={base} k={sel} onClose={() => setOpen(null)} />}
    </div>
  );
}

function KeySheet({ base, k, onClose }: { base: string; k: RedisKeysPage["keys"][number]; onClose: () => void }) {
  const [v, setV] = useState<RedisRevealResponse | null>(null);
  const [busy, setBusy] = useState(false);
  const reveal = async () => {
    setBusy(true);
    try { setV(await post<RedisRevealResponse>(`${base}/redis/keys/reveal`, { key: k.key })); } catch (e) { toastError(e); } finally { setBusy(false); }
  };
  return (
    <Sheet title={<span className="font-mono text-lead">{k.key}</span>} label={`Key ${k.key}`} onClose={onClose} width={400}>
      <dl className="grid grid-cols-[90px_1fr] gap-y-3 p-6 text-body">
        <dt className="text-fg-3">Type</dt><dd className="text-foreground">{k.type}</dd>
        <dt className="text-fg-3">Size</dt><dd className="text-foreground tabular-nums">{fmtBytes(k.sizeBytes)}</dd>
        <dt className="text-fg-3">Expires</dt><dd className="text-foreground">{ttlText(k.ttlMs)}</dd>
      </dl>
      <div className="px-6 pb-6">
        <div className="mb-2 flex items-center justify-between"><span className="text-caption font-medium text-fg-3">Value</span>
          {!v && <Button size="xs" busy={busy} onClick={reveal}>Reveal value</Button>}</div>
        {v ? (
          <pre className="max-h-[360px] overflow-auto whitespace-pre-wrap break-all rounded-md bg-surface p-3 font-mono text-body text-foreground scroll-thin">{v.value}{v.truncated ? "\n(first 1 KB shown)" : ""}</pre>
        ) : <div className="rounded-md bg-surface p-3 text-body text-fg-3">Hidden until revealed.</div>}
      </div>
    </Sheet>
  );
}
