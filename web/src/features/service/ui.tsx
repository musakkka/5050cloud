// SPDX-License-Identifier: AGPL-3.0-only

// Small primitives the WP8b features share. They follow the console kit's
// look; when WP8a's shared kit lands these can be swapped for it.

import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import { Check, Copy, Loader2, MoreVertical, X } from "lucide-react";
import { errText } from "./client";

export const cx = (...a: (string | false | null | undefined)[]) => a.filter(Boolean).join(" ");

export function timeAgo(ts?: number | null): string {
  if (!ts) return "";
  const s = Math.max(0, Math.round((Date.now() - ts) / 1000));
  if (s < 60) return `${s}s ago`;
  if (s < 3600) return `${Math.floor(s / 60)}m ago`;
  if (s < 86400) return `${Math.floor(s / 3600)}h ago`;
  return `${Math.floor(s / 86400)}d ago`;
}

export const fmtTime = (ts?: number | null) => (ts ? new Date(ts).toLocaleString() : "");

export function fmtBytes(b?: number | null, digits = 1): string {
  if (b === undefined || b === null || !isFinite(b)) return "";
  const u = ["B", "KB", "MB", "GB", "TB"];
  let i = 0;
  let v = b;
  while (v >= 1024 && i < u.length - 1) { v /= 1024; i++; }
  return `${v.toFixed(i === 0 ? 0 : digits)} ${u[i]}`;
}

/** Relative time with the absolute time as a tooltip. */
export function When({ ts }: { ts?: number | null }) {
  if (!ts) return null;
  return <span title={fmtTime(ts)}>{timeAgo(ts)}</span>;
}

/** Load data with a refresh function. `every` re-polls (never faster than needed). */
export function useLoad<T>(fn: () => Promise<T>, deps: unknown[], every = 0) {
  const [data, setData] = useState<T | undefined>(undefined);
  const [error, setError] = useState<string | null>(null);
  const fnRef = useRef(fn);
  fnRef.current = fn;
  const reload = useCallback(async () => {
    try { setData(await fnRef.current()); setError(null); } catch (e) { setError(errText(e)); }
  }, []);
  useEffect(() => {
    setData(undefined);
    void reload();
    if (!every) return;
    const t = setInterval(() => void reload(), every);
    return () => clearInterval(t);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps);
  return { data, error, reload, setData };
}

/** Run an async action with busy and error state. */
export function useAction() {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const run = useCallback(async (fn: () => Promise<unknown>) => {
    setBusy(true); setError(null);
    try { await fn(); return true; } catch (e) { setError(errText(e)); return false; } finally { setBusy(false); }
  }, []);
  return { busy, error, run, setError };
}

export function Button({ children, variant = "default", className, ...p }: React.ButtonHTMLAttributes<HTMLButtonElement> & { variant?: "default" | "primary" | "danger" | "ghost" }) {
  const v = {
    default: "border border-gray-200 hover:border-gray-400 text-foreground",
    primary: "bg-pink-500 hover:bg-pink-600 text-white border border-pink-500",
    danger: "bg-red-500 hover:bg-red-600 text-white border border-red-500",
    ghost: "text-gray-600 hover:text-foreground hover:bg-gray-100",
  }[variant];
  return (
    <button {...p} className={cx("inline-flex items-center justify-center gap-2 h-[34px] px-3 rounded-md text-sm transition-colors disabled:opacity-50 disabled:pointer-events-none", v, className)}>
      {children}
    </button>
  );
}

export function Input(p: React.InputHTMLAttributes<HTMLInputElement>) {
  return <input {...p} className={cx("h-[34px] w-full rounded-md border border-gray-200 bg-background px-3 text-sm outline-none focus:border-gray-400 disabled:opacity-60", p.className)} />;
}

export function Spinner({ className }: { className?: string }) {
  return <Loader2 size={18} className={cx("animate-spin text-gray-500", className)} />;
}

export function Loading() {
  return <div className="flex justify-center py-16"><Spinner /></div>;
}

export function ErrorLine({ children }: { children?: ReactNode }) {
  if (!children) return null;
  return <div role="alert" className="mt-3 rounded-md border border-red-300 bg-red-100 px-3 py-2 text-sm text-red-700">{children}</div>;
}

export function Empty({ children }: { children: ReactNode }) {
  return <div className="rounded-lg border border-dashed border-gray-200 px-6 py-8 text-center text-sm text-gray-600">{children}</div>;
}

export function CopyButton({ text, label }: { text: string; label?: string }) {
  const [done, setDone] = useState(false);
  return (
    <button type="button" aria-label={label || "Copy"} title="Copy" className="inline-flex items-center gap-1 text-gray-500 hover:text-foreground"
      onClick={() => { void navigator.clipboard?.writeText(text); setDone(true); setTimeout(() => setDone(false), 1200); }}>
      {done ? <Check size={14} /> : <Copy size={14} />}
    </button>
  );
}

export function Modal({ title, children, onClose, footer, width = 520 }: { title: string; children: ReactNode; onClose: () => void; footer?: ReactNode; width?: number }) {
  useEffect(() => {
    const k = (e: KeyboardEvent) => { if (e.key === "Escape") onClose(); };
    window.addEventListener("keydown", k);
    return () => window.removeEventListener("keydown", k);
  }, [onClose]);
  return (
    <div className="fixed inset-0 z-[100] bg-black/60 flex items-center justify-center p-4" onMouseDown={(e) => { if (e.target === e.currentTarget) onClose(); }}>
      <div role="dialog" aria-label={title} className="w-full max-h-[90vh] overflow-y-auto rounded-xl border border-gray-200 bg-secondaryBg shadow-xl" style={{ maxWidth: width }}>
        <div className="flex items-center justify-between px-6 pt-5 pb-3">
          <h3 className="text-lg font-semibold">{title}</h3>
          <button aria-label="Close" onClick={onClose} className="text-gray-500 hover:text-foreground"><X size={18} /></button>
        </div>
        <div className="px-6 pb-2 text-sm text-gray-700">{children}</div>
        {footer && <div className="flex justify-end gap-2 px-6 py-5">{footer}</div>}
      </div>
    </div>
  );
}

/** Confirm dialog. With `typeName`, the confirm button stays disabled until the name is typed. */
export function Confirm({ title, body, confirm, danger, typeName, onConfirm, onClose }: {
  title: string; body: ReactNode; confirm: string; danger?: boolean; typeName?: string;
  onConfirm: () => Promise<unknown>; onClose: () => void;
}) {
  const a = useAction();
  const [typed, setTyped] = useState("");
  const blocked = typeName !== undefined && typed !== typeName;
  return (
    <Modal title={title} onClose={onClose}
      footer={<>
        <Button onClick={onClose}>Cancel</Button>
        <Button variant={danger ? "danger" : "primary"} disabled={a.busy || blocked}
          onClick={async () => { if (await a.run(onConfirm)) onClose(); }}>
          {a.busy && <Loader2 size={14} className="animate-spin" />}{confirm}
        </Button>
      </>}>
      <div className="leading-6">{body}</div>
      {typeName !== undefined && (
        <label className="mt-4 block">
          <span className="text-gray-600">Type <b className="font-mono">{typeName}</b> to confirm</span>
          <Input className="mt-1" value={typed} onChange={(e) => setTyped(e.target.value)} autoFocus />
        </label>
      )}
      <ErrorLine>{a.error}</ErrorLine>
    </Modal>
  );
}

export function Tabs({ tabs, value, onChange, className }: { tabs: { id: string; label: string }[]; value: string; onChange: (id: string) => void; className?: string }) {
  return (
    <div role="tablist" className={cx("flex gap-6 overflow-x-auto border-b border-gray-200", className)}>
      {tabs.map((t) => (
        <button key={t.id} role="tab" aria-selected={value === t.id} onClick={() => onChange(t.id)}
          className={cx("relative whitespace-nowrap pb-3 text-[15px] transition-colors", value === t.id ? "text-foreground" : "text-gray-500 hover:text-gray-700")}>
          {t.label}
          {value === t.id && <span className="absolute left-0 right-0 -bottom-px h-0.5 rounded bg-foreground" />}
        </button>
      ))}
    </div>
  );
}

/** Three-dot menu. */
export function Menu({ items, label = "Actions" }: { items: { label: string; onClick: () => void; danger?: boolean; hidden?: boolean }[]; label?: string }) {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!open) return;
    const h = (e: MouseEvent) => { if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false); };
    document.addEventListener("mousedown", h);
    return () => document.removeEventListener("mousedown", h);
  }, [open]);
  const shown = items.filter((i) => !i.hidden);
  if (!shown.length) return null;
  return (
    <div ref={ref} className="relative inline-block">
      <button aria-label={label} onClick={() => setOpen((o) => !o)} className="p-1 rounded text-gray-500 hover:text-foreground hover:bg-gray-100"><MoreVertical size={16} /></button>
      {open && (
        <div role="menu" className="absolute right-0 z-50 mt-1 min-w-[180px] rounded-md border border-gray-200 bg-secondaryBg py-1 shadow-lg">
          {shown.map((i) => (
            <button key={i.label} role="menuitem" onClick={() => { setOpen(false); i.onClick(); }}
              className={cx("block w-full px-3 py-1.5 text-left text-sm hover:bg-gray-100", i.danger ? "text-red-600" : "text-foreground")}>{i.label}</button>
          ))}
        </div>
      )}
    </div>
  );
}

