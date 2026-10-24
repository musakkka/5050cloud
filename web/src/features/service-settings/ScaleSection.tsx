// SPDX-License-Identifier: AGPL-3.0-only

// Settings > Scale: where replicas run (host roles or pinned hosts) and how
// many, rollout surge, per-replica CPU and memory limits as sliders bounded
// by the largest eligible host, the memory reservation, and processes.

import { useEffect, useMemo, useState } from "react";
import { Cpu, Database, Server } from "lucide-react";
import type { Host } from "../../api/types.gen";
import { get } from "../service/client";
import { useLoad } from "../service/ui";
import { cx } from "../../kit";
import { useFieldState, useSettings } from "./context";
import { DropField, INPUT, SaveInput, SettingField, TintNote, ToggleRow } from "./ui";

const fmtCpu = (n: number) => `${Number.isInteger(n) ? n : n.toFixed(2).replace(/0$/, "")} vCPU`;
const fmtMem = (mb: number) => (mb >= 1024 ? `${(mb / 1024).toFixed(mb % 1024 ? 1 : 0)} GB` : `${mb} MB`);

export default function ScaleSection() {
  const { svc, cfg, stage, stagePatch } = useSettings();
  const hosts = useLoad(() => get<Host[]>("/hosts").catch(() => [] as Host[]), []);
  const placeSt = useFieldState("deploy.placement");
  const repSt = useFieldState("deploy.replicas");
  const surgeSt = useFieldState("deploy.maxSurge");
  const cpuSt = useFieldState("deploy.cpuLimit");
  const memSt = useFieldState("deploy.memoryLimitMb");
  const resSt = useFieldState("deploy.memoryReservationMb");
  const roles = cfg.deploy.placement?.roles?.length ? cfg.deploy.placement.roles : [svc.config.template ? "data" : "app"];
  const pinned = cfg.deploy.placement?.hosts?.[0];
  const hostList = (hosts.data || []).filter((h) => h.name && h.id !== "id_01jexample0000000000000000");
  const value = pinned ? `host:${pinned}` : `role:${roles[0]}`;
  const options = [
    { value: "role:app", label: "Any app host", icon: <Server size={18} className="text-fg-3" /> },
    { value: "role:data", label: "Any data host", icon: <Database size={18} className="text-fg-3" /> },
    ...hostList.map((h) => ({ value: `host:${h.id}`, label: `Pinned to ${h.name}`, icon: <Cpu size={18} className="text-fg-3" /> })),
  ];
  if (!options.some((o) => o.value === value)) options.push({ value, label: pinned ? `Pinned to ${pinned}` : `Hosts with role ${roles[0]}`, icon: <Server size={18} className="text-fg-3" /> });
  const eligible = hostList.filter((h) => (pinned ? h.id === pinned : roles.some((r) => h.roles.includes(r))));
  const maxCpu = Math.max(1, ...eligible.map((h) => h.cpus || 0));
  const maxMemMb = Math.max(512, ...eligible.map((h) => Math.floor((h.memBytes || 0) / 1048576)));
  const running = [...new Set(svc.replicas.map((r) => r.hostName))];
  const replicas = cfg.deploy.replicas ?? 1;

  return (
    <>
      <SettingField title="Placement & Replicas" keywords="placement replicas region hosts roles pinned horizontal scale" staged={placeSt.staged || repSt.staged} locked={repSt.locked}
        description="Choose which hosts run this service and how many replicas to keep running.">
        <div className="flex gap-2">
          <DropField label="Placement" className="flex-1" value={value} options={options}
            onChange={(v) => {
              const [kind, id] = v.split(":");
              void stagePatch({ deploy: { placement: kind === "host" ? { hosts: [id], roles: null } : { roles: [id], hosts: null } } });
            }} />
          <ReplicaInput value={replicas} disabled={!!svc.volume || !!repSt.locked} onSave={(n) => stage("deploy.replicas", n === 1 ? "" : n)} />
        </div>
        <div className="mt-3">
          {svc.volume ? (
            <TintNote>A service with a volume runs one replica, and deploys stop the old container first.</TintNote>
          ) : (
            <div className="text-sm text-fg-2">Running now on {running.length ? running.join(", ") : "no hosts"}.</div>
          )}
        </div>
      </SettingField>

      {!svc.volume && (
        <SettingField title="Rollout Surge" keywords="max surge rollout wave rolling deploy" staged={surgeSt.staged}
          description="How many new replicas start in each wave of a deploy: a number, or a share of the replicas such as 25%.">
          <SaveInput label="Max surge" value={cfg.deploy.maxSurge || ""} placeholder="1" mono
            validate={(v) => (/^\d+%?$/.test(v) ? null : "Use a number such as 2, or a percentage such as 25%")}
            onSave={(v) => stage("deploy.maxSurge", v)} />
        </SettingField>
      )}

      <SettingField title="Replica Limits" keywords="cpu memory limit vcpu ram resources replica limits" staged={cpuSt.staged || memSt.staged}
        description="The most CPU and memory each replica may use. Slide to the end for no limit.">
        <div className="rounded-md border border-line">
          <LimitSlider label="CPU" unit="vCPU" value={cfg.deploy.cpuLimit || 0} max={maxCpu} step={0.25} min={0.25} format={fmtCpu}
            limitText={`Host limit: ${fmtCpu(maxCpu)}`} onCommit={(v) => stage("deploy.cpuLimit", v)} />
          <div className="h-px bg-line" />
          <LimitSlider label="Memory" unit="MB" value={cfg.deploy.memoryLimitMb || 0} max={maxMemMb} step={128} min={128} format={fmtMem}
            limitText={`Host limit: ${fmtMem(maxMemMb)}`} onCommit={(v) => stage("deploy.memoryLimitMb", v)} />
        </div>
      </SettingField>

      <SettingField title="Memory Reservation" keywords="memory reservation placement reserve" staged={resSt.staged}
        description="What placement sets aside on a host for each replica. Defaults to the memory limit, else 256 MB.">
        <SaveInput label="Memory reservation" type="number" value={String(cfg.deploy.memoryReservationMb ?? "")} placeholder={String(cfg.deploy.memoryLimitMb || 256)} suffix="MB"
          validate={(v) => (Number(v) >= 16 ? null : "At least 16 MB")} onSave={(v) => stage("deploy.memoryReservationMb", v === "" ? "" : Number(v))} />
      </SettingField>

      <Processes />
    </>
  );
}

