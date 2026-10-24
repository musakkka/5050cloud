// SPDX-License-Identifier: AGPL-3.0-only

// Display pieces: page headers, cards, tabs, badges, status dots, avatars, skeletons, empty
// states, notices and banners, 404, copy buttons and service icons.
import { useState, type ReactNode } from "react";
import { Link } from "react-router-dom";
import { AlertCircle, AlertTriangle, ArrowUpRight, Check, CircleCheck, Compass, Copy, Info, Loader2, X } from "lucide-react";
import { iconUrl } from "../api/client";
import { BrandIcon, hasBrandIcon } from "./brand-icons";
import { cx, fmtDateTime, secs, timeAgo } from "./util";
import { TONE_DOT, toneOf, type Tone } from "./status";

/** A relative time with the absolute time as its tooltip. */
export function RelTime({ ts, className }: { ts?: number | null; className?: string }) {
  if (!ts) return null;
  return <time className={className} dateTime={new Date(secs(ts) * 1000).toISOString()} title={fmtDateTime(ts)}>{timeAgo(ts)}</time>;
}

/** Page title row: 28px/38.5px weight 400 title, optional description, actions on the right. */
export function PageHeader({ title, description, actions, className }: { title: ReactNode; description?: ReactNode; actions?: ReactNode; className?: string }) {
  return (
    <div className={cx("flex flex-wrap items-center justify-between gap-x-6 gap-y-3", className)}>
      <div className="min-w-0">
        <h1 className="truncate text-title text-foreground">{title}</h1>
        {description && <p className="mt-2 text-sm text-fg-3">{description}</p>}
      </div>
      {actions && <div className="flex shrink-0 items-center gap-3">{actions}</div>}
    </div>
  );
}

/** A 20px/600 section heading with an optional muted line and actions. */
export function SectionHeader({ title, description, actions, className }: { title: ReactNode; description?: ReactNode; actions?: ReactNode; className?: string }) {
  return (
    <div className={cx("flex items-start justify-between gap-4", className)}>
      <div className="min-w-0">
        <h2 className="text-h2 text-foreground">{title}</h2>
        {description && <p className="mt-1 text-sm text-fg-2">{description}</p>}
      </div>
      {actions && <div className="flex shrink-0 items-center gap-2">{actions}</div>}
    </div>
  );
}

/** The 8px-radius card surface. `interactive` adds the hover tint and soft shadow. */
export function Card({ children, className, interactive, bordered = true, padded = true, as: Tag = "div", ...p }: {
  children: ReactNode; className?: string; interactive?: boolean; bordered?: boolean; padded?: boolean; as?: "div" | "section" | "li" | "article";
} & React.HTMLAttributes<HTMLElement>) {
  return (
    <Tag {...p} className={cx("rounded-lg bg-surface", bordered && "border border-line", padded && "p-4",
      interactive && "transition-[background-color,box-shadow] duration-150 hover:bg-surface-hover hover:shadow-card", className)}>
      {children}
    </Tag>
  );
}

/** A full-width 1px hairline. */
export function Divider({ className }: { className?: string }) {
  return <hr className={cx("border-0 border-t border-line", className)} />;
}

/** The 2x20 rounded divider between header and toolbar items. */
export function VDivider({ className }: { className?: string }) {
  return <span aria-hidden className={cx("inline-block h-5 w-0.5 shrink-0 rounded-full bg-tint-pill", className)} />;
}

export interface TabItem { id: string; label: ReactNode; count?: number; icon?: ReactNode; disabled?: boolean }

