// SPDX-License-Identifier: AGPL-3.0-only

// The console kit: one place for buttons, menus, modals, toasts, the status
// glyph, relative times, copy buttons and the theme. Every feature imports from here.
import { useCallback, useEffect, useLayoutEffect, useRef, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { Box, Check, Clock, Copy, Github, Loader2, X } from "lucide-react";
import { iconUrl } from "../api/client";
import "./theme.css";

export * from "./toast";
export * from "./theme";
export * from "./status";

export const cx = (...a: (string | false | null | undefined)[]) => a.filter(Boolean).join(" ");

/** Seconds or milliseconds since the epoch, normalised to seconds. */
const secs = (ts: number) => (ts > 1e12 ? ts / 1000 : ts);

export function timeAgo(ts?: number | null, now = Date.now()) {
  if (!ts) return "";
  const s = Math.max(0, now / 1000 - secs(ts));
  if (s < 45) return "just now";
  const units: [number, string][] = [[60, "minute"], [3600, "hour"], [86400, "day"], [2592000, "month"], [31536000, "year"]];
  for (let i = units.length - 1; i >= 0; i--) {
    const [n, name] = units[i];
    if (s >= n * 0.92) {
      const v = Math.max(1, Math.round(s / n));
      return `${v} ${name}${v > 1 ? "s" : ""} ago`;
    }
  }
  return "just now";
}

export const fmtDateTime = (ts: number) => new Date(secs(ts) * 1000).toLocaleString();

/** A relative time with the absolute time as its tooltip. */
export function RelTime({ ts }: { ts?: number | null }) {
  if (!ts) return null;
  return <time dateTime={new Date(secs(ts) * 1000).toISOString()} title={fmtDateTime(ts)}>{timeAgo(ts)}</time>;
}

export function fmtBytes(b?: number | null, digits = 1) {
  if (b == null) return "";
  const u = ["B", "KB", "MB", "GB", "TB"];
  let i = 0;
  while (b >= 1000 && i < u.length - 1) { b /= 1000; i++; }
  return `${b.toFixed(i ? digits : 0)} ${u[i]}`;
}

export function Button({ children, variant = "default", className, busy, ...p }: React.ButtonHTMLAttributes<HTMLButtonElement> & { variant?: "default" | "primary" | "danger" | "ghost" | "staged"; busy?: boolean }) {
  const v = {
    default: "border border-gray-200 hover:border-gray-400 text-foreground",
    primary: "bg-pink-500 hover:bg-pink-600 text-white border border-pink-500",
    staged: "bg-pink-500 hover:bg-pink-600 text-white border border-pink-500",
    danger: "bg-red-500 hover:bg-red-600 text-white border border-red-500",
    ghost: "text-gray-600 hover:text-foreground hover:bg-gray-100",
  }[variant];
  return (
    <button type="button" {...p} disabled={p.disabled || busy}
      className={cx("inline-flex items-center justify-center gap-2 h-[34px] px-3 rounded-md text-sm transition-colors active:scale-[.97] disabled:opacity-50 disabled:pointer-events-none", v, className)}>
      {busy && <Loader2 size={14} className="spin" />}{children}
    </button>
  );
}

export function Input({ className, ...p }: React.InputHTMLAttributes<HTMLInputElement>) {
  return <input {...p} className={cx("h-[38px] w-full rounded-md border border-gray-200 bg-background px-3 text-sm outline-none placeholder:text-gray-400 focus:border-pink-500", className)} />;
}

export function Textarea({ className, ...p }: React.TextareaHTMLAttributes<HTMLTextAreaElement>) {
  return <textarea {...p} className={cx("w-full rounded-md border border-gray-200 bg-background px-3 py-2 font-mono text-sm outline-none focus:border-pink-500", className)} />;
}

export function Field({ label, hint, error, children }: { label: string; hint?: ReactNode; error?: string; children: ReactNode }) {
  // the hint sits outside <label> so the control's accessible name is just the label
  return (
    <div className="space-y-1.5">
      <label className="block space-y-1.5">
        <span className="block text-sm font-medium text-gray-800">{label}</span>
        {children}
      </label>
      {hint && !error && <span className="block text-xs text-gray-500">{hint}</span>}
      {error && <span role="alert" className="block text-xs text-red-600">{error}</span>}
    </div>
  );
}

export function ErrorLine({ error }: { error?: unknown }) {
  if (!error) return null;
  const msg = error instanceof Error ? error.message : String(error);
  return <div role="alert" className="rounded-md border border-red-300 bg-red-100 px-3 py-2 text-sm text-red-700">{msg}</div>;
}

/** Copies text with a check mark for a second. Used on every id, URL, record and command. */
export function CopyButton({ text, label = "Copy", className }: { text: string; label?: string; className?: string }) {
  const [done, setDone] = useState(false);
  return (
    <button type="button" aria-label={label} title={label} className={cx("inline-flex h-6 w-6 items-center justify-center rounded text-gray-500 hover:bg-gray-100 hover:text-foreground", className)}
      onClick={(e) => {
        e.stopPropagation();
        void navigator.clipboard?.writeText(text).then(() => { setDone(true); setTimeout(() => setDone(false), 1200); });
      }}>
      {done ? <Check size={13} /> : <Copy size={13} />}
    </button>
  );
}

/** A service or template icon: a known key, an /icons/ path, or the source kind as a fallback. */
export function ServiceIcon({ icon, source, size = 20 }: { icon?: string; source?: string; size?: number }) {
  const src = iconUrl(icon);
  const [broken, setBroken] = useState(false);
  if (src && !broken) return <img src={src} alt="" width={size} height={size} onError={() => setBroken(true)} className="shrink-0 object-contain" style={{ width: size, height: size }} />;
  if (source === "image") return <Box size={size} className="shrink-0 text-gray-700" strokeWidth={1.75} />;
  if (source === "cron") return <Clock size={size} className="shrink-0 text-gray-700" strokeWidth={1.75} />;
  return <Github size={size} className="shrink-0 text-gray-700" strokeWidth={1.75} />;
}

/** A Railway control we do not have yet, shown disabled with the reason (SPEC 13.6). */
export function NotInV01({ label, instead }: { label: string; instead: string }) {
  return (
    <span title={`Not in v0.1: ${instead}`} aria-disabled="true" className="inline-flex cursor-not-allowed items-center gap-2 text-sm text-gray-400">
      {label}<span className="rounded border border-gray-200 px-1.5 text-[10px] uppercase tracking-wider">Not in v0.1</span>
    </span>
  );
}

export function Skeleton({ className }: { className?: string }) {
  return <div aria-hidden className={cx("animate-pulse rounded-md bg-gray-100", className)} />;
}

/** Type-the-name confirm, only for destructive actions. */
export function Confirm({ title, body, confirm, danger, typeName, onConfirm, onClose }: {
  title: string; body: ReactNode; confirm: string; danger?: boolean; typeName?: string; onConfirm: () => Promise<unknown> | void; onClose: () => void;
}) {
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<unknown>(null);
  const [typed, setTyped] = useState("");
  return (
    <Modal title={title} onClose={onClose}
      footer={<>
        <Button onClick={onClose}>Cancel</Button>
        <Button variant={danger ? "danger" : "primary"} busy={busy} disabled={!!typeName && typed !== typeName} onClick={async () => {
          setBusy(true); setErr(null);
          try { await onConfirm(); onClose(); } catch (e) { setErr(e); setBusy(false); }
        }}>{confirm}</Button>
      </>}>
      <div className="space-y-3 leading-6">
        <div>{body}</div>
        {typeName && <Field label={`Type ${typeName} to confirm`}><Input autoFocus value={typed} onChange={(e) => setTyped(e.target.value)} /></Field>}
        <ErrorLine error={err} />
      </div>
    </Modal>
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
      <div ref={ref} role="dialog" aria-modal="true" aria-label={title} className="w-full max-h-[90vh] overflow-y-auto rounded-xl border border-gray-200 bg-secondaryBg shadow-[0_10px_30px_rgba(0,0,0,.5)]" style={{ maxWidth: width }}>
        <div className="flex items-center justify-between px-6 pt-5 pb-3">
          <h3 className="text-lg font-semibold">{title}</h3>
          <button type="button" aria-label="Close" onClick={onClose} className="text-gray-500 hover:text-foreground"><X size={18} /></button>
        </div>
        <div className="px-6 pb-2 text-sm text-gray-700">{children}</div>
        {footer && <div className="flex justify-end gap-2 px-6 py-5">{footer}</div>}
      </div>
    </div>
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
export { Logo } from "./Logo";
