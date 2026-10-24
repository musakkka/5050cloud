// SPDX-License-Identifier: AGPL-3.0-only

// Metric chart card: step lines or stacked bars drawn as plain SVG, a cursor that is synced
// across every chart inside a <CursorSync>, a portal tooltip, and a toggleable footer legend.

import { createContext, useCallback, useContext, useEffect, useLayoutEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { Skeleton, cx } from "../../kit";
import { bucketPoints, fmtStamp, niceTicks, timeTicks, valueAt } from "./series";

export const SERIES = {
  accent: "var(--accent)",
  blue: "var(--blue-500)",
  gold: "var(--yellow-400)",
  red: "var(--red-300)",
  teal: "hsl(172, 45%, 48%)",
  green: "var(--green-500)",
  gray: "var(--gray-400)",
};
/** Colour order for per-replica and per-service series. */
export const PALETTE = [SERIES.accent, SERIES.blue, SERIES.gold, SERIES.red, SERIES.teal, SERIES.green];

interface CursorState { ts: number | null; set: (t: number | null) => void }
const CursorCtx = createContext<CursorState | null>(null);

/** Wrap a group of charts so hovering one shows the cursor and tooltip on all of them. */
export function CursorSync({ children }: { children: ReactNode }) {
  const [ts, set] = useState<number | null>(null);
  const v = useMemo(() => ({ ts, set }), [ts]);
  return <CursorCtx.Provider value={v}>{children}</CursorCtx.Provider>;
}

export interface ChartSeries { key: string; name: string; color: string; points: number[][] }

export interface ChartProps {
  title: string;
  series: ChartSeries[];
  from: number;
  until: number;
  kind?: "line" | "bars";
  /** Smallest top of the y axis, so a flat zero line still gets a scale. */
  minMax: number;
  fmtAxis: (v: number, max: number) => string;
  fmtValue: (v: number) => string;
  /** Extra tooltip row (for example the plan limit). */
  limit?: { text: string };
  headerRight?: ReactNode;
  loading?: boolean;
  error?: string | null;
  onRetry?: () => void;
  markers?: { ts: number; label: string }[];
  onZoom?: (from: number, until: number) => void;
  height?: number;
  className?: string;
  actions?: ReactNode;
  /** `inset` draws the card on the page background (inside modals). */
  tone?: "card" | "inset";
}

const L = 58, R = 4, T = 24, B = 22;

function nearest(times: number[], ts: number): number | undefined {
  if (!times.length) return undefined;
  let lo = 0, hi = times.length - 1;
  while (lo < hi) {
    const mid = (lo + hi) >> 1;
    if (times[mid] < ts) lo = mid + 1; else hi = mid;
  }
  if (lo > 0 && ts - times[lo - 1] <= times[lo] - ts) lo--;
  return times[lo];
}

export function MetricChart(p: ChartProps) {
  const { from, until, kind = "line" } = p;
  const cursor = useContext(CursorCtx);
  const wrap = useRef<HTMLDivElement>(null);
  const svgRef = useRef<SVGSVGElement>(null);
  const [size, setSize] = useState({ w: 600, h: 260 });
  const [hidden, setHidden] = useState<Set<string>>(new Set());
  const [drag, setDrag] = useState<{ a: number; b: number } | null>(null);

  useEffect(() => {
    const el = wrap.current;
    if (!el) return;
    const ro = new ResizeObserver(() => setSize({ w: Math.max(120, el.clientWidth), h: Math.max(80, el.clientHeight) }));
    ro.observe(el);
    setSize({ w: Math.max(120, el.clientWidth), h: Math.max(80, el.clientHeight) });
    return () => ro.disconnect();
  }, [p.loading, p.error]);

  const plotW = Math.max(10, size.w - L - R);
  const plotH = Math.max(10, size.h - T - B);
  const span = Math.max(1, until - from);
  const bars = kind === "bars";
  const nBars = Math.max(1, Math.floor(plotW / 7));
  const bw = plotW / nBars;

  const visible = p.series.filter((s) => !hidden.has(s.key));
  const prepared = useMemo(() => visible.map((s) => ({
    ...s,
    pts: bars ? bucketPoints(s.points, from, until, nBars, "sum") : bucketPoints(s.points.filter((q) => q[0] >= from && q[0] <= until), from, until, 320),
  })), [visible, bars, from, until, nBars]);

  const times = useMemo(() => [...new Set(prepared.flatMap((s) => s.pts.map((q) => q[0])))].sort((a, b) => a - b), [prepared]);
  const hasData = p.series.some((s) => s.points.length > 0);

  const dataMax = useMemo(() => {
    if (!bars) return Math.max(0, ...prepared.flatMap((s) => s.pts.map((q) => q[1])));
    return Math.max(0, ...times.map((t) => prepared.reduce((a, s) => a + (s.pts.find((q) => q[0] === t)?.[1] ?? 0), 0)));
  }, [prepared, times, bars]);
  const ticks = useMemo(() => niceTicks(Math.max(dataMax, p.minMax), 4), [dataMax, p.minMax]);
  const stepV = ticks.length > 1 ? ticks[1] - ticks[0] : 1;
  const top = (ticks[ticks.length - 1] || 1) + stepV; // one unlabeled step of headroom
  const y = (v: number) => T + plotH - (Math.min(v, top) / top) * plotH;
  const x = (ts: number) => L + ((ts - from) / span) * plotW;

  // Cursor snapped to this chart's own timeline.
  const snap = cursor?.ts != null && cursor.ts >= from && cursor.ts <= until ? nearest(times, cursor.ts) : undefined;

  const move = useCallback((e: React.MouseEvent) => {
    const r = svgRef.current?.getBoundingClientRect();
    if (!r || !cursor) return;
    const ts = from + ((e.clientX - r.left - L) / plotW) * span;
    cursor.set(Math.min(until, Math.max(from, ts)));
    if (drag) setDrag({ a: drag.a, b: ts });
  }, [cursor, from, until, plotW, span, drag]);

  const finishDrag = () => {
    if (drag && p.onZoom && Math.abs(x(drag.b) - x(drag.a)) > 6) p.onZoom(Math.round(Math.min(drag.a, drag.b)), Math.round(Math.max(drag.a, drag.b)));
    setDrag(null);
  };

  const xt = useMemo(() => {
    if (bars) {
      if (!times.length) return [] as { ts: number; label: string; anchor: "start" | "end"; px: number }[];
      const long = span > 36 * 3600e3;
      const fmt = (t: number) => { const d = new Date(t); const z = (n: number) => String(n).padStart(2, "0"); return long ? `${d.getFullYear()}-${z(d.getMonth() + 1)}-${z(d.getDate())}` : `${z(d.getHours())}:${z(d.getMinutes())}`; };
      const f = times[0], l = times[times.length - 1];
      const out = [{ ts: f, label: fmt(f), anchor: "start" as "start" | "end" | "middle", px: x(f) }];
      if (l !== f) out.push({ ts: l, label: fmt(l), anchor: "end" as const, px: x(l) + bw });
      return out;
    }
    return timeTicks(from, until, Math.max(2, Math.floor(plotW / 72))).map((t) => ({ ...t, anchor: "middle" as "start" | "end" | "middle", px: x(t.ts) })).filter((t) => t.px > L + 12 && t.px < size.w - 16);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [bars, times, from, until, plotW, size.w, bw]);

  const tipRows = snap === undefined ? null : prepared.map((s) => ({ s, v: bars ? s.pts.find((q) => q[0] === snap)?.[1] : valueAt(s.pts, snap) })).filter((r) => r.v !== undefined && (!bars || (r.v as number) > 0));
  const barTotal = bars && tipRows ? tipRows.reduce((a, r) => a + (r.v as number), 0) : 0;
  const px = snap === undefined ? 0 : x(snap) + (bars ? bw / 2 : 0);

  return (
    <section className={cx("flex flex-col gap-2 rounded-lg border border-line px-4 pb-3 pt-4", p.tone === "inset" ? "bg-background" : "bg-surface-raised", p.className)} style={{ height: p.height ?? 362 }} aria-label={p.title}>
      <header className="flex min-h-[24px] items-center justify-between gap-3">
        <h3 className="truncate text-lead font-semibold text-foreground">{p.title}</h3>
        <div className="flex shrink-0 items-center gap-3">{p.headerRight}{p.actions}</div>
      </header>
      {p.loading ? (
        <div className="flex-1" aria-busy="true"><Skeleton className="h-full w-full" /></div>
      ) : p.error ? (
        <div role="alert" className="flex flex-1 flex-col items-center justify-center gap-2 text-body text-fg-3">
          <span className="max-w-sm text-center">{p.error}</span>
          {p.onRetry && <button className="rounded-md border border-line px-3 py-1 text-caption text-foreground transition hover:bg-surface-hover" onClick={p.onRetry}>Retry</button>}
        </div>
      ) : !hasData ? (
        <div className="flex flex-1 items-center justify-center text-body text-fg-3">No data in this range</div>
      ) : (
        <div ref={wrap} className="min-h-0 flex-1 select-none">
          <svg ref={svgRef} width={size.w} height={size.h} role="img" aria-label={`${p.title} chart`} className="block overflow-visible">
            <line x1={L} x2={size.w - R} y1={y(top)} y2={y(top)} stroke="var(--gray-100)" strokeWidth={1} />
            {ticks.map((t, i) => (
              <g key={t}>
                <line x1={L} x2={size.w - R} y1={y(t)} y2={y(t)} stroke={i === 0 ? "var(--gray-200)" : "var(--gray-100)"} strokeWidth={1} />
                <text x={0} y={y(t) - 4} style={{ fontSize: 11 }} className="fill-foreground">{p.fmtAxis(t, ticks[ticks.length - 1])}</text>
              </g>
            ))}
            {xt.map((t) => (
              <text key={t.ts} x={t.px} y={size.h - 4} textAnchor={t.anchor} style={{ fontSize: 11 }} className={cx("fill-foreground", bars && "font-mono")}>{t.label}</text>
            ))}
            {p.markers?.filter((m) => m.ts >= from && m.ts <= until).map((m) => (
              <g key={m.ts + m.label}>
                <line x1={x(m.ts)} x2={x(m.ts)} y1={T} y2={T + plotH} stroke="var(--accent)" strokeOpacity={0.5} strokeDasharray="2 3" />
                <circle cx={x(m.ts)} cy={T + 3} r={3} fill="var(--accent)"><title>{m.label}</title></circle>
              </g>
            ))}
            {bars ? (
              <g>
                {times.map((t) => {
                  const bx = x(t) + 1;
                  let acc = 0;
                  return prepared.map((s) => {
                    const v = s.pts.find((q) => q[0] === t)?.[1] ?? 0;
                    if (v <= 0) return null;
                    const y0 = y(acc), y1 = y(acc + v);
                    acc += v;
                    return <rect key={s.key + t} x={bx} y={y1} width={Math.max(1, bw - 2)} height={Math.max(0, y0 - y1)} fill={s.color} opacity={snap !== undefined && snap !== t ? 0.55 : 1} />;
                  });
                })}
              </g>
            ) : (
              prepared.map((s) => {
                let d = "";
                s.pts.forEach(([ts, v], i) => { d += i === 0 ? `M${x(ts).toFixed(1)} ${y(v).toFixed(1)}` : `H${x(ts).toFixed(1)}V${y(v).toFixed(1)}`; });
                const last = s.pts[s.pts.length - 1];
                if (last) d += `H${Math.min(size.w - R, x(Math.min(until, last[0] + (times.length > 1 ? (times[times.length - 1] - times[0]) / (times.length - 1) : 0)))).toFixed(1)}`;
                return <path key={s.key} d={d} fill="none" stroke={s.color} strokeWidth={2} strokeLinecap="round" strokeLinejoin="round" />;
              })
            )}
            {snap !== undefined && (
              <g pointerEvents="none">
                <line x1={px} x2={px} y1={T} y2={T + plotH} stroke="var(--gray-400)" strokeWidth={1} />
                {!bars && tipRows?.map((r) => <circle key={r.s.key} cx={px} cy={y(r.v as number)} r={4} fill={r.s.color} />)}
              </g>
            )}
            {drag && <rect x={Math.min(x(drag.a), x(drag.b))} y={T} width={Math.abs(x(drag.b) - x(drag.a))} height={plotH} fill="var(--accent)" opacity={0.15} pointerEvents="none" />}
            <rect x={L} y={T} width={plotW} height={plotH} fill="transparent" onMouseMove={move} onMouseLeave={() => { cursor?.set(null); setDrag(null); }}
              onMouseDown={(e) => { if (e.shiftKey && p.onZoom) { const r = svgRef.current!.getBoundingClientRect(); const ts = from + ((e.clientX - r.left - L) / plotW) * span; setDrag({ a: ts, b: ts }); } }}
              onMouseUp={finishDrag} />
          </svg>
          {snap !== undefined && tipRows && (
            <Tooltip svg={svgRef} px={px} stamp={fmtStamp(snap)}>
              {tipRows.map((r) => <TipRow key={r.s.key} color={r.s.color} name={r.s.name} value={p.fmtValue(r.v as number)} />)}
              {bars && tipRows.length > 0 && <><div className="my-1 h-px bg-line" /><TipRow name="Total" value={String(Math.round(barTotal))} /></>}
              {!bars && p.limit && <TipRow color="var(--gray-400)" name="Limit" value={p.limit.text} muted />}
            </Tooltip>
          )}
        </div>
      )}
      {!p.loading && !p.error && hasData && (
        <footer className="flex flex-wrap gap-x-3 gap-y-1 pl-7 text-caption text-fg-menu">
          {p.series.map((s) => {
            const off = hidden.has(s.key);
            return (
              <button key={s.key} type="button" aria-pressed={!off} onClick={() => setHidden((h) => { const n = new Set(h); if (n.has(s.key)) n.delete(s.key); else n.add(s.key); return n; })}
                className={cx("inline-flex items-center gap-2 rounded transition-opacity", off && "opacity-40")}>
                <span className="h-2 w-2 rounded-full" style={{ background: s.color }} />{s.name}
              </button>
            );
          })}
        </footer>
      )}
    </section>
  );
}

function TipRow({ color, name, value, muted }: { color?: string; name: string; value: string; muted?: boolean }) {
  return (
    <div className="flex h-5 items-center gap-2 text-body">
      <span className="h-2 w-2 shrink-0 rounded-full" style={{ background: color ?? "transparent" }} />
      <span className={cx("flex-1 whitespace-nowrap", muted ? "text-fg-2" : "text-foreground")}>{name}</span>
      <span className={cx("ml-6 whitespace-nowrap tabular-nums", muted ? "text-fg-2" : "text-foreground")}>{value}</span>
    </div>
  );
}

function Tooltip({ svg, px, stamp, children }: { svg: React.RefObject<SVGSVGElement>; px: number; stamp: string; children: ReactNode }) {
  const ref = useRef<HTMLDivElement>(null);
  const [pos, setPos] = useState<{ left: number; top: number } | null>(null);
  const r = svg.current?.getBoundingClientRect();
  useLayoutEffect(() => {
    const rect = svg.current?.getBoundingClientRect();
    const el = ref.current;
    if (!rect || !el) return;
    const w = el.offsetWidth, h = el.offsetHeight;
    const ax = rect.left + px;
    let left = ax + 12;
    if (left + w > window.innerWidth - 8) left = ax - 12 - w;
    const top = Math.min(window.innerHeight - h - 8, Math.max(8, rect.top + rect.height * 0.42));
    left = Math.max(8, left);
    setPos((p) => (p && Math.abs(p.left - left) < 0.5 && Math.abs(p.top - top) < 0.5 ? p : { left, top }));
  });
  const mid = r ? r.top + r.height * 0.5 : -1;
  if (!r || mid < 0 || mid > window.innerHeight) return null;
  return createPortal(
    <div ref={ref} role="tooltip" className="pointer-events-none fixed z-[120] min-w-[200px] rounded-lg border border-line bg-background px-3 py-2 shadow-popover"
      style={{ left: pos?.left ?? -9999, top: pos?.top ?? -9999 }}>
      <div className="mb-1 whitespace-nowrap text-caption text-fg-2">{stamp}</div>
      {children}
    </div>,
    document.body,
  );
}
