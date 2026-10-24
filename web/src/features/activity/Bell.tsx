// SPDX-License-Identifier: AGPL-3.0-only

// The bell: in-app notifications. A 380px popover with a Project / All switch (inside a project),
// unresolved warning cards first, resolved cards under their own heading, a kebab with "Mark all
// as read" and a pinned "Edit preferences" footer. Reloads every 60 s and on open.
import { useEffect, useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";
import { Bell as BellIcon, BellOff, Check, Info, MoreVertical, SlidersHorizontal, TriangleAlert } from "lucide-react";
import { api, type Notification } from "../../api/client";
import { useMaybeProject } from "../../app/project";
import { IconButton, Menu, Popover, SegmentedTabs, ShimmerCard, cx, optimistic, timeAgoShort } from "../../kit";

const TITLES: Record<string, string> = {
  "deployment.failed": "Deployment failed to build",
  "deployment.crashed": "Deployment crashed",
  "backup.failed": "Backup failed",
  "disk.high": "Host disk nearly full",
  "volume.high": "Volume nearly full",
  "monitor.triggered": "Monitor triggered",
  "host.offline": "Host went offline",
};
const titleOf = (kind: string) => TITLES[kind] || kind.replace(/[._-]+/g, " ").replace(/^./, (c) => c.toUpperCase());
const isProblem = (kind: string) => /fail|crash|high|offline|trigger|error|lost/i.test(kind);

export function Bell() {
  const proj = useMaybeProject();
  const [items, setItems] = useState<Notification[]>();
  const [tab, setTab] = useState<"project" | "all">("project");
  const nav = useNavigate();
  const load = () => api.notifications().then((p) => setItems(p.items || []), () => setItems((l) => l ?? []));
  useEffect(() => { void load(); const t = setInterval(load, 60000); return () => clearInterval(t); }, []);
  const unread = (items || []).filter((n) => !n.readAt).length;
  const markAll = () => {
    const before = items;
    void optimistic(() => setItems((l) => (l || []).map((n) => ({ ...n, readAt: n.readAt || Date.now() / 1000 }))), () => api.readNotifications("all"), () => setItems(before));
  };
  const scoped = useMemo(() => {
    const all = items || [];
    if (!proj || tab === "all") return all;
    return all.filter((n) => n.link?.includes(`/project/${proj.pid}`));
  }, [items, proj, tab]);
  const open = (n: Notification, close: () => void) => {
    close();
    if (!n.readAt) {
      setItems((l) => (l || []).map((x) => (x.id === n.id ? { ...x, readAt: Date.now() / 1000 } : x)));
      void api.readNotifications([n.id]).catch(() => {});
    }
    if (n.link) nav(n.link);
  };
  const open_ = scoped.filter((n) => !n.readAt);
  const resolved = scoped.filter((n) => n.readAt);

  return (
    <Popover align="right" width={380} className="!p-0" trigger={(isOpen) => (
      <IconButton label={unread ? `Notifications, ${unread} unread` : "Notifications"} size={30} active={isOpen} onClick={() => void load()} className="relative">
        <BellIcon size={16} />
        {unread > 0 && <span className="absolute right-1.5 top-1.5 size-2 rounded-full bg-danger" />}
      </IconButton>
    )}>
      {(close) => (
        <div className="flex min-h-0 flex-col">
          <div className="sticky top-0 z-10 bg-background px-4 pb-4 pt-5">
            <div className="flex items-center justify-between">
              <span className="text-body font-medium text-foreground">Notifications</span>
              <Menu align="right" minWidth={220}
                trigger={<IconButton label="Notification options" size={24}><MoreVertical size={16} /></IconButton>}
                items={[{ label: "Mark all as read", icon: <Check />, onClick: markAll, disabled: unread === 0 }]} />
            </div>
            {proj && <SegmentedTabs className="mt-4" tabs={[{ id: "project", label: "Project" }, { id: "all", label: "All" }]} value={tab} onChange={(t) => setTab(t as "project" | "all")} />}
          </div>
          <div className="px-4 pb-4">
            {!items && <div className="grid gap-2"><ShimmerCard h={82} /><ShimmerCard h={82} /><ShimmerCard h={82} /></div>}
            {items && scoped.length === 0 && (
              <div className="flex flex-col items-center gap-2 rounded-lg border border-dashed border-line px-6 py-10 text-center">
                <BellOff size={24} className="text-fg-4" />
                <p className="text-body font-medium text-foreground">You are all caught up</p>
                <p className="text-caption text-fg-3">Failed deployments, crashes and alerts will show up here.</p>
              </div>
            )}
            {open_.length > 0 && <ul className="grid gap-2">{open_.map((n) => <Card key={n.id} n={n} onOpen={() => open(n, close)} />)}</ul>}
            {resolved.length > 0 && (
              <>
                {open_.length > 0 && <h3 className="my-4 text-body font-medium text-foreground">Resolved</h3>}
                <ul className="grid gap-2">{resolved.map((n) => <Card key={n.id} n={n} onOpen={() => open(n, close)} />)}</ul>
              </>
            )}
          </div>
          <div className="sticky bottom-0 mt-auto border-t border-line-popover bg-background p-4">
            <button type="button" onClick={() => { close(); nav("/account/notifications"); }}
              className="flex items-center gap-2 text-caption font-medium text-fg-2 transition-colors duration-150 hover:text-accent-text">
              <SlidersHorizontal size={16} />Edit preferences
            </button>
          </div>
        </div>
      )}
    </Popover>
  );
}

function Card({ n, onOpen }: { n: Notification; onOpen: () => void }) {
  const resolved = !!n.readAt;
  const problem = isProblem(n.kind);
  const tone = resolved ? "border-line bg-surface text-fg-3 hover:border-fg-5" : problem ? "border-warn-line bg-warn-bg text-warn hover:border-warn" : "border-line bg-surface-hover text-foreground hover:border-fg-5";
  return (
    <li>
      <button type="button" onClick={onOpen} className={cx("grid w-full grid-cols-[auto_1fr] gap-2 rounded-lg border px-4 py-2 text-left transition-colors duration-150", tone)}>
        <span className="mt-0.5 flex size-5 items-center justify-center">
          {resolved ? <Check size={16} /> : problem ? <TriangleAlert size={20} className="text-warn-icon" /> : <Info size={20} className="text-fg-3" />}
        </span>
        <span className="grid min-w-0 gap-1">
          <span className="truncate text-body font-medium">{titleOf(n.kind)}</span>
          <span className={cx("line-clamp-2 text-caption", resolved ? "text-fg-4" : "text-fg-2")}>{n.text}</span>
          <span className={cx("flex justify-end text-caption", resolved ? "text-fg-4" : "text-fg-2")}><time>{timeAgoShort(n.ts)}</time></span>
        </span>
      </button>
    </li>
  );
}
