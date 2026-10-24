// SPDX-License-Identifier: AGPL-3.0-only

// Collapsible "added by the platform" group and the Shared Variable explainer panel.

import { useState } from "react";
import { ArrowUpRight, ChevronDown, ChevronRight, Link2, X } from "lucide-react";
import { Button, cx } from "../../kit";
import { MASK } from "./VariableRow";

const DESCRIPTIONS: [RegExp, string][] = [
  [/PUBLIC_DOMAIN$/, "The public domain assigned to the service"],
  [/PRIVATE_DOMAIN$/, "The private network domain of the service"],
  [/PROJECT_NAME$/, "The name of the project"],
  [/ENVIRONMENT_NAME$/, "The name of the environment"],
  [/SERVICE_NAME$/, "The name of the service"],
  [/PROJECT_ID$/, "The ID of the project"],
  [/ENVIRONMENT_ID$/, "The ID of the environment"],
  [/SERVICE_ID$/, "The ID of the service"],
  [/DEPLOYMENT_ID$/, "The ID of the running deployment"],
  [/REPLICA_ID$/, "The ID of this replica"],
  [/VOLUME_ID$/, "The ID of the attached volume"],
  [/VOLUME_NAME$/, "The name of the attached volume"],
  [/VOLUME_MOUNT_PATH$/, "Where the volume is mounted in the container"],
  [/GIT_COMMIT_SHA$/, "The commit this deployment was built from"],
  [/GIT_BRANCH$/, "The branch this deployment was built from"],
  [/^PORT$/, "The port your service should listen on"],
  [/TCP_PROXY/, "Details of the TCP proxy for the service"],
  [/DRAINING_SECONDS$/, "How long the old deployment keeps running during a swap"],
];
const describe = (n: string) => DESCRIPTIONS.find(([re]) => re.test(n))?.[1] ?? "Set by the platform for your service";

/** Footer row "N variables added by 5050cloud", collapsed by default. */
export function ProvidedGroup({ names, docsHref }: { names: string[]; docsHref: string }) {
  const [open, setOpen] = useState(false);
  if (!names.length) return null;
  return (
    <div className="mt-2 border-t border-gray-100 pt-4">
      <button type="button" aria-expanded={open} onClick={() => setOpen((o) => !o)}
        className="flex w-full items-center gap-2 px-4 py-1 text-left text-sm text-fg-2 transition-colors hover:text-foreground">
        {open ? <ChevronDown size={16} /> : <ChevronRight size={16} />}
        {names.length} variable{names.length === 1 ? "" : "s"} added by 5050cloud
      </button>
      {open && (
        <div className="animate-collapsible-down overflow-hidden">
          <p className="px-4 pb-2 pt-2 text-sm text-fg-3">
            These variables are set for you on every deployment and cannot be edited.{" "}
            <a href={docsHref} target="_blank" rel="noreferrer" className="inline-flex items-center gap-0.5 underline underline-offset-2 hover:text-foreground">Learn more<ArrowUpRight size={12} /></a>
          </p>
          {names.map((n) => (
            <div key={n} className="grid min-h-[58px] grid-cols-[minmax(0,2fr)_minmax(0,3fr)_auto] items-center gap-2 rounded px-4 py-2 transition-colors hover:bg-gray-100">
              <div className="min-w-0">
                <div className="truncate font-mono text-sm leading-5 text-foreground" title={n}>{n}</div>
                <div className="truncate text-caption text-fg-3">{describe(n)}</div>
              </div>
              <span className="font-mono text-sm text-fg-2">{MASK}</span>
              <span className="rounded border border-gray-100 px-2 py-0.5 text-xs leading-4 text-gray-300">Reference</span>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

/** Our own drawing: one shared value fanning out to three services. */
function FanOut() {
  return (
    <svg viewBox="0 0 220 150" className="h-[130px] w-[190px] shrink-0" fill="none" aria-hidden>
      <rect x="6" y="56" width="62" height="38" rx="8" className="fill-pink-100 stroke-pink-400" strokeWidth="1.5" />
      <text x="37" y="79" textAnchor="middle" className="fill-pink-700 font-mono" fontSize="11">{"{{ }}"}</text>
      {[16, 56, 96].map((y, i) => (
        <g key={y}>
          <path d={`M68 75 C 110 75, 100 ${y + 18}, 146 ${y + 18}`} className="stroke-pink-300" strokeWidth="1.5" strokeDasharray="4 4">
            <animate attributeName="stroke-dashoffset" from="16" to="0" dur={`${1.6 + i * 0.2}s`} repeatCount="indefinite" />
          </path>
          <rect x="146" y={y} width="64" height="36" rx="8" className="fill-gray-100 stroke-gray-200" strokeWidth="1.5" />
          <rect x="156" y={y + 11} width="14" height="14" rx="4" className="fill-pink-500" opacity={0.9 - i * 0.2} />
          <rect x="176" y={y + 15} width="26" height="6" rx="3" className="fill-gray-300" />
        </g>
      ))}
    </svg>
  );
}

export function SharedPanel({ open, onClose, onConfigure, docsHref }: { open: boolean; onClose: () => void; onConfigure: () => void; docsHref: string }) {
  return (
    <div className={cx("grid transition-all duration-300 ease-out", open ? "grid-rows-[1fr] opacity-100" : "pointer-events-none grid-rows-[0fr] opacity-0")} aria-hidden={!open}>
      <div className="overflow-hidden">
        <div className="relative my-4 flex min-h-[210px] flex-col items-center gap-6 rounded-md bg-gray-50 px-6 py-6 sm:flex-row sm:px-12">
          <FanOut />
          <div className="min-w-0 flex-1">
            <h3 className="text-base font-semibold leading-6 text-foreground">Keep variables in sync across services</h3>
            <p className="mt-2 text-sm leading-5 text-fg-2">
              Shared variables live once in your environment and any service can reference them as <code className="rounded bg-gray-100 px-1 font-mono text-caption">{"${{shared.NAME}}"}</code>.
              Manage them in Project Settings, or promote a service variable from its row menu.
            </p>
            <div className="mt-4 flex flex-wrap gap-2">
              <Button variant="primary" size="md" tabIndex={open ? 0 : -1} icon={<Link2 size={16} />} onClick={onConfigure}>Configure Shared Variables</Button>
              <a href={docsHref} target="_blank" rel="noreferrer" tabIndex={open ? 0 : -1}
                className="inline-flex h-[34px] items-center gap-2 rounded-md border border-gray-200 px-3 text-sm text-foreground transition-colors hover:bg-gray-100">View Docs</a>
            </div>
          </div>
          <button type="button" title="Close" aria-label="Close" tabIndex={open ? 0 : -1} onClick={onClose}
            className="absolute right-3 top-3 flex h-[34px] w-[34px] items-center justify-center rounded-md text-fg-3 transition-colors hover:bg-gray-100 hover:text-foreground"><X size={16} /></button>
        </div>
      </div>
    </div>
  );
}

