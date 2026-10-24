// SPDX-License-Identifier: AGPL-3.0-only

// /project/:pid/observability (docs/SPEC.md 13.1): a board of metric and log widgets for the
// environment, with Add block, a shared range and live toggle, and a starter dashboard.

import { useMemo, useState } from "react";
import { GripVertical, MoreVertical, Pencil, SquarePlus, Trash2 } from "lucide-react";
import type { Canvas } from "../../api/types.gen";
import { Button, Confirm, IconButton, Menu, Notice, Skeleton } from "../../kit";
import { get } from "../service/client";
import { useLoad } from "../service/ui";
import { CursorSync } from "./chart";
import { useEnvMetrics } from "./data";
import { NewItemModal } from "./NewItemModal";
import { rangeBounds, readRange, type RangeSel } from "./series";
import { RangePicker, LiveToggle } from "./Toolbar";
import { STARTER, useWidgets, type Widget } from "./widgets";
import { WidgetView } from "./WidgetView";

export default function Observability({ environmentId }: { environmentId: string; onOpenService?: (sid: string) => void }) {
  const eid = environmentId;
  const c = useLoad(() => get<Canvas>(`/environments/${encodeURIComponent(eid)}/canvas`), [eid]);
  const services = c.data?.services ?? [];
  const { widgets, add, addMany, update, remove, move } = useWidgets(eid);
  const [sel, setSel] = useState<RangeSel>(() => readRange(window.location.search));
  const [live, setLive] = useState(true);
  const [modal, setModal] = useState<{ edit?: Widget } | null>(null);
  const [removing, setRemoving] = useState<Widget | null>(null);
  const [dragFrom, setDragFrom] = useState<number | null>(null);
  const [overIdx, setOverIdx] = useState<number | null>(null);
  const ids = useMemo(() => (widgets.length ? services.map((s) => s.id) : []), [widgets.length, services]);
  const m = useEnvMetrics(eid, ids, sel, live);
  const first = m.byId ? Object.values(m.byId)[0] : undefined;
  const bounds = first ?? rangeBounds(sel);

  return (
    <div className="flex h-full min-h-0 flex-col bg-background">
      <div className="flex shrink-0 items-center justify-between gap-2 px-4 pb-0 pt-4">
        <Button icon={<SquarePlus size={16} />} onClick={() => setModal({})}>Add block</Button>
        <div className="flex items-center gap-2">
          <RangePicker value={sel} onChange={setSel} />
          <LiveToggle live={live} onChange={setLive} />
        </div>
      </div>
      <div className="scroll-thin flex min-h-0 flex-1 flex-col overflow-y-auto p-4">
        {c.error && <Notice tone="danger">{c.error}</Notice>}
        {!c.data && !c.error ? (
          <div className="grid grid-cols-1 gap-4 min-[900px]:grid-cols-2 min-[900px]:gap-x-6">{[0, 1].map((i) => <Skeleton key={i} h={362} className="rounded-lg" />)}</div>
        ) : widgets.length === 0 ? (
          <EmptyBoard onAdd={() => setModal({})} onStarter={() => addMany(STARTER)} />
        ) : (
          <CursorSync>
            <div className="grid grid-cols-1 gap-4 min-[900px]:grid-cols-2 min-[900px]:gap-x-6">
              {widgets.map((w, i) => (
                <div key={w.id} draggable={dragFrom === i}
                  onDragStart={(e) => { e.dataTransfer.effectAllowed = "move"; }}
                  onDragOver={(e) => { if (dragFrom !== null) { e.preventDefault(); setOverIdx(i); } }}
                  onDrop={(e) => { e.preventDefault(); if (dragFrom !== null) move(dragFrom, i); setDragFrom(null); setOverIdx(null); }}
                  onDragEnd={() => { setDragFrom(null); setOverIdx(null); }}
                  className={`rounded-lg transition-shadow duration-150 ${overIdx === i && dragFrom !== null && dragFrom !== i ? "shadow-ring" : ""} ${dragFrom === i ? "opacity-60" : ""}`}>
                  <WidgetView widget={w} eid={eid} services={services} byId={m.byId} error={m.error} onRetry={m.reload} from={bounds.from} until={bounds.until}
                    actions={
                      <div className="-mr-2 flex items-center">
                        <span aria-label="Drag to reorder" title="Drag to reorder" onMouseDown={() => setDragFrom(i)} onMouseUp={() => setDragFrom(null)}
                          className="inline-flex h-[26px] w-[26px] cursor-grab items-center justify-center rounded-md text-fg-4 transition-colors hover:text-foreground active:cursor-grabbing"><GripVertical size={16} /></span>
                        <Menu align="right" width={160} trigger={<IconButton label="Widget actions" size={26}><MoreVertical size={16} /></IconButton>}
                          items={[
                            { label: "Edit", icon: <Pencil size={16} />, onClick: () => setModal({ edit: w }) },
                            { label: "Delete", icon: <Trash2 size={16} />, danger: true, onClick: () => setRemoving(w) },
                          ]} />
                      </div>
                    } />
                </div>
              ))}
            </div>
          </CursorSync>
        )}
      </div>
      {modal && (
        <NewItemModal eid={eid} services={services} initial={modal.edit} onClose={() => setModal(null)}
          onSave={(w) => { if (modal.edit) update({ ...modal.edit, ...w }); else add(w); setModal(null); }} />
      )}
      {removing && <Confirm title="Delete item" body={`Remove "${removing.title}" from this board?`} confirm="Delete" danger onClose={() => setRemoving(null)} onConfirm={() => remove(removing.id)} />}
    </div>
  );
}

function EmptyBoard({ onAdd, onStarter }: { onAdd: () => void; onStarter: () => void }) {
  return (
    <div className="flex min-h-[520px] flex-1 flex-col items-center justify-center rounded-lg border border-dashed border-line px-6 py-12 text-center animate-fade-in">
      <Illustration />
      <h2 className="mt-8 text-h3 text-foreground">Observe this environment</h2>
      <p className="mt-2 max-w-[384px] text-body text-fg-2">Monitor project usage, resource metrics and custom log dashboards.</p>
      <div className="mt-6 flex w-full max-w-[384px] flex-col gap-2">
        <Button size="lg" block onClick={onAdd}>Add new item</Button>
        <Button size="lg" variant="primary" block onClick={onStarter}>Start with a simple dashboard</Button>
      </div>
    </div>
  );
}

/** Four mini bar charts in a 2x2 arrangement, drawn from neutral frames. */
function Illustration() {
  const sets = [[6, 10, 7, 13, 9], [4, 8, 12, 9, 14], [10, 6, 11, 5, 8], [7, 12, 9, 15, 11]];
  return (
    <div className="grid grid-cols-2 gap-3" aria-hidden>
      {sets.map((bars, i) => (
        <div key={i} className="flex h-[76px] w-[96px] items-end gap-1.5 rounded-lg border border-line bg-surface-raised px-3 pb-3 pt-4">
          {bars.map((h, j) => (
            <span key={j} className="w-full origin-bottom rounded-sm animate-rise-in" style={{ height: h * 3, background: j === bars.length - 1 ? "var(--accent)" : "var(--gray-200)", animationDelay: `${(i * 5 + j) * 40}ms` }} />
          ))}
        </div>
      ))}
    </div>
  );
}