/** Underlined tabs: 14px labels, white active label with a 2px accent underline, hairline under the list. */
export function Tabs({ tabs, value, onChange, className, right }: {
  tabs: TabItem[]; value: string; onChange: (id: string) => void; className?: string; right?: ReactNode;
}) {
  return (
    <div className={cx("flex items-end justify-between gap-4 border-b border-line", className)}>
      <div role="tablist" className="no-scrollbar -mb-px flex min-w-0 gap-1 overflow-x-auto">
        {tabs.map((t) => {
          const on = value === t.id;
          return (
            <button key={t.id} type="button" role="tab" aria-selected={on} disabled={t.disabled} onClick={() => onChange(t.id)}
              className={cx("relative flex h-[38px] shrink-0 items-center gap-2 px-3 pb-3 pt-1 text-sm transition-colors duration-150 disabled:opacity-40",
                on ? "text-foreground" : "text-fg-2 hover:text-foreground")}>
              {t.icon}{t.label}
              {t.count !== undefined && <span className="rounded-md border border-line-popover px-1.5 text-caption leading-[18px] text-fg-2">{t.count}</span>}
              <span className={cx("absolute inset-x-0 bottom-0 h-0.5 rounded-full bg-accent transition-opacity duration-150", on ? "opacity-100" : "opacity-0")} />
            </button>
          );
        })}
      </div>
      {right && <div className="flex shrink-0 items-center gap-2 pb-2">{right}</div>}
    </div>
  );
}

/** Pill tabs in a well (notification scope, small toggles): 34px container, 12px/500 tabs. */
export function SegmentedTabs({ tabs, value, onChange, className }: { tabs: TabItem[]; value: string; onChange: (id: string) => void; className?: string }) {
  return (
    <div role="tablist" className={cx("flex h-[34px] items-stretch rounded-lg border border-line bg-surface-hover p-px", className)}>
      {tabs.map((t) => {
        const on = value === t.id;
        return (
          <button key={t.id} type="button" role="tab" aria-selected={on} onClick={() => onChange(t.id)}
            className={cx("flex flex-1 items-center justify-center gap-1.5 rounded-lg text-caption font-medium transition-colors duration-150",
              on ? "bg-surface-raised text-foreground" : "text-fg-2 hover:text-foreground")}>
            {t.icon}{t.label}
          </button>
        );
      })}
    </div>
  );
}

export type BadgeTone = "neutral" | "accent" | "ok" | "warn" | "danger" | "info";
const BADGE_TONE: Record<BadgeTone, string> = {
  neutral: "border-line-popover text-fg-2",
  accent: "border-pink-200 bg-pink-50 text-pink-700",
  ok: "border-green-200 bg-green-100 text-ok-soft",
  warn: "border-warn-line bg-warn-bg text-warn",
  danger: "border-danger-line bg-danger-bg text-danger-text",
  info: "border-blue-200 bg-blue-50 text-blue-600",
};
const KIND_TONE: Record<string, BadgeTone> = {
  active: "ok", success: "ok", online: "ok", completed: "ok", removed: "neutral", crashed: "danger", failed: "danger",
  unhealthy: "warn", deploying: "info", building: "info", initializing: "info", waiting: "info", queued: "neutral", skipped: "neutral",
};

/** A small chip. `kind` maps a status string to a tone; `upper` gives the 10px uppercase plan-label style. */
export function Badge({ tone, kind, children, upper, className }: { tone?: BadgeTone; kind?: string; children?: ReactNode; upper?: boolean; className?: string }) {
  const t = tone ?? (kind ? KIND_TONE[kind.toLowerCase()] ?? "neutral" : "neutral");
  return (
    <span className={cx("inline-flex shrink-0 items-center gap-1 whitespace-nowrap rounded-md border",
      upper ? "h-5 px-1.5 text-micro font-medium uppercase tracking-[.06em]" : "h-[22px] px-1.5 text-caption",
      BADGE_TONE[t], className)}>
      {children ?? kind}
    </span>
  );
}

