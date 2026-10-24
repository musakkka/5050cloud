// SPDX-License-Identifier: AGPL-3.0-only

// Small shared pieces for the database views: copy icon button, code box,
// and the framed section card used on the Stats and Config sub-tabs.

import { useEffect, useRef, useState, type ReactNode } from "react";
import { Check, Copy } from "lucide-react";
import { Tooltip, cx } from "../../kit";

/** 34x34 copy button. The icon turns into a check for about 2 seconds. */
export function CopyIconButton({ text, label = "Copy", className }: { text: string; label?: string; className?: string }) {
  const [done, setDone] = useState(false);
  const t = useRef<number>();
  useEffect(() => () => window.clearTimeout(t.current), []);
  return (
    <Tooltip content={done ? "Copied" : label} side="top">
      <button type="button" aria-label={label}
        onClick={() => {
          void navigator.clipboard?.writeText(text).then(() => {
            setDone(true);
            window.clearTimeout(t.current);
            t.current = window.setTimeout(() => setDone(false), 2000);
          });
        }}
        className={cx("inline-flex h-[34px] w-[34px] shrink-0 items-center justify-center rounded-md text-fg-3 transition-colors hover:bg-surface-hover hover:text-foreground", className)}>
        {done ? <Check size={16} className="text-ok-text" /> : <Copy size={16} />}
      </button>
    </Tooltip>
  );
}

/** A 52px mono code box with a copy button at the right. */
export function CodeBox({ text, children, compact, copy = text, extra }: { text: string; children?: ReactNode; compact?: boolean; copy?: string; extra?: ReactNode }) {
  return (
    <div className={cx("flex items-center gap-2 rounded-md bg-surface pl-[13px] pr-2", compact ? "h-12" : "h-[52px]")}>
      <code className={cx("min-w-0 flex-1 truncate bg-transparent font-mono text-foreground", compact ? "text-caption" : "text-body")} style={{ lineHeight: "24px" }}>
        {children ?? text}
      </code>
      {extra}
      <CopyIconButton text={copy} />
    </div>
  );
}

/** Stats and Config card: surface fill, r8. */
export function Panel({ title, hint, actions, children, className, danger }: { title?: ReactNode; hint?: ReactNode; actions?: ReactNode; children: ReactNode; className?: string; danger?: boolean }) {
  return (
    <section className={cx("rounded-lg bg-surface py-4", danger && "border border-danger-line", className)}>
      {(title || actions) && (
        <header className="flex items-center justify-between gap-3 px-4 pb-3">
          <div className="min-w-0">
            <h3 className="text-body font-medium text-foreground">{title}</h3>
            {hint && <p className="mt-0.5 text-caption text-fg-3">{hint}</p>}
          </div>
          {actions}
        </header>
      )}
      {children}
    </section>
  );
}
