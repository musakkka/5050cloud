// SPDX-License-Identifier: AGPL-3.0-only

// Template list and detail (SPEC 10.2). With ?project=&environment= the
// template deploys into that environment; otherwise into a new project.
import { useEffect, useState } from "react";
import { Link, useNavigate, useParams, useSearchParams } from "react-router-dom";
import { ChevronRight, HardDrive, Globe, Eye, EyeOff } from "lucide-react";
import { api, type TemplateDetail, type TemplateSummary } from "../../api/client";
import { useLoad } from "../../api/hooks";
import { TopBar } from "../../app/Shell";
import { Button, ErrorLine, Field, Input, ServiceIcon, Skeleton, Spinner } from "../../kit";

function useInto() {
  const [p] = useSearchParams();
  const pid = p.get("project"), eid = p.get("environment");
  return pid && eid ? { pid, eid, q: `?project=${encodeURIComponent(pid)}&environment=${encodeURIComponent(eid)}` } : null;
}

export function TemplateList() {
  const [q, setQ] = useState("");
  const into = useInto();
  const list = useLoad(() => api.templates(q || undefined), [q]);
  const groups: Record<string, TemplateSummary[]> = {};
  for (const t of list.data || []) (groups[t.category] ||= []).push(t);
  return (
    <div className="flex min-h-full flex-col">
      <TopBar left={<><Link to="/new" className="text-gray-500 hover:text-foreground">New</Link><span className="text-gray-400">/</span><span className="font-medium">Templates</span></>} />
      <main className="mx-auto w-full max-w-[720px] space-y-5 px-4 py-10">
        <h1 className="text-2xl font-semibold">Deploy a template</h1>
        <Input aria-label="Search templates" autoFocus placeholder="Search templates" value={q} onChange={(e) => setQ(e.target.value)} />
        <ErrorLine error={list.error} />
        {list.loading && !list.data && <Skeleton className="h-40" />}
        {Object.entries(groups).map(([cat, items]) => (
          <section key={cat} className="space-y-2">
            <h2 className="text-sm font-medium capitalize text-gray-500">{cat === "app" ? "Apps" : cat === "database" ? "Databases" : cat}</h2>
            <ul className="divide-y divide-gray-200 overflow-hidden rounded-xl border border-gray-200 bg-secondaryBg">
              {items.map((t) => (
                <li key={t.id}>
                  <Link to={`/new/template/${encodeURIComponent(t.id)}${into?.q || ""}`} className="flex items-center gap-3 px-4 py-3 hover:bg-gray-100">
                    <ServiceIcon icon={t.icon} size={24} />
                    <span className="min-w-0 flex-1"><span className="block text-sm font-medium">{t.name}</span><span className="block truncate text-xs text-gray-500">{t.tagline}</span></span>
                    <ChevronRight size={16} className="text-gray-400" />
                  </Link>
                </li>
              ))}
            </ul>
          </section>
        ))}
        {list.data?.length === 0 && <p className="text-sm text-gray-500">No template matches.</p>}
      </main>
    </div>
  );
}

export function TemplateDetailPage() {
  const { id = "" } = useParams();
  const nav = useNavigate();
  const into = useInto();
  const [t, setT] = useState<TemplateDetail>();
  const [err, setErr] = useState<unknown>();
  const [inputs, setInputs] = useState<Record<string, string>>({});
  const [shown, setShown] = useState<Record<string, boolean>>({});
  const [busy, setBusy] = useState(false);
  const [deployErr, setDeployErr] = useState<unknown>();
  useEffect(() => {
    api.template(id).then((d) => { setT(d); setInputs(Object.fromEntries(d.inputs.map((i) => [i.name, i.default || ""]))); }, setErr);
  }, [id]);

  const missing = t?.inputs.filter((i) => i.required && !inputs[i.name]?.trim()) || [];
  const deploy = async () => {
    setBusy(true); setDeployErr(undefined);
    try {
      let pid = into?.pid, eid = into?.eid;
      if (!pid || !eid) {
        const p = await api.createProject({ name: t?.name });
        pid = p.id; eid = (p.environments.find((e) => e.name === "production") || p.environments[0]).id;
      }
      await api.deployTemplate(eid, id, inputs);
      nav(`/project/${pid}?environment=${encodeURIComponent(eid)}`);
    } catch (e) { setDeployErr(e); setBusy(false); }
  };

  return (
    <div className="flex min-h-full flex-col">
      <TopBar left={<><Link to={`/new/template${into?.q || ""}`} className="text-gray-500 hover:text-foreground">Templates</Link><span className="text-gray-400">/</span><span className="font-medium">{t?.name || id}</span></>} />
      <main className="mx-auto w-full max-w-[720px] space-y-6 px-4 py-10">
        <ErrorLine error={err} />
        {!t && !err && <Spinner />}
        {t && <>
          <div className="flex items-start gap-4">
            <ServiceIcon icon={t.icon} size={40} />
            <div className="min-w-0">
              <h1 className="text-2xl font-semibold">{t.name}</h1>
              <p className="text-sm text-gray-600">{t.description || t.tagline}</p>
              {t.docs && <a className="text-sm underline" href={t.docs} target="_blank" rel="noreferrer">Documentation</a>}
            </div>
          </div>
          <section className="space-y-2">
            <h2 className="text-sm font-medium text-gray-500">Services</h2>
            <ul className="divide-y divide-gray-200 rounded-xl border border-gray-200 bg-secondaryBg">
              {t.serviceInfo.map((s) => (
                <li key={s.name} className="flex flex-wrap items-center gap-x-4 gap-y-1 px-4 py-3 text-sm">
                  <span className="font-medium">{s.name}</span>
                  <span className="font-mono text-xs text-gray-500">{s.image}</span>
                  {s.volume && <span className="inline-flex items-center gap-1 text-xs text-gray-500"><HardDrive size={12} />{s.volume}</span>}
                  {s.domain && <span className="inline-flex items-center gap-1 text-xs text-gray-500"><Globe size={12} />public domain</span>}
                  {s.dependsOn?.length ? <span className="text-xs text-gray-500">starts after {s.dependsOn.join(", ")}</span> : null}
                </li>
              ))}
            </ul>
          </section>
          {t.inputs.length > 0 && (
            <section className="space-y-3">
              <h2 className="text-sm font-medium text-gray-500">Configuration</h2>
              {t.inputs.map((i) => (
                <Field key={i.name} label={i.label || i.name} hint={i.description}>
                  <div className="flex gap-2">
                    <Input aria-label={i.label || i.name} type={i.secret && !shown[i.name] ? "password" : "text"} value={inputs[i.name] || ""} required={i.required} autoComplete="off"
                      onChange={(e) => setInputs({ ...inputs, [i.name]: e.target.value })} className="font-mono" />
                    {i.secret && <Button aria-label={shown[i.name] ? "Hide" : "Show"} onClick={() => setShown({ ...shown, [i.name]: !shown[i.name] })}>{shown[i.name] ? <EyeOff size={14} /> : <Eye size={14} />}</Button>}
                  </div>
                </Field>
              ))}
            </section>
          )}
          <ErrorLine error={deployErr} />
          <Button variant="primary" busy={busy} disabled={missing.length > 0} onClick={() => void deploy()}>Deploy</Button>
        </>}
      </main>
    </div>
  );
}
