// SPDX-License-Identifier: AGPL-3.0-only

// Mounts the screens of the other console packages. WP8b and WP8c export
// named route components from src/features/<name>/routes.tsx:
//   service/routes.tsx:  ServiceRoute, VolumeRoute, LogExplorer, ObservabilityRoute (take the environment id)
//   settings/routes.tsx: InstallSettings, AccountPage, ProjectSettingsPage
// They load lazily (one chunk per package). A module or export that is not
// in this build renders NotBuilt, so the shell builds on its own.
import { lazy, type ComponentType, type LazyExoticComponent } from "react";
import { NotBuilt } from "./NotBuilt";

const mods = import.meta.glob<Record<string, unknown>>("../features/*/routes.tsx");

/** A lazy component for `name` exported by features/<feature>/routes.tsx. */
export function featureComponent<P = any>(feature: string, name: string, overlay = false): LazyExoticComponent<ComponentType<P>> {
  const load = mods[`../features/${feature}/routes.tsx`];
  return lazy(async () => {
    const fallback = { default: (() => <NotBuilt overlay={overlay} />) as unknown as ComponentType<P> };
    if (!load) return fallback;
    const m = await load();
    const c = m[name] as ComponentType<P> | undefined;
    return c ? { default: c } : fallback;
  });
}

/** First path segments under /project/:pid that replace the canvas instead of overlaying it. */
export const fullPage = new Set(["logs", "observability", "settings"]);
