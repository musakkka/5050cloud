// SPDX-License-Identifier: AGPL-3.0-only

import { useNavigate } from "react-router-dom";
import { Spinner } from "../ui/kit";
import LogView from "../ui/LogView";
import { useProject } from "./ProjectLayout";

export default function LogsPage() {
  const { canvas, pid, withEnv } = useProject();
  const nav = useNavigate();
  if (!canvas) return <div className="h-full flex items-center justify-center"><Spinner /></div>;
  const targets = canvas.nodes.filter((n) => n.status !== "removed").map((n) => n.id);
  return (
    <LogView targets={targets} showService defaultRange={3600} placeholder='Filter and search logs: text, "exact phrase", -exclude, @level:error'
      onService={(l) => l.target && nav(withEnv(`/project/${pid}/service/${encodeURIComponent(l.target)}`))} />
  );
}
