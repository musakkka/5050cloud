// SPDX-License-Identifier: AGPL-3.0-only

// Small shared pieces of the Variables tab: the reference list used by the
// name-field menu and the `${{` autocomplete, underlined fuzzy text, the
// dialog shell with its own focus and button layout.

import { useEffect, useRef, useState, type ReactNode } from "react";
import { Braces, Ellipsis, Info } from "lucide-react";
import { BrandIcon, Button, Modal, cx, hasBrandIcon } from "../../kit";

/** Renders `text` with the characters at `hits` underlined. */
export function Hit({ text, hits }: { text: string; hits: number[] }) {
  if (!hits.length) return <>{text}</>;
  const set = new Set(hits);
  const out: ReactNode[] = [];
  let run = "";
  let on = false;
  const flush = (k: number) => {
    if (!run) return;
    out.push(on ? <span key={k} className="underline decoration-fg-3 underline-offset-2 text-foreground">{run}</span> : run);
    run = "";
  };
  [...text].forEach((ch, i) => {
    const h = set.has(i);
    if (h !== on) { flush(i); on = h; }
    run += ch;
  });
  flush(text.length);
  return <>{out}</>;
}

export interface ListEntry {
  key: string;
  /** Icon key or image ref for BrandIcon; undefined renders the braces glyph. */
  icon?: string;
  label: ReactNode;
  right?: string;
}

const LIMIT = 13;

/** The dropdown frame: 4px radius, hover-grey surface, 36px rows, "Show N More" after 13 rows. */
export function RefList({ entries, active, onActive, onPick, header, empty, className, expanded, onExpand }: {
  entries: ListEntry[]; active: number; onActive: (i: number) => void; onPick: (i: number) => void;
  header?: string; empty?: string; className?: string; expanded: boolean; onExpand: () => void;
}) {
  const shown = expanded ? entries : entries.slice(0, LIMIT);
  const more = entries.length - shown.length;
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    ref.current?.querySelector<HTMLElement>('[aria-selected="true"]')?.scrollIntoView({ block: "nearest" });
  }, [active, entries.length]);
  return (
    <div ref={ref} role="listbox" aria-label={header ?? "Suggestions"}
      onMouseDown={(e) => e.preventDefault()}
      className={cx("scroll-thin z-30 max-h-[527px] overflow-y-auto rounded border border-gray-200 bg-gray-100 p-2 shadow-popover animate-pop-in", className)}>
      {header && (
        <div className="flex items-center gap-2 px-2 pb-2 pt-1 text-caption text-fg-3"><Info size={14} className="shrink-0" />{header}</div>
      )}
      {entries.length === 0 && (
        <div className="flex h-[38px] items-center px-2 text-sm italic text-fg-3">{empty ?? "No suggestions"}</div>
      )}
      {shown.map((e, i) => (
        <div key={e.key} role="option" aria-selected={i === active} onMouseEnter={() => onActive(i)} onClick={() => onPick(i)}
          className={cx("flex h-9 cursor-pointer items-center gap-2 rounded-md px-2 font-mono text-sm text-fg-2", i === active && "bg-gray-200 text-foreground")}>
          <span className="flex h-4 w-4 shrink-0 items-center justify-center">
            {e.icon && hasBrandIcon(e.icon) ? <BrandIcon name={e.icon} size={16} fallback="none" /> : <Braces size={14} className="text-fg-4" />}
          </span>
          <span className="min-w-0 flex-1 truncate">{e.label}</span>
          {e.right && <span className="shrink-0 pl-3 font-sans text-fg-3">{e.right}</span>}
        </div>
      ))}
      {more > 0 && (
        <button type="button" onClick={onExpand} className="flex h-9 w-full items-center gap-2 rounded-md px-2 text-left text-sm text-fg-3 hover:bg-gray-200 hover:text-foreground">
          <Ellipsis size={16} />Show {more} More
        </button>
      )}
    </div>
  );
}

/** Keyboard handling shared by both dropdowns. Returns true when the key was consumed. */
export function listKeys(e: React.KeyboardEvent, count: number, active: number, set: (i: number) => void, pick: (i: number) => void, close: () => void): boolean {
  if (e.key === "ArrowDown" && count) { e.preventDefault(); set((active + 1) % count); return true; }
  if (e.key === "ArrowUp" && count) { e.preventDefault(); set((active - 1 + count) % count); return true; }
  if ((e.key === "Enter" || e.key === "Tab") && count && !e.shiftKey) { e.preventDefault(); pick(active); return true; }
  if (e.key === "Escape") { e.preventDefault(); e.stopPropagation(); close(); return true; }
  return false;
}

/** Dialog shell: bare Modal with 24px padding (confirm) or 32px (large), own title, button row. */
export function Dialog({ title, subtitle, children, onClose, width = 500, large, footer, label }: {
  title: string; subtitle?: ReactNode; children?: ReactNode; onClose: () => void; width?: number; large?: boolean; footer: ReactNode; label?: string;
}) {
  return (
    <Modal title={label ?? title} bare width={width} onClose={onClose} className={large ? "!rounded-xl" : "!rounded-lg"}>
      <div className={large ? "p-8" : "p-6"}>
        <h2 className="pr-8 text-[20px] font-semibold leading-[30px] text-foreground">{title}</h2>
        {subtitle && <p className="mt-1 text-base text-fg-2">{subtitle}</p>}
        {children && <div className="mt-4 text-base leading-6 text-fg-2">{children}</div>}
        <div className="mt-6 flex items-center justify-end gap-4">{footer}</div>
      </div>
    </Modal>
  );
}

/** Confirm dialog: Cancel (focused first) and the action button, both 42px. */
export function ConfirmDialog({ title, body, confirm, danger, onConfirm, onClose }: {
  title: string; body: ReactNode; confirm: string; danger?: boolean; onConfirm: () => Promise<unknown>; onClose: () => void;
}) {
  const cancel = useRef<HTMLButtonElement>(null);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  useEffect(() => { const t = setTimeout(() => cancel.current?.focus(), 30); return () => clearTimeout(t); }, []);
  return (
    <Dialog title={title} onClose={onClose} width={500}
      footer={<>
        <Button ref={cancel} size="lg" onClick={onClose}>Cancel</Button>
        <Button size="lg" variant={danger ? "danger" : "primary"} busy={busy} onClick={async () => {
          setBusy(true); setErr("");
          try { await onConfirm(); onClose(); } catch (e) { setErr(e instanceof Error ? e.message : String(e)); setBusy(false); }
        }}>{confirm}</Button>
      </>}>
      {body}
      {err && <div role="alert" className="mt-3 text-sm text-danger-text">{err}</div>}
    </Dialog>
  );
}

/** Inline code chip used in dialog copy. */
export const Code = ({ children }: { children: ReactNode }) => <code className="font-mono text-pink-600">{children}</code>;
