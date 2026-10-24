// SPDX-License-Identifier: AGPL-3.0-only

// The xterm.js terminal for one instance: attaches the byte link, sends
// keystrokes and resizes, and reports connection status upward.

import { forwardRef, useEffect, useImperativeHandle, useRef, useState } from "react";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import "@xterm/xterm/css/xterm.css";
import { svcPath } from "../service/client";
import type { ShellControl } from "../../api/types.gen";
import { wsLink, type Link, type LinkHandlers } from "./link";
import { TERM_FONT_SIZE, TERM_LINE_HEIGHT, TERM_SCROLLBACK, XTERM_THEME } from "./theme";
import "./console.css";

const MOCK = import.meta.env.VITE_MOCKS === "1";

export type LinkStatus = "connecting" | "open" | "exited" | "closed";

export interface TerminalHandle {
  /** Types text into the shell, as if the user typed it. */
  type(text: string): void;
  focus(): void;
}

interface Props {
  eid: string;
  sid: string;
  replica: string;
  session: string;
  /** Bumped by the Reconnect action; a new attempt keeps the scrollback. */
  attempt: number;
  onStatus: (status: LinkStatus, exit?: number) => void;
}

const TERMINAL_OPTIONS = {
  fontFamily: 'ui-monospace, SFMono-Regular, "SF Mono", Consolas, "Liberation Mono", Menlo, monospace',
  fontSize: TERM_FONT_SIZE,
  lineHeight: TERM_LINE_HEIGHT,
  cursorBlink: true,
  cursorStyle: "block" as const,
  cursorInactiveStyle: "outline" as const,
  scrollback: TERM_SCROLLBACK,
  macOptionIsMeta: true,
  theme: XTERM_THEME,
};

const TerminalPane = forwardRef<TerminalHandle, Props>(function TerminalPane({ eid, sid, replica, session, attempt, onStatus }, ref) {
  const host = useRef<HTMLDivElement>(null);
  const term = useRef<Terminal | null>(null);
  const fit = useRef<FitAddon | null>(null);
  const link = useRef<Link | null>(null);
  const statusRef = useRef(onStatus);
  const attached = useRef<string | null>(null);
  const [ready, setReady] = useState(false);

  useEffect(() => { statusRef.current = onStatus; });

  useImperativeHandle(ref, () => ({
    type: (text) => link.current?.send(text),
    focus: () => term.current?.focus(),
  }), []);

  // One xterm per mount. It is created on the next frame so that React's double
  // effect run (StrictMode) cancels the first one before xterm starts its timers.
  useEffect(() => {
    const el = host.current;
    if (!el) return;
    let t: Terminal | null = null;
    let disposables: { dispose(): void }[] = [];
    let ro: ResizeObserver | null = null;
    const frame = requestAnimationFrame(() => {
      const made = new Terminal(TERMINAL_OPTIONS);
      const f = new FitAddon();
      made.loadAddon(f);
      made.open(el);
      t = made;
      term.current = made;
      fit.current = f;
      const safeFit = () => { try { f.fit(); } catch { /* not laid out yet */ } };
      safeFit();
      disposables = [
        made.onData((d) => link.current?.send(d)),
        made.onResize(({ cols, rows }) => link.current?.control({ resize: [cols, rows] })),
      ];
      // Ctrl/Cmd+C copies when something is selected; otherwise it goes to the shell.
      made.attachCustomKeyEventHandler((e) => {
        if (e.type === "keydown" && (e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "c" && made.hasSelection()) {
          void navigator.clipboard?.writeText(made.getSelection()).catch(() => undefined);
          return false;
        }
        return true;
      });
      ro = new ResizeObserver(safeFit);
      ro.observe(el);
      setReady(true);
    });
    return () => {
      cancelAnimationFrame(frame);
      ro?.disconnect();
      disposables.forEach((d) => d.dispose());
      link.current?.close();
      link.current = null;
      t?.dispose();
      term.current = null;
      fit.current = null;
      attached.current = null;
      setReady(false);
    };
  }, []);

  // Attach (or re-attach) the link whenever the instance, session or attempt changes.
  useEffect(() => {
    const t = term.current;
    if (!ready || !t) return;
    let live = true;
    let ended = false;
    const key = `${replica}/${session}`;
    if (attached.current !== key) {
      t.reset();
      attached.current = key;
    } else {
      t.write("\r\n\x1b[90mreconnecting...\x1b[0m\r\n");
    }
    statusRef.current("connecting");

    const handlers: LinkHandlers = {
      onOpen: () => {
        if (!live) return;
        statusRef.current("open");
        t.focus();
      },
      onData: (bytes) => { if (live) t.write(bytes); },
      onControl: (frame) => {
        if (!live) return;
        try {
          const c = JSON.parse(frame) as ShellControl;
          if (c.exit !== undefined) {
            ended = true;
            statusRef.current("exited", c.exit);
          }
        } catch { /* not a control frame */ }
      },
      onClose: () => {
        if (live && !ended) statusRef.current("closed");
      },
    };

    const attach = (l: Link) => {
      if (!live) { l.close(); return; }
      link.current = l;
    };
    if (MOCK) {
      void import("./mocks").then((m) => attach(m.mockLink(handlers)));
    } else {
      const params = new URLSearchParams({
        replica,
        cols: String(t.cols),
        rows: String(t.rows),
        session,
      });
      const proto = location.protocol === "https:" ? "wss:" : "ws:";
      attach(wsLink(`${proto}//${location.host}/api/v1${svcPath(eid, sid)}/shell?${params}`, handlers));
    }

    return () => {
      live = false;
      link.current?.close();
      link.current = null;
    };
  }, [ready, eid, sid, replica, session, attempt]);

  return <div ref={host} className="console-term" data-testid="terminal" role="region" aria-label="Terminal" />;
});

export default TerminalPane;
