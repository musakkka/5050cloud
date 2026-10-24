// SPDX-License-Identifier: AGPL-3.0-only

// Deployment drawer, Details tab: the status bar with its step list and
// diagnosis, the variables snapshot, where the deploy came from, and the
// configuration it ran with (Pretty cards or the config file as code).

import { useMemo, useState, type ReactNode } from "react";
import { Box, ChevronDown, ChevronRight, Cloud, Code2, FileCode, GitBranch, Info, List } from "lucide-react";
import type { DeploymentDetail } from "../../api/types.gen";
import { Button, CopyButton, Notice, cx } from "../../kit";
import { lookOf, outcomeText, viaLabel } from "./format";
import { AuthorAvatar, OUTCOME_TEXT, OutcomeIcon, StepRows } from "./parts";

export default function Details({ d, onRedeploy, onSettings }: { d: DeploymentDetail; onRedeploy?: () => void; onSettings?: () => void }) {
  const look = lookOf(d);
  return (
    <div className="space-y-6 px-4 py-8 sm:px-12">
      {d.notices?.map((n) => <Notice key={n} tone="warn">{n}</Notice>)}
      {d.steps.length > 0 && <StatusBlock d={d} look={look} onRedeploy={onRedeploy} onSettings={onSettings} />}
      <Variables names={d.variableNames} />
      <Origin d={d} />
      <Configuration d={d} />
    </div>
  );
}

function StatusBlock({ d, look, onRedeploy, onSettings }: { d: DeploymentDetail; look: ReturnType<typeof lookOf>; onRedeploy?: () => void; onSettings?: () => void }) {
  const [open, setOpen] = useState(look === "bad" || look === "busy" || look === "wait" || (d.status === "COMPLETED" && !!d.diagnosis));
  const frame = look === "bad" ? "border-danger-line" : look === "busy" || look === "wait" ? "border-blue-200" : "border-green-200";
  const head = look === "bad" ? "bg-danger-bg" : look === "busy" || look === "wait" ? "bg-blue-50" : "bg-green-50";
  return (
    <div className={cx("overflow-hidden rounded-md border", frame)}>
      <button type="button" onClick={() => setOpen((o) => !o)} aria-expanded={open}
        className={cx("flex h-11 w-full items-center gap-4 px-4 text-left text-sm transition-[filter] duration-150 hover:brightness-110", head, open && "border-b", frame)}>
        <OutcomeIcon look={look} size={18} />
        <span className={cx("min-w-0 flex-1 truncate", OUTCOME_TEXT[look])}>{outcomeText(d)}</span>
        <span className="inline-flex items-center gap-2 text-fg-2">{open ? "View less" : "View more"}<ChevronDown size={16} className={cx("transition-transform duration-150", open && "rotate-180")} /></span>
      </button>
      {open && (
        <StepRows d={d} indent={16} labelGap={32}
          after={d.diagnosis ? <Diagnosis text={d.diagnosis} onRedeploy={onRedeploy} onSettings={onSettings} /> : undefined} />
      )}
      {open && d.diagnosis && !d.steps.some((s) => s.state === "failed") && (d.status === "COMPLETED"
        ? <Diagnosis title="Ran to completion" text={d.diagnosis} />
        : <Diagnosis text={d.diagnosis} onRedeploy={onRedeploy} onSettings={onSettings} />)}
    </div>
  );
}

