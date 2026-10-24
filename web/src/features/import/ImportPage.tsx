// SPDX-License-Identifier: AGPL-3.0-only

// /new/import: token, progress, report, then the canvas with the
// staged changes (SPEC 9.22). The token is sent once and never kept here.
import { useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { ArrowLeft, ArrowRight, CircleCheck, Eye, EyeOff, KeyRound, ShieldCheck, TrainFront } from "lucide-react";
import { api, type Import } from "../../api/client";
import { TopBar, useShellOptions } from "../../app/Shell";
import { Button, ErrorLine, Field, Input, Spinner, cx } from "../../kit";
import { Backdrop } from "../new/Backdrop";
import { ImportReport, waitImport } from "../new/flows";

const STEPS = ["Token", "Import", "Review"];

export default function ImportPage() {
  useShellOptions({ bleed: true });
  const nav = useNavigate();
  const [token, setToken] = useState("");
  const [show, setShow] = useState(false);
  const [name, setName] = useState("");
  const [imp, setImp] = useState<Import>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>();

  const start = async () => {
    setBusy(true); setError(undefined);
    try {
      const first = await api.importRailway(token.trim(), name.trim() || undefined);
      setToken("");
      setImp(first);
      setImp(await waitImport(first, setImp));
    } catch (e) { setError(e); } finally { setBusy(false); }
  };
  const open = async (i: Import) => {
    let eid = i.environmentId;
    if (!eid) { const p = await api.project(i.projectId); eid = (p.environments.find((e) => e.name === "production") || p.environments[0])?.id; }
    nav(`/project/${i.projectId}${eid ? `?environment=${encodeURIComponent(eid)}` : ""}`);
  };
  const stage = !imp ? 0 : imp.status === "running" ? 1 : 2;

  return (
    <div className="absolute inset-0 overflow-y-auto scroll-thin">
      <TopBar left={<><Link to="/new" className="px-2 text-fg-2 transition-colors hover:text-foreground">New project</Link><span className="text-fg-5">/</span><span className="px-2 font-medium text-foreground">Import</span></>} />
      <Backdrop />
      <div className="relative flex min-h-full justify-center px-4 pb-16 pt-[max(24px,calc(50vh-300px))]">
        <div className="h-fit w-full max-w-[480px] overflow-hidden rounded-xl border border-line-faint bg-surface-hover shadow-modal animate-rise-in">
          <div className="bg-tint-hover">
            <div className="flex items-center gap-4 px-6 pb-5 pt-6">
              <span className="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg border border-line-popover bg-surface-raised text-foreground"><TrainFront size={20} /></span>
              <div className="min-w-0">
                <h1 className="text-h3 text-foreground">Import a project</h1>
                <p className="text-sm text-fg-3">Bring a project from your current host over as staged changes.</p>
              </div>
            </div>
            <ol className="flex items-center gap-2 px-6 pb-5" aria-label="Progress">
              {STEPS.map((s, i) => (
                <li key={s} className="flex flex-1 items-center gap-2">
                  <span className={cx("flex h-5 w-5 shrink-0 items-center justify-center rounded-full text-micro font-semibold transition-colors duration-200",
                    i < stage ? "bg-ok text-white" : i === stage ? "bg-accent text-accent-fg" : "bg-tint-pill text-fg-3")}>
                    {i < stage ? <CircleCheck size={12} /> : i + 1}
                  </span>
                  <span className={cx("text-caption", i <= stage ? "text-foreground" : "text-fg-4")}>{s}</span>
                  {i < STEPS.length - 1 && <span className={cx("h-px flex-1 transition-colors duration-300", i < stage ? "bg-ok" : "bg-line")} />}
                </li>
              ))}
            </ol>

            <div className="border-t border-line-faint px-6 pb-6 pt-5">
              {!imp && (
                <form className="space-y-5 animate-palette-push" onSubmit={(e) => { e.preventDefault(); void start(); }}>
                  <p className="text-sm text-fg-2">
                    Create a project token at your current host (in its project settings, under Tokens) and paste it here. Services, variables, domains, volumes and databases arrive as staged changes in a new project. Nothing deploys until you press Deploy.
                  </p>
                  <Field label="Project token">
                    <Input required autoFocus mono type={show ? "text" : "password"} autoComplete="off" value={token} onChange={(e) => setToken(e.target.value)}
                      icon={<KeyRound size={16} />} placeholder="Paste the token"
                      suffix={
                        <button type="button" aria-label={show ? "Hide token" : "Show token"} onClick={() => setShow((x) => !x)}
                          className="flex h-7 w-7 items-center justify-center rounded text-fg-4 transition-colors hover:text-foreground">
                          {show ? <EyeOff size={14} /> : <Eye size={14} />}
                        </button>
                      } />
                  </Field>
                  <Field label="Project name" optional hint="Defaults to the source project's name.">
                    <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="my-app" />
                  </Field>
                  <div className="flex items-start gap-2 rounded-lg bg-surface px-3 py-2.5 text-caption text-fg-3">
                    <ShieldCheck size={14} className="mt-0.5 shrink-0 text-ok-text" />
                    The token is used once for this import and never stored. Database data is not copied; the docs show how to move it.
                  </div>
                  <ErrorLine error={error} />
                  <div className="flex items-center justify-between">
                    <Button variant="ghost" icon={<ArrowLeft size={16} />} onClick={() => nav("/new")}>Back</Button>
                    <Button type="submit" variant="primary" busy={busy} disabled={!token.trim()} iconRight={<ArrowRight size={16} />}>Import</Button>
                  </div>
                </form>
              )}
              {imp && imp.status === "running" && (
                <div className="space-y-4 py-2 animate-palette-push" aria-live="polite">
                  <div className="flex items-center gap-3 text-sm text-foreground"><Spinner size={16} />Reading the project</div>
                  <div className="h-1 overflow-hidden rounded-full bg-tint-active"><div className="shimmer h-full w-full" /></div>
                  <p className="text-caption text-fg-3">Services, variables, domains and volumes are being staged. This usually takes under a minute.</p>
                </div>
              )}
              {imp && imp.status !== "running" && (
                <div className="space-y-5 animate-palette-push">
                  <div className="max-h-[50vh] overflow-y-auto scroll-thin pr-1"><ImportReport imp={imp} /></div>
                  <ErrorLine error={error} />
                  <Button variant="primary" block size="lg" iconRight={<ArrowRight size={16} />} onClick={() => void open(imp).catch(setError)}>Review on the canvas</Button>
                </div>
              )}
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
