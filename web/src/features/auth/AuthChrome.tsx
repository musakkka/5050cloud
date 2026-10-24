// SPDX-License-Identifier: AGPL-3.0-only

// Shared chrome for every logged-out screen: the marketing-style header, the four-column
// footer, the 380px auth card with its drifting streak lines, and the primary/secondary
// buttons and password meter the forms use.
import { useEffect, useRef, useState, type ButtonHTMLAttributes, type ReactNode } from "react";
import { Link } from "react-router-dom";
import { Loader2, Menu, X } from "lucide-react";
import { BrandIcon, Logo, cx } from "../../kit";
import { LINKS, REPO_URL } from "../../app/links";

const YEAR = new Date().getFullYear();

const HEADER_LINKS = [
  { label: "Docs", href: LINKS.docs },
  { label: "Releases", href: `${REPO_URL}/releases` },
  { label: "Community", href: LINKS.community },
];

const FOOTER_COLUMNS: { heading: string; links: { label: string; href: string }[] }[] = [
  {
    heading: "Product",
    links: [
      { label: "Documentation", href: LINKS.docs },
      { label: "Install guide", href: `${REPO_URL}/tree/main/install` },
      { label: "Releases", href: `${REPO_URL}/releases` },
    ],
  },
  {
    heading: "Project",
    links: [
      { label: "GitHub", href: REPO_URL },
      { label: "Issues", href: LINKS.support },
      { label: "Security policy", href: `${REPO_URL}/blob/main/SECURITY.md` },
    ],
  },
  {
    heading: "Community",
    links: [
      { label: "Discussions", href: LINKS.community },
      { label: "Contributing", href: `${REPO_URL}/blob/main/CONTRIBUTING.md` },
      { label: "Code of conduct", href: `${REPO_URL}/blob/main/CODE_OF_CONDUCT.md` },
    ],
  },
  {
    heading: "Legal",
    links: [
      { label: "License", href: `${REPO_URL}/blob/main/LICENSE` },
      { label: "Licensing notes", href: `${REPO_URL}/blob/main/LICENSING.md` },
    ],
  },
];

/** Page frame for logged-out screens: header, centred content, footer. */
export function AuthLayout({ children, align = "top" }: { children: ReactNode; align?: "top" | "center" }) {
  return (
    <div className="flex min-h-screen flex-col bg-background text-foreground">
      <AuthHeader />
      <main className={cx(
        "flex flex-1 flex-col items-center px-5 pb-16 animate-fade-in",
        align === "top" ? "pt-[67px] md:pt-[67px]" : "justify-center pt-8",
      )}>
        {children}
      </main>
      <AuthFooter />
    </div>
  );
}

function AuthHeader() {
  const [open, setOpen] = useState(false);
  return (
    <header className="relative">
      <div className="mx-auto flex h-[72px] w-full max-w-[1160px] items-center justify-between px-5 md:px-0">
        <Link to="/" className="flex items-center gap-2.5 rounded-md text-foreground" aria-label="5050cloud home">
          <Logo size={32} />
          <span className="text-[15px] font-semibold tracking-tight">5050cloud</span>
        </Link>
        <nav aria-label="Primary" className="hidden items-center gap-8 md:flex">
          {HEADER_LINKS.map((l) => (
            <a key={l.label} href={l.href} target="_blank" rel="noreferrer" className="rounded text-sm font-semibold text-fg-3 transition-colors duration-200 ease-out hover:text-foreground">
              {l.label}
            </a>
          ))}
          <a href={REPO_URL} target="_blank" rel="noreferrer" aria-label="GitHub repository" className="inline-flex items-center gap-2 rounded text-sm font-semibold text-fg-3 transition-colors duration-200 ease-out hover:text-foreground">
            <BrandIcon name="github" size={16} mono />
            GitHub
          </a>
        </nav>
        <button
          type="button"
          className="inline-flex h-[34px] w-[34px] items-center justify-center rounded-md text-fg-3 transition-colors duration-200 hover:bg-surface-hover hover:text-foreground md:hidden"
          aria-label={open ? "Close menu" : "Open menu"}
          aria-expanded={open}
          onClick={() => setOpen((v) => !v)}
        >
          {open ? <X size={18} /> : <Menu size={18} />}
        </button>
      </div>
      {open && (
        <nav aria-label="Mobile" className="absolute inset-x-0 top-[72px] z-10 border-b border-line bg-background px-5 py-2 md:hidden animate-rise-in">
          {HEADER_LINKS.concat([{ label: "GitHub", href: REPO_URL }]).map((l) => (
            <a key={l.label} href={l.href} target="_blank" rel="noreferrer" onClick={() => setOpen(false)} className="flex h-[42px] items-center text-base text-fg-2 hover:text-foreground">
              {l.label}
            </a>
          ))}
        </nav>
      )}
    </header>
  );
}

