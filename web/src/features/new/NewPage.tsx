// SPDX-License-Identifier: AGPL-3.0-only

// /new: the create picker floating over a dotted canvas. Steps live in the
// URL (/new/github, /new/database, /new/templates, /new/image) and slide in;
// Escape or the back arrow returns to the root list.
import { useCallback, useMemo, useRef, useState } from "react";
import { useLocation, useNavigate } from "react-router-dom";
import { ChevronDown, FolderPlus, LayoutGrid, LayoutTemplate } from "lucide-react";
import { api, type Project } from "../../api/client";
import { TopBar, useShellOptions } from "../../app/Shell";
import { canEdit, useMe } from "../../app/session";
import { Avatar, Menu } from "../../kit";
import { Backdrop } from "./Backdrop";
import { ComposeFlow, ImageFlow, ScanDialog, type Target } from "./flows";
import { PaletteFrame, type StepDir } from "./Palette";
import { DatabaseStep, GitHubStep, ImageStep, RootStep, TemplateStep, type PickerModal, type StepId, type StepProps } from "./steps";

/** The create options, in picker order (also used by the command palette). */
export const NEW_OPTIONS = [
  { id: "github", label: "GitHub Repository" },
  { id: "database", label: "Database" },
  { id: "template", label: "Template" },
  { id: "image", label: "Docker Image" },
  { id: "empty", label: "Empty Project" },
  { id: "import", label: "Import a project" },
  { id: "compose", label: "Docker Compose" },
] as const;

const prodOf = (p: Project) => (p.environments.find((e) => e.name === "production") || p.environments[0]).id;

/** A Target that creates the project on first use and opens its canvas when done. */
export function useNewProjectTarget(): Target {
  const nav = useNavigate();
  const made = useRef<Promise<{ pid: string; eid: string }>>();
  return useMemo<Target>(() => ({
    ensure: () => (made.current ||= api.createProject({}).then((p) => ({ pid: p.id, eid: prodOf(p) })).catch((e) => { made.current = undefined; throw e; })),
    done: ({ pid, eid, services, variables }) => {
      const q = `?environment=${encodeURIComponent(eid)}`;
      if (variables && services[0]) nav(`/project/${pid}/service/${services[0].id}/variables${q}`);
      else nav(`/project/${pid}${q}`);
    },
  }), [nav]);
}

/** URL segment per step. The template step uses /new/templates; /new/template is the gallery. */
const PATH: Record<Exclude<StepId, "root">, string> = { github: "github", database: "database", template: "templates", image: "image" };
const CRUMB: Record<Exclude<StepId, "root">, string> = { github: "GitHub", database: "Database", template: "Template", image: "Docker Image" };

function stepFromPath(path: string): StepId {
  const seg = path.replace(/^\/new\/?/, "").split("/")[0];
  const hit = (Object.keys(PATH) as (keyof typeof PATH)[]).find((k) => PATH[k] === seg);
  return hit || "root";
}

export default function NewPage() {
  useShellOptions({ bleed: true });
  const me = useMe();
  const nav = useNavigate();
  const { pathname } = useLocation();
  const step = stepFromPath(pathname);
  const [dir, setDir] = useState<StepDir>();
  const [seed, setSeed] = useState<{ step: StepId; text: string }>();
  const [modal, setModal] = useState<PickerModal>();
  const target = useNewProjectTarget();

  const go = useCallback((s: StepId, text?: string) => {
    setDir("push");
    setSeed(text ? { step: s, text } : undefined);
    nav(s === "root" ? "/new" : `/new/${PATH[s]}`);
  }, [nav]);
  const back = useCallback(() => { setDir("pop"); setSeed(undefined); nav("/new"); }, [nav]);

  const props: StepProps = { dir, go, back, target, open: setModal, canCreate: canEdit(me), seed: seed?.step === step ? seed.text : undefined };
  const Step = { root: RootStep, github: GitHubStep, database: DatabaseStep, template: TemplateStep, image: ImageStep }[step];

  return (
    <div className="absolute inset-0 overflow-hidden">
      <TopBar left={
        <>
          <Menu align="left" minWidth={220} trigger={
            <button type="button" className="flex h-[30px] items-center gap-2 rounded-md px-2 text-sm font-medium text-foreground transition-colors hover:bg-tint-active">
              <Avatar name={me.install.name} size={16} />New project<ChevronDown size={16} className="text-fg-5" />
            </button>
          } items={[
            { label: "All projects", icon: <LayoutGrid size={16} />, onClick: () => nav("/") },
            { label: "Template gallery", icon: <LayoutTemplate size={16} />, onClick: () => nav("/new/template") },
            "separator",
            { label: "Start over", icon: <FolderPlus size={16} />, onClick: () => { setDir("pop"); nav("/new"); } },
          ]} />
          {step !== "root" && (
            <span key={step} className="flex items-center gap-2 animate-fade-in">
              <span className="select-none text-fg-5">/</span>
              <span className="px-2 font-medium text-foreground">{CRUMB[step]}</span>
            </span>
          )}
        </>
      } />
      <Backdrop />
      <div className="pointer-events-none absolute inset-x-0 top-[max(24px,calc(50%-213px))] flex justify-center px-4">
        <PaletteFrame>
          <Step key={step} {...props} />
        </PaletteFrame>
      </div>
      {modal?.kind === "scan" && <ScanDialog target={target} repo={modal.repo} defaultBranch={modal.defaultBranch} onClose={() => setModal(undefined)} />}
      {modal?.kind === "image" && <ImageFlow target={target} initial={modal.image} onClose={() => setModal(undefined)} />}
      {modal?.kind === "compose" && <ComposeFlow target={target} onClose={() => setModal(undefined)} />}
    </div>
  );
}
