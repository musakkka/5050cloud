// SPDX-License-Identifier: AGPL-3.0-only

// Variables tab (docs/SPEC.md 13.3). Every edit is staged; the canvas banner
// picks the change up from the events stream.

import { useEffect, useMemo, useRef, useState } from "react";
import { AlertTriangle, ChevronDown, ChevronRight, Eye, EyeOff, Lock, Plus } from "lucide-react";
import type { Canvas, RevealResponse, Service, SetVariablesRequest, Variable, VariablesResponse } from "../../api/types.gen";
import { get, post, put, req, svcPath } from "../service/client";
import { Button, Confirm, CopyButton, Empty, ErrorLine, Loading, Menu, Modal, cx, useAction, useLoad } from "../service/ui";
import { NAME_RE, parseRaw, refAt, refSuggestions, toEnv } from "./raw";

const stagedColor = (v: Variable) =>
  v.staged === "delete" ? "line-through opacity-50" : v.staged ? "text-purple-500" : "";

export default function VariablesTab({ eid, service }: { eid: string; service: Service }) {
  const base = svcPath(eid, service.id);
  const vars = useLoad(() => get<VariablesResponse>(`${base}/variables`), [base]);
  const [revealed, setRevealed] = useState<Record<string, string>>({});
  const [adding, setAdding] = useState<{ name: string; value: string } | null>(null);
  const [raw, setRaw] = useState(false);
  const [heroku, setHeroku] = useState(false);
  const [shared, setShared] = useState(false);
  const [removing, setRemoving] = useState<string | null>(null);
  const [showProvided, setShowProvided] = useState(false);
  const act = useAction();
  const refs = useRefSources(eid, service.id);

  const stage = async (body: SetVariablesRequest) => {
    const r = await put<VariablesResponse>(`${base}/variables`, body);
    vars.setData(r);
  };
  const reveal = async (name: string) => {
    if (revealed[name] !== undefined) {
      setRevealed(({ [name]: _, ...rest }) => rest);
      return;
    }
    const r = await post<RevealResponse>(`${base}/variables/reveal`, { names: [name] });
    setRevealed((m) => ({ ...m, ...r.values }));
  };

  if (!vars.data) return vars.error ? <ErrorLine>{vars.error}</ErrorLine> : <Loading />;
  const list = vars.data.variables;
  const suggested = (vars.data.suggested || []).filter((s) => !list.some((v) => v.name === s));
  const dbServices = refs.services.filter((s) => s.isDb && s.id !== service.id);

  return (
    <div>
      <div className="mb-4 flex flex-wrap items-center justify-between gap-2">
        <div className="text-sm text-gray-600">{list.length} Service Variable{list.length === 1 ? "" : "s"}</div>
        <div className="flex flex-wrap gap-2">
          <Button onClick={() => setShared(true)}>Shared Variable</Button>
          <Button onClick={() => setRaw(true)}>Raw Editor</Button>
          <Button onClick={() => setHeroku(true)}>Import from Heroku</Button>
          <Button variant="primary" onClick={() => setAdding({ name: "", value: "" })}><Plus size={14} />New Variable</Button>
        </div>
      </div>

      {suggested.length > 0 && (
        <div className="mb-4 rounded-md border border-blue-300 bg-blue-100 px-4 py-3 text-sm text-blue-700">
          Suggested variables from your repo: {suggested.map((s) => (
            <button key={s} className="ml-2 underline" onClick={() => setAdding({ name: s, value: "" })}>{s}</button>
          ))}
        </div>
      )}

      {dbServices.length > 0 && (
        <div className="mb-4 flex flex-wrap gap-2">
          {dbServices.map((d) => d.urlVar && (
            <Button key={d.id} onClick={() => act.run(() => stage({ set: { [d.urlVar!]: `\${{${d.name}.${d.urlVar}}}` } }))}>
              Add a reference to {d.name}
            </Button>
          ))}
        </div>
      )}

      {adding && (
        <NewVariable initial={adding} refs={refs.suggest} onCancel={() => setAdding(null)}
          onSave={async (name, value) => { await stage({ set: { [name]: value } }); setAdding(null); }} />
      )}

      {list.length === 0 && !adding ? <Empty>No variables yet. Add one, or paste a <code>.env</code> file in the Raw Editor.</Empty> : (
        <div className="divide-y divide-gray-200 rounded-lg border border-gray-200">
          {list.map((v) => (
            <div key={v.name} className="flex items-start gap-3 px-4 py-2.5 text-sm">
              <div className={cx("w-1/3 min-w-0 break-all font-mono", stagedColor(v))}>
                {v.name}
                {v.sealed && <Lock size={12} className="ml-1 inline text-gray-500" aria-label="Sealed" />}
              </div>
              <div className="min-w-0 flex-1 break-all font-mono text-gray-700">
                {v.needsValue ? <span className="text-yellow-700">Set a value</span> :
                  revealed[v.name] !== undefined ? <span className="whitespace-pre-wrap">{revealed[v.name]}</span> :
                  v.reference ? <span className="text-purple-500">{v.reference}</span> : <span className="text-gray-500">•••••••</span>}
                {v.unresolved && v.unresolved.length > 0 && (
                  <div className="mt-1 flex items-center gap-1 font-sans text-xs text-yellow-700">
                    <AlertTriangle size={12} />Does not resolve: {v.unresolved.join(", ")}
                  </div>
                )}
              </div>
              {!v.sealed && (
                <button aria-label={revealed[v.name] !== undefined ? "Hide value" : "Reveal value"} className="text-gray-500 hover:text-foreground"
                  onClick={() => act.run(() => reveal(v.name))}>
                  {revealed[v.name] !== undefined ? <EyeOff size={15} /> : <Eye size={15} />}
                </button>
              )}
              <Menu label={`Actions for ${v.name}`} items={[
                { label: "Copy", hidden: v.sealed, onClick: () => void act.run(async () => {
                  const r = await post<RevealResponse>(`${base}/variables/reveal`, { names: [v.name] });
                  await navigator.clipboard?.writeText(r.values[v.name] ?? "");
                }) },
                { label: "Edit", onClick: () => void act.run(async () => {
                  let value = v.reference || "";
                  if (!v.sealed) {
                    const r = await post<RevealResponse>(`${base}/variables/reveal`, { names: [v.name] });
                    value = r.values[v.name] ?? value;
                  }
                  setAdding({ name: v.name, value: v.sealed ? "" : value });
                }) },
                { label: "Seal", hidden: v.sealed, onClick: () => void act.run(() => stage({ seal: [v.name] })) },
                { label: "Remove", danger: true, onClick: () => setRemoving(v.name) },
              ]} />
            </div>
          ))}
        </div>
      )}
      <ErrorLine>{act.error}</ErrorLine>

      {vars.data.provided.length > 0 && (
        <div className="mt-6">
          <button className="flex items-center gap-1 text-sm text-gray-600" onClick={() => setShowProvided((s) => !s)}>
            {showProvided ? <ChevronDown size={14} /> : <ChevronRight size={14} />}{vars.data.provided.length} provided variables
          </button>
          {showProvided && (
            <div className="mt-2 flex flex-wrap gap-2">
              {vars.data.provided.map((p) => <code key={p} className="rounded bg-gray-100 px-2 py-0.5 text-xs">{p}</code>)}
            </div>
          )}
        </div>
      )}

      {raw && <RawEditor base={base} list={list} onClose={() => setRaw(false)} onApplied={(r) => vars.setData(r)} />}
      {heroku && <HerokuImport base={base} onClose={() => setHeroku(false)} onDone={() => void vars.reload()} />}
      {shared && <ShareVariable eid={eid} sid={service.id} onClose={() => setShared(false)} onDone={() => void vars.reload()} />}
      {removing && (
        <Confirm title="Remove variable" body={<>Stage removal of <b className="font-mono">{removing}</b>? It takes effect on the next deploy.</>}
          confirm="Remove" danger onClose={() => setRemoving(null)} onConfirm={() => stage({ unset: [removing] })} />
      )}
    </div>
  );
}

