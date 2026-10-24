// SPDX-License-Identifier: AGPL-3.0-only

// Staged changes: the top-centre "Apply N changes" banner (Details, Deploy with Shift+Enter,
// a kebab with Discard Changes) and the Details review (commit message, one section per
// service with Change / Current Value / New Value rows, per-row and per-service discard).
import { useCallback, useEffect, useRef, useState, type CSSProperties, type ReactNode } from "react";
import { useLocation } from "react-router-dom";
import { Braces, Check, ChevronDown, CircleAlert, Container, Cpu, GitBranch, HardDrive, Info, Minus, MoreVertical, Pencil, Plus, Settings2, Terminal, X } from "lucide-react";
import { api, type Change, type ChangesResponse, type Service } from "../../api/client";
import { useProject } from "../../app/project";
import { useMe } from "../../app/session";
import { Avatar, Button, ErrorLine, Modal, Skeleton, Tooltip, cx, isMac, toast, toastError } from "../../kit";
import { CtxMenu } from "./CtxMenu";
import { NodeIcon } from "./nodes";

export const isTyping = (t: EventTarget | null) => {
  const el = t as HTMLElement | null;
  return !!el && (el.tagName === "INPUT" || el.tagName === "TEXTAREA" || el.tagName === "SELECT" || el.isContentEditable);
};

const plural = (n: number, one: string) => `${n} ${one}${n === 1 ? "" : "s"}`;

/** The staged change list of the environment, refetched whenever the count moves. */
export function useChanges(): { data?: ChangesResponse; reload: () => void; setData: (f: (d: ChangesResponse) => ChangesResponse) => void } {
  const { eid, canvas } = useProject();
  const [data, setState] = useState<ChangesResponse>();
  const n = canvas?.changesCount || 0;
  const reload = useCallback(() => { if (eid) api.changes(eid).then(setState, () => {}); }, [eid]);
  useEffect(() => { if (!n) { setState({ changes: [] } as unknown as ChangesResponse); return; } reload(); }, [n, reload]);
  const setData = useCallback((f: (d: ChangesResponse) => ChangesResponse) => setState((d) => (d ? f(d) : d)), []);
  return { data, reload, setData };
}

/** Per-service tag ("new" | "edited" | "removed") and setting count, for the cards. */
export function changeIndex(data?: ChangesResponse): Record<string, { tag?: string; count: number }> {
  const out: Record<string, { tag?: string; count: number }> = {};
  for (const c of data?.changes || []) {
    if (!c.serviceId) continue;
    const cur = out[c.serviceId] || { count: 0 };
    cur.count += Math.max(1, c.diff.length);
    cur.tag = c.tag || cur.tag || (c.kind === "service.create" ? "new" : c.kind === "service.delete" ? "removed" : "edited");
    out[c.serviceId] = cur;
  }
  return out;
}

function useCommit(readOnly?: boolean) {
  const { eid, canvas, setCanvas, reload } = useProject();
  const n = canvas?.changesCount || 0;
  const [busy, setBusy] = useState<"deploy" | "commit" | "discard">();
  const [error, setError] = useState<string>();
  const commit = async (deploy: boolean, message?: string): Promise<boolean> => {
    if (busy || readOnly) return false;
    const before = n;
    setBusy(deploy ? "deploy" : "commit"); setError(undefined);
    try {
      const r = await api.commit(eid, { deploy, message: message || undefined });
      setCanvas((c) => ({ ...c, changesCount: 0 }));
      const k = r.deployments?.length || 0;
      toast(deploy ? (k ? `Deploying ${plural(k, "service")}` : "Changes applied") : "Changes committed without deploying", "success");
      return true;
    } catch (e) {
      setCanvas((c) => ({ ...c, changesCount: before }));
      setError(e instanceof Error ? e.message : String(e));
      return false;
    } finally { setBusy(undefined); void reload(); }
  };
  const discard = async () => {
    const before = n;
    setBusy("discard"); setError(undefined);
    setCanvas((c) => ({ ...c, changesCount: 0 }));
    try { await api.discardChanges(eid); toast("Changes discarded"); } catch (e) { setCanvas((c) => ({ ...c, changesCount: before })); toastError(e); } finally { setBusy(undefined); void reload(); }
  };
  return { n, busy, error, commit, discard };
}

/** Shift+Enter (and the hint chip). */
const ShiftEnter = () => <span className="ml-1 inline-flex items-center gap-0.5 text-caption font-medium text-pink-800">{isMac() ? "⇧" : "Shift"}+Enter</span>;

