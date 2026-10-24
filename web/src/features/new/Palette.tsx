// SPDX-License-Identifier: AGPL-3.0-only

// The create picker: a 420px floating card with a tinted search input and a
// keyboard driven list. Steps slide in (push going deeper, pop going back)
// and the card eases its height as the content changes.
import { useCallback, useEffect, useLayoutEffect, useRef, useState, type KeyboardEvent, type ReactNode } from "react";
import { ArrowLeft, ChevronRight, CornerDownLeft } from "lucide-react";
import { cx } from "../../kit";

export type StepDir = "push" | "pop" | undefined;

export interface PItem {
  key: string;
  icon?: ReactNode;
  label: ReactNode;
  /** A second line (template descriptions). Makes the row tall. */
  sub?: ReactNode;
  /** A third line of small stats. */
  meta?: ReactNode;
  /** Trailing node: a small button, a spinner or a badge. */
  right?: ReactNode;
  chevron?: boolean;
  /** Shows the Enter glyph at the right. */
  enter?: boolean;
  /** Message rows ("No templates found") are shown but never highlighted. */
  inert?: boolean;
  disabled?: boolean;
  /** Draw the icon in the accent colour (suggested action rows). */
  accent?: boolean;
  /** A hairline above this row (groups extra options). */
  divider?: boolean;
  onPick?: () => void;
}

const pickable = (i: PItem) => !i.inert && !i.disabled && !!i.onPick;

/** The floating card. Animates its height to fit the current step. */
export function PaletteFrame({ children, className }: { children: ReactNode; className?: string }) {
  const inner = useRef<HTMLDivElement>(null);
  const [h, setH] = useState<number>();
  useLayoutEffect(() => {
    const el = inner.current;
    if (!el) return;
    const ro = new ResizeObserver(() => setH(el.offsetHeight));
    ro.observe(el);
    setH(el.offsetHeight);
    return () => ro.disconnect();
  }, []);
  return (
    <div className={cx("pointer-events-auto w-full overflow-hidden rounded-xl border border-line-faint bg-surface-hover text-sm sm:w-[420px]",
      "shadow-[0_13px_29px_rgba(20,17,29,.15),0_53px_53px_rgba(20,17,29,.13),0_119px_71px_rgba(20,17,29,.08),0_211px_84px_rgba(20,17,29,.02)]",
      "transition-[height] duration-200 ease-out motion-reduce:transition-none animate-rise-in", className)}
      style={{ height: h }}>
      <div ref={inner} className="bg-tint-hover">{children}</div>
    </div>
  );
}

/** One step's content: slides in from the right (push) or the left (pop). */
export function PaletteStep({ dir, children }: { dir: StepDir; children: ReactNode }) {
  return <div className={cx(dir === "push" && "animate-palette-push", dir === "pop" && "animate-palette-pop")}>{children}</div>;
}

export interface PaletteInputProps {
  value: string;
  onChange: (v: string) => void;
  placeholder: string;
  /** Leading glyph at the root step; sub-steps get a back button instead. */
  icon?: ReactNode;
  onBack?: () => void;
  onKeyDown?: (e: KeyboardEvent<HTMLInputElement>) => void;
  label: string;
  activeId?: string;
  listId?: string;
  mono?: boolean;
}

export function PaletteInput({ value, onChange, placeholder, icon, onBack, onKeyDown, label, activeId, listId, mono }: PaletteInputProps) {
  const ref = useRef<HTMLInputElement>(null);
  useEffect(() => { ref.current?.focus({ preventScroll: true }); }, []);
  return (
    <div className="relative w-full p-1.5 pb-1">
      <div className="absolute left-4 top-1.5 flex h-[46px] items-center">
        {onBack ? (
          <button type="button" aria-label="Back" onClick={onBack}
            className="-m-1 flex h-6 w-6 items-center justify-center rounded text-fg-4 transition-colors hover:bg-tint-active hover:text-foreground focus-visible:shadow-none">
            <ArrowLeft size={16} />
          </button>
        ) : <span className="flex text-fg-4" aria-hidden>{icon}</span>}
      </div>
      <input ref={ref} value={value} onChange={(e) => onChange(e.target.value)} onKeyDown={onKeyDown} placeholder={placeholder}
        aria-label={label} role="combobox" aria-expanded aria-controls={listId} aria-activedescendant={activeId} aria-autocomplete="list"
        autoComplete="off" spellCheck={false}
        className={cx("h-[46px] w-full rounded-md border border-pink-300 bg-pink-50 py-3 pl-9 pr-3 text-sm text-foreground outline-none",
          "placeholder:text-fg-4 transition-colors focus:border-pink-400 focus-visible:shadow-none", mono && "font-mono text-[13px]")} />
    </div>
  );
}

