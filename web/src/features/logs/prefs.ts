// SPDX-License-Identifier: AGPL-3.0-only

// Per-browser view settings and the chosen time range.

import { useCallback, useState } from "react";
import { readPref, writePref } from "../../kit";
import type { Range } from "./query";

export interface ViewOpts {
  histogram: boolean;
  severity: boolean;
  data: boolean;
  attrs: boolean;
  json: boolean;
  wrap: boolean;
  service: boolean;
  deploy: boolean;
  replica: boolean;
  dataCols: string[];
}

export const DEFAULT_VIEW: ViewOpts = {
  histogram: true, severity: true, data: true, attrs: true, json: false, wrap: true,
  service: true, deploy: false, replica: false, dataCols: [],
};

function usePref<T extends object>(key: string, fallback: T): [T, (n: T) => void] {
  const [v, set] = useState<T>(() => ({ ...fallback, ...readPref<Partial<T>>(key, {}) }));
  const save = useCallback((n: T) => { set(n); writePref(key, n); }, [key]);
  return [v, save];
}

export const useViewOpts = (scope = "explorer", fallback: ViewOpts = DEFAULT_VIEW) => usePref<ViewOpts>(`5050.logs.view.${scope}`, fallback);
export const useRange = (scope = "explorer") => usePref<Range>(`5050.logs.range.${scope}`, { id: "7d" });
