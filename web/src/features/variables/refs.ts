// SPDX-License-Identifier: AGPL-3.0-only

// Reference sources for the "Add Reference" menu and the `${{` autocomplete:
// other services (with their own and platform variables) and shared variables.

import { useEffect, useMemo, useState } from "react";
import type { Canvas, VariablesResponse } from "../../api/types.gen";
import { get, svcPath } from "../service/client";

export interface RefSource {
  /** Display name of the source service ("Shared" for shared variables). */
  service: string;
  /** Icon key or image ref for BrandIcon. */
  icon?: string;
  isDb: boolean;
  shared?: boolean;
  vars: string[];
}

export interface RefItem {
  /** Variable name (without service). */
  name: string;
  /** Text inserted in a value: `${{Service.NAME}}`. */
  token: string;
  source: RefSource;
}

/** Other services and shared variables of the environment, loaded once per panel. */
export function useRefSources(eid: string, selfId: string, own: string[]) {
  const [others, setOthers] = useState<RefSource[]>([]);
  useEffect(() => {
    let off = false;
    (async () => {
      const c = await get<Canvas>(`/environments/${encodeURIComponent(eid)}/canvas`);
      const out: RefSource[] = await Promise.all(c.services.filter((s) => s.id !== selfId).map(async (s) => {
        let vars: string[] = [];
        try {
          const v = await get<VariablesResponse>(`${svcPath(eid, s.id)}/variables`);
          vars = [...new Set([...v.variables.map((x) => x.name), ...v.provided])].sort();
        } catch { /* no access to that service's variables */ }
        return {
          service: s.config.name,
          icon: s.config.source?.image || s.config.icon,
          isDb: s.kind === "database",
          vars,
        };
      }));
      let shared: RefSource[] = [];
      try {
        const sv = await get<VariablesResponse>(`/environments/${encodeURIComponent(eid)}/shared-variables`);
        if (sv.variables.length) shared = [{ service: "shared", isDb: false, shared: true, vars: sv.variables.map((x) => x.name).sort() }];
      } catch { /* none */ }
      if (!off) setOthers([...out.filter((s) => s.vars.length), ...shared]);
    })().catch(() => undefined);
    return () => { off = true; };
  }, [eid, selfId]);

  return useMemo(() => {
    const self: RefSource = { service: "", isDb: false, vars: [...own].sort() };
    const sources = [self, ...others];
    const items: RefItem[] = sources.flatMap((s) => s.vars.map((name) => ({
      name, source: s, token: "${{" + (s.service ? `${s.service}.` : "") + name + "}}",
    })));
    return { sources: others, items };
  }, [others, own]);
}

/** Fuzzy match: every query char appears in order. Returns matched indexes, or null. */
export function fuzzy(text: string, query: string): number[] | null {
  if (!query) return [];
  const t = text.toLowerCase(), q = query.toLowerCase();
  const sub = t.indexOf(q);
  if (sub >= 0) return Array.from({ length: q.length }, (_, i) => sub + i);
  const idx: number[] = [];
  let at = 0;
  for (const ch of q) {
    const f = t.indexOf(ch, at);
    if (f < 0) return null;
    idx.push(f);
    at = f + 1;
  }
  return idx;
}

/** Substring hits rank first, then fuzzy hits, each in source order. */
export function rank<T>(items: T[], query: string, text: (t: T) => string): { item: T; hits: number[] }[] {
  const exact: { item: T; hits: number[] }[] = [];
  const loose: { item: T; hits: number[] }[] = [];
  for (const item of items) {
    const s = text(item);
    const hits = fuzzy(s, query);
    if (!hits) continue;
    const contiguous = hits.length < 2 || hits.every((h, i) => i === 0 || h === hits[i - 1] + 1);
    (contiguous ? exact : loose).push({ item, hits });
  }
  return [...exact, ...loose];
}

const GENERATED = /\$\{\{\s*(secret|randomInt)\s*\(/;
/** True when a stored template contains a generator function. */
export const isGenerated = (reference?: string) => !!reference && GENERATED.test(reference);

/** True when the text holds a `${{...}}` token. */
export const hasRef = (s: string) => /\$\{\{[^}]*\}\}/.test(s);
