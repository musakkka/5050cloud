// SPDX-License-Identifier: AGPL-3.0-only

// Small building blocks shared by the settings, account and project
// settings screens.
import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import { Link, NavLink, useLocation } from "react-router-dom";
import { Check, Copy, type LucideIcon } from "lucide-react";
import { useShellOptions } from "../../app/Shell";
import { Button, Input as KitInput, Notice as KitNotice, Select as KitSelect, ShimmerCard, Switch, Textarea, cx } from "../../kit";

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

export interface PageNavItem { to: string; label: string; icon?: LucideIcon }

/**
 * Settings page frame. With `aside` (account) it draws the 200px nav under a 28px
 * muted group title, which folds into a bordered grid card under 1024px. Without it
 * (install pages, whose nav lives in the sidebar) it is the 872px column.
 */
export function Page({ title, nav, children, heading, aside = true, wide }: { title: string; nav: PageNavItem[]; children: ReactNode; heading?: string; aside?: boolean; wide?: boolean }) {
  const { pathname } = useLocation();
  useShellOptions({ width: aside || wide ? "content" : "narrow" });
  const current = nav.find((n) => pathname === n.to || pathname.startsWith(n.to + "/"));
  const h = heading || current?.label || title;
  const column = (
    <div className={cx("min-w-0 flex-1 space-y-12", !aside && !wide && "mx-auto w-full max-w-narrow")}>
      {aside && nav.length > 0 && <MobileNav nav={nav} pathname={pathname} />}
      <div>
        <h1 className="text-title text-foreground">{h}</h1>
        <hr className="mt-4 border-0 border-t border-gray-100" />
      </div>
      <div className="animate-rise-in space-y-12" key={pathname}>{children}</div>
    </div>
  );
  if (!aside) return column;
  return (
    <div className="flex">
      <aside className="mr-12 hidden w-[200px] shrink-0 lg:block">
        <h2 className="mb-8 text-title text-fg-4">{title}</h2>
        <nav className="flex flex-col gap-1" aria-label={title}>
          {nav.map((n) => (
            <NavLink key={n.to} to={n.to}
              className={({ isActive }) => cx("flex h-nav items-center gap-3 rounded-lg px-3 text-body transition-colors duration-150",
                isActive ? "bg-tint-active font-medium text-foreground" : "text-fg-2 hover:bg-tint-hover hover:text-foreground")}>
              {({ isActive }) => (<>{n.icon && <n.icon size={16} strokeWidth={2} className={cx("shrink-0 transition-colors", isActive ? "text-accent-text" : "text-gray-300")} />}<span className="truncate">{n.label}</span></>)}
            </NavLink>
          ))}
        </nav>
      </aside>
      {column}
    </div>
  );
}

function MobileNav({ nav, pathname }: { nav: PageNavItem[]; pathname: string }) {
  return (
    <nav className="grid grid-cols-2 overflow-hidden rounded-lg border border-line sm:grid-cols-4 lg:hidden" aria-label="Sections">
      {nav.map((n) => {
        const on = pathname === n.to || pathname.startsWith(n.to + "/");
        return (
          <Link key={n.to} to={n.to} className={cx("flex h-11 items-center gap-2 border-b border-r border-gray-100 px-3 text-body transition-colors", on ? "bg-tint-active font-medium text-foreground" : "text-fg-2 hover:bg-tint-hover")}>
            {n.icon && <n.icon size={16} className={on ? "text-accent-text" : "text-gray-300"} />}<span className="truncate">{n.label}</span>
          </Link>
        );
      })}
    </nav>
  );
}

/** A titled block: 20px/600 title, 32px gap to the body. */
export function Section({ title, description, children, actions }: { title: ReactNode; description?: ReactNode; children: ReactNode; actions?: ReactNode }) {
  return (
    <section className="space-y-8">
      <div className="flex items-start justify-between gap-4">
        <div className="min-w-0">
          <h2 className="text-h2 text-foreground">{title}</h2>
          {description && <p className="mt-1 max-w-3xl text-body text-fg-2">{description}</p>}
        </div>
        {actions}
      </div>
      <div>{children}</div>
    </section>
  );
}

