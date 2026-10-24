// SPDX-License-Identifier: AGPL-3.0-only

// The New Item dialog: metric type and service comboboxes, live preview, title with an optional
// description, and Save Item. Also used for editing a widget.

import { useMemo, useState } from "react";
import { Plus } from "lucide-react";
import type { Service } from "../../api/types.gen";
import { Button, Modal, ServiceIcon } from "../../kit";
import { Combo, type ComboOption } from "./Combo";
import { useEnvMetrics } from "./data";
import { METRIC_TYPES, typeOf, type Widget } from "./widgets";
import { WidgetView } from "./WidgetView";
import type { WidgetMetric } from "./model";

const PREVIEW_MS = 4 * 86400e3;

export function NewItemModal({ eid, services, initial, onSave, onClose }: {
  eid: string; services: Service[]; initial?: Widget; onSave: (w: Omit<Widget, "id">) => void; onClose: () => void;
}) {
  const [metric, setMetric] = useState<WidgetMetric>(initial?.metric ?? "cpu");
  const [service, setService] = useState(initial?.service ?? "");
  const [title, setTitle] = useState(initial?.title ?? typeOf(initial?.metric ?? "cpu").label);
  const [touched, setTouched] = useState(!!initial);
  const [desc, setDesc] = useState(initial?.description ?? "");
  const [showDesc, setShowDesc] = useState(!!initial?.description);
  const sel = useMemo(() => { const now = Date.now(); return { from: now - PREVIEW_MS, until: now }; }, []);
  const m = useEnvMetrics(eid, services.map((s) => s.id), sel, false);

  const metricOpts: ComboOption[] = METRIC_TYPES.map((t) => ({ value: t.id, label: t.label, icon: <t.icon size={20} strokeWidth={1.75} className="shrink-0 text-fg-2" /> }));
  const serviceOpts: ComboOption[] = [
    { value: "", label: "(All services)" },
    ...services.map((s) => ({ value: s.id, label: s.config.name, icon: <ServiceIcon icon={s.config.icon} source={s.config.source.type} size={20} /> })),
  ];
  const pickMetric = (v: string) => {
    setMetric(v as WidgetMetric);
    if (!touched) setTitle(typeOf(v as WidgetMetric).label);
  };
  const save = () => onSave({ metric, service, title: title.trim() || typeOf(metric).label, description: desc.trim() || undefined });

  return (
    <Modal bare title={initial ? "Edit Item" : "New Item"} width={1080} onClose={onClose}>
      <form className="flex flex-col px-12 pb-12 pt-8" onSubmit={(e) => { e.preventDefault(); save(); }}>
        <h3 className="mb-8 text-h3 text-foreground">{initial ? "Edit Item" : "New Item"}</h3>
        <div className="flex">
          <Combo className="w-[320px] shrink-0" round="rounded-l-md" label="Metric type" value={metric} options={metricOpts} onChange={pickMetric} />
          <Combo className="min-w-0 flex-1" round="rounded-r-md border-l-0" label="Service" searchable placeholder="(All services)" value={service} options={serviceOpts} onChange={setService} />
        </div>
        <div className="mt-6">
          <WidgetView widget={{ metric, service, title }} eid={eid} services={services} byId={m.byId} error={m.error} onRetry={m.reload}
            from={sel.from} until={sel.until} height={411} tone="inset" />
        </div>
        <div className="mt-6 flex h-[42px] items-center rounded-md border border-line bg-transparent pr-1 transition-colors focus-within:border-fg-4">
          <input aria-label="Title" value={title} onChange={(e) => { setTitle(e.target.value); setTouched(true); }}
            className="h-full min-w-0 flex-1 bg-transparent px-3 text-base text-foreground outline-none focus-visible:shadow-none" />
          {!showDesc && (
            <button type="button" onClick={() => setShowDesc(true)}
              className="inline-flex h-[34px] items-center gap-2 rounded-md px-3 text-body text-fg-5 transition-colors hover:text-fg-2">
              <Plus size={16} />Add Description
            </button>
          )}
        </div>
        {showDesc && (
          <input aria-label="Description" autoFocus value={desc} onChange={(e) => setDesc(e.target.value)} placeholder="Description"
            className="mt-2 h-[42px] rounded-md border border-line bg-transparent px-3 text-base text-foreground outline-none transition-colors placeholder:text-fg-4 focus:border-fg-4 focus-visible:shadow-none" />
        )}
        <Button type="submit" variant="primary" size="lg" block className="mt-6">Save Item</Button>
      </form>
    </Modal>
  );
}
