// SPDX-License-Identifier: AGPL-3.0-only

// Log Explorer: /project/:pid/logs (docs/SPEC.md 13.1). Environment-wide,
// with service and type filters, a histogram and the live tail.

import { useMemo, useState } from "react";
import type { Canvas } from "../../api/types.gen";
import { get } from "../service/client";
import { ErrorLine, useLoad } from "../service/ui";
import LogStream from "./LogStream";

const TYPES = [
  { id: "deploy", label: "Deploy" },
  { id: "build", label: "Build" },
  { id: "http", label: "HTTP" },
];

export default function LogExplorer({ environmentId }: { environmentId: string }) {
  const canvas = useLoad(() => get<Canvas>(`/environments/${encodeURIComponent(environmentId)}/canvas`), [environmentId]);
  const [services, setServices] = useState<string[]>([]);
  const [types, setTypes] = useState<string[]>(["deploy"]);
  const names = useMemo(() => Object.fromEntries((canvas.data?.services || []).map((s) => [s.id, s.config.name])), [canvas.data]);
  const toggle = (list: string[], v: string) => (list.includes(v) ? list.filter((x) => x !== v) : [...list, v]);

  return (
    <div className="flex h-full flex-col p-4 sm:p-8">
      <h1 className="mb-4 text-2xl font-semibold">Logs</h1>
      <ErrorLine>{canvas.error}</ErrorLine>
      <div className="mb-3 flex flex-wrap gap-2 text-sm">
        {(canvas.data?.services || []).map((s) => (
          <button key={s.id} onClick={() => setServices(toggle(services, s.id))}
            className={`rounded-full border px-3 py-0.5 ${services.includes(s.id) ? "border-pink-500 text-pink-600" : "border-gray-200 text-gray-600"}`}>{s.config.name}</button>
        ))}
        <span className="mx-2 border-l border-gray-200" />
        {TYPES.map((t) => (
          <button key={t.id} onClick={() => setTypes(types.includes(t.id) && types.length > 1 ? types.filter((x) => x !== t.id) : [...new Set([...types, t.id])])}
            className={`rounded-full border px-3 py-0.5 ${types.includes(t.id) ? "border-pink-500 text-pink-600" : "border-gray-200 text-gray-600"}`}>{t.label}</button>
        ))}
      </div>
      <div className="min-h-0 flex-1">
        <LogStream source={{ kind: "logs", environment: environmentId, services: services.length ? services : undefined, type: types.join(",") }}
          serviceNames={names} showHistogram defaultService />
      </div>
    </div>
  );
}
