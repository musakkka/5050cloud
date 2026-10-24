// SPDX-License-Identifier: AGPL-3.0-only

// Form controls and buttons: Button, IconButton, Input, Textarea, Select, Field, Checkbox,
// Switch, Kbd, SearchButton and SegmentedControl. Sizes follow the console grid: buttons 26/30/34/42,
// inputs 42 (default) or 34 (compact), 6px radius everywhere.
import { forwardRef, useId, type ReactNode } from "react";
import { ChevronDown, Loader2, Search } from "lucide-react";
import { cx, isMac } from "./util";

export type ButtonVariant =
  | "primary" | "default" | "outline" | "secondary" | "ghost" | "danger" | "danger-outline"
  | "accent-outline" | "accent-ghost" | "success-outline" | "link" | "staged";
export type ButtonSize = "xs" | "sm" | "md" | "lg";

const VARIANT: Record<ButtonVariant, string> = {
  primary: "bg-accent border-accent text-accent-fg hover:bg-accent-hover hover:border-accent-hover",
  staged: "bg-accent border-accent text-accent-fg hover:bg-accent-hover hover:border-accent-hover",
  default: "border-line text-foreground hover:bg-surface-hover",
  outline: "border-line text-foreground hover:bg-surface-hover",
  secondary: "bg-surface-hover border-line text-foreground hover:border-fg-5",
  ghost: "border-transparent text-fg-2 hover:bg-surface-hover hover:text-foreground",
  danger: "bg-danger border-danger text-white hover:brightness-110",
  "danger-outline": "border-danger-line text-danger-soft hover:bg-danger-bg",
  "accent-outline": "border-pink-200 text-pink-700 hover:bg-pink-50",
  "accent-ghost": "border-transparent text-pink-700 hover:bg-pink-50",
  "success-outline": "border-green-200 text-ok-soft hover:bg-green-100",
  link: "border-transparent !px-0 !h-auto text-accent-text underline-offset-2 hover:underline",
};
const SIZE: Record<ButtonSize, string> = {
  xs: "h-[26px] px-1.5 text-xs gap-1.5",
  sm: "h-[30px] px-2 text-sm gap-1.5",
  md: "h-[34px] px-3 text-sm gap-2",
  lg: "h-[42px] px-3 text-base gap-2",
};

export interface ButtonProps extends React.ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: ButtonVariant;
  size?: ButtonSize;
  /** Shows a spinner and disables the button. */
  busy?: boolean;
  /** Leading icon node (16px lucide icon). */
  icon?: ReactNode;
  /** Trailing icon node. */
  iconRight?: ReactNode;
  /** Pill shape (rounded-full). */
  pill?: boolean;
  /** Stretch to the container width. */
  block?: boolean;
}

/** The console button. Default type is "button" (never submits by accident). */
export const Button = forwardRef<HTMLButtonElement, ButtonProps>(function Button(
  { children, variant = "default", size = "md", className, busy, icon, iconRight, pill, block, type = "button", ...p }, ref,
) {
  return (
    <button ref={ref} type={type} {...p} disabled={p.disabled || busy} aria-busy={busy || undefined}
      className={cx(
        "inline-flex shrink-0 select-none items-center justify-center whitespace-nowrap border font-normal",
        "transition-[color,background-color,border-color,filter,transform] duration-150 active:scale-[.97] active:duration-50",
        "disabled:pointer-events-none disabled:opacity-50",
        pill ? "rounded-full" : "rounded-md",
        block && "w-full",
        SIZE[size], VARIANT[variant], className,
      )}>
      {busy ? <Loader2 size={size === "xs" ? 12 : 16} className="spin" /> : icon}
      {children}
      {iconRight}
    </button>
  );
});

export interface IconButtonProps extends React.ButtonHTMLAttributes<HTMLButtonElement> {
  /** Required accessible name; also the native tooltip unless `title` is given. */
  label: string;
  /** 24, 26, 30 (header buttons) or 34 (toolbar buttons). Default 30. */
  size?: 24 | 26 | 30 | 34 | 40;
  /** Filled look for toggles that are on. */
  active?: boolean;
  variant?: "ghost" | "outline" | "secondary";
}

/** A square icon-only button: header bell, kebabs, close buttons, toolbars. */
export const IconButton = forwardRef<HTMLButtonElement, IconButtonProps>(function IconButton(
  { label, size = 30, active, variant = "ghost", className, children, type = "button", title, ...p }, ref,
) {
  return (
    <button ref={ref} type={type} aria-label={label} title={title ?? label} aria-pressed={active ?? undefined} {...p}
      className={cx(
        "inline-flex shrink-0 items-center justify-center rounded-md border transition-colors duration-150 active:scale-[.96]",
        "disabled:pointer-events-none disabled:opacity-40",
        variant === "ghost" && "border-transparent",
        variant === "outline" && "border-line",
        variant === "secondary" && "border-line bg-surface-raised",
        active ? "bg-surface-hover text-gray-800" : "text-fg-2 hover:bg-surface-hover hover:text-foreground",
        className,
      )}
      style={{ width: size, height: size, ...p.style }}>
      {children}
    </button>
  );
});

