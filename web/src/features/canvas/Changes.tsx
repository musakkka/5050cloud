// SPDX-License-Identifier: AGPL-3.0-only

// The floating "N changes" banner with Details, Discard and Deploy, and the
// Details review (SPEC 13.2). Shift+Enter deploys; Alt+Shift+Enter or
// Alt-click commits without deploying; Cmd+Enter in Details commits.
import { useEffect, useState } from "react";
import { X } from "lucide-react";
import { api, type Change, type ChangesResponse } from "../../api/client";
import { useProject } from "../../app/project";
import { Button, ErrorLine, Modal, Spinner, Textarea, toast, toastError } from "../../kit";

export const isTyping = (t: EventTarget | null) => {
  const el = t as HTMLElement | null;
  return !!el && (el.tagName === "INPUT" || el.tagName === "TEXTAREA" || el.tagName === "SELECT" || el.isContentEditable);
};

const show = (v: unknown) => (v === undefined || v === null || v === "" ? "(none)" : typeof v === "string" ? v : JSON.stringify(v));

export function ChangesBanner({ readOnly }: { readOnly?: boolean }) {
  const { eid, canvas, setCanvas, reload } = useProject();
  const n = canvas?.changesCount || 0;
  const [details, setDetails] = useState(false);
  const [busy, setBusy] = useState<"deploy" | "commit" | "discard">();

  const commit = async (deploy: boolean, message?: string) => {
    if (busy) return;
    const before = n;
    setBusy(deploy ? "deploy" : "commit");
    setCanvas((c) => ({ ...c, changesCount: 0 }));   // optimistic: the banner goes at once
    try {
      const r = await api.commit(eid, { deploy, message: message || undefined });
      setDetails(false);
      toast(deploy ? `Deploying ${r.deployments.length} service${r.deployments.length === 1 ? "" : "s"}` : "Changes committed without deploying");
    } catch (e) {
      setCanvas((c) => ({ ...c, changesCount: before }));
      toastError(e);
    } finally { setBusy(undefined); void reload(); }
  };
  const discard = async () => {
    const before = n;
    setBusy("discard");
    setCanvas((c) => ({ ...c, changesCount: 0 }));
    try { await api.discardChanges(eid); } catch (e) { setCanvas((c) => ({ ...c, changesCount: before })); toastError(e); } finally { setBusy(undefined); void reload(); }
  };

  useEffect(() => {
    if (!n || readOnly) return;
    const k = (e: KeyboardEvent) => {
      if (e.key !== "Enter" || !e.shiftKey || isTyping(e.target) || details) return;
      e.preventDefault();
      void commit(!e.altKey);
    };
    window.addEventListener("keydown", k);
    return () => window.removeEventListener("keydown", k);
  });

  if (!n) return null;
  return (
    <>
      <div role="region" aria-label="Staged changes" className="absolute bottom-6 left-1/2 z-30 flex -translate-x-1/2 items-center gap-2 rounded-xl border border-pink-500 bg-secondaryBg px-3 py-2 shadow-[0_10px_30px_rgba(0,0,0,.35)]">
        <span className="whitespace-nowrap px-1 text-sm font-medium text-pink-600">{n} change{n === 1 ? "" : "s"}</span>
        <Button variant="ghost" onClick={() => setDetails(true)}>Details</Button>
        {!readOnly && <Button variant="ghost" busy={busy === "discard"} onClick={() => void discard()}>Discard</Button>}
        {!readOnly && <Button variant="primary" busy={busy === "deploy" || busy === "commit"} title="Shift+Enter. Alt-click or Alt+Shift+Enter commits without deploying."
          onClick={(e) => void commit(!e.altKey)}>Deploy</Button>}
      </div>
      {details && <Details readOnly={readOnly} busy={!!busy} onClose={() => setDetails(false)} onCommit={(m, d) => void commit(d, m)} />}
    </>
  );
}

function Details({ onClose, onCommit, busy, readOnly }: { onClose: () => void; onCommit: (message: string, deploy: boolean) => void; busy: boolean; readOnly?: boolean }) {
  const { eid, reload } = useProject();
  const [data, setData] = useState<ChangesResponse>();
  const [error, setError] = useState<unknown>();
  const [message, setMessage] = useState("");
  const load = () => api.changes(eid).then(setData, setError);
  useEffect(() => { void load(); }, [eid]); // eslint-disable-line react-hooks/exhaustive-deps
  const drop = async (c: Change) => {
    const before = data;
    setData((d) => d && { ...d, changes: d.changes.filter((x) => x.id !== c.id) });
    try { await api.discardChange(eid, c.id); void reload(); } catch (e) { setData(before); toastError(e); }
  };
  return (
    <Modal title="Staged changes" onClose={onClose} width={680}>
      <div className="space-y-4 pb-2" onKeyDown={(e) => { if (e.key === "Enter" && (e.metaKey || e.ctrlKey) && !readOnly) { e.preventDefault(); onCommit(message, !e.altKey); } }}>
        <ErrorLine error={error} />
        {!data && !error && <Spinner />}
        {data?.changes.map((c) => (
          <div key={c.id} className="rounded-md border border-gray-200 px-3 py-2">
            <div className="flex items-center gap-2">
              <span className="font-medium text-foreground">{c.service || "Shared variables"}</span>
              <span className="text-xs text-gray-500">{c.kind}</span>
              {c.tag && <span className="rounded bg-pink-100 px-1.5 text-[11px] font-medium uppercase text-pink-600">{c.tag === "new" ? "New" : c.tag === "removed" ? "Removed" : "Edited"}</span>}
              <span className="ml-auto text-xs text-gray-500">{c.author}</span>
              {!readOnly && <button type="button" aria-label={`Discard change to ${c.service || "shared variables"}`} onClick={() => void drop(c)} className="text-gray-500 hover:text-foreground"><X size={14} /></button>}
            </div>
            <ul className="mt-1 space-y-0.5 font-mono text-xs leading-5">
              {c.diff.map((d) => <li key={d.path} className="break-all"><span className="text-gray-500">{d.path}</span>: <span className="text-red-600 line-through">{show(d.old)}</span> {"->"} <span className="text-green-600">{show(d.new)}</span></li>)}
            </ul>
            {c.rewrites && c.rewrites.length > 0 && (
              <div className="mt-1 text-xs"><span className="text-gray-500">Reference rewrites:</span>
                <ul className="font-mono">{c.rewrites.map((d) => <li key={d.path} className="break-all">{d.path}: {show(d.old)} {"->"} {show(d.new)}</li>)}</ul>
              </div>
            )}
            {c.notes?.map((n) => <div key={n} className="text-xs text-yellow-600">{n}</div>)}
          </div>
        ))}
        {data && data.changes.length === 0 && <p className="text-sm text-gray-500">No staged changes.</p>}
        {!readOnly && <>
          <Textarea aria-label="Commit message" rows={2} placeholder="Commit message (optional)" value={message} onChange={(e) => setMessage(e.target.value)} className="font-sans" />
          <div className="flex justify-end gap-2 pb-3">
            <Button disabled={busy} onClick={() => onCommit(message, false)}>Commit without deploying</Button>
            <Button variant="primary" busy={busy} title="Cmd+Enter" onClick={() => onCommit(message, true)}>Deploy</Button>
          </div>
        </>}
      </div>
    </Modal>
  );
}