/** A status dot: 6px in summaries, 8px next to service names. `pulse` for in-progress states. */
export function StatusDot({ status, tone, size = 6, pulse, className, label }: { status?: string; tone?: Tone; size?: number; pulse?: boolean; className?: string; label?: string }) {
  const t = tone ?? (status ? toneOf(status) : "idle");
  return (
    <span role={label ? "img" : undefined} aria-label={label} aria-hidden={label ? undefined : true}
      className={cx("relative inline-flex shrink-0 rounded-full", TONE_DOT[t], className)} style={{ width: size, height: size }}>
      {(pulse ?? t === "busy") && <span className={cx("absolute inset-0 rounded-full animate-ping opacity-60", TONE_DOT[t])} />}
    </span>
  );
}

const AVATAR_BG = ["#7c4dff", "#d6338a", "#2f9e77", "#e8590c", "#1c7ed6", "#ae3ec9", "#0c8599", "#f08c00"];

/** A round avatar: image, or initials on a colour picked from the name. */
export function Avatar({ name, src, size = 24, className }: { name?: string | null; src?: string | null; size?: number; className?: string }) {
  const [broken, setBroken] = useState(false);
  const n = (name || "?").trim();
  const initials = n.split(/[\s@._-]+/).filter(Boolean).slice(0, 2).map((w) => w[0]!.toUpperCase()).join("") || "?";
  let h = 0;
  for (const c of n) h = (h * 31 + c.charCodeAt(0)) >>> 0;
  if (src && !broken) {
    return <img src={src} alt="" onError={() => setBroken(true)} className={cx("shrink-0 rounded-full object-cover", className)} style={{ width: size, height: size }} />;
  }
  return (
    <span aria-hidden className={cx("inline-flex shrink-0 select-none items-center justify-center rounded-full font-semibold text-white", className)}
      style={{ width: size, height: size, background: AVATAR_BG[h % AVATAR_BG.length], fontSize: Math.max(8, Math.round(size * (initials.length > 1 ? 0.38 : 0.45))) }}>
      {initials}
    </span>
  );
}

/** A pulsing placeholder block (8% white, 4px radius, 2s pulse). `circle` for avatars. */
export function Skeleton({ className, circle, w, h }: { className?: string; circle?: boolean; w?: number | string; h?: number | string }) {
  return <div aria-hidden className={cx("animate-skeleton bg-skeleton", circle ? "rounded-full" : "rounded", className)} style={{ width: w, height: h }} />;
}

/** A shimmering card placeholder for lists that load in place (notifications). */
export function ShimmerCard({ className, h = 82 }: { className?: string; h?: number }) {
  return <div aria-hidden className={cx("shimmer rounded-lg border border-line", className)} style={{ height: h }} />;
}

/** Legacy compact empty box. Prefer EmptyState. */
export function Empty({ children, className }: { children: ReactNode; className?: string }) {
  return <div className={cx("rounded border border-dashed border-line px-6 py-8 text-center text-sm text-fg-2", className)}>{children}</div>;
}

/** The dashed empty card: 24px icon, 18px/500 title, muted body, optional action. */
export function EmptyState({ icon, title, body, action, className }: { icon?: ReactNode; title: ReactNode; body?: ReactNode; action?: ReactNode; className?: string }) {
  return (
    <div className={cx("flex flex-col items-center justify-center rounded border border-dashed border-line px-6 py-8 text-center", className)}>
      {icon && <div className="mb-3 text-fg-2 [&>svg]:h-6 [&>svg]:w-6">{icon}</div>}
      <div className="text-lg font-medium text-foreground">{title}</div>
      {body && <div className="mt-1 max-w-md text-sm text-fg-2">{body}</div>}
      {action && <div className="mt-5">{action}</div>}
    </div>
  );
}

export function Spinner({ className, size = 18 }: { className?: string; size?: number }) {
  return <Loader2 size={size} aria-label="Loading" className={cx("spin text-fg-3", className)} />;
}

