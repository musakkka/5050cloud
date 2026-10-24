// SPDX-License-Identifier: AGPL-3.0-only

// Settings > Deploy: start and pre-deploy commands, teardown, cron schedule,
// health check, restart policy, run-as user and shared memory.

import { useState } from "react";
import { Plus } from "lucide-react";
import CronSchedule from "../cron/CronSchedule";
import { useFieldState, useSettings } from "./context";
import { DocsLink, DropField, PlusField, SaveInput, SelectCard, SettingField, ToggleRow, docs } from "./ui";

const RESTART = [
  { value: "on_failure", label: "On Failure", description: "Restart the container when it exits with a non-zero code." },
  { value: "always", label: "Always", description: "Restart the container whenever it exits, for any reason." },
  { value: "never", label: "Never", description: "Leave the container stopped after it exits." },
] as const;

export default function DeploySection() {
  const { svc, cfg, stage, stagePatch } = useSettings();
  const start = useFieldState("deploy.startCommand");
  const pre = useFieldState("deploy.preDeployCommand");
  const preTo = useFieldState("deploy.preDeployTimeoutSec");
  const hc = useFieldState("deploy.healthcheckPath");
  const hcTo = useFieldState("deploy.healthcheckTimeoutSec");
  const rp = useFieldState("deploy.restartPolicy");
  const rr = useFieldState("deploy.restartMaxRetries");
  const cron = useFieldState("deploy.cronSchedule");
  const user = useFieldState("deploy.runAsUser");
  const shm = useFieldState("deploy.shmSizeMb");
  const overlap = useFieldState("deploy.overlapSec");
  const drain = useFieldState("deploy.drainingSec");
  const [preOpen, setPreOpen] = useState(false);
  const [teardownOpen, setTeardownOpen] = useState(false);
  const policy = (cfg.deploy.restartPolicy || "on_failure") as (typeof RESTART)[number]["value"];
  const teardown = teardownOpen || !!cfg.deploy.overlapSec || cfg.deploy.drainingSec !== undefined;
  const isWorker = svc.kind === "worker" || svc.kind === "cron";

  return (
    <>
      <SettingField title="Custom Start Command" keywords="start command run entrypoint pre-deploy predeploy migrate" staged={start.staged || pre.staged} locked={start.locked}
        description={<>The command that starts each new deployment. <DocsLink href={docs("SPEC.md")} /></>}>
        <PlusField label="Start Command" value={cfg.deploy.startCommand || ""} placeholder="npm run start" mono disabled={!!start.locked}
          onSave={(v) => stage("deploy.startCommand", v)} />
        {!cfg.deploy.preDeployCommand && !preOpen && (
          <div className="mt-3 flex items-center gap-1.5 text-sm text-fg-2">
            <Plus size={14} />
            <button type="button" onClick={() => setPreOpen(true)} className="underline decoration-fg-4 underline-offset-2 transition-colors hover:text-foreground">Add pre-deploy step</button>
            <span>(<DocsLink href={docs("SPEC.md")} />)</span>
          </div>
        )}
      </SettingField>

      {(preOpen || !!cfg.deploy.preDeployCommand) && (
        <SettingField title="Pre-deploy Command" keywords="pre-deploy predeploy migrate migration release" staged={pre.staged || preTo.staged} locked={pre.locked}
          description="Runs once in a new container before the deployment takes traffic, for example database migrations. A failure stops the deploy.">
          <PreDeploy onEmpty={() => setPreOpen(false)} />
          {cfg.deploy.preDeployCommand && (
            <SaveInput className="mt-2" label="Pre-deploy timeout" type="number" value={cfg.deploy.preDeployTimeoutSec ? String(cfg.deploy.preDeployTimeoutSec) : ""} placeholder="300" suffix="seconds"
              validate={(v) => (Number(v) >= 1 && Number(v) <= 3600 ? null : "1 to 3600 seconds")}
              onSave={(v) => stage("deploy.preDeployTimeoutSec", v === "" ? "" : Number(v))} />
          )}
        </SettingField>
      )}

      {!isWorker && (
        <SettingField title="Teardown" keywords="teardown overlap draining sigterm zero downtime old deployment" staged={overlap.staged || drain.staged}
          description={<>How the old deployment stops once the new one is live. <DocsLink href={docs("SPEC.md")} /></>}>
          <ToggleRow checked={teardown} label="Enable Teardown"
            onChange={(v) => { if (v) setTeardownOpen(true); else { setTeardownOpen(false); void stagePatch({ deploy: { overlapSec: null, drainingSec: null } }); } }} />
          {teardown && (
            <div className="mt-3 grid grid-cols-2 gap-2 animate-rise-in">
              <div>
                <div className="mb-1.5 text-sm text-fg-2">Overlap</div>
                <SaveInput label="Overlap seconds" type="number" value={cfg.deploy.overlapSec ? String(cfg.deploy.overlapSec) : ""} placeholder="0" suffix="seconds"
                  onSave={(v) => stage("deploy.overlapSec", v === "" ? "" : Number(v))} />
              </div>
              <div>
                <div className="mb-1.5 text-sm text-fg-2">Draining</div>
                <SaveInput label="Draining seconds" type="number" value={cfg.deploy.drainingSec !== undefined ? String(cfg.deploy.drainingSec) : ""} placeholder="30" suffix="seconds"
                  onSave={(v) => stage("deploy.drainingSec", v === "" ? "" : Number(v))} />
              </div>
              <div className="col-span-2 text-caption text-fg-3">Overlap keeps the old deployment serving after cutover; draining is the time between SIGTERM and SIGKILL.</div>
            </div>
          )}
        </SettingField>
      )}

      <SettingField title="Cron Schedule" keywords="cron schedule job timer recurring" staged={cron.staged} locked={cron.locked}
        description="Run the service on a schedule instead of keeping it up. Times are in the zone you choose.">
        <CronSchedule disabled={!!cron.locked}
          value={{ schedule: cfg.deploy.cronSchedule || "", timezone: cfg.deploy.cronTimezone || "", overlap: cfg.deploy.cronOverlap || "", timeoutSec: cfg.deploy.cronTimeoutSec }}
          onStage={(p) => stagePatch({ deploy: Object.fromEntries(Object.entries(p).map(([k, v]) => [k, v === "" ? null : v])) })}
          renderDropdown={(p) => <DropField label={p.label} value={p.value} options={p.options} onChange={p.onChange} />} />
      </SettingField>

      {!isWorker && (
        <SettingField title="Healthcheck Path" keywords="healthcheck health check path readiness probe timeout" staged={hc.staged || hcTo.staged} locked={hc.locked}
          description={<>An endpoint that must answer 200 before a deploy completes. Without one, the target port must accept TCP. <DocsLink href={docs("SPEC.md")} /></>}>
          <PlusField label="Healthcheck Path" value={cfg.deploy.healthcheckPath || ""} placeholder="/health" mono disabled={!!hc.locked}
            validate={(v) => (v.startsWith("/") ? null : "Start the path with /")}
            onSave={(v) => stage("deploy.healthcheckPath", v)} />
          {cfg.deploy.healthcheckPath && (
            <SaveInput className="mt-2" label="Healthcheck timeout" type="number" value={cfg.deploy.healthcheckTimeoutSec ? String(cfg.deploy.healthcheckTimeoutSec) : ""} placeholder="300" suffix="seconds"
              validate={(v) => (Number(v) >= 1 ? null : "At least 1 second")}
              onSave={(v) => stage("deploy.healthcheckTimeoutSec", v === "" ? "" : Number(v))} />
          )}
        </SettingField>
      )}

      <SettingField title="Serverless" keywords="serverless sleep scale to zero idle"
        description="Stop the container when no traffic arrives for a while and start it on the next request.">
        <ToggleRow checked={false} onChange={() => undefined} label="Enable Serverless" disabled reason="Not in v0.1: scale to zero comes in v0.2; idle apps only cost RAM on your own host" />
      </SettingField>

      <SettingField title="Restart Policy" keywords="restart policy crash on failure always never retries" staged={rp.staged || rr.staged} locked={rp.locked}
        description={<>What to do when the process exits. <DocsLink href={docs("SPEC.md")} /></>}>
        <SelectCard compact width={360} label="Restart policy" value={policy} disabled={!!rp.locked}
          options={RESTART.map((r) => ({ value: r.value, label: r.label, description: r.description }))}
          onChange={(v) => void stage("deploy.restartPolicy", v === "on_failure" ? "" : v)} />
        {policy === "on_failure" && (
          <>
            <div className="mb-2 mt-4 text-sm text-fg-2">How many times to restart the service after it stops with an error.</div>
            <SaveInput label="Max restart retries" type="number" value={String(cfg.deploy.restartMaxRetries ?? 10)}
              validate={(v) => (Number(v) >= 0 && Number(v) <= 100 ? null : "0 to 100")}
              onSave={(v) => stage("deploy.restartMaxRetries", v === "" || Number(v) === 10 ? "" : Number(v))} />
          </>
        )}
      </SettingField>

      <SettingField title="Run as User" keywords="user uid gid run as root" staged={user.staged} locked={user.locked}
        description="The user and group the process runs as. Empty uses the image's default.">
        <PlusField label="Run as User" value={cfg.deploy.runAsUser || ""} placeholder="1000:1000" mono disabled={!!user.locked}
          validate={(v) => (/^\d+(:\d+)?$/.test(v) ? null : "Use a uid, or uid:gid such as 1000:1000")}
          onSave={(v) => stage("deploy.runAsUser", v)} />
      </SettingField>

      <SettingField title="Shared Memory" keywords="shm shared memory dev shm" staged={shm.staged} locked={shm.locked}
        description="Size of /dev/shm in the container. Browsers and some databases want more than the 64 MB default.">
        <PlusField label="Shared Memory" type="number" value={cfg.deploy.shmSizeMb ? String(cfg.deploy.shmSizeMb) : ""} placeholder="64" suffix="MB" disabled={!!shm.locked}
          validate={(v) => (Number(v) >= 1 ? null : "At least 1 MB")}
          onSave={(v) => stage("deploy.shmSizeMb", v === "" ? "" : Number(v))} />
      </SettingField>
    </>
  );
}

function PreDeploy({ onEmpty }: { onEmpty: () => void }) {
  const { cfg, stage } = useSettings();
  return (
    <SaveInput label="Pre-deploy command" value={cfg.deploy.preDeployCommand || ""} placeholder="npm run migrate" mono
      onSave={async (x) => { await stage("deploy.preDeployCommand", x); if (!x) onEmpty(); }} />
  );
}
