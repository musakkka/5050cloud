// SPDX-License-Identifier: AGPL-3.0-only

import { useState } from "react";
import type * as T from "../../api/types.gen";
import { Button, Confirm, Empty, Menu, Modal } from "../../ui/kit";
import { v1 } from "../settings/client";
import { checkInvite, hasErrors, inviteBody, type InviteForm } from "../settings/forms";
import { CopyField, Field, Input, Loading, Notice, SaveRow, Section, Select, Toggle, fmtDate, optimistic, useAction, useLoad } from "../settings/ui";

const ROLE_OPTIONS = [{ value: "admin", label: "Admin" }, { value: "member", label: "Member" }, { value: "viewer", label: "Viewer" }];
const roleLabel = (r: string) => r.charAt(0).toUpperCase() + r.slice(1);

export default function MembersSettings({ me, install, onInstall }: { me: T.User; install: T.Install; onInstall: (i: T.Install) => void }) {
  const members = useLoad(v1.members, []);
  const invites = useLoad(v1.invites, []);
  const [inviting, setInviting] = useState(false);
  const [transfer, setTransfer] = useState<T.User | null>(null);
  const [remove, setRemove] = useState<T.User | null>(null);
  const [err, setErr] = useState<string | null>(null);
  if (!members.data || !invites.data) return <Loading error={members.error || invites.error} />;
  const list = members.data;
  const isOwner = me.role === "owner";
  const update = (u: T.User, b: T.UpdateMemberRequest) =>
    optimistic(list, list.map((x) => (x.id === u.id ? { ...x, ...b } as T.User : x)), members.setData, () => v1.updateMember(u.id, b), setErr);
  return (
    <>
      <Section title="Members" actions={<Button variant="primary" onClick={() => setInviting(true)}>Invite</Button>}>
        {err && <Notice kind="error">{err}</Notice>}
        <Toggle label="Require two-factor authentication for everyone" checked={install.requireTotp}
          onChange={(on) => optimistic(install, { ...install, requireTotp: on }, onInstall, () => v1.updateInstall({ requireTotp: on }), setErr)} />
        <div className="rounded-xl border border-gray-200 overflow-hidden mt-2">
          {list.map((u) => (
            <div key={u.id} className="flex items-center gap-3 px-4 py-3 border-b border-gray-100 last:border-b-0 text-sm">
              <div className="flex-1 min-w-0">
                <div className="font-medium truncate">{u.name || u.email}{u.id === me.id && <span className="text-gray-500 font-normal"> (you)</span>}</div>
                <div className="text-xs text-gray-500 truncate">{u.email}{u.totpEnabled ? " · 2FA on" : ""}{u.disabled ? " · disabled" : ""}{!u.emailVerified ? " · email not verified" : ""}</div>
              </div>
              {u.role === "owner" || u.id === me.id ? <span className="text-sm text-gray-600 w-28 text-right">{roleLabel(u.role)}</span> : (
                <>
                  <Select aria-label={`Role for ${u.email}`} className="w-28 h-9" value={u.role} options={ROLE_OPTIONS} onChange={(e) => update(u, { role: e.target.value })} />
                  <Menu trigger={<Button variant="ghost" aria-label={`Actions for ${u.email}`}>More</Button>} items={[
                    { label: u.disabled ? "Enable" : "Disable", onClick: () => update(u, { disabled: !u.disabled }) },
                    ...(isOwner && u.role === "admin" ? [{ label: "Transfer ownership", onClick: () => setTransfer(u) }] : []),
                    { label: "Remove", danger: true, onClick: () => setRemove(u) },
                  ]} />
                </>
              )}
            </div>
          ))}
        </div>
        {isOwner && <p className="text-xs text-gray-500 mt-2">To transfer ownership, make the person an Admin first.</p>}
      </Section>
      <Section title="Pending invites">
        {invites.data.length === 0 ? <Empty>No pending invites.</Empty> : (
          <div className="rounded-xl border border-gray-200 overflow-hidden">
            {invites.data.map((i) => (
              <div key={i.id} className="flex items-center gap-3 px-4 py-3 border-b border-gray-100 last:border-b-0 text-sm">
                <div className="flex-1 min-w-0">
                  <div className="font-medium truncate">{i.email || "Invite link"} · {roleLabel(i.role)}</div>
                  <div className="text-xs text-gray-500">Used {i.uses} of {i.maxUses}. Expires {fmtDate(i.expiresAt)}.</div>
                </div>
                <Button variant="ghost" onClick={() => optimistic(invites.data!, invites.data!.filter((x) => x.id !== i.id), invites.setData, () => v1.deleteInvite(i.id), setErr)}>Revoke</Button>
              </div>
            ))}
          </div>
        )}
      </Section>
      {inviting && <InviteModal smtp={install.smtpReady} onClose={() => setInviting(false)} onCreated={(i) => invites.setData([i, ...(invites.data || [])])} />}
      {transfer && <TransferModal user={transfer} totp={me.totpEnabled} onClose={() => setTransfer(null)} onDone={members.reload} />}
      {remove && <Confirm title="Remove member" danger confirm="Remove" body={<>Remove <b>{remove.email}</b> from this install? Their sessions and tokens stop working.</>}
        onClose={() => setRemove(null)} onConfirm={async () => { await v1.removeMember(remove.id); members.setData(list.filter((x) => x.id !== remove.id)); }} />}
    </>
  );
}

