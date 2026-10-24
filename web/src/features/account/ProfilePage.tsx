// SPDX-License-Identifier: AGPL-3.0-only

import { CircleCheck, MoreVertical, RefreshCw } from "lucide-react";
import { useState } from "react";
import type * as T from "../../api/types.gen";
import { Avatar, BrandIcon, Button, Menu } from "../../kit";
import { Link } from "react-router-dom";
import { v1 } from "../settings/client";
import { Field, FormGrid, Input, SaveRow, Section, useAction, useLoad } from "../settings/ui";

export default function ProfilePage({ user, onUser }: { user: T.User; onUser: (u: T.User) => void }) {
  return (
    <>
      <InfoSection user={user} onUser={onUser} />
      <IntegrationsSection user={user} />
    </>
  );
}

function InfoSection({ user, onUser }: { user: T.User; onUser: (u: T.User) => void }) {
  const [name, setName] = useState(user.name);
  const a = useAction();
  const dirty = name.trim() !== user.name && !!name.trim();
  return (
    <Section title="Account Information">
      <form onSubmit={async (e) => { e.preventDefault(); const r = await a.run(() => v1.updateMe({ name: name.trim() })); if (r) onUser(r); }}>
        <FormGrid>
          <Field label="Name"><Input value={name} onChange={(e) => setName(e.target.value)} autoComplete="name" /></Field>
          <Field label="Email" hint={user.emailVerified ? "Verified." : "Not verified yet. Email notifications go only to verified addresses."}>
            <Input value={user.email} disabled />
          </Field>
          <Field label="Role"><Input value={user.role} disabled className="capitalize" /></Field>
          <Field label="Avatar">
            <div className="flex items-center gap-6">
              <Input value={user.github ? `github.com/${user.github}.png` : "Initials, from your name"} disabled />
              <Avatar name={name || user.name} src={user.github ? `https://github.com/${user.github}.png?size=80` : undefined} size={40} />
            </div>
          </Field>
        </FormGrid>
        <SaveRow {...a} disabled={!dirty} label="Update Info" />
      </form>
    </Section>
  );
}

function IntegrationsSection({ user }: { user: T.User }) {
  const gh = useLoad(v1.github, []);
  const linked = !!user.github;
  const configure = gh.data?.configureUrl || gh.data?.installUrl;
  return (
    <Section title="Account Integrations">
      <div className="grid grid-cols-1 gap-6 lg:grid-cols-2">
        <div className="space-y-6 rounded-lg border border-gray-100 p-6">
          <div className="flex h-8 items-center justify-between">
            <div className="flex items-center gap-2 text-lead font-semibold"><BrandIcon name="github" size={20} />GitHub</div>
            {linked ? (
              <Menu align="right" width={192} trigger={<Button variant="outline" aria-label="GitHub options" className="!w-[34px] !px-0"><MoreVertical size={16} /></Button>}
                items={[{ label: "Reconnect", icon: <RefreshCw size={16} />, onClick: () => window.location.assign("/api/v1/auth/github") }]} />
            ) : <Button variant="accent-outline" onClick={() => window.location.assign("/api/v1/auth/github")}>Connect</Button>}
          </div>
          {linked ? (
            <div className="space-y-4 text-body">
              <div className="flex items-center gap-2"><CircleCheck size={20} className="text-ok-text" />
                <span>Connected as</span><Avatar name={user.github} src={`https://github.com/${user.github}.png?size=32`} size={16} /><span className="font-medium">{user.github}</span></div>
              <hr className="border-0 border-t border-gray-100" />
              <div className="flex items-center gap-2"><CircleCheck size={20} className="text-ok-text" /><span>Use GitHub to sign in</span></div>
              {gh.data?.appConfigured && (
                <div className="flex items-center gap-2"><CircleCheck size={20} className="text-ok-text" />
                  <span>App installed{gh.data.installations.length ? ` on ${gh.data.installations.join(", ")}` : ""}.</span>
                  {configure && <a className="text-accent-text hover:underline" href={configure} target="_blank" rel="noreferrer">Configure</a>}</div>
              )}
            </div>
          ) : <p className="text-body text-fg-2">Link a GitHub account to sign in with it and to deploy from your repositories.</p>}
        </div>
        <div className="space-y-6 rounded-lg border border-gray-100 p-6">
          <div className="flex h-8 items-center gap-2 text-lead font-semibold"><BrandIcon name="git" size={20} />Git providers</div>
          <p className="text-body text-fg-2">Deploy from GitHub through the app configured for this install. App setup and registry logins are under install settings.</p>
          <Link to="/settings/github" className="text-body text-accent-text hover:underline">Open GitHub settings</Link>
        </div>
      </div>
    </Section>
  );
}
