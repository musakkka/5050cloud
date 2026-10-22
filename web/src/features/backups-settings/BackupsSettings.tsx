// SPDX-License-Identifier: AGPL-3.0-only

import { useEffect, useState } from "react";
import type * as T from "../../api/types.gen";
import { Button } from "../../ui/kit";
import { v1 } from "../settings/client";
import { backupsBody, checkBackups, hasErrors } from "../settings/forms";
import { CopyField, Field, Input, Loading, Notice, SaveRow, Section, Toggle, fmtDate, useAction, useLoad } from "../settings/ui";

export function lagText(ms?: number): string {
  if (ms === undefined || ms === null) return "Not streaming";
  if (ms < 1000) return "Up to date";
  const s = Math.round(ms / 1000);
  return s < 120 ? `${s} seconds behind` : `${Math.round(s / 60)} minutes behind`;
}

export default function BackupsSettings({ isOwner }: { isOwner: boolean }) {
  const { data, error } = useLoad(v1.backups, []);
  const [b, setB] = useState<T.BackupSettings | null>(null);
  const [shown, setShown] = useState(false);
  const [test, setTest] = useState<T.TestResult | null>(null);
  const a = useAction();
  const t = useAction();
  useEffect(() => { if (data) setB({ ...data, secretAccessKey: "", retentionSecretAccessKey: "" }); }, [data]);
  if (!b || !data) return <Loading error={error} />;
  const stored = !!data.accessKeyId;
  const errs = checkBackups(b, stored);
  const err = (k: keyof T.BackupSettings) => (shown ? errs[k] : undefined);
  const set = (k: keyof T.BackupSettings) => (e: React.ChangeEvent<HTMLInputElement>) => setB({ ...b, [k]: e.target.value });
  const submit = async (ev: React.FormEvent) => {
    ev.preventDefault(); setShown(true);
    if (hasErrors(errs)) return;
    const r = await a.run(() => v1.setBackups(backupsBody(b)));
    if (r) setB({ ...r, secretAccessKey: "", retentionSecretAccessKey: "" });
  };
  return (
    <>
      <Section title="Backup bucket" description="Any S3-compatible bucket. Database, volume and control-plane backups go here, encrypted.">
        <form onSubmit={submit} noValidate>
          <Field label="Endpoint" error={err("endpoint")}><Input value={b.endpoint} onChange={set("endpoint")} placeholder="https://s3.us-east-1.amazonaws.com" /></Field>
          <div className="grid grid-cols-1 md:grid-cols-3 gap-x-3">
            <Field label="Bucket" error={err("bucket")}><Input value={b.bucket} onChange={set("bucket")} /></Field>
            <Field label="Region"><Input value={b.region} onChange={set("region")} placeholder="us-east-1" /></Field>
            <Field label="Prefix"><Input value={b.prefix} onChange={set("prefix")} placeholder="5050/" /></Field>
          </div>
          <h3 className="font-semibold mt-2 mb-1">Write key</h3>
          <p className="text-xs text-gray-500 mb-3">Needs put and get only. Data containers and backup jobs use it.</p>
          <div className="grid grid-cols-1 md:grid-cols-2 gap-x-3">
            <Field label="Access key id" error={err("accessKeyId")}><Input value={b.accessKeyId} onChange={set("accessKeyId")} autoComplete="off" /></Field>
            <Field label="Secret" error={err("secretAccessKey")} hint={stored ? "Leave empty to keep the stored secret." : undefined}><Input type="password" value={b.secretAccessKey || ""} onChange={set("secretAccessKey")} autoComplete="new-password" /></Field>
          </div>
          <h3 className="font-semibold mt-2 mb-1">Retention key (optional)</h3>
          <p className="text-xs text-gray-500 mb-3">Has delete rights. Only retention jobs on the control host use it, so a leaked write key cannot erase backups.</p>
          <div className="grid grid-cols-1 md:grid-cols-2 gap-x-3">
            <Field label="Access key id"><Input value={b.retentionAccessKeyId || ""} onChange={set("retentionAccessKeyId")} autoComplete="off" /></Field>
            <Field label="Secret" error={err("retentionSecretAccessKey")}><Input type="password" value={b.retentionSecretAccessKey || ""} onChange={set("retentionSecretAccessKey")} autoComplete="new-password" /></Field>
          </div>
          <Toggle label="Path-style addressing (MinIO and some other providers need this)" checked={b.pathStyle} onChange={(v) => setB({ ...b, pathStyle: v })} />
          <Toggle label="Back up the control plane state every hour and stream it with Litestream" checked={b.stateBackups} onChange={(v) => setB({ ...b, stateBackups: v })} />
          <Toggle label="Store built images in the bucket" checked={b.storeImages} onChange={(v) => setB({ ...b, storeImages: v })} />
          <SaveRow {...a} extra={<Button type="button" disabled={t.busy || !stored} onClick={async () => { setTest(null); const r = await t.run(v1.testBackups); if (r) setTest(r); }}>Test bucket</Button>} />
        </form>
        {test && <div className="mt-3">{test.ok ? <Notice kind="ok">The bucket accepted a write, a read and a list.</Notice> : <Notice kind="error">{test.error || "The bucket test failed."}</Notice>}</div>}
        {t.error && <div className="mt-3"><Notice kind="error">{t.error}</Notice></div>}
      </Section>
      <Section title="Control plane state">
        <div className="grid grid-cols-1 md:grid-cols-3 gap-3 text-sm">
          <Stat label="Last snapshot" value={fmtDate(data.lastStateBackup)} />
          <Stat label="Litestream" value={lagText(data.litestreamLagMs)} />
          <Stat label="Position" value={data.litestreamPosition || "None"} />
        </div>
      </Section>
      {isOwner && <RecoveryKey />}
    </>
  );
}

function Stat({ label, value }: { label: string; value: string }) {
  return <div className="rounded-lg border border-gray-200 p-3"><div className="text-xs text-gray-500">{label}</div><div className="mt-1 font-medium break-all">{value}</div></div>;
}

function RecoveryKey() {
  const [pw, setPw] = useState("");
  const [key, setKey] = useState<string | null>(null);
  const a = useAction();
  return (
    <Section title="Recovery key" description="You need this key to restore the install on a new machine from the bucket. Keep it somewhere safe outside this install.">
      {key ? <CopyField value={key} /> : (
        <form onSubmit={async (e) => { e.preventDefault(); const r = await a.run(() => v1.recoveryKey({ password: pw })); if (r) { setKey(r.recoveryKey); setPw(""); } }}>
          <Field label="Your password"><Input type="password" value={pw} onChange={(e) => setPw(e.target.value)} autoComplete="current-password" /></Field>
          <SaveRow {...a} saved={false} disabled={!pw} label="Show recovery key" />
        </form>
      )}
    </Section>
  );
}
