// SPDX-License-Identifier: AGPL-3.0-only

import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import type * as T from "../../api/types.gen";
import { Button, Empty, Modal } from "../../ui/kit";
import { v1 } from "../settings/client";
import { CRITICAL_EVENTS, WEBHOOK_EVENTS, checkEnvName, checkWebhook, hasErrors, webhookBody } from "../settings/forms";
import { CopyField, Field, Input, Loading, Notice, SaveRow, Section, Select, TextArea, Toggle, fmtDate, optimistic, useAction, useLoad } from "../settings/ui";
import TypedConfirm from "./TypedConfirm";

const STAGED_NOTE = "Changes here are staged. Review and deploy them from the canvas.";

export function General({ project, onProject, admin }: { project: T.Project; onProject: (p: T.Project) => void; admin: boolean }) {
  const [name, setName] = useState(project.name);
  const [desc, setDesc] = useState(project.description);
  const [err, setErr] = useState<string | null>(null);
  const a = useAction();
  const nameOk = /^.{1,64}$/.test(name.trim());
  return (
    <>
      <Section title="Project info">
        <form onSubmit={async (e) => { e.preventDefault(); if (!nameOk) return; const r = await a.run(() => v1.updateProject(project.id, { name: name.trim(), description: desc.trim() })); if (r) onProject(r); }}>
          <Field label="Name" error={nameOk ? null : "Enter a name of up to 64 characters."}><Input value={name} onChange={(e) => setName(e.target.value)} /></Field>
          <Field label="Description"><TextArea className="font-sans" value={desc} onChange={(e) => setDesc(e.target.value)} /></Field>
          <SaveRow {...a} disabled={!nameOk} />
        </form>
        <div className="mt-6"><Field label="Project ID"><CopyField value={project.id} /></Field></div>
      </Section>
      <Section title="Visibility" description="A public project shows its canvas, statuses and deployment list to anyone with the link. Variables, logs and settings stay private.">
        {err && <Notice kind="error">{err}</Notice>}
        <Toggle label="Public read-only" disabled={!admin} checked={project.publicRead}
          onChange={(on) => optimistic(project, { ...project, publicRead: on }, onProject, () => v1.updateProject(project.id, { publicRead: on }), setErr)} />
        {!admin && <p className="text-xs text-gray-500">Only admins can change visibility.</p>}
      </Section>
    </>
  );
}

export function Environments({ project, onProject, admin }: { project: T.Project; onProject: (p: T.Project) => void; admin: boolean }) {
  const [name, setName] = useState("");
  const [mode, setMode] = useState("empty");
  const [from, setFrom] = useState(project.environments[0]?.id || "");
  const [rename, setRename] = useState<T.Environment | null>(null);
  const [del, setDel] = useState<T.Environment | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const a = useAction();
  const envs = project.environments;
  const nameErr = name ? checkEnvName(name, envs.map((e) => e.name)) : null;
  const setEnvs = (list: T.Environment[]) => onProject({ ...project, environments: list });
  return (
    <Section title="Environments">
      {err && <Notice kind="error">{err}</Notice>}
      <div className="rounded-xl border border-gray-200 overflow-hidden mb-6">
        {envs.map((e) => (
          <div key={e.id} className="flex flex-wrap items-center gap-3 px-4 py-3 border-b border-gray-100 last:border-b-0 text-sm">
            <div className="flex-1 min-w-0"><div className="font-medium">{e.name}</div><div className="text-xs text-gray-500">Created {fmtDate(e.createdAt)}</div></div>
            <Toggle label="Block outbound traffic" disabled={!admin} checked={e.egress === "blocked"}
              onChange={(on) => optimistic(envs, envs.map((x) => (x.id === e.id ? { ...x, egress: on ? "blocked" : "allowed" } : x)), setEnvs, () => v1.updateEnvironment(e.id, { egress: on ? "blocked" : "allowed" }), setErr)} />
            <Button variant="ghost" onClick={() => setRename(e)}>Rename</Button>
            {admin && envs.length > 1 && <Button variant="ghost" onClick={() => setDel(e)}>Delete</Button>}
          </div>
        ))}
      </div>
      <h3 className="font-semibold mb-3">New environment</h3>
      <form onSubmit={async (ev) => { ev.preventDefault(); if (!name || nameErr) return; const r = await a.run(() => v1.createEnvironment(project.id, { name: name.trim(), mode, from: mode === "duplicate" ? from : undefined })); if (r) { setEnvs([...envs, r]); setName(""); } }}>
        <Field label="Name" error={nameErr}><Input value={name} onChange={(e) => setName(e.target.value)} placeholder="staging" /></Field>
        <Field label="Start with"><Select value={mode} onChange={(e) => setMode(e.target.value)} options={[{ value: "empty", label: "No services" }, { value: "duplicate", label: "A copy of another environment" }]} /></Field>
        {mode === "duplicate" && <Field label="Copy from" hint="Services, settings and variables are copied as staged changes. Sealed variables need new values."><Select value={from} onChange={(e) => setFrom(e.target.value)} options={envs.map((e) => ({ value: e.id, label: e.name }))} /></Field>}
        <SaveRow {...a} saved={false} disabled={!name || !!nameErr} label="Create environment" />
      </form>
      <div className="mt-6 opacity-60" title="Not in v0.1: create an environment per branch by hand, or use 5050 environment new in CI.">
        <Toggle label="PR environments (not in v0.1)" checked={false} disabled onChange={() => undefined} />
      </div>
      {rename && <RenameEnv env={rename} taken={envs.filter((x) => x.id !== rename.id).map((x) => x.name)} onClose={() => setRename(null)} onSaved={(e) => setEnvs(envs.map((x) => (x.id === e.id ? e : x)))} />}
      {del && <TypedConfirm title="Delete environment" name={del.name} action="Delete environment" onClose={() => setDel(null)}
        body={<>This stops every service in <b>{del.name}</b> and deletes its volumes after 48 hours.</>}
        onConfirm={async (c) => { await v1.deleteEnvironment(del.id, c); setEnvs(envs.filter((x) => x.id !== del.id)); }} />}
    </Section>
  );
}

