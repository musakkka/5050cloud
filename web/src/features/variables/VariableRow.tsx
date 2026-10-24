// SPDX-License-Identifier: AGPL-3.0-only

// One variable row: 40px pitch grid, hover-only reveal and copy, kebab menu,
// inline edit with reference highlighting.

import { useEffect, useRef, useState } from "react";
import { AlertTriangle, Braces, Check, Copy, Eye, EyeOff, EllipsisVertical, Globe, Lock, SquareFunction, SquarePen, Trash2, X } from "lucide-react";
import type { Variable } from "../../api/types.gen";
import { IconButton, Menu, Tooltip, cx } from "../../kit";
import { BracesButton, FIELD, MultilineModal, useGrow } from "./NewVariableForm";
import { isGenerated } from "./refs";

export const MASK = "*******";

export interface RowApi {
  /** Resolved value (references substituted) for reveal and copy. */
  resolved: (name: string) => Promise<string>;
  /** Raw stored text for editing. */
  raw: (v: Variable) => Promise<string>;
  save: (name: string, value: string) => Promise<void>;
  seal: (v: Variable) => void;
  promote: (v: Variable) => void;
  generator: (v: Variable) => void;
  remove: (v: Variable) => void;
}

/** Text with `${{...}}` tokens boxed: used as an underlay behind the edit field. */
function Underlay({ text }: { text: string }) {
  const parts = text.split(/(\$\{\{[^}]*\}\})/g);
  return (
    <div aria-hidden className="pointer-events-none absolute inset-0 whitespace-pre-wrap break-words py-1.5 pl-1.5 pr-7 text-sm leading-5 text-transparent" style={{ border: "1px solid transparent" }}>
      {parts.map((p, i) => (p.startsWith("${{") ? <span key={i} className="rounded-sm bg-gray-200 outline outline-1 outline-gray-200">{p}</span> : p))}
    </div>
  );
}

function InlineEdit({ initial, onSave, onCancel }: { initial: string; onSave: (v: string) => Promise<void>; onCancel: () => void }) {
  const [v, setV] = useState(initial);
  const [busy, setBusy] = useState(false);
  const [editor, setEditor] = useState(false);
  const ref = useGrow(v);
  useEffect(() => { ref.current?.focus(); }, [ref]);
  const save = async () => {
    setBusy(true);
    try { await onSave(v); } catch { setBusy(false); }
  };
  return (
    <div className="flex min-w-0 flex-1 items-start gap-2">
      <div className="relative min-w-0 flex-1">
        <Underlay text={v} />
        <textarea ref={ref} rows={1} aria-label="Variable value" value={v} spellCheck={false} autoComplete="off"
          onChange={(e) => setV(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Escape") { e.preventDefault(); e.stopPropagation(); onCancel(); }
            if (e.key === "Enter" && !e.shiftKey) { e.preventDefault(); void save(); }
          }}
          className={cx(FIELD, "relative min-h-[34px] font-mono !border-pink-400 focus:!border-pink-400")} />
        <BracesButton onClick={() => setEditor(true)} className="absolute right-[4px] top-[4px]" />
      </div>
      <IconButton label="Cancel" size={26} className="mt-1" onClick={onCancel}><X size={16} /></IconButton>
      <IconButton label="Save" size={26} className="mt-1 !text-foreground" disabled={busy} onClick={() => void save()}><Check size={16} /></IconButton>
      {editor && <MultilineModal value={v} onSave={setV} onClose={() => setEditor(false)} />}
    </div>
  );
}

