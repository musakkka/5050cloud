// SPDX-License-Identifier: AGPL-3.0-only

// Activity panel, docked in the project shell's 320px card: header, a multi-select category
// filter, and the environment's feed with outcome icons and colours. Infinite scroll, rise-in
// for live events, click opens the deployment or the commit's diff.
import { useCallback, useEffect, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";
import { Activity as ActivityIcon, CircleAlert, CircleCheck, ChevronDown, Info } from "lucide-react";
import { api, type Activity, type CommitDetail } from "../../api/client";
import { useProject, useProjectEvents } from "../../app/project";
import { EmptyState, ErrorLine, MenuItem, Modal, Popover, RelTime, Skeleton, Spinner, cx } from "../../kit";

const CATEGORIES = [
  { id: "deploys", label: "Deploys" },
  { id: "config", label: "Config changes" },
  { id: "storage", label: "Storage" },
  { id: "extensions", label: "Extensions" },
  { id: "passwords", label: "Database passwords" },
  { id: "autoupdates", label: "Auto-updates" },
  { id: "variables", label: "Variables" },
  { id: "services", label: "Services" },
] as const;
type Cat = (typeof CATEGORIES)[number]["id"];
const ALL = new Set<Cat>(CATEGORIES.map((c) => c.id));

/** Which filter category an event belongs to. */
export function categoryOf(a: Pick<Activity, "kind" | "text">): Cat {
  if (/password/i.test(a.text)) return "passwords";
  if (/auto-?update|upgrade/i.test(a.text)) return "autoupdates";
  if (/variable/i.test(a.text)) return "variables";
  switch (a.kind) {
    case "deployment": return "deploys";
    case "commit": return "config";
    case "backup": return "storage";
    case "host": return "extensions";
    default: return "services";
  }
}

type Outcome = "good" | "bad" | "info";
export function outcomeOf(a: Pick<Activity, "status" | "text">): Outcome {
  const t = `${a.status || ""} ${a.text}`;
  if (/crash|fail|error|unhealthy|offline/i.test(t)) return "bad";
  if (/success|succeeded|ok\b|online|healthy|complete/i.test(t)) return "good";
  return "info";
}
const OUTCOME = {
  good: { icon: CircleCheck, tone: "text-ok-text", text: "text-ok-soft" },
  bad: { icon: CircleAlert, tone: "text-danger-text", text: "text-danger-text" },
  info: { icon: Info, tone: "text-blue-600", text: "text-foreground" },
} as const;

export function ActivityPanel({ onClose }: { onClose: () => void }) {
  const { eid, canvas, pid, withEnv } = useProject();
  const nav = useNavigate();
  const [items, setItems] = useState<Activity[]>();
  const [next, setNext] = useState<string>();
  const [error, setError] = useState<unknown>();
  const [commit, setCommit] = useState<string>();
  const [cats, setCats] = useState<Set<Cat>>(new Set(ALL));
  const [hidden, setHidden] = useState(false);
  const [fresh, setFresh] = useState<Set<string>>(new Set());
  const loading = useRef(false);

  useEffect(() => {
    setItems(undefined); setNext(undefined); setError(undefined);
    api.activity(eid, {}).then((p) => { setItems(p.items || []); setNext(p.next); }, setError);
  }, [eid]);
  useProjectEvents((e) => {
    if (e.type === "activity" && e.activity) {
      const a = e.activity;
      setItems((l) => (l && l.some((x) => x.id === a.id) ? l : [a, ...(l || [])]));
      setFresh((f) => new Set(f).add(a.id));
    }
  });
  const more = useCallback(() => {
    if (!next || loading.current) return;
    loading.current = true;
    api.activity(eid, { before: next }).then((p) => {
      // a page with nothing new ends the feed (guards a server that repeats its cursor)
      setItems((l) => {
        const seen = new Set((l || []).map((x) => x.id));
        const add = (p.items || []).filter((x) => !seen.has(x.id));
        if (!add.length) setNext(undefined); else setNext(p.next);
        return [...(l || []), ...add];
      });
    }, setError).finally(() => { loading.current = false; });
  }, [eid, next]);
  const name = (id?: string) => canvas?.services.find((s) => s.id === id)?.config.name;

  const visible = hidden ? [] : (items || []).filter((a) => cats.has(categoryOf(a)));
  const short = visible.length < 6;
  useEffect(() => { if (next && !hidden && short) more(); }, [next, hidden, short, more]);
  const label = hidden ? "Activity hidden" : cats.size === ALL.size ? "All activity" : cats.size === 0 ? "No activity" : `${cats.size} of ${ALL.size}`;
  const toggle = (c: Cat) => { setHidden(false); setCats((s) => { const n = new Set(s); if (n.has(c)) n.delete(c); else n.add(c); return n; }); };
  const open = (a: Activity) => {
    if (a.kind === "commit" && a.ref) setCommit(a.ref);
    else if (a.kind === "deployment" && a.ref && a.serviceId) nav(withEnv(`/project/${pid}/service/${encodeURIComponent(a.serviceId)}/deployment/${encodeURIComponent(a.ref)}`));
    else if (a.serviceId) nav(withEnv(`/project/${pid}/service/${encodeURIComponent(a.serviceId)}`));
  };

  return (
    <section aria-label="Activity" className="flex h-full flex-col bg-background px-2 py-3">
      <button type="button" onClick={onClose} aria-label="Close activity" className="w-full rounded-md px-3 py-2 text-left text-lead font-semibold text-foreground transition-colors duration-150 hover:bg-surface">Activity</button>
      <div className="min-h-0 flex-1 overflow-y-auto scroll-thin p-2"
        onScroll={(e) => { const t = e.currentTarget; if (t.scrollHeight - t.scrollTop - t.clientHeight < 160) more(); }}>
        <div className="pb-2">
          <Popover align="left" minWidth={220} trigger={(isOpen) => (
            <button type="button" aria-label="Filter activity" className={cx("inline-flex h-[42px] items-center rounded-md border px-2 py-1 text-caption font-medium text-fg-2 transition-colors duration-150", isOpen ? "border-foreground" : "border-line hover:border-fg-4")}>
              {label}<ChevronDown size={16} className="ml-2 text-fg-3" />
            </button>
          )}>
            {() => (
              <>
                {CATEGORIES.map((c) => <MenuItem key={c.id} checkSide="left" active={!hidden && cats.has(c.id)} onClick={() => toggle(c.id)}>{c.label}</MenuItem>)}
                <div className="-mx-1 my-1 border-t border-line" />
                <MenuItem checkSide="left" onClick={() => setHidden(true)}>Hide all activity</MenuItem>
              </>
            )}
          </Popover>
        </div>
        <ErrorLine error={error} />
        {!items && !error && (
          <ul className="flex flex-col gap-3">{[0, 1, 2, 3].map((i) => <li key={i} className="grid grid-cols-[auto_1fr] gap-4 p-4"><Skeleton w={20} h={20} circle /><div className="grid gap-2"><Skeleton h={14} w="70%" /><Skeleton h={14} w="90%" /><Skeleton h={12} w="40%" /></div></li>)}</ul>
        )}
        {items && visible.length === 0 && (
          <EmptyState icon={<ActivityIcon size={24} />} title={hidden ? "Activity is hidden" : items.length ? "Nothing matches this filter" : "No activity yet"}
            body={hidden ? "Pick a category to show the feed again." : items.length ? "Turn on more categories to see more events." : "Deploys, config changes and other events appear here."} />
        )}
        <ul className="flex flex-col gap-3">
          {visible.map((a) => {
            const o = OUTCOME[outcomeOf(a)];
            const Icon = o.icon;
            const svc = name(a.serviceId) || a.actor || a.kind;
            return (
              <li key={a.id} className={cx(fresh.has(a.id) && "animate-rise-in")}>
                <button type="button" onClick={() => open(a)} className="grid w-full grid-cols-[auto_1fr] items-center gap-4 rounded-md p-4 text-left transition-colors duration-150 hover:bg-surface">
                  <Icon size={20} className={o.tone} />
                  <span className="grid min-w-0 gap-2">
                    <span className="flex items-center gap-2 text-body font-semibold text-foreground"><span aria-hidden className="size-2 shrink-0 rounded-full bg-ok" /><span className="line-clamp-2">{svc}</span></span>
                    <span className={cx("text-body", o.text)}>{a.text}</span>
                    <RelTime ts={a.ts} className="text-caption text-fg-3" />
                  </span>
                </button>
              </li>
            );
          })}
        </ul>
        {next && !hidden && <div className="flex justify-center py-3"><Spinner /></div>}
      </div>
      {commit && <CommitModal id={commit} onClose={() => setCommit(undefined)} />}
    </section>
  );
}

const show = (v: unknown) => (v === undefined || v === null ? "(none)" : typeof v === "string" ? v : JSON.stringify(v));

function CommitModal({ id, onClose }: { id: string; onClose: () => void }) {
  const [c, setC] = useState<CommitDetail>();
  const [error, setError] = useState<unknown>();
  useEffect(() => { api.commitDetail(id).then(setC, setError); }, [id]);
  return (
    <Modal title={c?.message || "Commit"} onClose={onClose} width={640}>
      <ErrorLine error={error} />
      {!c && !error && <Spinner />}
      {c && (
        <div className="space-y-4 pb-4">
          <div className="text-xs text-gray-500">{c.author} · <RelTime ts={c.createdAt} /></div>
          {c.changes.map((ch) => (
            <div key={ch.id}>
              <div className="font-medium">{ch.service || "Shared variables"} <span className="text-xs text-gray-500">{ch.kind}</span></div>
              <ul className="mt-1 space-y-1 font-mono text-xs">
                {ch.diff.map((d) => <li key={d.path}><span className="text-gray-500">{d.path}</span>: <span className="text-red-600 line-through">{show(d.old)}</span> {"->"} <span className="text-green-600">{show(d.new)}</span></li>)}
              </ul>
            </div>
          ))}
        </div>
      )}
    </Modal>
  );
}