function RenameEnv({ env, taken, onClose, onSaved }: { env: T.Environment; taken: string[]; onClose: () => void; onSaved: (e: T.Environment) => void }) {
  const [name, setName] = useState(env.name);
  const a = useAction();
  const err = checkEnvName(name, taken);
  return (
    <Modal title="Rename environment" onClose={onClose} footer={<><Button onClick={onClose}>Cancel</Button>
      <Button variant="primary" disabled={!!err || a.busy} onClick={async () => { const r = await a.run(() => v1.updateEnvironment(env.id, { name: name.trim() })); if (r) { onSaved(r); onClose(); } }}>Rename</Button></>}>
      <Field label="Name" error={err}><Input value={name} onChange={(e) => setName(e.target.value)} /></Field>
      {a.error && <Notice kind="error">{a.error}</Notice>}
    </Modal>
  );
}

export function SharedVariables({ env }: { env: T.Environment }) {
  const { data, error, setData } = useLoad(() => v1.sharedVariables(env.id), [env.id]);
  const [values, setValues] = useState<Record<string, string>>({});
  const [name, setName] = useState("");
  const [value, setValue] = useState("");
  const a = useAction();
  const [err, setErr] = useState<string | null>(null);
  useEffect(() => setValues({}), [env.id]);
  if (!data) return <Loading error={error} />;
  const nameOk = /^[A-Za-z_][A-Za-z0-9_]{0,255}$/.test(name);
  const reveal = async () => { try { setValues((await v1.revealSharedVariables(env.id, {})).values); } catch (e: any) { setErr(e.message); } };
  return (
    <Section title="Shared variables" description={`Variables any service in ${env.name} can reference as \${{shared.NAME}}. ${STAGED_NOTE}`}
      actions={data.variables.length ? <Button onClick={reveal}>Reveal values</Button> : undefined}>
      {err && <Notice kind="error">{err}</Notice>}
      {data.variables.length === 0 ? <Empty>No shared variables.</Empty> : (
        <div className="rounded-xl border border-gray-200 overflow-hidden mb-6">
          {data.variables.map((v) => (
            <div key={v.name} className="flex items-center gap-3 px-4 py-2.5 border-b border-gray-100 last:border-b-0 text-sm">
              <span className="font-mono w-1/3 truncate">{v.name}</span>
              <span className="flex-1 font-mono text-xs truncate text-gray-600">{v.reference || (v.sealed ? "sealed" : values[v.name] ?? "•••••••")}</span>
              {v.staged && <span className="text-xs text-yellow-700">staged {v.staged}</span>}
              {v.staged !== "delete" && <Button variant="ghost" onClick={async () => { try { setData(await v1.setSharedVariables(env.id, { unset: [v.name] })); } catch (e: any) { setErr(e.message); } }}>Remove</Button>}
            </div>
          ))}
        </div>
      )}
      <form onSubmit={async (e) => { e.preventDefault(); if (!nameOk) return; const r = await a.run(() => v1.setSharedVariables(env.id, { set: { [name]: value } })); if (r) { setData(r); setName(""); setValue(""); } }}>
        <div className="grid grid-cols-1 md:grid-cols-2 gap-x-3">
          <Field label="Name" error={name && !nameOk ? "Use letters, digits and underscores, not starting with a digit." : null}><Input className="font-mono" value={name} onChange={(e) => setName(e.target.value)} placeholder="API_URL" /></Field>
          <Field label="Value"><Input className="font-mono" value={value} onChange={(e) => setValue(e.target.value)} autoComplete="off" /></Field>
        </div>
        <SaveRow {...a} disabled={!nameOk} label="Add variable" />
      </form>
    </Section>
  );
}

