// SPDX-License-Identifier: AGPL-3.0-only

import { useState } from "react";
import { Check, Copy } from "lucide-react";
import { IconButton } from "../../kit";

/** A mono read-only box with a copy button (tokens, secrets, ids). */
export function CopyBox({ value }: { value: string }) {
  const [done, setDone] = useState(false);
  return (
    <div className="flex min-h-[52px] items-center justify-between gap-3 rounded-lg border border-surface-hover bg-surface-hover px-3 py-2">
      <pre className="min-w-0 flex-1 overflow-x-auto whitespace-pre-wrap break-all font-mono text-body text-foreground">{value}</pre>
      <IconButton label={done ? "Copied" : "Copy"} size={34} onClick={() => { void navigator.clipboard?.writeText(value); setDone(true); setTimeout(() => setDone(false), 1500); }}>
        {done ? <Check size={16} className="text-ok-text" /> : <Copy size={16} />}
      </IconButton>
    </div>
  );
}
