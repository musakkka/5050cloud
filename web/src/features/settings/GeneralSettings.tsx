// SPDX-License-Identifier: AGPL-3.0-only

import { useEffect, useState } from "react";
import type * as T from "../../api/types.gen";
import { Button } from "../../ui/kit";
import { v1 } from "./client";
import { checkAlerts, checkGeneral, checkSmtp, generalFromInstall, generalPatch, hasErrors, isEmail, smtpBody, splitList, type GeneralForm } from "./forms";
import { Field, Input, Loading, Notice, SaveRow, Section, Select, TextArea, Toggle, useAction, useLoad } from "./ui";

const ALERT_EVENTS: { id: string; label: string }[] = [
  { id: "host.offline", label: "A host goes offline" },
  { id: "disk.high", label: "A host disk is above 85%" },
  { id: "disk.critical", label: "A host disk is above 95%" },
  { id: "backup.stale", label: "Backups have not run on time" },
  { id: "upgrade.rolled_back", label: "An upgrade rolled back" },
  { id: "certificate.failing", label: "A certificate has failed to renew for 24 hours" },
  { id: "agent.version_skew", label: "A host runs a different version" },
  { id: "host.duplicate", label: "Two machines claim the same host identity" },
  { id: "controlplane.readonly", label: "The control plane went read-only" },
];

export default function GeneralSettings() {
  return <><InstallForm /><SmtpForm /><AlertsForm /></>;
}

// mergeLines appends the wanted lines the text does not have yet.
function mergeLines(text: string, add: string[]): string {
  const have = splitList(text);
  return [...have, ...add.filter((x) => !have.includes(x))].join("\n");
}

function InstallForm() {
  const { data, error } = useLoad(v1.install, []);
  const [f, setF] = useState<GeneralForm | null>(null);
  const [before, setBefore] = useState<GeneralForm | null>(null);
  const [shown, setShown] = useState(false);
  const a = useAction();
  useEffect(() => { if (data) { const g = generalFromInstall(data); setF(g); setBefore(g); } }, [data]);
  if (!f || !before || !data) return <Loading error={error} />;
  const errs = checkGeneral(f);
  const set = (k: keyof GeneralForm) => (e: React.ChangeEvent<HTMLInputElement | HTMLTextAreaElement>) => setF({ ...f, [k]: e.target.value });
  const submit = async (ev: React.FormEvent) => {
    ev.preventDefault(); setShown(true);
    if (hasErrors(errs)) return;
    const body = generalPatch(f, before);
    if (!Object.keys(body).length) return;
    const r = await a.run(() => v1.updateInstall(body));
    if (r) { const g = generalFromInstall(r); setF(g); setBefore(g); }
  };
  const err = (k: keyof GeneralForm) => (shown ? errs[k] : undefined);
  return (
    <Section title="Install" description={`Version ${data.version}. Public IP ${data.publicIp}.`}>
      <form onSubmit={submit} noValidate>
        <Field label="Name" error={err("name")}><Input value={f.name} onChange={set("name")} /></Field>
        <Field label="Base domain" error={err("baseDomain")} hint="New services get a domain under this one."><Input value={f.baseDomain} onChange={set("baseDomain")} /></Field>
        <Field label="Console domain" error={err("consoleDomain")} hint="The old domain keeps working for 24 hours after a change."><Input value={f.consoleDomain} onChange={set("consoleDomain")} /></Field>
        <Field label="ACME email" error={err("acmeEmail")} hint="Certificate authorities send expiry notices here."><Input type="email" value={f.acmeEmail} onChange={set("acmeEmail")} /></Field>
        <Field label="Trusted proxies" error={err("trustedProxies")} hint="IP ranges whose X-Forwarded-For header is trusted, one per line."><TextArea value={f.trustedProxies} onChange={set("trustedProxies")} />
          <Button type="button" variant="ghost" onClick={() => setF({ ...f, trustedProxies: mergeLines(f.trustedProxies, data.cloudflareRanges || []) })}>Use Cloudflare ranges</Button></Field>
        <SaveRow {...a} />
      </form>
    </Section>
  );
}

