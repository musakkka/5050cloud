// SPDX-License-Identifier: AGPL-3.0-only

// The New Variable form: name and value fields in one row, an in-field
// "Add Reference" control, the reference menu and the `${{` autocomplete.

import { useEffect, useMemo, useRef, useState } from "react";
import { Braces, Check, ChevronDown } from "lucide-react";
import { Button, Modal, useClickOutside, cx } from "../../kit";
import { NAME_RE, refAt } from "./raw";
import { Hit, RefList, listKeys, type ListEntry } from "./parts";
import { rank, type RefItem } from "./refs";

export const FIELD =
  "block w-full resize-none rounded-md border border-gray-200 bg-transparent py-1.5 pl-1.5 pr-7 text-sm leading-5 text-foreground outline-none focus-visible:shadow-none transition-colors duration-150 placeholder:text-fg-4 hover:border-gray-400 focus:border-pink-400 scroll-thin";

/** A one-row textarea that grows with its content up to 30vh. */
export function useGrow(value: string) {
  const ref = useRef<HTMLTextAreaElement>(null);
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    el.style.height = "auto";
    el.style.height = `${Math.min(el.scrollHeight + 2, window.innerHeight * 0.3)}px`;
  }, [value]);
  return ref;
}

/** Small `{ }` button that opens the multi-line editor. */
export function BracesButton({ onClick, className }: { onClick: () => void; className?: string }) {
  return (
    <button type="button" title="Open" aria-label="Open editor" onClick={onClick}
      className={cx("flex h-[26px] w-[26px] items-center justify-center rounded-md text-fg-3 transition-colors hover:bg-gray-100 hover:text-foreground", className)}>
      <Braces size={12} />
    </button>
  );
}

export function MultilineModal({ value, onSave, onClose }: { value: string; onSave: (v: string) => void; onClose: () => void }) {
  const [v, setV] = useState(value);
  return (
    <Modal title="Edit value" width={560} onClose={onClose}
      footer={<><Button onClick={onClose}>Cancel</Button><Button variant="primary" onClick={() => { onSave(v); onClose(); }}>Save</Button></>}>
      <textarea aria-label="Value" autoFocus value={v} onChange={(e) => setV(e.target.value)} rows={10} spellCheck={false}
        className="scroll-thin w-full resize-y rounded-md border border-gray-200 bg-transparent p-2 font-mono text-sm text-foreground outline-none focus-visible:shadow-none focus:border-pink-400" />
    </Modal>
  );
}