export function Webhooks({ project }: { project: T.Project }) {
  const { data, error, setData } = useLoad(() => v1.webhooks(project.id), [project.id]);
  const [url, setUrl] = useState("");
  const [events, setEvents] = useState<string[]>(CRITICAL_EVENTS);
  const [shown, setShown] = useState(false);
  const [secret, setSecret] = useState<string | null>(null);
  const [tests, setTests] = useState<Record<string, T.WebhookTestResponse>>({});
  const [err, setErr] = useState<string | null>(null);
  const a = useAction();
  if (!data) return <Loading error={error} />;
  const errs = checkWebhook(url, events);
  return (
    <Section title="Webhooks" description="Signed JSON posts for project events. Check X-5050-Signature with the secret.">
      {err && <Notice kind="error">{err}</Notice>}
      {secret && <><Notice kind="ok">Copy the signing secret now. It is not shown again.</Notice><div className="mb-6"><CopyField value={secret} /></div></>}
      {data.length === 0 ? <Empty>No webhooks yet.</Empty> : data.map((w) => (
        <div key={w.id} className="rounded-xl border border-gray-200 p-4 mb-3 text-sm">
          <div className="flex items-center gap-3 flex-wrap">
            <span className="font-mono flex-1 min-w-0 break-all">{w.url}</span>
            <Toggle label="Enabled" checked={w.enabled} onChange={(on) => optimistic(data, data.map((x) => (x.id === w.id ? { ...x, enabled: on } : x)), setData, () => v1.updateWebhook(w.id, { enabled: on }), setErr)} />
            <Button onClick={async () => { try { const r = await v1.testWebhook(w.id); setTests({ ...tests, [w.id]: r }); } catch (e: any) { setErr(e.message); } }}>Test</Button>
            <Button variant="ghost" onClick={() => optimistic(data, data.filter((x) => x.id !== w.id), setData, () => v1.deleteWebhook(w.id), setErr)}>Delete</Button>
          </div>
          <div className="text-xs text-gray-500 mt-1">{w.preset === "critical" ? "Critical events" : w.events.join(", ")}</div>
          {tests[w.id] && <div className="mt-2">{tests[w.id].status === "ok" ? <Notice kind="ok">Test delivered{tests[w.id].responseCode ? ` (HTTP ${tests[w.id].responseCode})` : ""}.</Notice> : <Notice kind="error">Test failed{tests[w.id].responseCode ? ` (HTTP ${tests[w.id].responseCode})` : ""}{tests[w.id].error ? `: ${tests[w.id].error}` : "."}</Notice>}</div>}
          {!!w.deliveries?.length && (
            <div className="mt-3 border-t border-gray-100 pt-2">
              {w.deliveries.map((d) => <div key={d.id} className="text-xs flex gap-3"><span className="w-40">{fmtDate(d.createdAt)}</span><span className="flex-1">{d.event}</span><span>{d.status}{d.responseCode ? ` ${d.responseCode}` : ""}, {d.attempts} attempt{d.attempts === 1 ? "" : "s"}</span></div>)}
            </div>
          )}
        </div>
      ))}
      <h3 className="font-semibold mt-6 mb-3">New webhook</h3>
      <form noValidate onSubmit={async (e) => { e.preventDefault(); setShown(true); if (hasErrors(errs)) return; const r = await a.run(() => v1.createWebhook(project.id, webhookBody(url, events))); if (r) { setData([r, ...data]); setSecret(r.secret || null); setUrl(""); setShown(false); } }}>
        <Field label="URL" error={shown ? errs.url : undefined}><Input value={url} onChange={(e) => setUrl(e.target.value)} placeholder="https://example.com/hooks/5050" /></Field>
        <div className="flex items-center gap-3 mb-2"><span className="text-sm font-medium">Events</span><Button type="button" variant="ghost" onClick={() => setEvents(CRITICAL_EVENTS)}>Critical events</Button><Button type="button" variant="ghost" onClick={() => setEvents(WEBHOOK_EVENTS)}>All</Button></div>
        <div className="grid grid-cols-1 md:grid-cols-2">
          {WEBHOOK_EVENTS.map((ev) => <Toggle key={ev} label={ev} checked={events.includes(ev)} onChange={(on) => setEvents(on ? [...events, ev] : events.filter((x) => x !== ev))} />)}
        </div>
        {shown && errs.events && <p role="alert" className="text-xs text-red-600 mb-2">{errs.events}</p>}
        <SaveRow {...a} saved={false} label="Add webhook" />
      </form>
    </Section>
  );
}

