// SPDX-License-Identifier: AGPL-3.0-only

// A small responsive line chart for host metrics: an area under the first series, a hover readout
// with a guide line, and quiet 12px axis labels. No chart library: the paths are plain SVG.
import { useLayoutEffect, useRef, useState, type PointerEvent } from "react";
import { cx } from "../../kit";

export interface ChartSeries {
  name: string;
  color: string;
  /** [timestamp, value] pairs, oldest first. Timestamps may be seconds or milliseconds. */
  points: number[][];
  /** Fill the area under the line (the total series). */
  area?: boolean;
}

const PAD = { top: 10, right: 12, bottom: 24, left: 64 };
const ms = (t: number) => (t > 1e12 ? t : t * 1000);

const nearest = (pts: number[][], t: number) => {
  let best: number[] | undefined;
  for (const p of pts) if (!best || Math.abs(p[0] - t) < Math.abs(best[0] - t)) best = p;
  return best;
};

export function LineChart({ series, format, yMax, height = 168, timeStyle = "time" }: {
  series: ChartSeries[];
  format: (v: number) => string;
  /** Fixed top of the scale (a limit). Defaults to the data with headroom. */
  yMax?: number;
  height?: number;
  timeStyle?: "time" | "date";
}) {
  const box = useRef<HTMLDivElement>(null);
  const [width, setWidth] = useState(0);
  const [hoverT, setHoverT] = useState<number | null>(null);
  useLayoutEffect(() => {
    const el = box.current;
    if (!el) return;
    const ro = new ResizeObserver(() => setWidth(el.clientWidth));
    ro.observe(el);
    setWidth(el.clientWidth);
    return () => ro.disconnect();
  }, []);

  const data = series.map((s) => ({ ...s, points: s.points.map((p) => [ms(p[0]), p[1]]) }));
  const all = data.flatMap((s) => s.points);
  if (!all.length) {
    return <div className="flex items-center justify-center text-caption text-fg-3" style={{ height }}>No data for this range yet.</div>;
  }
  const t0 = Math.min(...all.map((p) => p[0]));
  const t1 = Math.max(...all.map((p) => p[0]));
  const dataMax = Math.max(0, ...all.map((p) => p[1]));
  const vmax = yMax && yMax > 0 ? yMax : dataMax > 0 ? dataMax * 1.15 : 1;

  const W = width;
  const iw = Math.max(1, W - PAD.left - PAD.right);
  const ih = Math.max(1, height - PAD.top - PAD.bottom);
  const x = (t: number) => PAD.left + ((t - t0) / Math.max(1, t1 - t0)) * iw;
  const y = (v: number) => PAD.top + ih - (Math.min(v, vmax) / vmax) * ih;
  const bottom = PAD.top + ih;
  const path = (pts: number[][]) => pts.map((p, i) => `${i ? "L" : "M"}${x(p[0]).toFixed(1)} ${y(p[1]).toFixed(1)}`).join(" ");
  const area = (pts: number[][]) => (pts.length ? `${path(pts)} L${x(pts[pts.length - 1][0]).toFixed(1)} ${bottom} L${x(pts[0][0]).toFixed(1)} ${bottom} Z` : "");

  const yTicks = [0, 1, 2, 3].map((i) => (vmax * i) / 3);
  const xTicks = [t0, (t0 + t1) / 2, t1];
  const fmtT = (t: number) => (timeStyle === "date"
    ? new Date(t).toLocaleDateString([], { month: "short", day: "numeric" })
    : new Date(t).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" }));

  const onMove = (e: PointerEvent<SVGRectElement>) => {
    const r = e.currentTarget.getBoundingClientRect();
    const f = Math.min(1, Math.max(0, (e.clientX - r.left) / Math.max(1, r.width)));
    setHoverT(t0 + f * (t1 - t0));
  };
  const hover = hoverT === null ? null : data.map((s) => ({ s, p: nearest(s.points, hoverT) })).filter((h) => h.p);
  const hx = hover?.[0]?.p ? x(hover[0].p[0]) : 0;
  const tipLeft = Math.min(Math.max(hx + 12, 0), Math.max(0, W - 168));

  return (
    <div ref={box} className="relative w-full select-none" style={{ height }} onPointerLeave={() => setHoverT(null)}>
      {W > 0 && (
        <svg width={W} height={height} viewBox={`0 0 ${W} ${height}`} role="img" aria-label="Metric chart" className="overflow-visible">
          {yTicks.map((v, i) => (
            <g key={`y${i}`}>
              <line x1={PAD.left} x2={W - PAD.right} y1={y(v)} y2={y(v)} className="stroke-line" strokeWidth={1} strokeDasharray={i === 0 ? undefined : "2 4"} />
              <text x={PAD.left - 10} y={y(v)} dy="0.35em" textAnchor="end" className="fill-fg-3 font-mono text-caption">{format(v)}</text>
            </g>
          ))}
          {xTicks.map((t, i) => (
            <text key={`x${i}`} x={i === 0 ? PAD.left : i === 2 ? W - PAD.right : x(t)} y={height - 6}
              textAnchor={i === 0 ? "start" : i === 2 ? "end" : "middle"} className="fill-fg-3 font-mono text-caption">{fmtT(t)}</text>
          ))}
          {data.map((s, i) => s.area && s.points.length > 0 && (
            <path key={`a${i}`} d={area(s.points)} fill={s.color} fillOpacity={0.14} />
          ))}
          {data.map((s, i) => (
            <path key={`l${i}`} d={path(s.points)} fill="none" stroke={s.color} strokeWidth={s.area ? 1.5 : 1.25} strokeLinejoin="round" strokeLinecap="round" />
          ))}
          {hover && hoverT !== null && (
            <g>
              <line x1={hx} x2={hx} y1={PAD.top} y2={bottom} className="stroke-fg-5" strokeWidth={1} />
              {hover.map(({ s, p }) => p && (
                <circle key={s.name} cx={x(p[0])} cy={y(p[1])} r={3.5} fill={s.color} className="stroke-background" strokeWidth={2} />
              ))}
            </g>
          )}
          <rect x={PAD.left} y={PAD.top} width={iw} height={ih} fill="transparent" onPointerMove={onMove} />
        </svg>
      )}
      {hover && hover.length > 0 && hover[0].p && (
        <div className="pointer-events-none absolute top-2 z-10 min-w-[152px] rounded-md border border-line-popover bg-surface-raised px-3 py-2 text-caption shadow-popover"
          style={{ left: tipLeft }}>
          <div className="mb-1 font-mono text-fg-3">{fmtT(hover[0].p[0])}</div>
          {hover.map(({ s, p }) => p && (
            <div key={s.name} className="flex items-center justify-between gap-4 whitespace-nowrap text-fg-menu">
              <span className="flex items-center gap-1.5 text-fg-2"><span className={cx("h-2 w-2 rounded-full")} style={{ background: s.color }} />{s.name}</span>
              <span className="font-mono tabular-nums">{format(p[1])}</span>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
