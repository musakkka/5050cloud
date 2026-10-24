// SPDX-License-Identifier: AGPL-3.0-only

// Building blocks for the long settings pages (service Settings and the
// volume panel): a section with a round icon on a timeline, a described
// field, toggle rows, "+ Label" buttons that turn into inputs, select cards,
// inline panels, pills and the filter context that hides fields.

import { createContext, useContext, useEffect, useRef, useState, type ReactNode } from "react";
import { ArrowUpRight, Check, ChevronDown, Plus } from "lucide-react";
import { Button, Popover, Tooltip, cx } from "../../kit";
import { LINKS } from "../../app/links";

/* ---------- filter ---------- */

interface FilterCtx { q: string; sectionMatch: boolean }
const Filter = createContext<FilterCtx>({ q: "", sectionMatch: false });
export const FilterProvider = Filter.Provider;
export const useFilter = () => useContext(Filter);

/** True when a field with this title and keywords should show for the filter. */
export function matches(q: string, ...terms: (string | undefined)[]) {
  if (!q) return true;
  return terms.some((t) => t && t.toLowerCase().includes(q));
}

/* ---------- docs ---------- */

export const docs = (page: string) => `${LINKS.docs}/${page}`;

/** An underlined "Docs" link with the trailing arrow, inline in a description. */
export function DocsLink({ href, children = "Docs" }: { href: string; children?: ReactNode }) {
  return (
    <a href={href} target="_blank" rel="noreferrer" className="inline-flex items-baseline gap-0.5 text-inherit underline decoration-fg-4 underline-offset-2 transition-colors hover:text-foreground hover:decoration-fg-2">
      {children}<ArrowUpRight size={13} className="relative top-[2px] shrink-0" />
    </a>
  );
}

/* ---------- section ---------- */

export interface SectionDef { id: string; label: string; icon: ReactNode; danger?: boolean }

/**
 * A settings section: 40px round icon on the timeline, a 20px heading, then its fields 32px apart.
 * Hidden (but mounted) when the filter leaves it with no fields.
 */
export function SettingsSection({ def, children, hidden, prefix = "settings" }: { def: SectionDef; children: ReactNode; hidden?: boolean; prefix?: string }) {
  const { q } = useFilter();
  const sectionMatch = !!q && def.label.toLowerCase().includes(q);
  return (
    <section id={`${prefix}-${def.id}`} data-section={def.id} hidden={hidden} className="relative scroll-mt-[80px] pb-[72px] last:pb-10">
      <div className="flex h-10 items-center gap-[22px]">
        <span className={cx("relative z-[1] inline-flex h-10 w-10 shrink-0 items-center justify-center rounded-full border bg-secondaryBg [&>svg]:h-[18px] [&>svg]:w-[18px]",
          def.danger ? "border-danger-line text-danger" : "border-line text-fg-3")}>
          {def.icon}
        </span>
        {!def.danger && <h2 className="text-[20px] font-medium leading-[27.5px] text-fg-2">{def.label}</h2>}
      </div>
      <FilterProvider value={{ q, sectionMatch }}>
        <div className={cx("flex flex-col gap-8 pl-[62px]", def.danger ? "-mt-[30px]" : "mt-[22px]")}>{children}</div>
      </FilterProvider>
    </section>
  );
}

/** The vertical line behind the section icons. Wrap all sections in it. */
export function Timeline({ children }: { children: ReactNode }) {
  return (
    <div className="relative">
      <span aria-hidden className="absolute bottom-0 left-[19.5px] top-5 w-px bg-line" />
      {children}
    </div>
  );
}

/* ---------- field ---------- */

