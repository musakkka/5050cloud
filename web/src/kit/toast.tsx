// SPDX-License-Identifier: AGPL-3.0-only

import { useEffect, useState } from "react";
import { X } from "lucide-react";

export interface ToastItem { id: number; text: string; kind: "error" | "info" }

let next = 1;
const subs = new Set<(t: ToastItem[]) => void>();
let items: ToastItem[] = [];
const emit = () => subs.forEach((f) => f(items));

/** Shows a toast for six seconds. Optimistic mutations call it when they roll back. */
export function toast(text: string, kind: ToastItem["kind"] = "info") {
  const t = { id: next++, text, kind };
  items = [...items, t].slice(-4);
  emit();
  setTimeout(() => { items = items.filter((x) => x.id !== t.id); emit(); }, 6000);
}
export const toastError = (e: unknown) => toast(e instanceof Error ? e.message : String(e), "error");

/**
 * Runs an optimistic mutation: `apply` changes the UI at once, `send` talks to
 * the server, and on failure `rollback` restores the old state with a toast.
 */
export async function optimistic<R>(apply: () => void, send: () => Promise<R>, rollback: () => void): Promise<R | undefined> {
  apply();
  try { return await send(); } catch (e) { rollback(); toastError(e); return undefined; }
}

export function Toaster() {
  const [list, setList] = useState(items);
  useEffect(() => { subs.add(setList); return () => { subs.delete(setList); }; }, []);
  return (
    <div aria-live="polite" className="pointer-events-none fixed bottom-4 right-4 z-[300] flex w-[min(380px,calc(100vw-32px))] flex-col gap-2">
      {list.map((t) => (
        <div key={t.id} role={t.kind === "error" ? "alert" : "status"}
          className={`pointer-events-auto flex items-start gap-3 rounded-lg border px-4 py-3 text-sm shadow-lg ${t.kind === "error" ? "border-red-300 bg-red-100 text-red-700" : "border-gray-200 bg-secondaryBg text-gray-800"}`}>
          <span className="flex-1">{t.text}</span>
          <button type="button" aria-label="Dismiss" onClick={() => { items = items.filter((x) => x.id !== t.id); emit(); }}><X size={14} /></button>
        </div>
      ))}
    </div>
  );
}
