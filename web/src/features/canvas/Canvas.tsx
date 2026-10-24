// SPDX-License-Identifier: AGPL-3.0-only

// The project canvas (SPEC 13.2): service cards with modifier strips, group frames, dashed
// reference edges on the Variable References layer, right-click and kebab menus, the
// bottom-left toolbar (options, zoom, undo/redo, layers), the top-centre staged-changes banner,
// drag with grid snap, overlap repair and auto layout, and a phone list under 640px.
import { memo, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useLocation, useNavigate } from "react-router-dom";
import {
  Background, BackgroundVariant, BaseEdge, Position, ReactFlow, ReactFlowProvider, getSmoothStepPath, useInternalNode, useReactFlow,
  type Edge, type EdgeProps, type InternalNode, type Node, type NodeChange, type Viewport,
} from "@xyflow/react";
import {
  Braces, CalendarDays, Camera, ChartNoAxesColumn, Clipboard, Copy, HardDrive, EyeOff, Eye, Group as GroupIcon, PencilLine, Plus, Settings, SlidersVertical, Trash2, Undo2, Upload,
} from "lucide-react";
import { api, type Group as ApiGroup, type Service } from "../../api/client";
import { useProject } from "../../app/project";
import { canEdit, useMe } from "../../app/session";
import { Avatar, Confirm, ErrorLine, Tooltip, cx, readPref, toast, toastError, writePref } from "../../kit";
import { ComposeFlow, type Target } from "../new/flows";
import { ChangesBanner, changeIndex, isTyping, useChanges } from "./Changes";
import { CtxMenu, type CtxEntry } from "./CtxMenu";
import { History, groupSummary } from "./history";
import {
  EMPTY_H, EMPTY_W, GRID, H, W, autoLayout, frames, freeSpot, layoutInput, nodeH, repairOverlaps, resolvePositions, snap, stripsOf, type Frame, type Pos, type StripKind,
} from "./layout";
import { CANVAS_EVENT, ConfigField, GroupIconDialog, NEW_MENU, NewFlows, RenameChip, UpdateInfo, newIcon, type CanvasCommand, type NewKind } from "./menus";
import { COLOUR_DOT, CardBody, cronLabel, EmptyNode, GROUP_COLOURS, GroupFrame, ServiceNode, type GroupColour, type Tag } from "./nodes";
import { Toolbar, type Layer } from "./Toolbar";
import "./canvas.css";
import { PANEL_WIDTH } from "../service/panel";

export type { Tag } from "./nodes";

const MIN_ZOOM = 0.5;
const MAX_ZOOM = 1.26;
const STEP = 1.2;

/** The selected service id from /project/:pid/service/:sid/... */
export function useSelected(): string | undefined {
  const { pathname } = useLocation();
  const m = /\/project\/[^/]+\/service\/([^/]+)/.exec(pathname);
  return m ? decodeURIComponent(m[1]) : undefined;
}

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

// ---- edges ---------------------------------------------------------------------------------

function sides(s: InternalNode, t: InternalNode) {
  const b = (n: InternalNode) => ({ x: n.internals.positionAbsolute.x, y: n.internals.positionAbsolute.y, w: n.measured.width ?? W, h: n.measured.height ?? H });
  const a = b(s), c = b(t);
  if (c.x >= a.x + a.w + 24) return { sx: a.x + a.w, sy: a.y + Math.min(a.h, H) / 2, sp: Position.Right, tx: c.x, ty: c.y + Math.min(c.h, H) / 2, tp: Position.Left };
  if (c.x + c.w + 24 <= a.x) return { sx: a.x, sy: a.y + Math.min(a.h, H) / 2, sp: Position.Left, tx: c.x + c.w, ty: c.y + Math.min(c.h, H) / 2, tp: Position.Right };
  if (c.y >= a.y) return { sx: a.x + a.w / 2, sy: a.y + a.h, sp: Position.Bottom, tx: c.x + c.w / 2, ty: c.y, tp: Position.Top };
  return { sx: a.x + a.w / 2, sy: a.y, sp: Position.Top, tx: c.x + c.w / 2, ty: c.y + c.h, tp: Position.Bottom };
}

/** A floating reference edge: orthogonal, small rounded bends, 1px dashed gray, chevron head. */
const RefEdge = memo(({ id, source, target }: EdgeProps) => {
  const s = useInternalNode(source);
  const t = useInternalNode(target);
  if (!s || !t) return null;
  const g = sides(s, t);
  const [path] = getSmoothStepPath({ sourceX: g.sx, sourceY: g.sy, sourcePosition: g.sp, targetX: g.tx, targetY: g.ty, targetPosition: g.tp, borderRadius: 6, offset: 24 });
  return <BaseEdge id={id} path={path} markerEnd="url(#cv-arrow)" className="cv-edge" interactionWidth={20} />;
});

const nodeTypes = { svc: ServiceNode, grp: GroupFrame, empty: EmptyNode };
const edgeTypes = { ref: RefEdge };

const toPos = (p: Record<string, number[]>): Pos => Object.fromEntries(Object.entries(p).filter(([, v]) => v.length >= 2).map(([k, v]) => [k, [v[0], v[1]] as [number, number]]));
const newId = () => `grp_${Math.random().toString(36).slice(2, 10)}`;

type Snapshot = { positions: Pos; groups: ApiGroup[] };
type MenuState = { x: number; y: number; items: CtxEntry[]; rowH?: number };