/** A described setting: 14/600 title, 14px muted description, then the control. Filtered by title and keywords. */
export function SettingField({ title, description, children, keywords, danger, staged, locked, className }: {
  title: ReactNode; description?: ReactNode; children?: ReactNode; keywords?: string; danger?: boolean;
  /** Shows the staged marker next to the title. */ staged?: boolean;
  /** Set by a config file: "5050.yaml:12". */ locked?: string;
  className?: string;
}) {
  const { q, sectionMatch } = useFilter();
  if (q && !sectionMatch && !matches(q, typeof title === "string" ? title : undefined, keywords)) return null;
  return (
    <div data-field className={cx("max-w-[536px]", className)}>
      <div className={cx("flex items-center gap-2 text-sm font-semibold", danger ? "text-danger" : "text-foreground")}>
        {title}
        {staged && (
          <Tooltip content="Staged: applies on the next deploy">
            <span tabIndex={0} aria-label="Staged change" className="inline-flex h-[18px] items-center rounded border border-pink-200 bg-pink-50 px-1.5 text-micro font-medium uppercase tracking-[.06em] text-pink-700">Staged</span>
          </Tooltip>
        )}
        {locked && (
          <Tooltip content={`Set by ${locked}`}>
            <span tabIndex={0} aria-label={`Set by ${locked}`} className="inline-flex h-[18px] items-center rounded border border-line px-1.5 font-mono text-micro text-fg-3">{locked.split(":")[0]}</span>
          </Tooltip>
        )}
      </div>
      {description && <div className={cx("mt-1 text-sm leading-5", danger ? "text-danger [&_b]:font-semibold" : "text-fg-2")}>{description}</div>}
      {children && <div className={cx(description ? "mt-3" : "mt-2")}>{children}</div>}
    </div>
  );
}

/* ---------- toggle ---------- */

/** 40x24 toggle: muted track off, accent on, 20px thumb sliding in 100ms. */
export function Toggle({ checked, onChange, label, disabled, id }: { checked: boolean; onChange: (v: boolean) => void; label: string; disabled?: boolean; id?: string }) {
  return (
    <button id={id} type="button" role="switch" aria-checked={checked} aria-label={label} disabled={disabled}
      onClick={(e) => { e.stopPropagation(); onChange(!checked); }}
      className={cx("relative inline-flex h-6 w-10 shrink-0 items-center rounded-full transition-colors duration-100 disabled:cursor-not-allowed",
        checked ? "bg-accent" : "bg-fg-5", disabled && "opacity-50")}>
      <span className="absolute left-0.5 top-0.5 h-5 w-5 rounded-full bg-white shadow-sm transition-transform duration-100"
        style={{ transform: checked ? "translateX(16px)" : "none" }} />
    </button>
  );
}

/** A 44px filled row with a toggle and its label; clicking the label toggles. */
export function ToggleRow({ checked, onChange, label, disabled, reason }: { checked: boolean; onChange: (v: boolean) => void; label: string; disabled?: boolean; reason?: string }) {
  const row = (
    <div role="presentation" onClick={() => !disabled && onChange(!checked)}
      className={cx("flex h-11 items-center gap-4 rounded-md border border-surface-hover bg-surface-hover px-4 transition-colors duration-150",
        disabled ? "cursor-not-allowed" : "cursor-pointer hover:border-line")}>
      <Toggle checked={checked} onChange={onChange} label={label} disabled={disabled} />
      <span className={cx("min-w-0 flex-1 truncate text-base", disabled ? "text-fg-4" : "text-foreground")}>{label}</span>
      {reason && <NotYetChip />}
    </div>
  );
  return reason ? <Tooltip content={reason}>{row}</Tooltip> : row;
}

/** The small chip on controls this build does not have yet. */
export function NotYetChip({ className }: { className?: string }) {
  return <span className={cx("inline-flex h-5 shrink-0 items-center rounded border border-line px-1.5 text-micro font-medium uppercase tracking-[.06em] text-fg-4", className)}>Not in v0.1</span>;
}

/* ---------- "+ Label" that becomes an input ---------- */

/**
 * A text setting that starts as a "+ Label" button and becomes an input on click. Saves on Enter
 * or blur; Escape cancels; clearing the value reverts to the button.
 */