const FIELD_BASE = "w-full rounded-md border border-line bg-transparent text-foreground outline-none transition-colors duration-150 placeholder:text-fg-4 hover:border-fg-5 focus:border-accent focus:shadow-none disabled:cursor-not-allowed disabled:opacity-60 aria-[invalid=true]:border-danger";

export interface InputProps extends Omit<React.InputHTMLAttributes<HTMLInputElement>, "size"> {
  /** "md" = 42px, 16px text (forms, the default); "sm" = 34px, 14px text (toolbars, filters, dense rows). */
  size?: "sm" | "md";
  /** Leading icon inside the field. */
  icon?: ReactNode;
  /** Trailing node inside the field (unit, kbd, button). */
  suffix?: ReactNode;
  mono?: boolean;
}

export const Input = forwardRef<HTMLInputElement, InputProps>(function Input({ className, size = "md", icon, suffix, mono, ...p }, ref) {
  const cls = cx(FIELD_BASE, size === "md" ? "h-[42px] px-3 text-base" : "h-[34px] px-3 text-sm", mono && "font-mono", icon && (size === "md" ? "pl-10" : "pl-9"), suffix && "pr-10", className);
  if (!icon && !suffix) return <input ref={ref} {...p} className={cls} />;
  return (
    <div className="relative w-full">
      {icon && <span className={cx("pointer-events-none absolute top-1/2 -translate-y-1/2 text-fg-3", size === "md" ? "left-3" : "left-2.5")}>{icon}</span>}
      <input ref={ref} {...p} className={cls} />
      {suffix && <span className="absolute right-2 top-1/2 flex -translate-y-1/2 items-center text-fg-3">{suffix}</span>}
    </div>
  );
});

export const Textarea = forwardRef<HTMLTextAreaElement, React.TextareaHTMLAttributes<HTMLTextAreaElement> & { mono?: boolean }>(
  function Textarea({ className, mono = true, ...p }, ref) {
    return <textarea ref={ref} {...p} className={cx(FIELD_BASE, "px-3 py-2 text-sm leading-5 scroll-thin", mono && "font-mono", className)} />;
  },
);

export interface SelectProps extends Omit<React.SelectHTMLAttributes<HTMLSelectElement>, "size"> {
  /** Options as data; or pass <option> children. */
  options?: { value: string; label: string; disabled?: boolean }[];
  /** "outline" = bordered 34px (filters); "flat" = no border, muted text (sort control); "field" = 42px form field. */
  variant?: "outline" | "flat" | "field";
}

/** A native select with the console chevron. */
export const Select = forwardRef<HTMLSelectElement, SelectProps>(function Select({ options, variant = "outline", className, children, ...p }, ref) {
  return (
    <div className={cx("relative inline-flex min-w-0 items-center", variant === "field" && "w-full", className)}>
      <select ref={ref} {...p}
        className={cx(
          "w-full min-w-0 cursor-pointer appearance-none truncate rounded-md bg-transparent pr-8 outline-none transition-colors duration-150 disabled:cursor-not-allowed disabled:opacity-60",
          variant === "flat" && "h-[34px] border border-transparent pl-0 text-sm text-fg-2 hover:text-foreground",
          variant === "outline" && "h-[34px] border border-line pl-2.5 text-sm text-foreground hover:border-fg-5 focus:border-accent",
          variant === "field" && "h-[42px] border border-line pl-3 text-base text-foreground hover:border-fg-5 focus:border-accent",
        )}>
        {options ? options.map((o) => <option key={o.value} value={o.value} disabled={o.disabled} className="bg-surface-raised text-foreground">{o.label}</option>) : children}
      </select>
      <ChevronDown size={16} className={cx("pointer-events-none absolute text-fg-4", variant === "flat" ? "right-1" : "right-2.5")} />
    </div>
  );
});

/** A labelled form row: 12px/600 label, the control, then a hint or an error. */
export function Field({ label, hint, error, children, className, optional }: {
  label: ReactNode; hint?: ReactNode; error?: string; children: ReactNode; className?: string; optional?: boolean;
}) {
  // the hint sits outside <label> so the control's accessible name is just the label
  return (
    <div className={cx("space-y-2", className)}>
      <label className="block space-y-2">
        <span className="block text-caption font-semibold text-fg-2">{label}{optional && <span className="ml-1 font-normal text-fg-4">(optional)</span>}</span>
        {children}
      </label>
      {hint && !error && <span className="block text-caption text-fg-3">{hint}</span>}
      {error && <span role="alert" className="block text-caption text-danger-text">{error}</span>}
    </div>
  );
}

