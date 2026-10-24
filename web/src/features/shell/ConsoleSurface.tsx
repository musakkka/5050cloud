// SPDX-License-Identifier: AGPL-3.0-only

// The console itself: toolbar (instance, Copy SSH command, Full screen, status),
// the terminal, and the Files drawer. The same surface serves the service panel
// tab (variant "panel") and the popout page (variant "popout").

import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { useLocation } from "react-router-dom";
import { Check, Copy, ExternalLink, Terminal } from "lucide-react";
import type { Replica } from "../../api/types.gen";
import { Button, Listbox, cx, toastError } from "../../kit";
import FilesDrawer from "./FilesDrawer";
import TerminalPane, { type LinkStatus, type TerminalHandle } from "./TerminalPane";
import { LIVE_STATES, newSession, popoutHref, sshCommand } from "./theme";
import "./console.css";

export interface ConsoleTarget {
  eid: string;
  sid: string;
  name: string;
  replicas: Replica[];
}

interface SurfaceProps {
  target: ConsoleTarget;
  variant: "panel" | "popout";
  /** Instance to open first when it is still live. */
  initialReplica?: string;
  /** Popout only: where the breadcrumb returns to. */
  backHref?: string;
}

interface Conn {
  status: LinkStatus | "offline";
  exit?: number;
}

const STATUS_LABEL: Record<Conn["status"], string> = {
  connecting: "Connecting",
  open: "Connected",
  closed: "Disconnected",
  exited: "Session ended",
  offline: "Offline",
};

const STATUS_DOT: Record<Conn["status"], string> = {
  connecting: "bg-warn-icon animate-pulse",
  open: "bg-ok",
  closed: "bg-danger-text",
  exited: "bg-gray-400",
  offline: "bg-gray-400",
};

export function ConsoleSurface({ target, variant, initialReplica, backHref }: SurfaceProps) {
  const live = useMemo(() => target.replicas.filter((r) => LIVE_STATES.has(r.state)), [target.replicas]);
  const [pick, setPick] = useState(initialReplica ?? "");
  const replica = live.find((r) => r.id === pick) ?? live[0];

  if (!replica) {
    return (
      <div className={cx("flex h-full min-h-0 flex-col", variant === "panel" ? "-mx-4 sm:-mx-12" : "console-surface")}>
        <div className="console-surface">
          <Toolbar variant={variant} id8="" status={{ status: "offline" }} name={target.name} backHref={backHref} />
        </div>
        <div className="console-surface flex min-h-0 flex-1 flex-col items-center justify-center gap-2 px-6 text-center">
          <Terminal size={20} className="text-fg-4" />
          <p className="text-lead text-fg-2">No running instance</p>
          <p className="max-w-xs text-caption text-fg-4">Deploy this service to open a console in one of its instances.</p>
        </div>
      </div>
    );
  }

  return (
    <ConsoleBody
      key={replica.id}
      target={target}
      variant={variant}
      replica={replica}
      live={live}
      onPick={setPick}
      backHref={backHref}
    />
  );
}

function ConsoleBody({ target, variant, replica, live, onPick, backHref }: SurfaceProps & {
  replica: Replica;
  live: Replica[];
  onPick: (id: string) => void;
}) {
  const [session, setSession] = useState(newSession);
  const [attempt, setAttempt] = useState(0);
  const [conn, setConn] = useState<Conn>({ status: "connecting" });
  const pane = useRef<TerminalHandle>(null);
  const loc = useLocation();
  const id8 = replica.id.slice(0, 8);
  const cmd = sshCommand(target.eid, target.sid, replica.id, live.length > 1);
  const href = popoutHref({ eid: target.eid, sid: target.sid, replica: replica.id, name: target.name, back: loc.pathname + loc.search });

  const onStatus = (status: LinkStatus, exit?: number) => setConn({ status, exit });
  const reconnect = () => {
    if (conn.status === "exited") setSession(newSession());
    setAttempt((n) => n + 1);
  };

  return (
    <div className={cx("flex h-full min-h-0 flex-col", variant === "panel" ? "-mx-4 sm:-mx-12" : "console-surface")}>
      <div className="console-surface shrink-0">
        <Toolbar
          variant={variant}
          id8={id8}
          instanceTitle={replica.id}
          picker={live.length > 1 ? (
            <Listbox
              size="sm"
              label="Instance"
              width={232}
              value={replica.id}
              onChange={onPick}
              options={live.map((r) => ({ value: r.id, label: `${r.id.slice(0, 8)} on ${r.hostName}` }))}
            />
          ) : undefined}
          copyText={cmd}
          popout={href}
          status={conn}
          name={target.name}
          backHref={backHref}
        />
      </div>

      <div className="console-surface relative min-h-0 flex-1 px-4 pb-2 pt-3 sm:px-12">
        <TerminalPane
          ref={pane}
          eid={target.eid}
          sid={target.sid}
          replica={replica.id}
          session={session}
          attempt={attempt}
          onStatus={onStatus}
        />
        {(conn.status === "closed" || conn.status === "exited") && (
          <div role="status" className="animate-rise-in absolute bottom-5 left-1/2 flex -translate-x-1/2 items-center gap-3 whitespace-nowrap rounded-lg border border-line bg-gray-200 py-1.5 pl-3 pr-1.5 text-caption text-fg-2 shadow-popover">
            {conn.status === "exited" ? `The shell exited with code ${conn.exit ?? 0}.` : "The connection dropped. Scrollback is kept."}
            <Button size="xs" variant="secondary" onClick={reconnect}>
              {conn.status === "exited" ? "New session" : "Reconnect"}
            </Button>
          </div>
        )}
      </div>

      {variant === "panel" && (
        <div className="console-surface shrink-0">
          <FilesDrawer eid={target.eid} sid={target.sid} replica={replica.id} onPaste={(t) => { pane.current?.type(t); pane.current?.focus(); }} />
        </div>
      )}
    </div>
  );
}

