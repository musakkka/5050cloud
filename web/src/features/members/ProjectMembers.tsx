// SPDX-License-Identifier: AGPL-3.0-only

import { useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { Link2, MoreHorizontal, Plus, Search, Trash2 } from "lucide-react";
import type * as T from "../../api/types.gen";
import { Avatar, Button, Field, IconButton, Input, Menu, Modal, Select, Tabs, cx, toast } from "../../kit";
import { v1 } from "../settings/client";
import { isEmail } from "../settings/forms";
import { CopyBox } from "../project-settings/CopyBox";
import { InlineError, LoadError, SectionHead, SectionSkeleton, useAsync, useLoad } from "../project-settings/ui";

const ACCESS = [{ value: "editor", label: "Can Edit" }, { value: "viewer", label: "Can View" }];
const INSTALL_ROLE: Record<string, string> = { owner: "Install owner", admin: "Install admin", member: "Install member", viewer: "Install viewer" };

/** Project Settings > Members: who has access to this project. */
export default function ProjectMembers({ project, admin }: { project: T.Project; admin: boolean }) {
  const grants = useLoad(() => v1.projectMembers(project.id), [project.id]);
  const users = useLoad(v1.members, []);
  const [search, setSearch] = useState<string | null>(null);
  const [inviting, setInviting] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const rows = useMemo(() => {
    const g = grants.data ?? [];
    const granted = new Set(g.map((x) => x.userId));
    const implicit = (users.data ?? []).filter((u) => !granted.has(u.id) && (u.role === "owner" || u.role === "admin"));
    const q = (search || "").trim().toLowerCase();
    const match = (n: string, e: string) => !q || n.toLowerCase().includes(q) || e.toLowerCase().includes(q);
    return {
      implicit: implicit.filter((u) => match(u.name, u.email)),
      grants: g.filter((x) => match(x.name, x.email)),
      total: g.length + implicit.length,
    };
  }, [grants.data, users.data, search]);
  if (grants.error || users.error) return <LoadError error={(grants.error || users.error)!} onRetry={() => { grants.reload(); users.reload(); }} />;
  if (!grants.data || !users.data) return <SectionSkeleton />;
  const g = grants.data;
  const save = async (next: T.ProjectGrant[]) => {
    const prev = g; grants.setData(next); setErr(null);
    try { grants.setData(await v1.setProjectMembers(project.id, next)); } catch (e: any) { grants.setData(prev); setErr(e?.message || String(e)); }
  };
  const candidates = users.data.filter((u) => (u.role === "member" || u.role === "viewer") && !g.some((x) => x.userId === u.id));
  return (
    <section>
      <SectionHead title="Who has access" />
      <Tabs tabs={[{ id: "people", label: "People", count: rows.total }]} value="people" onChange={() => undefined}
        right={<>
          {search === null
            ? <IconButton label="Search people" size={34} variant="outline" onClick={() => setSearch("")}><Search size={16} /></IconButton>
            : <Input size="sm" autoFocus value={search} onChange={(e) => setSearch(e.target.value)} onBlur={() => !search && setSearch(null)} onKeyDown={(e) => e.key === "Escape" && setSearch(null)} icon={<Search size={14} />} placeholder="Search people" aria-label="Search people" className="w-48 animate-fade-in" />}
          {admin && <Button variant="primary" icon={<Plus size={16} />} onClick={() => setInviting(true)}>Invite</Button>}
        </>} />
      <p className="mb-6 mt-4 text-body text-fg-2">Everyone with an install role also reaches this project. <Link to="/settings/members" className="underline underline-offset-2 hover:text-foreground">Manage in install settings</Link></p>
      <InlineError>{err}</InlineError>
      <div className="overflow-x-auto rounded-lg border border-line">
        <table className="w-full min-w-[520px] text-left">
          <thead className="bg-tint-header text-caption text-fg-2"><tr><th className="w-1/2 px-4 py-3 font-semibold">Who</th><th className="w-[37.5%] px-4 py-3 font-semibold">Access</th><th className="w-[12.5%] px-4 py-3" /></tr></thead>
          <tbody>
            {rows.implicit.map((u) => <Row key={u.id} name={u.name || u.email} email={u.email} access={<span className="text-body text-fg-3">{INSTALL_ROLE[u.role] || u.role}</span>} />)}
            {rows.grants.map((x) => (
              <Row key={x.userId} name={x.name || x.email} email={x.email}
                access={admin ? <Select aria-label={`Access for ${x.email}`} className="w-32" value={x.role} options={ACCESS} onChange={(e) => save(g.map((y) => (y.userId === x.userId ? { ...y, role: e.target.value } : y)))} /> : <span className="text-body text-fg-3">{x.role === "viewer" ? "Can View" : "Can Edit"}</span>}
                actions={admin && <Menu width={200} trigger={<IconButton label={`Actions for ${x.email}`} size={34}><MoreHorizontal size={16} /></IconButton>}
                  items={[{ label: "Remove from project", icon: <Trash2 size={14} />, danger: true, onClick: () => save(g.filter((y) => y.userId !== x.userId)) }]} />} />
            ))}
            {rows.implicit.length + rows.grants.length === 0 && <tr><td colSpan={3} className="px-4 py-8 text-center text-body text-fg-3">{search ? "No one matches that search." : "Nobody has been added to this project yet."}</td></tr>}
          </tbody>
        </table>
      </div>
      {admin && candidates.length > 0 && (
        <div className="mt-6 flex max-w-sm items-end gap-2">
          <Field className="flex-1" label="Add an existing member">
            <Select variant="field" value="" onChange={(e) => { const u = users.data!.find((x) => x.id === e.target.value); if (u) void save([...g, { userId: u.id, email: u.email, name: u.name, role: "viewer" }]); }}
              options={[{ value: "", label: "Choose a person" }, ...candidates.map((u) => ({ value: u.id, label: `${u.name || u.email} (${u.role})` }))]} />
          </Field>
        </div>
      )}
      {inviting && <InviteToProject project={project} onClose={() => setInviting(false)} />}
    </section>
  );
}

function Row({ name, email, access, actions }: { name: string; email: string; access: React.ReactNode; actions?: React.ReactNode }) {
  return (
    <tr className="h-[57px] border-t border-surface-hover transition-colors hover:bg-tint-hover">
      <td className="px-4"><div className="flex items-center gap-3"><span className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md border border-line-popover"><Avatar name={name} size={20} /></span>
        <div className="min-w-0"><div className="truncate text-body text-foreground">{name}</div>{name !== email && <div className="truncate text-caption text-fg-3">{email}</div>}</div></div></td>
      <td className="px-4">{access}</td>
      <td className="px-4 text-right">{actions}</td>
    </tr>
  );
}

function InviteToProject({ project, onClose }: { project: T.Project; onClose: () => void }) {
  const [email, setEmail] = useState("");
  const [perm, setPerm] = useState("editor");
  const [link, setLink] = useState<string | null>(null);
  const [sent, setSent] = useState(false);
  const a = useAsync();
  const l = useAsync();
  const role = perm === "viewer" ? "viewer" : "member";
  const emailOk = isEmail(email);
  const invite = async (ev: React.FormEvent) => {
    ev.preventDefault();
    if (!emailOk) return;
    const r = await a.run(() => v1.createInvite({ role, email: email.trim(), projectId: project.id, maxUses: 1, expiresInHours: 168 }));
    if (r) { setSent(true); if (r.url) setLink(r.url); toast(`Invite sent to ${email.trim()}`, "success"); }
  };
  const copy = async () => {
    const r = await l.run(() => v1.createInvite({ role, projectId: project.id, maxUses: 1, expiresInHours: 168 }));
    if (r?.url) { setLink(r.url); void navigator.clipboard?.writeText(r.url); toast("Invite link copied", "success"); }
  };
  return (
    <Modal bare width={448} title="Invite to project" onClose={onClose}>
      <form onSubmit={invite} noValidate className="p-6">
        <h3 className="pr-8 text-h2 text-foreground">Invite to project</h3>
        <div className="mt-4 space-y-4">
          <Field label="Email" error={email && !emailOk ? "Enter a valid email address." : undefined}>
            <Input type="email" value={email} onChange={(e) => { setEmail(e.target.value); setSent(false); }} placeholder="name@company.com" autoFocus autoComplete="off" />
          </Field>
          <Field label="Project permission"><Select variant="field" value={perm} onChange={(e) => setPerm(e.target.value)} options={ACCESS} /></Field>
        </div>
        {link && <div className="mt-4 animate-rise-in"><CopyBox value={link} /></div>}
        <InlineError>{a.error || l.error}</InlineError>
        {sent && !link && <p role="status" className="mt-3 text-caption text-ok-soft">Invite sent. Mail goes out if email is set up for this install.</p>}
        <div className="mt-6 flex items-center justify-between gap-2">
          <Button variant="link" icon={<Link2 size={16} />} busy={l.busy} onClick={copy} className={cx("text-caption")}>Copy invite link</Button>
          <Button type="submit" variant="primary" size="lg" busy={a.busy} disabled={!emailOk}>Invite</Button>
        </div>
      </form>
    </Modal>
  );
}