export function ChangesBanner({ readOnly, changes, inline }: { readOnly?: boolean; changes: ReturnType<typeof useChanges>; inline?: boolean }) {
  const { n, busy, error, commit, discard } = useCommit(readOnly);
  const panelOpen = /\/(service|volume)\//.test(useLocation().pathname);
  const [details, setDetails] = useState(false);
  const [menu, setMenu] = useState<{ x: number; y: number }>();
  // keep the banner on screen while it animates out
  const [shown, setShown] = useState(n);
  const [leaving, setLeaving] = useState(false);
  useEffect(() => {
    if (n) { setShown(n); setLeaving(false); return; }
    if (!shown) return;
    setLeaving(true);
    const t = setTimeout(() => { setShown(0); setLeaving(false); }, 150);
    return () => clearTimeout(t);
  }, [n]); // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    if (!n || readOnly) return;
    const k = (e: KeyboardEvent) => {
      if (e.key !== "Enter" || !e.shiftKey || isTyping(e.target) || details) return;
      if (document.querySelector("[role=dialog],[data-ctx-menu]")) return;
      e.preventDefault();
      void commit(!e.altKey);
    };
    window.addEventListener("keydown", k);
    return () => window.removeEventListener("keydown", k);
  });

  if (!shown) return null;
  const count = n || shown;
  // with a side panel open, centre the bar in the canvas strip left of the panel instead of over it
  const strip: CSSProperties | undefined = panelOpen && !inline
    ? { left: "calc((100% - min(903px, 100% - 56px)) / 2)", minWidth: 0, maxWidth: "calc(100% - min(903px, 100% - 56px) - 16px)" }
    : undefined;
  return (
    <>
      <div role="region" aria-label="Staged changes" data-testid="changes-banner" style={strip}
        className={cx(inline ? "cv-banner-inline sticky top-2 z-30 mx-4 mb-2 animate-rise-in" : "cv-banner absolute left-1/2 top-[7px] -translate-x-1/2 z-30 w-[calc(100%-32px)] max-w-[620px] sm:w-auto sm:min-w-[420px]",
          "min-w-0 rounded-md border border-pink-200 px-4 py-2", leaving && !inline && "cv-leaving")}>
        <div className="flex min-h-[34px] flex-col gap-3 py-2 sm:flex-row sm:items-center sm:gap-6 sm:py-0">
          <span className="flex-1 whitespace-nowrap text-sm font-medium leading-5 text-pink-600">Apply {plural(count, "change")}</span>
          <div className="flex items-center gap-2">
            <Button size="md" variant="default" onClick={() => setDetails(true)} className="max-sm:flex-1">Details</Button>
            {!readOnly && (
              <Tooltip content={<span>Deploy. Hold {isMac() ? "⌥" : "Alt"} to commit without deploying.</span>} side="bottom">
                <Button variant="primary" busy={busy === "deploy" || busy === "commit"} onClick={(e) => void commit(!e.altKey)} className="max-sm:flex-1">
                  Deploy<ShiftEnter />
                </Button>
              </Tooltip>
            )}
            {!readOnly && (
              <button type="button" aria-label="More change actions" aria-haspopup="menu"
                onClick={(e) => { const r = e.currentTarget.getBoundingClientRect(); setMenu({ x: r.right - 220, y: r.bottom + 6 }); }}
                className="cv-press flex h-[34px] w-5 items-center justify-center rounded-md text-fg-2 hover:text-foreground">
                <MoreVertical size={16} />
              </button>
            )}
          </div>
        </div>
        {error && <p role="alert" className="flex items-center gap-1.5 pb-1 text-caption text-red-600"><CircleAlert size={14} />{error}</p>}
      </div>
      {menu && (
        <CtxMenu x={menu.x} y={menu.y} onClose={() => setMenu(undefined)}
          items={[{ label: busy === "discard" ? "Discarding..." : "Discard Changes", icon: <X />, danger: true, onClick: () => void discard() }]} />
      )}
      {details && <Details readOnly={readOnly} changes={changes} busy={busy === "deploy" || busy === "commit"} onClose={() => setDetails(false)}
        onCommit={async (m, d) => { if (await commit(d, m)) setDetails(false); }} />}
    </>
  );
}

// ---- Details -------------------------------------------------------------------------------

