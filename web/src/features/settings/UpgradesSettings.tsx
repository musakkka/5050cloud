// SPDX-License-Identifier: AGPL-3.0-only

import { useEffect } from "react";
import { Button } from "../../ui/kit";
import { v1 } from "./client";
import { Loading, Notice, Section, fmtDate, useAction, useLoad } from "./ui";

const BUSY = ["downloading", "switching", "verifying"];

export default function UpgradesSettings() {
  const inst = useLoad(v1.install, []);
  const st = useLoad(v1.upgradeStatus, []);
  const a = useAction();
  const running = !!st.data && BUSY.includes(st.data.state);
  useEffect(() => {
    if (!running) return;
    const t = setInterval(st.reload, 3000);
    return () => clearInterval(t);
  }, [running, st.reload]);
  if (!inst.data || !st.data) return <Loading error={inst.error || st.error} />;
  const i = inst.data, s = st.data;
  const newer = i.latestVersion && i.latestVersion !== i.version;
  return (
    <Section title="Upgrades" description={`This install runs ${i.version}.`}>
      {newer ? <Notice>Version {i.latestVersion} is available.</Notice> : <Notice kind="ok">You are on the latest version.</Notice>}
      {s.state !== "idle" && (
        <div className="rounded-lg border border-gray-200 p-4 mb-4 text-sm">
          <div className="font-medium">Last upgrade: {s.fromVersion} to {s.toVersion}, {s.state.replace("_", " ")}</div>
          <div className="text-gray-500 text-xs mt-1">{fmtDate(s.at)}{s.message ? `. ${s.message}` : ""}</div>
          {s.agents?.map((ag) => <div key={ag.hostId} className="text-xs mt-1">Host {ag.hostId}: {ag.state} ({ag.version}){ag.message ? `. ${ag.message}` : ""}</div>)}
        </div>
      )}
      <Button variant="primary" disabled={!newer || running || a.busy} onClick={async () => { const r = await a.run(() => v1.upgrade({})); if (r) st.setData(r); }}>
        {running ? "Upgrading" : "Upgrade now"}
      </Button>
      <p className="text-xs text-gray-500 mt-2">The control plane keeps the old version and rolls back on its own if the new one fails its checks.</p>
      {a.error && <div className="mt-3"><Notice kind="error">{a.error}</Notice></div>}
    </Section>
  );
}