function Flow() {
  const me = useMe();
  const editable = canEdit(me);
  const { pid, eid, canvas, setCanvas, reload, withEnv } = useProject();
  const selected = useSelected();
  const nav = useNavigate();
  const rf = useReactFlow();
  const wrap = useRef<HTMLDivElement>(null);
  const changes = useChanges();
  const idx = useMemo(() => changeIndex(changes.data), [changes.data]);

  const [menu, setMenu] = useState<MenuState>();
  const [flow, setFlow] = useState<{ kind: NewKind; position?: number[]; serviceId?: string }>();
  const [info, setInfo] = useState<{ s: Service; focus: "name" | "icon" }>();
  const [cfgField, setCfgField] = useState<{ s: Service; field: "startCommand" | "healthcheckPath" }>();
  const [rename, setRename] = useState<{ id: string; rect: DOMRect }>();
  const [groupIcon, setGroupIcon] = useState<string>();
  const [resetAsk, setResetAsk] = useState(false);
  const [compose, setCompose] = useState<string>();
  const [showEdges, setShowEdges] = useState<boolean>(() => readPref("5050.canvas.connections", true));
  const [layer, setLayerState] = useState<Layer>(() => readPref("5050.canvas.layer", "traffic"));
  const [showPos, setShowPos] = useState(false);
  const [collapsed, setCollapsed] = useState<string[]>(() => readPref(`5050.collapsed.${pid}`, []));
  const [colours, setColours] = useState<Record<string, GroupColour>>(() => readPref(`5050.canvas.colours.${pid}`, {}));
  const history = useRef(new History<Snapshot>());
  const [, setHv] = useState(0);
  const bump = () => setHv((v) => v + 1);

  // positions: the server's, then local drags; remote moves (events) arrive through canvas.positions
  const [saved, setSaved] = useState<Pos>({});
  const savedRef = useRef(saved);
  savedRef.current = saved;
  const dragging = useRef(false);
  useEffect(() => { if (canvas && !dragging.current) setSaved(toPos(canvas.positions || {})); }, [canvas?.positions]); // eslint-disable-line react-hooks/exhaustive-deps

  const services = useMemo(() => canvas?.services || [], [canvas]);
  const groups = useMemo(() => canvas?.groups || [], [canvas]);
  const refEdges = useMemo(() => canvas?.edges || [], [canvas]);
  const L = useMemo(() => layoutInput(services, groups), [services, groups]);
  const auto = useMemo(() => autoLayout(L.nodes, L.groups, refEdges), [L, refEdges]);
  const pos = useMemo(() => resolvePositions(L.nodes, auto, saved), [L, auto, saved]);
  const posRef = useRef(pos);
  posRef.current = pos;

  const memberOf = useMemo(() => { const m: Record<string, string> = {}; for (const g of groups) for (const s of g.services) m[s] = g.id; return m; }, [groups]);
  const byId = useMemo(() => new Map(services.map((s) => [s.id, s])), [services]);
  const groupById = useMemo(() => new Map(groups.map((g) => [g.id, g])), [groups]);
  const ancestors = useCallback((gid: string) => { const out: string[] = []; let p = groupById.get(gid)?.parent; while (p && groupById.has(p) && !out.includes(p)) { out.push(p); p = groupById.get(p)?.parent; } return out; }, [groupById]);
  const hiddenGroups = useMemo(() => new Set(groups.filter((g) => ancestors(g.id).some((a) => collapsed.includes(a))).map((g) => g.id)), [groups, ancestors, collapsed]);
  const hidden = useMemo(() => new Set(services.filter((s) => { const g = memberOf[s.id]; return g && (collapsed.includes(g) || hiddenGroups.has(g)); }).map((s) => s.id)), [services, memberOf, collapsed, hiddenGroups]);

  const openStrip = useCallback((s: Service, k: StripKind) => {
    if (k === "volume" && s.volume) nav(withEnv(`/project/${pid}/volume/${encodeURIComponent(s.volume.id)}`));
    else nav(withEnv(`/project/${pid}/service/${encodeURIComponent(s.id)}/${k === "cron" ? "cron" : "settings"}`));
  }, [nav, withEnv, pid]);

  // ---- persistence ----
  const saveGroups = useCallback((next: ApiGroup[], record = true) => {
    const before = groups;
    if (record) { history.current.push({ positions: savedRef.current, groups: before }); bump(); }
    setCanvas((c) => ({ ...c, groups: next }));
    api.saveGroups(eid, next).catch((e) => { setCanvas((c) => ({ ...c, groups: before })); toastError(e); });
  }, [groups, eid, setCanvas]);
  const savePositions = useCallback((p: Pos) => {
    setSaved(p);
    api.savePositions(eid, p).catch(() => { /* the next move tries again */ });
  }, [eid]);
  const record = useCallback(() => { history.current.push({ positions: Object.keys(savedRef.current).length ? savedRef.current : posRef.current, groups }); bump(); }, [groups]);

  const setCollapse = useCallback((id: string, v?: boolean) => setCollapsed((c) => {
    const on = v ?? !c.includes(id);
    const n = on ? [...c.filter((x) => x !== id), id] : c.filter((x) => x !== id);
    writePref(`5050.collapsed.${pid}`, n);
    return n;
  }), [pid]);
  const setColour = useCallback((id: string, c: GroupColour) => setColours((m) => { const n = { ...m, [id]: c }; writePref(`5050.canvas.colours.${pid}`, n); return n; }), [pid]);
  const setLayer = useCallback((l: Layer) => { setLayerState(l); writePref("5050.canvas.layer", l); if (l === "references") { setShowEdges(true); writePref("5050.canvas.connections", true); } }, []);
  const toggleEdges = useCallback(() => setShowEdges((h) => { writePref("5050.canvas.connections", !h); return !h; }), []);

  // ---- viewport ----
  // keep clear of the toolbar (left), the Add button and the staged banner (top)
  const staged = (canvas?.changesCount || 0) > 0;
  const fitPadding = useMemo(() => ({ top: staged ? "84px" : "64px", left: "76px", right: "40px", bottom: "40px" }) as const, [staged]);
  const fit = useCallback((duration = 250) => rf.fitView({ padding: fitPadding, maxZoom: 1, minZoom: MIN_ZOOM, duration }), [rf, fitPadding]);
  // repeated clicks build on the zoom still animating, not on the frame in between
  const zoomTarget = useRef<{ z: number; at: number }>();
  const zoomBy = useCallback((k: number) => {
    const t = zoomTarget.current;
    const base = t && Date.now() - t.at < 250 ? t.z : rf.getZoom();
    const z = Math.max(MIN_ZOOM, Math.min(MAX_ZOOM, base * k));
    zoomTarget.current = { z, at: Date.now() };
    void rf.zoomTo(z, { duration: 200 });
  }, [rf]);

  const vpKey = `5050.vp.${pid}.${eid}`;
  const savedVp = useMemo(() => readPref<Viewport | null>(vpKey, null), [vpKey]);
  const lastCount = useRef<number>();
  useEffect(() => {
    if (!canvas) return;
    const n = services.length, prev = lastCount.current;
    lastCount.current = n;
    if (prev === undefined) return;   // the first fit is ReactFlow's own (fitView prop)
    if ((prev === 0 && n > 0) || (prev > 0 && n === 0)) requestAnimationFrame(() => void fit(200));
  }, [canvas, services.length, fit]);

  // selecting a service: the panel covers the right, so zoom out and keep the card in the strip on the left
  const beforeSelect = useRef<Viewport | null>(null);
  const hasCanvas = !!canvas;
  useEffect(() => {
    if (!hasCanvas) return;
    if (!selected) {
      if (beforeSelect.current) { void rf.setViewport(beforeSelect.current, { duration: 200 }); beforeSelect.current = null; }
      return;
    }
    let tries = 0;
    let t: ReturnType<typeof setTimeout>;
    const go = () => {
      const n = rf.getInternalNode(selected);
      const el = wrap.current;
      if (!n?.measured.width || !el) { if (tries++ < 20) t = setTimeout(go, 50); return; }
      const cw = el.clientWidth, ch = el.clientHeight;
      const strip = cw - PANEL_WIDTH(cw);
      if (strip < 140) return;
      if (!beforeSelect.current) beforeSelect.current = rf.getViewport();
      const z = Math.max(MIN_ZOOM, Math.min(1, (strip - 21) / W));
      const { x, y } = n.internals.positionAbsolute;
      const h = n.measured.height ?? H;
      void rf.setViewport({ x: 11 - x * z, y: ch / 2 - (y + h / 2) * z, zoom: z }, { duration: 200 });
    };
    go();
    return () => clearTimeout(t);
  }, [selected, hasCanvas, rf]);

  // ---- nodes and edges ----
  const frameList = useMemo(() => frames(L.nodes.filter((n) => !hidden.has(n.id) || collapsed.includes(memberOf[n.id])), L.groups, pos, collapsed), [L, pos, collapsed, hidden, memberOf]);

  const onKebab = useCallback((id: string, el: HTMLElement) => {
    const r = el.getBoundingClientRect();
    setMenu({ x: r.right - 220 + 12, y: r.bottom + 6, items: groupItems(id, true), rowH: 36 });
  }, []); // eslint-disable-line react-hooks/exhaustive-deps -- groupItems reads fresh state through refs below
  const onRenameGroup = useCallback((id: string) => {
    const el = document.querySelector(`[data-id="g:${CSS.escape(id)}"]`);
    if (el) setRename({ id, rect: el.getBoundingClientRect() });
  }, []);

  const nodes: Node[] = useMemo(() => {
    if (canvas && services.length === 0) {
      return [{ id: "__empty", type: "empty", position: { x: -EMPTY_W / 2, y: -EMPTY_H / 2 }, width: EMPTY_W, height: EMPTY_H, measured: { width: EMPTY_W, height: EMPTY_H }, draggable: false, selectable: false,
        data: { editable, envName: canvas.environment.name, onPick: (k: NewKind) => setFlow({ kind: k, position: [snap(-W / 2), snap(-H / 2)] }) } } as Node];
    }
    const fr = frameList.filter((f) => !hiddenGroups.has(f.id)).map((f: Frame) => {
      const isCollapsed = collapsed.includes(f.id);
      const members = services.filter((s) => memberOf[s.id] === f.id);
      return {
        id: `g:${f.id}`, type: "grp", position: { x: f.x, y: f.y }, width: f.w, height: f.h, measured: { width: f.w, height: f.h },
        data: {
          ...f, collapsed: isCollapsed, colour: colours[f.id], editable,
          summary: groupSummary(members.map((m) => m.status), (st) => (st === "online" ? "Online" : st === "no_deployment" ? "Offline" : st.charAt(0).toUpperCase() + st.slice(1).replace("_", " "))),
          memberIcons: members, onKebab, onRename: onRenameGroup,
        },
        draggable: false, selectable: false, focusable: false, zIndex: isCollapsed ? 2 : -10 + f.depth,
      } as Node;
    });
    const cards = L.nodes.filter((n) => !hidden.has(n.id)).map((n) => {
      const s = byId.get(n.id)!;
      const ci = idx[s.id];
      return {
        id: n.id, type: "svc", position: { x: pos[n.id][0], y: pos[n.id][1] }, width: W, height: nodeH(n), measured: { width: W, height: nodeH(n) }, draggable: editable,
        data: { s, tag: stagedTag(s, ci?.tag), changes: ci?.count || 0, selected: s.id === selected, strips: stripsOf(s), showPos: showPos ? pos[n.id] : undefined, onStrip: openStrip },
        selectable: false, ariaLabel: `${(s.staged || s.config).name} service`,
      } as Node;
    });
    return [...fr, ...cards];
  }, [canvas, services, L, pos, frameList, hiddenGroups, hidden, collapsed, colours, memberOf, byId, idx, selected, editable, showPos, openStrip, onKebab, onRenameGroup]);

  const edges: Edge[] = useMemo(() => (!showEdges || layer !== "references" ? [] : refEdges
    .filter((e) => byId.has(e.source) && byId.has(e.target) && !hidden.has(e.source) && !hidden.has(e.target))
    .map((e) => ({ id: `${e.source}->${e.target}`, source: e.source, target: e.target, type: "ref", className: "cv-edge-wrap", focusable: false }))),
  [refEdges, showEdges, layer, hidden, byId]);

  // ---- dragging ----
  const dragStart = useRef<Snapshot>();
  const onNodesChange = useCallback((cs: NodeChange[]) => {
    const moved: Pos = {};
    for (const c of cs) if (c.type === "position" && c.position && !c.id.startsWith("g:") && c.id !== "__empty") moved[c.id] = [c.position.x, c.position.y];
    if (!Object.keys(moved).length) return;
    if (!dragging.current) { dragging.current = true; dragStart.current = { positions: { ...posRef.current }, groups }; }
    setSaved((prev) => ({ ...(Object.keys(prev).length ? prev : posRef.current), ...moved }));
  }, [groups]);

  const onDragStop = useCallback((e: React.MouseEvent, node: Node) => {
    dragging.current = false;
    if (dragStart.current) { history.current.push(dragStart.current); bump(); }
    dragStart.current = undefined;
    const snapped: Pos = Object.fromEntries(Object.entries({ ...posRef.current, ...savedRef.current }).map(([k, [x, y]]) => [k, [snap(x), snap(y)] as [number, number]]));
    if (node.type !== "svc") { savePositions(snapped); return; }
    const dropped = snapped[node.id];
    snapped[node.id] = freeSpot(node.id, L.nodes.filter((n) => !hidden.has(n.id)), snapped);
    savePositions(snapped);
    // groups: Shift drops it out of its group; otherwise exactly one innermost frame under the card decides
    const cur = memberOf[node.id];
    let next: string | undefined = cur;
    if (e.shiftKey) next = undefined;
    else {
      const others = L.nodes.filter((n) => n.id !== node.id);
      const fr = frames(others, L.groups, snapped, collapsed).filter((f) => !collapsed.includes(f.id) && !hiddenGroups.has(f.id));
      const [x, y] = dropped;   // where it was let go, before any overlap nudge
      const me = { x, y, w: W, h: H };
      const hit = fr.filter((f) => me.x < f.x + f.w && f.x < me.x + me.w && me.y < f.y + f.h && f.y < me.y + me.h);
      const leaves = hit.filter((f) => !hit.some((o) => o.id !== f.id && ancestors(o.id).includes(f.id)));
      if (leaves.length === 1) next = leaves[0].id;
      else if (!leaves.length && cur && fr.some((f) => f.id === cur)) next = undefined;
    }
    if (next === cur) return;
    saveGroups(groups.map((g) => ({ ...g, services: g.id === next ? [...g.services.filter((s) => s !== node.id), node.id] : g.services.filter((s) => s !== node.id) })), false);
  }, [L, memberOf, groups, saveGroups, savePositions, collapsed, hiddenGroups, hidden, ancestors]);

  // ---- canvas actions ----
  const autoLay = useCallback(() => {
    record();
    savePositions({});
    setTimeout(() => void fit(), 60);
  }, [record, savePositions, fit]);
  const repair = useCallback(() => {
    const r = repairOverlaps(L.nodes, pos);
    if (!r.moved.length) { toast("No overlapping services"); return; }
    record();
    savePositions(r.pos);
    setTimeout(() => void fit(), 60);
  }, [L, pos, record, savePositions, fit]);
  const reset = useCallback(async () => {
    record();
    setCanvas((c) => ({ ...c, groups: [] }));
    savePositions({});
    await api.saveGroups(eid, []);
    setTimeout(() => void fit(), 60);
  }, [record, setCanvas, savePositions, eid, fit]);

  const restore = useCallback((s: Snapshot | undefined) => {
    if (!s) return;
    savePositions(s.positions);
    if (JSON.stringify(s.groups) !== JSON.stringify(groups)) saveGroups(s.groups, false);
    bump();
  }, [groups, saveGroups, savePositions]);
  const undo = useCallback(() => restore(history.current.undo({ positions: savedRef.current, groups })), [restore, groups]);
  const redo = useCallback(() => restore(history.current.redo({ positions: savedRef.current, groups })), [restore, groups]);

  // ---- groups ----
  const groupsRef = useRef(groups);
  groupsRef.current = groups;
  const createGroupWith = useCallback((sid: string) => {
    const gs = groupsRef.current;
    const id = newId();
    const parent = memberOf[sid];
    const next = [...gs.map((g) => ({ ...g, services: g.services.filter((s) => s !== sid) })), { id, name: `Group ${gs.length + 1}`, services: [sid], ...(parent ? { parent } : {}) }];
    saveGroups(next);
    setTimeout(() => onRenameGroup(id), 120);
  }, [memberOf, saveGroups, onRenameGroup]);
  const moveTo = useCallback((sid: string, gid?: string) => {
    saveGroups(groupsRef.current.map((g) => ({ ...g, services: g.id === gid ? [...g.services.filter((s) => s !== sid), sid] : g.services.filter((s) => s !== sid) })));
  }, [saveGroups]);
  const removeGroup = useCallback((gid: string) => {
    const g = groupsRef.current.find((x) => x.id === gid);
    saveGroups(groupsRef.current.filter((x) => x.id !== gid).map((x) => (x.parent === gid ? { ...x, parent: g?.parent } : x)));
  }, [saveGroups]);
  const nestGroup = useCallback((gid: string, parent?: string) => {
    saveGroups(groupsRef.current.map((x) => (x.id === gid ? { ...x, parent } : x)));
  }, [saveGroups]);
  const wrapGroup = useCallback((gid: string) => {
    const gs = groupsRef.current;
    const g = gs.find((x) => x.id === gid);
    const id = newId();
    saveGroups([...gs.map((x) => (x.id === gid ? { ...x, parent: id } : x)), { id, name: `Group ${gs.length + 1}`, services: [], ...(g?.parent ? { parent: g.parent } : {}) }]);
    setTimeout(() => onRenameGroup(id), 120);
  }, [saveGroups, onRenameGroup]);

  // ---- menus ----
  const stateRef = useRef({ collapsed, colours });
  stateRef.current = { collapsed, colours };
  function groupItems(gid: string, kebab: boolean): CtxEntry[] {
    const gs = groupsRef.current;
    const g = gs.find((x) => x.id === gid);
    if (!g) return [];
    const isCollapsed = stateRef.current.collapsed.includes(gid);
    const colour = stateRef.current.colours[gid] || "gray";
    const descendants = new Set<string>();
    const walk = (id: string) => { for (const x of gs) if (x.parent === id && !descendants.has(x.id)) { descendants.add(x.id); walk(x.id); } };
    walk(gid);
    const targets = gs.filter((x) => x.id !== gid && !descendants.has(x.id));
    return [
      { label: "Rename", icon: <PencilLine />, onClick: () => onRenameGroup(gid) },
      ...(kebab ? [{ label: "Update Icon", icon: <Camera />, onClick: () => setGroupIcon(gid) }] : []),
      { label: isCollapsed ? "Show contents" : "Hide contents", icon: isCollapsed ? <Eye /> : <EyeOff />, onClick: () => setCollapse(gid) },
      {
        label: "Colour", icon: <span className="h-4 w-4 rounded-full" style={{ background: COLOUR_DOT[colour] }} />,
        sub: GROUP_COLOURS.map((c) => ({ label: c.charAt(0).toUpperCase() + c.slice(1), checked: c === colour, icon: <span className="h-5 w-5 rounded-full" style={{ background: COLOUR_DOT[c] }} />, onClick: () => setColour(gid, c) })),
      },
      "sep",
      {
        label: "Group", icon: <GroupIcon />,
        sub: [
          { label: "New Group", icon: <Plus />, onClick: () => wrapGroup(gid) },
          ...(targets.length ? ["sep" as const, ...targets.map((t) => ({ id: t.id, label: t.name, checked: g.parent === t.id, onClick: () => nestGroup(gid, t.id) }))] : []),
          ...(g.parent ? ["sep" as const, { label: "Remove from group", onClick: () => nestGroup(gid, undefined) }] : []),
        ],
      },
      "sep",
      { label: "Remove Group", icon: <Trash2 />, danger: true, onClick: () => removeGroup(gid) },
    ];
  }

  const svcPath = useCallback((s: Service, tab: string) => withEnv(`/project/${pid}/service/${encodeURIComponent(s.id)}/${tab}`), [withEnv, pid]);
  const stageDelete = useCallback(async (s: Service) => {
    const name = (s.staged || s.config).name;
    setCanvas((c) => ({ ...c, services: c.services.map((x) => (x.id === s.id ? { ...x, stagedAction: "delete" } : x)) }));
    try {
      await api.deleteService(eid, s.id, name);
      toast(`${name} will be deleted when you deploy`);
    } catch (e) { toastError(e); } finally { void reload(); }
  }, [eid, setCanvas, reload]);
  const undoDelete = useCallback(async (s: Service) => {
    const c = changes.data?.changes.find((x) => x.serviceId === s.id && (x.kind === "service.delete" || x.tag === "removed"));
    try { if (c) await api.discardChange(eid, c.id); } catch (e) { toastError(e); } finally { void reload(); }
  }, [changes.data, eid, reload]);

  const serviceItems = (s: Service): CtxEntry[] => {
    const view: CtxEntry[] = [
      { label: "View Variables", icon: <Braces />, onClick: () => nav(svcPath(s, "variables")) },
      { label: "View Metrics", icon: <ChartNoAxesColumn />, onClick: () => nav(svcPath(s, "metrics")) },
      { label: "View Settings", icon: <Settings />, onClick: () => nav(svcPath(s, "settings")) },
    ];
    if (!editable) return view;
    const cur = memberOf[s.id];
    const removed = s.stagedAction === "delete";
    return [
      {
        label: "Group", icon: <GroupIcon />,
        sub: [
          { label: "New Group", icon: <Plus />, onClick: () => createGroupWith(s.id) },
          ...(groups.length ? ["sep" as const, ...groups.map((g) => ({ id: g.id, label: g.name, checked: g.id === cur, onClick: () => moveTo(s.id, g.id) }))] : []),
          ...(cur ? ["sep" as const, { label: "Remove from group", onClick: () => moveTo(s.id, undefined) }] : []),
        ],
      },
      {
        label: "Config", icon: <SlidersVertical />,
        sub: [
          { label: "Name", onClick: () => setInfo({ s, focus: "name" }) },
          { label: "Icon", onClick: () => setInfo({ s, focus: "icon" }) },
          "sep",
          { label: "Add Variable", onClick: () => nav(`${svcPath(s, "variables")}&new=1`) },
          { label: "Start command", onClick: () => setCfgField({ s, field: "startCommand" }) },
          { label: "Healthcheck path", onClick: () => setCfgField({ s, field: "healthcheckPath" }) },
        ],
      },
      "sep",
      ...view,
      "sep",
      { label: "Duplicate", icon: <Clipboard />, onClick: () => { api.duplicate(eid, s.id).then(() => { toast(`Duplicated ${(s.staged || s.config).name}`); return reload(); }, toastError); } },
      "sep",
      removed
        ? { label: "Undo Delete", icon: <Undo2 />, onClick: () => void undoDelete(s) }
        : { label: "Delete Service", icon: <Trash2 />, danger: true, onClick: () => void stageDelete(s) },
    ];
  };

  const addItems = (flowPos: { x: number; y: number }, extra: boolean): CtxEntry[] => [
    { heading: "Add New Service" },
    ...NEW_MENU.filter((m) => extra || m.id !== "volume").map((m) => ({
      label: m.label, icon: newIcon(m.id), disabled: m.disabled,
      onClick: () => setFlow({ kind: m.id as NewKind, position: [snap(flowPos.x), snap(flowPos.y)] }),
    })),
    ...(extra ? ["sep" as const, { label: "Import Compose File", icon: <Upload />, onClick: () => setFlow({ kind: "compose" }) }] : []),
  ];

  const onPaneMenu = (e: React.MouseEvent | MouseEvent) => {
    e.preventDefault();
    if (!editable) return;
    const p = rf.screenToFlowPosition({ x: e.clientX, y: e.clientY });
    setMenu({ x: e.clientX, y: e.clientY, items: addItems(p, false) });
  };
  const onNodeMenu = (e: React.MouseEvent, n: Node) => {
    e.preventDefault();
    if (n.type === "svc") { const s = byId.get(n.id); if (s) setMenu({ x: e.clientX, y: e.clientY, items: serviceItems(s) }); return; }
    if (n.type === "grp" && editable) setMenu({ x: e.clientX, y: e.clientY, items: groupItems(n.id.slice(2), false), rowH: 36 });
  };

  // ---- keyboard ----
  useEffect(() => {
    const k = (e: KeyboardEvent) => {
      if (isTyping(e.target) || document.querySelector("[role=dialog]")) return;
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "z" && editable) { e.preventDefault(); if (e.shiftKey) redo(); else undo(); return; }
      if (e.key === "Escape" && selected && !document.querySelector("[data-ctx-menu],[data-popover]")) nav(withEnv(`/project/${pid}`));
    };
    window.addEventListener("keydown", k);
    return () => window.removeEventListener("keydown", k);
  }, [undo, redo, editable, selected, nav, withEnv, pid]);

  // ---- palette commands ----
  useEffect(() => {
    const h = (ev: Event) => {
      const c = (ev as CustomEvent<CanvasCommand>).detail;
      const centre = () => { const r = wrap.current?.getBoundingClientRect(); return rf.screenToFlowPosition({ x: (r?.left ?? 0) + (r?.width ?? 0) / 2, y: (r?.top ?? 0) + (r?.height ?? 0) / 2 }); };
      switch (c.kind) {
        case "new": { const p = centre(); setFlow({ kind: c.what, position: [snap(p.x - W / 2), snap(p.y - H / 2)] }); break; }
        case "toggle-connections": toggleEdges(); break;
        case "tidy": case "auto-layout": autoLay(); break;
        case "repair-overlaps": repair(); break;
        case "reset": setResetAsk(true); break;
        case "toggle-positions": setShowPos((v) => !v); break;
        case "layer": setLayer(c.layer); break;
        case "update-info": { const s = byId.get(c.serviceId); if (s) setInfo({ s, focus: "name" }); break; }
      }
    };
    window.addEventListener(CANVAS_EVENT, h);
    return () => window.removeEventListener(CANVAS_EVENT, h);
  }, [byId, autoLay, repair, toggleEdges, setLayer, rf]);

  // presence heartbeat
  useEffect(() => {
    if (!eid) return;
    const beat = () => void api.presence(eid).catch(() => {});
    beat();
    const t = setInterval(beat, 20000);
    return () => clearInterval(t);
  }, [eid]);

  const flowTarget = useMemo<Target>(() => ({ canvas: true, ensure: async () => ({ pid, eid }), done: () => { void reload(); } }), [pid, eid, reload]);

  if (!canvas) return <div className="cv absolute inset-0 bg-background" aria-busy="true"><DotsOnly /></div>;
  const others = canvas.presence.filter((p) => p.userId !== me.user.id && Date.now() / 1000 - (p.lastSeen > 1e12 ? p.lastSeen / 1000 : p.lastSeen) < 90);

  return (
    <div ref={wrap} className="cv absolute inset-0 overflow-hidden"
      onDragOver={(e) => { if (editable && e.dataTransfer.types.includes("Files")) e.preventDefault(); }}
      onDrop={(e) => {
        const f = e.dataTransfer.files?.[0];
        if (!f || !editable) return;
        e.preventDefault();
        if (!/\.ya?ml$/i.test(f.name)) { toastError(new Error("Drop a docker-compose.yml file to import it.")); return; }
        void f.text().then(setCompose);
      }}
      onDoubleClick={(e) => {
        if (!(e.target as Element).classList?.contains("react-flow__pane")) return;
        const r = wrap.current!.getBoundingClientRect();
        const vp = rf.getViewport();
        const z = Math.min(MAX_ZOOM, vp.zoom * 1.26);
        if (z === vp.zoom) return;
        const px = e.clientX - r.left, py = e.clientY - r.top, k = z / vp.zoom;
        void rf.setViewport({ x: px - (px - vp.x) * k, y: py - (py - vp.y) * k, zoom: z }, { duration: 200 });
      }}>
      <ReactFlow defaultViewport={savedVp || { x: 0, y: 0, zoom: 1 }} fitView={!savedVp && !selected} fitViewOptions={{ padding: fitPadding, maxZoom: 1, minZoom: MIN_ZOOM }}
        onMoveEnd={(_, vp) => { if (!selected) writePref(vpKey, vp); }}
        nodes={nodes} edges={edges} nodeTypes={nodeTypes} edgeTypes={edgeTypes} onNodesChange={onNodesChange} onNodeDragStop={onDragStop}
        snapToGrid snapGrid={[GRID, GRID]} minZoom={MIN_ZOOM} maxZoom={MAX_ZOOM} proOptions={{ hideAttribution: true }}
        onNodeClick={(_, n) => { if (n.type === "svc") nav(withEnv(`/project/${pid}/service/${encodeURIComponent(n.id)}`)); }}
        onNodeContextMenu={onNodeMenu} onPaneContextMenu={onPaneMenu}
        onPaneClick={() => { if (selected) nav(withEnv(`/project/${pid}`)); }}
        nodesConnectable={false} elementsSelectable={false} nodesFocusable panOnScroll zoomOnScroll={false} zoomOnPinch zoomOnDoubleClick={false}
        nodeDragThreshold={4} selectNodesOnDrag={false} deleteKeyCode={null} selectionKeyCode={null} multiSelectionKeyCode={null}>
        <Background variant={BackgroundVariant.Dots} gap={GRID} size={1} color="var(--gray-400)" bgColor="var(--background)" />
        <svg aria-hidden width="0" height="0" className="absolute">
          <defs>
            <marker id="cv-arrow" viewBox="-10 -10 20 20" markerWidth="24" markerHeight="24" refX="0" refY="0" markerUnits="userSpaceOnUse" orient="auto-start-reverse">
              <polyline points="-5,-4 0,0 -5,4" fill="var(--gray-300)" stroke="var(--gray-300)" strokeWidth="1" strokeLinejoin="round" />
            </marker>
          </defs>
        </svg>
      </ReactFlow>

      <Toolbar editable={editable} canUndo={history.current.canUndo} canRedo={history.current.canRedo} showEdges={showEdges} layer={layer}
        onZoomIn={() => zoomBy(STEP)} onZoomOut={() => zoomBy(1 / STEP)} onFit={() => void fit()} onUndo={undo} onRedo={redo}
        onToggleEdges={toggleEdges} onAutoLayout={autoLay} onRepair={repair} onReset={() => setResetAsk(true)} onLayer={setLayer} />

      <div className="absolute right-4 top-4 z-20 flex items-center gap-2" onDoubleClick={(e) => e.stopPropagation()}>
        {others.length > 0 && (
          <div className="flex -space-x-1.5" aria-label="Also viewing">
            {others.slice(0, 5).map((p) => (
              <Tooltip key={p.userId} content={`${p.name} is here`} side="bottom">
                <span data-testid="presence-avatar" className="rounded-full ring-2 ring-background"><Avatar name={p.name} size={28} /></span>
              </Tooltip>
            ))}
          </div>
        )}
        {editable && (
          <button type="button" aria-haspopup="menu" data-testid="canvas-add"
            onClick={(e) => {
              const r = e.currentTarget.getBoundingClientRect();
              const p = centreOf(wrap.current, rf);
              setMenu({ x: r.right - 220, y: r.bottom + 8, items: addItems({ x: p.x - W / 2, y: p.y - H / 2 }, true) });
            }}
            className="cv-press flex h-[34px] items-center gap-2 rounded-md border border-line bg-secondaryBg px-3 text-sm text-foreground hover:bg-surface-hover">
            <Plus size={16} className="text-fg-3" />Add
          </button>
        )}
      </div>

      <ChangesBanner readOnly={!editable} changes={changes} />

      {menu && <CtxMenu x={menu.x} y={menu.y} items={menu.items} rowH={menu.rowH} onClose={() => setMenu(undefined)} />}
      {flow && <NewFlows open={flow.kind} position={flow.position} serviceId={flow.serviceId} onClose={() => setFlow(undefined)} />}
      {info && <UpdateInfo service={info.s} focus={info.focus} onClose={() => setInfo(undefined)} />}
      {cfgField && <ConfigField service={cfgField.s} field={cfgField.field} onClose={() => setCfgField(undefined)} />}
      {compose !== undefined && <ComposeFlow target={flowTarget} initial={compose} onClose={() => setCompose(undefined)} />}
      {rename && (
        <RenameChip rect={rename.rect} value={groupById.get(rename.id)?.name || ""} onClose={() => setRename(undefined)}
          onSave={(v) => saveGroups(groupsRef.current.map((g) => (g.id === rename.id ? { ...g, name: v } : g)))} />
      )}
      {groupIcon && (
        <GroupIconDialog value={groupById.get(groupIcon)?.icon} onClose={() => setGroupIcon(undefined)}
          onSave={(icon) => saveGroups(groupsRef.current.map((g) => (g.id === groupIcon ? { ...g, icon } : g)))} />
      )}
      {resetAsk && (
        <Confirm danger title="Reset Canvas" confirm="Reset" body="This removes every group and puts each service back in the automatic layout. Services are not changed."
          onClose={() => setResetAsk(false)} onConfirm={reset} />
      )}
    </div>
  );
}

