// SPDX-License-Identifier: AGPL-3.0-only

import { useState } from "react";
import { Check, Copy } from "lucide-react";
import type * as T from "../../api/types.gen";
import { Button, IconButton, Input, Modal, Select, toast, toastError } from "../../kit";
import { v1 } from "../settings/client";
import TypedConfirm from "./TypedConfirm";
import { BlockDivider, FormLabel, InlineError, SectionHead, downloadText, useAsync } from "./ui";

export default function General({ project, onProject, admin }: { project: T.Project; onProject: (p: T.Project) => void; admin: boolean }) {
  const [name, setName] = useState(project.name);
  const [desc, setDesc] = useState(project.description);
  const [vis, setVis] = useState(false);
  const [tpl, setTpl] = useState(false);
  const [xfer, setXfer] = useState(false);
  const save = useAsync();
  const nameOk = name.trim().length >= 1 && name.trim().length <= 64;
  const dirty = name.trim() !== project.name || desc.trim() !== project.description;
  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!dirty || !nameOk) return;
    const r = await save.run(() => v1.updateProject(project.id, { name: name.trim(), description: desc.trim() }));
    if (r) { onProject(r); setName(r.name); setDesc(r.description); toast("Project updated", "success"); }
  };
  return (
    <>
      <section>
        <SectionHead title="Project Info" />
        <form onSubmit={submit} noValidate className="space-y-6">
          <div>
            <FormLabel htmlFor="ps-name">Name</FormLabel>
            <Input id="ps-name" value={name} onChange={(e) => setName(e.target.value)} aria-invalid={!nameOk || undefined} maxLength={64} />
            {!nameOk && <InlineError>Enter a name of up to 64 characters.</InlineError>}
          </div>
          <div>
            <FormLabel htmlFor="ps-desc">Description</FormLabel>
            <Input id="ps-desc" value={desc} onChange={(e) => setDesc(e.target.value)} placeholder="Optional description of this project" />
          </div>
          <div>
            <FormLabel>Project ID</FormLabel>
            <IdBox value={project.id} />
          </div>
          <div>
            <Button type="submit" variant="primary" size="lg" disabled={!dirty || !nameOk} busy={save.busy}>Update</Button>
            <InlineError>{save.error}</InlineError>
          </div>
        </form>
      </section>
      <BlockDivider />
      <section>
        <SectionHead title="Visibility" />
        <p className="text-body text-foreground">
          This project is currently <b className="font-bold uppercase">{project.publicRead ? "Public" : "Private"}</b>.{" "}
          {project.publicRead
            ? "Anyone with the link can see the canvas, statuses and deployment list. Variables, logs and settings stay private."
            : "Private projects are only visible to members of the project."}
        </p>
        <Button variant="accent-outline" size="lg" className="mt-6" disabled={!admin} onClick={() => setVis(true)} title={admin ? undefined : "Only admins can change visibility"}>Change visibility</Button>
      </section>
      <BlockDivider />
      <section>
        <SectionHead title="Generate Template from Project" />
        <p className="text-body text-foreground">Save this project as a one-click template. Other people can import the file to deploy the same services. Secrets and sealed values are never included.</p>
        <Button variant="accent-outline" size="lg" className="mt-6" onClick={() => setTpl(true)}>Create template</Button>
      </section>
      <BlockDivider />
      <section>
        <SectionHead title="Transfer Project" />
        <p className="text-body text-foreground">Transfer the project to a different workspace.</p>
        <Button variant="accent-outline" size="lg" className="mt-6" disabled={!admin} onClick={() => setXfer(true)}>Transfer project</Button>
      </section>
      {vis && (
        <TypedConfirm title="Change visibility" name={project.name} action="Change visibility" warn={false}
          body={<>Are you sure you want to convert the project <b>{project.name}</b> from {project.publicRead ? "PUBLIC to PRIVATE" : "PRIVATE to PUBLIC"}?</>}
          onClose={() => setVis(false)}
          onConfirm={async () => { onProject(await v1.updateProject(project.id, { publicRead: !project.publicRead })); toast("Visibility changed", "success"); }} />
      )}
      {tpl && <TemplateModal project={project} onClose={() => setTpl(false)} />}
      {xfer && <TransferModal onClose={() => setXfer(false)} />}
    </>
  );
}

function IdBox({ value }: { value: string }) {
  const [done, setDone] = useState(false);
  return (
    <div className="flex min-h-[52px] items-center justify-between gap-3 rounded-lg border border-surface-hover bg-surface-hover px-3 py-2">
      <pre className="min-w-0 flex-1 overflow-x-auto font-mono text-body text-foreground scroll-thin">{value}</pre>
      <IconButton label={done ? "Copied" : "Copy project ID"} size={34}
        onClick={() => { void navigator.clipboard?.writeText(value); setDone(true); setTimeout(() => setDone(false), 1500); }}>
        {done ? <Check size={16} className="text-ok-text" /> : <Copy size={16} className="opacity-70 transition-opacity duration-150 hover:opacity-100" />}
      </IconButton>
    </div>
  );
}

function TemplateModal({ project, onClose }: { project: T.Project; onClose: () => void }) {
  const a = useAsync();
  const env = project.environments[0];
  const go = async () => {
    if (!env) return;
    const c = await a.run(() => v1.canvas(env.id));
    if (!c) return;
    const tpl = {
      kind: "5050cloud.template/v1", name: project.name, description: project.description,
      services: c.services.map((s) => ({ name: s.config.name, icon: s.config.icon, source: s.config.source, build: s.config.build, deploy: s.config.deploy, networking: s.config.networking, template: s.config.template, templateVersion: s.config.templateVersion })),
    };
    downloadText(`${project.name.replace(/[^a-z0-9-]+/gi, "-").toLowerCase()}.template.json`, JSON.stringify(tpl, null, 2));
    toast("Template downloaded", "success");
    onClose();
  };
  return (
    <Modal bare width={548} onClose={onClose} title="Generate Template">
      <div className="p-6">
        <h3 className="text-h2 text-foreground">Generate Template</h3>
        <p className="mt-4 text-lead text-foreground">Create a template from the services in <b>{env?.name || "this project"}</b>, without any variable values.</p>
        <InlineError>{a.error}</InlineError>
        <div className="mt-6 flex justify-end gap-2">
          <Button size="lg" onClick={onClose}>Cancel</Button>
          <Button size="lg" variant="primary" busy={a.busy} disabled={!env} onClick={go}>Generate</Button>
        </div>
      </div>
    </Modal>
  );
}

function TransferModal({ onClose }: { onClose: () => void }) {
  const [to, setTo] = useState("");
  return (
    <Modal bare width={496} onClose={onClose} title="Transfer Project">
      <div className="p-6">
        <h3 className="pr-8 text-h2 text-foreground">Transfer Project</h3>
        <p className="mt-4 text-lead text-foreground">Please choose the workspace you would like to transfer the project to:</p>
        <div className="mt-4">
          <Select variant="field" value={to} onChange={(e) => setTo(e.target.value)} aria-label="Workspace"
            options={[{ value: "", label: "No other workspace available" }]} disabled />
        </div>
        <p className="mt-2 text-caption text-fg-3">This install has a single workspace. Multi-workspace transfers arrive with teams.</p>
        <div className="mt-6 flex justify-end gap-2">
          <Button size="lg" variant="accent-outline" onClick={onClose}>Cancel</Button>
          <Button size="lg" variant="primary" disabled={!to} onClick={() => { toastError(new Error("Nothing to transfer to")); }}>Transfer project</Button>
        </div>
      </div>
    </Modal>
  );
}
