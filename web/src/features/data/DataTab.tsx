// SPDX-License-Identifier: AGPL-3.0-only

// Data tab for databases (docs/SPEC.md 13.3, 9.13): tables and collections,
// Postgres extensions, the Redis key browser, and Regenerate credentials.
// There is no query box in v0.1.

import { useEffect, useState } from "react";
import type { DataResponse, Extension, RedisKeysPage, RedisRevealResponse, Service } from "../../api/types.gen";
import { get, post, qs, svcPath } from "../service/client";
import { Button, Confirm, Empty, ErrorLine, Input, Loading, NotInV01, Section, fmtBytes, useAction, useLoad } from "../service/ui";

export default function DataTab({ eid, service }: { eid: string; service: Service }) {
  const base = svcPath(eid, service.id);
  const tpl = service.config.template;
  const data = useLoad(() => (tpl === "redis" ? Promise.resolve(null) : get<DataResponse>(`${base}/data`)), [base, tpl]);
  const [regen, setRegen] = useState(false);
  const [regenDone, setRegenDone] = useState(false);

  return (
    <div>
      {data.error && <ErrorLine>{data.error}</ErrorLine>}
      {tpl !== "redis" && (
        !data.data ? (!data.error && <Loading />) : (
          <Section title={tpl === "mongodb" ? "Collections" : "Tables"}>
            <div className="mb-3 text-xs text-gray-500">Version {data.data.version} · {fmtBytes(data.data.sizeBytes)} · {data.data.connections} connections</div>
            {data.data.tables.length === 0 ? <Empty>Nothing here yet.</Empty> : (
              <div className="overflow-x-auto rounded-lg border border-gray-200">
                <table className="w-full text-sm">
                  <thead className="text-left text-xs text-gray-500"><tr><th className="p-2">Name</th><th className="p-2">Rows (estimate)</th><th className="p-2">Size</th></tr></thead>
                  <tbody>
                    {data.data.tables.map((t) => (
                      <tr key={`${t.schema}.${t.name}`} className="border-t border-gray-200">
                        <td className="p-2 font-mono">{t.schema && t.schema !== "public" ? `${t.schema}.` : ""}{t.name}</td>
                        <td className="p-2">{t.rows.toLocaleString()}</td>
                        <td className="p-2">{fmtBytes(t.bytes)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </Section>
        )
      )}
      {tpl === "postgres" && <Extensions base={base} />}
      {tpl === "redis" && <RedisKeys base={base} />}
      <NotInV01 label="SQL query box" instead="connect with 5050 connect and your own client" />
      <Section title="Credentials">
        <div className="flex items-center justify-between gap-3 text-sm">
          <span className="text-gray-600">Make a new password. Services that reference this database redeploy with it.</span>
          <Button variant="danger" onClick={() => setRegen(true)}>Regenerate credentials</Button>
        </div>
        {regenDone && <div className="mt-2 text-sm text-green-700">New credentials are set and dependent services are redeploying.</div>}
      </Section>
      {regen && <Confirm title="Regenerate credentials" danger confirm="Regenerate" typeName={service.config.name}
        body="The old password stops working. Services that use this database through references redeploy with the new one."
        onClose={() => setRegen(false)} onConfirm={async () => { await post(`${base}/regenerate-credentials`); setRegenDone(true); }} />}
    </div>
  );
}

function Extensions({ base }: { base: string }) {
  const ext = useLoad(() => get<Extension[]>(`${base}/extensions`), [base]);
  const [filter, setFilter] = useState("");
  const a = useAction();
  return (
    <Section title="Extensions">
      <Input aria-label="Filter extensions" placeholder="Filter extensions" value={filter} onChange={(e) => setFilter(e.target.value)} className="mb-2" />
      <ErrorLine>{a.error || ext.error}</ErrorLine>
      {!ext.data ? (!ext.error && <Loading />) : (
        <div className="max-h-80 divide-y divide-gray-200 overflow-y-auto rounded-lg border border-gray-200">
          {ext.data.filter((x) => x.name.includes(filter.toLowerCase())).map((x) => (
            <div key={x.name} className="flex items-center gap-3 px-3 py-1.5 text-sm">
              <span className="w-40 font-mono">{x.name}</span>
              <span className="flex-1 truncate text-xs text-gray-500" title={x.comment}>{x.version} {x.comment}</span>
              {x.installed ? <span className="text-xs text-green-700">Enabled</span> : (
                <Button variant="ghost" disabled={a.busy} onClick={() => void a.run(async () => {
                  const r = await post<Extension>(`${base}/extensions`, { name: x.name });
                  ext.setData((cur) => cur?.map((e) => (e.name === r.name ? r : e)));
                })}>Enable</Button>
              )}
            </div>
          ))}
        </div>
      )}
    </Section>
  );
}

function RedisKeys({ base }: { base: string }) {
  const [match, setMatch] = useState("*");
  const [page, setPage] = useState<RedisKeysPage | null>(null);
  const [values, setValues] = useState<Record<string, RedisRevealResponse>>({});
  const a = useAction();
  const load = (cursor?: string) => a.run(async () => {
    const r = await get<RedisKeysPage>(`${base}/redis/keys${qs({ match, cursor })}`);
    setPage((cur) => (cursor && cur ? { keys: [...cur.keys, ...r.keys], cursor: r.cursor } : r));
  });
  // eslint-disable-next-line react-hooks/exhaustive-deps
  useEffect(() => { void load(); }, [base]);
  return (
    <Section title="Keys">
      <form className="mb-2 flex gap-2" onSubmit={(e) => { e.preventDefault(); setPage(null); void load(); }}>
        <Input aria-label="Key pattern" value={match} onChange={(e) => setMatch(e.target.value)} />
        <Button type="submit">Search</Button>
      </form>
      <ErrorLine>{a.error}</ErrorLine>
      {!page ? <Loading /> : page.keys.length === 0 ? <Empty>No keys match.</Empty> : (
        <div className="divide-y divide-gray-200 rounded-lg border border-gray-200">
          {page.keys.map((k) => (
            <div key={k.key} className="px-3 py-1.5 text-sm">
              <div className="flex items-center gap-3">
                <span className="flex-1 break-all font-mono">{k.key}</span>
                <span className="text-xs text-gray-500">{k.type}</span>
                <span className="text-xs text-gray-500">{fmtBytes(k.sizeBytes)}</span>
                <span className="text-xs text-gray-500">{k.ttlMs > 0 ? `TTL ${Math.round(k.ttlMs / 1000)}s` : "no TTL"}</span>
                <Button variant="ghost" onClick={() => void a.run(async () => {
                  const v = await post<RedisRevealResponse>(`${base}/redis/keys/reveal`, { key: k.key });
                  setValues((m) => ({ ...m, [k.key]: v }));
                })}>Reveal</Button>
              </div>
              {values[k.key] && <pre className="mt-1 whitespace-pre-wrap break-all rounded bg-gray-100 p-2 text-xs">{values[k.key].value}{values[k.key].truncated ? "\n(first 1 KB shown)" : ""}</pre>}
            </div>
          ))}
        </div>
      )}
      {page?.cursor && <Button className="mt-2" onClick={() => void load(page.cursor)}>Load more</Button>}
    </Section>
  );
}
