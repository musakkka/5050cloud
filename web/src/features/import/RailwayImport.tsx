// SPDX-License-Identifier: AGPL-3.0-only

// /new/import/railway: token, progress, report, then the canvas with the
// staged changes (SPEC 9.22). The token is sent once and never kept here.
import { useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { api, type Import } from "../../api/client";
import { TopBar } from "../../app/Shell";
import { Button, ErrorLine, Field, Input, Spinner } from "../../kit";
import { ImportReport, waitImport } from "../new/flows";

export default function RailwayImport() {
  const nav = useNavigate();
  const [token, setToken] = useState("");
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

  return (
    <div className="flex min-h-full flex-col">
      <TopBar left={<><Link to="/new" className="text-gray-500 hover:text-foreground">New</Link><span className="text-gray-400">/</span><span className="font-medium">Import from Railway</span></>} />
      <main className="mx-auto w-full max-w-[560px] space-y-5 px-4 py-10">
        <h1 className="text-2xl font-semibold">Import from Railway</h1>
        {!imp && (
          <form className="space-y-4" onSubmit={(e) => { e.preventDefault(); void start(); }}>
            <p className="text-sm text-gray-600">Create a project token in Railway (Project Settings, Tokens) and paste it here. Services, variables, domains, volumes and databases arrive as staged changes in a new project. Nothing deploys until you press Deploy. Database data is not copied; the docs show how to move it.</p>
            <Field label="Railway project token"><Input required type="password" autoComplete="off" value={token} onChange={(e) => setToken(e.target.value)} /></Field>
            <Field label="Project name" hint="Optional. Defaults to the Railway project's name."><Input value={name} onChange={(e) => setName(e.target.value)} /></Field>
            <ErrorLine error={error} />
            <Button type="submit" variant="primary" busy={busy} disabled={!token.trim()}>Import</Button>
          </form>
        )}
        {imp && imp.status === "running" && <div className="flex items-center gap-2 text-sm text-gray-600"><Spinner />Reading the Railway project</div>}
        {imp && imp.status !== "running" && (
          <div className="space-y-5">
            <ImportReport imp={imp} />
            <Button variant="primary" onClick={() => void open(imp).catch(setError)}>Review on the canvas</Button>
          </div>
        )}
        {imp && <ErrorLine error={error} />}
      </main>
    </div>
  );
}