function ReplicaInput({ value, onSave, disabled }: { value: number; onSave: (n: number) => Promise<unknown>; disabled?: boolean }) {
  const [t, setT] = useState(String(value));
  useEffect(() => setT(String(value)), [value]);
  const n = Number(t);
  const ok = Number.isInteger(n) && n >= 1 && n <= 50;
  return (
    <label className="relative block w-[124px] shrink-0">
      <input aria-label="Replicas" inputMode="numeric" value={t} disabled={disabled} aria-invalid={!ok || undefined}
        onChange={(e) => setT(e.target.value.replace(/\D/g, ""))}
        onBlur={() => { if (ok && n !== value) void onSave(n).catch(() => setT(String(value))); else if (!ok) setT(String(value)); }}
        onKeyDown={(e) => { if (e.key === "Enter") (e.target as HTMLInputElement).blur(); if (e.key === "ArrowUp" && n < 50) setT(String(n + 1)); if (e.key === "ArrowDown" && n > 1) setT(String(n - 1)); }}
        className={cx(INPUT, "h-11 pr-[72px]")} />
      <span className="pointer-events-none absolute right-3 top-1/2 -translate-y-1/2 text-sm text-fg-3">{n === 1 ? "Replica" : "Replicas"}</span>
    </label>
  );
}

/** A limit slider: 8px track, accent fill, 16px square thumb; the far end means unlimited (0). */
function LimitSlider({ label, value, max, min, step, format, limitText, onCommit }: {
  label: string; unit: string; value: number; max: number; min: number; step: number; format: (n: number) => string; limitText: string; onCommit: (v: number) => Promise<unknown>;
}) {
  const shown = value <= 0 || value > max ? max : value;
  const [v, setV] = useState(shown);
  useEffect(() => setV(shown), [shown]);
  const pct = ((v - min) / Math.max(max - min, step)) * 100;
  const commit = () => {
    const next = v >= max ? 0 : v;
    if (next !== (value || 0)) void onCommit(next).catch(() => setV(shown));
  };
  const unlimited = v >= max;
  return (
    <div className="px-4 py-4">
      <div className="mb-3 flex items-center justify-between text-sm">
        <span className="text-fg-2">{label}: <b className="font-semibold text-foreground">{unlimited ? `${format(max)} (no limit)` : format(v)}</b></span>
        <span className="text-fg-2">{limitText}</span>
      </div>
      <div className="relative h-4">
        <div className="absolute inset-x-0 top-1/2 h-2 -translate-y-1/2 rounded-full bg-line" />
        <div className="absolute left-0 top-1/2 h-2 -translate-y-1/2 rounded-full bg-pink-300 transition-[width] duration-75" style={{ width: `${Math.max(0, Math.min(100, pct))}%` }} />
        <div className="pointer-events-none absolute top-0 h-4 w-4 rounded border-2 border-white bg-secondaryBg transition-[left] duration-75" style={{ left: `calc(${Math.max(0, Math.min(100, pct))}% - ${(pct / 100) * 16}px)` }} />
        <input type="range" aria-label={`${label} limit`} min={min} max={max} step={step} value={v}
          aria-valuetext={unlimited ? "No limit" : format(v)}
          onChange={(e) => setV(Number(e.target.value))} onPointerUp={commit} onKeyUp={commit} onBlur={commit}
          className="absolute inset-0 h-4 w-full cursor-pointer opacity-0" />
      </div>
    </div>
  );
}

function Processes() {
  const { svc, cfg, stage } = useSettings();
  const st = useFieldState("deploy.processes");
  const auto = cfg.deploy.processes === "auto";
  const text = useMemo(() => svc.processesText || `Runs ${svc.processes || 1} process${svc.processes === 1 ? "" : "es"} per replica.`, [svc]);
  return (
    <SettingField title="Processes" keywords="processes cores cluster web concurrency workers threads" staged={st.staged} locked={st.locked}
      description={<>{text} Node apps started with <code className="font-mono text-[13px]">node file.js</code> run in cluster mode; every runtime gets <code className="font-mono text-[13px]">WEB_CONCURRENCY</code>.</>}>
      <ToggleRow checked={auto} label="Use all cores" disabled={!!st.locked} onChange={(v) => void stage("deploy.processes", v ? "auto" : "")} />
      {!auto && (
        <SaveInput className="mt-2" label="Processes per replica" value={cfg.deploy.processes || ""} placeholder="1" suffix="processes"
          validate={(v) => (Number(v) >= 1 && Number(v) <= 32 ? null : "1 to 32")} onSave={(v) => stage("deploy.processes", v)} />
      )}
    </SettingField>
  );
}
