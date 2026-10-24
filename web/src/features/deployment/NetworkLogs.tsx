// SPDX-License-Identifier: AGPL-3.0-only

// Deployment drawer, Network Logs: HTTP requests, DNS lookups and network
// flows of one deployment. Clicking a value adds it to the filter.

import { useEffect, useMemo, useRef, useState } from "react";
import { ArrowDown, ArrowUp, Globe, Link2 } from "lucide-react";
import type { DeploymentDetail, LogLine } from "../../api/types.gen";
import { Button, Checkbox, IconButton, Popover, cx, fmtBytes, readPref, writePref } from "../../kit";
import { Settings } from "lucide-react";
import { qs } from "../service/client";
import { inProgress } from "./actions";
import { fmtSecond, matches, parseQuery, tzLabel } from "./format";
import { FilterBox, LogsEmpty, useLogLines, useSlashFocus } from "./LogView";

type Sub = "http" | "dns" | "flow";

const PLACEHOLDER: Record<Sub, string> = {
  http: "Search HTTP logs e.g. @path:/api/users @httpStatus:200",
  dns: "Filter e.g. @rcode:NXDOMAIN @qtype:A @domain:example.com",
  flow: "Filter e.g. @protocol:tcp @peer:\"My Service\" @status:ok",
};

const a = (l: LogLine, k: string) => (l.attrs?.[k] === undefined || l.attrs?.[k] === null ? "" : String(l.attrs[k]));

export default function NetworkLogs({ d }: { d: DeploymentDetail }) {
  const [sub, setSub] = useState<Sub>(() => readPref<Sub>("5050.netlogs.tab", "http"));
  const pick = (s: Sub) => { setSub(s); writePref("5050.netlogs.tab", s); };
  const [input, setInput] = useState("");
  const [q, setQ] = useState("");
  const [limit, setLimit] = useState(100);
  const [cols, setCols] = useState(() => readPref("5050.netlogs.cols", { requestId: false, srcIp: false, host: false }));
  const box = useRef<HTMLInputElement>(null);
  useSlashFocus(box);
  useEffect(() => { setInput(""); setQ(""); setLimit(100); }, [sub]);
  useEffect(() => { const t = setTimeout(() => setQ(input.trim()), 250); return () => clearTimeout(t); }, [input]);

  const live = (d.active || inProgress(d)) && sub === "http";
  const path = `/logs${qs({ environment: d.environmentId, deployment: d.id, type: sub, limit })}`;
  const stream = live ? `/api/v1/logs/stream${qs({ environment: d.environmentId, deployment: d.id, type: "http" })}` : null;
  const { lines, loading, error, truncated } = useLogLines(path, stream, limit);
  const terms = useMemo(() => parseQuery(q), [q]);
  const shown = useMemo(() => (terms.length ? lines.filter((l) => matches(l, terms)) : lines), [lines, terms]);
  const add = (k: string, v: string) => {
    if (!v) return;
    const t = `@${k}:${/\s/.test(v) ? `"${v}"` : v}`;
    const nq = q.split(/\s+/).includes(t) ? q : `${q} ${t}`.trim();
    setInput(nq); setQ(nq);
  };
  const tz = tzLabel();

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="flex items-center gap-3 border-b border-line px-4 py-2 sm:px-6">
        <div role="tablist" aria-label="Network log type" className="inline-flex shrink-0">
          {([["http", "HTTP"], ["dns", "DNS"], ["flow", "Network Flow"]] as const).map(([id, label], i) => (
            <button key={id} type="button" role="tab" aria-selected={sub === id} onClick={() => pick(id)}
              className={cx("relative inline-flex h-[34px] items-center border px-3 text-sm transition-colors duration-150",
                i === 0 ? "rounded-l-md" : "-ml-px", i === 2 && "rounded-r-md",
                sub === id ? "z-[1] border-pink-300 bg-background text-accent-text" : "border-line text-foreground hover:bg-surface-hover")}>
              {label}
            </button>
          ))}
        </div>
        <FilterBox ref={box} value={input} onChange={setInput} onSubmit={(v) => { setInput(v); setQ(v); }} placeholder={PLACEHOLDER[sub]} tags={false} />
      </div>

      <div className="relative min-h-0 flex-1">
        <div className="scroll-thin absolute inset-0 overflow-auto">
          {error && !lines.length ? (
            sub === "http" ? <div role="alert" className="m-6 rounded-md border border-danger-line bg-danger-bg px-4 py-3 text-sm text-danger-text">Could not load HTTP logs. {error}</div>
              : <Empty sub={sub} />
          ) : !loading && !shown.length ? (
            q ? <LogsEmpty title="No matching requests" body="Nothing in this range matches the filter." /> : <Empty sub={sub} />
          ) : sub === "http" ? (
            <HttpTable lines={shown} tz={tz} cols={cols} onAdd={add} loading={loading && !lines.length}
              gear={<NetCols cols={cols} onChange={(c) => { setCols(c); writePref("5050.netlogs.cols", c); }} />}
              more={truncated || lines.length >= limit ? () => setLimit((n) => n + 100) : undefined} />
          ) : sub === "flow" ? (
            <FlowTable lines={shown} tz={tz} onAdd={add} />
          ) : <Empty sub={sub} />}
        </div>
      </div>
    </div>
  );
}

