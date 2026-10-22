// SPDX-License-Identifier: AGPL-3.0-only

// Small building blocks shared by the settings, account and project
// settings screens.
import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import { NavLink } from "react-router-dom";
import { Check, Copy } from "lucide-react";
import { Button, Spinner, cx } from "../../ui/kit";

export function useLoad<T>(fn: () => Promise<T>, deps: unknown[]) {
  const [data, setData] = useState<T | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [n, setN] = useState(0);
  const alive = useRef(true);
  useEffect(() => {
    alive.current = true;
    setError(null);
    fn().then((d) => alive.current && setData(d)).catch((e) => alive.current && setError(e?.message || String(e)));
    return () => { alive.current = false; };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [...deps, n]);
  const reload = useCallback(() => setN((x) => x + 1), []);
  return { data, error, setData, reload };
}

/**
 * Optimistic mutation: apply the new state at once, send the request and
 * put the old state back with an error if it fails.
 */
export async function optimistic<S>(cur: S, next: S, set: (s: S) => void, send: () => Promise<unknown>, onError: (m: string) => void) {
  set(next);
  try { await send(); } catch (e: any) { set(cur); onError(e?.message || String(e)); }
}

export function Page({ title, nav, children }: { title: string; nav: { to: string; label: string }[]; children: ReactNode }) {
  return (
    <div className="h-full flex flex-col">
      <div className="px-4 md:px-12 py-6 md:py-8 border-b border-gray-200"><h1 className="text-2xl font-semibold">{title}</h1></div>
      <div className="flex-1 min-h-0 flex flex-col md:flex-row">
        <nav className="md:w-[240px] shrink-0 px-4 md:px-8 py-4 md:py-8 flex md:block gap-4 overflow-x-auto md:space-y-1">
          {nav.map((s) => (
            <NavLink key={s.to} to={s.to} className={({ isActive }) => cx("flex items-center md:w-full h-10 text-[15px] whitespace-nowrap", isActive ? "text-foreground font-medium" : "text-gray-600 hover:text-foreground")}>{s.label}</NavLink>
          ))}
        </nav>
        <div className="flex-1 min-w-0 overflow-y-auto scroll-thin px-4 md:px-6 py-6 md:py-8 max-w-[860px]">{children}</div>
      </div>
    </div>
  );
}

export function Section({ title, description, children, actions }: { title: string; description?: ReactNode; children: ReactNode; actions?: ReactNode }) {
  return (
    <section className="mb-10">
      <div className="flex items-start justify-between gap-4 mb-4">
        <div>
          <h2 className="text-xl font-semibold">{title}</h2>
          {description && <p className="text-sm text-gray-600 mt-1">{description}</p>}
        </div>
        {actions}
      </div>
      {children}
    </section>
  );
}

export function Field({ label, error, hint, children }: { label: string; error?: string | null; hint?: ReactNode; children: ReactNode }) {
  return (
    <label className="block mb-4">
      <span className="block text-sm text-gray-700 mb-1.5 font-medium">{label}</span>
      {children}
      {hint && !error && <span className="block text-xs text-gray-500 mt-1">{hint}</span>}
      {error && <span role="alert" className="block text-xs text-red-600 mt-1">{error}</span>}
    </label>
  );
}

const inputCls = "w-full h-10 px-3 rounded-md border border-gray-200 bg-transparent text-[15px] outline-none focus:border-gray-400";

export function Input(p: React.InputHTMLAttributes<HTMLInputElement>) {
  return <input {...p} className={cx(inputCls, p.className)} />;
}

export function Select({ options, ...p }: React.SelectHTMLAttributes<HTMLSelectElement> & { options: { value: string; label: string }[] }) {
  return <select {...p} className={cx(inputCls, p.className)}>{options.map((o) => <option key={o.value} value={o.value}>{o.label}</option>)}</select>;
}

export function TextArea(p: React.TextareaHTMLAttributes<HTMLTextAreaElement>) {
  return <textarea {...p} className={cx("w-full min-h-[90px] px-3 py-2 rounded-md border border-gray-200 bg-transparent text-sm font-mono outline-none focus:border-gray-400", p.className)} />;
}

export function Toggle({ checked, onChange, label, disabled }: { checked: boolean; onChange: (v: boolean) => void; label: ReactNode; disabled?: boolean }) {
  return (
    <label className={cx("flex items-center gap-3 mb-3 text-sm", disabled && "opacity-50")}>
      <input type="checkbox" checked={checked} disabled={disabled} onChange={(e) => onChange(e.target.checked)} className="h-4 w-4" />
      <span>{label}</span>
    </label>
  );
}

export function Notice({ kind = "info", children }: { kind?: "info" | "error" | "ok"; children: ReactNode }) {
  const c = { info: "border-gray-200 bg-gray-50 text-gray-700", error: "border-red-300 bg-red-100 text-red-700", ok: "border-green-300 bg-green-50 text-green-700" }[kind];
  return <div role={kind === "error" ? "alert" : "status"} className={cx("rounded-md border px-3 py-2 text-sm mb-4", c)}>{children}</div>;
}

export function CopyField({ value, mono = true }: { value: string; mono?: boolean }) {
  const [done, setDone] = useState(false);
  return (
    <div className={cx("min-h-10 px-3 py-2 flex items-center justify-between gap-3 rounded-md bg-secondaryBg text-sm break-all", mono && "font-mono")}>
      <span>{value}</span>
      <button type="button" aria-label="Copy" onClick={() => { void navigator.clipboard?.writeText(value); setDone(true); setTimeout(() => setDone(false), 1500); }} className="text-gray-500 hover:text-foreground shrink-0">
        {done ? <Check size={15} /> : <Copy size={15} />}
      </button>
    </div>
  );
}

/** Save button with busy state and a short "Saved" note. */
export function SaveRow({ busy, saved, error, disabled, label = "Save", extra }: { busy: boolean; saved: boolean; error: string | null; disabled?: boolean; label?: string; extra?: ReactNode }) {
  return (
    <div className="flex items-center gap-3 mt-2">
      <Button type="submit" variant="primary" disabled={busy || disabled}>{busy && <Spinner className="text-white" />}{label}</Button>
      {extra}
      {saved && !error && <span className="text-sm text-green-700">Saved</span>}
      {error && <span role="alert" className="text-sm text-red-600">{error}</span>}
    </div>
  );
}

export function Loading({ error }: { error?: string | null }) {
  if (error) return <Notice kind="error">{error}</Notice>;
  return <div className="py-10 flex justify-center"><Spinner /></div>;
}

/** Runs an async action with busy, error and saved flags for a form. */
export function useAction() {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [saved, setSaved] = useState(false);
  const run = useCallback(async <R,>(fn: () => Promise<R>): Promise<R | undefined> => {
    setBusy(true); setError(null); setSaved(false);
    try { const r = await fn(); setSaved(true); setTimeout(() => setSaved(false), 2500); return r; }
    catch (e: any) { setError(e?.message || String(e)); return undefined; }
    finally { setBusy(false); }
  }, []);
  return { busy, error, saved, run, setError };
}

export const fmtDate = (ms?: number) => (ms ? new Date(ms).toLocaleString() : "Never");
