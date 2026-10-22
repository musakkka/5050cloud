// SPDX-License-Identifier: AGPL-3.0-only

import { useEffect, useState } from "react";
import { Navigate, useParams } from "react-router-dom";
import type * as T from "../../api/types.gen";
import { Button } from "../../ui/kit";
import { v1 } from "../settings/client";
import { checkPassword, hasErrors, isTotpCode } from "../settings/forms";
import { CopyField, Field, Input, Loading, Notice, Page, SaveRow, Section, useAction, useLoad } from "../settings/ui";

const NAV = [{ to: "/account/profile", label: "Profile" }, { to: "/account/security", label: "Security" }, { to: "/account/notifications", label: "Notifications" }];

export const NOTIFY_EVENTS: { id: string; label: string }[] = [
  { id: "deployment.failed", label: "A deployment failed" },
  { id: "deployment.crashed", label: "A deployment crashed" },
  { id: "backup.failed", label: "A backup failed" },
  { id: "disk.high", label: "A host disk is nearly full" },
  { id: "volume.high", label: "A volume is nearly full" },
  { id: "monitor.triggered", label: "A monitor triggered" },
  { id: "host.offline", label: "A host went offline" },
];

/** Fills in every event so the form always shows a full table. */
export function fullPrefs(p: T.NotificationPref[]): T.NotificationPref[] {
  return NOTIFY_EVENTS.map((e) => p.find((x) => x.event === e.id) || { event: e.id, email: false, inApp: true });
}

export default function AccountPage() {
  const { section = "profile" } = useParams();
  const me = useLoad(v1.me, []);
  if (!NAV.some((n) => n.to.endsWith(`/${section}`))) return <Navigate to="/account/profile" replace />;
  return (
    <Page title="Account" nav={NAV}>
      {!me.data ? <Loading error={me.error} /> : (
        <>
          {section === "profile" && <Profile user={me.data.user} onUser={(user) => me.setData({ ...me.data!, user })} />}
          {section === "security" && <Security user={me.data.user} onUser={(user) => me.setData({ ...me.data!, user })} requireTotp={me.data.install.requireTotp} />}
          {section === "notifications" && <Notifications smtp={me.data.install.smtpReady} />}
        </>
      )}
    </Page>
  );
}

function Profile({ user, onUser }: { user: T.User; onUser: (u: T.User) => void }) {
  const [name, setName] = useState(user.name);
  const a = useAction();
  return (
    <Section title="Profile">
      <form onSubmit={async (e) => { e.preventDefault(); const r = await a.run(() => v1.updateMe({ name: name.trim() })); if (r) onUser(r); }}>
        <Field label="Name"><Input value={name} onChange={(e) => setName(e.target.value)} /></Field>
        <Field label="Email" hint={user.emailVerified ? "Verified." : "Not verified yet. Email notifications go only to verified addresses."}><Input value={user.email} disabled /></Field>
        <Field label="Role"><Input value={user.role} disabled /></Field>
        {user.github && <Field label="GitHub"><Input value={user.github} disabled /></Field>}
        <SaveRow {...a} disabled={!name.trim() || name.trim() === user.name} />
      </form>
    </Section>
  );
}

function Security({ user, onUser, requireTotp }: { user: T.User; onUser: (u: T.User) => void; requireTotp: boolean }) {
  return (
    <>
      <PasswordForm />
      <TwoFactor user={user} onUser={onUser} required={requireTotp} />
      <Section title="Sessions" description="Signing out ends this browser's session. Resetting your password ends every session.">
        <Button onClick={async () => { try { await v1.logout(); } finally { window.location.assign("/login"); } }}>Sign out</Button>
      </Section>
    </>
  );
}

function PasswordForm() {
  const [cur, setCur] = useState("");
  const [next, setNext] = useState("");
  const [again, setAgain] = useState("");
  const [shown, setShown] = useState(false);
  const a = useAction();
  const errs = checkPassword(cur, next, again);
  return (
    <Section title="Password">
      <form noValidate onSubmit={async (e) => {
        e.preventDefault(); setShown(true);
        if (hasErrors(errs)) return;
        const r = await a.run(() => v1.updateMe({ password: next, currentPassword: cur }));
        if (r) { setCur(""); setNext(""); setAgain(""); setShown(false); }
      }}>
        <Field label="Current password" error={shown ? errs.current : undefined}><Input type="password" value={cur} onChange={(e) => setCur(e.target.value)} autoComplete="current-password" /></Field>
        <Field label="New password" error={shown ? errs.next : undefined}><Input type="password" value={next} onChange={(e) => setNext(e.target.value)} autoComplete="new-password" /></Field>
        <Field label="New password again" error={shown ? errs.again : undefined}><Input type="password" value={again} onChange={(e) => setAgain(e.target.value)} autoComplete="new-password" /></Field>
        <SaveRow {...a} label="Change password" />
      </form>
    </Section>
  );
}

