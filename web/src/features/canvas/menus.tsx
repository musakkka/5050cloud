// SPDX-License-Identifier: AGPL-3.0-only

// The New menu (SPEC 13.2), the right-click menus and the Update Info dialog.
import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { useNavigate } from "react-router-dom";
import { api, type Service } from "../../api/client";
import { useProject } from "../../app/project";
import { Button, ErrorLine, Field, Input, Modal, ServiceIcon, cx, toastError } from "../../kit";
import { ComposeFlow, DatabaseFlow, GitHubFlow, ImageFlow, VolumeFlow, createEmptyService, type Target } from "../new/flows";

export type NewKind = "github" | "database" | "template" | "image" | "empty" | "volume" | "compose";

export const NEW_MENU: { id: NewKind | "bucket" | "function"; label: string; disabled?: string }[] = [
  { id: "github", label: "GitHub Repo" },
  { id: "database", label: "Database" },
  { id: "template", label: "Template" },
  { id: "image", label: "Docker Image" },
  { id: "empty", label: "Empty Service" },
  { id: "volume", label: "Volume" },
  { id: "bucket", label: "Bucket", disabled: "Not in v0.1: run MinIO from the Template menu" },
  { id: "function", label: "Function", disabled: "Not in v0.1: deploy a small service from a Docker image" },
];

/** Window events let the palette drive the canvas without importing it. */
export const CANVAS_EVENT = "5050:canvas";
export type CanvasCommand =
  | { kind: "new"; what: NewKind }
  | { kind: "toggle-connections" }
  | { kind: "tidy" }
  | { kind: "update-info"; serviceId: string };
export const sendCanvas = (c: CanvasCommand) => window.dispatchEvent(new CustomEvent<CanvasCommand>(CANVAS_EVENT, { detail: c }));

/** A menu at a screen point (right-click). */
export function PointMenu({ x, y, onClose, children }: { x: number; y: number; onClose: () => void; children: ReactNode }) {
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const down = (e: MouseEvent) => { if (!ref.current?.contains(e.target as Node)) onClose(); };
    const key = (e: KeyboardEvent) => { if (e.key === "Escape") onClose(); };
    document.addEventListener("mousedown", down);
    document.addEventListener("keydown", key);
    return () => { document.removeEventListener("mousedown", down); document.removeEventListener("keydown", key); };
  }, [onClose]);
  const left = Math.min(x, window.innerWidth - 220), top = Math.min(y, window.innerHeight - 320);
  return createPortal(
    <div ref={ref} role="menu" style={{ left, top }} onContextMenu={(e) => e.preventDefault()}
      className="fixed z-[200] min-w-[200px] rounded-lg border border-gray-200 bg-secondaryBg py-1.5 shadow-[0_10px_30px_rgba(0,0,0,.45)]">
      {children}
    </div>,
    document.body,
  );
}

export function Item({ children, onClick, danger, disabled, title }: { children: ReactNode; onClick?: () => void; danger?: boolean; disabled?: boolean; title?: string }) {
  return (
    <button type="button" role="menuitem" title={title} disabled={disabled} onClick={onClick}
      className={cx("flex w-full items-center gap-2 px-3 py-1.5 text-left text-sm hover:bg-gray-100 disabled:cursor-not-allowed disabled:opacity-40", danger ? "text-red-600" : "text-gray-800")}>
      {children}
    </button>
  );
}
export const Sep = () => <div className="my-1 border-t border-gray-200" />;

export function NewMenuItems({ onPick }: { onPick: (k: NewKind) => void }) {
  return <>{NEW_MENU.map((m) => <Item key={m.id} disabled={!!m.disabled} title={m.disabled} onClick={() => !m.disabled && onPick(m.id as NewKind)}>{m.label}</Item>)}</>;
}

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

/** A short list of devicon names the icon picker offers; the server serves them under /icons/. */
export const ICONS = ["nodejs", "python", "go", "rust", "ruby", "php", "java", "kotlin", "dotnet", "elixir", "deno", "typescript", "javascript",
  "react", "nextjs", "vuejs", "nuxtjs", "svelte", "angular", "astro", "django", "flask", "fastapi", "rails", "laravel", "spring", "express",
  "docker", "nginx", "caddy", "postgresql", "mysql", "redis", "mongodb", "rabbitmq", "kafka", "elasticsearch", "grafana", "prometheus", "minio", "ghost", "wordpress"];

export function UpdateInfo({ service, onClose }: { service: Service; onClose: () => void }) {
  const { eid, setCanvas, reload } = useProject();
  const cfg = service.staged || service.config;
  const [name, setName] = useState(cfg.name);
  const [icon, setIcon] = useState(cfg.icon);
  const [q, setQ] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>();
  const list = ICONS.filter((i) => i.includes(q.trim().toLowerCase()));
  const save = async () => {
    setBusy(true); setError(undefined);
    const body = { name: name !== cfg.name ? name.trim() : undefined, icon: icon !== cfg.icon ? icon : undefined };
    try {
      const s = await api.updateInfo(eid, service.id, body);
      setCanvas((c) => ({ ...c, services: c.services.map((x) => (x.id === s.id ? s : x)) }));
      void reload();
      onClose();
    } catch (e) { setError(e); setBusy(false); }
  };
  return (
    <Modal title="Update service info" onClose={onClose} width={520}>
      <form className="space-y-4 pb-2" onSubmit={(e) => { e.preventDefault(); void save(); }}>
        <Field label="Name" hint="A rename is staged; references to it are rewritten."><Input autoFocus value={name} maxLength={32} onChange={(e) => setName(e.target.value)} /></Field>
        <Field label="Icon">
          <Input placeholder="Search icons" value={q} onChange={(e) => setQ(e.target.value)} />
        </Field>
        <div className="grid max-h-48 grid-cols-6 gap-2 overflow-y-auto sm:grid-cols-8 scroll-thin">
          {list.map((i) => (
            <button type="button" key={i} title={i} aria-label={i} aria-pressed={icon === i} onClick={() => setIcon(i)}
              className={cx("flex h-11 items-center justify-center rounded-md border", icon === i ? "border-pink-500 bg-pink-100" : "border-gray-200 hover:border-gray-400")}>
              <ServiceIcon icon={i} size={22} />
            </button>
          ))}
        </div>
        <ErrorLine error={error} />
        <div className="flex justify-end gap-2 py-2"><Button onClick={onClose}>Cancel</Button><Button type="submit" variant="primary" busy={busy}>Save</Button></div>
      </form>
    </Modal>
  );
}
