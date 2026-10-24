// SPDX-License-Identifier: AGPL-3.0-only

// Layout settings: view options, tag columns and structured-log data columns.

import { useState } from "react";
import { Settings } from "lucide-react";
import { Checkbox, IconButton, Popover, cx } from "../../kit";
import type { ViewOpts } from "./prefs";

const VIEW: [keyof ViewOpts, string][] = [
  ["histogram", "Show histogram"], ["severity", "Show severity level"], ["data", "Show data column"],
  ["attrs", "Show attributes in data column"], ["json", "Display data column as JSON"], ["wrap", "Wrap long lines"],
];
const TAGS: [keyof ViewOpts, string][] = [["service", "Service"], ["deploy", "Deploy"], ["replica", "Replica"]];

export default function SettingsPopover({ opts, onChange, hideTags }: { opts: ViewOpts; onChange: (o: ViewOpts) => void; hideTags?: (keyof ViewOpts)[] }) {
  const [draft, setDraft] = useState("");
  const set = (k: keyof ViewOpts, v: boolean) => onChange({ ...opts, [k]: v });
  const add = () => {
    const v = draft.trim().replace(/,$/, "");
    if (v && !opts.dataCols.includes(v)) onChange({ ...opts, dataCols: [...opts.dataCols, v] });
    setDraft("");
  };
  return (
    <Popover align="right" width={320} offset={6} role="dialog" className="!p-0"
      trigger={() => <IconButton label="Layout settings" size={26} className="text-fg-3"><Settings size={16} /></IconButton>}>
      {() => (
        <div className="flex flex-col gap-5 p-4">
          <section>
            <h3 className="mb-2 text-caption font-medium text-fg-2">View options</h3>
            <div className="flex flex-col gap-1.5">
              {VIEW.map(([k, label]) => <Checkbox key={k} checked={opts[k] as boolean} onChange={(v) => set(k, v)} label={<span className="text-caption">{label}</span>} />)}
            </div>
          </section>
          <section>
            <h3 className="mb-2 text-caption font-medium text-fg-2">Tag columns</h3>
            <div className="grid grid-cols-2 gap-1.5">
              {TAGS.filter(([k]) => !hideTags?.includes(k)).map(([k, label]) => <Checkbox key={k} checked={opts[k] as boolean} onChange={(v) => set(k, v)} label={<span className="text-caption">{label}</span>} />)}
            </div>
          </section>
          <section>
            <h3 className="mb-1 text-caption font-medium text-fg-2">Data columns</h3>
            <p className="mb-2 text-caption text-fg-2">Include properties from JSON structured logs as columns e.g. <code className="font-mono">nested.message</code></p>
            <div className={cx("flex min-h-[35px] flex-wrap items-center gap-1 rounded-md border border-line px-1 py-1 transition-colors duration-150 focus-within:border-fg-4")}>
              {opts.dataCols.map((c) => (
                <span key={c} className="inline-flex items-center gap-1 rounded bg-surface-hover px-1.5 py-0.5 font-mono text-[11px] text-foreground">
                  {c}
                  <button type="button" aria-label={`Remove ${c}`} onClick={() => onChange({ ...opts, dataCols: opts.dataCols.filter((x) => x !== c) })} className="text-fg-3 hover:text-foreground">&times;</button>
                </span>
              ))}
              <input value={draft} onChange={(e) => setDraft(e.target.value)} aria-label="Add data column" spellCheck={false}
                onKeyDown={(e) => {
                  if (e.key === "Enter" || e.key === ",") { e.preventDefault(); add(); }
                  else if (e.key === "Backspace" && !draft && opts.dataCols.length) onChange({ ...opts, dataCols: opts.dataCols.slice(0, -1) });
                }}
                onBlur={add}
                className="min-w-[60px] flex-1 bg-transparent px-1 font-mono text-[11px] text-foreground outline-none" />
            </div>
          </section>
        </div>
      )}
    </Popover>
  );
}
