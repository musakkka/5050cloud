// SPDX-License-Identifier: AGPL-3.0-only

// A combobox for the New Item dialog: 42px field, 56px option rows with icons, optional search,
// arrow keys, Enter and Escape (Escape closes only the list).

import { useEffect, useId, useMemo, useRef, useState, type ReactNode } from "react";
import { Check, ChevronDown } from "lucide-react";
import { cx, useClickOutside } from "../../kit";

export interface ComboOption { value: string; label: string; icon?: ReactNode }

export function Combo({ value, options, onChange, label, searchable, placeholder, className, round, listWidth }: {
  value: string; options: ComboOption[]; onChange: (v: string) => void; label: string; searchable?: boolean; placeholder?: string;
  className?: string; round: string; listWidth?: number;
}) {
  const [open, setOpen] = useState(false);
  const [q, setQ] = useState("");
  const [hi, setHi] = useState(0);
  const root = useRef<HTMLDivElement>(null);
  const input = useRef<HTMLInputElement>(null);
  const listId = useId();
  useClickOutside(root, () => setOpen(false), open);
  const cur = options.find((o) => o.value === value);
  const shown = useMemo(() => (searchable && q ? options.filter((o) => o.label.toLowerCase().includes(q.toLowerCase())) : options), [options, q, searchable]);
  useEffect(() => { setHi(Math.max(0, shown.findIndex((o) => o.value === value))); }, [open, q]); // eslint-disable-line react-hooks/exhaustive-deps
  const pick = (o: ComboOption) => { onChange(o.value); setOpen(false); setQ(""); };
  const onKey = (e: React.KeyboardEvent) => {
    if (e.key === "Escape" && open) { e.stopPropagation(); setOpen(false); return; }
    if (e.key === "ArrowDown") { e.preventDefault(); if (!open) setOpen(true); else setHi((h) => Math.min(shown.length - 1, h + 1)); }
    else if (e.key === "ArrowUp") { e.preventDefault(); setHi((h) => Math.max(0, h - 1)); }
    else if (e.key === "Enter" && open && shown[hi]) { e.preventDefault(); pick(shown[hi]); }
  };
  return (
    <div ref={root} className={cx("relative", className)} onKeyDown={onKey}>
      <div className={cx("flex h-[42px] items-center gap-3 border bg-background px-4 transition-colors duration-150", round, open ? "border-fg-4" : "border-line hover:border-fg-5")}
        onClick={() => { setOpen(true); input.current?.focus(); }}>
        {!searchable || !open ? cur?.icon : null}
        {searchable ? (
          <input ref={input} role="combobox" aria-label={label} aria-expanded={open} aria-controls={listId} aria-autocomplete="list"
            value={open ? q : cur?.label ?? ""} placeholder={cur ? undefined : placeholder} onFocus={() => setOpen(true)} onChange={(e) => { setQ(e.target.value); setOpen(true); }}
            className={cx("h-full min-w-0 flex-1 bg-transparent text-base outline-none focus-visible:shadow-none placeholder:text-fg-4", cur ? "text-foreground" : "text-fg-4")} />
        ) : (
          <button type="button" role="combobox" aria-label={label} aria-expanded={open} aria-controls={listId} onClick={(e) => { e.stopPropagation(); setOpen((o) => !o); }}
            className="h-full min-w-0 flex-1 truncate text-left text-base text-foreground outline-none">{cur?.label}</button>
        )}
        <ChevronDown size={18} className={cx("shrink-0 text-foreground transition-transform duration-200", open && "rotate-180")} />
      </div>
      {open && (
        <ul id={listId} role="listbox" data-popover aria-label={label} style={{ width: listWidth }}
          className="scroll-thin absolute left-0 top-full z-30 mt-1 max-h-[300px] min-w-full overflow-y-auto rounded-lg border border-line-popover bg-surface-hover shadow-popover animate-pop-in">
          {shown.length === 0 && <li className="px-4 py-4 text-body text-fg-3">No results</li>}
          {shown.map((o, i) => (
            <li key={o.value} role="option" aria-selected={o.value === value} onMouseEnter={() => setHi(i)} onMouseDown={(e) => e.preventDefault()} onClick={() => pick(o)}
              className={cx("flex h-14 cursor-pointer items-center gap-3 px-4 text-lead transition-colors duration-100", i === hi ? "bg-surface-raised text-foreground" : "text-fg-menu")}>
              {o.icon}<span className="min-w-0 flex-1 truncate">{o.label}</span>{o.value === value && <Check size={16} className="text-fg-2" />}
            </li>
          ))}
        </ul>
      )}
      <span className="sr-only" aria-live="polite">{open ? `${shown.length} results available. Use Up and Down to choose options, press Enter to select` : ""}</span>
    </div>
  );
}
