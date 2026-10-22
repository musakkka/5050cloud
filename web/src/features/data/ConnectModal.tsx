// SPDX-License-Identifier: AGPL-3.0-only

// Database Connect modal (docs/SPEC.md 9.13): private and public URLs
// (masked until revealed) and the CLI command.

import { useState } from "react";
import type { ConnectInfo, RevealResponse } from "../../api/types.gen";
import { get, post } from "../service/client";
import { Button, CopyButton, ErrorLine, Loading, Modal, useAction, useLoad } from "../service/ui";

export default function ConnectModal({ base, onClose }: { base: string; onClose: () => void }) {
  const info = useLoad(() => get<ConnectInfo>(`${base}/connect-info`), [base]);
  const [vals, setVals] = useState<Record<string, string>>({});
  const a = useAction();
  const reveal = (name: string) => a.run(async () => {
    const r = await post<RevealResponse>(`${base}/variables/reveal`, { names: [name], resolved: true });
    setVals((m) => ({ ...m, ...r.values }));
  });
  const row = (label: string, masked: string, variable?: string) => {
    const shown = variable && vals[variable] !== undefined ? vals[variable] : masked;
    return (
      <div className="mb-4">
        <div className="mb-1 text-sm font-medium">{label}</div>
        <div className="flex items-center gap-2 rounded-md border border-gray-200 bg-background px-3 py-2 font-mono text-xs">
          <span className="flex-1 break-all">{shown}</span>
          {variable && vals[variable] === undefined && <Button variant="ghost" onClick={() => void reveal(variable)}>Reveal</Button>}
          <CopyButton text={shown} />
        </div>
        {variable && <div className="mt-1 text-xs text-gray-500">In your services: <span className="font-mono">{"${{"}…{"."}{variable}{"}}"}</span></div>}
      </div>
    );
  };
  return (
    <Modal title="Connect" width={640} onClose={onClose} footer={<Button onClick={onClose}>Close</Button>}>
      {!info.data ? (info.error ? <ErrorLine>{info.error}</ErrorLine> : <Loading />) : <>
        {row("Private URL (from other services)", info.data.privateUrl, info.data.privateVariable)}
        {info.data.publicUrl ? row("Public URL", info.data.publicUrl, info.data.publicVariable) :
          <p className="mb-4 text-xs text-gray-500">Public access is off. Add a TCP Proxy in Settings, Networking to connect from outside.</p>}
        <div className="mb-1 text-sm font-medium">From your machine</div>
        <div className="flex items-center gap-2 rounded-md border border-gray-200 bg-background px-3 py-2 font-mono text-xs">
          <span className="flex-1">{info.data.cliCommand}</span><CopyButton text={info.data.cliCommand} />
        </div>
        <p className="mt-1 text-xs text-gray-500">Opens {info.data.client} through a tunnel; nothing is exposed publicly.</p>
      </>}
      <ErrorLine>{a.error}</ErrorLine>
    </Modal>
  );
}
