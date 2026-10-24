// SPDX-License-Identifier: AGPL-3.0-only

// Small primitives the service panel features share. They keep their old
// props (several tabs import them) but render the console kit, so every tab
// picks up the same tokens, radii and motion.

import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import { EllipsisVertical } from "lucide-react";
import {
  Button as KitButton, Confirm as KitConfirm, CopyButton as KitCopyButton, IconButton, Input as KitInput, Menu as KitMenu,
  Modal as KitModal, NotInV01 as KitNotInV01, Spinner as KitSpinner, cx as kcx, fmtBytes as kFmtBytes, timeAgo as kTimeAgo,
} from "../../kit";
import { errText } from "./client";
import { BADGE, lookOf } from "../deployment/format";

export const cx = kcx;
export const timeAgo = (ts?: number | null) => kTimeAgo(ts);
export const fmtTime = (ts?: number | null) => (ts ? new Date(ts > 1e12 ? ts : ts * 1000).toLocaleString() : "");
export const fmtBytes = (b?: number | null, digits = 1) => (b === undefined || b === null || !isFinite(b) ? "" : kFmtBytes(b, digits));

/** Relative time with the absolute time as a tooltip. */
export function When({ ts }: { ts?: number | null }) {
  if (!ts) return null;
  return <time title={fmtTime(ts)}>{timeAgo(ts)}</time>;
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

export function Button({ variant = "default", ...p }: React.ButtonHTMLAttributes<HTMLButtonElement> & { variant?: "default" | "primary" | "danger" | "ghost" }) {
  return <KitButton {...p} variant={variant} />;
}

export function Input({ size: _size, ...p }: React.InputHTMLAttributes<HTMLInputElement>) {
  return <KitInput size="sm" {...p} />;
}

export function Spinner({ className }: { className?: string }) {
  return <KitSpinner className={className} />;
}

export function Loading() {
  return <div className="flex justify-center py-16"><KitSpinner /></div>;
}

export function ErrorLine({ children }: { children?: ReactNode }) {
  if (!children) return null;
  return <div role="alert" className="mt-3 rounded-md border border-danger-line bg-danger-bg px-3 py-2 text-sm text-danger-text">{children}</div>;
}

export function Empty({ children }: { children: ReactNode }) {
  return <div className="rounded-md border border-dashed border-line px-6 py-8 text-center text-sm text-fg-3">{children}</div>;
}

export function CopyButton({ text, label }: { text: string; label?: string }) {
  return <KitCopyButton text={text} label={label || "Copy"} />;
}

export function Modal({ title, children, onClose, footer, width = 520 }: { title: string; children: ReactNode; onClose: () => void; footer?: ReactNode; width?: number }) {
  return <KitModal title={title} onClose={onClose} footer={footer} width={width}>{children}</KitModal>;
}

/** Confirm dialog. With `typeName`, the confirm button stays disabled until the name is typed. */
export function Confirm(p: {
  title: string; body: ReactNode; confirm: string; danger?: boolean; typeName?: string;
  onConfirm: () => Promise<unknown>; onClose: () => void;
}) {
  return <KitConfirm {...p} />;
}

/** Underline tabs: 14px, white active text with a 2px underline, muted idle text. */
export function Tabs({ tabs, value, onChange, className }: { tabs: { id: string; label: string }[]; value: string; onChange: (id: string) => void; className?: string }) {
  return (
    <div role="tablist" className={cx("no-scrollbar flex gap-4 overflow-x-auto border-b border-line", className)}>
      {tabs.map((t) => (
        <button key={t.id} type="button" role="tab" aria-selected={value === t.id} onClick={() => onChange(t.id)}
          className={cx("relative whitespace-nowrap pb-3 text-sm transition-colors duration-150", value === t.id ? "text-foreground" : "text-fg-3 hover:text-fg-2")}>
          {t.label}
          {value === t.id && <span className="absolute inset-x-0 -bottom-px h-0.5 bg-foreground" />}
        </button>
      ))}
    </div>
  );
}

/** Three-dot menu. */
export function Menu({ items, label = "Actions" }: { items: { label: string; onClick: () => void; danger?: boolean; hidden?: boolean }[]; label?: string }) {
  const shown = items.filter((i) => !i.hidden);
  if (!shown.length) return null;
  return (
    <KitMenu minWidth={220} trigger={<IconButton label={label} size={24}><EllipsisVertical size={16} strokeWidth={2.5} /></IconButton>}
      items={shown.map((i) => ({ label: i.label, onClick: i.onClick, danger: i.danger }))} />
  );
}

/** Status badge with the label every client shows (Deployment.label). */
export function LabelBadge({ label }: { label: string }) {
  const status = label.toUpperCase();
  const look = lookOf({ status: status === "ACTIVE" ? "SUCCESS" : status, active: status === "ACTIVE" });
  return (
    <span className={cx("inline-flex h-[26px] items-center whitespace-nowrap rounded-md px-2 text-caption font-medium uppercase", BADGE[look])}>{label}</span>
  );
}

/** A control we do not have yet (13.6). */
export function NotInV01({ label, instead }: { label: string; instead: string }) {
  return <div className="flex items-center justify-between gap-4 py-2"><KitNotInV01 label={label} instead={instead} /></div>;
}

export function Section({ id, title, children, actions }: { id?: string; title: string; children: ReactNode; actions?: ReactNode }) {
  return (
    <section id={id} className="mb-10 scroll-mt-4">
      <div className="mb-3 flex items-center justify-between">
        <h3 className="text-sm font-semibold text-foreground">{title}</h3>
        {actions}
      </div>
      {children}
    </section>
  );
}
