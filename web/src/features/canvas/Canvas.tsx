// SPDX-License-Identifier: AGPL-3.0-only

// The project canvas (SPEC 13.2): cards, references, groups, staged changes,
// right-click menus, undo and redo, presence, compose drop and a phone list.
import { memo, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useLocation, useNavigate } from "react-router-dom";
import {
  Background, BackgroundVariant, BaseEdge, Handle, MarkerType, Position, ReactFlow, ReactFlowProvider, getSmoothStepPath, useInternalNode, useReactFlow,
  type Edge, type EdgeProps, type InternalNode, type Node, type NodeChange, type NodeProps,
} from "@xyflow/react";
import { ChevronDown, ChevronRight, Clock, Globe, HardDrive, LayoutGrid, Maximize, Minus, Plus } from "lucide-react";
import { api, type Group as ApiGroup, type Service } from "../../api/client";
import { useProject } from "../../app/project";
import { canEdit, useMe } from "../../app/session";
import { Button, Confirm, ErrorLine, Field, Input, Modal, STATUS_TEXT, ServiceIcon, Spinner, StatusGlyph, TONE_TEXT, cx, fmtBytes, toastError, toneOf } from "../../kit";
import { ComposeFlow, type Target } from "../new/flows";
import { ChangesBanner, isTyping } from "./Changes";
import { History, groupSummary } from "./history";
import { GRID, H, VOL, W, autoLayout, frames, layoutInput, nodeH, resolvePositions, snap, type Frame, type Pos } from "./layout";
import { CANVAS_EVENT, Item, NewFlows, NewMenuItems, PointMenu, Sep, UpdateInfo, type CanvasCommand, type NewKind } from "./menus";

const pref = <T,>(k: string, d: T): T => { try { const v = localStorage.getItem(k); return v ? JSON.parse(v) : d; } catch { return d; } };
const setPref = (k: string, v: unknown) => { try { localStorage.setItem(k, JSON.stringify(v)); } catch { /* private window */ } };

/** The selected service id from /project/:pid/service/:sid/... */
export function useSelected(): string | undefined {
  const { pathname } = useLocation();
  const m = /\/project\/[^/]+\/service\/([^/]+)/.exec(pathname);
  return m ? decodeURIComponent(m[1]) : undefined;
}

export type Tag = "New" | "Edited" | "Removed" | undefined;
export function stagedTag(s: Service, syncTag?: string): Tag {
  if (syncTag) return syncTag === "new" ? "New" : syncTag === "removed" ? "Removed" : "Edited";
  switch (s.stagedAction) {
    case "create": return "New";
    case "delete": return "Removed";
    case "update": case "rename": return "Edited";
    default: return s.volume?.staged ? "Edited" : undefined;
  }
}

export function useNarrow() {
  const q = "(max-width: 639px)";
  const [narrow, setNarrow] = useState(() => typeof matchMedia !== "undefined" && matchMedia(q).matches);
  useEffect(() => {
    if (typeof matchMedia === "undefined") return;
    const m = matchMedia(q);
    const f = () => setNarrow(m.matches);
    m.addEventListener("change", f);
    return () => m.removeEventListener("change", f);
  }, []);
  return narrow;
}

interface CardProps { s: Service; tag: Tag; editable: boolean; onRestart: (s: Service) => void; onDomain: (s: Service) => void }
type CardData = CardProps & { selected: boolean; dim: boolean; onVolume: (s: Service) => void } & Record<string, unknown>;

