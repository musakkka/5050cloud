// SPDX-License-Identifier: AGPL-3.0-only

import { useState, type ReactNode } from "react";
import { TriangleAlert } from "lucide-react";
import { Button, Input, Modal, cx } from "../../kit";
import { InlineError, WhyDisabled } from "./ui";

/**
 * A destructive confirm that needs the name typed in (ConfirmRequest).
 * Warning icon title, "Type X to confirm", Cancel and a confirm button that stays
 * disabled (with a tooltip) until the text matches exactly.
 */
export default function TypedConfirm({ title, name, body, action, onConfirm, onClose, danger = true, warn = true }: {
  title: string; name: string; body: ReactNode; action: string; onConfirm: (typed: string) => Promise<unknown>; onClose: () => void; danger?: boolean; warn?: boolean;
}) {
  const [typed, setTyped] = useState("");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const ok = typed === name;
  const go = async () => {
    if (!ok || busy) return;
    setBusy(true); setErr(null);
    try { await onConfirm(typed); onClose(); } catch (e: any) { setErr(e?.message || String(e)); setBusy(false); }
  };
  return (
    <Modal bare width={548} onClose={onClose} title={title}>
      <form className="p-6" onSubmit={(e) => { e.preventDefault(); void go(); }}>
        <div className="flex items-center gap-2">
          {warn && <TriangleAlert size={24} className="text-danger" />}
          <h3 className="text-h2 text-foreground">{title}</h3>
        </div>
        <div className="mt-4 text-lead text-foreground [&_b]:font-semibold">{body}</div>
        <p className="mb-2 mt-4 text-body text-fg-2">Type <b className="font-mono font-semibold text-foreground">{name}</b> to confirm</p>
        <Input value={typed} onChange={(e) => setTyped(e.target.value)} autoFocus autoComplete="off" spellCheck={false} aria-label={`Type ${name} to confirm`} />
        <InlineError>{err}</InlineError>
        <div className="mt-6 flex justify-end gap-2">
          <Button size="lg" onClick={onClose}>Cancel</Button>
          <WhyDisabled when={!ok} reason="Input text must match the confirmation text exactly">
            <Button size="lg" type="submit" variant={danger ? "danger" : "primary"} busy={busy} disabled={!ok} className={cx(!ok && "pointer-events-none")}>{action}</Button>
          </WhyDisabled>
        </div>
      </form>
    </Modal>
  );
}
