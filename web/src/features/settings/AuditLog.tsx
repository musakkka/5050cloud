// SPDX-License-Identifier: AGPL-3.0-only

import { useEffect, useState } from "react";
import type * as T from "../../api/types.gen";
import { Button, Empty } from "../../ui/kit";
import { v1 } from "./client";
import { Loading, Section, fmtDate } from "./ui";

export default function AuditLog() {
  const [items, setItems] = useState<T.AuditEntry[] | null>(null);
  const [next, setNext] = useState<string | undefined>();
  const [error, setError] = useState<string | null>(null);
  const load = (before?: string) => v1.audit(before).then((p) => { setItems((cur) => [...(before ? cur || [] : []), ...p.items]); setNext(p.next || undefined); }).catch((e) => setError(e.message));
  useEffect(() => { void load(); }, []);
  if (!items) return <Loading error={error} />;
  return (
    <Section title="Audit log" description="Every change, reveal, login and shell session. Values are never recorded.">
      {items.length === 0 ? <Empty>Nothing recorded yet.</Empty> : (
        <div className="rounded-xl border border-gray-200 overflow-x-auto">
          <table className="w-full text-sm">
            <thead className="text-left text-xs text-gray-500"><tr><th className="px-4 py-2">Time</th><th className="px-4 py-2">Actor</th><th className="px-4 py-2">Action</th><th className="px-4 py-2">Target</th><th className="px-4 py-2">IP</th></tr></thead>
            <tbody>
              {items.map((a) => (
                <tr key={a.id} className="border-t border-gray-100 align-top">
                  <td className="px-4 py-2 whitespace-nowrap">{fmtDate(a.ts)}</td>
                  <td className="px-4 py-2">{a.actor}</td>
                  <td className="px-4 py-2 font-mono text-xs">{a.action}{a.detail && <div className="text-gray-500 font-sans">{a.detail}</div>}</td>
                  <td className="px-4 py-2 font-mono text-xs break-all">{a.target}</td>
                  <td className="px-4 py-2 text-xs">{a.ip}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {next && <Button className="mt-4" onClick={() => load(next)}>Load older</Button>}
    </Section>
  );
}