function CardBody({ s, tag, editable, onRestart, onDomain }: CardProps) {
  const cfg = s.staged || s.config;
  const tone = toneOf(s.status);
  const web = s.listeningPorts.length > 0 && s.domains.length === 0 && s.kind !== "database" && s.kind !== "cron" && s.kind !== "worker";
  return (
    <>
      <div className="flex min-w-0 items-start gap-3">
        <ServiceIcon icon={cfg.icon} source={cfg.source.type} size={24} />
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2">
            <span className={cx("truncate text-base font-semibold leading-6", tag === "Removed" && "line-through")}>{cfg.name}</span>
            {s.kind === "cron" && <Clock size={14} className="shrink-0 text-gray-500" aria-label="Cron" />}
            {s.monitorFiring && <span className="h-2 w-2 shrink-0 rounded-full bg-red-500" title="A monitor is in alarm" />}
            {s.replicas.length > 1 && <span className="shrink-0 rounded border border-gray-200 px-1.5 text-[11px] text-gray-600" title="Replicas">x{s.replicas.length}</span>}
            {tag && <span className="ml-auto shrink-0 rounded bg-pink-100 px-1.5 text-[10px] font-semibold uppercase tracking-wider text-pink-600">{tag}</span>}
          </div>
          {s.domains[0] && <div className="truncate text-sm leading-5 text-gray-700">{s.domains[0].domain}</div>}
        </div>
      </div>
      <div className={cx("flex items-center gap-2 text-sm leading-5", TONE_TEXT[tone])}>
        <StatusGlyph status={s.status} size={18} />
        <span className="truncate">{s.statusText || STATUS_TEXT[s.status] || s.status}</span>
        {editable && s.status === "crashed" && s.activeDeployment && (
          <button type="button" className="nodrag ml-auto shrink-0 rounded border border-gray-200 px-2 text-xs text-foreground hover:border-gray-400" onClick={(e) => { e.stopPropagation(); onRestart(s); }}>Restart</button>
        )}
        {editable && web && s.status === "online" && (
          <button type="button" className="nodrag ml-auto inline-flex shrink-0 items-center gap-1 rounded border border-gray-200 px-2 text-xs text-foreground hover:border-gray-400" onClick={(e) => { e.stopPropagation(); onDomain(s); }}><Globe size={11} />Generate Domain</button>
        )}
      </div>
    </>
  );
}

const ServiceCard = memo(({ data: d }: NodeProps<Node<CardData>>) => (
  <div style={{ width: W }} className={cx("transition-opacity duration-150", d.dim && "opacity-40", d.tag === "Removed" && "opacity-50")} data-testid="service-card" data-name={(d.s.staged || d.s.config).name}>
    <Handle type="target" position={Position.Left} /><Handle type="source" position={Position.Right} />
    <div style={{ height: H }}
      className={cx("relative z-[1] flex flex-col justify-between rounded-xl border bg-secondaryBg p-6 transition-[border-color,box-shadow] duration-150",
        d.tag && d.tag !== "Removed" ? "staged-ring" : d.selected ? "border-pink-500 shadow-[0_0_0_2px_var(--pink-500)]" : "border-gray-200 hover:border-gray-400")}>
      <CardBody {...d} />
    </div>
    {d.s.volume && (
      <div style={{ height: VOL + 12 }} role="button" tabIndex={-1} onClick={(e) => { e.stopPropagation(); d.onVolume(d.s); }} className={cx("-mt-3 flex cursor-pointer items-end gap-2 rounded-b-xl border border-t-0 bg-gray-50 px-6 pb-[11px] text-sm text-gray-500", d.s.volume.staged ? "border-pink-500" : "border-gray-200")}>
        <HardDrive size={14} className="mb-[3px] shrink-0" /><span className="truncate">{d.s.volume.name || d.s.volume.mountPath}</span>
        <span className="ml-auto shrink-0 text-xs">{d.s.volume.usedBytes ? fmtBytes(d.s.volume.usedBytes) : d.s.volume.mountPath}</span>
      </div>
    )}
  </div>
));

type FrameData = Frame & { collapsed: boolean; summary: string; onToggle: (id: string) => void; onRename: (id: string) => void } & Record<string, unknown>;
const GroupFrame = memo(({ data }: NodeProps<Node<FrameData>>) => (
  <div style={{ width: data.w, height: data.collapsed ? H : data.h }} className={cx("rounded-2xl border border-dashed", data.depth ? "border-gray-300/80" : "border-gray-300", data.collapsed && "bg-secondaryBg")}>
    <div className="flex items-center gap-2 px-5 pt-3.5 text-sm font-medium text-gray-600">
      <button type="button" className="nodrag" aria-label={data.collapsed ? "Expand group" : "Collapse group"} onClick={(e) => { e.stopPropagation(); data.onToggle(data.id); }}>
        {data.collapsed ? <ChevronRight size={14} /> : <ChevronDown size={14} />}
      </button>
      {data.icon && <ServiceIcon icon={data.icon} size={16} />}
      <button type="button" className="nodrag truncate hover:text-foreground" title="Rename group" onClick={(e) => { e.stopPropagation(); data.onRename(data.id); }}>{data.name}</button>
    </div>
    {data.collapsed && <div className="px-5 pt-3 text-sm text-gray-600">{data.summary}</div>}
  </div>
));