export function PlusField({ label, value, placeholder, onSave, validate, mono = false, disabled, type = "text", suffix }: {
  label: string; value: string; placeholder?: string; onSave: (v: string) => Promise<unknown> | void;
  validate?: (v: string) => string | null; mono?: boolean; disabled?: boolean; type?: "text" | "number"; suffix?: ReactNode;
}) {
  const [editing, setEditing] = useState(false);
  const [text, setText] = useState(value);
  const [busy, setBusy] = useState(false);
  useEffect(() => setText(value), [value]);
  const err = validate && text ? validate(text) : null;
  if (!editing && !value) {
    return <Button icon={<Plus size={16} />} disabled={disabled} onClick={() => setEditing(true)}>{label}</Button>;
  }
  const save = async () => {
    if (err) return;
    const v = text.trim();
    if (v === value) { if (!v) setEditing(false); return; }
    setBusy(true);
    try { await onSave(v); if (!v) setEditing(false); } catch { setText(value); } finally { setBusy(false); }
  };
  return (
    <div>
      <div className="relative">
        <input aria-label={label} autoFocus={editing && !value} disabled={disabled || busy} type={type} value={text} placeholder={placeholder}
          aria-invalid={err ? true : undefined}
          onChange={(e) => setText(e.target.value)} onBlur={() => void save()}
          onKeyDown={(e) => {
            if (e.key === "Enter") (e.target as HTMLInputElement).blur();
            if (e.key === "Escape") { e.stopPropagation(); setText(value); if (!value) setEditing(false); (e.target as HTMLInputElement).blur(); }
          }}
          className={cx(INPUT, mono && "font-mono text-[15px]", suffix ? "pr-24" : "")} />
        {suffix && <span className="pointer-events-none absolute right-3 top-1/2 -translate-y-1/2 text-sm text-fg-3">{suffix}</span>}
      </div>
      {err && <div role="alert" className="mt-1.5 text-caption text-danger-text">{err}</div>}
    </div>
  );
}

/** The 42px settings input look (transparent, #33323e border, accent border on focus). */
export const INPUT = "h-[42px] w-full rounded-md border border-line bg-transparent px-3 text-base text-foreground outline-none transition-colors duration-150 placeholder:text-fg-4 hover:border-fg-5 focus:border-accent-muted focus:shadow-none disabled:cursor-not-allowed disabled:opacity-60 aria-[invalid=true]:border-danger";

/** A text input that saves on blur or Enter (always visible). */
export function SaveInput({ value, onSave, label, placeholder, validate, mono, type = "text", suffix, disabled, className }: {
  value: string; onSave: (v: string) => Promise<unknown> | void; label: string; placeholder?: string; validate?: (v: string) => string | null;
  mono?: boolean; type?: "text" | "number"; suffix?: ReactNode; disabled?: boolean; className?: string;
}) {
  const [text, setText] = useState(value);
  const [busy, setBusy] = useState(false);
  useEffect(() => setText(value), [value]);
  const err = validate && text ? validate(text) : null;
  const save = async () => {
    if (err || text.trim() === value) return;
    setBusy(true);
    try { await onSave(text.trim()); } catch { setText(value); } finally { setBusy(false); }
  };
  return (
    <div className={className}>
      <div className="relative">
        <input aria-label={label} type={type} value={text} placeholder={placeholder} disabled={disabled || busy} aria-invalid={err ? true : undefined}
          onChange={(e) => setText(e.target.value)} onBlur={() => void save()}
          onKeyDown={(e) => { if (e.key === "Enter") (e.target as HTMLInputElement).blur(); if (e.key === "Escape") { e.stopPropagation(); setText(value); } }}
          className={cx(INPUT, mono && "font-mono text-[15px]", suffix ? "pr-24" : "")} />
        {suffix && <span className="pointer-events-none absolute right-3 top-1/2 -translate-y-1/2 text-sm text-fg-3">{suffix}</span>}
      </div>
      {err && <div role="alert" className="mt-1.5 text-caption text-danger-text">{err}</div>}
    </div>
  );
}

/* ---------- select card ---------- */

export interface CardOption<T extends string> { value: T; label: string; description?: ReactNode; pill?: ReactNode; icon?: ReactNode; meta?: ReactNode; disabled?: boolean }

/**
 * A large select: a filled card with the current option's name, pill and description, and a
 * popover list of options with descriptions (builders, restart policies). Keyboard: arrows move,
 * Enter picks, Escape closes; the list is a listbox.
 */
