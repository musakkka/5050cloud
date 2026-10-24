// SPDX-License-Identifier: AGPL-3.0-only

import { useState } from "react";
import { Button, Modal } from "../../kit";
import { req } from "../service/client";

/** Paste `heroku config -s` or `--json` output; staged like any other edit. */
export function HerokuImport({ base, onClose, onDone }: { base: string; onClose: () => void; onDone: () => void }) {
  const [text, setText] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  return (
    <Modal title="Import from Heroku" width={560} onClose={onClose}
      footer={<>
        <Button onClick={onClose}>Cancel</Button>
        <Button variant="primary" busy={busy} disabled={!text.trim()} onClick={async () => {
          setBusy(true); setError("");
          try { await req("POST", `${base}/imports/heroku-env`, undefined, text); onDone(); onClose(); }
          catch (e) { setError(e instanceof Error ? e.message : String(e)); setBusy(false); }
        }}>Import</Button>
      </>}>
      <p className="mb-2">Paste the output of <code className="font-mono">heroku config -s</code> or <code className="font-mono">heroku config --json</code>.</p>
      <textarea aria-label="Heroku config" value={text} onChange={(e) => setText(e.target.value)} rows={10} spellCheck={false}
        className="scroll-thin w-full rounded-md border border-gray-200 bg-transparent p-3 font-mono text-sm text-foreground outline-none focus-visible:shadow-none focus:border-pink-400" />
      {error && <div role="alert" className="mt-2 text-sm text-danger-text">{error}</div>}
    </Modal>
  );
}