export function ErrorLine({ error, className }: { error?: unknown; className?: string }) {
  if (!error) return null;
  const msg = error instanceof Error ? error.message : String(error);
  return <div role="alert" className={cx("rounded-md border border-danger-line bg-danger-bg px-3 py-2 text-sm text-danger-text", className)}>{msg}</div>;
}

export type NoticeTone = "info" | "warn" | "danger" | "ok" | "neutral";
const NOTICE: Record<NoticeTone, { cls: string; icon: ReactNode }> = {
  info: { cls: "border-blue-200 bg-blue-50 text-blue-700", icon: <Info size={16} /> },
  warn: { cls: "border-warn-line bg-warn-bg text-warn", icon: <AlertTriangle size={16} /> },
  danger: { cls: "border-danger-line bg-danger-bg text-danger-text", icon: <AlertCircle size={16} /> },
  ok: { cls: "border-green-200 bg-green-100 text-ok-soft", icon: <CircleCheck size={16} /> },
  neutral: { cls: "border-line bg-surface text-fg-2", icon: <Info size={16} /> },
};

/** An inline callout box with an icon, text and optional action. */
export function Notice({ tone = "info", title, children, action, className, icon }: { tone?: NoticeTone; title?: ReactNode; children?: ReactNode; action?: ReactNode; className?: string; icon?: ReactNode | false }) {
  const t = NOTICE[tone];
  return (
    <div role={tone === "danger" ? "alert" : "status"} className={cx("flex items-start gap-3 rounded-lg border px-4 py-3 text-sm", t.cls, className)}>
      {icon !== false && <span className="mt-0.5 shrink-0">{icon ?? t.icon}</span>}
      <div className="min-w-0 flex-1">
        {title && <div className="font-medium">{title}</div>}
        {children && <div className={cx(title && "mt-0.5 opacity-90")}>{children}</div>}
      </div>
      {action && <div className="shrink-0">{action}</div>}
    </div>
  );
}

/** The full-width 52px page banner (install notices, updates, restores). */
export function Banner({ tone = "warn", children, link, onDismiss }: { tone?: "warn" | "info" | "danger" | "ok"; children: ReactNode; link?: { label: string; href?: string; to?: string; onClick?: () => void }; onDismiss?: () => void }) {
  const cls = { warn: "bg-warn-bg text-warn", info: "bg-blue-50 text-blue-700", danger: "bg-danger-bg text-danger-text", ok: "bg-green-100 text-ok-soft" }[tone];
  const Icon = tone === "ok" ? CircleCheck : AlertCircle;
  const linkCls = "font-normal underline underline-offset-2 transition-colors hover:text-accent-text";
  return (
    <div role="status" className={cx("relative flex min-h-banner shrink-0 items-center justify-center py-3 pl-3 pr-12 text-sm font-medium sm:px-8", cls)}>
      <div className="flex flex-wrap items-center justify-center gap-x-2 gap-y-1 text-center">
        <Icon size={20} className="shrink-0" />
        <span>{children}</span>
        {link && (link.to ? <Link to={link.to} className={linkCls}>{link.label}</Link>
          : link.href ? <a href={link.href} target="_blank" rel="noreferrer" className={linkCls}>{link.label}</a>
          : <button type="button" onClick={link.onClick} className={linkCls}>{link.label}</button>)}
      </div>
      {onDismiss && (
        <button type="button" aria-label="Dismiss" onClick={onDismiss}
          className="absolute right-2 top-1/2 inline-flex h-[34px] w-[34px] -translate-y-1/2 items-center justify-center rounded-md border border-transparent transition-colors hover:border-surface-hover hover:bg-surface-hover">
          <X size={16} />
        </button>
      )}
    </div>
  );
}

/** An external link with the trailing arrow glyph, opening in a new tab. */
export function ExternalLink({ href, children, className }: { href: string; children: ReactNode; className?: string }) {
  return (
    <a href={href} target="_blank" rel="noreferrer" className={cx("inline-flex items-center gap-0.5 underline-offset-2 transition-colors hover:text-accent-text", className)}>
      {children}<ArrowUpRight size={12} className="shrink-0 opacity-80" />
    </a>
  );
}