export function SelectCard<T extends string>({ value, options, onChange, label, disabled, compact, width = 310 }: {
  value: T; options: CardOption<T>[]; onChange: (v: T) => void; label: string; disabled?: boolean; compact?: boolean; width?: number;
}) {
  const cur = options.find((o) => o.value === value) ?? options[0];
  return (
    <Popover role="listbox" align="right" width={width} wrapperClassName="relative w-full" trigger={(open) => (
      <button type="button" aria-label={label} aria-haspopup="listbox" aria-expanded={open} disabled={disabled}
        className={cx("flex w-full items-center gap-4 rounded-md border bg-surface-hover text-left transition-colors duration-150 disabled:cursor-not-allowed disabled:opacity-60",
          compact ? "px-4 py-2.5" : "px-6 py-4", open ? "border-line" : "border-surface-hover hover:border-line")}>
        <span className="min-w-0 flex-1">
          <span className="flex min-w-0 items-center gap-2.5">
            {cur.icon}
            <span className="truncate text-base text-foreground">{cur.label}</span>
            {cur.pill}
            {cur.meta}
          </span>
          {cur.description && <span className="mt-1 block truncate text-caption text-fg-2">{cur.description}</span>}
        </span>
        <ChevronDown size={16} className={cx("shrink-0 text-fg-2 transition-transform duration-200", open && "rotate-180")} />
      </button>
    )}>
      {(close) => options.map((o) => (
        <button key={o.value} type="button" role="option" aria-selected={o.value === value} disabled={o.disabled}
          onClick={() => { close(); if (o.value !== value) onChange(o.value); }}
          className="flex w-full items-start gap-2 rounded px-3 py-2 text-left outline-none transition-colors hover:bg-surface-hover focus-visible:bg-surface-hover focus-visible:shadow-none disabled:opacity-40">
          <span className="min-w-0 flex-1">
            <span className="flex items-center gap-2">
              <span className={cx("text-[15px]", o.value === value ? "text-accent-text" : "text-foreground")}>{o.label}</span>
              {o.pill}
            </span>
            {o.description && <span className="mt-0.5 block text-caption text-fg-2">{o.description}</span>}
          </span>
          {o.value === value && <Check size={15} className="mt-1 shrink-0 text-accent-text" />}
        </button>
      ))}
    </Popover>
  );
}

/** A 44px dropdown field (duration, region, cron preset) using the shared popover. */
export function DropField<T extends string>({ value, options, onChange, label, disabled, icon, className }: {
  value: T; options: { value: T; label: string; icon?: ReactNode }[]; onChange: (v: T) => void; label: string; disabled?: boolean; icon?: ReactNode; className?: string;
}) {
  const cur = options.find((o) => o.value === value);
  return (
    <Popover role="listbox" wrapperClassName={cx("relative min-w-0", className)} trigger={(open) => (
      <button type="button" aria-label={label} aria-haspopup="listbox" aria-expanded={open} disabled={disabled}
        className={cx("flex h-11 w-full items-center gap-3 rounded-md border px-3.5 text-left text-base transition-colors duration-150 disabled:cursor-not-allowed disabled:opacity-60",
          open ? "border-accent-muted" : "border-line hover:border-fg-5")}>
        {cur?.icon ?? icon}
        <span className={cx("min-w-0 flex-1 truncate", cur ? "text-foreground" : "text-fg-4")}>{cur?.label ?? "Select"}</span>
        <ChevronDown size={16} className={cx("shrink-0 text-fg-2 transition-transform duration-200", open && "rotate-180")} />
      </button>
    )}>
      {(close) => options.map((o) => (
        <button key={o.value} type="button" role="option" aria-selected={o.value === value}
          onClick={() => { close(); if (o.value !== value) onChange(o.value); }}
          className={cx("flex min-h-[36px] w-full items-center gap-3 rounded px-3 text-left text-base outline-none transition-colors hover:bg-surface-hover focus-visible:bg-surface-hover focus-visible:shadow-none",
            o.value === value ? "bg-surface-hover text-foreground" : "text-fg-2 hover:text-foreground")}>
          {o.icon}
          <span className="min-w-0 flex-1 truncate">{o.label}</span>
          {o.value === value && <Check size={15} className="shrink-0 text-accent-text" />}
        </button>
      ))}
    </Popover>
  );
}