export function InviteModal({ smtp, projectId, onClose, onCreated }: { smtp: boolean; projectId?: string; onClose: () => void; onCreated: (i: T.Invite) => void }) {
  const [f, setF] = useState<InviteForm>({ role: "member", email: "", projectId: projectId || "", maxUses: "1", expiresInHours: "168" });
  const [shown, setShown] = useState(false);
  const [made, setMade] = useState<T.Invite | null>(null);
  const a = useAction();
  const errs = checkInvite(f);
  const submit = async (ev: React.FormEvent) => {
    ev.preventDefault(); setShown(true);
    if (hasErrors(errs)) return;
    const r = await a.run(() => v1.createInvite(inviteBody(f)));
    if (r) { setMade(r); onCreated(r); }
  };
  return (
    <Modal title="Invite people" onClose={onClose} width={560}>
      {made ? (
        <div className="pb-4">
          {made.email && smtp && <Notice kind="ok">We emailed an invite to {made.email}.</Notice>}
          {made.url && <><p className="mb-2">Share this link. It works {made.maxUses === 1 ? "once" : `${made.maxUses} times`} and expires {fmtDate(made.expiresAt)}.</p><CopyField value={made.url} /></>}
          <div className="flex justify-end mt-4"><Button onClick={onClose}>Done</Button></div>
        </div>
      ) : (
        <form onSubmit={submit} noValidate className="pb-4">
          <Field label="Email" error={shown ? errs.email : undefined} hint={smtp ? "Leave empty to create a link instead." : "Email is not set up, so you get a link to copy."}><Input type="email" value={f.email} onChange={(e) => setF({ ...f, email: e.target.value })} /></Field>
          <Field label="Role" error={shown ? errs.role : undefined}><Select value={f.role} options={ROLE_OPTIONS} onChange={(e) => setF({ ...f, role: e.target.value })} /></Field>
          <div className="grid grid-cols-2 gap-3">
            <Field label="Uses" error={shown ? errs.maxUses : undefined}><Input inputMode="numeric" value={f.maxUses} onChange={(e) => setF({ ...f, maxUses: e.target.value })} /></Field>
            <Field label="Expires after (hours)" error={shown ? errs.expiresInHours : undefined}><Input inputMode="numeric" value={f.expiresInHours} onChange={(e) => setF({ ...f, expiresInHours: e.target.value })} /></Field>
          </div>
          <SaveRow {...a} saved={false} label="Create invite" />
        </form>
      )}
    </Modal>
  );
}

function TransferModal({ user, totp, onClose, onDone }: { user: T.User; totp: boolean; onClose: () => void; onDone: () => void }) {
  const [pw, setPw] = useState("");
  const [code, setCode] = useState("");
  const a = useAction();
  return (
    <Modal title="Transfer ownership" onClose={onClose}>
      <form className="pb-4" onSubmit={async (e) => { e.preventDefault(); const ok = await a.run(async () => { await v1.transferOwnership(user.id, { password: pw, code: code.trim() || undefined }); return true; }); if (ok) { onDone(); onClose(); } }}>
        <p className="mb-3"><b>{user.email}</b> becomes the owner and you become an Admin.</p>
        <Field label="Your password"><Input type="password" value={pw} onChange={(e) => setPw(e.target.value)} autoComplete="current-password" /></Field>
        {totp && <Field label="Two-factor code"><Input inputMode="numeric" value={code} onChange={(e) => setCode(e.target.value)} autoComplete="one-time-code" /></Field>}
        <SaveRow {...a} saved={false} disabled={!pw || (totp && !code)} label="Transfer ownership" />
      </form>
    </Modal>
  );
}