function AuthFooter() {
  return (
    <footer className="mx-auto w-full max-w-[1160px] px-5 pb-14 pt-12 md:px-0">
      <div className="grid grid-cols-2 gap-x-6 gap-y-10 md:grid-cols-[397px_repeat(4,199px)] md:gap-x-0">
        <div className="order-last col-span-2 flex flex-col items-center gap-3 text-center md:order-first md:col-span-1 md:max-w-[266px] md:items-start md:text-left">
          <Logo size={32} />
          <p className="font-mono text-xs leading-[18px] text-ok">
            <span className="mr-1.5 inline-block h-1.5 w-1.5 rounded-full bg-ok align-middle" aria-hidden />
            Open source, self-hosted
          </p>
          <div className="my-1 hidden h-px w-full bg-line md:block" aria-hidden />
          <p className="text-xs leading-[18px] text-fg-3">
            &copy; {YEAR} 5050cloud contributors. Released under AGPL-3.0-only.
          </p>
        </div>
        {FOOTER_COLUMNS.map((col) => (
          <div key={col.heading} className="md:pr-6">
            <h2 className="text-xs font-bold uppercase leading-[18px] tracking-wide text-fg-2">{col.heading}</h2>
            <ul className="mt-2 md:mt-[9px]">
              {col.links.map((l) => (
                <li key={l.label}>
                  <a href={l.href} target="_blank" rel="noreferrer" className="block rounded py-2 text-base leading-[26px] text-fg-2 transition-colors duration-200 ease-out hover:text-foreground">
                    {l.label}
                  </a>
                </li>
              ))}
            </ul>
          </div>
        ))}
      </div>
    </footer>
  );
}

/** Card width and the streak rows, as offsets from the card top (matches the target layout). */
const CARD_W = 380;
const STREAKS = [
  { top: 64, width: 120, delay: 0, duration: 2600 },
  { top: 144, width: 80, delay: 900, duration: 3100 },
  { top: 160, width: 120, delay: 2200, duration: 2800 },
  { top: 256, width: 90, delay: 1500, duration: 3300 },
  { top: 298, width: 120, delay: 3000, duration: 2900 },
];

/** Five 2px streak lines that drift across the card. Driven by the Web Animations API, stopped under reduced motion. */
function Streaks({ active }: { active: boolean }) {
  const refs = useRef<(HTMLSpanElement | null)[]>([]);
  useEffect(() => {
    if (!active || window.matchMedia("(prefers-reduced-motion: reduce)").matches) return;
    const anims = STREAKS.map((s, i) => refs.current[i]?.animate(
      [
        { transform: "translateX(-100%)", opacity: 0 },
        { opacity: 0.6, offset: 0.15 },
        { opacity: 0.6, offset: 0.85 },
        { transform: `translateX(${CARD_W}px)`, opacity: 0 },
      ],
      { duration: s.duration, delay: s.delay, iterations: Infinity, easing: "linear" },
    ));
    return () => anims.forEach((a) => a?.cancel());
  }, [active]);
  if (!active) return null;
  return (
    <div aria-hidden className="pointer-events-none absolute inset-0 overflow-hidden rounded-xl">
      {STREAKS.map((s, i) => (
        <div key={s.top} className="absolute left-0 right-0 h-[2px] overflow-hidden" style={{ top: s.top }}>
          <span
            ref={(el) => { refs.current[i] = el; }}
            className="absolute left-0 top-0 block h-[2px] rounded-full bg-fg-3"
            style={{ width: s.width, opacity: 0 }}
          />
        </div>
      ))}
    </div>
  );
}

