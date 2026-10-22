// SPDX-License-Identifier: AGPL-3.0-only

// /new: start a project from a repo, database, template, image, nothing,
// a Railway project or a compose file (SPEC 13.1).
import { useMemo, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";
import { Box, ChevronRight, Database, FileCode, FolderPlus, Github, LayoutTemplate, TrainFront } from "lucide-react";
import { api, type Project } from "../../api/client";
import { TopBar } from "../../app/Shell";
import { ErrorLine, Input, Spinner } from "../../kit";
import { ComposeFlow, DatabaseFlow, GitHubFlow, ImageFlow, type Target } from "./flows";

export const NEW_OPTIONS = [
  { id: "github", label: "GitHub Repository", icon: Github },
  { id: "database", label: "Database", icon: Database },
  { id: "template", label: "Template", icon: LayoutTemplate },
  { id: "image", label: "Docker Image", icon: Box },
  { id: "empty", label: "Empty Project", icon: FolderPlus },
  { id: "railway", label: "Import from Railway", icon: TrainFront },
  { id: "compose", label: "Docker Compose", icon: FileCode },
] as const;
type OptionId = (typeof NEW_OPTIONS)[number]["id"];

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

export default function NewPage() {
  const nav = useNavigate();
  const [q, setQ] = useState("");
  const [open, setOpen] = useState<OptionId>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>();
  const target = useNewProjectTarget();
  const list = NEW_OPTIONS.filter((o) => o.label.toLowerCase().includes(q.trim().toLowerCase()));

  const choose = async (id: OptionId) => {
    if (id === "template") return nav("/new/template");
    if (id === "railway") return nav("/new/import/railway");
    if (id === "empty") {
      setBusy(true); setError(undefined);
      try { const t = await target.ensure(); target.done({ ...t, services: [] }); } catch (e) { setError(e); } finally { setBusy(false); }
      return;
    }
    setOpen(id);
  };

  return (
    <div className="flex min-h-full flex-col">
      <TopBar left={<span className="font-medium">New project</span>} />
      <main className="mx-auto w-full max-w-[560px] space-y-4 px-4 py-10">
        <h1 className="text-2xl font-semibold">What would you like to create?</h1>
        <Input aria-label="Search" autoFocus placeholder="Search" value={q} onChange={(e) => setQ(e.target.value)}
          onKeyDown={(e) => { if (e.key === "Enter" && list[0]) void choose(list[0].id); }} />
        <ul className="divide-y divide-gray-200 overflow-hidden rounded-xl border border-gray-200 bg-secondaryBg">
          {list.map((o) => (
            <li key={o.id}>
              <button type="button" onClick={() => void choose(o.id)} className="flex w-full items-center gap-3 px-4 py-3.5 text-left hover:bg-gray-100">
                <o.icon size={18} className="text-gray-600" /><span className="flex-1 text-sm font-medium">{o.label}</span>
                {busy && o.id === "empty" ? <Spinner /> : <ChevronRight size={16} className="text-gray-400" />}
              </button>
            </li>
          ))}
          {list.length === 0 && <li className="px-4 py-6 text-center text-sm text-gray-500">Nothing matches.</li>}
        </ul>
        <ErrorLine error={error} />
      </main>
      {open === "github" && <GitHubFlow target={target} onClose={() => setOpen(undefined)} />}
      {open === "image" && <ImageFlow target={target} onClose={() => setOpen(undefined)} />}
      {open === "database" && <DatabaseFlow target={target} onClose={() => setOpen(undefined)} />}
      {open === "compose" && <ComposeFlow target={target} onClose={() => setOpen(undefined)} />}
    </div>
  );
}
