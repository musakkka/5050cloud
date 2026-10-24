// SPDX-License-Identifier: AGPL-3.0-only

import { useEffect, useState } from "react";
import { AlertCircle, CircleCheck, Info, X } from "lucide-react";

export type ToastKind = "error" | "info" | "success";
export interface ToastItem { id: number; text: string; kind: ToastKind; action?: { label: string; onClick: () => void } }

let next = 1;
const subs = new Set<(t: ToastItem[]) => void>();
let items: ToastItem[] = [];
const emit = () => subs.forEach((f) => f(items));
const dismiss = (id: number) => { items = items.filter((x) => x.id !== id); emit(); };

/** Shows a toast for six seconds. Optimistic mutations call it when they roll back. */
export function toast(text: string, kind: ToastKind = "info", action?: ToastItem["action"]) {
  const t = { id: next++, text, kind, action };
  items = [...items, t].slice(-4);
  emit();
  setTimeout(() => dismiss(t.id), 6000);
}
export const toastError = (e: unknown) => toast(e instanceof Error ? e.message : String(e), "error");
export const toastSuccess = (text: string) => toast(text, "success");

/**
 * Runs an optimistic mutation: `apply` changes the UI at once, `send` talks to
 * the server, and on failure `rollback` restores the old state with a toast.
 */
export async function optimistic<R>(apply: () => void, send: () => Promise<R>, rollback: () => void): Promise<R | undefined> {
  apply();
  try { return await send(); } catch (e) { rollback(); toastError(e); return undefined; }
}

const ICON = {
  error: <AlertCircle size={20} className="shrink-0 text-danger-text" />,
  success: <CircleCheck size={20} className="shrink-0 text-ok-text" />,
  info: <Info size={20} className="shrink-0 text-fg-3" />,
};

/** Bottom-right stack of toasts: raised surface, hairline border, 8px radius, rise-in motion. */
export function Toaster() {
  const [list, setList] = useState(items);
  useEffect(() => { subs.add(setList); return () => { subs.delete(setList); }; }, []);
  return (
    <div aria-live="polite" className="pointer-events-none fixed bottom-14 right-6 z-[100000] flex w-[min(320px,calc(100vw-48px))] flex-col gap-2 transition-[right] duration-200 ease-out">
      {list.map((t) => (
        <div key={t.id} role={t.kind === "error" ? "alert" : "status"}
          className={`pointer-events-auto flex animate-toast-in items-center gap-4 rounded-lg border bg-surface-hover p-4 text-foreground shadow-[0_2px_12px_rgba(0,0,0,.04)] ${t.kind === "error" ? "border-danger-line" : t.kind === "success" ? "border-ok-soft" : "border-line"}`}>
          {ICON[t.kind]}
          <span className="min-w-0 flex-1 break-words text-lead font-medium">{t.text}</span>
          {t.action && <button type="button" className="shrink-0 font-medium text-accent-text hover:underline" onClick={() => { t.action!.onClick(); dismiss(t.id); }}>{t.action.label}</button>}
          <button type="button" aria-label="Dismiss" className="flex size-5 shrink-0 items-center justify-center rounded-md text-fg-3 transition-colors duration-150 hover:text-foreground" onClick={() => dismiss(t.id)}><X size={16} /></button>
        </div>
      ))}
    </div>
  );
}
