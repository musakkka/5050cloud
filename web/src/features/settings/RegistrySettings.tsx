// SPDX-License-Identifier: AGPL-3.0-only

import { useState } from "react";
import { Trash2 } from "lucide-react";
import type * as T from "../../api/types.gen";
import { Button, Confirm, Empty } from "../../ui/kit";
import { v1 } from "./client";
import { checkRegistry, hasErrors } from "./forms";
import { Field, Input, Loading, Notice, SaveRow, Section, fmtDate, useAction, useLoad } from "./ui";

const blank: T.CreateRegistryCredentialRequest = { name: "", server: "", username: "", password: "" };

export default function RegistrySettings() {
  const { data, error, setData } = useLoad(v1.registryCredentials, []);
  const [f, setF] = useState(blank);
  const [shown, setShown] = useState(false);
  const [del, setDel] = useState<T.RegistryCredential | null>(null);
  const a = useAction();
  if (!data) return <Loading error={error} />;
  const errs = checkRegistry(f);
  const submit = async (ev: React.FormEvent) => {
    ev.preventDefault(); setShown(true);
    if (hasErrors(errs)) return;
    const r = await a.run(() => v1.addRegistryCredential({ ...f, name: f.name.trim(), server: f.server.trim(), username: f.username.trim() }));
    if (r) { setData([r, ...data]); setF(blank); setShown(false); }
  };
  return (
    <Section title="Registry credentials" description="Used to pull private Docker images. Passwords are stored encrypted and never shown again.">
      {data.length === 0 ? <Empty>No credentials yet.</Empty> : (
        <div className="rounded-xl border border-gray-200 overflow-hidden mb-8">
          {data.map((c) => (
            <div key={c.id} className="flex items-center gap-3 px-4 py-3 border-b border-gray-100 last:border-b-0 text-sm">
              <div className="flex-1 min-w-0">
                <div className="font-medium truncate">{c.name}</div>
                <div className="text-xs text-gray-500 truncate">{c.username} at {c.server}. Last used {fmtDate(c.lastUsedAt)}.</div>
                <div className="text-xs text-gray-500">{c.usedBy.length ? `Used by ${c.usedBy.length} service${c.usedBy.length === 1 ? "" : "s"}.` : "Not used by any service."}</div>
              </div>
              <Button variant="ghost" aria-label={`Delete ${c.name}`} disabled={c.usedBy.length > 0} title={c.usedBy.length ? "Remove it from the services that use it first." : undefined} onClick={() => setDel(c)}><Trash2 size={15} /></Button>
            </div>
          ))}
        </div>
      )}
      <h3 className="font-semibold mb-3">Add credential</h3>
      <form onSubmit={submit} noValidate>
        <Field label="Name" error={shown ? errs.name : undefined}><Input value={f.name} onChange={(e) => setF({ ...f, name: e.target.value })} /></Field>
        <Field label="Registry" error={shown ? errs.server : undefined}><Input value={f.server} onChange={(e) => setF({ ...f, server: e.target.value })} placeholder="ghcr.io" /></Field>
        <Field label="User name" error={shown ? errs.username : undefined}><Input value={f.username} onChange={(e) => setF({ ...f, username: e.target.value })} autoComplete="off" /></Field>
        <Field label="Password or access token" error={shown ? errs.password : undefined}><Input type="password" value={f.password} onChange={(e) => setF({ ...f, password: e.target.value })} autoComplete="new-password" /></Field>
        <SaveRow {...a} label="Add credential" />
      </form>
      {del && <Confirm title="Delete credential" danger confirm="Delete" body={<>Delete <b>{del.name}</b>?</>} onClose={() => setDel(null)}
        onConfirm={async () => { await v1.deleteRegistryCredential(del.id); setData(data.filter((x) => x.id !== del.id)); }} />}
      {error && <Notice kind="error">{error}</Notice>}
    </Section>
  );
}
