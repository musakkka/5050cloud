// SPDX-License-Identifier: AGPL-3.0-only

// The bottom-left canvas toolbar: four 36px boxes 8px apart (canvas options, zoom in / out /
// fit, undo / redo, visibility layers), each a bordered secondary surface with 34px buttons.
import { useState, type ReactNode } from "react";
import { Expand, Grip, LayoutGrid, Layers, Minus, Network, Plus, Redo2, RotateCcw, Spline, Undo2, Wrench } from "lucide-react";
import { Tooltip, cx, isMac } from "../../kit";
import { CtxMenu } from "./CtxMenu";

export type Layer = "traffic" | "references";

function Btn({ label, onClick, disabled, active, children, kbd }: { label: string; onClick: (el: HTMLElement) => void; disabled?: boolean; active?: boolean; children: ReactNode; kbd?: string }) {
  return (
    <Tooltip content={kbd ? <span className="flex items-center gap-2">{label}<span className="text-fg-3">{kbd}</span></span> : label} side="right">
      <button type="button" aria-label={label} disabled={disabled} aria-pressed={active}
        onClick={(e) => onClick(e.currentTarget)}
        className={cx("cv-press flex h-[34px] w-[34px] items-center justify-center rounded-md text-fg-3 hover:bg-tint-pill hover:text-foreground disabled:pointer-events-none disabled:opacity-40",
          active && "bg-tint-pill text-foreground")}>
        {children}
      </button>
    </Tooltip>
  );
}
const Box = ({ children, label }: { children: ReactNode; label?: string }) => (
  <div role={label ? "group" : undefined} aria-label={label} className="flex w-9 flex-col items-center rounded-md border border-line bg-secondaryBg">{children}</div>
);

export function Toolbar({ editable, canUndo, canRedo, showEdges, layer, onZoomIn, onZoomOut, onFit, onUndo, onRedo, onToggleEdges, onAutoLayout, onRepair, onReset, onLayer }: {
  editable: boolean; canUndo: boolean; canRedo: boolean; showEdges: boolean; layer: Layer;
  onZoomIn: () => void; onZoomOut: () => void; onFit: () => void; onUndo: () => void; onRedo: () => void;
  onToggleEdges: () => void; onAutoLayout: () => void; onRepair: () => void; onReset: () => void; onLayer: (l: Layer) => void;
}) {
  const [menu, setMenu] = useState<{ kind: "options" | "layers"; x: number; y: number }>();
  const mod = isMac() ? "⌘" : "Ctrl+";
  return (
    <>
      <div role="toolbar" aria-label="Canvas options" className="absolute bottom-4 left-4 z-20 flex flex-col gap-2" onDoubleClick={(e) => e.stopPropagation()}>
        <Box>
          <Btn label="Canvas options" active={menu?.kind === "options"} onClick={(el) => { const r = el.parentElement!.getBoundingClientRect(); setMenu({ kind: "options", x: r.right + 8, y: r.top - 1 }); }}><Grip size={16} /></Btn>
        </Box>
        <Box label="Canvas actions">
          <Btn label="Zoom in" onClick={onZoomIn}><Plus size={16} /></Btn>
          <Btn label="Zoom out" onClick={onZoomOut}><Minus size={16} /></Btn>
          <Btn label="Fit view" onClick={onFit}><Expand size={16} /></Btn>
        </Box>
        {editable && (
          <Box label="History">
            <Btn label="Undo" kbd={`${mod}Z`} disabled={!canUndo} onClick={onUndo}><Undo2 size={16} /></Btn>
            <Btn label="Redo" kbd={`${mod}⇧Z`} disabled={!canRedo} onClick={onRedo}><Redo2 size={16} /></Btn>
          </Box>
        )}
        <Box>
          <Btn label="Layers" active={menu?.kind === "layers"} onClick={(el) => { const r = el.parentElement!.getBoundingClientRect(); setMenu({ kind: "layers", x: r.right + 8, y: r.bottom + 1 }); }}><Layers size={16} /></Btn>
        </Box>
      </div>
      {menu?.kind === "options" && (
        <CtxMenu x={menu.x} y={menu.y} rowH={36} pad={4} onClose={() => setMenu(undefined)} items={[
          { label: showEdges ? "Hide Connections" : "Show Connections", icon: <Spline />, onClick: onToggleEdges },
          ...(editable ? [
            { label: "Auto Layout", icon: <LayoutGrid />, onClick: onAutoLayout },
            { label: "Repair Overlaps", icon: <Wrench />, onClick: onRepair },
            { label: "Reset Canvas", icon: <RotateCcw />, onClick: onReset },
          ] : []),
        ]} />
      )}
      {menu?.kind === "layers" && (
        <CtxMenu x={menu.x} y={menu.y} up width={240} rowH={56} pad={4} onClose={() => setMenu(undefined)} items={[
          { label: "Network Traffic", description: "Show traffic between services", icon: <Network />, selected: layer === "traffic", onClick: () => onLayer("traffic") },
          { label: "Variable References", description: "Show variable connections", icon: <Spline />, selected: layer === "references", onClick: () => onLayer("references") },
        ]} />
      )}
    </>
  );
}