function Empty({ sub }: { sub: Sub }) {
  return sub === "dns" ? <LogsEmpty title="No DNS queries in this time range" body="DNS lookups made by your service will show up here." />
    : sub === "flow" ? <LogsEmpty title="No network flows in this time range" body="Connections to and from your service will show up here." />
    : <LogsEmpty title="No HTTP requests in this time range" body="Requests that reach this deployment through a domain will show up here." />;
}

function NetCols({ cols, onChange }: { cols: { requestId: boolean; srcIp: boolean; host: boolean }; onChange: (c: { requestId: boolean; srcIp: boolean; host: boolean }) => void }) {
  return (
    <Popover align="right" width={240} role="dialog" wrapperClassName="relative shrink-0"
      trigger={(open) => <IconButton label="View options" size={26} active={open}><Settings size={16} /></IconButton>}>
      {() => (
        <div className="p-3">
          <div className="mb-2 text-caption font-medium text-fg-2">Columns</div>
          {([["host", "Host"], ["requestId", "Request id"], ["srcIp", "Client IP"]] as const).map(([k, label]) => (
            <Checkbox key={k} checked={cols[k]} onChange={(v) => onChange({ ...cols, [k]: v })} label={<span className="text-caption font-light text-fg-menu">{label}</span>} className="h-6 items-center !gap-3" />
          ))}
        </div>
      )}
    </Popover>
  );
}

const statusTone = (s: number) => (s >= 500 ? "text-danger-text" : s >= 400 ? "text-warn" : s >= 300 ? "text-blue-600" : "text-ok-text");
const th = "whitespace-nowrap px-0 py-0 text-left text-caption font-medium text-fg-2";

function Cell({ children, onClick, className }: { children: React.ReactNode; onClick?: () => void; className?: string }) {
  return (
    <td className={cx("whitespace-nowrap py-1 pr-6 align-top leading-[18px]", className)}>
      {onClick ? <button type="button" onClick={onClick} title="Add as a filter" className="rounded-sm text-left transition-colors hover:underline hover:decoration-fg-4 hover:underline-offset-2">{children}</button> : children}
    </td>
  );
}

