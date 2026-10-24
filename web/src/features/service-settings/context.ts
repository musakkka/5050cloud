// SPDX-License-Identifier: AGPL-3.0-only

// Shared state for the Settings sections: the service, its staged config and
// the staging calls (merge patches of the service config, docs/SPEC.md 13.3).

import { createContext, useContext } from "react";
import type { Service, ServiceConfig } from "../../api/types.gen";
import { isStaged } from "./patch";

export interface SettingsCtx {
  eid: string;
  base: string;
  svc: Service;
  /** The config with staged changes applied. */
  cfg: ServiceConfig;
  stage: (path: string, value: unknown) => Promise<void>;
  stagePatch: (p: Record<string, unknown>) => Promise<void>;
  onService: (s: Service) => void;
  onDeleted?: () => void;
}

export const Settings = createContext<SettingsCtx | null>(null);
export const useSettings = () => useContext(Settings)!;

/** Staged marker and config-file lock for a config path. */
export function useFieldState(path: string) {
  const { svc } = useSettings();
  return { staged: isStaged(svc.config, svc.staged, path), locked: svc.source?.[path] };
}

export const isDatabase = (s: Service) => s.kind === "database" || !!s.config.template;