/** The 380px card: logo, title, subtitle, then the form body. */
export function AuthCard({ title, subtitle, children, streaks = false, titleId = "auth-title", logoSize = 64, className, ...rest }: {
  title: ReactNode;
  subtitle?: ReactNode;
  children?: ReactNode;
  streaks?: boolean;
  titleId?: string;
  logoSize?: number;
  className?: string;
} & Omit<React.HTMLAttributes<HTMLElement>, "title">) {
  return (
    <section
      aria-labelledby={titleId}
      className={cx("relative w-[380px] max-w-full animate-rise-in rounded-xl border border-line px-8 pb-8 pt-[68px] text-center", className)}
      {...rest}
    >
      <Streaks active={streaks} />
      <div className="relative">
        <div className="flex justify-center" style={{ height: logoSize }}>
          <Logo size={logoSize} />
        </div>
        <h1 id={titleId} className="mt-4 text-[18px] font-medium leading-[27px] text-foreground">{title}</h1>
        {subtitle && <p className="mt-1 text-base leading-6 text-fg-3">{subtitle}</p>}
        {children && <div className="mt-10 text-left">{children}</div>}
      </div>
    </section>
  );
}

type AuthButtonProps = ButtonHTMLAttributes<HTMLButtonElement> & {
  busy?: boolean;
  iconRight?: ReactNode;
  variant?: "primary" | "secondary";
};

/** Full-width 42px button: accent with glow when enabled, the muted control style when disabled. */
export function AuthButton({ busy, iconRight, variant = "primary", className, children, disabled, type = "submit", ...p }: AuthButtonProps) {
  const off = disabled || busy;
  const primary = variant === "primary";
  return (
    <button
      {...p}
      type={type}
      disabled={off}
      aria-busy={busy || undefined}
      style={primary && !off ? { boxShadow: "0 4px 24px color-mix(in srgb, var(--pink-500) 27%, transparent)" } : undefined}
      className={cx(
        "inline-flex h-[42px] w-full items-center justify-center gap-3 rounded-md border px-3 text-sm font-medium",
        "transition-[background-color,border-color,color,transform] duration-200 ease-out active:scale-[.98] active:duration-[50ms]",
        "focus-visible:outline-none",
        primary && !off && "border-accent bg-accent text-accent-fg hover:border-accent-hover hover:bg-accent-hover",
        primary && off && "border-transparent bg-surface-hover text-fg-3 cursor-not-allowed",
        !primary && "border-line bg-surface-hover text-foreground hover:border-fg-5 hover:bg-line",
        className,
      )}
    >
      {busy ? <Loader2 size={16} className="animate-spin" aria-hidden /> : null}
      <span>{children}</span>
      {!busy && iconRight}
    </button>
  );
}

/** Four-segment strength hint for new passwords. Never blocks submit; the server enforces the minimum. */
export function PasswordMeter({ value }: { value: string }) {
  const score = passwordScore(value);
  const label = ["Too short", "Weak", "Fair", "Good", "Strong"][score];
  return (
    <div className="mt-2 space-y-1.5" aria-live="polite">
      <div className="flex gap-1" aria-hidden>
        {[0, 1, 2, 3].map((i) => (
          <span key={i} className={cx("h-1 flex-1 rounded-full transition-colors duration-200", i < score ? (score >= 3 ? "bg-ok" : "bg-warn-icon") : "bg-line")} />
        ))}
      </div>
      <p className="text-xs leading-[18px] text-fg-3">{value ? label : "At least 10 characters."}</p>
    </div>
  );
}

function passwordScore(v: string): number {
  if (v.length === 0) return 0;
  if (v.length < 10) return 1;
  let s = 2;
  if (/[a-z]/.test(v) && /[A-Z]/.test(v) && /\d/.test(v)) s++;
  if (v.length >= 14 || /[^A-Za-z0-9]/.test(v)) s++;
  return Math.min(s, 4);
}

/** Positive confirmation line (check mail, verified) in the success tone. */
export function SuccessLine({ children }: { children: ReactNode }) {
  return <p className="rounded-md border border-line bg-surface px-3 py-2 text-sm leading-5 text-ok-soft" role="status">{children}</p>;
}
