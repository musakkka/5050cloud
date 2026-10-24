// SPDX-License-Identifier: AGPL-3.0-only

import { useEffect, useRef, useState } from "react";
import { Braces, Check, ChevronDown, ChevronRight, Eye, EyeOff, Trash2 } from "lucide-react";
import type * as T from "../../api/types.gen";
import { Button, IconButton, Input, Popover, MenuItem, MenuLabel, Textarea, cx, plural, toast } from "../../kit";
import { v1 } from "../settings/client";
import { InlineError, LoadError, SectionHead, useAsync, useLoad } from "./ui";

const NAME_RE = /^[A-Za-z_][A-Za-z0-9_]{0,255}$/;

export default function SharedVariables({ project, current }: { project: T.Project; current?: T.Environment }) {
  return (
    <section>
      <SectionHead wide title="Shared Variables" description="Variables every service in an environment can reference as ${{shared.NAME}}. Edits are staged; review and deploy them from the canvas." />
      <div className="space-y-3">
        {project.environments.map((e) => <EnvCard key={e.id} env={e} startOpen={e.id === current?.id && project.environments.length === 1} />)}
      </div>
    </section>
  );
}

function EnvCard({ env, startOpen }: { env: T.Environment; startOpen: boolean }) {
  const [open, setOpen] = useState(startOpen);
  const [raw, setRaw] = useState(false);
  const { data, error, setData, reload } = useLoad(() => v1.sharedVariables(env.id), [env.id]);
  const count = data ? data.variables.filter((v) => v.staged !== "delete").length : null;
  return (
    <div className="overflow-hidden rounded-md border border-surface-hover transition-colors hover:border-line">
      <div className="flex h-[50px] items-center pr-2">
        <button type="button" aria-expanded={open} onClick={() => setOpen((o) => !o)} className="flex h-full min-w-0 flex-1 items-center gap-3 px-4 text-left">
          {open ? <ChevronDown size={16} className="text-fg-3" /> : <ChevronRight size={16} className="text-fg-3" />}
          <span className="truncate text-lead font-semibold text-foreground">{env.name}</span>
        </button>
        <span className="mr-2 text-body text-fg-2">{count === null ? "" : plural(count, "variable", "variables")}</span>
        <IconButton label={`Raw editor for ${env.name}`} size={34} active={raw} onClick={() => { setOpen(true); setRaw((r) => !r); }}><Braces size={16} /></IconButton>
      </div>
      <div className={cx("grid transition-[grid-template-rows] duration-200 ease-out", open ? "grid-rows-[1fr]" : "grid-rows-[0fr]")}>
        <div className="overflow-hidden">
          {open && (error ? <div className="p-4"><LoadError error={error} onRetry={reload} /></div> : !data ? <div className="p-4 text-body text-fg-3">Loading...</div>
            : <EnvBody env={env} data={data} setData={setData} raw={raw} closeRaw={() => setRaw(false)} />)}
        </div>
      </div>
    </div>
  );
}