function Diagnosis({ title = "Diagnosis", text, onRedeploy, onSettings }: { title?: string; text: string; onRedeploy?: () => void; onSettings?: () => void }) {
  return (
    <div className="flex pb-4 pl-4 pr-4 pt-3">
      <span className="flex w-4 shrink-0 flex-col items-center">
        <Info size={16} className="shrink-0 text-accent-text" />
        <span className="mt-2 w-px flex-1 bg-pink-200" />
      </span>
      <div className="ml-8 min-w-0 flex-1">
        <div className="text-sm font-medium text-accent-text">{title}</div>
        <p className="mt-3 text-sm leading-[22.75px] text-foreground">{text}</p>
        {(onRedeploy || onSettings) && (
          <div className="mt-3 flex flex-wrap gap-2">
            {onSettings && <Button size="xs" variant="primary" onClick={onSettings}>Open service settings</Button>}
            {onRedeploy && <Button size="xs" variant="accent-outline" onClick={onRedeploy}>Redeploy</Button>}
          </div>
        )}
      </div>
    </div>
  );
}

function Variables({ names }: { names: string[] }) {
  const [open, setOpen] = useState(false);
  if (!names.length) return null;
  return (
    <div className="rounded-lg bg-surface-hover">
      <button type="button" onClick={() => setOpen((o) => !o)} aria-expanded={open}
        className="flex h-[52px] w-full items-center gap-3 rounded-lg px-4 text-left text-sm text-foreground">
        <ChevronRight size={16} className={cx("text-fg-2 transition-transform duration-150", open && "rotate-90")} />
        {names.length} Variable{names.length === 1 ? "" : "s"}
      </button>
      {open && (
        <div className="animate-collapsible-down space-y-6 overflow-hidden px-4 pb-6 pt-2">
          {names.map((n) => (
            <div key={n} className="grid grid-cols-[minmax(0,1fr)_minmax(0,1fr)] items-center gap-4">
              <span className="truncate font-mono text-sm text-foreground" title={n}>{n}</span>
              <span className="flex h-12 items-center rounded-md bg-tint-active px-4 font-mono text-sm tracking-widest text-fg-3" aria-label="Sealed value">••••••••••••</span>
            </div>
          ))}
          <p className="text-caption text-fg-3">Values are kept sealed in the snapshot. The Variables tab shows the current values.</p>
        </div>
      )}
    </div>
  );
}

