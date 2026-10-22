// SPDX-License-Identifier: AGPL-3.0-only

import { useState } from "react";
import type * as T from "../../api/types.gen";
import { Button, Confirm, Empty } from "../../ui/kit";
import { v1 } from "../settings/client";
import { checkToken, hasErrors } from "../settings/forms";
import { CopyField, Field, Input, Loading, Notice, SaveRow, Section, Select, fmtDate, useAction, useLoad } from "../settings/ui";

/**
 * API tokens. With environments it makes environment tokens (Project
 * Settings > Tokens); without, account tokens (Settings > API tokens).
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
    <Section title={environments ? "Environment tokens" : "API tokens"} description={environments ? "A token that can deploy, read logs and set variables in one environment. Use it in CI." : "Tokens act as you. Use them with the 5050 CLI or the API."}>
      {secret && <><Notice kind="ok">Copy the token now. It is not shown again.</Notice><div className="mb-6"><CopyField value={secret} /></div></>}
      {rows.length === 0 ? <Empty>No tokens yet.</Empty> : (
        <div className="rounded-xl border border-gray-200 overflow-hidden mb-8">
          {rows.map((t) => (
            <div key={t.id} className="flex items-center gap-3 px-4 py-3 border-b border-gray-100 last:border-b-0 text-sm">
              <div className="flex-1 min-w-0">
                <div className="font-medium truncate">{t.name} <span className="font-mono text-xs text-gray-500">{t.prefix}...</span></div>
                <div className="text-xs text-gray-500">{environments ? `${envName(t.environmentId)}. ` : ""}Created {fmtDate(t.createdAt)}. Last used {fmtDate(t.lastUsed)}. {t.expiresAt ? `Expires ${fmtDate(t.expiresAt)}.` : "Never expires."}</div>
              </div>
              <Button variant="ghost" onClick={() => setDel(t)}>Revoke</Button>
            </div>
          ))}
        </div>
      )}
      <form onSubmit={submit} noValidate>
        <Field label="Name" error={shown ? errs.name : undefined}><Input value={name} onChange={(e) => setName(e.target.value)} placeholder="CI deploys" /></Field>
        {environments && <Field label="Environment" error={shown ? errs.environmentId : undefined}><Select value={env} onChange={(e) => setEnv(e.target.value)} options={environments.map((e) => ({ value: e.id, label: e.name }))} /></Field>}
        <Field label="Expires after (days)" error={shown ? errs.days : undefined} hint="0 means it never expires."><Input inputMode="numeric" value={days} onChange={(e) => setDays(e.target.value)} /></Field>
        <SaveRow {...a} saved={false} label="Create token" />
      </form>
      {del && <Confirm title="Revoke token" danger confirm="Revoke" body={<>Revoke <b>{del.name}</b>? Anything using it stops working.</>} onClose={() => setDel(null)}
        onConfirm={async () => { await v1.deleteToken(del.id); setData(data.filter((x) => x.id !== del.id)); }} />}
    </Section>
  );
}
