// SPDX-License-Identifier: AGPL-3.0-only

// Shared building blocks for the project settings sections.
import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import { Loader2 } from "lucide-react";
import { Skeleton, Tooltip, cx } from "../../kit";

export type Reload = () => void;

/** Fetch on mount and when deps change. Keeps the last data while reloading. */
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

/** Run an async action, return its result or undefined, and keep a busy and error flag. */
export function useAsync() {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const run = useCallback(async <R,>(fn: () => Promise<R>): Promise<R | undefined> => {
    setBusy(true); setError(null);
    try { return await fn(); } catch (e: any) { setError(e?.message || String(e)); return undefined; } finally { setBusy(false); }
  }, []);
  return { busy, error, setError, run };
}

/** Section heading: 20/600 title, 14px description 16px below, content 24px below. */
export function SectionHead({ title, description, actions, wide }: { title: ReactNode; description?: ReactNode; actions?: ReactNode; wide?: boolean }) {
  return (
    <div className="mb-6 flex items-start justify-between gap-4">
      <div className="min-w-0">
        <h2 className="text-h2 text-foreground">{title}</h2>
        {description && <p className={cx("mt-4 text-fg-2", wide ? "text-lead" : "text-body")}>{description}</p>}
      </div>
      {actions && <div className="shrink-0">{actions}</div>}
    </div>
  );
}

/** A block of a section page: the 48px rhythm between blocks comes from BlockDivider. */
export const BlockDivider = () => <div aria-hidden className="my-12 h-px bg-surface-hover" />;

export function FormLabel({ children, htmlFor }: { children: ReactNode; htmlFor?: string }) {
  return <label htmlFor={htmlFor} className="mb-2 block text-caption font-semibold text-fg-2">{children}</label>;
}

/** Inline link used inside copy: underlined, white. */
export function InlineLink({ href, children, external, onClick }: { href?: string; children: ReactNode; external?: boolean; onClick?: () => void }) {
  const cls = "underline underline-offset-2 text-foreground hover:text-fg-2 transition-colors";
  if (!href) return <button type="button" onClick={onClick} className={cls}>{children}</button>;
  return <a href={href} className={cls} {...(external ? { target: "_blank", rel: "noreferrer" } : {})}>{children}</a>;
}

/** A disabled control with the explanatory tooltip. */
export function WhyDisabled({ when, reason, children }: { when: boolean; reason: string; children: ReactNode }) {
  if (!when) return <>{children}</>;
  return <Tooltip content={reason}><span className="inline-flex">{children}</span></Tooltip>;
}

export function InlineError({ children }: { children?: ReactNode }) {
  if (!children) return null;
  return <p role="alert" className="mt-2 text-caption text-danger-text">{children}</p>;
}

/** Danger-style alert box (red outline, warning icon slot). */
export function AlertBox({ children, icon, className }: { children: ReactNode; icon: ReactNode; className?: string }) {
  return (
    <div role="alert" className={cx("flex items-start gap-3 rounded-md border border-danger bg-danger-bg px-4 py-2 text-body text-danger-text", className)}>
      <span className="mt-0.5 shrink-0 text-danger">{icon}</span>
      <div>{children}</div>
    </div>
  );
}

export function SectionSkeleton({ rows = 3 }: { rows?: number }) {
  return (
    <div aria-busy className="space-y-4">
      <Skeleton w={220} h={28} />
      <Skeleton w={420} h={20} />
      <div className="space-y-3 pt-4">{Array.from({ length: rows }, (_, i) => <Skeleton key={i} h={i === 0 ? 62 : 52} className="w-full rounded-lg" />)}</div>
    </div>
  );
}

export function LoadError({ error, onRetry }: { error: string; onRetry?: () => void }) {
  return (
    <div role="alert" className="rounded-md border border-danger bg-danger-bg px-4 py-3 text-body text-danger-text">
      {error}{onRetry && <button type="button" onClick={onRetry} className="ml-3 underline underline-offset-2">Try again</button>}
    </div>
  );
}

export const BusyIcon = () => <Loader2 size={16} className="spin" />;

export const downloadText = (name: string, text: string, type = "application/json") => {
  const url = URL.createObjectURL(new Blob([text], { type }));
  const a = document.createElement("a");
  a.href = url; a.download = name; a.click();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
};