const GRANT_OPTIONS = [{ value: "editor", label: "Editor" }, { value: "viewer", label: "Viewer" }, { value: "none", label: "No access" }];

export function Members({ project, admin }: { project: T.Project; admin: boolean }) {
  const grants = useLoad(() => v1.projectMembers(project.id), [project.id]);
  const users = useLoad(v1.members, []);
  const [err, setErr] = useState<string | null>(null);
  if (!grants.data || !users.data) return <Loading error={grants.error || users.error} />;
  const g = grants.data;
  const save = (next: T.ProjectGrant[]) => optimistic(g, next, grants.setData, async () => grants.setData(await v1.setProjectMembers(project.id, next)), setErr);
  const candidates = users.data.filter((u) => (u.role === "member" || u.role === "viewer") && !g.some((x) => x.userId === u.id));
  return (
    <Section title="Members" description="A grant replaces the install role on this project. Owners and admins always have full access; members default to editor.">
      {err && <Notice kind="error">{err}</Notice>}
      {g.length === 0 ? <Empty>No project grants. Install roles apply.</Empty> : (
        <div className="rounded-xl border border-gray-200 overflow-hidden mb-6">
          {g.map((x) => (
            <div key={x.userId} className="flex items-center gap-3 px-4 py-2.5 border-b border-gray-100 last:border-b-0 text-sm">
              <div className="flex-1 min-w-0"><div className="font-medium truncate">{x.name || x.email}</div><div className="text-xs text-gray-500 truncate">{x.email}</div></div>
              <Select aria-label={`Project role for ${x.email}`} className="w-36 h-9" disabled={!admin} value={x.role} options={GRANT_OPTIONS} onChange={(e) => save(g.map((y) => (y.userId === x.userId ? { ...y, role: e.target.value } : y)))} />
              {admin && <Button variant="ghost" onClick={() => save(g.filter((y) => y.userId !== x.userId))}>Remove</Button>}
            </div>
          ))}
        </div>
      )}
      {admin && candidates.length > 0 && (
        <Field label="Add a grant">
          <Select value="" onChange={(e) => { const u = users.data!.find((x) => x.id === e.target.value); if (u) save([...g, { userId: u.id, email: u.email, name: u.name, role: "viewer" }]); }}
            options={[{ value: "", label: "Choose a person" }, ...candidates.map((u) => ({ value: u.id, label: `${u.name || u.email} (${u.role})` }))]} />
        </Field>
      )}
    </Section>
  );
}

export function Danger({ project, env, admin }: { project: T.Project; env: T.Environment; admin: boolean }) {
  const canvas = useLoad(() => v1.canvas(env.id), [env.id]);
  const [svc, setSvc] = useState<T.Service | null>(null);
  const [delProject, setDelProject] = useState(false);
  const nav = useNavigate();
  if (!admin) return <Section title="Danger"><Notice>Only admins can delete services and projects.</Notice></Section>;
  return (
    <>
      <Section title={`Services in ${env.name}`} description={STAGED_NOTE}>
        {!canvas.data ? <Loading error={canvas.error} /> : canvas.data.services.length === 0 ? <Empty>No services.</Empty> : (
          <div className="rounded-xl border border-gray-200 overflow-hidden">
            {canvas.data.services.map((s) => (
              <div key={s.id} className="flex items-center gap-3 px-4 py-2.5 border-b border-gray-100 last:border-b-0 text-sm">
                <span className="flex-1 truncate">{s.config.name}</span>
                {s.stagedAction === "delete" ? <span className="text-xs text-yellow-700">staged for deletion</span> : <Button variant="ghost" onClick={() => setSvc(s)}>Delete</Button>}
              </div>
            ))}
          </div>
        )}
      </Section>
      <Section title="Delete project" description="Stops every service in every environment and deletes volumes after 48 hours.">
        <Button variant="danger" onClick={() => setDelProject(true)}>Delete project</Button>
      </Section>
      {svc && <TypedConfirm title="Delete service" name={svc.config.name} action="Delete service" onClose={() => setSvc(null)}
        body={<>Stage the deletion of <b>{svc.config.name}</b>. It is removed when you deploy the staged changes.</>}
        onConfirm={async (c) => { await v1.deleteService(env.id, svc.id, c); canvas.reload(); }} />}
      {delProject && <TypedConfirm title="Delete project" name={project.name} action="Delete project" onClose={() => setDelProject(false)}
        body={<>Delete <b>{project.name}</b> and everything in it?</>}
        onConfirm={async (c) => { await v1.deleteProject(project.id, c); nav("/"); }} />}
    </>
  );
}