function EnvBody({ env, data, setData, raw, closeRaw }: { env: T.Environment; data: T.VariablesResponse; setData: (d: T.VariablesResponse) => void; raw: boolean; closeRaw: () => void }) {
  const [name, setName] = useState("");
  const [value, setValue] = useState("");
  const [shown, setShown] = useState<Record<string, string>>({});
  const nameRef = useRef<HTMLInputElement>(null);
  const a = useAsync();
  useEffect(() => { if (!raw) nameRef.current?.focus(); }, [raw]);
  const nameOk = NAME_RE.test(name);
  const add = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!nameOk) return;
    const r = await a.run(() => v1.setSharedVariables(env.id, { set: { [name]: value } }));
    if (r) { setData(r); setName(""); setValue(""); nameRef.current?.focus(); toast("Variable staged", "success"); }
  };
  const remove = async (n: string) => { const r = await a.run(() => v1.setSharedVariables(env.id, { unset: [n] })); if (r) setData(r); };
  const reveal = async (n: string) => {
    if (n in shown) { const { [n]: _x, ...rest } = shown; setShown(rest); return; }
    const r = await a.run(() => v1.revealSharedVariables(env.id, { names: [n] }));
    if (r) setShown({ ...shown, ...r.values });
  };
  if (raw) return <RawEditor env={env} data={data} setData={setData} onClose={closeRaw} />;
  const refs = [...data.variables.map((v) => `shared.${v.name}`), ...data.provided];
  const live = data.variables.filter((v) => v.staged !== "delete");
  return (
    <div className="border-t border-surface-hover p-3.5">
      <form onSubmit={add} className="flex flex-col gap-2 sm:flex-row" noValidate>
        <Input ref={nameRef} size="sm" mono value={name} onChange={(e) => setName(e.target.value.toUpperCase())} placeholder="VARIABLE_NAME" aria-label="Variable name" aria-invalid={(!!name && !nameOk) || undefined} className="sm:max-w-[321px]" autoComplete="off" />
        <div className="relative min-w-0 flex-1">
          <Input size="sm" mono value={value} onChange={(e) => setValue(e.target.value)} placeholder="VALUE or ${{REF}}" aria-label="Variable value" autoComplete="off" className="pr-10" />
          <div className="absolute right-1 top-1/2 -translate-y-1/2">
            <Popover align="right" minWidth={240} trigger={() => <button type="button" aria-label="Insert a reference" className="inline-flex h-[26px] w-6 items-center justify-center rounded-md bg-surface-hover text-xs text-fg-2 transition-colors hover:text-foreground"><Braces size={12} /></button>}>
              {(close) => (
                <div className="max-h-64 overflow-y-auto scroll-thin">
                  <MenuLabel>Insert reference</MenuLabel>
                  {refs.length === 0 && <div className="px-3 py-2 text-body text-fg-3">Nothing to reference yet.</div>}
                  {refs.map((r) => <MenuItem key={r} onClick={() => { setValue(value + "${{" + r + "}}"); close(); }}><span className="font-mono text-caption">{r}</span></MenuItem>)}
                </div>
              )}
            </Popover>
          </div>
        </div>
        <Button type="submit" variant="primary" icon={<Check size={16} />} busy={a.busy} disabled={!nameOk}>Add</Button>
      </form>
      {name && !nameOk && <InlineError>Use letters, digits and underscores, not starting with a digit.</InlineError>}
      <InlineError>{a.error}</InlineError>
      <div className="mt-3">
        {live.length === 0 ? (
          <div className="flex h-11 items-center justify-center rounded-md bg-surface-hover text-body text-fg-2">No shared variables yet</div>
        ) : (
          <div className="overflow-hidden rounded-md border border-surface-hover">
            {data.variables.map((v, i) => (
              <div key={v.name} className={cx("flex min-h-11 items-center gap-3 px-3 text-body transition-colors hover:bg-tint-hover", i > 0 && "border-t border-surface-hover", v.staged === "delete" && "opacity-50")}>
                <span className="w-2/5 min-w-0 truncate font-mono text-caption text-foreground">{v.name}</span>
                <span className="min-w-0 flex-1 truncate font-mono text-caption text-fg-2">{v.reference || (v.sealed ? "Sealed" : shown[v.name] ?? "••••••••••")}</span>
                {v.staged && <span className="text-caption text-warn">staged {v.staged}</span>}
                {!v.sealed && !v.reference && v.staged !== "delete" && <IconButton label={v.name in shown ? "Hide value" : "Show value"} size={26} onClick={() => reveal(v.name)}>{v.name in shown ? <EyeOff size={14} /> : <Eye size={14} />}</IconButton>}
                {v.staged !== "delete" && <IconButton label={`Remove ${v.name}`} size={26} onClick={() => remove(v.name)}><Trash2 size={14} /></IconButton>}
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}

function RawEditor({ env, data, setData, onClose }: { env: T.Environment; data: T.VariablesResponse; setData: (d: T.VariablesResponse) => void; onClose: () => void }) {
  const [text, setText] = useState<string | null>(null);
  const a = useAsync();
  useEffect(() => {
    let live = true;
    const names = data.variables.filter((v) => !v.sealed && !v.reference && v.staged !== "delete").map((v) => v.name);
    (names.length ? v1.revealSharedVariables(env.id, { names }) : Promise.resolve({ values: {} as Record<string, string> }))
      .then((r) => live && setText(data.variables.filter((v) => v.staged !== "delete").map((v) => `${v.name}=${v.reference ?? (v.sealed ? "" : r.values[v.name] ?? "")}`).join("\n")))
      .catch(() => live && setText(""));
    return () => { live = false; };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [env.id]);
  const save = async () => { const r = await a.run(() => v1.setSharedVariables(env.id, { raw: text ?? "" })); if (r) { setData(r); toast("Variables staged", "success"); onClose(); } };
  return (
    <div className="border-t border-surface-hover p-3.5">
      <p className="mb-2 text-caption text-fg-3">One KEY=value per line, or paste JSON. Saving replaces the whole set.</p>
      <Textarea value={text ?? ""} onChange={(e) => setText(e.target.value)} disabled={text === null} rows={9} spellCheck={false} className="w-full" aria-label="Raw variables" placeholder="KEY=value" />
      <InlineError>{a.error}</InlineError>
      <div className="mt-3 flex justify-end gap-2"><Button onClick={onClose}>Cancel</Button><Button variant="primary" busy={a.busy} disabled={text === null} onClick={save}>Update variables</Button></div>
    </div>
  );
}