const nodeTypes = { svc: ServiceCard, grp: GroupFrame };

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

const toPos = (p: Record<string, number[]>): Pos => Object.fromEntries(Object.entries(p).filter(([, v]) => v.length >= 2).map(([k, v]) => [k, [v[0], v[1]] as [number, number]]));

type Snapshot = { positions: Pos; groups: ApiGroup[] };
type Menu = { x: number; y: number; flow: { x: number; y: number }; service?: Service };

function Flow() {
  const me = useMe();
  const editable = canEdit(me);
  const { pid, eid, canvas, setCanvas, reload, withEnv } = useProject();
  const selected = useSelected();
  const nav = useNavigate();
  const rf = useReactFlow();
  const [hover, setHover] = useState<string | null>(null);
  const [menu, setMenu] = useState<Menu>();
  const [flow, setFlow] = useState<{ kind: NewKind; position?: number[]; serviceId?: string }>();
  const [info, setInfo] = useState<Service>();
  const [del, setDel] = useState<Service>();
  const [groupDlg, setGroupDlg] = useState<{ id?: string; seed?: string }>();
  const [compose, setCompose] = useState<string>();
  const [hideEdges, setHideEdges] = useState<boolean>(() => pref(`5050.edges.${pid}`, false));
  const [collapsed, setCollapsed] = useState<string[]>(() => pref(`5050.collapsed.${pid}`, []));
  const history = useRef(new History<Snapshot>());

  // positions: the server's, then local drags; remote moves (events) arrive through canvas.positions
  const [saved, setSaved] = useState<Pos>({});
  const savedRef = useRef(saved);
  savedRef.current = saved;
  const dragging = useRef(false);
  useEffect(() => { if (canvas && !dragging.current) setSaved(toPos(canvas.positions || {})); }, [canvas?.positions]); // eslint-disable-line react-hooks/exhaustive-deps

  const services = useMemo(() => canvas?.services || [], [canvas]);
  const groups = useMemo(() => canvas?.groups || [], [canvas]);
  const L = useMemo(() => layoutInput(services, groups), [services, groups]);
  const auto = useMemo(() => autoLayout(L.nodes, L.groups), [L]);
  const pos = useMemo(() => resolvePositions(L.nodes, auto, saved), [L, auto, saved]);
  const posRef = useRef(pos);
  posRef.current = pos;
  const [syncTags, setSyncTags] = useState<Record<string, string>>({});
  useEffect(() => {
    if (!canvas?.changesCount) { setSyncTags({}); return; }
    api.changes(eid).then((r) => setSyncTags(Object.fromEntries(r.changes.filter((c) => c.tag && c.serviceId).map((c) => [c.serviceId!, c.tag!]))), () => {});
  }, [eid, canvas?.changesCount]);

  const memberOf = useMemo(() => { const m: Record<string, string> = {}; for (const g of groups) for (const s of g.services) m[s] = g.id; return m; }, [groups]);
  const hidden = useMemo(() => new Set(services.filter((s) => memberOf[s.id] && collapsed.includes(memberOf[s.id])).map((s) => s.id)), [services, memberOf, collapsed]);

  const focus = hover || selected || null;
  const linked = useMemo(() => {
    const s = new Set<string>();
    if (focus) { s.add(focus); for (const e of canvas?.edges || []) { if (e.source === focus) s.add(e.target); if (e.target === focus) s.add(e.source); } }
    return s;
  }, [canvas, focus]);

  const restart = useCallback((s: Service) => {
    setCanvas((c) => ({ ...c, services: c.services.map((x) => (x.id === s.id ? { ...x, status: "deploying", statusText: "Restarting" } : x)) }));
    api.deploymentAction(s.activeDeployment!, "restart").catch((e) => { toastError(e); void reload(); });
  }, [setCanvas, reload]);
  const genDomain = useCallback((s: Service) => {
    api.generateDomain(eid, s.id, s.listeningPorts[0]).then(() => reload(), toastError);
  }, [eid, reload]);

  const openVolume = useCallback((s: Service) => { if (s.volume) nav(withEnv(`/project/${pid}/volume/${encodeURIComponent(s.volume.id)}`)); }, [nav, withEnv, pid]);

  const saveGroups = useCallback((next: ApiGroup[], record = true) => {
    const before = groups;
    if (record) history.current.push({ positions: savedRef.current, groups: before });
    setCanvas((c) => ({ ...c, groups: next }));
    api.saveGroups(eid, next).catch((e) => { setCanvas((c) => ({ ...c, groups: before })); toastError(e); });
  }, [groups, eid, setCanvas]);
  const savePositions = useCallback((p: Pos) => {
    setSaved(p);
    api.savePositions(eid, p).catch(() => { /* the next move tries again */ });
  }, [eid]);

  const toggleCollapse = useCallback((id: string) => setCollapsed((c) => { const n = c.includes(id) ? c.filter((x) => x !== id) : [...c, id]; setPref(`5050.collapsed.${pid}`, n); return n; }), [pid]);
  const renameGroup = useCallback((id: string) => setGroupDlg({ id }), []);

  const nodes: Node[] = useMemo(() => {
    const visible = L.nodes.filter((n) => !hidden.has(n.id));
    const fr = frames(L.nodes, L.groups, pos).map((f) => {
      const isCollapsed = collapsed.includes(f.id);
      const members = services.filter((s) => memberOf[s.id] === f.id);
      return {
        id: `g:${f.id}`, type: "grp", position: { x: f.x, y: f.y }, width: f.w, height: isCollapsed ? H : f.h,
        data: { ...f, collapsed: isCollapsed, summary: groupSummary(members.map((m) => m.status), (st) => STATUS_TEXT[st] || st), onToggle: toggleCollapse, onRename: renameGroup },
        draggable: false, selectable: false, focusable: false, zIndex: isCollapsed ? 2 : -1 + f.depth * 0.01,
      } as Node;
    });
    const byId = new Map(services.map((s) => [s.id, s]));
    const cards = visible.map((n) => {
      const s = byId.get(n.id)!;
      return {
        id: n.id, type: "svc", position: { x: pos[n.id][0], y: pos[n.id][1] }, width: W, height: nodeH(n), draggable: editable,
        data: { s, tag: stagedTag(s, syncTags[s.id]), selected: s.id === selected, dim: !!hover && !linked.has(s.id), editable, onRestart: restart, onDomain: genDomain, onVolume: openVolume },
        selectable: false,
      } as Node;
    });
    return [...fr, ...cards];
  }, [L, pos, services, hidden, collapsed, memberOf, selected, hover, linked, editable, restart, genDomain, openVolume, syncTags, toggleCollapse, renameGroup]);

  const edges: Edge[] = useMemo(() => (hideEdges ? [] : (canvas?.edges || []).filter((e) => !hidden.has(e.source) && !hidden.has(e.target)).map((e) => {
    const hl = !!focus && (e.source === focus || e.target === focus);
    return {
      id: `${e.source}->${e.target}`, source: e.source, target: e.target, type: "float", data: { hl, dim: !!focus && !hl }, zIndex: hl ? 1 : 0,
      markerEnd: { type: MarkerType.ArrowClosed, width: 16, height: 16, color: hl ? "var(--pink-600)" : "var(--gray-400)" },
    };
  })), [canvas, focus, hideEdges, hidden]);

  const dragStart = useRef<Snapshot>();
  const onNodesChange = useCallback((changes: NodeChange[]) => {
    const moved: Pos = {};
    for (const c of changes) if (c.type === "position" && c.position && !c.id.startsWith("g:")) moved[c.id] = [c.position.x, c.position.y];
    if (!Object.keys(moved).length) return;
    if (!dragging.current) { dragging.current = true; dragStart.current = { positions: { ...posRef.current }, groups }; }
    setSaved((prev) => ({ ...(Object.keys(prev).length ? prev : posRef.current), ...moved }));
  }, [groups]);

  const onDragStop = useCallback((_: unknown, node: Node) => {
    dragging.current = false;
    if (dragStart.current) history.current.push(dragStart.current);
    dragStart.current = undefined;
    const snapped: Pos = Object.fromEntries(Object.entries({ ...posRef.current, ...savedRef.current }).map(([k, [x, y]]) => [k, [snap(x), snap(y)] as [number, number]]));
    savePositions(snapped);
    // drag in and out of groups: the frame (computed without this card) under its centre wins
    if (node.type !== "svc") return;
    const others = L.nodes.filter((n) => n.id !== node.id);
    const fr = frames(others, L.groups, snapped);
    const [x, y] = snapped[node.id];
    const cx0 = x + W / 2, cy0 = y + H / 2;
    const inside = [...fr].reverse().find((f) => cx0 >= f.x && cx0 <= f.x + f.w && cy0 >= f.y && cy0 <= f.y + f.h);
    const cur = memberOf[node.id];
    const curFrame = fr.find((f) => f.id === cur);
    let next: string | undefined = cur;
    if (inside && inside.id !== cur) next = inside.id;
    else if (!inside && cur && curFrame) next = undefined;
    if (next === cur) return;
    saveGroups(groups.map((g) => ({ ...g, services: g.id === next ? [...g.services.filter((s) => s !== node.id), node.id] : g.services.filter((s) => s !== node.id) })), false);
  }, [L, memberOf, groups, saveGroups, savePositions]);

  const tidy = useCallback(() => {
    history.current.push({ positions: savedRef.current, groups });
    savePositions({});
    setTimeout(() => rf.fitView({ padding: 0.1, maxZoom: 1, duration: 250 }), 50);
  }, [groups, savePositions, rf]);
  const toggleEdges = useCallback(() => setHideEdges((h) => { setPref(`5050.edges.${pid}`, !h); return !h; }), [pid]);

  const restore = useCallback((s: Snapshot | undefined) => {
    if (!s) return;
    savePositions(s.positions);
    if (JSON.stringify(s.groups) !== JSON.stringify(groups)) saveGroups(s.groups, false);
  }, [groups, saveGroups, savePositions]);

  // keyboard: Cmd+Z / Cmd+Shift+Z
  useEffect(() => {
    const k = (e: KeyboardEvent) => {
      if (!(e.metaKey || e.ctrlKey) || e.key.toLowerCase() !== "z" || isTyping(e.target) || !editable) return;
      e.preventDefault();
      const cur = { positions: savedRef.current, groups };
      restore(e.shiftKey ? history.current.redo(cur) : history.current.undo(cur));
    };
    window.addEventListener("keydown", k);
    return () => window.removeEventListener("keydown", k);
  }, [groups, restore, editable]);

  // commands from the palette
  useEffect(() => {
    const h = (ev: Event) => {
      const c = (ev as CustomEvent<CanvasCommand>).detail;
      if (c.kind === "new") setFlow({ kind: c.what });
      if (c.kind === "toggle-connections") toggleEdges();
      if (c.kind === "tidy") tidy();
      if (c.kind === "update-info") { const s = services.find((x) => x.id === c.serviceId); if (s) setInfo(s); }
    };
    window.addEventListener(CANVAS_EVENT, h);
    return () => window.removeEventListener(CANVAS_EVENT, h);
  }, [services, tidy, toggleEdges]);

  // presence heartbeat
  useEffect(() => {
    if (!eid) return;
    const beat = () => void api.presence(eid).catch(() => {});
    beat();
    const t = setInterval(beat, 20000);
    return () => clearInterval(t);
  }, [eid]);

  const vpKey = `5050.vp.${pid}.${eid}`;
  const savedVp = useMemo(() => pref<{ x: number; y: number; zoom: number } | null>(vpKey, null), [vpKey]);
  // fit on the first load without a saved viewport, and when the first card lands on an empty canvas
  const lastCount = useRef<number>();
  useEffect(() => {
    if (!canvas) return;
    const n = services.length, prev = lastCount.current;
    lastCount.current = n;
    if (n && ((prev === undefined && !savedVp) || prev === 0)) setTimeout(() => rf.fitView({ padding: 0.12, maxZoom: 1, duration: prev === 0 ? 200 : 0 }), 50);
  }, [canvas, savedVp, services.length, rf]);

  const openMenu = (e: React.MouseEvent | MouseEvent, service?: Service) => {
    e.preventDefault();
    if (!editable && !service) return;
    setMenu({ x: e.clientX, y: e.clientY, flow: rf.screenToFlowPosition({ x: e.clientX, y: e.clientY }), service });
  };
  const flowTarget = useMemo<Target>(() => ({ canvas: true, ensure: async () => ({ pid, eid }), done: () => { void reload(); } }), [pid, eid, reload]);

  if (!canvas) return <div className="dot-grid absolute inset-0 flex items-center justify-center"><Spinner /></div>;
  const others = canvas.presence.filter((p) => p.userId !== me.user.id && Date.now() / 1000 - (p.lastSeen > 1e12 ? p.lastSeen / 1000 : p.lastSeen) < 90);

  return (
    <div className="absolute inset-0"
      onDragOver={(e) => { if (editable && e.dataTransfer.types.includes("Files")) e.preventDefault(); }}
      onDrop={(e) => {
        const f = e.dataTransfer.files?.[0];
        if (!f || !editable) return;
        e.preventDefault();
        if (!/\.ya?ml$/i.test(f.name)) { toastError(new Error("Drop a docker-compose.yml file to import it.")); return; }
        void f.text().then(setCompose);
      }}>
      <ReactFlow defaultViewport={savedVp || undefined}
        onMoveEnd={(_, vp) => setPref(vpKey, vp)}
        nodes={nodes} edges={edges} nodeTypes={nodeTypes} edgeTypes={edgeTypes} onNodesChange={onNodesChange} onNodeDragStop={onDragStop}
        snapToGrid snapGrid={[GRID, GRID]} minZoom={0.15} maxZoom={1.6} proOptions={{ hideAttribution: true }}
        onNodeClick={(_, n) => (n.type === "svc" ? nav(withEnv(`/project/${pid}/service/${encodeURIComponent(n.id)}`)) : undefined)}
        onNodeContextMenu={(e, n) => { if (n.type === "svc") openMenu(e, services.find((s) => s.id === n.id)); }}
        onPaneContextMenu={(e) => openMenu(e)}
        onNodeMouseEnter={(_, n) => n.type === "svc" && setHover(n.id)} onNodeMouseLeave={() => setHover(null)}
        onPaneClick={() => selected && nav(withEnv(`/project/${pid}`))}
        nodesConnectable={false} elementsSelectable={false} panOnScroll zoomOnPinch nodeDragThreshold={4}>
        <Background variant={BackgroundVariant.Dots} gap={GRID} size={1.6} color="var(--gray-300)" bgColor="var(--background)" />
      </ReactFlow>

      <div className="absolute left-4 top-4 z-10 flex flex-col gap-2">
        <div className="flex flex-col overflow-hidden rounded-lg border border-gray-200 bg-secondaryBg">
          <Ctl title="Zoom in" onClick={() => rf.zoomIn({ duration: 150 })}><Plus size={14} /></Ctl>
          <Ctl title="Zoom out" onClick={() => rf.zoomOut({ duration: 150 })}><Minus size={14} /></Ctl>
          <Ctl title="Fit to screen" onClick={() => rf.fitView({ padding: 0.1, maxZoom: 1, duration: 250 })}><Maximize size={13} /></Ctl>
        </div>
        {editable && <div className="flex flex-col overflow-hidden rounded-lg border border-gray-200 bg-secondaryBg"><Ctl title="Tidy" onClick={tidy}><LayoutGrid size={14} /></Ctl></div>}
      </div>

      <div className="absolute right-4 top-4 z-10 flex items-center gap-2">
        {others.length > 0 && (
          <div className="flex -space-x-2" aria-label="Also viewing">
            {others.slice(0, 5).map((p) => (
              <span key={p.userId} title={p.name} data-testid="presence-avatar" className="flex h-8 w-8 items-center justify-center rounded-full border-2 border-background bg-blue-500 text-xs font-semibold text-white">{(p.name || "?").slice(0, 1).toUpperCase()}</span>
            ))}
          </div>
        )}
        {editable && <Button variant="default" aria-label="+ Create" className="bg-secondaryBg" onClick={(e) => { const r = (e.currentTarget as HTMLElement).getBoundingClientRect(); setMenu({ x: r.right - 200, y: r.bottom + 4, flow: rf.screenToFlowPosition({ x: window.innerWidth / 2, y: window.innerHeight / 2 }) }); }}><Plus size={15} />Create</Button>}
      </div>

      {services.length === 0 && (
        <div className="pointer-events-none absolute inset-0 flex items-center justify-center">
          <div className="pointer-events-auto text-center">
            <p className="mb-3 text-sm text-gray-500">Nothing runs in {canvas.environment.name} yet.</p>
            {editable && <Button variant="primary" onClick={(e) => setMenu({ x: (e.currentTarget as HTMLElement).getBoundingClientRect().left, y: (e.currentTarget as HTMLElement).getBoundingClientRect().bottom + 4, flow: { x: 0, y: 0 } })}><Plus size={15} />Add a service</Button>}
          </div>
        </div>
      )}

      <ChangesBanner readOnly={!editable} />

      {menu && (
        <PointMenu x={menu.x} y={menu.y} onClose={() => setMenu(undefined)}>
          {menu.service ? (() => {
            const s = menu.service, close = () => setMenu(undefined);
            return <>
              <Item onClick={() => { close(); nav(withEnv(`/project/${pid}/service/${encodeURIComponent(s.id)}`)); }}>Open</Item>
              {editable && <>
                <Item onClick={() => { close(); api.duplicate(eid, s.id).then(() => reload(), toastError); }}>Duplicate</Item>
                <Item disabled={!!s.volume} onClick={() => { close(); setFlow({ kind: "volume", serviceId: s.id }); }}>Attach Volume</Item>
                <Item onClick={() => { close(); setInfo(s); }}>Update Info</Item>
                <Sep />
                {groups.map((g) => <Item key={g.id} onClick={() => { close(); saveGroups(groups.map((x) => ({ ...x, services: x.id === g.id ? [...x.services.filter((y) => y !== s.id), s.id] : x.services.filter((y) => y !== s.id) }))); }}>Add to {g.name}</Item>)}
                <Item onClick={() => { close(); setGroupDlg({ seed: s.id }); }}>Add to Group</Item>
                {memberOf[s.id] && <Item onClick={() => { close(); saveGroups(groups.map((x) => ({ ...x, services: x.services.filter((y) => y !== s.id) }))); }}>Remove from group</Item>}
                <Sep />
                <Item danger onClick={() => { close(); setDel(s); }}>Delete</Item>
              </>}
            </>;
          })() : <>
            <NewMenuItems onPick={(k) => { setMenu(undefined); setFlow({ kind: k, position: [snap(menu.flow.x), snap(menu.flow.y)] }); }} />
            <Sep />
            <Item onClick={() => { setMenu(undefined); setGroupDlg({}); }}>New Group</Item>
            <Item onClick={() => { setMenu(undefined); toggleEdges(); }}>{hideEdges ? "Show Connections" : "Hide Connections"}</Item>
            <Item onClick={() => { setMenu(undefined); tidy(); }}>Tidy</Item>
          </>}
        </PointMenu>
      )}

      {flow && <NewFlows open={flow.kind} position={flow.position} serviceId={flow.serviceId} onClose={() => setFlow(undefined)} />}
      {info && <UpdateInfo service={info} onClose={() => setInfo(undefined)} />}
      {compose !== undefined && <ComposeFlow target={flowTarget} initial={compose} onClose={() => setCompose(undefined)} />}
      {del && (
        <Confirm danger title={`Delete ${(del.staged || del.config).name}?`} confirm="Delete" typeName={(del.staged || del.config).name}
          body="The delete is staged. The service stops and its volume is removed after you deploy the change."
          onClose={() => setDel(undefined)}
          onConfirm={async () => { await api.deleteService(eid, del.id, (del.staged || del.config).name); await reload(); }} />
      )}
      {groupDlg && <GroupDialog id={groupDlg.id} seed={groupDlg.seed} groups={groups} services={services} onSave={(g) => saveGroups(g)} onClose={() => setGroupDlg(undefined)} />}
    </div>
  );
}