function TwoFactor({ user, onUser, required }: { user: T.User; onUser: (u: T.User) => void; required: boolean }) {
  const [setup, setSetup] = useState<T.TOTPSetupResponse | null>(null);
  const [code, setCode] = useState("");
  const [pw, setPw] = useState("");
  const [codes, setCodes] = useState<string[] | null>(null);
  const a = useAction();
  return (
    <Section title="Two-factor authentication" description="Use an authenticator app for a code at every login.">
      {required && !user.totpEnabled && <Notice kind="error">This install requires two-factor authentication. Turn it on to keep access.</Notice>}
      {codes && (
        <>
          <Notice kind="ok">Two-factor authentication is on. Save these recovery codes now. Each works once and they are not shown again.</Notice>
          <div className="mb-6"><CopyField value={codes.join("\n")} /></div>
        </>
      )}
      {user.totpEnabled ? !codes && (
        <form onSubmit={async (e) => { e.preventDefault(); const ok = await a.run(async () => { await v1.totpDisable({ password: pw, code: code.trim() }); return true; }); if (ok) { onUser({ ...user, totpEnabled: false }); setPw(""); setCode(""); } }}>
          <Notice kind="ok">Two-factor authentication is on.</Notice>
          {required ? <p className="text-sm text-gray-600">An admin requires it for everyone, so it cannot be turned off.</p> : <>
            <Field label="Password"><Input type="password" value={pw} onChange={(e) => setPw(e.target.value)} autoComplete="current-password" /></Field>
            <Field label="Code"><Input inputMode="numeric" value={code} onChange={(e) => setCode(e.target.value)} autoComplete="one-time-code" /></Field>
            <SaveRow {...a} saved={false} disabled={!pw || !isTotpCode(code)} label="Turn off" />
          </>}
        </form>
      ) : setup ? (
        <form onSubmit={async (e) => { e.preventDefault(); const r = await a.run(() => v1.totpEnable({ code: code.replace(/\s/g, "") })); if (r) { setCodes(r.recoveryCodes); setSetup(null); setCode(""); onUser({ ...user, totpEnabled: true }); } }}>
          <p className="text-sm mb-2">Add this account to your authenticator app with the setup key, or open the link on a phone that has one.</p>
          <Field label="Setup key"><CopyField value={setup.secret} /></Field>
          <p className="text-sm mb-4"><a className="underline" href={setup.otpauthUrl}>Open in authenticator app</a></p>
          <Field label="Code from the app"><Input inputMode="numeric" value={code} onChange={(e) => setCode(e.target.value)} autoComplete="one-time-code" /></Field>
          <SaveRow {...a} saved={false} disabled={!/^\d{6}$/.test(code.replace(/\s/g, ""))} label="Turn on" />
        </form>
      ) : (
        <>
          <Button variant="primary" disabled={a.busy} onClick={async () => { const r = await a.run(v1.totpSetup); if (r) setSetup(r); }}>Set up two-factor authentication</Button>
          {a.error && <div className="mt-3"><Notice kind="error">{a.error}</Notice></div>}
        </>
      )}
    </Section>
  );
}

function Notifications({ smtp }: { smtp: boolean }) {
  const { data, error } = useLoad(v1.notificationPrefs, []);
  const [prefs, setPrefs] = useState<T.NotificationPref[] | null>(null);
  const a = useAction();
  useEffect(() => { if (data) setPrefs(fullPrefs(data)); }, [data]);
  if (!prefs) return <Loading error={error} />;
  const set = (i: number, p: Partial<T.NotificationPref>) => setPrefs(prefs.map((x, j) => (j === i ? { ...x, ...p } : x)));
  return (
    <Section title="Notifications" description={smtp ? "Choose how you hear about each event." : "Email is not set up on this install, so only in-app notifications are sent."}>
      <form onSubmit={async (e) => { e.preventDefault(); const r = await a.run(() => v1.setNotificationPrefs(prefs)); if (r) setPrefs(fullPrefs(r)); }}>
        <div className="rounded-xl border border-gray-200 overflow-hidden mb-4">
          <div className="grid grid-cols-[1fr_70px_70px] px-4 py-2 text-xs text-gray-500"><span>Event</span><span className="text-center">Email</span><span className="text-center">In app</span></div>
          {prefs.map((p, i) => (
            <div key={p.event} className="grid grid-cols-[1fr_70px_70px] items-center px-4 py-2.5 border-t border-gray-100 text-sm">
              <span>{NOTIFY_EVENTS.find((e) => e.id === p.event)?.label || p.event}</span>
              <span className="text-center"><input type="checkbox" aria-label={`Email for ${p.event}`} checked={p.email} disabled={!smtp} onChange={(e) => set(i, { email: e.target.checked })} /></span>
              <span className="text-center"><input type="checkbox" aria-label={`In app for ${p.event}`} checked={p.inApp} onChange={(e) => set(i, { inApp: e.target.checked })} /></span>
            </div>
          ))}
        </div>
        <SaveRow {...a} />
      </form>
    </Section>
  );
}