/** Keyboard model shared by every step: Up/Down move, Enter picks, Escape steps back. */
export function useListNav(items: PItem[], onEscape: () => void) {
  const [active, setActive] = useState(0);
  const [byKey, setByKey] = useState(false);
  const sig = items.map((i) => i.key).join("|");
  const firstPickable = items.findIndex(pickable);
  useEffect(() => { setActive(firstPickable < 0 ? 0 : firstPickable); }, [sig, firstPickable]);
  const move = useCallback((d: 1 | -1) => {
    if (!items.some(pickable)) return;
    setByKey(true);
    setActive((a) => {
      let n = a;
      for (let k = 0; k < items.length; k++) {
        n = (n + d + items.length) % items.length;
        if (pickable(items[n])) return n;
      }
      return a;
    });
  }, [items]);
  const onKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key === "ArrowDown") { e.preventDefault(); move(1); }
    else if (e.key === "ArrowUp") { e.preventDefault(); move(-1); }
    else if (e.key === "Enter") { const it = items[active]; if (it && pickable(it)) { e.preventDefault(); it.onPick!(); } }
    else if (e.key === "Escape") { e.preventDefault(); e.stopPropagation(); onEscape(); }
  };
  const hover = (i: number) => { setByKey(false); if (pickable(items[i])) setActive(i); };
  return { active, onKeyDown, hover, byKey };
}

/** The scrolling option list. */
export function PaletteList({ items, active, onHover, byKey, id, maxHeight = 368, children }: {
  items: PItem[]; active: number; onHover: (i: number) => void; byKey?: boolean; id: string; maxHeight?: number | string; children?: ReactNode;
}) {
  const rows = useRef<(HTMLDivElement | null)[]>([]);
  useEffect(() => { if (byKey) rows.current[active]?.scrollIntoView({ block: "nearest" }); }, [active, byKey]);
  if (!items.length && !children) return null;
  return (
    <div id={id} role="listbox" aria-label="Options" className="scroll-palette overflow-y-auto overscroll-contain py-2" style={{ maxHeight }}>
      {children}
      {items.map((it, i) => (
        <div key={it.key}>
          {it.divider && <div className="mx-4 my-2 h-px bg-tint-active" />}
          <PaletteRow item={it} id={`${id}-${i}`} active={i === active && pickable(it)} onHover={() => onHover(i)}
            refFn={(el) => { rows.current[i] = el; }} />
        </div>
      ))}
    </div>
  );
}

function PaletteRow({ item, active, onHover, id, refFn }: { item: PItem; active: boolean; onHover: () => void; id: string; refFn: (el: HTMLDivElement | null) => void }) {
  const tall = !!(item.sub || item.meta);
  return (
    <div ref={refFn} id={id} role="option" aria-selected={active} aria-disabled={item.disabled || item.inert || undefined}
      onMouseMove={onHover} onClick={() => { if (pickable(item)) item.onPick!(); }}
      className={cx("group mx-2 flex gap-4 rounded-lg p-3 transition-colors duration-100",
        tall ? "items-start" : "items-center",
        item.inert ? "cursor-default text-fg-3" : item.disabled ? "cursor-not-allowed text-fg-4" : "cursor-pointer",
        !item.inert && !item.disabled && (active ? "bg-surface-hover text-foreground" : "text-fg-3"))}>
      {item.icon && (
        <span className={cx("flex shrink-0 items-center justify-center", tall ? "h-5 w-5 -ml-0.5" : "h-4 w-4", item.accent && "text-accent-text")}>{item.icon}</span>
      )}
      <span className="min-w-0 flex-1">
        <span className={cx("block truncate", tall && "font-semibold")}>{item.label}</span>
        {item.sub && <span className="block truncate">{item.sub}</span>}
        {item.meta && <span className="mt-2 flex flex-wrap items-center gap-x-4 gap-y-1 text-caption">{item.meta}</span>}
      </span>
      {item.right}
      {item.enter && <CornerDownLeft size={14} className={cx("shrink-0 transition-opacity", active ? "opacity-100" : "opacity-40")} />}
      {item.chevron && <ChevronRight size={16} className="shrink-0" />}
    </div>
  );
}

/** A centred message card inside the picker (pasted repo, not found). */
export function PaletteCard({ icon, title, body, action }: { icon: ReactNode; title: ReactNode; body: ReactNode; action?: ReactNode }) {
  return (
    <div className="flex flex-col items-center px-6 pb-6 pt-5 text-center animate-fade-in">
      <div className="mb-2 flex h-8 items-center justify-center text-foreground">{icon}</div>
      <div className="max-w-full truncate font-semibold text-foreground">{title}</div>
      <div className="text-fg-3">{body}</div>
      {action && <div className="mt-4">{action}</div>}
    </div>
  );
}
