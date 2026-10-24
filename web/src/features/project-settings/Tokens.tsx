// SPDX-License-Identifier: AGPL-3.0-only

import { useState } from "react";
import { Check, KeyRound, Trash2 } from "lucide-react";
import type * as T from "../../api/types.gen";
import { Button, Confirm, ExternalLink, IconButton, Input, RelTime, Select, cx, toast } from "../../kit";
import { v1 } from "../settings/client";
import { checkToken } from "../settings/forms";
import { CopyBox } from "./CopyBox";
import { FormLabel, InlineError, LoadError, SectionHead, SectionSkeleton, useAsync, useLoad } from "./ui";

export default function Tokens({ environments, current }: { environments: T.Environment[]; current?: T.Environment }) {
  const { data, error, setData, reload } = useLoad(v1.tokens, []);
  const [name, setName] = useState("");
  const [env, setEnv] = useState(current?.id || environments[0]?.id || "");
  const [secret, setSecret] = useState<string | null>(null);
  const [del, setDel] = useState<T.APIToken | null>(null);
  const a = useAsync();
  if (error) return <LoadError error={error} onRetry={reload} />;
  if (!data) return <SectionSkeleton />;
  const ids = new Set(environments.map((e) => e.id));
  const rows = data.filter((t) => t.scope === "environment" && ids.has(t.environmentId || ""));
  const errs = checkToken(name, "environment", env, "0");
  const ok = !errs.name && !errs.environmentId;
  const envName = (id?: string) => environments.find((e) => e.id === id)?.name || id;
  const create = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!ok) return;
    const r = await a.run(() => v1.createToken({ name: name.trim(), scope: "environment", environmentId: env, expiresInDays: 0 }));
    if (r) { setSecret(r.secret); setData([r.token, ...data]); setName(""); toast("Token created", "success"); }
  };
  return (
    <>
      <section>
        <SectionHead title="New Token" />
        <form onSubmit={create} noValidate className="flex flex-col gap-3 sm:flex-row sm:items-end">
          <div className="min-w-0 flex-1"><FormLabel htmlFor="tk-name">Token Name</FormLabel><Input id="tk-name" value={name} onChange={(e) => setName(e.target.value)} placeholder="My Token" autoComplete="off" maxLength={64} /></div>
          <div className="sm:w-[160px]"><FormLabel htmlFor="tk-env">Environment</FormLabel><Select id="tk-env" variant="field" value={env} onChange={(e) => setEnv(e.target.value)} options={environments.map((e) => ({ value: e.id, label: e.name }))} /></div>
          <Button type="submit" variant="accent-outline" size="lg" busy={a.busy} disabled={!ok}>Create</Button>
        </form>
        <InlineError>{a.error}</InlineError>
        {secret && (
          <div className="mt-6 animate-rise-in rounded-lg border border-line p-4">
            <p className="mb-2 flex items-center gap-2 text-body text-ok-soft"><Check size={16} />Copy the token now. It is not shown again.</p>
            <CopyBox value={secret} />
            <Button variant="ghost" size="sm" className="mt-2" onClick={() => setSecret(null)}>Done</Button>
          </div>
        )}
      </section>
      <section className="mt-12">
        <SectionHead title="Your Tokens" description={<>Anyone with any of your tokens has access to your project environment variables. Be careful. Visit the <ExternalLink href="https://github.com/5050cloud/5050cloud#tokens" className="underline underline-offset-2 hover:text-foreground">Tokens documentation</ExternalLink>.</>} />
        {rows.length === 0 ? (
          <div className="flex min-h-[54px] items-center justify-center rounded-lg border border-dashed border-line p-4 text-body text-fg-2">You have no tokens yet</div>
        ) : (
          <div className="overflow-hidden rounded-lg border border-line">
            {rows.map((t, i) => (
              <div key={t.id} className={cx("flex min-h-[60px] items-center gap-3 px-4 transition-colors hover:bg-tint-hover", i > 0 && "border-t border-surface-hover")}>
                <KeyRound size={16} className="shrink-0 text-fg-4" />
                <div className="min-w-0 flex-1">
                  <div className="truncate text-body font-medium text-foreground">{t.name} <span className="font-mono text-caption text-fg-3">{t.prefix}...</span></div>
                  <div className="text-caption text-fg-3">{envName(t.environmentId)} · created <RelTime ts={t.createdAt} /> · {t.lastUsed ? <>used <RelTime ts={t.lastUsed} /></> : "never used"} · {t.expiresAt ? <>expires {new Date(t.expiresAt).toLocaleDateString()}</> : "never expires"}</div>
                </div>
                <IconButton label={`Revoke ${t.name}`} size={34} onClick={() => setDel(t)}><Trash2 size={16} /></IconButton>
              </div>
            ))}
          </div>
        )}
      </section>
      {del && <Confirm title="Revoke token" danger confirm="Revoke" body={<>Revoke <b>{del.name}</b>? Anything using it stops working.</>} onClose={() => setDel(null)}
        onConfirm={async () => { await v1.deleteToken(del.id); setData(data.filter((x) => x.id !== del.id)); toast("Token revoked", "success"); }} />}
    </>
  );
}
