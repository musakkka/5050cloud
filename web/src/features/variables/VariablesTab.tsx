// SPDX-License-Identifier: AGPL-3.0-only

// Variables tab (docs/SPEC.md 13.3). Every edit is staged; the canvas banner
// picks the change up from the events stream.

import { useMemo, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { Braces, ChevronUp, CornerDownRight, Link2, Plus, Search, X } from "lucide-react";
import type { RevealResponse, Service, SetVariablesRequest, Variable, VariablesResponse } from "../../api/types.gen";
import { Button, IconButton, Skeleton, cx, readPref, toastError, writePref } from "../../kit";
import { LINKS } from "../../app/links";
import { get, post, put, svcPath } from "../service/client";
import { ErrorLine, useLoad } from "../service/ui";
import { DeleteDialog, GeneratorDialog, PromoteDialog, SealDialog, SECRET_DEFAULT } from "./dialogs";
import { ProvidedGroup, SharedPanel } from "./groups";
import { NewVariableForm } from "./NewVariableForm";
import { RawEditor } from "./RawEditor";
import { useRefSources, isGenerated } from "./refs";
import { VariableRow, type RowApi } from "./VariableRow";

type Dialog =
  | { kind: "seal" | "promote" | "delete" | "generator"; v: Variable }
  | null;

export default function VariablesTab({ eid, service }: { eid: string; service: Service }) {
  const base = svcPath(eid, service.id);
  const env = `/environments/${encodeURIComponent(eid)}`;
  const { pid = "" } = useParams();
  const nav = useNavigate();
  const vars = useLoad(() => get<VariablesResponse>(`${base}/variables`), [base]);
  const [adding, setAdding] = useState<{ openRefs: boolean } | null>(null);
  const [editing, setEditing] = useState<string | null>(null);
  const [raw, setRaw] = useState(false);
  const [shared, setShared] = useState(false);
  const [searching, setSearching] = useState(false);
  const [query, setQuery] = useState("");
  const [dialog, setDialog] = useState<Dialog>(null);
  const hintKey = `5050.vars.hint.${service.id}`;
  const [hintGone, setHintGone] = useState(() => readPref(hintKey, false));

  const list = useMemo(() => vars.data?.variables ?? [], [vars.data]);
  const own = useMemo(() => [...list.map((v) => v.name), ...(vars.data?.provided ?? [])], [list, vars.data]);
  const refs = useRefSources(eid, service.id, own);

  const stage = async (body: SetVariablesRequest) => {
    vars.setData(await put<VariablesResponse>(`${base}/variables`, body));
  };
  const guard = <T,>(fn: () => Promise<T>) => async (): Promise<T> => {
    try { return await fn(); } catch (e) { toastError(e); throw e; }
  };
  const resolved = async (name: string) => {
    const r = await post<RevealResponse>(`${base}/variables/reveal`, { names: [name], resolved: true });
    return r.values[name] ?? "";
  };

  const api: RowApi = {
    resolved: (n) => guard(() => resolved(n))(),
    raw: (v) => guard(async () => {
      if (v.reference) return v.reference;
      const r = await post<RevealResponse>(`${base}/variables/reveal`, { names: [v.name] });
      return r.values[v.name] ?? "";
    })(),
    save: (name, value) => guard(() => stage({ set: { [name]: value } }))(),
    seal: (v) => setDialog({ kind: "seal", v }),
    promote: (v) => setDialog({ kind: "promote", v }),
    generator: (v) => setDialog({ kind: "generator", v }),
    remove: (v) => setDialog({ kind: "delete", v }),
  };

  const dbSources = refs.sources.filter((s) => s.isDb);
  const showHint = !hintGone && dbSources.length > 0 &&
    !list.some((v) => v.reference && dbSources.some((d) => v.reference!.includes("${{" + d.service + ".")));

  if (!vars.data) {
    return vars.error ? <ErrorLine>{vars.error}</ErrorLine> : (
      <div aria-busy>
        <div className="flex h-[34px] items-center justify-between border-b border-gray-100 pb-6 box-content"><Skeleton w={180} h={20} /><Skeleton w={300} h={34} /></div>
        <div className="mt-4 space-y-1">{Array.from({ length: 8 }, (_, i) => <Skeleton key={i} h={36} className="w-full" />)}</div>
      </div>
    );
  }

  const q = query.trim().toLowerCase();
  const shown = q ? list.filter((v) => v.name.toLowerCase().includes(q)) : list;
  const goShared = () => nav(`/project/${pid}/settings/shared-variables${window.location.search}`);
  const count = list.length;

  return (
    <div>
      <div className="mb-4 border-b border-gray-100 pb-6">
        {adding ? (
          <NewVariableForm openRefs={adding.openRefs} items={refs.items} existing={list.map((v) => v.name)}
            onCancel={() => setAdding(null)}
            onSave={async (name, value) => { await stage({ set: { [name]: value } }); setAdding(null); }} />
        ) : (
          <div className="flex flex-wrap items-center justify-between gap-2">
            <div className="flex min-h-[34px] items-center gap-1">
              <h2 className="text-base font-medium leading-6 text-foreground">{count} Service Variable{count === 1 ? "" : "s"}</h2>
              {searching ? (
                <div className="relative ml-2 animate-fade-in">
                  <Search size={16} className="pointer-events-none absolute left-2 top-1/2 -translate-y-1/2 text-fg-3" />
                  <input autoFocus aria-label="Filter variables" placeholder="Search..." value={query} onChange={(e) => setQuery(e.target.value)}
                    onKeyDown={(e) => { if (e.key === "Escape") { e.stopPropagation(); setQuery(""); setSearching(false); } }}
                    onBlur={() => { if (!query) setSearching(false); }}
                    className="h-8 w-48 rounded-md border border-gray-200 bg-transparent pl-8 pr-2 text-sm text-foreground outline-none focus-visible:shadow-none transition-colors placeholder:text-fg-4 hover:border-gray-400 focus:border-pink-400" />
                </div>
              ) : (
                <button type="button" title="Filter variables" aria-label="Filter variables" aria-expanded={false} onClick={() => setSearching(true)}
                  className="flex h-[34px] w-[34px] items-center justify-center rounded-md text-gray-400 transition-colors hover:bg-gray-100 hover:text-gray-600">
                  <Search size={16} />
                </button>
              )}
            </div>
            <div className="flex items-center gap-1">
              <Button variant="accent-ghost" aria-controls="shared-variables-panel" aria-expanded={shared}
                className={cx("!text-fg-2 hover:!text-foreground [&>svg:first-child]:text-pink-700", shared && "!border-pink-200")}
                icon={shared ? <ChevronUp size={16} /> : <CornerDownRight size={16} />}
                onClick={() => setShared((s) => !s)}>{shared ? "Close Shared Variables" : "Shared Variable"}</Button>
              <Button variant="accent-ghost" className="hidden !text-fg-2 hover:!text-foreground md:inline-flex [&>svg:first-child]:text-pink-700"
                icon={<Braces size={16} />} onClick={() => setRaw(true)}>Raw Editor</Button>
              <Button variant="accent-outline" icon={<Plus size={16} />} onClick={() => setAdding({ openRefs: false })}>New Variable</Button>
            </div>
          </div>
        )}
      </div>

      <div id="shared-variables-panel">
        <SharedPanel open={shared} onClose={() => setShared(false)} onConfigure={goShared} docsHref={LINKS.docs} />
      </div>

      {showHint && (
        <div className="mb-3 flex h-[42px] items-center gap-3 rounded-md bg-pink-50 px-3 py-1 text-sm text-pink-600 animate-fade-in">
          <Link2 size={16} className="shrink-0" />
          <span className="min-w-0 flex-1 truncate">Trying to connect a database?{" "}
            <button type="button" className="underline underline-offset-2 hover:text-pink-700" onClick={() => setAdding({ openRefs: true })}>Add Variable</button>
          </span>
          <IconButton label="Dismiss" size={26} className="!text-fg-2" onClick={() => { writePref(hintKey, true); setHintGone(true); }}><X size={16} /></IconButton>
        </div>
      )}

      {count === 0 && !adding && (
        <div className="mt-4 flex h-[106px] flex-col items-center justify-center gap-1 rounded-md border border-dashed border-gray-200 text-center text-sm text-fg-2">
          <p>No variables yet.</p>
          <p className="text-fg-3">Add one with New Variable, or paste a .env file in the Raw Editor.</p>
        </div>
      )}
      {count > 0 && shown.length === 0 && (
        <div className="mt-4 flex h-[106px] items-center justify-center rounded-md border border-dashed border-gray-200 text-sm leading-6 text-fg-2">
          No variables match “{query.trim()}”
        </div>
      )}
      <div role="list" aria-label="Variables">
        {shown.map((v) => (
          <VariableRow key={v.name} v={v} api={api} editing={editing === v.name}
            onEdit={() => setEditing(v.name)} onEditEnd={() => setEditing((e) => (e === v.name ? null : e))} />
        ))}
      </div>

      <ProvidedGroup names={vars.data.provided} docsHref={LINKS.docs} />

      {raw && <RawEditor base={base} list={list} onClose={() => setRaw(false)} onApplied={(r) => vars.setData(r)} />}
      {dialog?.kind === "delete" && (
        <DeleteDialog name={dialog.v.name} onClose={() => setDialog(null)} onConfirm={guard(() => stage({ unset: [dialog.v.name] }))} />
      )}
      {dialog?.kind === "seal" && (
        <SealDialog name={dialog.v.name} onClose={() => setDialog(null)} onConfirm={guard(() => stage({ seal: [dialog.v.name] }))} />
      )}
      {dialog?.kind === "promote" && (
        <PromoteDialog name={dialog.v.name} onClose={() => setDialog(null)}
          onConfirm={guard(async () => {
            const v = dialog.v;
            let value = v.reference ?? "";
            if (!value) value = (await post<RevealResponse>(`${base}/variables/reveal`, { names: [v.name] })).values[v.name] ?? "";
            await put(`${env}/shared-variables`, { set: { [v.name]: value } });
            await stage({ set: { [v.name]: "${{shared." + v.name + "}}" } });
          })} />
      )}
      {dialog?.kind === "generator" && (
        <GeneratorDialog initial={isGenerated(dialog.v.reference) ? dialog.v.reference! : SECRET_DEFAULT} onClose={() => setDialog(null)}
          onSave={(t) => guard(() => stage({ set: { [dialog.v.name]: t } }))()} />
      )}
    </div>
  );
}