/** Other services and their variable names, for `${{` autocomplete and "Add a reference". */
function useRefSources(eid: string, selfId: string) {
  const [services, setServices] = useState<{ id: string; name: string; isDb: boolean; vars: string[]; urlVar?: string }[]>([]);
  useEffect(() => {
    let off = false;
    (async () => {
      const c = await get<Canvas>(`/environments/${encodeURIComponent(eid)}/canvas`);
      const out = await Promise.all(c.services.filter((s) => s.id !== selfId).map(async (s) => {
        let names: string[] = [];
        try {
          const v = await get<VariablesResponse>(`${svcPath(eid, s.id)}/variables`);
          names = [...v.variables.map((x) => x.name), ...v.provided];
        } catch { /* viewer without access to that service's variables */ }
        const isDb = s.kind === "database";
        const urlVar = names.find((n) => n.endsWith("_URL") && !n.includes("PUBLIC"));
        return { id: s.id, name: s.config.name, isDb, vars: names, urlVar };
      }));
      if (!off) setServices(out);
    })().catch(() => undefined);
    return () => { off = true; };
  }, [eid, selfId]);
  const suggest = useMemo(() => services.map((s) => ({ name: s.name, vars: s.vars })), [services]);
  return { services, suggest };
}

function NewVariable({ initial, refs, onSave, onCancel }: {
  initial: { name: string; value: string }; refs: { name: string; vars: string[] }[];
  onSave: (name: string, value: string) => Promise<void>; onCancel: () => void;
}) {
  const [name, setName] = useState(initial.name);
  const [value, setValue] = useState(initial.value);
  const [sugg, setSugg] = useState<{ field: "name" | "value"; start: number; items: string[] } | null>(null);
  const a = useAction();
  const valueRef = useRef<HTMLTextAreaElement>(null);
  const nameOk = NAME_RE.test(name);

  const update = (field: "name" | "value", text: string, pos: number) => {
    if (field === "name") setName(text); else setValue(text);
    const r = refAt(text, pos);
    if (!r) { setSugg(null); return; }
    const items = refSuggestions(r.partial, refs).slice(0, 8);
    setSugg(items.length ? { field, start: r.start, items } : null);
  };
  const pick = (item: string) => {
    if (!sugg) return;
    const cur = sugg.field === "name" ? name : value;
    const done = !item.endsWith(".");
    const next = cur.slice(0, sugg.start) + "${{" + item + (done ? "}}" : "");
    if (sugg.field === "name") setName(next); else setValue(next);
    if (done) setSugg(null); else update(sugg.field, next, next.length);
  };
  const save = () => { if (nameOk) void a.run(() => onSave(name, value)); };

  return (
    <div className="mb-4 rounded-lg border border-purple-400 p-3">
      <div className="flex flex-col gap-2 sm:flex-row">
        <input aria-label="Variable name" placeholder="VARIABLE_NAME" value={name} autoFocus={!initial.name}
          onChange={(e) => update("name", e.target.value, e.target.selectionStart ?? e.target.value.length)}
          className="h-[34px] rounded-md border border-gray-200 bg-background px-3 font-mono text-sm outline-none sm:w-1/3" />
        <div className="relative flex-1">
          <textarea ref={valueRef} aria-label="Variable value" placeholder="value or ${{Service.VAR}}" value={value} rows={Math.min(8, Math.max(1, value.split("\n").length))}
            autoFocus={!!initial.name}
            onChange={(e) => update("value", e.target.value, e.target.selectionStart ?? e.target.value.length)}
            onKeyDown={(e) => {
              if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) { e.preventDefault(); save(); }
              if (e.key === "Escape") onCancel();
            }}
            className="min-h-[34px] w-full resize-y rounded-md border border-gray-200 bg-background px-3 py-1.5 font-mono text-sm outline-none" />
          {sugg && (
            <div role="listbox" className="absolute left-0 right-0 z-40 mt-1 rounded-md border border-gray-200 bg-secondaryBg py-1 shadow-lg">
              {sugg.items.map((i) => (
                <button key={i} role="option" aria-selected={false} onMouseDown={(e) => { e.preventDefault(); pick(i); }}
                  className="block w-full px-3 py-1 text-left font-mono text-xs hover:bg-gray-100">{i}</button>
              ))}
            </div>
          )}
        </div>
      </div>
      <div className="mt-2 flex items-center justify-between">
        <span className="text-xs text-gray-500">{name && !nameOk ? "Names use letters, digits and _ and cannot start with a digit." : "Cmd+Enter to add. Multi-line values are fine."}</span>
        <div className="flex gap-2">
          <Button onClick={onCancel}>Cancel</Button>
          <Button variant="primary" disabled={!nameOk || a.busy} onClick={save}>Add</Button>
        </div>
      </div>
      <ErrorLine>{a.error}</ErrorLine>
    </div>
  );
}

