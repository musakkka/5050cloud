// SPDX-License-Identifier: AGPL-3.0-only

import { useState, type ReactNode } from "react";
import { Button, Modal } from "../../ui/kit";
import { Field, Input, Notice } from "../settings/ui";

/** A destructive confirm that needs the name typed in (ConfirmRequest). */
export default function TypedConfirm({ title, name, body, action, onConfirm, onClose }: { title: string; name: string; body: ReactNode; action: string; onConfirm: (typed: string) => Promise<unknown>; onClose: () => void }) {
  const [typed, setTyped] = useState("");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  return (
    <Modal title={title} onClose={onClose} footer={<>
      <Button onClick={onClose}>Cancel</Button>
      <Button variant="danger" disabled={typed !== name || busy} onClick={async () => {
        setBusy(true); setErr(null);
        try { await onConfirm(typed); onClose(); } catch (e: any) { setErr(e?.message || String(e)); setBusy(false); }
      }}>{action}</Button>
    </>}>
      <div className="mb-3 leading-6">{body}</div>
      <Field label={`Type ${name} to confirm`}><Input value={typed} onChange={(e) => setTyped(e.target.value)} autoFocus /></Field>
      {err && <Notice kind="error">{err}</Notice>}
    </Modal>
  );
}