function Origin({ d }: { d: DeploymentDetail }) {
  const src = d.config.source;
  const repo = src.type === "github" ? src.repo?.replace(/^https:\/\/github\.com\//, "") : undefined;
  const via = d.commit && (d.trigger === "github" || d.trigger === "commit" || d.trigger === "redeploy") ? "GitHub" : d.image && !d.commit ? "Docker image" : viaLabel(d.trigger);
  return (
    <section>
      <h3 className="mb-3 text-sm font-semibold text-foreground">Deployed via {via}</h3>
      <div className="flex gap-4 rounded-md bg-surface-hover p-4">
        <AuthorAvatar d={d} />
        <div className="min-w-0 flex-1">
          {d.commit ? (
            <>
              <p className="whitespace-pre-line text-sm text-foreground">{d.commit.message.trim()}</p>
              <p className="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 text-caption text-fg-3">
                {repo && <span>{repo}</span>}
                {d.commit.branch && <span className="inline-flex items-center gap-1"><GitBranch size={13} />{d.commit.branch}</span>}
                {d.commit.url
                  ? <a href={d.commit.url} target="_blank" rel="noreferrer" className="font-mono transition-colors duration-150 hover:text-foreground">{d.commit.sha.slice(0, 7)}</a>
                  : <span className="font-mono">{d.commit.sha.slice(0, 7)}</span>}
                {!repo && <span>{d.commit.author}</span>}
              </p>
            </>
          ) : d.image ? (
            <>
              <p className="flex items-center gap-1 break-all font-mono text-sm text-foreground">{d.image}<CopyButton text={d.image} label="Copy image" /></p>
              {d.digest && <p className="mt-1 flex items-center gap-1 break-all font-mono text-caption text-fg-3">{d.digest}<CopyButton text={d.digest} label="Copy digest" size={20} /></p>}
            </>
          ) : (
            <>
              <p className="text-sm text-foreground">{d.message || (d.rollbackOf ? `Rollback to ${d.rollbackOf.slice(0, 8)}` : `Deployment ${d.id.slice(0, 8)}`)}</p>
              <p className="mt-1 text-sm text-fg-3">{(d.actor || "").replace(/^(github|token|agent):/, "") || viaLabel(d.trigger)}</p>
            </>
          )}
        </div>
      </div>
    </section>
  );
}

const BUILDER: Record<string, string> = { auto: "Automatic", railpack: "Railpack", dockerfile: "Dockerfile", nixpacks: "Railpack (Nixpacks compatible)" };
const POLICY: Record<string, string> = { on_failure: "on failure", always: "always", never: "never" };

function clean(v: unknown): unknown {
  if (Array.isArray(v)) return v.length ? v : undefined;
  if (v && typeof v === "object") {
    const o: Record<string, unknown> = {};
    for (const [k, x] of Object.entries(v)) { const c = clean(x); if (c !== undefined && c !== "") o[k] = c; }
    return Object.keys(o).length ? o : undefined;
  }
  return v === null ? undefined : v;
}

function toYaml(v: unknown, ind = ""): string {
  if (v && typeof v === "object" && !Array.isArray(v)) {
    return Object.entries(v).map(([k, x]) => (x && typeof x === "object" ? `${ind}${k}:\n${toYaml(x, ind + "  ")}` : `${ind}${k}: ${JSON.stringify(x)}`)).join("\n");
  }
  if (Array.isArray(v)) return v.map((x) => `${ind}- ${JSON.stringify(x)}`).join("\n");
  return `${ind}${JSON.stringify(v)}`;
}

function Configuration({ d }: { d: DeploymentDetail }) {
  const [mode, setMode] = useState<"pretty" | "code">("pretty");
  const [fmt, setFmt] = useState<"json" | "yaml">(d.configFile?.format === "yaml" ? "yaml" : "json");
  const b = d.config.build, dp = d.config.deploy;
  const file = (k: string) => d.source?.[k];
  const build: [string, ReactNode, string?][] = [
    ["Builder", BUILDER[b.builder || "auto"] || b.builder, "build.builder"],
    ...(b.dockerfilePath ? [["Dockerfile path", `/${b.dockerfilePath.replace(/^\//, "")}`, "build.dockerfilePath"] as [string, ReactNode, string]] : []),
    ...(b.buildCommand ? [["Build command", b.buildCommand, "build.buildCommand"] as [string, ReactNode, string]] : []),
    ...(d.config.source.rootDir && d.config.source.rootDir !== "/" ? [["Root directory", d.config.source.rootDir, "source.rootDir"] as [string, ReactNode, string]] : []),
    ...(b.watchPaths?.length ? [["Watch paths", b.watchPaths.join(", "), "build.watchPaths"] as [string, ReactNode, string]] : []),
  ];
  const where = dp.placement?.hosts?.length ? dp.placement.hosts.join(", ") : (dp.placement?.roles || ["app"]).map((r) => `${r} hosts`).join(", ");
  const deploy: [string, ReactNode, string?][] = [
    ...(dp.startCommand ? [["Start command", dp.startCommand, "deploy.startCommand"] as [string, ReactNode, string]] : []),
    ...(dp.preDeployCommand ? [["Pre-deploy command", dp.preDeployCommand, "deploy.preDeployCommand"] as [string, ReactNode, string]] : []),
    ...(dp.cronSchedule ? [["Cron schedule", `${dp.cronSchedule} (${dp.cronTimezone || "UTC"})`, "deploy.cronSchedule"] as [string, ReactNode, string]] : []),
    ["Placement", where, "deploy.placement"],
    ["Number of replicas", String(dp.replicas ?? 1), "deploy.replicas"],
    ...(dp.healthcheckPath ? [["Healthcheck path", dp.healthcheckPath, "deploy.healthcheckPath"] as [string, ReactNode, string]] : []),
    ["Restart policy", POLICY[dp.restartPolicy || "on_failure"] || dp.restartPolicy, "deploy.restartPolicy"],
    ...((dp.restartPolicy || "on_failure") === "on_failure" ? [["Restart policy max retries", String(dp.restartMaxRetries ?? 10), "deploy.restartMaxRetries"] as [string, ReactNode, string]] : []),
  ];
  const code = useMemo(() => {
    const body = clean({ build: d.config.build, deploy: d.config.deploy }) || {};
    return fmt === "json" ? JSON.stringify(body, null, 2) : toYaml(body);
  }, [d.config, fmt]);
  const fileName = d.configFile?.path || (fmt === "json" ? "5050.json" : "5050.yaml");

  return (
    <section>
      <div className="mb-4 flex items-center justify-between">
        <h3 className="text-sm font-semibold text-foreground">Configuration</h3>
        <div role="radiogroup" aria-label="Configuration view" className="inline-flex">
          {([["pretty", "Pretty", <List key="i" size={16} />], ["code", "Code", <Code2 key="i" size={16} />]] as const).map(([v, label, icon], i) => (
            <button key={v} type="button" role="radio" aria-checked={mode === v} onClick={() => setMode(v)}
              className={cx("inline-flex h-[38px] min-w-[89px] items-center justify-center gap-2 border border-line px-3 text-sm transition-colors duration-150",
                i === 0 ? "rounded-l-md" : "-ml-px rounded-r-md", mode === v ? "text-foreground" : "text-fg-3 hover:text-fg-2")}>
              {icon}{label}
            </button>
          ))}
        </div>
      </div>
      {mode === "pretty" ? (
        <div className="grid gap-2 sm:grid-cols-2">
          <ConfigCard icon={<Box size={16} />} title="Build" rows={build} file={file} />
          <ConfigCard icon={<Cloud size={16} />} title="Deploy" rows={deploy} file={file} />
        </div>
      ) : (
        <div className="space-y-4">
          <div className="flex items-center gap-3 rounded-md bg-blue-50 px-4 py-3 text-sm text-blue-600">
            <Info size={16} className="shrink-0" />
            <span>Save this in a file called <b className="font-semibold">{fileName}</b> at the root of your repo to keep this deploy config in code.</span>
          </div>
          <div className="relative rounded-md bg-surface-hover">
            <div className="absolute right-4 top-4 flex items-center gap-2">
              <select aria-label="Format" value={fmt} onChange={(e) => setFmt(e.target.value as "json" | "yaml")}
                className="h-[34px] rounded-md border border-line bg-transparent px-3 text-sm text-foreground outline-none hover:border-fg-5 focus:border-accent">
                <option value="json">JSON</option><option value="yaml">YAML</option>
              </select>
              <CopyButton text={code} label="Copy config" size={34} className="border border-line" />
            </div>
            <pre className="scroll-thin overflow-x-auto p-6 pr-44 font-mono text-sm leading-6 text-foreground">{code}</pre>
          </div>
        </div>
      )}
    </section>
  );
}

function ConfigCard({ icon, title, rows, file }: { icon: ReactNode; title: string; rows: [string, ReactNode, string?][]; file: (k: string) => string | undefined }) {
  return (
    <div className="self-start rounded-md bg-surface-hover p-4">
      <div className="flex items-center gap-4 text-sm font-semibold text-foreground"><span className="text-fg-2">{icon}</span>{title}</div>
      <dl className="ml-8 mt-1">
        {rows.map(([k, v, key], i) => (
          <div key={k} className={cx("py-3", i > 0 && "border-t border-line")}>
            <dt className="flex items-center gap-1.5 text-caption text-fg-3">
              {k}
              {key && file(key) && <span title={`Set by ${file(key)}`} className="inline-flex text-fg-4"><FileCode size={12} /></span>}
            </dt>
            <dd className="mt-0.5 break-words text-lead text-foreground">{v}</dd>
          </div>
        ))}
      </dl>
    </div>
  );
}