export function VariableRow({ v, api, editing, onEdit, onEditEnd }: {
  v: Variable; api: RowApi; editing: boolean; onEdit: () => void; onEditEnd: () => void;
}) {
  const [shown, setShown] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);
  const [editText, setEditText] = useState("");
  const timer = useRef<number>();
  useEffect(() => () => window.clearTimeout(timer.current), []);
  const generated = isGenerated(v.reference);
  const deleted = v.staged === "delete";

  const toggle = async () => {
    if (shown !== null) { setShown(null); return; }
    try { setShown(await api.resolved(v.name)); } catch { /* error surfaced by caller */ }
  };
  const copy = async () => {
    try {
      const text = shown ?? (await api.resolved(v.name));
      await navigator.clipboard?.writeText(text);
      setCopied(true);
      window.clearTimeout(timer.current);
      timer.current = window.setTimeout(() => setCopied(false), 1200);
    } catch { /* clipboard blocked */ }
  };
  const startEdit = async () => {
    try { setEditText(v.sealed ? "" : await api.raw(v)); onEdit(); } catch { /* ignore */ }
  };

  const lead = v.sealed ? (
    <span title="Sealed" className="flex h-4 w-4 shrink-0 cursor-default items-center justify-center text-gray-400"><Lock size={14} /></span>
  ) : generated ? (
    <Tooltip content={<>This variable is generated using a template variable function.</>} side="right">
      <span className="flex h-4 w-4 shrink-0 cursor-default items-center justify-center text-gray-400"><SquareFunction size={16} /></span>
    </Tooltip>
  ) : <Braces size={16} className="shrink-0 text-gray-400" />;

  const actionVis = "lg:opacity-0 lg:group-hover:opacity-100 lg:group-focus-within:opacity-100";

  return (
    <div className="pb-1">
      <div className={cx("variable-row group grid min-h-[36px] grid-cols-[minmax(0,2fr)_minmax(0,3fr)_minmax(4rem,auto)] items-center gap-2 rounded px-4 font-mono text-sm leading-5 transition-all hover:bg-gray-100", editing && "bg-gray-100")}>
        <div className="flex min-w-0 items-center space-x-4">
          {lead}
          <span title={v.name} className={cx("truncate", deleted ? "text-fg-3 line-through" : v.staged ? "text-pink-700" : "text-foreground")}>{v.name}</span>
        </div>
        {editing ? (
          <InlineEdit initial={editText} onSave={async (t) => { await api.save(v.name, t); onEditEnd(); }} onCancel={onEditEnd} />
        ) : (
          <div className="flex min-w-[60px] items-center">
            {v.needsValue ? (
              <span className="rounded border border-warn-line bg-warn-bg px-2 py-0.5 font-sans text-xs text-warn">Set a value</span>
            ) : shown !== null ? (
              <span className="max-h-[15vh] min-w-0 overflow-y-auto whitespace-pre-wrap break-all my-1 rounded border border-gray-200 px-2 py-1 text-foreground scroll-thin">{shown}</span>
            ) : <span className="text-fg-2">{MASK}</span>}
            {!v.sealed && !v.needsValue && (
              <span className="ml-2 flex shrink-0 items-center space-x-0.5">
                <button type="button" title={shown !== null ? "Hide value" : "Show value"} aria-label={shown !== null ? "Mask Variable" : "Reveal Variable"} onClick={() => void toggle()}
                  className={cx("flex h-[26px] w-[26px] items-center justify-center rounded-md border border-transparent text-fg-2 transition-all hover:border-gray-100 hover:bg-gray-100 hover:text-foreground focus:opacity-100", actionVis)}>
                  {shown !== null ? <EyeOff size={14} /> : <Eye size={14} />}
                </button>
                <button type="button" title="Copy value" aria-label="Copy value" onClick={() => void copy()}
                  className={cx("flex h-[26px] w-[26px] items-center justify-center rounded-md border border-transparent text-fg-2 transition-all hover:border-gray-100 hover:bg-gray-100 hover:text-foreground focus:opacity-100", actionVis)}>
                  {copied ? <Check size={14} className="text-ok-text" /> : <Copy size={14} />}
                </button>
              </span>
            )}
          </div>
        )}
        <div className="flex justify-end">
          <Menu minWidth={220} trigger={
            <button type="button" aria-haspopup="menu" aria-label="Variable actions" title="Variable actions"
              className="flex h-[26px] w-[22px] items-center justify-end text-fg-2 transition-colors hover:text-gray-700">
              <EllipsisVertical size={16} />
            </button>}
            items={[
              { label: v.sealed ? "Replace value" : "Edit", icon: <SquarePen size={16} />, onClick: () => void startEdit() },
              ...(v.sealed ? [] : [{ label: "Seal", icon: <EyeOff size={16} />, onClick: () => api.seal(v) }]),
              { label: "Promote", icon: <Globe size={16} />, onClick: () => api.promote(v) },
              { label: "Generator", icon: <SquareFunction size={16} />, onClick: () => api.generator(v) },
              { label: "Delete", icon: <Trash2 size={16} />, danger: true, onClick: () => api.remove(v) },
            ]} />
        </div>
      </div>
      {v.unresolved && v.unresolved.length > 0 && (
        <div className="flex items-center gap-1.5 px-4 pb-1 pl-[48px] font-sans text-caption text-warn">
          <AlertTriangle size={12} />Does not resolve: {v.unresolved.join(", ")}
        </div>
      )}
    </div>
  );
}
