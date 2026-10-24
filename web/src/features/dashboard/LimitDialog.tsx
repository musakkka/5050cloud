// SPDX-License-Identifier: AGPL-3.0-only

// The "can't create yet" dialog that New opens only for a genuine install limit: the control host
// disk is full (read-only mode) or no host is online to run anything. Two columns: our own artwork
// on the left (a host rack on the dot grid), the reason, what fixes it and the next action on the right.
import { useNavigate } from "react-router-dom";
import { ArrowUpRight, HardDrive, type LucideIcon, Plus, Server, ShieldCheck, Trash2, Wifi } from "lucide-react";
import { Button, Modal } from "../../kit";
import { LINKS } from "../../app/links";

export type Limit = "read-only" | "no-hosts";

const COPY: Record<Limit, {
  kicker: string; step: string; title: string; body: string; heading: string;
  items: { icon: LucideIcon; text: string }[];
}> = {
  "read-only": {
    kicker: "DISK FULL",
    step: "FREE SPACE",
    title: "Free up disk space to create a project",
    body: "The control host disk is full, so this install is read-only. Running services keep serving traffic; changes resume once space is back.",
    heading: "WAYS TO RECOVER",
    items: [
      { icon: Trash2, text: "Prune old images and build cache" },
      { icon: HardDrive, text: "Grow the disk or attach a volume" },
      { icon: Server, text: "Move data services to another host" },
      { icon: ShieldCheck, text: "Backups and logs stay intact" },
    ],
  },
  "no-hosts": {
    kicker: "NO HOSTS ONLINE",
    step: "ADD A HOST",
    title: "Connect a host to run projects",
    body: "No host is online to build or run services. You can still create a project now and deploy once a host joins.",
    heading: "A HOST GIVES YOU",
    items: [
      { icon: Server, text: "Builds and replicas on your own machine" },
      { icon: Wifi, text: "Private networking between services" },
      { icon: HardDrive, text: "Volumes and scheduled backups" },
      { icon: ShieldCheck, text: "One command to join over SSH" },
    ],
  },
};

/** Decorative rack of three host units on the dot grid, with a pulsing link light. */
function Artwork({ kicker, step }: { kicker: string; step: string }) {
  return (
    <div aria-hidden className="relative hidden w-[288px] shrink-0 flex-col justify-between overflow-hidden bg-background p-6 sm:flex">
      <div className="dot-grid absolute inset-0 opacity-60" />
      <div className="absolute -bottom-24 left-1/2 h-72 w-72 -translate-x-1/2 rounded-full bg-pink-500 opacity-[.12] blur-3xl" />
      <div className="relative flex items-center gap-2 font-mono text-caption uppercase tracking-[.12em] text-accent-text">
        <span className="relative flex h-2 w-2"><span className="absolute inset-0 animate-ping rounded-full bg-pink-500 opacity-60" /><span className="relative h-2 w-2 rounded-full bg-pink-500" /></span>
        {kicker}
      </div>
      <div className="relative mx-auto flex w-40 flex-col gap-2">
        <div className="mx-auto h-10 w-px bg-gradient-to-b from-transparent to-pink-500" />
        {[0, 1, 2].map((i) => (
          <div key={i} className="flex h-9 items-center gap-2 rounded-md border border-line bg-surface px-3 shadow-card animate-rise-in" style={{ animationDelay: `${i * 80}ms`, animationFillMode: "both" }}>
            <span className={i === 0 ? "h-1.5 w-1.5 rounded-full bg-pink-500" : "h-1.5 w-1.5 rounded-full bg-fg-5"} />
            <span className="h-1.5 w-1.5 rounded-full bg-fg-5" />
            <span className="ml-auto h-1 w-12 rounded-full bg-tint-pill" />
          </div>
        ))}
      </div>
      <div className="relative font-mono uppercase">
        <div className="text-caption tracking-[.12em] text-fg-4">Next step</div>
        <div className="mt-1 text-xl font-medium tracking-[.06em] text-foreground">{step}</div>
      </div>
    </div>
  );
}

export function LimitDialog({ limit, admin, onClose, onCreateAnyway }: { limit: Limit; admin: boolean; onClose: () => void; onCreateAnyway: () => void }) {
  const nav = useNavigate();
  const c = COPY[limit];
  const primary = admin
    ? { label: limit === "no-hosts" ? "Add a host" : "Review hosts", icon: limit === "no-hosts" ? <Plus size={16} /> : <Server size={16} />, go: () => { onClose(); nav(limit === "no-hosts" ? "/settings/hosts?add=1" : "/settings/hosts"); } }
    : null;
  return (
    <Modal title={c.title} onClose={onClose} width={672} bare>
      <div className="flex min-h-[480px]">
        <Artwork kicker={c.kicker} step={c.step} />
        <div className="flex min-w-0 flex-1 flex-col px-10 pb-4 pt-10">
          <h2 className="text-h1 text-foreground">{c.title}</h2>
          <p className="mt-2 text-lead text-fg-2">{c.body}</p>
          <div className="mt-6 text-caption font-semibold uppercase tracking-[.06em] text-fg-2">{c.heading}</div>
          <ul className="mt-3 flex flex-col gap-3">
            {c.items.map(({ icon: I, text }) => (
              <li key={text} className="flex items-center gap-3 text-sm text-foreground"><I size={16} className="shrink-0 text-accent-muted" />{text}</li>
            ))}
          </ul>
          <div className="mt-auto pt-8">
            {primary
              ? <Button variant="primary" size="lg" block icon={primary.icon} onClick={primary.go}>{primary.label}</Button>
              : <p className="rounded-md border border-line px-3 py-2 text-sm text-fg-2">Ask an admin of this install to {limit === "no-hosts" ? "add a host" : "free up disk space"}.</p>}
            <div className="mt-3 flex items-center justify-center gap-4 text-caption text-fg-3">
              {limit === "no-hosts" && (
                <button type="button" onClick={onCreateAnyway} className="underline-offset-2 transition-colors hover:text-accent-text hover:underline">Create the project anyway</button>
              )}
              <a href={LINKS.docs} target="_blank" rel="noreferrer" className="inline-flex items-center gap-0.5 transition-colors hover:text-accent-text">
                Read the guide<ArrowUpRight size={12} />
              </a>
            </div>
          </div>
        </div>
      </div>
    </Modal>
  );
}