function HttpTable({ lines, tz, cols, onAdd, more, gear, loading }: {
  lines: LogLine[]; tz: string; cols: { requestId: boolean; srcIp: boolean; host: boolean }; onAdd: (k: string, v: string) => void; more?: () => void; gear: React.ReactNode; loading: boolean;
}) {
  return (
    <>
      <table className="w-full border-collapse font-mono text-caption">
        <thead className="sticky top-0 z-[2] bg-surface-raised font-sans">
          <tr className="h-[38px] border-b border-line">
            <th className={cx(th, "w-[180px] pl-6")}>Time ({tz})</th>
            <th className={cx(th, "w-[64px]")}>Method</th>
            <th className={cx(th, "w-[200px]")}>Path</th>
            <th className={cx(th, "w-[120px]")}>HTTP Status</th>
            <th className={cx(th, cols.host || cols.requestId || cols.srcIp ? "w-[120px]" : "")}>Total Duration</th>
            {cols.host && <th className={th}>Host</th>}
            {cols.requestId && <th className={th}>Request id</th>}
            {cols.srcIp && <th className={th}>Client IP</th>}
            <th className="w-10 pr-4 text-right">{gear}</th>
          </tr>
        </thead>
        <tbody>
          {loading ? Array.from({ length: 10 }, (_, i) => (
            <tr key={i} className="h-[26px]"><td colSpan={6} className="pl-6"><span className="block h-3 animate-skeleton rounded bg-skeleton" style={{ width: `${40 + ((i * 13) % 40)}%` }} /></td></tr>
          )) : lines.map((l) => {
            const st = Number(a(l, "httpStatus"));
            return (
              <tr key={l.id} className="text-fg-menu transition-colors hover:bg-tint-hover">
                <Cell className="pl-6 text-fg-2">{fmtSecond(l.ts)}</Cell>
                <Cell onClick={() => onAdd("method", a(l, "method"))}>{a(l, "method")}</Cell>
                <Cell onClick={() => onAdd("path", a(l, "path"))} className="max-w-[260px] truncate">{a(l, "path")}</Cell>
                <Cell onClick={() => onAdd("httpStatus", a(l, "httpStatus"))} className={statusTone(st)}>{a(l, "httpStatus")}</Cell>
                <Cell>{a(l, "durationMs") ? `${a(l, "durationMs")}ms` : ""}</Cell>
                {cols.host && <Cell onClick={() => onAdd("host", a(l, "host"))}>{a(l, "host")}</Cell>}
                {cols.requestId && <Cell onClick={() => onAdd("requestId", a(l, "requestId"))}>{a(l, "requestId")}</Cell>}
                {cols.srcIp && <Cell onClick={() => onAdd("srcIp", a(l, "srcIp"))}>{a(l, "srcIp")}</Cell>}
                <td />
              </tr>
            );
          })}
        </tbody>
      </table>
      {more && <div className="px-2 pb-6 pt-3"><Button size="xs" variant="outline" icon={<ArrowDown size={14} />} onClick={more} className="!h-7 !px-3">Load more</Button></div>}
    </>
  );
}

function FlowTable({ lines, tz, onAdd }: { lines: LogLine[]; tz: string; onAdd: (k: string, v: string) => void }) {
  return (
    <table className="w-full min-w-[960px] border-collapse font-mono text-caption">
      <thead className="sticky top-0 z-[2] bg-surface-raised font-sans">
        <tr className="h-[38px] border-b border-line">
          {[["Time (" + tz + ")", "w-[170px] pl-6"], ["Dir", "w-[50px]"], ["Proto", "w-[60px]"], ["Source", "w-[160px]"], ["Destination", "w-[160px]"], ["Peer", "w-[120px]"], ["Traffic", "w-[80px] text-right pr-4"], ["Latency", "w-[70px] text-right pr-4"], ["Status", ""]].map(([h, c]) => (
            <th key={h} className={cx(th, c)}>{h}</th>
          ))}
        </tr>
      </thead>
      <tbody>
        {lines.map((l) => {
          const ingress = a(l, "dir") === "ingress";
          const svc = a(l, "peerKind") === "service";
          return (
            <tr key={l.id} className="text-fg-menu transition-colors hover:bg-tint-hover">
              <Cell className="pl-6 text-fg-2">{fmtSecond(l.ts)}</Cell>
              <Cell onClick={() => onAdd("dir", a(l, "dir"))}>
                <span title={ingress ? "Ingress" : "Egress"}>{ingress ? <ArrowDown size={14} className="text-blue-500" /> : <ArrowUp size={14} className="text-foreground" />}</span>
              </Cell>
              <Cell onClick={() => onAdd("protocol", a(l, "proto").toLowerCase())}>{a(l, "proto")}</Cell>
              <Cell className="max-w-[160px] truncate">{a(l, "src")}</Cell>
              <Cell className="max-w-[160px] truncate">{a(l, "dst")}</Cell>
              <Cell onClick={() => onAdd("peer", a(l, "peer"))}>
                <span className="inline-flex items-center gap-1.5 font-sans text-fg-2">{svc ? <Link2 size={13} /> : <Globe size={13} />}{svc ? a(l, "peer") || "Service" : "Internet"}</span>
              </Cell>
              <Cell className="pr-4 text-right">{fmtBytes(Number(a(l, "bytes")))}</Cell>
              <Cell className="pr-4 text-right">{a(l, "latencyMs") || "0"}ms</Cell>
              <Cell onClick={() => onAdd("status", a(l, "status"))} className={a(l, "status") === "ok" ? "text-ok-text" : "text-danger-text"}>{a(l, "status").toUpperCase()}</Cell>
            </tr>
          );
        })}
      </tbody>
    </table>
  );
}
