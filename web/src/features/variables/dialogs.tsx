// SPDX-License-Identifier: AGPL-3.0-only

// Variable dialogs: seal, promote, delete confirms and the generator modal.

import { useState } from "react";
import { EyeOff } from "lucide-react";
import { Button } from "../../kit";
import { Code, ConfirmDialog, Dialog } from "./parts";

export const SECRET_DEFAULT = "${{ secret(32) }}";

export function PromoteDialog({ name, onConfirm, onClose }: { name: string; onConfirm: () => Promise<unknown>; onClose: () => void }) {
  return (
    <ConfirmDialog title="Promote to Shared Variable" confirm="Promote" onConfirm={onConfirm} onClose={onClose}
      body={<>Convert <Code>{name}</Code> into a shared variable that several services can reference. This service will point to it.</>} />
  );
}

export function SealDialog({ name, onConfirm, onClose }: { name: string; onConfirm: () => Promise<unknown>; onClose: () => void }) {
  return (
    <ConfirmDialog title="Seal Variable" confirm="Seal variable" onConfirm={onConfirm} onClose={onClose}
      body={<>
        <p>Sealing <Code>{name}</Code> hides its value for good. Sealed values stay out of the UI and the API
          (<EyeOff size={14} className="mx-0.5 inline align-[-2px]" />) and only reach your running service.</p>
        <p className="mt-3">This cannot be undone. You can still replace the value later.</p>
      </>} />
  );
}

export function DeleteDialog({ name, onConfirm, onClose }: { name: string; onConfirm: () => Promise<unknown>; onClose: () => void }) {
  return (
    <ConfirmDialog title="Delete Variable" confirm="Delete" danger onConfirm={onConfirm} onClose={onClose}
      body={<>Are you sure you want to delete <Code>{name}</Code>? Once deleted, it will be gone forever.</>} />
  );
}

const EXAMPLES = ["${{ secret() }}", "${{ secret(10) }}", '${{ secret(8, "alphabet") }}'];

export function GeneratorDialog({ initial, onSave, onClose }: { initial: string; onSave: (template: string) => Promise<unknown>; onClose: () => void }) {
  const [t, setT] = useState(initial || SECRET_DEFAULT);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const save = async () => {
    setBusy(true); setErr("");
    try { await onSave(t.trim()); onClose(); } catch (e) { setErr(e instanceof Error ? e.message : String(e)); setBusy(false); }
  };
  return (
    <Dialog title="Variable Generator" subtitle="Control how this variable behaves when creating new environments" large width={520} onClose={onClose}
      footer={<Button variant="primary" size="lg" busy={busy} disabled={!t.trim()} onClick={() => void save()}>Save</Button>}>
      <input aria-label="Generator template" autoFocus value={t} onChange={(e) => setT(e.target.value)} spellCheck={false}
        onKeyDown={(e) => { if (e.key === "Enter") void save(); }}
        className="h-[42px] w-full rounded-md border border-gray-200 bg-transparent px-3 py-2 font-mono text-base text-foreground outline-none focus-visible:shadow-none transition-colors hover:border-gray-400 focus:border-pink-400" />
      {err && <div role="alert" className="mt-2 text-sm text-danger-text">{err}</div>}
      <div className="mt-4 rounded-md bg-gray-50 p-4">
        <h3 className="text-sm font-semibold leading-5 text-foreground">How this works</h3>
        <p className="mt-1 text-sm leading-5 text-fg-2">
          When a new environment is created, this variable keeps its value unless a generator function is set.
          With a function, every environment gets its own freshly generated value.
        </p>
        <h3 className="mt-3 text-sm font-semibold leading-5 text-foreground">Examples</h3>
        <div className="mt-2 flex flex-wrap gap-2">
          {EXAMPLES.map((x) => (
            <button key={x} type="button" onClick={() => setT(x)} className="rounded bg-gray-100 px-2 py-0.5 font-mono text-caption text-fg-2 transition-colors hover:bg-gray-200 hover:text-foreground">{x}</button>
          ))}
        </div>
      </div>
    </Dialog>
  );
}