const LABELS: Record<string, string> = {
  "source.branch": "Branch", "source.repo": "Repository", "source.image": "Image", "source.rootDir": "Root directory", "source.autoDeploy": "Auto deploy",
  "build.builder": "Builder", "build.buildCommand": "Build command", "build.dockerfilePath": "Dockerfile path", "build.watchPaths": "Watch paths",
  "deploy.startCommand": "Start command", "deploy.preDeployCommand": "Pre-deploy command", "deploy.healthcheckPath": "Healthcheck path",
  "deploy.cronSchedule": "Cron schedule", "deploy.replicas": "Replicas", "deploy.restartPolicy": "Restart policy", "deploy.memoryLimitMb": "Memory limit",
  "deploy.cpuLimit": "CPU limit", "name": "Name", "icon": "Icon",
};
function labelOf(path: string): { label: string; icon: ReactNode } {
  if (path.startsWith("variables.")) return { label: path.slice(10), icon: <Braces size={14} /> };
  const icon = path.startsWith("source.branch") ? <GitBranch size={14} />
    : path.startsWith("source") ? <Container size={14} />
      : path.startsWith("deploy.startCommand") || path.startsWith("build") ? <Terminal size={14} />
        : path.startsWith("volume") ? <HardDrive size={14} />
          : path.includes("Limit") ? <Cpu size={14} /> : <Settings2 size={14} />;
  const last = path.split(".").pop() || path;
  return { label: LABELS[path] || last.replace(/([a-z])([A-Z])/g, "$1 $2").replace(/^./, (c) => c.toUpperCase()), icon };
}
const empty = (v: unknown) => v === undefined || v === null || v === "";
const show = (v: unknown) => (empty(v) ? "" : typeof v === "string" ? v : JSON.stringify(v));

function verb(c: Change): { text: string; cls: string } {
  const t = c.tag || "";
  if (c.kind === "service.create" || c.kind === "volume.create" || t === "new") return { text: "will be created", cls: "text-green-600" };
  if (c.kind === "service.delete" || c.kind === "volume.delete" || t === "removed") return { text: "will be removed", cls: "text-red-600" };
  if (c.kind === "service.rename") return { text: "will be renamed", cls: "text-blue-600" };
  return { text: "will be updated", cls: "text-blue-600" };
}

function Section({ c, svc, readOnly, onDiscard }: { c: Change; svc?: Service; readOnly?: boolean; onDiscard: () => void }) {
  const [open, setOpen] = useState(true);
  const v = verb(c);
  const name = c.service || "Shared variables";
  const settings = c.diff.length;
  return (
    <div className="overflow-hidden rounded-lg border border-line">
      <div className="flex min-h-[66px] items-center gap-3 px-4">
        <button type="button" aria-label={open ? "Collapse" : "Expand"} aria-expanded={open} onClick={() => setOpen(!open)}
          className="cv-press flex h-6 w-6 items-center justify-center rounded text-fg-3 hover:text-foreground">
          <ChevronDown size={16} className={cx("transition-transform duration-200", !open && "-rotate-90")} />
        </button>
        <span className="flex h-6 w-6 items-center justify-center">{svc ? <NodeIcon s={svc} /> : <Braces size={20} className="text-fg-2" />}</span>
        <p className={cx("min-w-0 flex-1 truncate text-base", v.cls)}><span className="font-semibold">{name}</span> {v.text}</p>
        <span className="shrink-0 text-base text-fg-3">{settings ? plural(settings, "Setting") : ""}</span>
        {!readOnly && <Button size="md" onClick={onDiscard}>Discard</Button>}
      </div>
      {open && settings > 0 && (
        <div className="border-t border-line">
          <div className="grid grid-cols-[minmax(0,1fr)_minmax(0,1fr)_minmax(0,1fr)_32px] gap-3 px-4 py-2.5 text-caption text-fg-2">
            <span>Change</span><span>Current Value</span><span>New Value</span><span />
          </div>
          {c.diff.map((d) => {
            const l = labelOf(d.path);
            const added = empty(d.old) && !empty(d.new), removed = !empty(d.old) && empty(d.new);
            return (
              <div key={d.path} className="grid grid-cols-[minmax(0,1fr)_minmax(0,1fr)_minmax(0,1fr)_32px] items-center gap-3 border-t border-gray-100 px-4 py-3 first:border-t-0">
                <span className="flex min-w-0 items-center gap-3 text-base">
                  {added ? <Plus size={14} className="shrink-0 text-green-600" /> : removed ? <Minus size={14} className="shrink-0 text-red-600" /> : <Pencil size={13} className="shrink-0 text-yellow-600" />}
                  <span className={cx("shrink-0", added ? "text-green-600" : removed ? "text-red-600" : "text-yellow-600")}>{l.icon}</span>
                  <span className={cx("truncate", added ? "text-green-600" : removed ? "text-red-600" : "text-yellow-600")} title={d.path}>{l.label}</span>
                </span>
                <span title={show(d.old)} className={cx("flex h-9 min-w-0 items-center truncate rounded px-3 font-mono text-sm", empty(d.old) ? "bg-surface-hover" : removed ? "bg-red-100 text-foreground" : "bg-surface-hover text-fg-2")}>{show(d.old)}</span>
                <span title={show(d.new)} className={cx("flex h-9 min-w-0 items-center truncate rounded px-3 font-mono text-sm text-foreground", empty(d.new) ? "bg-surface-hover" : "bg-green-100")}>{show(d.new)}</span>
                {!readOnly && c.diff.length === 1 ? (
                  <button type="button" aria-label={`Discard ${l.label}`} onClick={onDiscard} className="cv-press flex h-8 w-8 items-center justify-center rounded text-fg-3 hover:bg-surface-hover hover:text-foreground"><X size={16} /></button>
                ) : <span />}
              </div>
            );
          })}
          {c.rewrites && c.rewrites.length > 0 && (
            <div className="border-t border-gray-100 px-4 py-3 text-caption text-fg-3">
              References rewritten: {c.rewrites.map((r) => <code key={r.path} className="ml-1 rounded bg-surface-hover px-1 font-mono text-fg-2">{r.path}</code>)}
            </div>
          )}
          {c.notes?.map((n) => <p key={n} className="flex items-center gap-2 border-t border-gray-100 px-4 py-2.5 text-caption text-yellow-600"><Info size={14} />{n}</p>)}
        </div>
      )}
    </div>
  );
}