function GroupDialog({ id, seed, groups, services, onSave, onClose }: { id?: string; seed?: string; groups: ApiGroup[]; services: Service[]; onSave: (g: ApiGroup[]) => void; onClose: () => void }) {
  const existing = groups.find((g) => g.id === id);
  const [name, setName] = useState(existing?.name || `Group ${groups.length + 1}`);
  const [members, setMembers] = useState<string[]>(existing?.services || (seed ? [seed] : []));
  const [error, setError] = useState<string>();
  const save = () => {
    if (!name.trim()) { setError("Give the group a name."); return; }
    const gid = existing?.id || `grp_${Math.random().toString(36).slice(2, 10)}`;
    const strip = groups.map((g) => (g.id === gid ? g : { ...g, services: g.services.filter((s) => !members.includes(s)) }));
    const next = existing ? strip.map((g) => (g.id === gid ? { ...g, name: name.trim(), services: members } : g)) : [...strip, { id: gid, name: name.trim(), services: members }];
    onSave(next);
    onClose();
  };
  return (
    <Modal title={existing ? "Edit group" : "New group"} onClose={onClose} width={440}>
      <form className="space-y-4 pb-2" onSubmit={(e) => { e.preventDefault(); save(); }}>
        <Field label="Name"><Input autoFocus value={name} onChange={(e) => setName(e.target.value)} /></Field>
        <fieldset className="max-h-56 space-y-1 overflow-y-auto">
          <legend className="mb-1 text-sm font-medium">Services</legend>
          {services.map((s) => (
            <label key={s.id} className="flex items-center gap-2 text-sm">
              <input type="checkbox" checked={members.includes(s.id)} onChange={(e) => setMembers(e.target.checked ? [...members, s.id] : members.filter((x) => x !== s.id))} />
              {(s.staged || s.config).name}
            </label>
          ))}
        </fieldset>
        <ErrorLine error={error} />
        <div className="flex justify-end gap-2 py-2">
          {existing && <Button variant="ghost" className="mr-auto text-red-600" onClick={() => { onSave(groups.filter((g) => g.id !== existing.id)); onClose(); }}>Delete group</Button>}
          <Button onClick={onClose}>Cancel</Button><Button type="submit" variant="primary">Save</Button>
        </div>
      </form>
    </Modal>
  );
}