export function NewVariableForm({ initialName = "", openRefs, items, existing, onSave, onCancel }: {
  initialName?: string; openRefs?: boolean; items: RefItem[]; existing: string[];
  onSave: (name: string, value: string) => Promise<void>; onCancel: () => void;
}) {
  const [name, setName] = useState(initialName);
  const [value, setValue] = useState("");
  const [menu, setMenu] = useState<"name" | "value" | null>(openRefs ? "name" : null);
  const [caret, setCaret] = useState(0);
  const [active, setActive] = useState(0);
  const [expanded, setExpanded] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [editor, setEditor] = useState(false);
  const wrap = useRef<HTMLFormElement>(null);
  const nameRef = useGrow(name);
  const valueRef = useGrow(value);
  useClickOutside(wrap, () => setMenu(null), menu !== null);

  const sources = useMemo(() => items.filter((i) => i.source.service), [items]);
  const nameEntries = useMemo(() => rank(sources, name.trim(), (i) => i.name), [sources, name]);
  const ref = menu === "value" ? refAt(value, caret) : null;
  const valueEntries = useMemo(
    () => (ref ? rank(items, "${{" + ref.partial, (i) => i.token) : []),
    [items, ref?.partial], // eslint-disable-line react-hooks/exhaustive-deps
  );
  const entries: ListEntry[] = menu === "name"
    ? nameEntries.map(({ item }) => ({ key: item.token, icon: item.source.icon, label: item.name, right: item.source.service }))
    : valueEntries.map(({ item, hits }) => ({
      key: item.token, icon: item.source.service ? item.source.icon : undefined,
      label: <Hit text={item.token} hits={hits} />,
    }));
  const visible = expanded ? entries.length : Math.min(entries.length, 13);

  useEffect(() => { setActive(0); setExpanded(false); }, [menu, name, ref?.partial]);

  const pick = (i: number) => {
    if (menu === "name") {
      const it = nameEntries[i]?.item;
      if (!it) return;
      setName(it.name);
      if (!value) setValue(it.token);
      setMenu(null);
      valueRef.current?.focus();
    } else if (ref) {
      const it = valueEntries[i]?.item;
      if (!it) return;
      const next = value.slice(0, ref.start) + it.token + value.slice(caret).replace(/^\}\}/, "");
      setValue(next);
      setMenu(null);
      const pos = ref.start + it.token.length;
      requestAnimationFrame(() => { valueRef.current?.focus(); valueRef.current?.setSelectionRange(pos, pos); });
    }
  };

  const submit = async () => {
    const n = name.trim();
    if (!n || busy) return;
    if (!NAME_RE.test(n)) { setError("Names use letters, digits and underscores, and cannot start with a digit."); return; }
    if (existing.includes(n)) { setError(`${n} already exists. Edit it from its row menu.`); return; }
    setBusy(true); setError("");
    try { await onSave(n, value); } catch (e) { setError(e instanceof Error ? e.message : String(e)); setBusy(false); }
  };

  const keys = (e: React.KeyboardEvent, field: "name" | "value") => {
    if (menu === (field === "name" ? "name" : "value") && (menu === "name" || ref)) {
      if (listKeys(e, visible, active, setActive, pick, () => setMenu(null))) return;
    }
    if (e.key === "Escape") { e.preventDefault(); e.stopPropagation(); onCancel(); return; }
    if (e.key === "Enter" && (field === "name" || !e.shiftKey)) { e.preventDefault(); void submit(); }
  };

  return (
    <form ref={wrap} name="Add Variable" className="relative mb-0" onSubmit={(e) => { e.preventDefault(); void submit(); }}>
      <div className="grid grid-cols-1 items-start gap-2 md:grid-cols-[1fr_1fr_auto]">
        <div className="relative">
          <textarea ref={nameRef} rows={1} autoFocus name="variable-name" aria-label="Variable name" placeholder="VARIABLE_NAME"
            autoComplete="off" spellCheck={false} value={name} className={cx(FIELD, "min-h-[34px]")}
            onChange={(e) => { setName(e.target.value.replace(/\n/g, "")); setError(""); }}
            onKeyDown={(e) => keys(e, "name")} />
          <button type="button" aria-label="Add Reference" aria-expanded={menu === "name"}
            onClick={() => setMenu((m) => (m === "name" ? null : "name"))}
            className="absolute right-[4px] top-[4px] flex h-[26px] items-center gap-1 overflow-hidden rounded-md bg-gray-100 px-1.5 text-xs text-fg-2 transition-colors hover:bg-gray-200 hover:text-foreground">
            <ChevronDown size={12} className="shrink-0" />
            <span className={cx("overflow-hidden whitespace-nowrap transition-all duration-150", name ? "w-0 opacity-0" : "w-[84px] opacity-100")}>Add Reference</span>
          </button>
          {menu === "name" && (
            <RefList className="absolute left-0 top-full mt-0.5 w-[443px] max-w-[calc(100vw-48px)]" header="Filter Database, Bucket or Shared Variables"
              entries={entries} active={active} onActive={setActive} onPick={pick} expanded={expanded} onExpand={() => setExpanded(true)} />
          )}
        </div>
        <div className="relative">
          <textarea ref={valueRef} rows={1} name="variable-value" aria-label="Variable value" placeholder={"VALUE or ${{REF}}"}
            autoComplete="off" spellCheck={false} value={value} className={cx(FIELD, "min-h-[34px]")}
            onChange={(e) => {
              const pos = e.target.selectionStart ?? e.target.value.length;
              setValue(e.target.value); setCaret(pos); setError("");
              setMenu(refAt(e.target.value, pos) ? "value" : null);
            }}
            onSelect={(e) => setCaret((e.target as HTMLTextAreaElement).selectionStart ?? 0)}
            onKeyDown={(e) => keys(e, "value")} />
          <BracesButton onClick={() => setEditor(true)} className="absolute right-[4px] top-[4px]" />
          {menu === "value" && ref && (
            <RefList className="absolute left-0 top-full mt-0.5 w-max min-w-full max-w-[480px]"
              entries={entries} active={active} onActive={setActive} onPick={pick} expanded={expanded} onExpand={() => setExpanded(true)} />
          )}
        </div>
        <div className="flex gap-2">
          <Button type="submit" variant="primary" disabled={!name.trim()} busy={busy} icon={<Check size={16} />}>Add</Button>
          <Button onClick={onCancel}>Cancel</Button>
        </div>
      </div>
      {error && <p role="alert" className="mt-2 text-caption text-danger-text">{error}</p>}
      {editor && <MultilineModal value={value} onSave={setValue} onClose={() => setEditor(false)} />}
    </form>
  );
}