function Details({ onClose, onCommit, busy, readOnly, changes }: {
  onClose: () => void; onCommit: (message: string, deploy: boolean) => void; busy: boolean; readOnly?: boolean; changes: ReturnType<typeof useChanges>;
}) {
  const { eid, canvas, reload } = useProject();
  const me = useMe();
  const [error, setError] = useState<unknown>();
  const [message, setMessage] = useState("");
  const input = useRef<HTMLInputElement>(null);
  const data = changes.data;
  useEffect(() => { changes.reload(); }, []); // eslint-disable-line react-hooks/exhaustive-deps
  const drop = async (c: Change) => {
    const before = data;
    changes.setData((d) => ({ ...d, changes: d.changes.filter((x) => x.id !== c.id) }));
    try { await api.discardChange(eid, c.id); void reload(); } catch (e) { if (before) changes.setData(() => before); setError(e); }
  };
  const list = data?.changes || [];
  const total = list.reduce((a, c) => a + Math.max(1, c.diff.length), 0);
  const byId = new Map((canvas?.services || []).map((s) => [s.id, s]));
  const redeploy = [...new Set(list.filter((c) => c.serviceId && c.kind !== "service.delete").map((c) => c.service || ""))].filter(Boolean);
  useEffect(() => { if (data && !list.length) onClose(); }, [data, list.length, onClose]);
  return (
    <Modal bare title={`${plural(total, "change")} to apply`} onClose={onClose} width={1024} className="h-[min(668px,90vh)]">
      <div className="flex h-full min-h-0 flex-col"
        onKeyDown={(e) => { if (e.key === "Enter" && (e.metaKey || e.ctrlKey || e.shiftKey) && !readOnly) { e.preventDefault(); onCommit(message, !e.altKey); } }}>
        <div className="flex h-[74px] shrink-0 items-center justify-between border-b border-line pl-6 pr-12">
          <h2 className="text-h2 text-foreground">{data ? `${plural(total, "change")} to apply` : "Changes to apply"}</h2>
        </div>
        {!readOnly && (
          <label className="flex h-16 shrink-0 items-center gap-4 border-b border-line px-8">
            <Avatar name={me.user.name || me.user.email} size={32} />
            <input ref={input} value={message} onChange={(e) => setMessage(e.target.value)} placeholder="Commit message (optional)" aria-label="Commit message"
              className="h-full min-w-0 flex-1 bg-transparent text-base text-foreground outline-none placeholder:text-fg-3 focus-visible:shadow-none" />
          </label>
        )}
        <div className="min-h-0 flex-1 space-y-4 overflow-y-auto p-8 scroll-thin">
          <ErrorLine error={error} />
          {!data && [0, 1].map((i) => <Skeleton key={i} h={120} className="rounded-lg" />)}
          {list.map((c) => <Section key={c.id} c={c} svc={c.serviceId ? byId.get(c.serviceId) : undefined} readOnly={readOnly} onDiscard={() => void drop(c)} />)}
        </div>
        <div className="flex min-h-[66px] shrink-0 flex-wrap items-center gap-3 bg-surface px-6 py-3">
          <Info size={20} className="shrink-0 text-fg-3" />
          <p className="min-w-0 flex-1 truncate text-sm text-fg-2">
            {redeploy.length ? <><span className="font-semibold text-fg-2">{redeploy.slice(0, 3).join(", ")}{redeploy.length > 3 ? ` and ${redeploy.length - 3} more` : ""}</span> will redeploy</> : "Nothing will redeploy"}
          </p>
          {!readOnly && <>
            <Button variant="ghost" disabled={busy} onClick={() => onCommit(message, false)}>Apply without deploying</Button>
            <Button variant="primary" size="lg" busy={busy} icon={<Check size={18} />} onClick={() => onCommit(message, true)} title={`${isMac() ? "⌘" : "Ctrl"}+Enter`}>Deploy Changes</Button>
          </>}
        </div>
      </div>
    </Modal>
  );
}
