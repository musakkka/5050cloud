// SPDX-License-Identifier: AGPL-3.0-only

import { useEffect, useMemo, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { Braces, ChevronDown, ChevronRight, Copy, Eye, EyeOff, Globe, Lock, MapPin, MoreVertical, Plus, X } from "lucide-react";
import { Area, AreaChart, CartesianGrid, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts";
import { api, type Deployment, type Resource } from "../api";
import { Badge, Button, Confirm, Empty, Menu, Modal, ResourceIcon, Spinner, StatusGlyph, Tabs, cx, fmtBytes, fmtDateTime, timeAgo, tzLabel, usePoll } from "../ui/kit";
import LogView, { LogTable } from "../ui/LogView";
import { useProject } from "./ProjectLayout";

export default function ServicePanel({ target }: { target: string }) {
  const { pid, withEnv, reload } = useProject();
  const { tab = "deployments", dep } = useParams();
  const nav = useNavigate();
  const { data: res } = usePoll(() => api.resource(target), [target], 8000);
  const node = useProject().canvas?.nodes.find((n) => n.id === target);
  const base = `/project/${pid}/service/${encodeURIComponent(target)}`;
  const close = () => nav(withEnv(`/project/${pid}`));
  const tabs = [
    { id: "deployments", label: "Deployments" },
    ...(res?.kind === "service" ? [{ id: "variables", label: "Variables" }] : []),
    ...(res?.kind === "pg" ? [{ id: "data", label: "Data" }] : []),
    { id: "metrics", label: "Metrics" },
    { id: "settings", label: "Settings" },
  ];
  return (
    <div className="absolute right-0 top-0 bottom-0 z-20 w-[min(1040px,74%)] panel-in rounded-l-xl border-l border-gray-200 bg-secondaryBg shadow-[-12px_0_32px_rgba(0,0,0,.35)] flex flex-col">
      {dep ? <DeploymentView target={target} res={res} hid={dep} onBack={() => nav(withEnv(base))} onClose={close} /> : (
        <>
          <div className="px-12 pt-10">
            <div className="flex items-start justify-between">
              <div className="flex items-center gap-4 min-w-0">
                <ResourceIcon icon={node?.icon || res?.icon} kind={res?.kind} source={node?.source} size={32} />
                <h2 className="text-[28px] leading-9 font-semibold truncate">{res?.name || node?.name || target}</h2>
              </div>
              <button onClick={close} className="text-gray-500 hover:text-foreground mt-1"><X size={20} /></button>
            </div>
            <Tabs className="mt-8" tabs={tabs} value={tab} onChange={(t) => nav(withEnv(t === "deployments" ? base : `${base}/${t}`))} />
          </div>
          <div className="flex-1 min-h-0 overflow-y-auto scroll-thin px-12 py-8">
            {!res ? <div className="flex justify-center py-16"><Spinner /></div> :
              tab === "variables" ? <VariablesTab res={res} onDeployed={reload} /> :
              tab === "metrics" ? <MetricsTab res={res} /> :
              tab === "settings" ? <SettingsTab res={res} /> :
              tab === "data" ? <DataTab res={res} /> :
              <DeploymentsTab res={res} base={base} onChanged={reload} />}
          </div>
        </>
      )}
    </div>
  );
}

/* ------------------------------------------------------------------ deployments */

/** GitHub avatars are a third-party request; a viewer can turn them off with localStorage "5050.avatars" = "0". */
function avatarsOff(): boolean {
  try { return localStorage.getItem("5050.avatars") === "0"; } catch { return false; }
}

function via(d: Deployment, res: Resource) {
  if (res.kind !== "service") return "via Docker Image";
  if (res.image) return "via Docker Image";
  if (d.source === "console") return `via 5050cloud${d.actor ? ` (${d.actor.split("@")[0]})` : ""}`;
  return "via GitHub";
}

function DeploymentsTab({ res, base, onChanged }: { res: Resource; base: string; onChanged: () => void }) {
  const { withEnv } = useProject();
  const { data, reload } = usePoll(() => api.deployments(res.id), [res.id], 5000);
  const [showHistory, setShowHistory] = useState(true);
  const [confirm, setConfirm] = useState<null | { title: string; body: React.ReactNode; confirm: string; danger?: boolean; run: () => Promise<unknown> }>(null);
  const nav = useNavigate();
  const isSvc = res.kind === "service";
  const run = (action: string, version?: string) => async () => { await api.action(res.id, action, version); reload(); onChanged(); };

  const actions = {
    redeploy: () => setConfirm({ title: "Redeploy", confirm: "Redeploy", body: <>Start <b>{res.name}</b> again from its current image with the current variables. Web services switch over with zero downtime.</>, run: run("redeploy") }),
    restart: () => setConfirm({ title: "Restart", confirm: "Restart", danger: true, body: <>Restart the running container{res.replicas > 1 ? "s" : ""} of <b>{res.name}</b>. This is a hard restart: requests in flight are cut and {res.kind !== "service" ? "every app using it reconnects" : "there is a short gap"}.</>, run: run("restart") }),
    latest: () => setConfirm({ title: "Deploy latest commit", confirm: "Deploy", body: <>Build the head of <b>{res.repo}</b>{res.entry?.branch ? ` (${res.entry.branch})` : ""} and deploy it, the same as a push would.</>, run: run("deploy-latest") }),
    rollback: (d: Deployment) => setConfirm({ title: "Rollback", confirm: "Rollback", danger: true, body: <>Deploy <b>{d.version}</b>{d.commit ? <> ({d.commit.message})</> : null} again. It runs with today's variables, not the ones it had back then.</>, run: run("rollback", d.version || undefined) }),
  };

  if (!data) return <div className="flex justify-center py-16"><Spinner /></div>;
  const active = data.deployments.find((d) => d.id === data.active);
  const pending = data.deployments.filter((d) => d.badge === "deploying");
  const rest = data.deployments.filter((d) => d !== active && !pending.includes(d));
  const logsHref = (d: Deployment) => withEnv(`${base}/deployment/${encodeURIComponent(d.historyId || d.id)}`);

  return (
    <div>
      <div className="flex items-center justify-between text-sm mb-4">
        <div className="flex items-center gap-2 min-w-0">
          {(res.domains?.length ? res.domains : res.internal ? [res.internal] : []).slice(0, 2).map((d) => (
            <a key={d} href={d === res.internal ? undefined : `https://${d}`} target="_blank" rel="noreferrer" className="flex items-center gap-2 text-foreground hover:underline truncate">
              {d === res.internal ? <Lock size={14} className="text-gray-500" /> : <Globe size={14} className="text-gray-500" />}{d}
            </a>
          ))}
        </div>
        <div className="flex items-center gap-4 text-gray-500 shrink-0">
          <span className="flex items-center gap-1.5"><MapPin size={14} />{res.region} · {res.hostName}</span>
          <span className="flex items-center gap-1.5"><Copy size={14} />{res.replicas} Replica{res.replicas === 1 ? "" : "s"}</span>
        </div>
      </div>

      {pending.map((d) => (
        <DeployCard key={d.id} d={d} res={res} highlight onLogs={() => nav(logsHref(d))} menu={[]} />
      ))}

      {active ? (
        <DeployCard d={active} res={res} highlight onLogs={() => nav(logsHref(active))}
          menu={[
            { label: "View logs", onClick: () => nav(logsHref(active)) },
            ...(isSvc ? [{ label: "Redeploy", onClick: actions.redeploy }] : []),
            { label: "Restart", onClick: actions.restart },
            ...(isSvc && res.repo ? [{ label: "Deploy latest commit", onClick: actions.latest }] : []),
          ]} />
      ) : !pending.length && (
        <div className="rounded-lg border border-dashed border-gray-200 px-6 py-8 text-center text-sm text-gray-600 mb-6">
          There is no active deployment for this service.
          {isSvc && <div className="mt-2"><button className="underline text-gray-700 hover:text-foreground" onClick={res.repo ? actions.latest : actions.redeploy}>Make a deployment to get started →</button></div>}
        </div>
      )}

      {rest.length > 0 && (
        <div className="mt-8">
          <button onClick={() => setShowHistory((s) => !s)} className="flex items-center gap-2 text-[13px] font-medium tracking-wide text-gray-700 uppercase mb-3">
            {showHistory ? <ChevronDown size={16} /> : <ChevronRight size={16} />}History
          </button>
          {showHistory && rest.map((d) => (
            <DeployCard key={d.id} d={d} res={res} onLogs={() => nav(logsHref(d))}
              menu={[
                { label: "View logs", onClick: () => nav(logsHref(d)) },
                ...(isSvc && d.version && d.badge !== "failed" ? [{ label: "Rollback", onClick: () => actions.rollback(d) }] : []),
              ]} />
          ))}
        </div>
      )}
      {confirm && <Confirm title={confirm.title} body={confirm.body} confirm={confirm.confirm} danger={confirm.danger} onConfirm={confirm.run} onClose={() => setConfirm(null)} />}
    </div>
  );
}

function DeployCard({ d, res, highlight, onLogs, menu }: { d: Deployment; res: Resource; highlight?: boolean; onLogs: () => void; menu: { label: string; onClick: () => void; danger?: boolean }[] }) {
  const [open, setOpen] = useState(false);
  const title = d.commit?.message || (d.version ? (d.version.length > 20 ? d.version.slice(0, 12) : d.version) : d.containerName || "deployment");
  const ok = d.badge === "active";
  const footer = { active: ["success", "Deployment successful", "text-green-600"], crashed: ["crashed", "Deployment crashed", "text-red-600"], failed: ["failed", "Deployment failed", "text-red-600"], deploying: ["deploying", d.kind === "deploy" ? "Building and deploying" : "Deploying", "text-blue-600"], unhealthy: ["warning", "Health check failing", "text-yellow-600"] }[d.badge];
  return (
    <div className={cx("rounded-xl border mb-3 overflow-hidden", highlight ? (ok ? "border-green-300 bg-green-100/40" : d.badge === "deploying" ? "border-blue-300 bg-blue-100/40" : "border-red-300 bg-red-100/40") : "border-gray-200 bg-background/40")}>
      <div className="flex items-center gap-4 px-5 py-4">
        <Badge kind={d.badge}>{d.badge === "deploying" ? (d.kind === "deploy" ? "building" : "deploying") : d.badge}</Badge>
        <div className="relative w-8 h-8 shrink-0">
          {d.actor && d.actor.startsWith("github:") && !avatarsOff() ? <img src={`https://avatars.githubusercontent.com/${d.actor.slice("github:".length)}?size=64`} alt="" className="w-7 h-7 rounded-full" /> : <div className="w-7 h-7 rounded-full bg-gray-200" />}
          <div className="absolute -right-0.5 -bottom-0.5 w-[18px] h-[18px] rounded-full bg-background flex items-center justify-center"><ResourceIcon source={res.image || res.kind !== "service" ? "image" : "github"} size={12} /></div>
        </div>
        <div className="min-w-0 flex-1">
          <div className="text-[15px] truncate">{title}</div>
          <div className="text-sm text-gray-600 truncate">
            {timeAgo(d.created)} {via(d, res)}
            {d.version && <span className="ml-2 font-mono text-xs text-gray-500">{d.version.slice(0, 12)}</span>}
          </div>
        </div>
        {(highlight || d.log) && <Button onClick={onLogs} className="shrink-0">View logs</Button>}
        {menu.length > 0 && <Menu trigger={<button className="w-8 h-8 rounded-md flex items-center justify-center text-gray-600 hover:text-foreground hover:bg-gray-100"><MoreVertical size={16} /></button>} items={menu} />}
      </div>
      {highlight && footer && (
        <>
          <button onClick={() => setOpen((o) => !o)} className={cx("flex w-full items-center gap-3 px-5 py-3 border-t border-white/5 bg-black/10 text-sm", footer[2])}>
            <span className="pl-[88px] flex items-center gap-3 flex-1"><StatusGlyph status={footer[0]} size={18} />{footer[1]}</span>
            <ChevronDown size={16} className={cx("transition-transform", open && "rotate-180")} />
          </button>
          {open && (
            <div className="grid grid-cols-[140px_1fr] gap-y-2 px-5 pl-[108px] py-4 text-sm border-t border-white/5">
              <span className="text-gray-500">Version</span><span className="font-mono">{d.version}</span>
              {d.commit && <><span className="text-gray-500">Commit</span><span className="truncate">{d.commit.sha.slice(0, 12)} by {d.commit.author}</span></>}
              {d.containerName && <><span className="text-gray-500">Container</span><span className="font-mono text-xs leading-5">{d.containerName}</span></>}
              <span className="text-gray-500">Host</span><span>{res.hostName} ({res.region})</span>
              {d.created && <><span className="text-gray-500">Started</span><span>{fmtDateTime(d.created)}</span></>}
            </div>
          )}
        </>
      )}
    </div>
  );
}

function DeploymentView({ target, res, hid, onBack, onClose }: { target: string; res: Resource | null; hid: string; onBack: () => void; onClose: () => void }) {
  const { data } = usePoll(() => api.deployments(target), [target], 5000);
  const d = data?.deployments.find((x) => x.id === hid || x.historyId === hid);
  const hasBuild = !!d?.historyId;
  const [tab, setTab] = useState<string>("deploy");
  useEffect(() => { if (d && !d.container && hasBuild) setTab("build"); }, [d?.id]); // eslint-disable-line react-hooks/exhaustive-deps
  const node = useProject().canvas?.nodes.find((n) => n.id === target);
  return (
    <div className="flex flex-col h-full min-h-0">
      <div className="px-12 pt-10">
        <div className="flex items-start justify-between">
          <div className="flex items-center gap-3 min-w-0">
            <button onClick={onBack} className="flex items-center gap-3 min-w-0 hover:opacity-80">
              <ResourceIcon icon={node?.icon || res?.icon} kind={res?.kind} source={node?.source} size={26} />
              <span className="text-[22px] font-semibold truncate">{res?.name}</span>
            </button>
            <span className="text-gray-300 text-xl">/</span>
            <span className="text-[22px] font-mono text-gray-800">{d?.version?.slice(0, 7) || hid.slice(-6)}</span>
          </div>
          <div className="flex items-center gap-5">
            {d?.created && <span className="text-sm text-gray-600">{fmtDateTime(d.created)}</span>}
            <button onClick={onClose} className="text-gray-500 hover:text-foreground"><X size={20} /></button>
          </div>
        </div>
        <div className="mt-5 flex items-center gap-3">
          {d && <Badge kind={d.badge}>{d.badge === "deploying" ? "building" : d.badge}</Badge>}
          {d?.commit && <span className="text-sm text-gray-700 truncate">{d.commit.message}</span>}
        </div>
        <div className="mt-6">
          <Tabs tabs={[{ id: "details", label: "Details" }, ...(hasBuild ? [{ id: "build", label: "Build Logs" }] : []), { id: "deploy", label: "Deploy Logs" }]} value={tab} onChange={setTab} />
        </div>
      </div>
      <div className="flex-1 min-h-0 flex flex-col">
        {!d ? <div className="flex justify-center py-16"><Spinner /></div> :
          tab === "details" ? (
            <div className="px-12 py-8 grid grid-cols-[160px_1fr] gap-y-3 text-sm">
              <span className="text-gray-500">Version</span><span className="font-mono">{d.version || "pending"}</span>
              {d.commit && <><span className="text-gray-500">Commit</span><span>{d.commit.message}<span className="ml-2 text-gray-500 font-mono text-xs">{d.commit.sha.slice(0, 12)}</span></span></>}
              {d.commit && <><span className="text-gray-500">Author</span><span>{d.commit.author}</span></>}
              <span className="text-gray-500">Trigger</span><span>{d.source === "console" ? `5050cloud, by ${d.actor}` : d.source === "github" ? `Push to GitHub${d.actor ? ` by ${d.actor}` : ""}` : "Deploy from the command line"}</span>
              {d.containerName && <><span className="text-gray-500">Container</span><span className="font-mono text-xs leading-5">{d.containerName}</span></>}
              <span className="text-gray-500">Host</span><span>{res?.hostName} ({res?.region})</span>
              {d.created && <><span className="text-gray-500">Started</span><span>{fmtDateTime(d.created)} ({tzLabel()})</span></>}
            </div>
          ) : tab === "build" ? <BuildLog target={target} hid={d.historyId!} /> :
          d.container ? <LogView targets={[target]} version={d.version || undefined} histogram={false} live={d.badge === "active"} placeholder="Filter logs using text, -exclude, @level:error" /> :
          <div className="p-12"><Empty>This deployment never started a container. The build logs say why.</Empty></div>}
      </div>
    </div>
  );
}

function BuildLog({ target, hid }: { target: string; hid: string }) {
  const { data } = usePoll(() => api.buildLog(target, hid), [target, hid], 3000);
  const lines = useMemo(() => (data?.lines || []).filter((l) => l.msg.trim()).map((l) => ({ ts: l.ts || 0, msg: l.msg, stream: "stdout", level: /\b(error|failed|fatal)\b/i.test(l.msg) ? "error" : "info" })), [data]);
  if (!data) return <div className="flex justify-center py-16"><Spinner /></div>;
  return <div className="flex-1 min-h-0 flex flex-col border-t border-gray-200"><LogTable lines={lines} follow={!data.done} empty="No build output was recorded for this deployment." /></div>;
}

/* ------------------------------------------------------------------ variables */

function VariablesTab({ res, onDeployed }: { res: Resource; onDeployed: () => void }) {
  const { data, reload } = usePoll(() => api.variables(res.id), [res.id], 0);
  const [values, setValues] = useState<Record<string, string>>({});
  const [shown, setShown] = useState<Record<string, boolean>>({});
  const [staged, setStaged] = useState<{ set: Record<string, string>; unset: string[] }>({ set: {}, unset: [] });
  const [adding, setAdding] = useState(false);
  const [raw, setRaw] = useState<string | null>(null);
  const [editing, setEditing] = useState<string | null>(null);
  const [showProvided, setShowProvided] = useState(false);
  const [deploying, setDeploying] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const changes = Object.keys(staged.set).length + staged.unset.length;

  const reveal = async (names?: string[]) => {
    const v = await api.reveal(res.id, names);
    setValues((cur) => ({ ...cur, ...v }));
    return v;
  };
  const toggle = async (k: string) => {
    if (!shown[k] && values[k] === undefined) await reveal([k]);
    setShown((s) => ({ ...s, [k]: !s[k] }));
  };
  const stage = (k: string, v: string) => setStaged((s) => ({ set: { ...s.set, [k]: v }, unset: s.unset.filter((x) => x !== k) }));
  const stageDelete = (k: string) => setStaged((s) => { const set = { ...s.set }; delete set[k]; return { set, unset: data?.names.includes(k) ? [...new Set([...s.unset, k])] : s.unset }; });
  const apply = async () => {
    setDeploying(true); setErr(null);
    try {
      await api.setVariables(res.id, { ...staged, deploy: true });
      setValues((cur) => { const n = { ...cur, ...staged.set }; staged.unset.forEach((k) => delete n[k]); return n; });
      setStaged({ set: {}, unset: [] }); reload(); onDeployed();
    } catch (e: any) { setErr(e.message); } finally { setDeploying(false); }
  };

  if (!data) return <div className="flex justify-center py-16"><Spinner /></div>;
  const names = [...new Set([...data.names, ...Object.keys(staged.set)])].sort();
  return (
    <div>
      {changes > 0 && (
        <div className="sticky top-0 z-10 -mt-2 mb-6 flex items-center justify-between rounded-lg border border-pink-300 bg-pink-100 px-4 py-3">
          <span className="text-sm text-pink-800">{changes} change{changes > 1 ? "s" : ""} to {res.name}'s variables. Deploying applies them with a zero-downtime redeploy.</span>
          <div className="flex gap-2">
            <Button onClick={() => setStaged({ set: {}, unset: [] })}>Discard</Button>
            <Button variant="primary" onClick={apply} disabled={deploying}>{deploying ? <Spinner className="!text-white" /> : null}Deploy</Button>
          </div>
        </div>
      )}
      {err && <div className="mb-4 rounded-md border border-red-300 bg-red-100 px-3 py-2 text-sm text-red-700">{err}</div>}
      <div className="flex items-center justify-between pb-4 border-b border-gray-200">
        <h3 className="text-lg font-semibold">{data.names.length} Service Variables</h3>
        <div className="flex items-center gap-2">
          <Button variant="ghost" onClick={async () => { const v = await reveal(); setRaw(Object.keys(v).sort().map((k) => `${k}=${v[k]}`).join("\n")); }}><Braces size={15} />Raw Editor</Button>
          <Button onClick={() => setAdding(true)} className="!border-pink-300 text-pink-700 hover:!border-pink-500"><Plus size={15} />New Variable</Button>
        </div>
      </div>
      <button onClick={() => setShowProvided((s) => !s)} className="flex items-center gap-2 py-4 text-sm text-gray-600 border-b border-gray-200 w-full">
        {showProvided ? <ChevronDown size={16} /> : <ChevronRight size={16} />}{data.provided.length} 5050cloud provided variables available
      </button>
      {showProvided && (
        <div className="py-3 border-b border-gray-200">
          {data.provided.map((k) => <div key={k} className="grid grid-cols-[minmax(0,320px)_1fr] px-5 py-2 font-mono text-sm"><span>{k}</span><span className="text-gray-500 font-sans">{k === "PORT" ? "the port this service listens on" : "provided at deploy time"}</span></div>)}
        </div>
      )}
      {adding && <VarEditor onCancel={() => setAdding(false)} onSave={(k, v) => { stage(k, v); setAdding(false); }} />}
      <div className="py-2">
        {names.map((k) => {
          const isNew = !data.names.includes(k);
          const changed = k in staged.set;
          const deleted = staged.unset.includes(k);
          if (editing === k) return <VarEditor key={k} initialKey={k} initialValue={staged.set[k] ?? values[k] ?? ""} lockKey onCancel={() => setEditing(null)} onSave={(kk, v) => { stage(kk, v); setEditing(null); }} />;
          return (
            <div key={k} className={cx("group grid grid-cols-[minmax(0,320px)_1fr_auto] items-center gap-4 px-5 py-3 rounded-md hover:bg-gray-50", deleted && "opacity-50 line-through")}>
              <span className={cx("font-mono text-sm truncate", isNew ? "text-green-600" : changed ? "text-yellow-600" : "")}>{k}</span>
              <span className="font-mono text-sm text-gray-700 truncate">{shown[k] || changed ? (staged.set[k] ?? values[k]) : "*******"}</span>
              <div className="flex items-center gap-1 opacity-60 group-hover:opacity-100">
                {!isNew && <button onClick={() => toggle(k)} className="w-8 h-8 rounded-md flex items-center justify-center text-gray-600 hover:text-foreground">{shown[k] ? <EyeOff size={15} /> : <Eye size={15} />}</button>}
                <Menu trigger={<button className="w-8 h-8 rounded-md flex items-center justify-center text-gray-600 hover:text-foreground"><MoreVertical size={15} /></button>}
                  items={[
                    { label: "Copy value", onClick: async () => { const v = staged.set[k] ?? values[k] ?? (await reveal([k]))[k]; navigator.clipboard.writeText(v || ""); } },
                    { label: "Edit", onClick: async () => { if (values[k] === undefined && !isNew) await reveal([k]); setEditing(k); } },
                    deleted ? { label: "Keep", onClick: () => setStaged((s) => ({ ...s, unset: s.unset.filter((x) => x !== k) })) } : { label: "Remove", danger: true, onClick: () => stageDelete(k) },
                  ]} />
              </div>
            </div>
          );
        })}
      </div>
      {raw !== null && (
        <Modal title="Raw Editor" width={760} onClose={() => setRaw(null)}
          footer={<><Button onClick={() => setRaw(null)}>Cancel</Button><Button variant="primary" onClick={() => {
            const next: Record<string, string> = {};
            raw.split("\n").forEach((line) => { const m = /^\s*([A-Za-z_][A-Za-z0-9_.]*)\s*=(.*)$/.exec(line); if (m) next[m[1]] = m[2]; });
            const set: Record<string, string> = {};
            Object.entries(next).forEach(([k, v]) => { if (values[k] !== v) set[k] = v; });
            const unset = data.names.filter((k) => !(k in next));
            setStaged({ set, unset }); setRaw(null);
          }}>Update Variables</Button></>}>
          <p className="mb-3 text-gray-600">One KEY=value per line. Nothing changes until you press Deploy.</p>
          <textarea value={raw} onChange={(e) => setRaw(e.target.value)} spellCheck={false} className="w-full h-[420px] rounded-md border border-gray-200 bg-background p-3 font-mono text-xs leading-5 outline-none focus:border-pink-400 scroll-thin" />
        </Modal>
      )}
    </div>
  );
}

function VarEditor({ initialKey = "", initialValue = "", lockKey, onSave, onCancel }: { initialKey?: string; initialValue?: string; lockKey?: boolean; onSave: (k: string, v: string) => void; onCancel: () => void }) {
  const [k, setK] = useState(initialKey);
  const [v, setV] = useState(initialValue);
  const ok = /^[A-Za-z_][A-Za-z0-9_.]*$/.test(k) && !/[\r\n]/.test(v);
  return (
    <form onSubmit={(e) => { e.preventDefault(); if (ok) onSave(k, v); }} className="grid grid-cols-[minmax(0,320px)_1fr_auto] items-center gap-3 px-2 py-3">
      <input autoFocus={!lockKey} disabled={lockKey} value={k} onChange={(e) => setK(e.target.value.toUpperCase().replace(/\s/g, "_"))} placeholder="VARIABLE_NAME" className="h-[34px] px-3 rounded-md border border-gray-200 bg-background font-mono text-sm outline-none focus:border-pink-400 disabled:opacity-70" />
      <input autoFocus={lockKey} value={v} onChange={(e) => setV(e.target.value)} placeholder="value" className="h-[34px] px-3 rounded-md border border-gray-200 bg-background font-mono text-sm outline-none focus:border-pink-400" />
      <div className="flex gap-2"><Button type="submit" variant="primary" disabled={!ok}>Add</Button><Button type="button" onClick={onCancel}><X size={14} /></Button></div>
    </form>
  );
}

/* ------------------------------------------------------------------ metrics */

const RANGES: [number, string][] = [[3600, "1h"], [6 * 3600, "6h"], [86400, "1d"], [7 * 86400, "7d"], [14 * 86400, "14d"]];

export function MetricChart({ title, right, points, idx, fmt, color = "hsl(270, 70%, 65%)", second, height = 220 }: {
  title: string; right?: string; points: number[][]; idx: number; fmt: (v: number) => string; color?: string; second?: { idx: number; color: string; label: string }; height?: number;
}) {
  const data = points.map((p) => ({ t: p[0] * 1000, a: p[idx], b: second ? p[second.idx] : undefined }));
  const tick = (t: number) => { const d = new Date(t); return `${d.getHours()}:${String(d.getMinutes()).padStart(2, "0")}`; };
  return (
    <div className="rounded-xl border border-gray-200 bg-background/40 p-5">
      <div className="flex items-center justify-between pb-4 mb-2 border-b border-gray-200">
        <span className="text-[17px] font-semibold">{title}</span>
        <span className="text-sm text-gray-500">{right}</span>
      </div>
      {data.length < 2 ? <div className="flex items-center justify-center text-sm text-gray-500" style={{ height }}>Collecting samples (every 30 seconds)</div> : (
        <ResponsiveContainer width="100%" height={height}>
          <AreaChart data={data} margin={{ top: 8, right: 4, left: 0, bottom: 0 }}>
            <defs>
              <linearGradient id={`g-${title}`} x1="0" y1="0" x2="0" y2="1"><stop offset="0%" stopColor={color} stopOpacity={0.25} /><stop offset="100%" stopColor={color} stopOpacity={0} /></linearGradient>
            </defs>
            <CartesianGrid stroke="hsl(246, 11%, 22%)" strokeOpacity={0.5} vertical={false} />
            <XAxis dataKey="t" type="number" domain={["dataMin", "dataMax"]} tickFormatter={tick} tick={{ fill: "hsl(246,6%,55%)", fontSize: 11 }} axisLine={false} tickLine={false} minTickGap={40} />
            <YAxis tickFormatter={fmt} tick={{ fill: "hsl(246,6%,55%)", fontSize: 11 }} axisLine={false} tickLine={false} width={84} />
            <Tooltip contentStyle={{ background: "hsl(250,21%,11%)", border: "1px solid hsl(246,11%,22%)", borderRadius: 8, fontSize: 12 }} labelFormatter={(t) => fmtDateTime(Number(t) / 1000)} formatter={(v: number, n) => [fmt(v), n === "a" ? title : second?.label]} />
            <Area type="linear" dataKey="a" stroke={color} strokeWidth={1.6} fill={`url(#g-${title})`} isAnimationActive={false} />
            {second && <Area type="linear" dataKey="b" stroke={second.color} strokeWidth={1.6} fill="transparent" isAnimationActive={false} />}
          </AreaChart>
        </ResponsiveContainer>
      )}
    </div>
  );
}

export const fmtCpu = (v: number) => `${v.toFixed(v < 0.1 ? 3 : 2)}\u00a0vCPU`;
export const fmtMem = (v: number) => fmtBytes(v, v > 1e9 ? 2 : 0).replace(" ", "\u00a0");
export const fmtRate = (v: number) => `${fmtBytes(v)}/s`.replace(" ", "\u00a0");

function MetricsTab({ res }: { res: Resource }) {
  const [range, setRange] = useState(3600);
  const { data } = usePoll(() => api.metrics(res.id, range), [res.id, range], 30000);
  return (
    <div>
      <div className="flex justify-end mb-4">
        <div className="flex rounded-md border border-gray-200 p-0.5">
          {RANGES.map(([v, l]) => <button key={v} onClick={() => setRange(v)} className={cx("px-3 h-7 rounded text-sm", v === range ? "bg-gray-100 text-foreground" : "text-gray-500 hover:text-foreground")}>{l}</button>)}
        </div>
      </div>
      {!data ? <div className="flex justify-center py-16"><Spinner /></div> : (
        <div className="grid grid-cols-1 xl:grid-cols-2 gap-4">
          <MetricChart title="CPU" right={data.hostCpus ? `Max ${data.hostCpus} vCPU` : undefined} points={data.points} idx={1} fmt={fmtCpu} />
          <MetricChart title="Memory" right={data.memLimit ? `Limit ${String(res.entry?.memory).toUpperCase()}` : data.hostMem ? `Max ${fmtMem(data.hostMem)}` : undefined} points={data.points} idx={2} fmt={fmtMem} />
          <div className="xl:col-span-2"><MetricChart title="Network" right="ingress / egress" points={data.points} idx={3} fmt={fmtRate} color="hsl(220, 80%, 65%)" second={{ idx: 4, color: "hsl(152, 38%, 60%)", label: "egress" }} /></div>
        </div>
      )}
    </div>
  );
}

/* ------------------------------------------------------------------ settings */

function Section({ title, children, note }: { title: string; children: React.ReactNode; note?: string }) {
  return (
    <section className="py-7 border-b border-gray-200 last:border-b-0">
      <h3 className="text-lg font-semibold mb-1">{title}</h3>
      {note && <p className="text-sm text-gray-600 mb-4">{note}</p>}
      <div className="mt-3 space-y-3">{children}</div>
    </section>
  );
}

function Field({ label, value, mono }: { label: string; value?: React.ReactNode; mono?: boolean }) {
  if (value == null || value === "" || (Array.isArray(value) && !value.length)) return null;
  return (
    <div className="grid grid-cols-[200px_1fr] items-start gap-4 text-sm">
      <span className="text-gray-600 pt-2">{label}</span>
      <div className={cx("min-h-[34px] rounded-md border border-gray-200 bg-background px-3 py-1.5 leading-6 break-all", mono && "font-mono text-[13px]")}>{value}</div>
    </div>
  );
}

function SettingsTab({ res }: { res: Resource }) {
  const e = res.entry || {};
  const src = e.source || {};
  const repoUrl = src.repo ? `https://github.com/${src.repo}` : null;
  return (
    <div className="max-w-[760px]">
      {res.kind === "service" ? (
        <>
          <Section title="Source">
            <Field label="Repository" value={repoUrl && <a className="hover:underline" href={repoUrl} target="_blank" rel="noreferrer">{src.repo}</a>} />
            <Field label="Image" value={src.image} mono />
            <Field label="Branch" value={e.branch || (src.repo ? "default branch" : undefined)} />
            <Field label="Root directory" value={src.context} mono />
            <Field label="Watch paths" value={e.watch?.join("  ")} mono />
            <Field label="Last deployed" value={res.lastVersion} mono />
          </Section>
          <Section title="Networking">
            <Field label="Public domains" value={res.domains?.length ? res.domains.map((d) => <div key={d}>{d}</div>) : undefined} />
            <Field label="Private network" value={res.internal} mono />
            <Field label="Port" value={e.port} mono />
          </Section>
          <Section title="Build">
            <Field label="Builder" value={src.image ? "Prebuilt image" : src.dockerfile || src.repo ? `Dockerfile${src.dockerfile ? ` (${src.dockerfile})` : ""} or Railpack when there is none` : undefined} />
            <Field label="Build arguments" value={e.build_args && Object.keys(e.build_args).join(", ")} mono />
          </Section>
          <Section title="Deploy">
            <Field label="Kind" value={{ web: "Web service (zero-downtime deploys)", worker: "Worker (no HTTP)", cron: "Cron job", direct: "Own ports" }[res.svcKind || "web"]} />
            <Field label="Start command" value={e.cmd} mono />
            <Field label="Cron schedule" value={e.schedule} mono />
            <Field label="Healthcheck path" value={e.health} mono />
            <Field label="Restart policy" value="Docker restarts it on failure" />
            <Field label="Volumes" value={e.volumes?.map((v: string) => <div key={v}>{v}</div>)} mono />
          </Section>
          <Section title="Resources">
            <Field label="Host" value={`${res.hostName} (${res.region})`} />
            <Field label="Memory limit" value={e.memory || "none (shares the host)"} />
            <Field label="Side effects" value={e.side_effects ? "Yes: sends money, emails or webhooks" : undefined} />
          </Section>
        </>
      ) : (
        <Section title="Database">
          <Field label="Host" value={`${res.hostName} (${res.region})`} />
          {res.kind === "pg" && <Field label="Database" value={res.name} mono />}
        </Section>
      )}
    </div>
  );
}

/* ------------------------------------------------------------------ data */

function DataTab({ res }: { res: Resource }) {
  const { data } = usePoll(() => api.data(res.id), [res.id], 60000);
  const [q, setQ] = useState("");
  if (!data) return <div className="flex justify-center py-16"><Spinner /></div>;
  const rows = data.tables.filter((t) => t.name.includes(q.toLowerCase()));
  return (
    <div>
      <div className="flex items-center gap-8 mb-6 text-sm">
        <div><div className="text-gray-500">Size</div><div className="text-xl font-semibold">{fmtBytes(data.size)}</div></div>
        <div><div className="text-gray-500">Connections</div><div className="text-xl font-semibold">{data.connections}</div></div>
        <div><div className="text-gray-500">Tables</div><div className="text-xl font-semibold">{data.tables.length}</div></div>
        <input value={q} onChange={(e) => setQ(e.target.value)} placeholder="Filter tables" className="ml-auto h-[34px] w-[220px] px-3 rounded-md border border-gray-200 bg-background text-sm outline-none focus:border-pink-400" />
      </div>
      <div className="rounded-xl border border-gray-200 overflow-hidden">
        <div className="grid grid-cols-[1fr_140px_120px] bg-background px-5 py-2.5 text-xs font-medium text-gray-600 border-b border-gray-200"><span>Table</span><span className="text-right">Rows (estimate)</span><span className="text-right">Size</span></div>
        {rows.map((t) => (
          <div key={t.name} className="grid grid-cols-[1fr_140px_120px] px-5 py-2 text-sm border-b border-gray-100 last:border-b-0 hover:bg-gray-50">
            <span className="font-mono text-[13px] truncate">{t.name}</span><span className="text-right tabular-nums text-gray-700">{t.rows.toLocaleString()}</span><span className="text-right tabular-nums text-gray-700">{fmtBytes(t.bytes)}</span>
          </div>
        ))}
      </div>
      <p className="mt-3 text-xs text-gray-500">Read only. For queries use psql through PgBouncer.</p>
    </div>
  );
}

