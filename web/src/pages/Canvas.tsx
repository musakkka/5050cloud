// SPDX-License-Identifier: AGPL-3.0-only

import { memo, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import {
  Background, BackgroundVariant, BaseEdge, Handle, MarkerType, Position, ReactFlow, ReactFlowProvider, getSmoothStepPath, useInternalNode, useReactFlow,
  type Edge, type EdgeProps, type InternalNode, type Node, type NodeChange, type NodeProps, type ReactFlowInstance,
} from "@xyflow/react";
import { Clock, HardDrive, LayoutGrid, Maximize, Minus, Plus } from "lucide-react";
import { api, type CanvasNode } from "../api";
import { ResourceIcon, STATUS_COLOR, Spinner, StatusGlyph, cx, timeAgo } from "../ui/kit";
import { useProject } from "./ProjectLayout";
import ServicePanel from "./ServicePanel";
import { GRID, H, VOL, W, autoLayout, frames, nodeH, resolvePositions, snap, type Frame, type Pos } from "./canvasLayout";

type SvcData = CanvasNode & { selected: boolean; dim: boolean } & Record<string, unknown>;

function subline(d: CanvasNode) {
  switch (d.status) {
    case "online": return d.since ? `${timeAgo(d.since)} via ${d.kind === "service" && d.source !== "image" ? "GitHub" : "Docker Image"}` : "Online";
    case "deploying": return "Deploying";
    case "crashed": return d.since ? `Crashed ${timeAgo(d.since)}` : "Crashed";
    case "unhealthy": return "Health check failing";
    default: return "No active deployment";
  }
}

const ServiceNode = memo(({ data: d }: NodeProps<Node<SvcData>>) => (
  <div style={{ width: W }} className={cx("transition-opacity duration-150", d.dim && "opacity-40")}>
    {/* React Flow only draws edges between nodes with handles; the floating edge picks the real sides */}
    <Handle type="target" position={Position.Left} /><Handle type="source" position={Position.Right} />
    <div style={{ height: H }}
      className={cx("relative z-[1] flex flex-col justify-between rounded-xl border bg-secondaryBg p-6 transition-[border-color,box-shadow] duration-150",
        d.selected ? "border-pink-500 shadow-[0_0_0_2px_var(--pink-500)]" : "border-gray-200 hover:border-gray-400")}>
      <div className="flex items-start gap-3 min-w-0">
        <ResourceIcon icon={d.icon} kind={d.kind} source={d.source} size={24} />
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2">
            <span className="truncate text-base font-semibold leading-6">{d.name}</span>
            {d.svcKind === "cron" && <Clock size={14} className="shrink-0 text-gray-500" />}
          </div>
          {d.domain && <div className="truncate text-sm leading-5 text-gray-700">{d.domain}</div>}
        </div>
      </div>
      <div className={cx("flex items-center gap-2 text-sm leading-5", d.status === "online" ? "text-gray-600" : STATUS_COLOR[d.status])}>
        <StatusGlyph status={d.status} size={18} />
        <span className="truncate">{subline(d)}</span>
      </div>
    </div>
    {d.volume && (
      <div style={{ height: VOL + 12 }} className="-mt-3 flex items-end gap-2 rounded-b-xl border border-t-0 border-gray-200 bg-gray-50 px-6 pb-[11px] text-sm text-gray-500">
        <HardDrive size={14} className="mb-[3px] shrink-0" /><span className="truncate">{d.volume}</span>
      </div>
    )}
  </div>
));

type GroupData = Frame & Record<string, unknown>;
const GroupNode = memo(({ data }: NodeProps<Node<GroupData>>) => (
  <div style={{ width: data.w, height: data.h }} className={cx("rounded-2xl border border-dashed", data.depth ? "border-gray-300/80 bg-white/[0.02]" : "border-gray-300 bg-white/[0.015]")}>
    <div className="flex items-center gap-2 px-5 pt-3.5 text-sm font-medium text-gray-600">
      {data.icon && <img src={data.icon} alt="" className="h-4 w-4" />}{data.name}
    </div>
  </div>
));

const nodeTypes = { svc: ServiceNode, grp: GroupNode };

/** Joins the nearest sides of two cards with a dashed connector. */
function sides(s: InternalNode, t: InternalNode) {
  const b = (n: InternalNode) => ({ x: n.internals.positionAbsolute.x, y: n.internals.positionAbsolute.y, w: n.measured.width ?? W, h: Math.min(n.measured.height ?? H, H) });
  const a = b(s), c = b(t);
  if (c.x >= a.x + a.w) return { sx: a.x + a.w, sy: a.y + a.h / 2, sp: Position.Right, tx: c.x, ty: c.y + c.h / 2, tp: Position.Left };
  if (c.x + c.w <= a.x) return { sx: a.x, sy: a.y + a.h / 2, sp: Position.Left, tx: c.x + c.w, ty: c.y + c.h / 2, tp: Position.Right };
  if (c.y >= a.y) return { sx: a.x + a.w / 2, sy: a.y + (s.measured.height ?? H), sp: Position.Bottom, tx: c.x + c.w / 2, ty: c.y, tp: Position.Top };
  return { sx: a.x + a.w / 2, sy: a.y, sp: Position.Top, tx: c.x + c.w / 2, ty: c.y + (t.measured.height ?? H), tp: Position.Bottom };
}

function FloatingEdge({ id, source, target, markerEnd, data }: EdgeProps<Edge<{ hl: boolean; dim: boolean }>>) {
  const s = useInternalNode(source);
  const t = useInternalNode(target);
  if (!s || !t) return null;
  const g = sides(s, t);
  const [path] = getSmoothStepPath({ sourceX: g.sx, sourceY: g.sy, sourcePosition: g.sp, targetX: g.tx, targetY: g.ty, targetPosition: g.tp, borderRadius: 14, offset: 28 });
  return <BaseEdge id={id} path={path} markerEnd={markerEnd} className={cx(data?.hl && "edge-hl", data?.dim && "edge-dim")} />;
}

const edgeTypes = { float: FloatingEdge };

function Flow() {
  const { canvas, pid, env, withEnv } = useProject();
  const { target = null } = useParams();
  const nav = useNavigate();
  const rf = useReactFlow();
  const [hover, setHover] = useState<string | null>(null);
  const scope = canvas ? `${canvas.project.id}/${canvas.env}` : "";

  // where cards are: the server's saved layout, then local drags on top
  const [saved, setSaved] = useState<Pos>({});
  const savedRef = useRef(saved);
  savedRef.current = saved;
  const loaded = useRef("");
  useEffect(() => {
    if (canvas && loaded.current !== scope) { loaded.current = scope; setSaved(canvas.positions || {}); }
  }, [canvas, scope]);

  const auto = useMemo(() => (canvas ? autoLayout(canvas.nodes, canvas.groups) : {}), [canvas]);
  const pos = useMemo(() => (canvas ? resolvePositions(canvas.nodes, auto, loaded.current === scope ? saved : canvas.positions || {}) : {}), [canvas, auto, saved, scope]);
  const posRef = useRef(pos);
  posRef.current = pos;

  const focus = hover || target;
  const linked = useMemo(() => {
    const s = new Set<string>();
    if (focus) { s.add(focus); for (const e of canvas?.edges || []) { if (e.source === focus) s.add(e.target); if (e.target === focus) s.add(e.source); } }
    return s;
  }, [canvas, focus]);

  const nodes: Node[] = useMemo(() => {
    if (!canvas) return [];
    const grp = frames(canvas.nodes, canvas.groups, pos).map((f) => ({
      id: `g:${f.id}`, type: "grp", position: { x: f.x, y: f.y }, width: f.w, height: f.h, data: { ...f },
      draggable: false, selectable: false, focusable: false, zIndex: -1 + f.depth * 0.01,
    } as Node));
    const svc = canvas.nodes.map((n) => ({
      id: n.id, type: "svc", position: { x: pos[n.id][0], y: pos[n.id][1] }, width: W, height: nodeH(n),
      data: { ...n, selected: n.id === target, dim: !!hover && !linked.has(n.id) }, selectable: false,
    } as Node));
    return [...grp, ...svc];
  }, [canvas, pos, target, hover, linked]);

  const edges: Edge[] = useMemo(() => (canvas?.edges || []).map((e) => {
    const hl = !!focus && (e.source === focus || e.target === focus);
    return {
      id: `${e.source}->${e.target}`, source: e.source, target: e.target, type: "float", data: { hl, dim: !!focus && !hl }, zIndex: hl ? 1 : 0,
      markerEnd: { type: MarkerType.ArrowClosed, width: 16, height: 16, color: hl ? "hsl(270,70%,65%)" : "hsl(246,7%,45%)" },
    };
  }), [canvas, focus]);

  const onNodesChange = useCallback((changes: NodeChange[]) => {
    const moved: Pos = {};
    for (const c of changes) if (c.type === "position" && c.position && !c.id.startsWith("g:")) moved[c.id] = [c.position.x, c.position.y];
    if (!Object.keys(moved).length) return;
    // the first drag pins the whole current layout, so later automatic changes can't shuffle it
    setSaved((prev) => ({ ...(Object.keys(prev).length ? prev : posRef.current), ...moved }));
  }, []);
  const persist = useCallback(() => {
    const snapped = Object.fromEntries(Object.entries(savedRef.current).map(([k, [x, y]]) => [k, [snap(x), snap(y)] as [number, number]]));
    setSaved(snapped);
    api.savePositions(pid, env, snapped).catch(() => { /* the next drag tries again */ });
  }, [pid, env]);
  const tidy = () => { setSaved({}); api.savePositions(pid, env, {}).catch(() => {}); setTimeout(() => fitAll(), 50); };

  const fitAll = () => rf.fitView({ padding: 0.08, maxZoom: 1, duration: 250 });
  const vpKey = `cloud.vp.${scope}`;
  const savedVp = (() => { try { return JSON.parse(localStorage.getItem(vpKey) || "null"); } catch { return null; } })();
  const box = useRef<HTMLDivElement>(null);
  // first visit: fit everything if it stays readable, otherwise start at a readable zoom on the top left
  const onInit = (inst: ReactFlowInstance) => {
    if (savedVp || !canvas || !box.current) return;
    const ps = canvas.nodes.map((n) => pos[n.id]);
    if (!ps.length) return;
    const x0 = Math.min(...ps.map((p) => p[0])) - 48, y0 = Math.min(...ps.map((p) => p[1])) - 72;
    const x1 = Math.max(...ps.map((p) => p[0] + W)) + 48, y1 = Math.max(...ps.map((p) => p[1] + H + VOL)) + 48;
    const { clientWidth: cw, clientHeight: ch } = box.current;
    const fit = Math.min(cw / (x1 - x0), ch / (y1 - y0), 1);
    if (fit >= 0.7) inst.setViewport({ zoom: fit, x: (cw - (x1 - x0) * fit) / 2 - x0 * fit, y: (ch - (y1 - y0) * fit) / 2 - y0 * fit });
    else inst.setViewport({ zoom: 0.8, x: 72 - x0 * 0.8, y: 24 - y0 * 0.8 });   // clear of the zoom controls
  };

  if (!canvas) return <div className="absolute inset-0 flex items-center justify-center dot-grid"><Spinner /></div>;
  return (
    <div ref={box} className="absolute inset-0">
      <ReactFlow key={vpKey} defaultViewport={savedVp || undefined} onInit={onInit}
        onMoveEnd={(_, vp) => { try { localStorage.setItem(vpKey, JSON.stringify(vp)); } catch { /* ignore */ } }}
        nodes={nodes} edges={edges} nodeTypes={nodeTypes} edgeTypes={edgeTypes} onNodesChange={onNodesChange} onNodeDragStop={persist}
        snapToGrid snapGrid={[GRID, GRID]} minZoom={0.15} maxZoom={1.6} proOptions={{ hideAttribution: true }}
        onNodeClick={(_, n) => (n.type === "svc" ? nav(withEnv(`/project/${pid}/service/${encodeURIComponent(n.id)}`)) : target && nav(withEnv(`/project/${pid}`)))}
        onNodeMouseEnter={(_, n) => n.type === "svc" && setHover(n.id)} onNodeMouseLeave={() => setHover(null)}
        onPaneClick={() => target && nav(withEnv(`/project/${pid}`))}
        nodesConnectable={false} elementsSelectable={false} panOnScroll zoomOnPinch nodeDragThreshold={4}>
        <Background variant={BackgroundVariant.Dots} gap={GRID} size={1.6} color="rgba(255,255,255,.1)" bgColor="var(--background)" />
      </ReactFlow>
      <div className="absolute left-4 top-4 z-10 flex flex-col gap-2">
        <div className="flex flex-col overflow-hidden rounded-lg border border-gray-200 bg-secondaryBg">
          <CtlBtn title="Zoom in" onClick={() => rf.zoomIn({ duration: 150 })}><Plus size={14} /></CtlBtn>
          <CtlBtn title="Zoom out" onClick={() => rf.zoomOut({ duration: 150 })}><Minus size={14} /></CtlBtn>
          <CtlBtn title="Fit to screen" onClick={fitAll}><Maximize size={13} /></CtlBtn>
        </div>
        <div className="flex flex-col overflow-hidden rounded-lg border border-gray-200 bg-secondaryBg">
          <CtlBtn title={Object.keys(saved).length ? "Tidy up (undo your arrangement)" : "Already tidy. Drag cards to arrange them."} onClick={tidy}><LayoutGrid size={14} /></CtlBtn>
        </div>
      </div>
      {canvas.nodes.length === 0 && <div className="absolute inset-0 flex items-center justify-center text-sm text-gray-500">Nothing runs in {canvas.env}.</div>}
    </div>
  );
}

function CtlBtn({ children, onClick, title }: { children: React.ReactNode; onClick: () => void; title: string }) {
  return <button title={title} onClick={onClick} className="flex h-9 w-9 items-center justify-center border-b border-gray-200 text-gray-600 last:border-b-0 hover:bg-gray-100 hover:text-foreground">{children}</button>;
}

export default function CanvasPage() {
  const { target } = useParams();
  return (
    <ReactFlowProvider>
      <Flow />
      {target && <ServicePanel key={target} target={decodeURIComponent(target)} />}
    </ReactFlowProvider>
  );
}
