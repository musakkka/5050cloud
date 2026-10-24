// SPDX-License-Identifier: AGPL-3.0-only

import { Globe, MoreVertical, Smartphone } from "lucide-react";
import { useState } from "react";
import type * as T from "../../api/types.gen";
import { Button, Menu, Notice as KitNotice } from "../../kit";
import { v1 } from "../settings/client";
import { checkPassword, hasErrors, isTotpCode } from "../settings/forms";
import { CopyField, Field, FormGrid, Input, Notice, SaveRow, Section, useAction } from "../settings/ui";

export default function SecurityPage({ user, onUser, requireTotp }: { user: T.User; onUser: (u: T.User) => void; requireTotp: boolean }) {
  return (
    <>
      <TwoFactor user={user} onUser={onUser} required={requireTotp} />
      <PasswordForm />
      <Sessions />
    </>
  );
}

function browserName(ua: string): string {
  const b = /Edg\//.test(ua) ? "Edge" : /Firefox\//.test(ua) ? "Firefox" : /Chrome\//.test(ua) ? "Chrome" : /Safari\//.test(ua) ? "Safari" : "Browser";
  const os = /Mac OS/.test(ua) ? "Mac OS" : /Windows/.test(ua) ? "Windows" : /Android/.test(ua) ? "Android" : /iPhone|iPad/.test(ua) ? "iOS" : /Linux/.test(ua) ? "Linux" : "Unknown OS";
  return `${b} (${os})`;
}

function Sessions() {
  return (
    <Section title="Sessions" description="Signing out ends this browser's session. Resetting your password ends every session.">
      <div className="overflow-x-auto rounded-lg border border-line">
        <table className="w-full min-w-[520px] text-left">
          <thead className="bg-tint-header text-caption font-semibold text-fg-2"><tr><th className="h-[43px] px-4">Name</th><th className="px-4">Last Active</th><th className="w-14" /></tr></thead>
          <tbody>
            <tr className="h-[70px] border-t border-gray-100">
              <td className="px-4"><div className="flex items-center gap-3"><Globe size={20} className="text-fg-3" />
                <div><div className="text-lead">{browserName(navigator.userAgent)}</div><div className="text-caption text-fg-3">(This Device)</div></div></div></td>
              <td className="px-4 text-lead">a few seconds ago</td>
              <td className="px-2 text-right">
                <Menu align="right" width={220} trigger={<Button variant="ghost" aria-label="Session options" className="!w-[34px] !px-0"><MoreVertical size={16} /></Button>}
                  items={[{ label: "Sign out", danger: true, onClick: async () => { try { await v1.logout(); } finally { window.location.assign("/login"); } } }]} />
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </Section>
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
        <FormGrid>
          <Field label="Current password" error={shown ? errs.current : undefined}><Input type="password" value={cur} onChange={(e) => setCur(e.target.value)} autoComplete="current-password" /></Field>
          <span className="hidden md:block" />
          <Field label="New password" error={shown ? errs.next : undefined}><Input type="password" value={next} onChange={(e) => setNext(e.target.value)} autoComplete="new-password" /></Field>
          <Field label="New password again" error={shown ? errs.again : undefined}><Input type="password" value={again} onChange={(e) => setAgain(e.target.value)} autoComplete="new-password" /></Field>
        </FormGrid>
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
      {required && !user.totpEnabled && <KitNotice tone="danger" className="mb-4">This install requires two-factor authentication. Turn it on to keep access.</KitNotice>}
      {codes && (
        <>
          <Notice kind="ok">Two-factor authentication is on. Save these recovery codes now. Each works once and they are not shown again.</Notice>
          <div className="mb-6"><CopyField value={codes.join("\n")} /></div>
        </>
      )}
      {user.totpEnabled ? !codes && (
        <form onSubmit={async (e) => { e.preventDefault(); const ok = await a.run(async () => { await v1.totpDisable({ password: pw, code: code.trim() }); return true; }); if (ok) { onUser({ ...user, totpEnabled: false }); setPw(""); setCode(""); } }}>
          <div className="mb-5 flex items-center gap-3 border-y border-gray-100 py-4">
            <Smartphone size={20} className="text-fg-3" /><span className="flex-1 text-lead">Authenticator App</span>
            <span className="text-body text-ok-text">On</span>
          </div>
          {required ? <p className="text-body text-fg-2">An admin requires it for everyone, so it cannot be turned off.</p> : <>
            <FormGrid>
              <Field label="Password"><Input type="password" value={pw} onChange={(e) => setPw(e.target.value)} autoComplete="current-password" /></Field>
              <Field label="Code"><Input inputMode="numeric" value={code} onChange={(e) => setCode(e.target.value)} autoComplete="one-time-code" /></Field>
            </FormGrid>
            <SaveRow {...a} saved={false} disabled={!pw || !isTotpCode(code)} label="Turn off" />
          </>}
        </form>
      ) : setup ? (
        <form onSubmit={async (e) => { e.preventDefault(); const r = await a.run(() => v1.totpEnable({ code: code.replace(/\s/g, "") })); if (r) { setCodes(r.recoveryCodes); setSetup(null); setCode(""); onUser({ ...user, totpEnabled: true }); } }}>
          <p className="mb-4 text-body">Add this account to your authenticator app with the setup key, or open the link on a phone that has one.</p>
          <Field label="Setup key"><CopyField value={setup.secret} /></Field>
          <p className="mb-5 text-body"><a className="text-accent-text hover:underline" href={setup.otpauthUrl}>Open in authenticator app</a></p>
          <Field label="Code from the app" className="max-w-sm"><Input inputMode="numeric" value={code} onChange={(e) => setCode(e.target.value)} autoComplete="one-time-code" /></Field>
          <SaveRow {...a} saved={false} disabled={!/^\d{6}$/.test(code.replace(/\s/g, ""))} label="Turn on" />
        </form>
      ) : (
        <div className="flex items-center gap-3 border-y border-gray-100 py-4">
          <Smartphone size={20} className="text-fg-3" />
          <span className="flex-1 text-lead">Authenticator App</span>
          <Button variant="accent-outline" busy={a.busy} onClick={async () => { const r = await a.run(v1.totpSetup); if (r) setSetup(r); }}>Set up two-factor authentication</Button>
        </div>
      )}
      {a.error && !user.totpEnabled && !setup && <div className="mt-3"><Notice kind="error">{a.error}</Notice></div>}
    </Section>
  );
}
