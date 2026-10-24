// SPDX-License-Identifier: AGPL-3.0-only

// Canvas menus (right-click, kebab, toolbar): 220px panels, 6px radius, 8px padding, 32px rows
// with 4px radius and an accent-tinted hover, hairline separators and side submenus.
import { useEffect, useLayoutEffect, useRef, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { ChevronRight, Check } from "lucide-react";
import { Tooltip, cx } from "../../kit";

export type CtxEntry =
  | {
    label: ReactNode;
    /** A stable key when the label is not a string. */
    id?: string;
    icon?: ReactNode;
    onClick?: () => void;
    danger?: boolean;
    /** true, or the reason shown as a tooltip. */
    disabled?: boolean | string;
    hint?: ReactNode;
    /** A second muted line (layers menu). */
    description?: ReactNode;
    sub?: CtxEntry[];
    checked?: boolean;
    /** Radio-style selection shown as a filled row (layers menu). */
    selected?: boolean;
  }
  | "sep"
  | { heading: ReactNode };

const ROW = "flex w-full items-center gap-3 rounded px-3 text-left text-sm outline-none transition-colors duration-100";

function Rows({ items, onClose, onSub, subOpen, rowH }: {
  items: CtxEntry[]; onClose: () => void; onSub: (i: number | null, el?: HTMLElement) => void; subOpen: number | null; rowH: number;
}) {
  return (
    <>
      {items.map((it, i) => {
        if (it === "sep") return <div key={`s${i}`} role="separator" className="m-2 h-px bg-gray-100" />;
        if ("heading" in it) return <div key={`h${i}`} className="px-2 pb-1 pt-0.5 text-caption font-medium text-fg-2">{it.heading}</div>;
        const disabled = !!it.disabled;
        const open = subOpen === i;
        const row = (
          <button key={it.id ?? (typeof it.label === "string" ? it.label : i)} type="button" role="menuitem" data-ctx-row
            aria-checked={it.selected ?? undefined}
            aria-haspopup={it.sub ? "menu" : undefined} aria-expanded={it.sub ? open : undefined} aria-disabled={disabled || undefined}
            style={{ minHeight: rowH }}
            onMouseEnter={(e) => onSub(it.sub ? i : null, e.currentTarget)}
            onFocus={(e) => { if (!it.sub) onSub(null); else if (open) onSub(i, e.currentTarget); }}
            onKeyDown={(e) => { if (it.sub && (e.key === "ArrowRight" || e.key === "Enter")) { e.preventDefault(); e.stopPropagation(); onSub(i, e.currentTarget); } }}
            onClick={(e) => {
              e.stopPropagation();
              if (disabled) return;
              if (it.sub) { onSub(i, e.currentTarget); return; }
              onClose();
              it.onClick?.();
            }}
            className={cx(ROW, it.description ? "py-2" : "py-1.5",
              disabled ? "cursor-not-allowed text-fg-2 opacity-40"
                : it.danger ? "text-danger hover:bg-danger-bg focus-visible:bg-danger-bg focus-visible:shadow-none"
                  : cx("text-fg-2 hover:bg-pink-50 hover:text-pink-600 focus-visible:bg-pink-50 focus-visible:text-pink-600 focus-visible:shadow-none", open && "bg-pink-50 text-pink-600", it.selected && "bg-surface-hover text-foreground"))}>
            {it.icon && <span className="flex h-4 w-4 shrink-0 items-center justify-center [&>svg]:h-4 [&>svg]:w-4">{it.icon}</span>}
            <span className="min-w-0 flex-1">
              <span className="block truncate leading-5">{it.label}</span>
              {it.description && <span className="block truncate text-caption text-fg-3">{it.description}</span>}
            </span>
            {it.hint && <span className="shrink-0 text-caption text-fg-4">{it.hint}</span>}
            {it.checked && <Check size={16} className="shrink-0 text-accent-text" />}
            {it.sub && <ChevronRight size={16} className="shrink-0" />}
          </button>
        );
        return typeof it.disabled === "string" ? <Tooltip key={`t${i}`} content={it.disabled} side="right">{row}</Tooltip> : row;
      })}
    </>
  );
}

/** Arrow keys move between rows of one panel. */
function roving(e: React.KeyboardEvent<HTMLDivElement>) {
  if (e.key !== "ArrowDown" && e.key !== "ArrowUp" && e.key !== "Home" && e.key !== "End") return;
  const rows = [...e.currentTarget.querySelectorAll<HTMLElement>(":scope > [data-ctx-row], :scope > * > [data-ctx-row]")];
  if (!rows.length) return;
  e.preventDefault();
  const i = rows.indexOf(document.activeElement as HTMLElement);
  const n = e.key === "Home" ? 0 : e.key === "End" ? rows.length - 1 : e.key === "ArrowDown" ? (i + 1) % rows.length : (i - 1 + rows.length) % rows.length;
  rows[n].focus();
}

function Panel({ x, y, items, onClose, width, rowH, level, anchorLeft, onBack, up, pad }: {
  x: number; y: number; items: CtxEntry[]; onClose: () => void; width: number; rowH: number; level: number; anchorLeft?: number; onBack?: () => void; up?: boolean; pad: number;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const [at, setAt] = useState({ left: x, top: y, ready: false });
  const [sub, setSub] = useState<{ i: number; x: number; y: number; left: number } | null>(null);
  const timer = useRef<ReturnType<typeof setTimeout>>();
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    const vw = window.innerWidth, vh = window.innerHeight, edge = 8;
    let left = x, top = up ? y - el.offsetHeight : y;
    if (left + el.offsetWidth > vw - edge) left = anchorLeft !== undefined ? anchorLeft - el.offsetWidth - 4 : vw - el.offsetWidth - edge;
    if (top + el.offsetHeight > vh - edge) top = Math.max(edge, vh - el.offsetHeight - edge);
    setAt({ left: Math.max(edge, left), top, ready: true });
  }, [x, y, anchorLeft, up]);
  useEffect(() => {
    if (level === 0) requestAnimationFrame(() => ref.current?.focus({ preventScroll: true }));
    else requestAnimationFrame(() => ref.current?.querySelector<HTMLElement>("[data-ctx-row]")?.focus({ preventScroll: true }));
  }, [level]);
  const onSub = (i: number | null, el?: HTMLElement) => {
    clearTimeout(timer.current);
    const go = () => {
      if (i === null || !el) { setSub(null); return; }
      const r = el.getBoundingClientRect(), p = ref.current!.getBoundingClientRect();
      setSub({ i, x: p.right + 4, y: r.top - 8, left: p.left });
    };
    if (i === null) timer.current = setTimeout(go, 120); else go();
  };
  useEffect(() => () => clearTimeout(timer.current), []);
  const entry = sub ? items[sub.i] : undefined;
  return (
    <>
      <div ref={ref} role="menu" tabIndex={-1} data-ctx-menu
        style={{ left: at.left, top: at.top, width, padding: pad, visibility: at.ready ? "visible" : "hidden" }}
        onContextMenu={(e) => e.preventDefault()}
        onMouseDown={(e) => e.stopPropagation()}
        onKeyDown={(e) => {
          if (e.key === "ArrowLeft" && onBack) { e.preventDefault(); e.stopPropagation(); onBack(); return; }
          roving(e);
        }}
        className="cv-menu fixed z-[210] rounded-md border border-line bg-background text-foreground shadow-[0_2px_6px_rgba(0,0,0,.04),0_6px_24px_rgba(0,0,0,.24)] outline-none focus-visible:shadow-[0_2px_6px_rgba(0,0,0,.04),0_6px_24px_rgba(0,0,0,.24)]">
        <Rows items={items} onClose={onClose} onSub={onSub} subOpen={sub?.i ?? null} rowH={rowH} />
      </div>
      {sub && entry && entry !== "sep" && !("heading" in entry) && entry.sub && (
        <Panel x={sub.x} y={sub.y} anchorLeft={sub.left} items={entry.sub} onClose={onClose} width={width} rowH={rowH} level={level + 1} pad={pad}
          onBack={() => { setSub(null); ref.current?.querySelectorAll<HTMLElement>("[data-ctx-row]")[0]?.focus(); }} />
      )}
    </>
  );
}

