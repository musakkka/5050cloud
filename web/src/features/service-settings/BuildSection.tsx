// SPDX-License-Identifier: AGPL-3.0-only

// Settings > Build (repo services): builder card, Dockerfile path or
// Railpack version, custom build command, watch paths and build timeout.

import { useState } from "react";
import { Check, X } from "lucide-react";
import { IconButton, cx } from "../../kit";
import { useFieldState, useSettings } from "./context";
import { DocsLink, INPUT, Pill, PlusField, SelectCard, SettingField, docs, type CardOption } from "./ui";

type Builder = "" | "railpack" | "dockerfile" | "nixpacks";

export default function BuildSection() {
  const { cfg, stage } = useSettings();
  const builder = (cfg.build.builder === "auto" ? "" : cfg.build.builder || "") as Builder;
  const b = useFieldState("build.builder");
  const df = useFieldState("build.dockerfilePath");
  const rv = useFieldState("build.railpackVersion");
  const bc = useFieldState("build.buildCommand");
  const to = useFieldState("build.timeoutMin");
  const options: CardOption<Builder>[] = [
    { value: "", label: "Auto", pill: <Pill tone="info">Default</Pill>, description: "Uses the Dockerfile when the repo has one, else Railpack." },
    {
      value: "railpack", label: "Railpack",
      meta: <span className="font-mono text-[13px] text-fg-3">{cfg.build.railpackVersion || "current"}</span>,
      description: "Detects the language and builds an image with zero config.",
    },
    { value: "dockerfile", label: "Dockerfile", description: "Builds the Dockerfile in the root directory with BuildKit." },
    { value: "nixpacks", label: "Nixpacks", pill: <Pill tone="warn">Compat</Pill>, description: "Reads Nixpacks config and builds it with Railpack." },
  ];

  return (
    <>
      <SettingField title="Builder" keywords="builder railpack dockerfile nixpacks buildkit" staged={b.staged} locked={b.locked}>
        <SelectCard label="Builder" value={builder} options={options} disabled={!!b.locked} onChange={(v) => void stage("build.builder", v)} />
      </SettingField>

      {builder === "dockerfile" && (
        <SettingField title="Dockerfile Path" keywords="dockerfile path" staged={df.staged} locked={df.locked}
          description="Relative to the root directory.">
          <PlusField label="Dockerfile Path" value={cfg.build.dockerfilePath || ""} placeholder="Dockerfile" mono disabled={!!df.locked}
            onSave={(v) => stage("build.dockerfilePath", v)} />
        </SettingField>
      )}

      {(builder === "" || builder === "railpack" || builder === "nixpacks") && (
        <SettingField title="Railpack Version" keywords="railpack version pin" staged={rv.staged} locked={rv.locked}
          description="Pin one of the three most recent Railpack releases. Empty follows the current one.">
          <PlusField label="Pin Version" value={cfg.build.railpackVersion || ""} placeholder="v0.9.0" mono disabled={!!rv.locked}
            validate={(v) => (/^v?\d+\.\d+\.\d+$/.test(v) ? null : "Use a version such as v0.9.0")}
            onSave={(v) => stage("build.railpackVersion", v)} />
        </SettingField>
      )}

      <SettingField title="Custom Build Command" keywords="build command custom npm run build" staged={bc.staged} locked={bc.locked}
        description={<>Replace the build command the builder would run. <DocsLink href={docs("SPEC.md")} /></>}>
        <PlusField label="Build Command" value={cfg.build.buildCommand || ""} placeholder="npm run build" mono disabled={!!bc.locked}
          onSave={(v) => stage("build.buildCommand", v)} />
      </SettingField>

      <WatchPaths />

      <SettingField title="Build Timeout" keywords="build timeout minutes" staged={to.staged} locked={to.locked}
        description="Stop builds that run longer than this. Empty uses the install's default (30 minutes).">
        <PlusField label="Build Timeout" type="number" value={cfg.build.timeoutMin ? String(cfg.build.timeoutMin) : ""} placeholder="30" suffix="minutes" disabled={!!to.locked}
          validate={(v) => (Number(v) >= 1 && Number(v) <= 240 ? null : "1 to 240 minutes")}
          onSave={(v) => stage("build.timeoutMin", v === "" ? "" : Number(v))} />
      </SettingField>
    </>
  );
}

function WatchPaths() {
  const { cfg, stage } = useSettings();
  const st = useFieldState("build.watchPaths");
  const paths = cfg.build.watchPaths || [];
  const [text, setText] = useState("");
  const [busy, setBusy] = useState(false);
  const add = async () => {
    const p = text.trim();
    if (!p || paths.includes(p)) { setText(""); return; }
    setBusy(true);
    try { await stage("build.watchPaths", [...paths, p]); setText(""); } finally { setBusy(false); }
  };
  return (
    <SettingField title="Watch Paths" keywords="watch paths gitignore monorepo trigger" staged={st.staged} locked={st.locked}
      description={<>Gitignore-style patterns. Only commits that change a matching file trigger a deploy. <DocsLink href={docs("SPEC.md")} /></>}>
      {paths.length > 0 && (
        <div className="mb-2 flex flex-col gap-1.5">
          {paths.map((p) => (
            <div key={p} className="group flex h-10 items-center gap-2 rounded-md bg-surface-hover pl-3 pr-1.5 font-mono text-[15px] text-foreground">
              <span className="min-w-0 flex-1 truncate">{p}</span>
              {!st.locked && (
                <IconButton label={`Remove ${p}`} size={26} className="opacity-60 group-hover:opacity-100" onClick={() => void stage("build.watchPaths", paths.filter((x) => x !== p))}>
                  <X size={14} />
                </IconButton>
              )}
            </div>
          ))}
        </div>
      )}
      <form className="relative" onSubmit={(e) => { e.preventDefault(); void add(); }}>
        <input aria-label="Add watch path" value={text} disabled={busy || !!st.locked} placeholder="Add pattern e.g. /src/**"
          onChange={(e) => setText(e.target.value)} onKeyDown={(e) => { if (e.key === "Escape") { e.stopPropagation(); setText(""); } }}
          className={cx(INPUT, "h-11 pr-11")} />
        <button type="submit" aria-label="Add pattern" disabled={!text.trim()}
          className="absolute right-1.5 top-1/2 inline-flex h-8 w-8 -translate-y-1/2 items-center justify-center rounded-md text-fg-2 transition-colors hover:bg-surface-hover hover:text-foreground disabled:opacity-60">
          <Check size={16} />
        </button>
      </form>
    </SettingField>
  );
}
