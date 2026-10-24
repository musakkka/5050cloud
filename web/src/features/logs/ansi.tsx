// SPDX-License-Identifier: AGPL-3.0-only

// Minimal SGR renderer: colours and bold from ANSI escape sequences, mapped to the palette.

import type { ReactNode } from "react";

const FG: Record<number, string> = {
  30: "text-fg-3", 31: "text-red-600", 32: "text-green-600", 33: "text-yellow-600", 34: "text-blue-600", 35: "text-pink-600", 36: "text-blue-500", 37: "text-gray-700",
  90: "text-fg-3", 91: "text-red-500", 92: "text-green-500", 93: "text-yellow-500", 94: "text-blue-500", 95: "text-pink-500", 96: "text-blue-400", 97: "text-foreground",
};
const BG: Record<number, string> = { 41: "bg-red-300", 42: "bg-green-300", 43: "bg-yellow-300", 44: "bg-blue-300", 45: "bg-pink-300", 46: "bg-blue-200" };

// eslint-disable-next-line no-control-regex
const SGR = /\u001b\[([0-9;]*)m/g;
// eslint-disable-next-line no-control-regex
export const hasAnsi = (s: string) => /\u001b\[[0-9;]*m/.test(s);
// eslint-disable-next-line no-control-regex
export const stripAnsi = (s: string) => s.replace(/\u001b\[[0-9;]*[A-Za-z]/g, "");

export function Ansi({ text }: { text: string }): ReactNode {
  if (!hasAnsi(text)) return text;
  const out: ReactNode[] = [];
  let cls: string[] = [];
  let last = 0;
  let m: RegExpExecArray | null;
  SGR.lastIndex = 0;
  const push = (s: string) => { if (s) out.push(cls.length ? <span key={out.length} className={cls.join(" ")}>{s}</span> : s); };
  while ((m = SGR.exec(text))) {
    push(text.slice(last, m.index));
    last = m.index + m[0].length;
    for (const code of (m[1] || "0").split(";").map(Number)) {
      if (code === 0) cls = [];
      else if (code === 1) cls = [...cls, "font-semibold"];
      else if (FG[code]) cls = [...cls.filter((c) => !c.startsWith("text-")), FG[code]];
      else if (BG[code]) cls = [...cls.filter((c) => !c.startsWith("bg-")), BG[code]];
      else if (code === 39) cls = cls.filter((c) => !c.startsWith("text-"));
    }
  }
  push(text.slice(last));
  return out;
}
