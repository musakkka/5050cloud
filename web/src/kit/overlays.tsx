// SPDX-License-Identifier: AGPL-3.0-only

// Floating UI: Popover, Menu, MenuItem, Listbox, Tooltip, Modal, Sheet and Confirm.
// Popovers and tooltips render into <body> with fixed positioning, so no overflow-hidden parent
// can clip them. Escape and outside clicks close them and focus returns to the trigger.
import { cloneElement, isValidElement, useCallback, useEffect, useLayoutEffect, useRef, useState, type ReactElement, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { Check, ChevronDown, X } from "lucide-react";
import { cx } from "./util";
import { Button } from "./controls";

export function useClickOutside(ref: React.RefObject<HTMLElement>, onOut: () => void, active: boolean) {
  useEffect(() => {
    if (!active) return;
    // a click in a floating panel (rendered in <body>) belongs to whatever opened it
    const h = (e: MouseEvent) => { if (ref.current && !ref.current.contains(e.target as Node) && !(e.target as Element).closest?.("[data-popover]")) onOut(); };
    const k = (e: KeyboardEvent) => { if (e.key === "Escape") onOut(); };
    document.addEventListener("mousedown", h);
    document.addEventListener("keydown", k);
    return () => { document.removeEventListener("mousedown", h); document.removeEventListener("keydown", k); };
  }, [active, onOut, ref]);
}

let typed = "";
let typedAt = 0;

/** Moves focus between [role=menuitem]/[role=option] buttons with the arrow keys, Home/End and type-ahead. */
function rovingKeys(e: React.KeyboardEvent<HTMLElement>) {
  const printable = e.key.length === 1 && !e.metaKey && !e.ctrlKey && !e.altKey && !(e.target instanceof HTMLInputElement);
  if (!printable && !["ArrowDown", "ArrowUp", "Home", "End"].includes(e.key)) return;
  const items = Array.from(e.currentTarget.querySelectorAll<HTMLElement>('[role=menuitem]:not([disabled]),[role=option]:not([aria-disabled=true]),[role=menuitemcheckbox]:not([disabled])'));
  if (!items.length) return;
  if (printable) {
    const now = Date.now();
    typed = now - typedAt > 500 ? e.key.toLowerCase() : typed + e.key.toLowerCase();
    typedAt = now;
    const from = Math.max(0, items.indexOf(document.activeElement as HTMLElement));
    const order = [...items.slice(from + (typed.length === 1 ? 1 : 0)), ...items.slice(0, from + (typed.length === 1 ? 1 : 0))];
    const hit = order.find((el) => (el.textContent || "").trim().toLowerCase().startsWith(typed));
    if (hit) { e.preventDefault(); hit.focus(); }
    return;
  }
  e.preventDefault();
  const i = items.indexOf(document.activeElement as HTMLElement);
  const n = e.key === "Home" ? 0 : e.key === "End" ? items.length - 1 : e.key === "ArrowDown" ? (i + 1) % items.length : (i - 1 + items.length) % items.length;
  items[n].focus();
}

export type Placement = "bottom" | "top" | "right" | "left";

export interface PopoverProps {
  trigger: (open: boolean) => ReactNode;
  children: (close: () => void) => ReactNode;
  /** Horizontal alignment for top/bottom placement. */
  align?: "left" | "right" | "center";
  /** Preferred side; flips when short of room. Default bottom. */
  placement?: Placement;
  minWidth?: number;
  /** Fixed panel width (e.g. 280 for the user menu, 380 for notifications). */
  width?: number;
  /** Gap between the trigger and the panel. Default 4. */
  offset?: number;
  className?: string;
  /** Controlled open state (optional). */
  open?: boolean;
  onOpenChange?: (open: boolean) => void;
  /** ARIA role of the panel. Default "menu" when it holds MenuItems. */
  role?: string;
  /** Inline trigger wrapper class (defaults to a block-level min-w-0 div). */
  wrapperClassName?: string;
}

/** A floating panel anchored to its trigger: 6px radius, 1px light border, 4px padding, pop-in motion. */
export function Popover({
  trigger, children, align = "left", placement = "bottom", minWidth = 180, width, offset = 4, className,
  open: openProp, onOpenChange, role = "menu", wrapperClassName,
}: PopoverProps) {
  const [openState, setOpenState] = useState(false);
  const open = openProp ?? openState;
  const setOpen = useCallback((v: boolean | ((o: boolean) => boolean)) => {
    const next = typeof v === "function" ? v(openProp ?? openState) : v;
    if (openProp === undefined) setOpenState(next);
    onOpenChange?.(next);
  }, [openProp, openState, onOpenChange]);
  const [style, setStyle] = useState<React.CSSProperties>({ position: "fixed", visibility: "hidden" });
  const [side, setSide] = useState<Placement>(placement);
  const anchor = useRef<HTMLDivElement>(null);
  const panel = useRef<HTMLDivElement>(null);
  const close = useCallback(() => {
    setOpen(false);
    // focus returns to the trigger
    const f = anchor.current?.querySelector<HTMLElement>("button,[tabindex],a,input");
    f?.focus({ preventScroll: true });
  }, [setOpen]);
  useLayoutEffect(() => {
    if (!open) return;
    const place = () => {
      const r = anchor.current?.getBoundingClientRect();
      if (!r) return;
      const vw = window.innerWidth, vh = window.innerHeight, edge = 8;
      const pw = width ?? Math.max(panel.current?.offsetWidth || 0, minWidth, r.width);
      const ph = panel.current?.offsetHeight || 0;
      const s: React.CSSProperties = { position: "fixed", minWidth: width ? undefined : Math.max(minWidth, r.width), width, maxWidth: vw - 2 * edge };
      let used: Placement = placement;
      if (placement === "right" || placement === "left") {
        const roomR = vw - r.right - offset - edge, roomL = r.left - offset - edge;
        used = placement === "right" ? (roomR < pw && roomL > roomR ? "left" : "right") : (roomL < pw && roomR > roomL ? "right" : "left");
        if (used === "right") s.left = r.right + offset; else s.right = vw - r.left + offset;
        s.top = Math.max(edge, Math.min(r.top, vh - ph - edge));
        s.maxHeight = vh - 2 * edge;
      } else {
        const below = vh - r.bottom - offset - edge, above = r.top - offset - edge;
        const up = placement === "top" ? !(above < Math.min(ph || 220, 220) && below > above) : (below < Math.min(ph || 220, 220) && above > below);
        used = up ? "top" : "bottom";
        if (align === "right") s.right = Math.max(edge, vw - r.right);
        else if (align === "center") s.left = Math.max(edge, Math.min(r.left + r.width / 2 - pw / 2, vw - pw - edge));
        else s.left = Math.max(edge, Math.min(r.left, vw - pw - edge));
        if (up) { s.bottom = vh - r.top + offset; s.maxHeight = above; } else { s.top = r.bottom + offset; s.maxHeight = below; }
      }
      setSide(used);
      setStyle(s);
    };
    place();
    const again = requestAnimationFrame(place);   // once more with the panel's real size
    window.addEventListener("resize", place);
    window.addEventListener("scroll", place, true);
    const down = (e: MouseEvent) => {
      const t = e.target as Node;
      if (!anchor.current?.contains(t) && !panel.current?.contains(t) && !(t as Element).closest?.("[data-popover]")) setOpen(false);
    };
    const key = (e: KeyboardEvent) => { if (e.key === "Escape") { e.stopPropagation(); close(); } };
    document.addEventListener("mousedown", down);
    document.addEventListener("keydown", key);
    return () => {
      cancelAnimationFrame(again);
      window.removeEventListener("resize", place);
      window.removeEventListener("scroll", place, true);
      document.removeEventListener("mousedown", down);
      document.removeEventListener("keydown", key);
    };
  }, [open, align, placement, minWidth, width, offset, close, setOpen]);
  return (
    <div ref={anchor} className={wrapperClassName ?? "relative min-w-0"} onClick={(e) => e.stopPropagation()}>
      <div onClick={() => setOpen((o) => !o)}
        onKeyDown={(e) => { if (e.key === "ArrowDown" && !open) { e.preventDefault(); setOpen(true); } }}>
        {trigger(open)}
      </div>
      {open && createPortal(
        // portal events still bubble through the React tree: keep clicks away from canvas nodes and links
        <div ref={panel} data-popover role={role} style={style} onClick={(e) => e.stopPropagation()} onMouseDown={(e) => e.stopPropagation()} onKeyDown={rovingKeys}
          className={cx(
            "z-[200] overflow-y-auto scroll-thin rounded-md border border-line-popover bg-background p-1 text-sm text-foreground shadow-popover",
            side === "top" ? "animate-pop-in-up" : "animate-pop-in",
            className,
          )}>
          {children(close)}
        </div>,
        document.body,
      )}
    </div>
  );
}

export interface MenuItemProps {
  children: ReactNode;
  onClick: () => void;
  /** Shows a check (current workspace, current environment). */
  active?: boolean;
  /** Where the check sits. Default right. */
  checkSide?: "left" | "right";
  danger?: boolean;
  disabled?: boolean;
  /** Leading 16px icon or avatar. */
  icon?: ReactNode;
  /** Right-aligned hint: shortcut chips, a count, an external-link glyph. */
  hint?: ReactNode;
  /** Second muted line. */
  description?: ReactNode;
  /** Accent-tinted icon for "+ New ..." rows. */
  accentIcon?: boolean;
}

/** A 36px menu row: 14px/500 secondary text, white on hover. */
export function MenuItem({ children, onClick, active, checkSide = "right", danger, disabled, icon, hint, description, accentIcon }: MenuItemProps) {
  return (
    <button type="button" role="menuitem" disabled={disabled} onClick={onClick}
      className={cx(
        "flex w-full items-center gap-2.5 rounded px-3 text-left text-sm font-medium outline-none transition-colors duration-150",
        description ? "min-h-[44px] py-1.5" : "min-h-[36px]",
        "hover:bg-surface-hover focus-visible:bg-surface-hover focus-visible:shadow-none disabled:pointer-events-none disabled:opacity-40",
        danger ? "text-danger-text hover:text-danger-soft" : active ? "text-fg-menu" : "text-fg-2 hover:text-foreground focus-visible:text-foreground",
      )}>
      {checkSide === "left" && <span className="flex w-4 shrink-0 justify-center">{active && <Check size={16} className="text-accent-text" />}</span>}
      {icon && <span className={cx("flex shrink-0 items-center [&>svg]:h-4 [&>svg]:w-4", accentIcon && "text-accent-text")}>{icon}</span>}
      <span className="min-w-0 flex-1">
        <span className="block truncate">{children}</span>
        {description && <span className="block truncate text-caption font-normal text-fg-3">{description}</span>}
      </span>
      {hint && <span className="flex shrink-0 items-center gap-0.5 text-caption font-normal text-fg-4">{hint}</span>}
      {checkSide === "right" && active && <Check size={16} className="shrink-0 text-accent-text" />}
    </button>
  );
}

/** A 12px/500 section label inside a menu. */
export function MenuLabel({ children, icon }: { children: ReactNode; icon?: ReactNode }) {
  return <div className="flex items-center gap-2 px-3 pb-1 pt-1.5 text-caption font-medium text-fg-2">{icon}{children}</div>;
}

export function MenuSeparator() {
  return <div role="separator" className="-mx-1 my-1 h-px bg-line-popover" />;
}

export type MenuEntry =
  | { label: string; onClick: () => void; danger?: boolean; disabled?: boolean; icon?: ReactNode; hint?: ReactNode; active?: boolean }
  | "separator"
  | { heading: string };

/** A trigger plus a list of actions. `items` may include "separator" and { heading }. */
export function Menu({ trigger, items, align = "right", width, minWidth = 180, placement }: {
  trigger: ReactNode; items: MenuEntry[]; align?: "left" | "right"; width?: number; minWidth?: number; placement?: Placement;
}) {
  return (
    <Popover align={align} width={width} minWidth={minWidth} placement={placement} trigger={() => trigger}>
      {(close) => items.map((it, i) => {
        if (it === "separator") return <MenuSeparator key={`s${i}`} />;
        if ("heading" in it) return <MenuLabel key={`h${i}`}>{it.heading}</MenuLabel>;
        return (
          <MenuItem key={it.label} danger={it.danger} disabled={it.disabled} icon={it.icon} hint={it.hint} active={it.active}
            onClick={() => { close(); it.onClick(); }}>{it.label}</MenuItem>
        );
      })}
    </Popover>
  );
}

export interface ListboxOption<T extends string = string> { value: T; label: string; icon?: ReactNode; description?: string; disabled?: boolean }

/** A custom dropdown select (activity filters, environment pickers): a 34px trigger and a popover list with a check. */
export function Listbox<T extends string = string>({ value, options, onChange, placeholder = "Select", label, width, className, size = "md" }: {
  value: T | undefined; options: ListboxOption<T>[]; onChange: (v: T) => void; placeholder?: string; label?: string; width?: number; className?: string; size?: "sm" | "md";
}) {
  const cur = options.find((o) => o.value === value);
  return (
    <Popover role="listbox" width={width} minWidth={160} trigger={(open) => (
      <button type="button" aria-label={label ? `${label}: ${cur?.label ?? placeholder}` : undefined} aria-haspopup="listbox" aria-expanded={open}
        className={cx("inline-flex max-w-full items-center gap-2 rounded-md border border-line px-2.5 text-foreground transition-colors duration-150 hover:border-fg-5",
          size === "sm" ? "h-[30px] text-caption" : "h-[34px] text-sm", open && "border-fg-5", className)}>
        {cur?.icon}
        <span className={cx("min-w-0 flex-1 truncate text-left", !cur && "text-fg-4")}>{cur?.label ?? placeholder}</span>
        <ChevronDown size={16} className={cx("shrink-0 text-fg-4 transition-transform duration-200", open && "rotate-180")} />
      </button>
    )}>
      {(close) => options.map((o) => (
        <button key={o.value} type="button" role="option" aria-selected={o.value === value} aria-disabled={o.disabled || undefined} disabled={o.disabled}
          onClick={() => { close(); onChange(o.value); }}
          className={cx("flex min-h-[36px] w-full items-center gap-2.5 rounded px-3 text-left text-sm font-medium outline-none transition-colors hover:bg-surface-hover focus-visible:bg-surface-hover focus-visible:shadow-none disabled:opacity-40",
            o.value === value ? "text-fg-menu" : "text-fg-2 hover:text-foreground")}>
          {o.icon && <span className="flex shrink-0 [&>svg]:h-4 [&>svg]:w-4">{o.icon}</span>}
          <span className="min-w-0 flex-1"><span className="block truncate">{o.label}</span>{o.description && <span className="block truncate text-caption font-normal text-fg-3">{o.description}</span>}</span>
          {o.value === value && <Check size={16} className="shrink-0 text-accent-text" />}
        </button>
      ))}
    </Popover>
  );
}

/** A hover/focus tooltip: #33323e chip, 14px text, arrow toward the target, 100ms delay. */
export function Tooltip({ content, children, side = "top", delay = 100, disabled }: {
  content: ReactNode; children: ReactElement; side?: Placement; delay?: number; disabled?: boolean;
}) {
  const [rect, setRect] = useState<DOMRect | null>(null);
  const timer = useRef<number>();
  const ref = useRef<HTMLElement | null>(null);
  const show = (el: HTMLElement) => { window.clearTimeout(timer.current); timer.current = window.setTimeout(() => setRect(el.getBoundingClientRect()), delay); };
  const hide = () => { window.clearTimeout(timer.current); setRect(null); };
  useEffect(() => () => window.clearTimeout(timer.current), []);
  if (disabled || !isValidElement(children)) return children;
  const child = children as ReactElement<any>;
  const trigger = cloneElement(child, {
    ref: (n: HTMLElement | null) => { ref.current = n; },
    onMouseEnter: (e: React.MouseEvent<HTMLElement>) => { child.props.onMouseEnter?.(e); show(e.currentTarget); },
    onMouseLeave: (e: React.MouseEvent<HTMLElement>) => { child.props.onMouseLeave?.(e); hide(); },
    onFocus: (e: React.FocusEvent<HTMLElement>) => { child.props.onFocus?.(e); show(e.currentTarget); },
    onBlur: (e: React.FocusEvent<HTMLElement>) => { child.props.onBlur?.(e); hide(); },
    onClick: (e: React.MouseEvent<HTMLElement>) => { child.props.onClick?.(e); hide(); },
  });
  let pos: React.CSSProperties = {};
  let arrow: React.CSSProperties = {};
  const gap = 6;
  if (rect) {
    if (side === "right") { pos = { left: rect.right + gap, top: rect.top + rect.height / 2, transform: "translateY(-50%)" }; arrow = { left: -5, top: "50%", transform: "translateY(-50%)", borderWidth: "6px 6px 6px 0", borderColor: "transparent var(--tooltip) transparent transparent" }; }
    else if (side === "left") { pos = { right: window.innerWidth - rect.left + gap, top: rect.top + rect.height / 2, transform: "translateY(-50%)" }; arrow = { right: -5, top: "50%", transform: "translateY(-50%)", borderWidth: "6px 0 6px 6px", borderColor: "transparent transparent transparent var(--tooltip)" }; }
    else if (side === "bottom") { pos = { left: rect.left + rect.width / 2, top: rect.bottom + gap, transform: "translateX(-50%)" }; arrow = { top: -5, left: "50%", transform: "translateX(-50%)", borderWidth: "0 6px 6px 6px", borderColor: "transparent transparent var(--tooltip) transparent" }; }
    else { pos = { left: rect.left + rect.width / 2, bottom: window.innerHeight - rect.top + gap, transform: "translateX(-50%)" }; arrow = { bottom: -5, left: "50%", transform: "translateX(-50%)", borderWidth: "6px 6px 0 6px", borderColor: "var(--tooltip) transparent transparent transparent" }; }
  }
  return (
    <>
      {trigger}
      {rect && createPortal(
        <div role="tooltip" style={{ position: "fixed", ...pos }}
          className="pointer-events-none z-[400] animate-fade-in whitespace-nowrap rounded bg-tooltip px-3 py-2 text-sm leading-[19.25px] text-tooltip-fg shadow-popover">
          {content}
          <span aria-hidden className="absolute h-0 w-0 border-solid" style={arrow} />
        </div>,
        document.body,
      )}
    </>
  );
}

/** Open overlays, innermost last: Escape closes only the top one. */
const stack: object[] = [];

/** Locks body scroll and closes the topmost overlay on Escape (an open popover takes Escape first). */
function useOverlay(onClose: () => void) {
  const cb = useRef(onClose);
  cb.current = onClose;
  useEffect(() => {
    const me = {};
    stack.push(me);
    const prev = document.activeElement as HTMLElement | null;
    const key = (e: KeyboardEvent) => {
      if (e.key !== "Escape" || stack[stack.length - 1] !== me || document.querySelector("[data-popover]")) return;
      e.stopPropagation();
      cb.current();
    };
    document.addEventListener("keydown", key);
    const overflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    return () => {
      stack.splice(stack.indexOf(me), 1);
      document.removeEventListener("keydown", key);
      document.body.style.overflow = overflow;
      prev?.focus?.({ preventScroll: true });
    };
  }, []);
}

export interface ModalProps {
  title: string;
  children: ReactNode;
  onClose: () => void;
  footer?: ReactNode;
  /** Max width in px. Default 480. */
  width?: number;
  /** Muted line under the title. */
  description?: ReactNode;
  /** Hides the title row (custom layouts such as two-column dialogs). The title stays as the aria-label. */
  bare?: boolean;
  /** With `bare`, also hides the corner close button (confirm dialogs that close from their own buttons). */
  hideClose?: boolean;
  /** Footer alignment. Default end. */
  footerAlign?: "end" | "between";
  className?: string;
}

/** A centred dialog: 80% black overlay fading in, panel scaling .8 to 1 over 200ms, 8px radius, raised surface. */
export function Modal({ title, children, onClose, footer, width = 480, description, bare, hideClose, footerAlign = "end", className }: ModalProps) {
  const ref = useRef<HTMLDivElement>(null);
  useOverlay(onClose);
  useEffect(() => {
    const el = ref.current?.querySelector<HTMLElement>("[autofocus],input,textarea,select");
    if (el) el.focus(); else ref.current?.focus();
  }, []);
  return createPortal(
    <div className="fixed inset-0 z-[100] flex items-center justify-center bg-overlay p-4 animate-overlay-in"
      onMouseDown={(e) => { if (e.target === e.currentTarget) onClose(); }}>
      <div ref={ref} role="dialog" aria-modal="true" aria-label={title} tabIndex={-1}
        className={cx("relative flex max-h-[90vh] w-full flex-col overflow-hidden rounded-lg bg-surface-raised text-foreground shadow-modal outline-none animate-modal-in", className)}
        style={{ maxWidth: width }}>
        {!bare && (
          <div className="flex items-start justify-between gap-4 px-6 pb-3 pt-6">
            <div className="min-w-0">
              <h3 className="text-h3">{title}</h3>
              {description && <p className="mt-1 text-sm text-fg-2">{description}</p>}
            </div>
            <button type="button" aria-label="Close" onClick={onClose}
              className="-mr-2 -mt-1 inline-flex h-[26px] w-[26px] shrink-0 items-center justify-center rounded-md text-fg-4 transition-colors hover:bg-surface-hover hover:text-foreground">
              <X size={14} />
            </button>
          </div>
        )}
        {bare && !hideClose && (
          <button type="button" aria-label="Close" onClick={onClose}
            className="absolute right-3 top-3 z-10 inline-flex h-[26px] w-[26px] items-center justify-center rounded-md text-fg-4 transition-colors hover:bg-surface-hover hover:text-foreground">
            <X size={14} />
          </button>
        )}
        <div className={cx("min-h-0 flex-1 overflow-y-auto scroll-thin text-sm text-fg-2", !bare && "px-6 pb-2")}>{children}</div>
        {footer && <div className={cx("flex items-center gap-2 px-6 pb-6 pt-4", footerAlign === "between" ? "justify-between" : "justify-end")}>{footer}</div>}
      </div>
    </div>,
    document.body,
  );
}

export interface SheetProps {
  title?: ReactNode;
  /** Accessible name when the title is a node. */
  label?: string;
  children: ReactNode;
  onClose: () => void;
  /** Panel width in px. Default 560. */
  width?: number;
  /** Header actions next to the close button. */
  actions?: ReactNode;
  /** Dim the page behind the sheet. Default false (the canvas stays visible). */
  overlay?: boolean;
  className?: string;
}

/** A right-side drawer: an r12 card inset 8px from the viewport edge, sliding in from the right. */
export function Sheet({ title, label, children, onClose, width = 560, actions, overlay, className }: SheetProps) {
  useOverlay(onClose);
  return createPortal(
    <div className={cx("fixed inset-0 z-[90] flex justify-end p-2", overlay ? "bg-overlay animate-overlay-in" : "pointer-events-none")}
      onMouseDown={(e) => { if (overlay && e.target === e.currentTarget) onClose(); }}>
      <aside role="dialog" aria-modal={overlay ? true : undefined} aria-label={label ?? (typeof title === "string" ? title : undefined)}
        className={cx("pointer-events-auto flex h-full w-full flex-col overflow-hidden rounded-panel border border-line bg-background shadow-modal animate-sheet-in", className)}
        style={{ maxWidth: width }}>
        {(title || actions) && (
          <div className="flex h-14 shrink-0 items-center justify-between gap-3 border-b border-line px-5">
            <div className="min-w-0 truncate text-h3">{title}</div>
            <div className="flex shrink-0 items-center gap-1">
              {actions}
              <button type="button" aria-label="Close" onClick={onClose}
                className="inline-flex h-[30px] w-[30px] items-center justify-center rounded-md text-fg-3 transition-colors hover:bg-surface-hover hover:text-foreground"><X size={16} /></button>
            </div>
          </div>
        )}
        <div className="min-h-0 flex-1 overflow-y-auto scroll-thin">{children}</div>
      </aside>
    </div>,
    document.body,
  );
}
/** Alias: a drawer is a sheet. */
export const Drawer = Sheet;

/** Confirms an action; destructive ones can require typing a name. */
export function Confirm({ title, body, confirm, danger, typeName, onConfirm, onClose }: {
  title: string; body: ReactNode; confirm: string; danger?: boolean; typeName?: string; onConfirm: () => Promise<unknown> | void; onClose: () => void;
}) {
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<unknown>(null);
  const [typed, setTyped] = useState("");
  return (
    <Modal title={title} onClose={onClose}
      footer={<>
        <Button variant="ghost" onClick={onClose}>Cancel</Button>
        <Button variant={danger ? "danger" : "primary"} busy={busy} disabled={!!typeName && typed !== typeName} onClick={async () => {
          setBusy(true); setErr(null);
          try { await onConfirm(); onClose(); } catch (e) { setErr(e); setBusy(false); }
        }}>{confirm}</Button>
      </>}>
      <div className="space-y-4 leading-6">
        <div>{body}</div>
        {typeName && (
          <label className="block space-y-2">
            <span className="block text-caption font-semibold text-fg-2">Type <span className="font-mono text-foreground">{typeName}</span> to confirm</span>
            <input autoFocus value={typed} onChange={(e) => setTyped(e.target.value)}
              className="h-[42px] w-full rounded-md border border-line bg-transparent px-3 text-base text-foreground outline-none focus:border-accent focus:shadow-none" />
          </label>
        )}
        {!!err && <div role="alert" className="rounded-md border border-danger-line bg-danger-bg px-3 py-2 text-sm text-danger-text">{err instanceof Error ? err.message : String(err)}</div>}
      </div>
    </Modal>
  );
}
