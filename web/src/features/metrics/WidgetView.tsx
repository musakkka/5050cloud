// SPDX-License-Identifier: AGPL-3.0-only

// One Observability widget: a chart card (one series per service) or a log panel.

import type { ReactNode } from "react";
import type { LogsResponse, Service } from "../../api/types.gen";
import { Skeleton, cx } from "../../kit";
import { get, qs } from "../service/client";
import { useLoad } from "../service/ui";
import { MetricChart, PALETTE } from "./chart";
import type { Loaded } from "./data";
import { buildServicesModel } from "./model";
import { typeOf, type Widget } from "./widgets";

export function serviceColor(services: Service[], id: string) {
  return PALETTE[Math.max(0, services.findIndex((s) => s.id === id)) % PALETTE.length];
}

export interface WidgetViewProps {
  widget: Pick<Widget, "metric" | "service" | "title">;
  eid: string;
  services: Service[];
  byId?: Record<string, Loaded>;
  error?: string | null;
  onRetry?: () => void;
  from: number;
  until: number;
  height?: number;
  tone?: "card" | "inset";
  actions?: ReactNode;
}

export function WidgetView(p: WidgetViewProps) {
  const t = typeOf(p.widget.metric);
  const title = p.widget.title || t.label;
  if (t.log) return <LogPanel {...p} title={title} type={t.log} />;
  const picked = p.widget.service ? p.services.filter((s) => s.id === p.widget.service) : p.services;
  const entries = picked.filter((s) => p.byId?.[s.id]).map((s) => ({ name: s.config.name, color: serviceColor(p.services, s.id), data: p.byId![s.id].data }));
  const model = buildServicesModel(p.widget.metric, entries, title);
  return (
    <MetricChart title={title} series={entries.length ? model.series : []} kind="line" minMax={model.minMax} fmtAxis={model.fmtAxis} fmtValue={model.fmtValue}
      from={p.from} until={p.until} height={p.height} tone={p.tone} actions={p.actions} loading={!p.byId && !p.error} error={!p.byId ? p.error : null} onRetry={p.onRetry} />
  );
}

const LEVEL: Record<string, string> = { error: "text-danger-text", warn: "text-warn", debug: "text-fg-4" };

function LogPanel({ title, type, eid, widget, services, height, tone, actions }: WidgetViewProps & { title: string; type: string }) {
  const q = qs({ environment: eid, services: widget.service || undefined, type, limit: 60 });
  const logs = useLoad(() => get<LogsResponse>(`/logs${q}`), [q], 15000);
  const names = Object.fromEntries(services.map((s) => [s.id, s.config.name]));
  return (
    <section aria-label={title} style={{ height: height ?? 362 }}
      className={cx("flex flex-col gap-2 rounded-lg border border-line px-4 pb-3 pt-4", tone === "inset" ? "bg-background" : "bg-surface-raised")}>
      <header className="flex min-h-[24px] items-center justify-between gap-3">
        <h3 className="truncate text-lead font-semibold text-foreground">{title}</h3>{actions}
      </header>
      <div className="scroll-thin min-h-0 flex-1 overflow-y-auto font-mono text-caption">
        {!logs.data && !logs.error ? <Skeleton className="h-full w-full" /> : logs.error ? <div role="alert" className="text-fg-3">{logs.error}</div>
          : logs.data!.lines.length === 0 ? <div className="flex h-full items-center justify-center font-sans text-body text-fg-3">No logs in this range</div>
          : logs.data!.lines.map((l) => (
            <div key={l.id} className="flex gap-3 whitespace-pre-wrap py-0.5 leading-5">
              <span className="shrink-0 text-fg-4">{new Date(l.ts).toLocaleTimeString([], { hour12: false })}</span>
              {!widget.service && l.serviceId && <span className="shrink-0 text-fg-3">{names[l.serviceId] ?? l.serviceId}</span>}
              <span className={cx("min-w-0 break-all", LEVEL[l.level] ?? "text-fg-menu")}>{l.msg}</span>
            </div>
          ))}
      </div>
    </section>
  );
}