const LABEL_COLOR: Record<string, string> = {
  Active: "bg-green-100 text-green-700 border-green-300",
  Success: "bg-green-100 text-green-700 border-green-300",
  Completed: "bg-green-100 text-green-700 border-green-300",
  Building: "bg-blue-100 text-blue-700 border-blue-300",
  Deploying: "bg-blue-100 text-blue-700 border-blue-300",
  Initializing: "bg-blue-100 text-blue-700 border-blue-300",
  Waiting: "bg-yellow-100 text-yellow-700 border-yellow-300",
  Crashed: "bg-red-100 text-red-700 border-red-300",
  Failed: "bg-red-100 text-red-700 border-red-300",
};

/** Status badge with the label every client shows (Deployment.label). */
export function LabelBadge({ label }: { label: string }) {
  return (
    <span className={cx("inline-flex items-center h-6 px-2 rounded border text-[11px] font-medium tracking-wider uppercase whitespace-nowrap", LABEL_COLOR[label] || "bg-gray-100 text-gray-600 border-gray-300")}>
      {label}
    </span>
  );
}

/** A Railway control we do not have yet (13.6). */
export function NotInV01({ label, instead }: { label: string; instead: string }) {
  return (
    <div className="flex items-center justify-between gap-4 py-2 opacity-60" title={`Not in v0.1: ${instead}`}>
      <span className="text-sm">{label}</span>
      <span className="text-xs text-gray-500">Not in v0.1</span>
    </div>
  );
}

export function Section({ id, title, children, actions }: { id?: string; title: string; children: ReactNode; actions?: ReactNode }) {
  return (
    <section id={id} className="mb-10 scroll-mt-4">
      <div className="mb-3 flex items-center justify-between">
        <h3 className="text-base font-semibold">{title}</h3>
        {actions}
      </div>
      {children}
    </section>
  );
}