interface ToolbarProps {
  variant: "panel" | "popout";
  id8: string;
  instanceTitle?: string;
  picker?: ReactNode;
  copyText?: string;
  popout?: string;
  status: Conn;
  name: string;
  backHref?: string;
}

function Toolbar({ variant, id8, instanceTitle, picker, copyText, popout, status, name, backHref }: ToolbarProps) {
  const [copied, setCopied] = useState(false);
  const timer = useRef<number | undefined>(undefined);
  useEffect(() => () => window.clearTimeout(timer.current), []);

  const copy = async () => {
    if (!copyText) return;
    try {
      await navigator.clipboard.writeText(copyText);
      setCopied(true);
      window.clearTimeout(timer.current);
      timer.current = window.setTimeout(() => setCopied(false), 1500);
    } catch {
      toastError(new Error("Could not reach the clipboard. Select the command and copy it by hand."));
    }
  };

  const dot = STATUS_DOT[status.status];
  const label = status.status === "exited" ? `Exited (code ${status.exit ?? 0})` : STATUS_LABEL[status.status];

  return (
    <div className="flex h-[34px] items-center gap-3 px-4 text-caption sm:px-12">
      <div className="flex min-w-0 items-center gap-3">
        {variant === "popout" ? (
          <nav aria-label="Breadcrumb" className="console-mono flex min-w-0 items-center gap-1.5 text-caption">
            <a href={backHref} className="truncate text-fg-3 transition-colors duration-150 hover:text-fg-2">{name}</a>
            <span className="text-fg-5">/</span>
            <span className="text-fg-4" title={instanceTitle}>{id8 || "no instance"}</span>
          </nav>
        ) : (
          <>
            {id8 && <span className="console-mono shrink-0 whitespace-nowrap text-caption text-fg-4" title={instanceTitle}>{id8}</span>}
            {picker}
          </>
        )}
      </div>
      <span className="flex-1" />
      <div className="flex shrink-0 items-center gap-2 sm:gap-3">
        {copyText && (
          <button type="button" onClick={copy} aria-label={copied ? "Copied" : "Copy SSH command"} title={copyText}
            className="flex shrink-0 items-center gap-1 whitespace-nowrap text-fg-3 transition-colors duration-150 hover:text-fg-2">
            {copied ? <Check size={12} className="text-ok-text" /> : <Copy size={12} />}
            <span className="hidden sm:inline">{copied ? "Copied" : "Copy SSH command"}</span>
          </button>
        )}
        {variant === "panel" && popout && (
          <a href={popout} target="_blank" rel="noopener noreferrer" aria-label="Full screen"
            className="flex shrink-0 items-center gap-1 whitespace-nowrap text-fg-3 transition-colors duration-150 hover:text-fg-2">
            <ExternalLink size={12} />
            <span className="hidden sm:inline">Full screen</span>
          </a>
        )}
        <span className="flex items-center gap-2 whitespace-nowrap text-fg-3" role="status">
          <span aria-hidden className={cx("h-1.5 w-1.5 rounded-full", dot)} />
          {label}
        </span>
      </div>
    </div>
  );
}
