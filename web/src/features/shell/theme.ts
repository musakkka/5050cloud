// SPDX-License-Identifier: AGPL-3.0-only

// Console constants: the xterm theme, live replica states, status labels, the
// Copy SSH command and the popout link. Colours mirror console.css.

import type { ITheme } from "@xterm/xterm";

export const XTERM_THEME: ITheme = {
  background: "#131520",
  foreground: "#e2e4e9",
  cursor: "#e2e4e9",
  cursorAccent: "#000000",
  selectionBackground: "#5a5c63",
  black: "#1a1b26",
  red: "#f7768e",
  green: "#9ece6a",
  yellow: "#e0af68",
  blue: "#7aa2f7",
  magenta: "#bb9af7",
  cyan: "#7dcfff",
  white: "#c0caf5",
  brightBlack: "#414868",
  brightRed: "#f7768e",
  brightGreen: "#9ece6a",
  brightYellow: "#e0af68",
  brightBlue: "#7aa2f7",
  brightMagenta: "#bb9af7",
  brightCyan: "#7dcfff",
  brightWhite: "#c0caf5",
};

export const TERM_FONT_SIZE = 13;
/** 15 px rows at 13 px type, as the reference measures. */
export const TERM_LINE_HEIGHT = 15 / 13;
export const TERM_SCROLLBACK = 5000;

/** Replica states that accept a shell. */
export const LIVE_STATES: ReadonlySet<string> = new Set(["running", "healthy", "starting", "unhealthy"]);

export function newSession(): string {
  const c = globalThis.crypto as Crypto | undefined;
  if (c && typeof c.randomUUID === "function") return c.randomUUID();
  return Math.random().toString(36).slice(2) + Date.now().toString(36);
}

/** The CLI line the Copy SSH command button puts on the clipboard. */
export function sshCommand(eid: string, sid: string, replicaId: string, showReplica: boolean): string {
  const parts = ["5050", "ssh", "--environment", eid, "--service", sid];
  if (showReplica) parts.push("--replica", replicaId);
  return parts.join(" ");
}

/** Popout page address for one instance. `back` is where the popout's breadcrumb returns to. */
export function popoutHref(opts: { eid: string; sid: string; replica: string; name: string; back: string }): string {
  const p = new URLSearchParams({
    environmentId: opts.eid,
    serviceId: opts.sid,
    replica: opts.replica,
    name: opts.name,
    back: opts.back,
  });
  return `/console-popout?${p.toString()}`;
}

export const DRAWER_KEY = "5050.console.files";
export const DRAWER_HEIGHT_KEY = "5050.console.filesHeight";
export const DRAWER_DEFAULT_HEIGHT = 300;