export function Field({ label, error, hint, children, className }: { label: ReactNode; error?: string | null; hint?: ReactNode; children: ReactNode; className?: string }) {
  return (
    <div className={cx("mb-5 w-full", className)}>
      <label className="block">
        <span className="mb-2 block text-caption font-semibold text-fg-2">{label}</span>
        {children}
      </label>
      {hint && !error && <span className="mt-2 block text-caption text-fg-3">{hint}</span>}
      {error && <span role="alert" className="mt-2 block text-caption text-danger-text">{error}</span>}
    </div>
  );
}

/** Two columns from md up, 24px gaps. */
export function FormGrid({ children }: { children: ReactNode }) {
  return <div className="grid grid-cols-1 gap-x-6 md:grid-cols-2">{children}</div>;
}

export function Input({ size: _size, ...p }: React.InputHTMLAttributes<HTMLInputElement>) {
  return <KitInput {...p} />;
}

export function Select({ options, ...p }: React.SelectHTMLAttributes<HTMLSelectElement> & { options: { value: string; label: string }[] }) {
  return <KitSelect variant="field" options={options} {...p} />;
}

export function TextArea(p: React.TextareaHTMLAttributes<HTMLTextAreaElement>) {
  return <Textarea {...p} className={cx("min-h-[96px]", p.className)} />;
}

/** A switch row on a raised bar (the profile "enable" row look). */
export function Toggle({ checked, onChange, label, disabled }: { checked: boolean; onChange: (v: boolean) => void; label: ReactNode; disabled?: boolean }) {
  return (
    <div className={cx("mb-3 flex items-center gap-3 rounded-md bg-surface-raised px-4 py-3 text-lead", disabled && "opacity-50")}>
      <Switch size="sm" checked={checked} disabled={disabled} onChange={onChange} label={typeof label === "string" ? label : undefined} />
      <span>{label}</span>
    </div>
  );
}

export function Notice({ kind = "info", children }: { kind?: "info" | "error" | "ok"; children: ReactNode }) {
  return <KitNotice className="mb-4" tone={{ info: "neutral", error: "danger", ok: "ok" }[kind] as "neutral" | "danger" | "ok"}>{children}</KitNotice>;
}

export function CopyField({ value, mono = true }: { value: string; mono?: boolean }) {
  const [done, setDone] = useState(false);
  return (
    <div className={cx("flex min-h-[42px] items-center justify-between gap-3 break-all rounded-md bg-surface-raised px-3 py-2 text-body", mono && "font-mono")}>
      <span className="whitespace-pre-wrap">{value}</span>
      <button type="button" aria-label="Copy" onClick={() => { void navigator.clipboard?.writeText(value); setDone(true); setTimeout(() => setDone(false), 1500); }}
        className="shrink-0 rounded p-1 text-fg-3 transition-colors hover:text-foreground">
        {done ? <Check size={16} className="text-ok-text" /> : <Copy size={16} />}
      </button>
    </div>
  );
}

/** Save button with busy state and a short "Saved" note. Primary 34px, full width on phones. */
export function SaveRow({ busy, saved, error, disabled, label = "Save", extra }: { busy: boolean; saved: boolean; error: string | null; disabled?: boolean; label?: string; extra?: ReactNode }) {
  return (
    <div className="mt-2 flex flex-wrap items-center gap-3">
      <Button type="submit" variant="primary" busy={busy} disabled={disabled} className="w-full md:w-auto">{label}</Button>
      {extra}
      {saved && !error && <span role="status" className="flex animate-fade-in items-center gap-1 text-body text-ok-text"><Check size={16} />Saved</span>}
      {error && <span role="alert" className="text-body text-danger-text">{error}</span>}
    </div>
  );
}

/** Shimmer placeholders while a section loads; the error notice on failure. */
export function Loading({ error }: { error?: string | null }) {
  if (error) return <Notice kind="error">{error}</Notice>;
  return <div className="space-y-3" aria-busy="true" aria-label="Loading"><ShimmerCard h={42} /><ShimmerCard h={42} /><ShimmerCard h={110} /></div>;
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