function centreOf(el: HTMLElement | null, rf: ReturnType<typeof useReactFlow>) {
  const r = el?.getBoundingClientRect();
  return rf.screenToFlowPosition({ x: (r?.left ?? 0) + (r?.width ?? 0) / 2, y: (r?.top ?? 0) + (r?.height ?? 0) / 2 });
}

/** Loading: the bare dotted surface, no spinner. */
function DotsOnly() {
  return <div className="absolute inset-0" style={{ backgroundImage: "radial-gradient(circle, var(--gray-400) 0.5px, transparent 0.75px)", backgroundSize: `${GRID}px ${GRID}px` }} />;
}

// ---- phones ----------------------------------------------------------------------------------

/** Under 640px: one full-width row per service with its strips, and a round Add button. */
function MobileList() {
  const me = useMe();
  const editable = canEdit(me);
  const { pid, canvas, withEnv } = useProject();
  const nav = useNavigate();
  const changes = useChanges();
  const idx = useMemo(() => changeIndex(changes.data), [changes.data]);
  const [menu, setMenu] = useState<MenuState>();
  const [flow, setFlow] = useState<NewKind>();
  if (!canvas) {
    return (
      <div className="absolute inset-0 overflow-hidden">
        {[0, 1, 2].map((i) => <div key={i} className="flex h-[118px] items-center gap-3 border-b border-gray-100 px-4"><span className="h-6 w-6 rounded bg-skeleton animate-skeleton" /><span className="h-4 w-40 rounded bg-skeleton animate-skeleton" /></div>)}
      </div>
    );
  }
  return (
    <div className="cv absolute inset-0 overflow-y-auto pb-28 scroll-thin">
      <ChangesBanner readOnly={!editable} changes={changes} inline />
      <div className="flex gap-2 p-4">
        <button type="button" onClick={() => nav(withEnv(`/project/${pid}/settings/general`))} className="cv-press flex h-10 flex-1 items-center justify-center gap-2 rounded-lg border border-line text-sm text-foreground"><Settings size={16} className="text-fg-3" />Settings</button>
        <button type="button" onClick={() => nav(withEnv(`/project/${pid}/logs`))} className="cv-press flex h-10 flex-1 items-center justify-center gap-2 rounded-lg border border-line text-sm text-foreground">Logs</button>
      </div>
      {canvas.services.length === 0 && <p className="px-4 py-10 text-center text-sm text-fg-3">Nothing runs in {canvas.environment.name} yet.</p>}
      <ul>
        {canvas.services.map((s) => {
          const ci = idx[s.id];
          const tag = stagedTag(s, ci?.tag);
          return (
            <li key={s.id} data-testid="service-card" className={cx("border-b border-line first:border-t", tag === "Edited" ? "bg-blue-50" : tag === "New" ? "bg-green-100" : tag === "Removed" ? "bg-red-50" : "bg-secondaryBg")}>
              <button type="button" className="flex min-h-[118px] w-full flex-col justify-between gap-6 px-4 py-6 text-left" onClick={() => nav(withEnv(`/project/${pid}/service/${encodeURIComponent(s.id)}`))}>
                <CardBody s={s} tag={tag} changes={ci?.count || 0} />
              </button>
              {stripsOf(s).map((k) => (
                <div key={k} className="relative grid h-12 grid-cols-[24px_1fr] items-center gap-3 border-t border-gray-100 px-4 pl-10 text-sm text-fg-3">
                  <span aria-hidden className="absolute bottom-0 left-4 top-0 w-1.5 bg-gray-200 opacity-30" />
                  <span className="mx-auto">{k === "volume" ? <HardDrive size={16} /> : k === "cron" ? <CalendarDays size={16} /> : <Copy size={16} />}</span>
                  <span className="truncate">{k === "volume" ? s.volume?.name : k === "cron" ? cronLabel(s) : `${(s.staged || s.config).deploy.replicas} Replicas`}</span>
                </div>
              ))}
            </li>
          );
        })}
      </ul>
      {editable && (
        <button type="button" aria-label="Add a service" onClick={(e) => {
          const r = e.currentTarget.getBoundingClientRect();
          setMenu({ x: r.right - 220, y: r.top - 8, items: [{ heading: "Add New Service" }, ...NEW_MENU.map((m) => ({ label: m.label, icon: newIcon(m.id), disabled: m.disabled, onClick: () => setFlow(m.id as NewKind) }))] });
        }}
          className="cv-press fixed bottom-20 right-4 z-30 flex h-14 w-14 items-center justify-center rounded-full bg-gradient-to-br from-pink-400 to-pink-600 text-white shadow-modal">
          <Plus size={24} />
        </button>
      )}
      {menu && <CtxMenu x={menu.x} y={menu.y} items={menu.items} onClose={() => setMenu(undefined)} />}
      {flow && <NewFlows open={flow} onClose={() => setFlow(undefined)} />}
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
