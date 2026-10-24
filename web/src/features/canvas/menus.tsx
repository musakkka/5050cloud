// SPDX-License-Identifier: AGPL-3.0-only

// What the canvas can create (the Add menu and the pane menu), the palette bridge, the
// creation flows, and the small dialogs behind the node and group menus: name and icon,
// start command and healthcheck path, group rename and group icon.
import { useEffect, useLayoutEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { useNavigate } from "react-router-dom";
import { Archive, Database, HardDrive, Layers, Package, Search, SquareFunction, SquareTerminal } from "lucide-react";
import { api, type Service, type UpdateServiceConfigRequest } from "../../api/client";
import { useProject } from "../../app/project";
import { BRAND_KEYS, BrandIcon, Button, ErrorLine, Field, Input, Modal, ServiceIcon, cx, toast, toastError } from "../../kit";
import { ComposeFlow, DatabaseFlow, GitHubFlow, ImageFlow, VolumeFlow, createEmptyService, type Target } from "../new/flows";

export type NewKind = "github" | "database" | "template" | "image" | "empty" | "volume" | "compose";

/** The pane menu order. Function and Bucket are not in v0.1 and show why. */
export const NEW_MENU: { id: NewKind | "bucket" | "function"; label: string; disabled?: string }[] = [
  { id: "github", label: "GitHub Repo" },
  { id: "template", label: "Template" },
  { id: "database", label: "Database" },
  { id: "image", label: "Docker Image" },
  { id: "function", label: "Function", disabled: "Not in v0.1: deploy a small service from a Docker image" },
  { id: "bucket", label: "Bucket", disabled: "Not in v0.1: run MinIO from a template" },
  { id: "empty", label: "Empty Service" },
  { id: "volume", label: "Volume" },
];

export function newIcon(id: string, size = 16): ReactNode {
  switch (id) {
    case "github": return <BrandIcon name="github" size={size} mono />;
    case "template": return <Layers size={size} />;
    case "database": return <Database size={size} />;
    case "image": return <Package size={size} />;
    case "function": return <SquareFunction size={size} />;
    case "bucket": return <Archive size={size} />;
    case "volume": return <HardDrive size={size} />;
    default: return <SquareTerminal size={size} />;
  }
}

/** Window events let the palette drive the canvas without importing it. */
export const CANVAS_EVENT = "5050:canvas";
export type CanvasCommand =
  | { kind: "new"; what: NewKind }
  | { kind: "toggle-connections" }
  | { kind: "tidy" }
  | { kind: "auto-layout" }
  | { kind: "repair-overlaps" }
  | { kind: "reset" }
  | { kind: "toggle-positions" }
  | { kind: "layer"; layer: "traffic" | "references" }
  | { kind: "update-info"; serviceId: string };
export const sendCanvas = (c: CanvasCommand) => window.dispatchEvent(new CustomEvent<CanvasCommand>(CANVAS_EVENT, { detail: c }));

/** Opens the creation flows in the current environment. */
export function NewFlows({ open, position, serviceId, onClose }: { open?: NewKind; position?: number[]; serviceId?: string; onClose: () => void }) {
  const { pid, eid, canvas, reload } = useProject();
  const nav = useNavigate();
  const target = useMemo<Target>(() => ({
    canvas: true,
    position,
    ensure: async () => ({ pid, eid }),
    done: () => { void reload(); onClose(); },
  }), [pid, eid, position, reload, onClose]);
  useEffect(() => {
    if (open === "template") { onClose(); nav(`/new/template?project=${encodeURIComponent(pid)}&environment=${encodeURIComponent(eid)}`); }
    if (open === "empty") { onClose(); createEmptyService(target).catch(toastError); }
  }, [open, nav, pid, eid, target, onClose]);
  switch (open) {
    case "github": return <GitHubFlow target={target} onClose={onClose} />;
    case "image": return <ImageFlow target={target} onClose={onClose} />;
    case "database": return <DatabaseFlow target={target} onClose={onClose} />;
    case "compose": return <ComposeFlow target={target} onClose={onClose} />;
    case "volume": return <VolumeFlow target={target} services={canvas?.services || []} serviceId={serviceId} onClose={onClose} />;
    default: return null;
  }
}

/** A searchable grid of official logos. */
function IconGrid({ value, onPick }: { value?: string | null; onPick: (k: string) => void }) {
  const [q, setQ] = useState("");
  const list = BRAND_KEYS.filter((k) => k.includes(q.trim().toLowerCase().replace(/\s+/g, "")));
  return (
    <div className="space-y-3">
      <Input size="sm" icon={<Search size={14} />} placeholder="Search icons" value={q} onChange={(e) => setQ(e.target.value)} aria-label="Search icons" />
      <div className="grid max-h-56 grid-cols-7 gap-1.5 overflow-y-auto pr-1 scroll-thin sm:grid-cols-9">
        {list.map((k) => (
          <button type="button" key={k} title={k} aria-label={k} aria-pressed={value === k} onClick={() => onPick(k)}
            className={cx("cv-press flex h-10 items-center justify-center rounded-md border transition-colors",
              value === k ? "border-pink-500 bg-pink-50" : "border-transparent hover:border-line hover:bg-surface-hover")}>
            <BrandIcon name={k} size={20} />
          </button>
        ))}
        {!list.length && <p className="col-span-full py-6 text-center text-sm text-fg-3">No icon matches "{q}".</p>}
      </div>
    </div>
  );
}

/** Config > Name and Config > Icon: both are staged with the service info call. */
export function UpdateInfo({ service, focus = "name", onClose }: { service: Service; focus?: "name" | "icon"; onClose: () => void }) {
  const { eid, setCanvas, reload } = useProject();
  const cfg = service.staged || service.config;
  const [name, setName] = useState(cfg.name);
  const [icon, setIcon] = useState(cfg.icon);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>();
  const save = async () => {
    if (!name.trim()) { setError(new Error("Give the service a name.")); return; }
    setBusy(true); setError(undefined);
    const body = { name: name.trim() !== cfg.name ? name.trim() : undefined, icon: icon !== cfg.icon ? icon : undefined };
    if (!body.name && !body.icon) { onClose(); return; }
    try {
      const s = await api.updateInfo(eid, service.id, body);
      setCanvas((c) => ({ ...c, services: c.services.map((x) => (x.id === s.id ? s : x)) }));
      void reload();
      onClose();
    } catch (e) { setError(e); setBusy(false); }
  };
  return (
    <Modal title={focus === "icon" ? "Service icon" : "Service name"} onClose={onClose} width={520}
      footer={<><Button variant="ghost" onClick={onClose}>Cancel</Button><Button variant="primary" busy={busy} onClick={() => void save()}>Save</Button></>}>
      <form className="space-y-5 pb-1" onSubmit={(e) => { e.preventDefault(); void save(); }}>
        {focus === "name" ? (
          <Field label="Name" hint="Renames are staged. References in other variables are rewritten.">
            <Input autoFocus value={name} maxLength={32} onChange={(e) => setName(e.target.value)} onFocus={(e) => e.currentTarget.select()} />
          </Field>
        ) : (
          <div className="space-y-3">
            <div className="flex items-center gap-3 text-sm text-fg-2"><span className="flex h-10 w-10 items-center justify-center rounded-lg border border-line-popover bg-surface-hover"><ServiceIcon icon={icon} size={20} /></span>{cfg.name}</div>
            <IconGrid value={icon} onPick={setIcon} />
          </div>
        )}
        <ErrorLine error={error} />
      </form>
    </Modal>
  );
}

/** Config > Start command and Config > Healthcheck path: a one-field dialog that stages the change. */
export function ConfigField({ service, field, onClose }: { service: Service; field: "startCommand" | "healthcheckPath"; onClose: () => void }) {
  const { eid, reload } = useProject();
  const cfg = service.staged || service.config;
  const initial = (field === "startCommand" ? cfg.deploy.startCommand : cfg.deploy.healthcheckPath) || "";
  const [v, setV] = useState(initial);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>();
  const save = async () => {
    if (v === initial) { onClose(); return; }
    setBusy(true); setError(undefined);
    const body: UpdateServiceConfigRequest = { patch: { deploy: { [field]: v.trim() || null } } };
    try {
      await api.updateConfig(eid, service.id, body);
      toast(`${field === "startCommand" ? "Start command" : "Healthcheck path"} staged for ${cfg.name}`);
      void reload();
      onClose();
    } catch (e) { setError(e); setBusy(false); }
  };
  const label = field === "startCommand" ? "Start command" : "Healthcheck path";
  return (
    <Modal title={label} onClose={onClose} width={480}
      description={field === "startCommand" ? "Runs instead of the image or builder default." : "The service only receives traffic once this path answers 200."}
      footer={<><Button variant="ghost" onClick={onClose}>Cancel</Button><Button variant="primary" busy={busy} onClick={() => void save()}>Stage change</Button></>}>
      <form className="space-y-3 pb-1" onSubmit={(e) => { e.preventDefault(); void save(); }}>
        <Input autoFocus mono value={v} placeholder={field === "startCommand" ? "npm run start" : "/health"} onChange={(e) => setV(e.target.value)} aria-label={label} />
        <ErrorLine error={error} />
      </form>
    </Modal>
  );
}

/** Double-click on a group header: an inline rename chip over a dimmed canvas. */
export function RenameChip({ rect, value, onSave, onClose }: { rect: DOMRect; value: string; onSave: (v: string) => void; onClose: () => void }) {
  const ref = useRef<HTMLInputElement>(null);
  const [v, setV] = useState(value);
  useLayoutEffect(() => { ref.current?.focus(); ref.current?.select(); }, []);
  const w = Math.max(360, Math.min(560, rect.width + 160));
  const left = Math.max(8, Math.min(rect.left + rect.width / 2 - w / 2, window.innerWidth - w - 8));
  const top = Math.max(60, rect.top - 64);
  return createPortal(
    <div className="cv-dim fixed inset-0 z-[150] bg-overlay" onMouseDown={(e) => { if (e.target === e.currentTarget) onClose(); }}>
      <div style={{ left, top: top - 34, width: w }} className="absolute animate-rise-in">
        <span className="mb-3 inline-block rounded bg-surface-raised px-2 py-1 text-sm font-medium text-foreground">Rename</span>
        <input ref={ref} value={v} maxLength={48} aria-label="Group name" onChange={(e) => setV(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Escape") { e.preventDefault(); e.stopPropagation(); onClose(); }
            if (e.key === "Enter") { e.preventDefault(); if (v.trim()) onSave(v.trim()); onClose(); }
          }}
          className="block h-14 w-full rounded-md bg-surface-raised px-6 text-base text-foreground shadow-modal outline-none placeholder:text-fg-4 focus-visible:shadow-modal" />
      </div>
    </div>,
    document.body,
  );
}

/** Group > Update Icon. */
export function GroupIconDialog({ value, onSave, onClose }: { value?: string | null; onSave: (icon: string | undefined) => void; onClose: () => void }) {
  const [icon, setIcon] = useState<string | undefined>(value || undefined);
  return (
    <Modal title="Group icon" onClose={onClose} width={520}
      footer={<>
        {value && <Button variant="ghost" className="mr-auto" onClick={() => { onSave(undefined); onClose(); }}>Use the first service's icon</Button>}
        <Button variant="ghost" onClick={onClose}>Cancel</Button>
        <Button variant="primary" onClick={() => { onSave(icon); onClose(); }}>Save</Button>
      </>}>
      <div className="pb-1"><IconGrid value={icon} onPick={setIcon} /></div>
    </Modal>
  );
}
