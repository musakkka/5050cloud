// SPDX-License-Identifier: AGPL-3.0-only

// Full-panel states: a dim terminal illustration with a two-line message.

import type { ReactNode } from "react";

function Illustration({ dim }: { dim?: boolean }) {
  return (
    <svg width="98" height="82" viewBox="0 0 98 82" fill="none" aria-hidden="true" className={dim ? "opacity-60" : undefined}>
      <rect x="1" y="1" width="96" height="80" rx="8" className="fill-surface stroke-line" strokeWidth="2" />
      <rect x="12" y="12" width="52" height="3" rx="1.5" className="fill-fg-5" />
      <rect x="76" y="12" width="10" height="3" rx="1.5" className="fill-fg-5" />
      {[24, 30, 36, 42, 48, 54, 60, 66].map((y, i) => (
        <rect key={y} x="12" y={y} width={[44, 62, 54, 70, 38, 58, 48, 30][i]} height="2.5" rx="1.25" className="fill-fg-4" opacity={0.9 - i * 0.06} />
      ))}
    </svg>
  );
}

export function LogState({ title, body, dim, children }: { title: string; body: string; dim?: boolean; children?: ReactNode }) {
  return (
    <div className="flex h-full min-h-[260px] flex-col items-center justify-center gap-4 px-6 py-10 text-center animate-fade-in" role="status">
      <Illustration dim={dim} />
      <div>
        <div className={dim ? "text-h3 font-medium text-fg-3" : "text-h3 font-medium text-foreground"}>{title}</div>
        <div className={dim ? "mt-1 text-caption text-fg-4" : "mt-1 text-caption text-fg-2"}>{body}</div>
      </div>
      {children}
    </div>
  );
}

/** Keyframes for freshly tailed rows (the kit flash is blue only). */
export function LogStyles() {
  return (
    <style>{`
      @keyframes log-flash { from { background-color: var(--blue-100); opacity: .6; } to { background-color: transparent; opacity: 1; } }
      @keyframes log-flash-error { from { background-color: hsl(1 45% 12%); opacity: .6; } to { background-color: var(--danger-bg); opacity: 1; } }
      .log-flash { animation: log-flash 1.2s ease-out; }
      .log-flash-error { animation: log-flash-error 1.2s ease-out; }
      .log-row { content-visibility: auto; contain-intrinsic-size: auto 26px; }
      @keyframes log-wave { 0%,100% { background-position-x: 0 } 100% { background-position-x: 8px } }
      .log-wavy { text-decoration: underline wavy var(--danger-text); text-underline-offset: 3px; }
      @media (prefers-reduced-motion: reduce) { .log-flash, .log-flash-error { animation: none; } }
    `}</style>
  );
}
