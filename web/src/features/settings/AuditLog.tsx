// SPDX-License-Identifier: AGPL-3.0-only

import { KeyRound, LogIn, Pencil, Plus, Rocket, Search, Terminal, Trash2, type LucideIcon } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import type * as T from "../../api/types.gen";
import { Avatar, Button, EmptyState, Input, Tooltip, cx } from "../../kit";
import { v1 } from "./client";
import { Loading, Section, fmtDate } from "./ui";

type Tone = "accent" | "warn" | "danger" | "ok" | "neutral";
const RING: Record<Tone, string> = {
  accent: "border-pink-300 bg-pink-100 text-accent-text", warn: "border-warn-line bg-warn-bg text-warn-icon",
  danger: "border-danger-line bg-danger-bg text-danger-text", ok: "border-green-200 bg-green-100 text-ok-text", neutral: "border-line bg-surface-hover text-fg-2",
};

/** Picks the icon and ring colour from the action name, e.g. "variables.delete". */
export function describe(action: string): { Icon: LucideIcon; tone: Tone } {
  const verb = action.split(".").pop() || "";
  const noun = action.split(".")[0];
  if (/delete|remove|revoke|destroy/.test(verb)) return { Icon: Trash2, tone: "danger" };
  if (/shell|exec/.test(action)) return { Icon: Terminal, tone: "accent" };
  if (/login|logout|auth/.test(action)) return { Icon: LogIn, tone: "neutral" };
  if (/reveal|token|key/.test(action) || noun === "token") return { Icon: KeyRound, tone: "warn" };
  if (/deploy|rollback|redeploy|restart/.test(action)) return { Icon: Rocket, tone: "ok" };
  if (/create|add|invite/.test(verb)) return { Icon: Plus, tone: "ok" };
  return { Icon: Pencil, tone: "warn" };
}

export default function AuditLog() {
  const [items, setItems] = useState<T.AuditEntry[] | null>(null);
  const [next, setNext] = useState<string | undefined>();
  const [error, setError] = useState<string | null>(null);
  const [q, setQ] = useState("");
  const [more, setMore] = useState(false);
  const load = (before?: string) => { setMore(true); return v1.audit(before).then((p) => { setItems((cur) => [...(before ? cur || [] : []), ...p.items]); setNext(p.next || undefined); }).catch((e) => setError(e.message)).finally(() => setMore(false)); };
  useEffect(() => { void load(); }, []);
  const shown = useMemo(() => {
    const needle = q.trim().toLowerCase();
    return (items || []).filter((a) => !needle || `${a.action} ${a.actor} ${a.target} ${a.detail || ""}`.toLowerCase().includes(needle));
  }, [items, q]);
  if (!items) return <Loading error={error} />;
  return (
    <Section title="Events" description="Every change, reveal, login and shell session. Values are never recorded.">
      <div className="mb-6 flex flex-wrap items-center gap-3">
        <Input size="sm" icon={<Search size={16} />} placeholder="Filter by event, actor or target..." value={q} onChange={(e) => setQ(e.target.value)} className="sm:!w-[320px]" aria-label="Filter events" />
        <span className="text-caption text-fg-3">{shown.length} of {items.length} loaded</span>
      </div>
      {shown.length === 0 ? <EmptyState title={items.length ? "No events match" : "Nothing recorded yet"} /> : (
        <ol className="relative">
          <span aria-hidden className="absolute bottom-4 left-[11px] top-4 border-l border-dotted border-fg-5" />
          {shown.map((a) => {
            const { Icon, tone } = describe(a.action);
            const [project, env] = a.target.split(/[/:]/);
            return (
              <li key={a.id} className="relative flex animate-rise-in gap-4 pb-6">
                <span className={cx("z-10 mt-0.5 flex h-6 w-6 shrink-0 items-center justify-center rounded-full border", RING[tone])}><Icon size={12} /></span>
                <div className="min-w-0">
                  <time className="text-caption text-fg-2" dateTime={new Date(a.ts).toISOString()}>{fmtDate(a.ts)}</time>
                  <p className="mt-0.5 flex flex-wrap items-center gap-x-1.5 text-body text-fg-2">
                    <Tooltip content={a.detail || a.action}><span className="cursor-help font-medium text-foreground underline decoration-dotted underline-offset-4">{a.action}</span></Tooltip>
                    {a.target && <><span>on</span><span className="break-all font-mono text-caption text-foreground">{project}</span>{env && <span className="text-accent-text">{env}</span>}</>}
                    <span>by</span><Avatar name={a.actor} size={16} /><span className="text-foreground">{a.actor}</span>
                    {a.ip && <span className="text-caption text-fg-4">from {a.ip}</span>}
                  </p>
                </div>
              </li>
            );
          })}
        </ol>
      )}
      {next && <Button className="mt-2" busy={more} onClick={() => load(next)}>Load older</Button>}
    </Section>
  );
}