function RawEditor({ base, list, onClose, onApplied }: { base: string; list: Variable[]; onClose: () => void; onApplied: (r: VariablesResponse) => void }) {
  const [text, setText] = useState<string | null>(null);
  const [preview, setPreview] = useState(false);
  const a = useAction();
  const sealed = list.filter((v) => v.sealed).map((v) => v.name);
  useEffect(() => {
    post<RevealResponse>(`${base}/variables/reveal`, {})
      .then((r) => {
        const vals: Record<string, string> = {};
        for (const v of list) if (!v.sealed && r.values[v.name] !== undefined) vals[v.name] = r.values[v.name];
        setText(toEnv(vals));
      })
      .catch(() => setText(""));
  }, [base, list]);
  const parsed = useMemo(() => parseRaw(text ?? ""), [text]);
  const names = Object.keys(parsed.vars);

  return (
    <Modal title="Raw Editor" width={720} onClose={onClose}
      footer={<>
        <Button onClick={onClose}>Cancel</Button>
        {preview ? <>
          <Button onClick={() => setPreview(false)}>Back</Button>
          <Button variant="primary" disabled={a.busy || parsed.errors.length > 0}
            onClick={async () => {
              if (await a.run(async () => onApplied(await put<VariablesResponse>(`${base}/variables`, { raw: text ?? "" })))) onClose();
            }}>Apply {names.length} variable{names.length === 1 ? "" : "s"}</Button>
        </> : <Button variant="primary" disabled={text === null} onClick={() => setPreview(true)}>Review</Button>}
      </>}>
      {text === null ? <Loading /> : preview ? (
        <div>
          <div className="mb-2 text-gray-600">Parsed as {parsed.format === "json" ? "JSON" : ".env"}: {names.length} variables.</div>
          {parsed.errors.map((e) => <div key={e} className="text-red-600">{e}</div>)}
          <div className="max-h-80 overflow-y-auto rounded border border-gray-200">
            {names.map((n) => (
              <div key={n} className="flex gap-3 border-b border-gray-200 px-3 py-1.5 font-mono text-xs last:border-0">
                <span className="w-1/3 break-all">{n}</span><span className="flex-1 whitespace-pre-wrap break-all text-gray-600">{parsed.vars[n]}</span>
              </div>
            ))}
          </div>
          {sealed.length > 0 && <div className="mt-2 text-xs text-gray-500">Sealed variables are not shown and stay as they are: {sealed.join(", ")}</div>}
        </div>
      ) : (
        <div>
          <p className="mb-2 text-gray-600">Paste a <code>.env</code> file or a JSON object. The whole set is replaced with what you apply.</p>
          <textarea aria-label="Raw variables" value={text} onChange={(e) => setText(e.target.value)} rows={16} spellCheck={false}
            className="w-full rounded-md border border-gray-200 bg-background p-3 font-mono text-xs outline-none" />
        </div>
      )}
      <ErrorLine>{a.error}</ErrorLine>
    </Modal>
  );
}

