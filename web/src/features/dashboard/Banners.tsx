// SPDX-License-Identifier: AGPL-3.0-only

// Install notices shown in the shell's full-width banner row while the dashboard is open:
// read-only mode (disk full), a pending restore review, unhealthy hosts and a new release.
// Dismissing a notice hides it for this browser session only.
import { useState } from "react";
import type { Host, Install } from "../../api/client";
import { ShellBanner } from "../../app/Shell";
import { Banner, RelTime, plural } from "../../kit";

const DISMISS_KEY = "5050.dash.dismissed";

function readDismissed(): string[] {
  try { return JSON.parse(sessionStorage.getItem(DISMISS_KEY) || "[]") as string[]; } catch { return []; }
}

/** The first problem across hosts: offline or lost hosts first, then disks at 90% or more. */
export function hostIssue(hosts?: Host[]): { id: string; text: string } | null {
  if (!hosts?.length) return null;
  const down = hosts.filter((h) => h.state === "offline" || h.state === "lost");
  if (down.length) {
    const first = down[0]!;
    return { id: `down:${down.map((h) => h.id).join(",")}`, text: down.length === 1 ? `Host ${first.name} is ${first.state}.` : `${down.length} hosts are offline (${down.map((h) => h.name).join(", ")}).` };
  }
  for (const h of hosts) {
    for (const d of h.disks) {
      const pct = d.total ? Math.round((d.used / d.total) * 100) : 0;
      if (pct >= 90) return { id: `disk:${h.id}:${d.mount}:${pct}`, text: `Disk ${d.mount} on ${h.name} is ${pct}% full.` };
    }
  }
  return null;
}

export function DashboardBanners({ install, hosts, admin }: { install: Install; hosts?: Host[]; admin: boolean }) {
  const [dismissed, setDismissed] = useState<string[]>(readDismissed);
  const dismiss = (id: string) => {
    const n = [...dismissed, id];
    setDismissed(n);
    try { sessionStorage.setItem(DISMISS_KEY, JSON.stringify(n)); } catch { /* private window */ }
  };
  const update = install.latestVersion && install.latestVersion !== install.version ? `update:${install.latestVersion}` : null;
  const issue = hostIssue(hosts);
  const r = install.restore;
  return (
    <ShellBanner>
      {install.readOnly && (
        <Banner tone="danger" link={admin ? { label: "Review hosts", to: "/settings/hosts" } : undefined}>
          The control host disk is full. This install is read-only until space is freed.
        </Banner>
      )}
      {r && !dismissed.includes("restore") && (
        <Banner tone="warn" link={admin ? { label: "Review", to: "/settings/hosts" } : undefined} onDismiss={() => dismiss("restore")}>
          Restored from a snapshot taken <RelTime ts={r.snapshotAt} />. {plural(r.unknownReplicas, "unknown replica")} and {plural(r.unknownHosts, "unknown host")} wait for review.
        </Banner>
      )}
      {issue && !install.readOnly && !dismissed.includes(issue.id) && (
        <Banner tone="warn" link={admin ? { label: "View hosts", to: "/settings/hosts" } : undefined} onDismiss={() => dismiss(issue.id)}>
          {issue.text}
        </Banner>
      )}
      {update && !dismissed.includes(update) && (
        <Banner tone="info" link={admin ? { label: "View upgrade", to: "/settings/upgrades" } : undefined} onDismiss={() => dismiss(update)}>
          Version {install.latestVersion} is available. You run {install.version}.
        </Banner>
      )}
    </ShellBanner>
  );
}