/** 404: centred compass ring, "404", a line and a Go home button. `bare` for routes outside the shell. */
export function NotFound({ bare, title = "Nothing here", body, home = "/" }: { bare?: boolean; title?: string; body?: ReactNode; home?: string }) {
  return (
    <div className={cx("flex h-full min-h-[60vh] flex-col items-center justify-center gap-2 p-6 text-center", bare && "min-h-screen bg-background")}>
      <span className={cx("mb-4 inline-flex items-center justify-center rounded-full", bare ? "h-[60px] w-[60px] text-fg-2" : "h-[88px] w-[88px] border-[6px] border-foreground text-foreground")}>
        <Compass size={bare ? 56 : 40} strokeWidth={1.5} />
      </span>
      <div className={cx("text-foreground", bare ? "text-2xl font-semibold" : "text-[32px] font-bold leading-10")}>404</div>
      <div className={cx(bare ? "text-base text-fg-3" : "text-h2 text-foreground")}>{title}</div>
      {body && <div className="max-w-sm text-sm text-fg-2">{body}</div>}
      <Link to={home} className="mt-4 inline-flex h-[42px] items-center rounded-md border border-accent bg-accent px-3 text-base text-accent-fg transition-colors duration-150 hover:border-accent-hover hover:bg-accent-hover active:scale-[.97]">Go home</Link>
    </div>
  );
}

/** Copies text with a check mark for a second. Used on every id, URL, record and command. */
export function CopyButton({ text, label = "Copy", className, size = 24 }: { text: string; label?: string; className?: string; size?: number }) {
  const [done, setDone] = useState(false);
  return (
    <button type="button" aria-label={label} title={label}
      className={cx("inline-flex shrink-0 items-center justify-center rounded-md text-fg-3 transition-colors hover:bg-surface-hover hover:text-foreground", className)}
      style={{ width: size, height: size }}
      onClick={(e) => {
        e.stopPropagation();
        void navigator.clipboard?.writeText(text).then(() => { setDone(true); setTimeout(() => setDone(false), 1200); });
      }}>
      {done ? <Check size={13} className="text-ok-text" /> : <Copy size={13} />}
    </button>
  );
}

/**
 * A service or template icon. Known technologies (an icon key, an /icons/ path or an image name)
 * render the official logo; anything else shows a custom icon URL, then a glyph for the source kind.
 */
export function ServiceIcon({ icon, source, image, size = 20 }: { icon?: string | null; source?: string; image?: string | null; size?: number }) {
  const [broken, setBroken] = useState(false);
  if (hasBrandIcon(icon)) return <BrandIcon name={icon} size={size} />;
  if (hasBrandIcon(image)) return <BrandIcon name={image} size={size} />;
  const src = icon ? iconUrl(icon) : undefined;
  if (src && !broken) return <img src={src} alt="" width={size} height={size} onError={() => setBroken(true)} className="shrink-0 object-contain" style={{ width: size, height: size }} />;
  if (source === "image") return <BrandIcon name="docker" size={size} />;
  if (source === "cron") return <BrandIcon name={null} fallback="clock" size={size} />;
  if (source === "repo" || source === "github" || source === undefined) return <BrandIcon name="github" size={size} />;
  return <BrandIcon name={null} fallback="terminal" size={size} />;
}

/** A control we do not have yet, shown disabled with the reason (SPEC 13.6). */
export function NotInV01({ label, instead }: { label: string; instead: string }) {
  return (
    <span title={`Not in v0.1: ${instead}`} aria-disabled="true" className="inline-flex cursor-not-allowed items-center gap-2 text-sm text-fg-4">
      {label}<Badge upper>Not in v0.1</Badge>
    </span>
  );
}
