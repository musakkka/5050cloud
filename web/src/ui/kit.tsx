// SPDX-License-Identifier: AGPL-3.0-only

import { useCallback, useEffect, useLayoutEffect, useRef, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { AlertTriangle, Box, Check, CircleAlert, CircleCheck, CircleSlash, Github, Loader2, X } from "lucide-react";
import type { Status } from "../api";

export const cx = (...a: (string | false | null | undefined)[]) => a.filter(Boolean).join(" ");

export function timeAgo(ts?: number | null) {
  if (!ts) return "";
  const s = Math.max(0, Date.now() / 1000 - ts);
  if (s < 45) return "just now";
  const units: [number, string][] = [[60, "minute"], [3600, "hour"], [86400, "day"], [2592000, "month"], [31536000, "year"]];
  let out = "";
  for (let i = units.length - 1; i >= 0; i--) {
    const [n, name] = units[i];
    if (s >= n * 0.92) {
      const v = Math.max(1, Math.round(s / n));
      out = `${v} ${name}${v > 1 ? "s" : ""} ago`;
      break;
    }
  }
  return out || "just now";
}

export function fmtBytes(b?: number | null, digits = 1) {
  if (b == null) return "";
  const u = ["B", "KB", "MB", "GB", "TB"];
  let i = 0;
  while (b >= 1000 && i < u.length - 1) { b /= 1000; i++; }
  return `${b.toFixed(i ? digits : 0)} ${u[i]}`;
}

export const fmtDateTime = (ts: number) => {
  const d = new Date(ts * 1000);
  const p = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`;
};

export const tzLabel = () => {
  const off = -new Date().getTimezoneOffset() / 60;
  return `GMT${off >= 0 ? "+" : ""}${off}`;
};

export function usePoll<T>(fn: () => Promise<T>, deps: unknown[], every = 10000) {
  const [data, setData] = useState<T | null>(null);
  const [error, setError] = useState<string | null>(null);
  const tick = useRef(0);
  const [nonce, setNonce] = useState(0);
  useEffect(() => {
    let alive = true;
    const my = ++tick.current;
    const run = () => fn().then((d) => alive && my === tick.current && (setData(d), setError(null))).catch((e) => alive && setError(String(e.message || e)));
    run();
    const t = every ? setInterval(run, every) : undefined;
    return () => { alive = false; if (t) clearInterval(t); };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [...deps, nonce]);
  return { data, error, reload: () => setNonce((n) => n + 1), setData };
}

const DB_ICONS: Record<string, string> = { postgresql: "/icons/postgresql.svg", pgbouncer: "/icons/postgresql.svg", redis: "/icons/redis.svg", mysql: "/icons/mysql.svg" };

export function ResourceIcon({ icon, kind, source, size = 20 }: { icon?: string | null; kind?: string; source?: string; size?: number }) {
  const src = icon && (icon.startsWith("/") || icon.startsWith("http")) ? icon : DB_ICONS[icon || ""] || (kind && DB_ICONS[{ pg: "postgresql", postgres: "postgresql", redis: "redis", mysql: "mysql", pgbouncer: "pgbouncer" }[kind] || ""]);
  if (src) return <img src={src} alt="" width={size} height={size} className="shrink-0 object-contain" style={{ width: size, height: size }} />;
  if (source === "image") return <Box size={size} className="shrink-0 text-gray-700" strokeWidth={1.75} />;
  return <Github size={size} className="shrink-0 text-gray-700" strokeWidth={1.75} />;
}

export function StatusGlyph({ status, size = 20 }: { status: Status | string; size?: number }) {
  switch (status) {
    case "online":
    case "success":
      return <CircleCheck size={size} className="text-success shrink-0" strokeWidth={1.75} />;
    case "crashed":
    case "failed":
      return <CircleAlert size={size} className="text-red-600 shrink-0" strokeWidth={1.75} />;
    case "unhealthy":
    case "warning":
      return <AlertTriangle size={size} className="text-yellow-600 shrink-0" strokeWidth={1.75} />;
    case "deploying":
    case "progress":
    case "building":
      return <Loader2 size={size} className="text-blue-600 shrink-0 spin" strokeWidth={1.75} />;
    case "restarted":
      return <CircleCheck size={size} className="text-blue-600 shrink-0" strokeWidth={1.75} />;
    default:
      return <CircleSlash size={size} className="text-gray-500 shrink-0" strokeWidth={1.75} />;
  }
}

export const STATUS_TEXT: Record<string, string> = {
  online: "Online", deploying: "Deploying", crashed: "Crashed", unhealthy: "Unhealthy", removed: "Stopped", failed: "Failed",
};
export const STATUS_COLOR: Record<string, string> = {
  online: "text-green-600", deploying: "text-blue-600", crashed: "text-red-600", unhealthy: "text-yellow-600", removed: "text-gray-500", failed: "text-red-600",
};
export const DOT_COLOR: Record<string, string> = {
  online: "bg-green-500", deploying: "bg-blue-500", crashed: "bg-red-500", unhealthy: "bg-yellow-500", removed: "bg-gray-400", failed: "bg-red-500",
};

const BADGES: Record<string, string> = {
  active: "bg-green-100 text-green-600 border-green-300",
  success: "bg-green-100 text-green-600 border-green-300",
  removed: "bg-gray-100 text-gray-500 border-gray-200",
  crashed: "bg-red-100 text-red-600 border-red-300",
  failed: "bg-red-100 text-red-600 border-red-300",
  unhealthy: "bg-yellow-100 text-yellow-600 border-yellow-300",
  deploying: "bg-blue-100 text-blue-600 border-blue-300",
  building: "bg-blue-100 text-blue-600 border-blue-300",
};

export function Badge({ kind, children }: { kind: string; children?: ReactNode }) {
  return (
    <span className={cx("inline-flex items-center h-6 px-2 rounded border text-[11px] font-medium tracking-wider uppercase whitespace-nowrap", BADGES[kind] || BADGES.removed)}>
      {children ?? kind}
    </span>
  );
}

export function Button({ children, variant = "default", className, ...p }: React.ButtonHTMLAttributes<HTMLButtonElement> & { variant?: "default" | "primary" | "danger" | "ghost" }) {
  const v = {
    default: "border border-gray-200 hover:border-gray-400 text-foreground",
    primary: "bg-pink-500 hover:bg-pink-600 text-white border border-pink-500",
    danger: "bg-red-500 hover:bg-red-600 text-white border border-red-500",
    ghost: "text-gray-600 hover:text-foreground hover:bg-gray-100",
  }[variant];
  return (
    <button {...p} className={cx("inline-flex items-center justify-center gap-2 h-[34px] px-3 rounded-md text-sm transition-colors active:scale-[.97] disabled:opacity-50 disabled:pointer-events-none", v, className)}>
      {children}
    </button>
  );
}

export function useClickOutside(ref: React.RefObject<HTMLElement>, onOut: () => void, active: boolean) {
  useEffect(() => {
    if (!active) return;
    // a click in a floating panel (rendered in <body>) belongs to whatever opened it
    const h = (e: MouseEvent) => { if (ref.current && !ref.current.contains(e.target as Node) && !(e.target as Element).closest?.("[data-popover]")) onOut(); };
    const k = (e: KeyboardEvent) => { if (e.key === "Escape") onOut(); };
    document.addEventListener("mousedown", h);
    document.addEventListener("keydown", k);
    return () => { document.removeEventListener("mousedown", h); document.removeEventListener("keydown", k); };
  }, [active, onOut, ref]);
}

/** A floating panel anchored to its trigger. Rendered into <body> with fixed positioning, so no
 *  overflow-hidden parent (cards, the canvas frame, side panels) can clip it; flips above when short of room. */
export function Popover({ trigger, children, align = "left", minWidth = 180, className }: {
  trigger: (open: boolean) => ReactNode; children: (close: () => void) => ReactNode; align?: "left" | "right"; minWidth?: number; className?: string;
}) {
  const [open, setOpen] = useState(false);
  const [style, setStyle] = useState<React.CSSProperties>({});
  const anchor = useRef<HTMLDivElement>(null);
  const panel = useRef<HTMLDivElement>(null);
  const close = useCallback(() => setOpen(false), []);
  useLayoutEffect(() => {
    if (!open) return;
    const place = () => {
      const r = anchor.current?.getBoundingClientRect();
      if (!r) return;
      const vw = window.innerWidth, vh = window.innerHeight, gap = 4, edge = 8;
      const below = vh - r.bottom - gap - edge, above = r.top - gap - edge;
      const up = below < 220 && above > below;
      const pw = Math.max(panel.current?.offsetWidth || 0, minWidth, r.width);
      setStyle({
        position: "fixed", minWidth: Math.max(minWidth, r.width), maxWidth: Math.min(420, vw - 2 * edge),
        ...(align === "right" ? { right: Math.max(edge, vw - r.right) } : { left: Math.max(edge, Math.min(r.left, vw - pw - edge)) }),
        ...(up ? { bottom: vh - r.top + gap, maxHeight: above } : { top: r.bottom + gap, maxHeight: below }),
      });
    };
    place();
    const again = requestAnimationFrame(place);   // once more with the panel's real width
    window.addEventListener("resize", place);
    window.addEventListener("scroll", place, true);
    const down = (e: MouseEvent) => {
      const t = e.target as Node;
      if (!anchor.current?.contains(t) && !panel.current?.contains(t)) close();
    };
    const key = (e: KeyboardEvent) => e.key === "Escape" && close();
    document.addEventListener("mousedown", down);
    document.addEventListener("keydown", key);
    return () => {
      cancelAnimationFrame(again);
      window.removeEventListener("resize", place);
      window.removeEventListener("scroll", place, true);
      document.removeEventListener("mousedown", down);
      document.removeEventListener("keydown", key);
    };
  }, [open, align, minWidth, close]);
  return (
    <div ref={anchor} className="relative min-w-0" onClick={(e) => e.stopPropagation()}>
      <div onClick={() => setOpen((o) => !o)}>{trigger(open)}</div>
      {open && createPortal(
        // portal events still bubble through the React tree: keep clicks away from canvas nodes and links
        <div ref={panel} data-popover style={style} onClick={(e) => e.stopPropagation()} onMouseDown={(e) => e.stopPropagation()}
          className={cx("z-[200] overflow-y-auto scroll-thin rounded-lg border border-gray-200 bg-secondaryBg py-1.5 shadow-[0_10px_30px_rgba(0,0,0,.45)]", className)}>
          {children(close)}
        </div>,
        document.body,
      )}
    </div>
  );
}

export function MenuItem({ children, onClick, active, danger, disabled }: { children: ReactNode; onClick: () => void; active?: boolean; danger?: boolean; disabled?: boolean }) {
  return (
    <button disabled={disabled} onClick={onClick}
      className={cx("flex w-full items-center justify-between gap-3 px-3 py-1.5 text-left text-sm hover:bg-gray-100 disabled:opacity-40", danger ? "text-red-600" : "text-gray-800")}>
      <span className="truncate">{children}</span>{active && <Check size={14} className="shrink-0 text-pink-600" />}
    </button>
  );
}

export function Menu({ trigger, items, align = "right" }: { trigger: ReactNode; items: { label: string; onClick: () => void; danger?: boolean; disabled?: boolean }[]; align?: "left" | "right" }) {
  return (
    <Popover align={align} trigger={() => trigger}>
      {(close) => items.map((it) => (
        <MenuItem key={it.label} danger={it.danger} disabled={it.disabled} onClick={() => { close(); it.onClick(); }}>{it.label}</MenuItem>
      ))}
    </Popover>
  );
}

export function Modal({ title, children, onClose, footer, width = 480 }: { title: string; children: ReactNode; onClose: () => void; footer?: ReactNode; width?: number }) {
  const ref = useRef<HTMLDivElement>(null);
  useClickOutside(ref, onClose, true);
  return (
    <div className="fixed inset-0 z-[100] bg-black/60 flex items-center justify-center p-4">
      <div ref={ref} className="w-full rounded-xl border border-gray-200 bg-secondaryBg shadow-[0_10px_30px_rgba(0,0,0,.5)]" style={{ maxWidth: width }}>
        <div className="flex items-center justify-between px-6 pt-5 pb-3">
          <h3 className="text-lg font-semibold">{title}</h3>
          <button onClick={onClose} className="text-gray-500 hover:text-foreground"><X size={18} /></button>
        </div>
        <div className="px-6 pb-2 text-sm text-gray-700">{children}</div>
        {footer && <div className="flex justify-end gap-2 px-6 py-5">{footer}</div>}
      </div>
    </div>
  );
}

export function Confirm({ title, body, confirm, danger, onConfirm, onClose }: { title: string; body: ReactNode; confirm: string; danger?: boolean; onConfirm: () => Promise<unknown> | void; onClose: () => void }) {
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  return (
    <Modal title={title} onClose={onClose}
      footer={<>
        <Button onClick={onClose}>Cancel</Button>
        <Button variant={danger ? "danger" : "primary"} disabled={busy} onClick={async () => {
          setBusy(true); setErr(null);
          try { await onConfirm(); onClose(); } catch (e: any) { setErr(e.message || String(e)); setBusy(false); }
        }}>{busy && <Loader2 size={14} className="spin" />}{confirm}</Button>
      </>}>
      <div className="leading-6">{body}</div>
      {err && <div className="mt-3 rounded-md border border-red-300 bg-red-100 px-3 py-2 text-red-700">{err}</div>}
    </Modal>
  );
}

export function Tabs({ tabs, value, onChange, className }: { tabs: { id: string; label: string }[]; value: string; onChange: (id: string) => void; className?: string }) {
  return (
    <div className={cx("flex gap-6 border-b border-gray-200", className)}>
      {tabs.map((t) => (
        <button key={t.id} onClick={() => onChange(t.id)}
          className={cx("relative pb-3 text-[15px] transition-colors", value === t.id ? "text-foreground" : "text-gray-500 hover:text-gray-700")}>
          {t.label}
          {value === t.id && <span className="absolute left-0 right-0 -bottom-px h-0.5 rounded bg-foreground" />}
        </button>
      ))}
    </div>
  );
}

export function Empty({ children }: { children: ReactNode }) {
  return <div className="rounded-lg border border-dashed border-gray-200 px-6 py-8 text-center text-sm text-gray-600">{children}</div>;
}

export function Spinner({ className }: { className?: string }) {
  return <Loader2 size={18} className={cx("spin text-gray-500", className)} />;
}