/** A menu at a screen point. Escape, outside clicks, scrolling the canvas and blur close it. */
export function CtxMenu({ x, y, items, onClose, width = 220, rowH = 32, up, pad = 8 }: {
  x: number; y: number; items: CtxEntry[]; onClose: () => void; width?: number; rowH?: number;
  /** Panel padding: 8 for context menus, 4 for toolbar menus. */
  pad?: number;
  /** y is the bottom edge: the menu opens upward (layers menu). */
  up?: boolean;
}) {
  useEffect(() => {
    const down = (e: MouseEvent) => { if (!(e.target as Element).closest?.("[data-ctx-menu]")) onClose(); };
    const key = (e: KeyboardEvent) => { if (e.key === "Escape") { e.stopPropagation(); onClose(); } };
    const wheel = (e: WheelEvent) => { if (!(e.target as Element).closest?.("[data-ctx-menu]")) onClose(); };
    document.addEventListener("mousedown", down, true);
    document.addEventListener("keydown", key, true);
    window.addEventListener("wheel", wheel, { passive: true });
    window.addEventListener("blur", onClose);
    window.addEventListener("resize", onClose);
    return () => {
      document.removeEventListener("mousedown", down, true);
      document.removeEventListener("keydown", key, true);
      window.removeEventListener("wheel", wheel);
      window.removeEventListener("blur", onClose);
      window.removeEventListener("resize", onClose);
    };
  }, [onClose]);
  return createPortal(<Panel x={x} y={y} items={items} onClose={onClose} width={width} rowH={rowH} level={0} up={up} pad={pad} />, document.body);
}
