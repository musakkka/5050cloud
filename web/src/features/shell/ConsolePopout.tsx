// SPDX-License-Identifier: AGPL-3.0-only

// Full-screen console in its own tab (/console-popout?environmentId=&serviceId=&replica=&name=&back=).
// No app chrome: a 34 px top bar with the service breadcrumb, the terminal below.
// The route sits outside the app shell (the route is listed in the console integration notes).

import { useEffect, useState } from "react";
import { useSearchParams } from "react-router-dom";
import type { Service } from "../../api/types.gen";
import { Spinner } from "../../kit";
import { errText, get, svcPath } from "../service/client";
import { ConsoleSurface } from "./ConsoleSurface";
import "./console.css";

export default function ConsolePopout() {
  const [params] = useSearchParams();
  const eid = params.get("environmentId") ?? "";
  const sid = params.get("serviceId") ?? "";
  const replica = params.get("replica") ?? undefined;
  const back = params.get("back") ?? "/";
  const [svc, setSvc] = useState<Service | null>(null);
  const [err, setErr] = useState<string | null>(null);

  useEffect(() => {
    if (!eid || !sid) return;
    let live = true;
    get<Service>(svcPath(eid, sid))
      .then((s) => { if (live) setSvc(s); })
      .catch((e) => { if (live) setErr(errText(e)); });
    return () => { live = false; };
  }, [eid, sid]);

  if (!eid || !sid) {
    return <Message title="This console link is incomplete" body="Open the console from its service panel to get a working link." />;
  }
  if (err) return <Message title="The console could not load" body={err} />;
  if (!svc) {
    return (
      <div className="console-surface flex h-dvh items-center justify-center">
        <Spinner />
      </div>
    );
  }
  return (
    <div className="console-surface h-dvh">
      <ConsoleSurface
        variant="popout"
        initialReplica={replica}
        backHref={back}
        target={{ eid, sid, name: svc.config.name, replicas: svc.replicas }}
      />
    </div>
  );
}

function Message({ title, body }: { title: string; body: string }) {
  return (
    <div className="console-surface flex h-dvh flex-col items-center justify-center gap-2 px-6 text-center">
      <p className="text-lead text-fg-2">{title}</p>
      <p className="max-w-sm text-caption text-fg-4">{body}</p>
    </div>
  );
}