function Ctl({ children, onClick, title }: { children: React.ReactNode; onClick: () => void; title: string }) {
  return <button type="button" title={title} aria-label={title} onClick={onClick} className="flex h-9 w-9 items-center justify-center border-b border-gray-200 text-gray-600 last:border-b-0 hover:bg-gray-100 hover:text-foreground">{children}</button>;
}

/** Phones (SPEC 13.7): a list of cards with status, Restart and Redeploy. */
function MobileList() {
  const me = useMe();
  const editable = canEdit(me);
  const { pid, canvas, reload, withEnv } = useProject();
  const nav = useNavigate();
  if (!canvas) return <div className="flex justify-center p-8"><Spinner /></div>;
  const act = (s: Service, a: "restart" | "redeploy") => s.activeDeployment && api.deploymentAction(s.activeDeployment, a).then(() => reload(), toastError);
  return (
    <div className="absolute inset-0 overflow-y-auto p-4 pb-24">
      {canvas.services.length === 0 && <p className="py-8 text-center text-sm text-gray-500">Nothing runs here yet.</p>}
      <ul className="space-y-3">
        {canvas.services.map((s) => (
          <li key={s.id} data-testid="service-card" className={cx("rounded-xl border bg-secondaryBg p-4", stagedTag(s) && stagedTag(s) !== "Removed" ? "staged-ring" : "border-gray-200")}>
            <button type="button" className="w-full space-y-3 text-left" onClick={() => nav(withEnv(`/project/${pid}/service/${encodeURIComponent(s.id)}`))}>
              <CardBody s={s} tag={stagedTag(s)} editable={false} onRestart={() => {}} onDomain={() => {}} />
            </button>
            {editable && s.activeDeployment && (
              <div className="mt-3 flex gap-2">
                <Button onClick={() => void act(s, "restart")}>Restart</Button>
                <Button onClick={() => void act(s, "redeploy")}>Redeploy</Button>
              </div>
            )}
          </li>
        ))}
      </ul>
      <ChangesBanner readOnly={!editable} />
    </div>
  );
}

export default function CanvasView() {
  const narrow = useNarrow();
  const { error } = useProject();
  if (error) return <div className="p-6"><ErrorLine error={error} /></div>;
  if (narrow) return <MobileList />;
  return <ReactFlowProvider><Flow /></ReactFlowProvider>;
}
