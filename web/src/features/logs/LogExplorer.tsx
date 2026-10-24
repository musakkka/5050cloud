// SPDX-License-Identifier: AGPL-3.0-only

// Log Explorer: /project/:pid/logs (docs/SPEC.md 13.1). Environment-wide: one filter bar with tag
// suggestions (@service, @level, ...), time range, histogram and the live tail.

import { useEffect, useMemo, useState } from "react";
import type { Canvas } from "../../api/types.gen";
import { get } from "../service/client";
import { useLoad } from "../service/ui";
import LogStream from "./LogStream";

const mocksOn = import.meta.env.VITE_MOCKS === "1";

export default function LogExplorer({ environmentId }: { environmentId: string }) {
  const canvas = useLoad(() => get<Canvas>(`/environments/${encodeURIComponent(environmentId)}/canvas`), [environmentId]);
  const [extra, setExtra] = useState<Record<string, string>>({});
  useEffect(() => {
    if (!mocksOn) return;
    void import("./mocks").then((m) => setExtra(Object.fromEntries(m.EXPLORER_SERVICES.map((s) => [s.id, s.name]))));
  }, []);
  const names = useMemo(
    () => ({ ...extra, ...Object.fromEntries((canvas.data?.services || []).map((s) => [s.id, s.config.name])) }),
    [canvas.data, extra],
  );
  return (
    <div className="h-full p-2 sm:p-4">
      <LogStream source={{ kind: "logs", environment: environmentId }} serviceNames={names} showHistogram defaultService scope="explorer" />
    </div>
  );
}
