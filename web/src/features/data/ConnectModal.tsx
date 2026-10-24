// SPDX-License-Identifier: AGPL-3.0-only

// Database Connect modal: Private Network and Public Network tabs. Private shows
// the three reference steps, Public shows the exposure notice, the tunnel
// commands and, once public access is on, the masked public URL.

import { useState } from "react";
import { AlertTriangle, Eye, EyeOff, Info, Monitor, ShieldAlert, Terminal } from "lucide-react";
import type { ConnectInfo, RevealResponse, Service } from "../../api/types.gen";
import { BrandIcon, Button, Confirm, ErrorLine, Modal, Skeleton, cx, toastError, toastSuccess } from "../../kit";
import { get, post } from "../service/client";
import { useLoad } from "../service/ui";
import { engineOf, ENGINES } from "./engine";
import { CodeBox, CopyIconButton } from "./parts";

type Tab = "private" | "public";

export default function ConnectModal({ base, onClose, onOpenNetworking }: { base: string; onClose: () => void; onOpenNetworking?: () => void }) {
  const info = useLoad(() => get<ConnectInfo>(`${base}/connect-info`), [base]);
  const svc = useLoad(() => get<Service>(base), [base]);
  const [tab, setTab] = useState<Tab>("private");
  const [confirmPublic, setConfirmPublic] = useState(false);
  const [vals, setVals] = useState<Record<string, string>>({});
  const [err, setErr] = useState<string | null>(null);
  const engine = svc.data ? engineOf(svc.data) : null;
  const name = svc.data?.config.name ?? "database";
  const eng = engine ?? ENGINES.postgres;
  const d = info.data;
  const privVar = d?.privateVariable || eng.privateVar;
  const pubVar = d?.publicVariable || eng.publicVar;
  const cli = d?.cliCommand || `5050 connect ${name}`;

  const reveal = async (variable: string) => {
    setErr(null);
    try {
      const r = await post<RevealResponse>(`${base}/variables/reveal`, { names: [variable], resolved: true });
      setVals((m) => ({ ...m, ...r.values }));
    } catch (e) { setErr(e instanceof Error ? e.message : String(e)); }
  };

  const enablePublic = async () => {
    try {
      await post(`${base}/tcp-proxies`, { targetPort: eng.port });
      toastSuccess("Public access added");
      await info.reload();
    } catch (e) { toastError(e); throw e; }
  };

  return (
    <>
      <Modal title={`Connect to ${name}`} bare width={868} onClose={onClose} className="focus-visible:shadow-none">
        <div className="p-6 text-fg-2">
          <div className="flex items-center gap-3 pb-5 pr-8">
            {engine && <BrandIcon name={eng.brand} size={22} />}
            <h2 className="truncate text-h2 text-foreground" style={{ lineHeight: "30px" }}>Connect to {name}</h2>
          </div>
          <div role="tablist" className="flex gap-6 border-b border-line">
            {([["private", "Private Network"], ["public", "Public Network"]] as const).map(([id, label]) => (
              <button key={id} role="tab" aria-selected={tab === id} onClick={() => setTab(id)}
                className={cx("-mb-px h-[38px] border-b-2 text-body transition-colors",
                  tab === id ? "border-foreground font-semibold text-foreground" : "border-transparent text-fg-3 hover:text-foreground")}>
                {label}
              </button>
            ))}
          </div>

          <div className="pt-6">
            {info.error && <ErrorLine error={info.error} className="mb-4" />}
            {tab === "private" && (
              <ol className="relative">
                <Step n={1} last={false}>Create a new variable in the service you want to connect to this database.</Step>
                <Step n={2} last={false}>
                  Assign it the following value:
                  <div className="mt-3">
                    <CodeBox text={`\${{ ${name}.${privVar} }}`} />
                  </div>
                  {d && <Revealed label="Resolved private URL" masked={d.privateUrl} variable={privVar} vals={vals} onReveal={reveal} />}
                </Step>
                <Step n={3} last>Use the variable in your application code.</Step>
              </ol>
            )}

            {tab === "public" && (
              <div className="text-body">
                <p>Private by default, only services inside your project can reach {name}.</p>
                <h3 className="mt-6 text-body font-semibold text-foreground">Public Access</h3>
                <p className="mt-1">Adds a public host and port and sets <code className="rounded-sm bg-surface px-1 font-mono text-accent-text">{pubVar}</code></p>
                <div className="mt-3 divide-y divide-line rounded-md border border-line">
                  <div className="flex h-10 items-center gap-3 px-3 text-danger-text"><AlertTriangle size={16} className="shrink-0" />Anyone with the string can connect, use a strong password and SSL.</div>
                  <div className="flex h-10 items-center gap-3 px-3 text-fg-2"><Info size={16} className="shrink-0 text-fg-3" />Public traffic is billed as egress by your host.</div>
                </div>

                {d?.publicUrl && (
                  <div className="mt-4">
                    <Revealed label="Public URL" masked={d.publicUrl} variable={pubVar} vals={vals} onReveal={reveal} />
                  </div>
                )}

                <h3 className="mt-6 text-body font-semibold text-foreground">Connect without exposing the database</h3>
                <div className="mt-3 divide-y divide-line rounded-md border border-line">
                  <div className="p-3 pb-3">
                    <div className="flex gap-3">
                      <Terminal size={16} className="mt-0.5 shrink-0 text-fg-3" />
                      <div className="min-w-0 flex-1">
                        <div className="font-medium text-foreground">From your machine</div>
                        <p className="text-fg-2">Opens an encrypted tunnel session with no public access needed.</p>
                        <div className="mt-2"><CodeBox text={cli} compact>{cli}</CodeBox></div>
                        <a href="https://github.com/musakkka/5050cloud/tree/main/docs" target="_blank" rel="noreferrer"
                          className="mt-1.5 inline-block text-caption font-medium text-foreground underline underline-offset-2 hover:text-accent-text">Install the 5050 CLI</a>
                      </div>
                    </div>
                  </div>
                  <div className="p-3">
                    <div className="flex gap-3">
                      <Monitor size={16} className="mt-0.5 shrink-0 text-fg-3" />
                      <div className="min-w-0 flex-1">
                        <div className="font-medium text-foreground">External GUI client</div>
                        <p className="text-fg-2">Keeps the tunnel open for tools like TablePlus, DBeaver, or pgAdmin.</p>
                        <div className="mt-2">
                          <CodeBox text={`${cli} --tunnel-only`} copy={`${cli} --tunnel-only`} compact>
                            {cli} <b className="font-semibold">--tunnel-only</b>
                          </CodeBox>
                        </div>
                      </div>
                    </div>
                  </div>
                </div>

                {!d?.publicUrl && (
                  <Button variant="danger-outline" className="mt-5" icon={<ShieldAlert size={16} />} disabled={!d} onClick={() => setConfirmPublic(true)}>Add Public Access</Button>
                )}
                <p className="mt-3 text-caption font-medium">
                  Remove it any time from{" "}
                  <button type="button" className="underline underline-offset-2 hover:text-foreground" onClick={() => { onClose(); onOpenNetworking?.(); }}>Networking settings</button>.
                </p>
              </div>
            )}
            {!d && !info.error && tab === "private" && <Skeleton className="mt-4 h-[52px]" />}
            {err && <ErrorLine error={err} className="mt-4" />}
          </div>
        </div>
      </Modal>
      {confirmPublic && (
        <Confirm title="Add public access" confirm="Add public access" danger
          body={`This opens a public port for ${name}. Anyone with the connection string can reach it, so keep the password strong and require SSL.`}
          onClose={() => setConfirmPublic(false)} onConfirm={enablePublic} />
      )}
    </>
  );
}