function SmtpForm() {
  const { data, error } = useLoad(v1.smtp, []);
  const [s, setS] = useState<T.SMTPSettings | null>(null);
  const [shown, setShown] = useState(false);
  const [to, setTo] = useState("");
  const [test, setTest] = useState<T.TestResult | null>(null);
  const a = useAction();
  const t = useAction();
  useEffect(() => { if (data) setS({ ...data, port: data.port || 587, tlsMode: data.tlsMode || "starttls", password: "" }); }, [data]);
  if (!s) return <Loading error={error} />;
  const errs = checkSmtp(s);
  const err = (k: keyof T.SMTPSettings) => (shown ? errs[k] : undefined);
  const submit = async (ev: React.FormEvent) => {
    ev.preventDefault(); setShown(true);
    if (hasErrors(errs)) return;
    const r = await a.run(() => v1.setSmtp(smtpBody(s)));
    if (r) setS({ ...r, password: "" });
  };
  return (
    <Section title="Email" description="Without email, invites show a link to copy and password resets run on the control host.">
      <form onSubmit={submit} noValidate>
        <div className="grid grid-cols-1 md:grid-cols-[1fr_120px_160px] gap-x-3">
          <Field label="SMTP host" error={err("host")}><Input value={s.host} onChange={(e) => setS({ ...s, host: e.target.value })} placeholder="smtp.example.com" /></Field>
          <Field label="Port" error={err("port")}><Input inputMode="numeric" value={String(s.port || "")} onChange={(e) => setS({ ...s, port: Number(e.target.value) || 0 })} /></Field>
          <Field label="TLS" error={err("tlsMode")}><Select value={s.tlsMode} onChange={(e) => setS({ ...s, tlsMode: e.target.value })} options={[{ value: "starttls", label: "STARTTLS" }, { value: "tls", label: "TLS" }, { value: "none", label: "None" }]} /></Field>
        </div>
        <Field label="User name"><Input value={s.username} onChange={(e) => setS({ ...s, username: e.target.value })} autoComplete="off" /></Field>
        <Field label="Password" hint="Leave empty to keep the stored password."><Input type="password" value={s.password || ""} onChange={(e) => setS({ ...s, password: e.target.value })} autoComplete="new-password" /></Field>
        <Field label="From address" error={err("from")}><Input value={s.from} onChange={(e) => setS({ ...s, from: e.target.value })} placeholder="5050cloud <noreply@example.com>" /></Field>
        <SaveRow {...a} />
      </form>
      <div className="mt-6 flex flex-col md:flex-row gap-3 md:items-end">
        <div className="flex-1"><Field label="Send a test email to"><Input type="email" value={to} onChange={(e) => setTo(e.target.value)} /></Field></div>
        <Button className="mb-4" disabled={!isEmail(to) || t.busy} onClick={async () => { setTest(null); const r = await t.run(() => v1.testSmtp({ to: to.trim() })); if (r) setTest(r); }}>Send test email</Button>
      </div>
      {test && (test.ok ? <Notice kind="ok">Test email sent.</Notice> : <Notice kind="error">{test.error || "The test email was not sent."}</Notice>)}
      {t.error && <Notice kind="error">{t.error}</Notice>}
    </Section>
  );
}

function AlertsForm() {
  const { data, error } = useLoad(v1.alerts, []);
  const [emails, setEmails] = useState("");
  const [hook, setHook] = useState("");
  const [events, setEvents] = useState<string[]>([]);
  const [shown, setShown] = useState(false);
  const a = useAction();
  useEffect(() => { if (data) { setEmails(data.emails.join("\n")); setHook(data.webhookUrl || ""); setEvents(data.events); } }, [data]);
  if (!data) return <Loading error={error} />;
  const errs = checkAlerts(emails, hook);
  const submit = async (ev: React.FormEvent) => {
    ev.preventDefault(); setShown(true);
    if (hasErrors(errs)) return;
    await a.run(() => v1.setAlerts({ emails: splitList(emails), webhookUrl: hook.trim() || undefined, events }));
  };
  return (
    <Section title="Alerts" description="Platform events sent to these addresses and one webhook.">
      <form onSubmit={submit} noValidate>
        <Field label="Email addresses" error={shown ? errs.emails : undefined} hint="One per line."><TextArea value={emails} onChange={(e) => setEmails(e.target.value)} /></Field>
        <Field label="Webhook URL" error={shown ? errs.webhookUrl : undefined}><Input value={hook} onChange={(e) => setHook(e.target.value)} placeholder="https://example.com/hooks/5050" /></Field>
        <div className="mb-4">
          {ALERT_EVENTS.map((ev) => (
            <Toggle key={ev.id} label={ev.label} checked={events.includes(ev.id)} onChange={(on) => setEvents(on ? [...events, ev.id] : events.filter((x) => x !== ev.id))} />
          ))}
        </div>
        <SaveRow {...a} />
      </form>
    </Section>
  );
}
