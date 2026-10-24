// SPDX-License-Identifier: AGPL-3.0-only

// The filter field: highlighted mirror layer over a transparent input, a `/` hint, clear button,
// parse-error affordances and the suggestion popover (tags, values, operators, docs link).

import { useEffect, useMemo, useRef, useState } from "react";
import { AlertTriangle, Search, X } from "lucide-react";
import { BrandIcon, brandKey, cx } from "../../kit";
import { LEVELS, LEVEL_BAR, parseFilter, quoteValues, tokenize } from "./query";

export interface TagDef { tag: string; hint: string }
export const ALL_TAGS: TagDef[] = [
  { tag: "service", hint: "Filter by service" },
  { tag: "level", hint: "Filter by severity" },
  { tag: "deployment", hint: "Filter by deploy" },
  { tag: "replica", hint: "Filter by replica" },
];

export interface ValueOption { value: string; label?: string; icon?: string }

interface Props {
  value: string;
  onApply: (q: string) => void;
  placeholder?: string;
  tags?: TagDef[];
  /** Values for a tag: services, replicas, deployments, attribute values. */
  valuesFor: (tag: string) => ValueOption[];
  docsHref?: string;
  className?: string;
}

const CHUNK = /[^\s()]*$/;

export default function FilterBar({ value, onApply, placeholder = "Filter and search logs", tags = ALL_TAGS, valuesFor, docsHref = "https://docs.5050.cloud/observability/logs#filters", className }: Props) {
  const [text, setText] = useState(value);
  const [focus, setFocus] = useState(false);
  const [caret, setCaret] = useState(0);
  const [hi, setHi] = useState(-1);
  const input = useRef<HTMLInputElement>(null);
  const mirror = useRef<HTMLDivElement>(null);
  const box = useRef<HTMLDivElement>(null);

  useEffect(() => setText(value), [value]);

  useEffect(() => {
    const k = (e: KeyboardEvent) => {
      if (e.key !== "/" || e.metaKey || e.ctrlKey || e.altKey) return;
      const t = e.target as HTMLElement | null;
      if (t && (t.tagName === "INPUT" || t.tagName === "TEXTAREA" || t.isContentEditable)) return;
      e.preventDefault();
      input.current?.focus();
    };
    window.addEventListener("keydown", k);
    return () => window.removeEventListener("keydown", k);
  }, []);

  const parsed = useMemo(() => parseFilter(text), [text]);
  const tokens = useMemo(() => tokenize(text), [text]);

  // What the caret is on: `@tag` (list tags), `@tag:partial` (list values), or anything else.
  const before = text.slice(0, caret);
  const chunk = CHUNK.exec(before)?.[0] ?? "";
  const mTag = /^-?@([A-Za-z0-9_.-]*)$/.exec(chunk);
  const mVal = /^-?@([A-Za-z0-9_.-]+):"?([^"]*)$/.exec(chunk);

  type Item = { key: string; label: string; hint?: string; insert: string; icon?: string; level?: string };
  const { items, group } = useMemo((): { items: Item[]; group: string } => {
    if (mVal) {
      const tag = mVal[1];
      const part = mVal[2].toLowerCase();
      const opts: ValueOption[] = tag === "level" ? LEVELS.map((l) => ({ value: l })) : valuesFor(tag);
      const list = opts.filter((o) => (o.label ?? o.value).toLowerCase().includes(part)).slice(0, 50);
      return {
        group: tag === "level" ? "Log Severity" : tag === "service" ? "Services" : `Values for @${tag}`,
        items: list.map((o) => ({ key: o.value, label: o.label ?? o.value, insert: `@${tag}:"${o.value}" `, icon: o.icon, level: tag === "level" ? o.value : undefined })),
      };
    }
    const part = (mTag?.[1] ?? "").toLowerCase();
    return {
      group: "Tags",
      items: tags.filter((t) => t.tag.startsWith(part) || !mTag).map((t) => ({ key: t.tag, label: `@${t.tag}`, hint: t.hint, insert: `@${t.tag}:` })),
    };
  }, [mVal, mTag, tags, valuesFor]);

  const open = focus && (items.length > 0 || !mVal);
  useEffect(() => setHi(-1), [chunk]);

  const sync = () => { if (mirror.current && input.current) mirror.current.scrollLeft = input.current.scrollLeft; };
  const track = () => { setCaret(input.current?.selectionStart ?? 0); sync(); };

  const replaceChunk = (ins: string) => {
    const neg = chunk.startsWith("-") ? "-" : "";
    const start = caret - chunk.length;
    const next = text.slice(0, start) + neg + ins + text.slice(caret);
    setText(next);
    const pos = start + neg.length + ins.length;
    requestAnimationFrame(() => { input.current?.focus(); input.current?.setSelectionRange(pos, pos); setCaret(pos); sync(); });
  };
  const insertToken = (tok: string) => {
    const sep = text && !/\s$/.test(text) ? " " : "";
    const next = `${text}${sep}${tok}`;
    setText(next);
    requestAnimationFrame(() => { input.current?.focus(); input.current?.setSelectionRange(next.length, next.length); setCaret(next.length); });
  };

  const apply = () => {
    const q = quoteValues(text.trim());
    setText(q);
    onApply(q);
  };
  const clear = () => { setText(""); onApply(""); input.current?.focus(); };

  const onKey = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === "ArrowDown" || e.key === "ArrowUp") {
      if (!open || !items.length) return;
      e.preventDefault();
      setHi((h) => (e.key === "ArrowDown" ? (h + 1) % items.length : (h <= 0 ? items.length : h) - 1));
    } else if (e.key === "Enter") {
      e.preventDefault();
      if (open && hi >= 0 && items[hi]) replaceChunk(items[hi].insert);
      else { apply(); setFocus(false); input.current?.blur(); }
    } else if (e.key === "Tab" && open && items.length && (mTag || mVal)) {
      e.preventDefault();
      replaceChunk(items[Math.max(0, hi)].insert);
    } else if (e.key === "Escape") {
      e.stopPropagation();
      if (open) setFocus(false);
      else input.current?.blur();
      if (!open) setText(value);
    }
  };

  // While typing the token under the caret, stay quiet.
  const typing = focus && !!parsed.errorToken && parsed.errorToken.end === caret && parsed.errorToken.kind !== "op" && !/\s$/.test(before);
  const showErr = !!parsed.error && !typing;
  const bad = showErr ? parsed.errorToken : undefined;

  return (
    <div ref={box} className={cx("relative min-w-0", className)}>
      <div className={cx("relative flex h-[34px] items-center rounded-md border bg-transparent transition-colors duration-150", focus ? "border-fg-4" : "border-line hover:border-fg-5")}>
        <Search size={16} className={cx("pointer-events-none absolute left-[11px] transition-colors", focus ? "text-accent-muted" : "text-fg-3")} />
        <div ref={mirror} aria-hidden="true" className="pointer-events-none absolute inset-y-0 left-[38px] right-[62px] overflow-hidden whitespace-pre font-mono text-sm leading-[32px]">
          {tokens.map((t, i) => {
            const isBad = bad && t.start === bad.start;
            const c = t.kind === "op" || t.kind === "neg" ? "text-accent-muted" : t.kind === "paren" ? "text-fg-2" : "text-foreground";
            if (t.kind === "tag") {
              const k = `@${t.key}`;
              return <span key={i} className={isBad ? "log-wavy" : undefined}><span className="text-fg-2">{k}{t.value !== undefined ? ":" : ""}</span>{t.value !== undefined && <span className="font-semibold text-foreground">{t.raw.slice(k.length + 1)}</span>}</span>;
            }
            return <span key={i} className={cx(c, (isBad || (t.kind === "bad" && showErr)) && "log-wavy")}>{t.raw}</span>;
          })}
        </div>
        <input ref={input} value={text} aria-label={placeholder} placeholder={placeholder} spellCheck={false} autoComplete="off" role="combobox" aria-expanded={open} aria-autocomplete="list"
          onChange={(e) => { setText(e.target.value); setCaret(e.target.selectionStart ?? 0); }}
          onFocus={() => { setFocus(true); track(); }} onBlur={() => window.setTimeout(() => setFocus(false), 120)}
          onKeyDown={onKey} onKeyUp={track} onClick={track} onSelect={track} onScroll={sync}
          className="h-full w-full bg-transparent pl-[38px] pr-[62px] font-mono text-sm text-transparent caret-white outline-none placeholder:font-sans placeholder:text-fg-2 focus-visible:shadow-none" />
        <div className="absolute right-1 flex items-center gap-1">
          {showErr && text && <span title={parsed.error} className="flex h-[26px] w-[22px] items-center justify-center text-warn-icon"><AlertTriangle size={16} aria-label={parsed.error} /></span>}
          {text ? (
            <button type="button" aria-label="Clear filter" onMouseDown={(e) => e.preventDefault()} onClick={clear}
              className="flex h-[26px] w-[26px] items-center justify-center rounded-md text-foreground transition-colors hover:bg-surface-hover"><X size={16} /></button>
          ) : (
            <span aria-hidden="true" className="flex h-6 w-6 items-center justify-center rounded-md bg-surface-raised text-sm font-black text-fg-2">/</span>
          )}
        </div>
      </div>

      {open && (
        <div role="listbox" data-popover onMouseDown={(e) => e.preventDefault()}
          className="absolute left-0 right-0 top-full z-[60] mt-0.5 overflow-hidden rounded-lg border border-line bg-background animate-pop-in">
          <div className="flex h-[42px] items-center px-4 text-caption font-medium text-fg-2" style={{ background: "rgb(28 26 40)" }}>{group}</div>
          <div className="max-h-[280px] overflow-y-auto scroll-thin">
            {items.length === 0 && <div className="px-4 py-3 text-caption text-fg-3">Nothing to suggest.</div>}
            {items.map((it, i) => (
              <button key={it.key} type="button" role="option" aria-selected={i === hi} onMouseEnter={() => setHi(i)} onClick={() => replaceChunk(it.insert)}
                className={cx("flex h-9 w-full items-center gap-3 px-4 text-left font-mono text-sm text-foreground", i === hi && "bg-gray-50")}>
                {it.level && <span className={cx("h-4 w-[3px] shrink-0 rounded-sm", LEVEL_BAR[it.level as keyof typeof LEVEL_BAR])} />}
                {mVal?.[1] === "service" && <BrandIcon name={it.icon || (brandKey(it.label) ? it.label : "")} size={16} fallback="terminal" />}
                <span className="min-w-0 flex-1 truncate">{it.label}</span>
                {it.hint && <span className="shrink-0 font-sans text-caption font-medium text-fg-2">{it.hint}</span>}
              </button>
            ))}
          </div>
          <div className="flex min-h-[47px] flex-wrap items-center gap-x-2 gap-y-1 border-t border-line px-4 py-2 text-caption font-medium text-fg-2" style={{ background: "rgb(28 26 40)" }}>
            <span>Tags:</span>
            {tags.map((t) => <Chip key={t.tag} onClick={() => insertToken(`@${t.tag}:`)}>@{t.tag}</Chip>)}
            <span className="ml-3">Operators:</span>
            {["-", "AND", "OR"].map((o) => <Chip key={o} onClick={() => insertToken(o === "-" ? "-" : o + " ")}>{o}</Chip>)}
            <a href={docsHref} target="_blank" rel="noreferrer" className="ml-auto underline underline-offset-2 transition-colors hover:text-foreground">Documentation</a>
          </div>
        </div>
      )}
    </div>
  );
}

function Chip({ children, onClick }: { children: React.ReactNode; onClick: () => void }) {
  return (
    <button type="button" onClick={onClick}
      className="rounded-md border border-line bg-background px-1.5 py-px font-mono text-caption font-medium text-accent-muted transition-colors hover:border-fg-5 hover:text-accent-text">{children}</button>
  );
}
