// SPDX-License-Identifier: AGPL-3.0-only

// Raw Editor: ENV and JSON tabs over a highlighted editor (a transparent
// textarea above a coloured mirror), copy button, update button.

import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { Check, Copy } from "lucide-react";
import type { RevealResponse, Variable, VariablesResponse } from "../../api/types.gen";
import { Button, Modal, cx } from "../../kit";
import { post, put } from "../service/client";
import { parseRaw } from "./raw";
import { HerokuImport } from "./HerokuImport";

type Mode = "env" | "json";

const dq = (v: string) => `"${v.replace(/\\/g, "\\\\").replace(/"/g, '\\"').replace(/\n/g, "\\n")}"`;
const toEnvText = (vars: Record<string, string>) => Object.entries(vars).map(([k, v]) => `${k}=${dq(v)}`).join("\n");
const toJsonText = (vars: Record<string, string>) => (Object.keys(vars).length ? JSON.stringify(vars, null, 2) : "{}");

const KEY = "text-[#8f9bff]";
const STR = "text-[#a5e075]";
const NUM = "text-[#e6b450]";
const DIM = "text-fg-4";

function highlightEnv(text: string): ReactNode[] {
  return text.split("\n").map((line, i) => {
    let body: ReactNode = line;
    const eq = line.indexOf("=");
    if (line.trimStart().startsWith("#")) body = <span className={DIM}>{line}</span>;
    else if (eq > 0) {
      const val = line.slice(eq + 1);
      const quoted = /^\s*["'`]/.test(val);
      body = <><span className={KEY}>{line.slice(0, eq)}</span><span className="text-foreground">=</span><span className={quoted ? STR : "text-foreground"}>{val}</span></>;
    }
    return <div key={i} className="min-h-[19.6px]">{body}</div>;
  });
}

function highlightJson(text: string): ReactNode[] {
  return text.split("\n").map((line, i) => {
    const m = /^(\s*)("(?:[^"\\]|\\.)*")(\s*:\s*)(.*)$/.exec(line);
    let body: ReactNode = <span className="text-foreground">{line}</span>;
    if (m) {
      const val = m[4];
      const cls = val.startsWith('"') ? STR : /^-?\d|true|false|null/.test(val) ? NUM : "text-foreground";
      body = <>{m[1]}<span className={KEY}>{m[2]}</span><span className="text-foreground">{m[3]}</span><span className={cls}>{val}</span></>;
    }
    return <div key={i} className="min-h-[19.6px]">{body}</div>;
  });
}

export function RawEditor({ base, list, onClose, onApplied }: {
  base: string; list: Variable[]; onClose: () => void; onApplied: (r: VariablesResponse) => void;
}) {
  const [vars, setVars] = useState<Record<string, string> | null>(null);
  const [mode, setMode] = useState<Mode>("env");
  const [text, setText] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [copied, setCopied] = useState(false);
  const [heroku, setHeroku] = useState(false);
  const mirror = useRef<HTMLPreElement>(null);

  useEffect(() => {
    let off = false;
    post<RevealResponse>(`${base}/variables/reveal`, {})
      .then((r) => {
        const v: Record<string, string> = {};
        for (const x of list) if (!x.sealed && r.values[x.name] !== undefined) v[x.name] = r.values[x.name];
        if (!off) { setVars(v); setText(toEnvText(v)); }
      })
      .catch(() => { if (!off) { setVars({}); setText(""); } });
    return () => { off = true; };
  }, [base, list]);

  const parsed = useMemo(() => parseRaw(text), [text]);
  const problem = parsed.errors[0] ?? "";

  const switchTo = (m: Mode) => {
    if (m === mode) return;
    if (parsed.errors.length) { setError(problem); return; }
    setError("");
    setText(m === "json" ? toJsonText(parsed.vars) : toEnvText(parsed.vars));
    setMode(m);
  };

  const copy = async () => {
    try { await navigator.clipboard?.writeText(text); setCopied(true); setTimeout(() => setCopied(false), 1200); } catch { /* blocked */ }
  };
  const update = async () => {
    if (parsed.errors.length) { setError(problem); return; }
    setBusy(true); setError("");
    try { onApplied(await put<VariablesResponse>(`${base}/variables`, { raw: text })); onClose(); }
    catch (e) { setError(e instanceof Error ? e.message : String(e)); setBusy(false); }
  };

  const placeholder = mode === "env" ? "HELLO=world\\nFOO=bar" : '{\n  "HELLO": "world"\n}';
  const lineCls = "whitespace-pre-wrap break-words px-3 py-1 font-mono text-sm leading-[19.6px]";

  return (
    <Modal title="Raw Editor" bare width={640} onClose={onClose} className="!rounded-xl">
      <div className="p-8">
        <h2 className="pr-8 text-[20px] font-semibold leading-[30px] text-foreground">Raw Editor</h2>
        <p className="mt-1 text-base text-fg-2">Add, edit, or delete your project variables</p>
        <div role="tablist" className="mt-4 grid grid-cols-2 rounded-t-md border border-b-0 border-gray-200">
          {(["env", "json"] as Mode[]).map((m) => (
            <button key={m} role="tab" aria-selected={mode === m} type="button" onClick={() => switchTo(m)}
              className={cx("relative h-10 text-sm transition-colors", mode === m ? "font-semibold text-foreground" : "text-fg-3 hover:text-fg-2")}>
              {m.toUpperCase()}
              {mode === m && <span className="absolute inset-x-0 bottom-0 h-0.5 bg-accent" />}
            </button>
          ))}
        </div>
        <div className="relative h-[342px] overflow-hidden rounded-b-md border border-gray-200 bg-surface-raised">
          {vars === null ? (
            <div className="space-y-2 p-3">{[70, 45, 60].map((w) => <div key={w} className="h-4 animate-skeleton rounded bg-skeleton" style={{ width: `${w}%` }} />)}</div>
          ) : (
            <>
              <pre ref={mirror} aria-hidden className={cx(lineCls, "pointer-events-none absolute inset-0 m-0 overflow-hidden")}>
                {text ? (mode === "env" ? highlightEnv(text) : highlightJson(text)) : <span className="text-fg-4">{placeholder}</span>}
              </pre>
              <textarea aria-label="Raw variables" value={text} spellCheck={false} autoFocus wrap="soft"
                onChange={(e) => { setText(e.target.value); setError(""); }}
                onScroll={(e) => { if (mirror.current) mirror.current.scrollTop = e.currentTarget.scrollTop; }}
                className={cx(lineCls, "scroll-thin absolute inset-0 h-full w-full resize-none overflow-y-auto border-0 bg-transparent text-transparent caret-white outline-none focus-visible:shadow-none")} />
            </>
          )}
        </div>
        <div className="mt-2 min-h-[18px] text-caption text-danger-text" role="alert">{error || (text && problem)}</div>
        <div className="mt-4 flex items-center justify-between gap-2">
          <div className="flex items-center gap-1">
            <Button variant="accent-ghost" icon={copied ? <Check size={16} /> : <Copy size={16} />} onClick={() => void copy()}>Copy {mode.toUpperCase()}</Button>
            <Button variant="ghost" onClick={() => setHeroku(true)}>Import from Heroku</Button>
          </div>
          <div className="flex items-center gap-2">
            <Button onClick={onClose}>Cancel</Button>
            <Button variant="primary" busy={busy} disabled={vars === null} onClick={() => void update()}>Update Variables</Button>
          </div>
        </div>
      </div>
      {heroku && <HerokuImport base={base} onClose={() => setHeroku(false)} onDone={onClose} />}
    </Modal>
  );
}
