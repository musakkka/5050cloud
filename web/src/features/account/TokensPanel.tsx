// SPDX-License-Identifier: AGPL-3.0-only

import { KeyRound, Trash2 } from "lucide-react";
import { useState } from "react";
import type * as T from "../../api/types.gen";
import { Button, Confirm, EmptyState, Input as KitInput, Select as KitSelect } from "../../kit";
import { v1 } from "../settings/client";
import { checkToken, hasErrors } from "../settings/forms";
import { CopyField, Field, Loading, Notice, Section, fmtDate, useAction, useLoad } from "../settings/ui";

/**
 * API tokens. With environments it makes environment tokens (Project
 * Settings > Tokens); without, account tokens (Account > Tokens).
 */
export default function TokensPanel({ environments }: { environments?: T.Environment[] }) {
  const { data, error, setData } = useLoad(v1.tokens, []);
  const scope = environments ? "environment" : "account";
  const [name, setName] = useState("");
  const [env, setEnv] = useState(environments?.[0]?.id || "");
  const [days, setDays] = useState("0");
  const [shown, setShown] = useState(false);
  const [secret, setSecret] = useState<string | null>(null);
  const [del, setDel] = useState<T.APIToken | null>(null);
  const a = useAction();
  if (!data) return <Loading error={error} />;
  const envIds = new Set(environments?.map((e) => e.id));
  const rows = data.filter((t) => (environments ? t.scope === "environment" && envIds.has(t.environmentId || "") : t.scope === "account"));
  const errs = checkToken(name, scope, env, days);
  const submit = async (ev: React.FormEvent) => {
    ev.preventDefault(); setShown(true);
    if (hasErrors(errs)) return;
    const r = await a.run(() => v1.createToken({ name: name.trim(), scope, environmentId: environments ? env : undefined, expiresInDays: Number(days || "0") }));
    if (r) { setSecret(r.secret); setData([r.token, ...data]); setName(""); setShown(false); }
  };
  const envName = (id?: string) => environments?.find((e) => e.id === id)?.name || id;
  return (
    <div className="max-w-3xl space-y-12">
      <Section title="New Token" description={environments ? "A token that can deploy, read logs and set variables in one environment. Use it in CI." : "Tokens act as you. Use them with the 5050 CLI or the API."}>
        <form onSubmit={submit} noValidate className="flex flex-col gap-4 md:flex-row md:items-end">
          <Field label="Name" error={shown ? errs.name : undefined} className="!mb-0 md:max-w-[300px]"><KitInput value={name} onChange={(e) => setName(e.target.value)} placeholder="API Token" /></Field>
          {environments
            ? <Field label="Environment" error={shown ? errs.environmentId : undefined} className="!mb-0 md:max-w-[300px]"><KitSelect variant="field" value={env} onChange={(e) => setEnv(e.target.value)} options={environments.map((e) => ({ value: e.id, label: e.name }))} /></Field>
            : <Field label="Expires" className="!mb-0 md:max-w-[300px]"><KitSelect variant="field" value={days} onChange={(e) => setDays(e.target.value)} options={[{ value: "0", label: "Never" }, { value: "30", label: "In 30 days" }, { value: "90", label: "In 90 days" }, { value: "365", label: "In 1 year" }]} /></Field>}
          <Button type="submit" variant="accent-outline" size="lg" busy={a.busy} disabled={!name.trim()} className="md:w-36">Create</Button>
        </form>
        {a.error && <p role="alert" className="mt-3 text-body text-danger-text">{a.error}</p>}
        {secret && <div className="mt-6"><Notice kind="ok">Copy the token now. It is not shown again.</Notice><CopyField value={secret} /></div>}
      </Section>
      <Section title={environments ? "Environment Tokens" : "API Tokens"} description="Anyone holding a token can act with its access. Revoke one the moment it leaks.">
        {rows.length === 0 ? (
          <EmptyState icon={<KeyRound size={24} />} title="You have no tokens yet" />
        ) : (
          <div className="overflow-hidden rounded-lg border border-line">
            {rows.map((t) => (
              <div key={t.id} className="flex items-center gap-3 border-b border-gray-100 px-4 py-3 last:border-b-0 hover:bg-tint-hover">
                <div className="min-w-0 flex-1">
                  <div className="truncate text-lead">{t.name} <span className="font-mono text-caption text-fg-3">{t.prefix}...</span></div>
                  <div className="text-caption text-fg-3">{environments ? `${envName(t.environmentId)}. ` : ""}Created {fmtDate(t.createdAt)}. Last used {fmtDate(t.lastUsed)}. {t.expiresAt ? `Expires ${fmtDate(t.expiresAt)}.` : "Never expires."}</div>
                </div>
                <Button variant="ghost" aria-label={`Revoke ${t.name}`} className="!w-[34px] !px-0" onClick={() => setDel(t)}><Trash2 size={16} /></Button>
              </div>
            ))}
          </div>
        )}
      </Section>
      {del && <Confirm title="Revoke token" danger confirm="Revoke" body={<>Revoke <b>{del.name}</b>? Anything using it stops working.</>} onClose={() => setDel(null)}
        onConfirm={async () => { await v1.deleteToken(del.id); setData(data.filter((x) => x.id !== del.id)); }} />}
    </div>
  );
}