/* ---------- small display pieces ---------- */

/** 12/600 pill on a tinted background (Default, Deprecated, IPv4). */
export function Pill({ tone = "accent", children, icon }: { tone?: "accent" | "info" | "warn" | "ok" | "neutral"; children: ReactNode; icon?: ReactNode }) {
  const cls = {
    accent: "bg-pink-50 text-pink-600",
    info: "bg-blue-50 text-blue-600",
    warn: "bg-warn-bg text-warn",
    ok: "bg-green-100 text-ok-soft",
    neutral: "bg-surface-hover text-fg-2",
  }[tone];
  return <span className={cx("inline-flex h-[22px] shrink-0 items-center gap-1 rounded px-1.5 text-caption font-medium", cls)}>{icon}{children}</span>;
}

/** A mono chip for env var names and short host names. */
export function Code({ children, tone = "accent" }: { children: ReactNode; tone?: "accent" | "neutral" }) {
  return <code className={cx("rounded px-1 py-px font-mono text-[12px]", tone === "accent" ? "bg-pink-50 text-pink-600" : "bg-surface-hover text-fg-2")}>{children}</code>;
}

/** A tinted 44px notice with text and an optional right-hand link (plan limits, deprecations). */
export function TintNote({ tone = "accent", children, action, icon }: { tone?: "accent" | "info" | "warn"; children: ReactNode; action?: ReactNode; icon?: ReactNode }) {
  const cls = { accent: "bg-pink-50 text-pink-700", info: "bg-blue-50 text-blue-600", warn: "bg-warn-bg text-warn" }[tone];
  return (
    <div className={cx("flex min-h-11 items-center gap-3 rounded-md px-3 py-2.5 text-sm", cls)}>
      {icon && <span className="shrink-0">{icon}</span>}
      <span className="min-w-0 flex-1">{children}</span>
      {action && <span className="shrink-0">{action}</span>}
    </div>
  );
}

/** The filled inline panel for add forms (custom domain, TCP proxy, port edit). */
export function InlinePanel({ title, children, hint, onCancel, submit, onSubmit, valid, busy }: {
  title?: string; children: ReactNode; hint?: ReactNode; onCancel: () => void; submit: string; onSubmit: () => void; valid: boolean; busy?: boolean;
}) {
  const ref = useRef<HTMLFormElement>(null);
  return (
    <form ref={ref} onSubmit={(e) => { e.preventDefault(); if (valid && !busy) onSubmit(); }}
      onKeyDown={(e) => { if (e.key === "Escape") { e.stopPropagation(); onCancel(); } }}
      className="animate-rise-in rounded-lg bg-surface-hover p-4">
      {title && <div className="mb-4 text-sm font-semibold text-foreground">{title}</div>}
      <div className="flex flex-col gap-4">{children}</div>
      <div className="mt-4 flex items-center gap-2">
        <span className="flex min-w-0 flex-1 items-center gap-2 text-sm text-fg-3">{hint}</span>
        <Button size="md" onClick={onCancel}>Cancel</Button>
        <Button size="md" type="submit" variant="primary" busy={busy} disabled={!valid}>{submit}</Button>
      </div>
    </form>
  );
}

/** A 42px input for inline panels (filled darker than the panel). */
export const PANEL_INPUT = "h-[42px] w-full rounded-md border border-line bg-secondaryBg px-3 text-base text-foreground outline-none transition-colors duration-150 placeholder:text-fg-4 focus:border-accent-muted focus:shadow-none";

/** A muted underlined text button ("Add Root Directory"). */
export function TextButton({ children, onClick, icon }: { children: ReactNode; onClick: () => void; icon?: ReactNode }) {
  return (
    <button type="button" onClick={onClick} className="inline-flex items-center gap-1.5 text-sm text-fg-2 underline decoration-fg-4 underline-offset-2 transition-colors hover:text-foreground">
      {icon}{children}
    </button>
  );
}
