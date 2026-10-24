// SPDX-License-Identifier: AGPL-3.0-only

import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { TriangleAlert } from "lucide-react";
import type * as T from "../../api/types.gen";
import { Button, Notice, ServiceIcon, Skeleton, cx, toast } from "../../kit";
import { v1 } from "../settings/client";
import TypedConfirm from "./TypedConfirm";
import { AlertBox, BlockDivider, LoadError, SectionHead, useLoad } from "./ui";

export default function Danger({ project, env, admin }: { project: T.Project; env?: T.Environment; admin: boolean }) {
  const canvas = useLoad(() => (env ? v1.canvas(env.id) : Promise.resolve({ services: [] } as unknown as T.Canvas)), [env?.id]);
  const [svc, setSvc] = useState<T.Service | null>(null);
  const [delProject, setDelProject] = useState(false);
  const nav = useNavigate();
  if (!admin) return <Notice tone="warn" title="Admins only">Only admins can delete services and projects.</Notice>;
  const services = canvas.data?.services ?? [];
  return (
    <div className="max-w-[565px]">
      <section>
        <SectionHead title="Manage Services" />
        <AlertBox icon={<TriangleAlert size={20} />}>Removing a service stops it and stages its deletion. It is deleted for good when you deploy the staged changes{env ? ` in ${env.name}` : ""}.</AlertBox>
        <div className="mt-6 overflow-hidden rounded-lg border border-line">
          {canvas.error ? <div className="p-4"><LoadError error={canvas.error} onRetry={canvas.reload} /></div>
            : !canvas.data ? <div className="space-y-2 p-3"><Skeleton h={36} className="w-full" /><Skeleton h={36} className="w-full" /></div>
            : services.length === 0 ? <div className="p-6 text-center text-body text-fg-2">No services in this environment.</div>
            : services.map((s, i) => (
              <div key={s.id} className={cx("flex h-[59px] items-center gap-3 px-4 transition-colors hover:bg-tint-hover", i > 0 && "border-t border-surface-hover")}>
                <ServiceIcon icon={s.config.icon} image={s.config.source.image} source={s.config.source.type} size={24} />
                <span className="min-w-0 flex-1 truncate text-lead text-foreground">{s.config.name}</span>
                {s.stagedAction === "delete" ? <span className="text-caption text-warn">Staged for deletion</span> : <Button variant="danger-outline" onClick={() => setSvc(s)}>Remove</Button>}
              </div>
            ))}
        </div>
      </section>
      <BlockDivider />
      <section>
        <SectionHead title="Delete Project" />
        <AlertBox icon={<TriangleAlert size={20} />}>Deleting this project will <b className="font-semibold">permanently delete</b> every service, environment and volume in it, after a 48 hour grace period.</AlertBox>
        <Button variant="danger" size="lg" className="mt-6" onClick={() => setDelProject(true)}>Delete Project</Button>
      </section>
      {svc && env && (
        <TypedConfirm title="Delete Service" name={svc.config.name} action="Delete"
          body={<>You are <b className="text-danger-text">deleting</b> the service <b>{svc.config.name}</b>. All data will be removed forever once you deploy the change.</>}
          onClose={() => setSvc(null)} onConfirm={async (c) => { await v1.deleteService(env.id, svc.id, c); canvas.reload(); toast("Service staged for deletion", "success"); }} />
      )}
      {delProject && (
        <TypedConfirm title="Delete Project" name={project.name} action="Delete"
          body={<>
            <p>You are <b className="text-danger-text">deleting</b> the project <b>{project.name}</b>. This includes all services, environments and data.</p>
            <p className="mt-3 text-body text-fg-2">The project is permanently deleted after 48 hours. You can cancel or force delete during the grace period.</p>
          </>}
          onClose={() => setDelProject(false)} onConfirm={async (c) => { await v1.deleteProject(project.id, c); nav("/"); }} />
      )}
    </div>
  );
}
