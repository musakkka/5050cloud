// SPDX-License-Identifier: AGPL-3.0-only

// Thin stacked severity histogram (30px) with axis labels, hover tooltip, dimming and drag-to-zoom.

import { useEffect, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { cx } from "../../kit";
import type { LogLine } from "../../api/types.gen";
import { LEVELS, LEVEL_BAR, bucketize, fmtClock, fmtDate, fmtStamp, tzLabel, type Level } from "./query";

const BAR_W = 7.46;
const STACK: Level[] = ["info", "debug", "warn", "error"];
const FILL: Record<Level, string> = { error: "bg-danger", warn: "bg-yellow-600", debug: "bg-gray-300", info: "bg-blue-600" };

export default function Histogram({ lines, from, to, onZoom }: { lines: LogLine[]; from: number; to: number; onZoom?: (from: number, to: number) => void }) {
  const wrap = useRef<HTMLDivElement>(null);
  const [w, setW] = useState(0);
  const [hover, setHover] = useState<{ i: number; x: number; y: number } | null>(null);
  const [drag, setDrag] = useState<{ a: number; b: number } | null>(null);
  const now = Date.now();

  useEffect(() => {
    const el = wrap.current;
    if (!el) return;
    const ro = new ResizeObserver(() => setW(el.clientWidth));
    ro.observe(el);
    setW(el.clientWidth);
    return () => ro.disconnect();
  }, []);

  const count = Math.max(8, Math.floor(w / BAR_W));
  const buckets = useMemo(() => bucketize(lines, from, to, count), [lines, from, to, count]);
  const max = Math.max(1, ...buckets.map((b) => b.total));
  const multiDay = to - from > 86_400_000;
  const idxAt = (clientX: number) => {
    const r = wrap.current!.getBoundingClientRect();
    return Math.max(0, Math.min(count - 1, Math.floor(((clientX - r.left) / r.width) * count)));
  };
  const hb = hover ? buckets[hover.i] : null;

  return (
    <div className="px-6 pb-2 pt-3">
      <div ref={wrap} className="group/hist relative flex h-[30px] cursor-crosshair select-none items-end" aria-label="Log volume" role="img"
        onMouseMove={(e) => { const i = idxAt(e.clientX); setHover({ i, x: e.clientX, y: e.clientY }); if (drag) setDrag({ ...drag, b: i }); }}
        onMouseLeave={() => { setHover(null); setDrag(null); }}
        onMouseDown={(e) => { const i = idxAt(e.clientX); setDrag({ a: i, b: i }); }}
        onMouseUp={() => {
          if (drag && drag.a !== drag.b && onZoom) {
            const lo = Math.min(drag.a, drag.b), hi = Math.max(drag.a, drag.b);
            onZoom(Math.round(buckets[lo].t), Math.round(buckets[hi].end));
          }
          setDrag(null);
        }}>
        {buckets.map((b, i) => {
          const future = b.t > now;
          const sel = drag && i >= Math.min(drag.a, drag.b) && i <= Math.max(drag.a, drag.b);
          return (
            <div key={i} className={cx("relative flex h-full flex-1 flex-col justify-end px-[2px] transition-opacity duration-150 group-hover/hist:opacity-55", hover?.i === i && "!opacity-100")}>
              {(hover?.i === i || sel) && <div className="pointer-events-none absolute inset-x-0 bottom-0 top-0 bg-gray-200 opacity-50" />}
              {future ? <div className="h-px w-full border-t border-dashed border-line" /> : b.total === 0 ? <div className="h-[3px] w-full rounded-t-sm bg-surface-hover" /> : (
                <div className="flex w-full flex-col-reverse overflow-hidden rounded-t-sm" style={{ height: `${Math.max(3, (b.total / max) * 100)}%` }}>
                  {STACK.map((l) => b.counts[l] > 0 && <div key={l} className={FILL[l]} style={{ flex: b.counts[l] }} />)}
                </div>
              )}
            </div>
          );
        })}
      </div>
      <div className="mt-px flex justify-between text-caption tabular-nums text-fg-2">
        <span>{multiDay ? fmtDate(from) : fmtClock(from, false)}</span>
        <span>{multiDay ? fmtDate(to) : fmtClock(to, false)}</span>
      </div>
      {hover && hb && createPortal(
        <div className="pointer-events-none fixed z-[300] w-[194px] rounded-lg border border-line bg-background px-3 py-2 animate-fade-in"
          style={{ left: Math.min(hover.x + 14, window.innerWidth - 210), top: hover.y + 18 }}>
          <div className="mb-1 text-caption text-fg-2 tabular-nums">{fmtDate(hb.t)} &middot; {fmtClock(hb.t, false)} {tzLabel()}</div>
          {LEVELS.slice().reverse().map((l) => (
            <div key={l} className={cx("flex items-center gap-2 text-sm", hb.counts[l] === 0 && "opacity-40")}>
              <span className={cx("h-2 w-2 rounded-full", LEVEL_BAR[l])} />
              <span className="text-fg-2">@level:</span><span className="text-foreground">{l}</span>
              <span className="ml-auto tabular-nums">{hb.counts[l]}</span>
            </div>
          ))}
          <span className="sr-only">{fmtStamp(hb.t)}</span>
        </div>, document.body)}
    </div>
  );
}