function HerokuImport({ base, onClose, onDone }: { base: string; onClose: () => void; onDone: () => void }) {
  const [text, setText] = useState("");
  const a = useAction();
  return (
    <Modal title="Import from Heroku" width={640} onClose={onClose}
      footer={<>
        <Button onClick={onClose}>Cancel</Button>
        <Button variant="primary" disabled={!text.trim() || a.busy} onClick={async () => {
          if (await a.run(() => req("POST", `${base}/imports/heroku-env`, undefined, text))) { onDone(); onClose(); }
        }}>Import</Button>
      </>}>
      <p className="mb-2 text-gray-600">Paste the output of <code>heroku config -s</code> or <code>heroku config --json</code>.</p>
      <textarea aria-label="Heroku config" value={text} onChange={(e) => setText(e.target.value)} rows={12} spellCheck={false}
        className="w-full rounded-md border border-gray-200 bg-background p-3 font-mono text-xs outline-none" />
      <ErrorLine>{a.error}</ErrorLine>
    </Modal>
  );
}

function ShareVariable({ eid, sid, onClose, onDone }: { eid: string; sid: string; onClose: () => void; onDone: () => void }) {
  const env = `/environments/${encodeURIComponent(eid)}`;
  const sharedVars = useLoad(() => get<VariablesResponse>(`${env}/shared-variables`), [env]);
  const [pick, setPick] = useState("");
  const a = useAction();
  return (
    <Modal title="Shared Variable" onClose={onClose}
      footer={<>
        <Button onClick={onClose}>Cancel</Button>
        <Button variant="primary" disabled={!pick || a.busy} onClick={async () => {
          if (await a.run(() => post(`${env}/shared-variables/share`, { name: pick, services: [sid] }))) { onDone(); onClose(); }
        }}>Add to this service</Button>
      </>}>
      {!sharedVars.data ? (sharedVars.error ? <ErrorLine>{sharedVars.error}</ErrorLine> : <Loading />) :
        sharedVars.data.variables.length === 0 ? <Empty>No shared variables in this environment. Add them in Project Settings, Shared Variables.</Empty> : (
          <div className="flex flex-col gap-1">
            {sharedVars.data.variables.map((v) => (
              <label key={v.name} className="flex items-center gap-2 font-mono text-sm">
                <input type="radio" name="shared" checked={pick === v.name} onChange={() => setPick(v.name)} />{v.name}
                <CopyButton text={`\${{shared.${v.name}}}`} label="Copy reference" />
              </label>
            ))}
          </div>
        )}
      <ErrorLine>{a.error}</ErrorLine>
    </Modal>
  );
}
