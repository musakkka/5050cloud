// SPDX-License-Identifier: AGPL-3.0-only

// Activity slide-over from the project top bar (9.24): the environment's feed,
// filter by service, open a commit to see its full diff.
import { useEffect, useState } from "react";
import { X } from "lucide-react";
import { api, type Activity, type CommitDetail } from "../../api/client";
import { useProject, useProjectEvents } from "../../app/project";
import { Button, ErrorLine, Modal, RelTime, Spinner, StatusGlyph } from "../../kit";

export function ActivityPanel({ onClose }: { onClose: () => void }) {
  const { eid, canvas } = useProject();
  const [service, setService] = useState("");
  const [items, setItems] = useState<Activity[]>();
  const [next, setNext] = useState<string>();
  const [error, setError] = useState<unknown>();
  const [commit, setCommit] = useState<string>();

  useEffect(() => {
    setItems(undefined);
    api.activity(eid, { service: service || undefined }).then((p) => { setItems(p.items || []); setNext(p.next); }, setError);
  }, [eid, service]);
  useProjectEvents((e) => {
    if (e.type === "activity" && e.activity && (!service || e.activity.serviceId === service)) setItems((l) => [e.activity!, ...(l || [])]);
  });
  const more = () => next && api.activity(eid, { service: service || undefined, before: next }).then((p) => { setItems((l) => [...(l || []), ...(p.items || [])]); setNext(p.next); }, setError);
  const name = (id?: string) => canvas?.services.find((s) => s.id === id)?.config.name;

  return (
    <aside aria-label="Activity" className="panel-in fixed inset-y-0 right-0 z-[90] flex w-full max-w-[420px] flex-col border-l border-gray-200 bg-secondaryBg shadow-2xl">
      <div className="flex items-center justify-between border-b border-gray-200 px-5 py-4">
        <h2 className="text-base font-semibold">Activity</h2>
        <button type="button" aria-label="Close" onClick={onClose} className="text-gray-500 hover:text-foreground"><X size={18} /></button>
      </div>
      <div className="px-5 py-3">
        <select aria-label="Filter by service" value={service} onChange={(e) => setService(e.target.value)} className="h-9 w-full rounded-md border border-gray-200 bg-background px-2 text-sm">
          <option value="">All services</option>
          {canvas?.services.map((s) => <option key={s.id} value={s.id}>{s.config.name}</option>)}
        </select>
      </div>
      <div className="flex-1 overflow-y-auto px-5 pb-5 scroll-thin">
        <ErrorLine error={error} />
        {!items && !error && <Spinner />}
        {items?.length === 0 && <div className="py-8 text-center text-sm text-gray-500">No activity yet.</div>}
        <ol className="space-y-3">
          {items?.map((a) => (
            <li key={a.id} className="flex gap-3 text-sm">
              <div className="pt-0.5">{a.status ? <StatusGlyph status={a.status} size={16} /> : <span className="block h-4 w-4 rounded-full border border-gray-300" />}</div>
              <div className="min-w-0 flex-1">
                <div className="text-gray-800">{a.text}</div>
                <div className="text-xs text-gray-500">
                  {name(a.serviceId) && <>{name(a.serviceId)} · </>}{a.actor && <>{a.actor} · </>}<RelTime ts={a.ts} />
                  {a.kind === "commit" && a.ref && <> · <button type="button" className="underline hover:text-foreground" onClick={() => setCommit(a.ref)}>View changes</button></>}
                </div>
              </div>
            </li>
          ))}
        </ol>
        {next && <Button className="mt-4 w-full" onClick={() => void more()}>Load more</Button>}
      </div>
      {commit && <CommitModal id={commit} onClose={() => setCommit(undefined)} />}
    </aside>
  );
}

const show = (v: unknown) => (v === undefined || v === null ? "(none)" : typeof v === "string" ? v : JSON.stringify(v));

function CommitModal({ id, onClose }: { id: string; onClose: () => void }) {
  const [c, setC] = useState<CommitDetail>();
  const [error, setError] = useState<unknown>();
  useEffect(() => { api.commitDetail(id).then(setC, setError); }, [id]);
  return (
    <Modal title={c?.message || "Commit"} onClose={onClose} width={640}>
      <ErrorLine error={error} />
      {!c && !error && <Spinner />}
      {c && (
        <div className="space-y-4 pb-4">
          <div className="text-xs text-gray-500">{c.author} · <RelTime ts={c.createdAt} /></div>
          {c.changes.map((ch) => (
            <div key={ch.id}>
              <div className="font-medium">{ch.service || "Shared variables"} <span className="text-xs text-gray-500">{ch.kind}</span></div>
              <ul className="mt-1 space-y-1 font-mono text-xs">
                {ch.diff.map((d) => <li key={d.path}><span className="text-gray-500">{d.path}</span>: <span className="text-red-600 line-through">{show(d.old)}</span> {"->"} <span className="text-green-600">{show(d.new)}</span></li>)}
              </ul>
            </div>
          ))}
        </div>
      )}
    </Modal>
  );
}