function Step({ n, last, children }: { n: number; last: boolean; children: React.ReactNode }) {
  return (
    <li className="relative flex gap-4 pb-6 last:pb-0">
      {!last && <span aria-hidden className="absolute bottom-0 left-4 top-8 -ml-px w-px bg-line" />}
      <span className="relative z-10 flex h-8 w-8 shrink-0 items-center justify-center rounded-full border border-line bg-background text-body text-foreground">{n}</span>
      <div className="min-w-0 flex-1 pt-1 text-lead text-foreground" style={{ lineHeight: "24px" }}>{children}</div>
    </li>
  );
}

function Revealed({ label, masked, variable, vals, onReveal }: { label: string; masked: string; variable: string; vals: Record<string, string>; onReveal: (v: string) => void }) {
  const shown = vals[variable];
  const text = shown ?? masked;
  const [hide, setHide] = useState(false);
  const visible = shown !== undefined && !hide;
  return (
    <div className="mt-3 text-body">
      <div className="mb-1 text-caption font-medium text-fg-3">{label}</div>
      <div className="flex h-[44px] items-center gap-1 rounded-md bg-surface pl-3 pr-1.5">
        <code className="min-w-0 flex-1 truncate bg-transparent font-mono text-caption text-foreground">{visible ? text : masked}</code>
        <button type="button" aria-label={visible ? "Hide value" : "Reveal value"}
          onClick={() => { if (shown === undefined) { onReveal(variable); setHide(false); } else setHide(!hide); }}
          className="inline-flex h-[34px] w-[34px] items-center justify-center rounded-md text-fg-3 hover:bg-surface-hover hover:text-foreground">
          {visible ? <EyeOff size={16} /> : <Eye size={16} />}
        </button>
        <CopyIconButton text={shown ?? masked} label={shown ? "Copy" : "Copy masked value"} />
      </div>
    </div>
  );
}
