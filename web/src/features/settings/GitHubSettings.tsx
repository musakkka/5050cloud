// SPDX-License-Identifier: AGPL-3.0-only

import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { Button } from "../../ui/kit";
import { v1 } from "./client";
import { continueAfterCreate, goInstall, takeReturn } from "./githubFlow";
import { Field, Input, Loading, Notice, Section, useAction, useLoad } from "./ui";

/** GitHub's manifest flow wants a form POST of the manifest to its URL. */
export function postManifest(formUrl: string, manifest: unknown) {
  const form = document.createElement("form");
  form.method = "post";
  form.action = formUrl;
  const input = document.createElement("input");
  input.type = "hidden";
  input.name = "manifest";
  input.value = JSON.stringify(manifest);
  form.appendChild(input);
  document.body.appendChild(form);
  form.submit();
}

export default function GitHubSettings() {
  const nav = useNavigate();
  const [forwarding] = useState(() => continueAfterCreate());
  const { data, error } = useLoad(v1.github, []);
  const [org, setOrg] = useState("");
  const a = useAction();
  useEffect(() => {
    const back = takeReturn();
    if (back) nav(back, { replace: true });
  }, [nav]);
  if (forwarding) return <Loading />;
  if (!data) return <Loading error={error} />;
  const create = () => a.run(async () => {
    const r = await v1.githubApp({ owner: org.trim() || undefined, returnTo: "/settings/github" });
    postManifest(r.formUrl, r.manifest);
  });
  return (
    <Section title="GitHub" description="A GitHub App lets this install read your repositories and deploy on push.">
      {data.appConfigured ? (
        <>
          {data.installations.length ? (
            <>
              <Notice kind="ok">GitHub App {data.appSlug ? <b>{data.appSlug}</b> : null} is connected.</Notice>
              <div className="text-sm mb-4">Installed on: {data.installations.join(", ")}</div>
            </>
          ) : (
            <>
              <Notice kind="info">GitHub App {data.appSlug ? <b>{data.appSlug}</b> : null} is created but not installed yet. Install it on your account or organization so this install can see your repositories.</Notice>
              <div className="mb-4" />
            </>
          )}
          <div className="flex gap-2">
            {data.installUrl && (data.installations.length
              ? <Button onClick={() => goInstall(data.installUrl!, "/settings/github")}>Install on another account</Button>
              : <Button variant="primary" onClick={() => goInstall(data.installUrl!, "/settings/github")}>Install GitHub App</Button>)}
            {data.configureUrl && <a href={data.configureUrl} target="_blank" rel="noreferrer"><Button>Configure GitHub App</Button></a>}
          </div>
        </>
      ) : (
        <>
          <Field label="Organization" hint="Leave empty to create the app on your own GitHub account."><Input value={org} onChange={(e) => setOrg(e.target.value)} /></Field>
          <Button variant="primary" disabled={a.busy} onClick={create}>Connect GitHub</Button>
          {a.error && <div className="mt-3"><Notice kind="error">{a.error}</Notice></div>}
        </>
      )}
    </Section>
  );
}