export function Checkbox({ checked, onChange, label, description, disabled, className }: {
  checked: boolean; onChange: (v: boolean) => void; label?: ReactNode; description?: ReactNode; disabled?: boolean; className?: string;
}) {
  const id = useId();
  return (
    <label htmlFor={id} className={cx("flex cursor-pointer items-start gap-2.5 text-sm", disabled && "cursor-not-allowed opacity-60", className)}>
      <input id={id} type="checkbox" checked={checked} disabled={disabled} onChange={(e) => onChange(e.target.checked)}
        className="peer mt-0.5 h-4 w-4 shrink-0 cursor-pointer appearance-none rounded border border-fg-5 bg-transparent transition-colors checked:border-accent checked:bg-accent disabled:cursor-not-allowed
          bg-center bg-no-repeat checked:bg-[url('data:image/svg+xml,%3Csvg%20xmlns%3D%22http%3A%2F%2Fwww.w3.org%2F2000%2Fsvg%22%20viewBox%3D%220%200%2016%2016%22%20fill%3D%22none%22%20stroke%3D%22white%22%20stroke-width%3D%222.2%22%20stroke-linecap%3D%22round%22%20stroke-linejoin%3D%22round%22%3E%3Cpath%20d%3D%22M3.5%208.5l3%203%206-7%22%2F%3E%3C%2Fsvg%3E')]" />
      {(label || description) && (
        <span className="min-w-0">
          {label && <span className="block text-foreground">{label}</span>}
          {description && <span className="block text-caption text-fg-3">{description}</span>}
        </span>
      )}
    </label>
  );
}

/** An on/off toggle (44x24 track). */
export function Switch({ checked, onChange, label, disabled, size = "md", className }: {
  checked: boolean; onChange: (v: boolean) => void; label?: string; disabled?: boolean; size?: "sm" | "md"; className?: string;
}) {
  const w = size === "sm" ? 32 : 40, h = size === "sm" ? 18 : 22, k = h - 4;
  return (
    <button type="button" role="switch" aria-checked={checked} aria-label={label} disabled={disabled} onClick={() => onChange(!checked)}
      className={cx("relative inline-flex shrink-0 items-center rounded-full transition-colors duration-150 disabled:cursor-not-allowed disabled:opacity-50", checked ? "bg-accent" : "bg-gray-300", className)}
      style={{ width: w, height: h }}>
      <span className="absolute left-0.5 top-0.5 rounded-full bg-white shadow-sm transition-transform duration-150"
        style={{ width: k, height: k, transform: checked ? `translateX(${w - k - 4}px)` : "none" }} />
    </button>
  );
}

/** A keyboard key chip: 20px tall, 12px text. */
export function Kbd({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <kbd className={cx("inline-flex h-5 min-w-[18px] items-center justify-center rounded border border-line bg-surface-hover px-1 font-sans text-caption leading-none text-fg-3", className)}>
      {children}
    </kbd>
  );
}

/** The platform modifier glyph: Cmd on Apple devices, Ctrl elsewhere. */
export const ModKey = () => <Kbd>{isMac() ? "⌘" : "Ctrl"}</Kbd>;

/** A search field that is really a button opening the palette: icon, placeholder, Cmd K chips. */
export function SearchButton({ label = "Search...", onClick, shortcut = "K", className }: { label?: string; onClick: () => void; shortcut?: string | null; className?: string }) {
  return (
    <button type="button" onClick={onClick}
      className={cx("group inline-flex h-[34px] w-56 min-w-0 items-center gap-2 rounded-md border border-line px-2.5 text-left text-sm text-fg-2 transition-colors duration-150 hover:border-fg-4", className)}>
      <Search size={16} className="shrink-0 text-fg-3" />
      <span className="min-w-0 flex-1 truncate">{label}</span>
      {shortcut && <span className="flex shrink-0 gap-0.5"><ModKey /><Kbd>{shortcut}</Kbd></span>}
    </button>
  );
}

export interface SegmentOption<T extends string> { value: T; label: string; icon?: ReactNode }

/** Joined toggle buttons (grid/list view, ranges). Icon-only when an option has an icon. */
export function SegmentedControl<T extends string>({ options, value, onChange, label, className, size = 32 }: {
  options: SegmentOption<T>[]; value: T; onChange: (v: T) => void; label: string; className?: string; size?: number;
}) {
  return (
    <div role="radiogroup" aria-label={label} className={cx("inline-flex shrink-0 items-center rounded-lg border border-line-faint bg-surface", className)}>
      {options.map((o) => {
        const on = o.value === value;
        return (
          <button key={o.value} type="button" role="radio" aria-checked={on} aria-label={o.icon ? o.label : undefined} title={o.icon ? o.label : undefined}
            onClick={() => onChange(o.value)}
            className={cx("inline-flex items-center justify-center rounded-lg border text-sm transition-colors duration-150",
              o.icon ? "" : "px-3",
              on ? "border-black/60 bg-surface-raised text-foreground [border-width:.5px]" : "border-transparent text-fg-2 hover:text-foreground")}
            style={{ height: size, minWidth: size }}>
            {o.icon ?? o.label}
          </button>
        );
      })}
    </div>
  );
}
