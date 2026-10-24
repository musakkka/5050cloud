// SPDX-License-Identifier: AGPL-3.0-only

// Data helpers shared by /new, the template gallery and the template page.
import { api, type GitHubRepo, type GitHubStatus, type TemplateDetail, type TemplateSummary } from "../../api/client";

/** Resolves once the demo fixtures are registered (dev mocks only; immediate otherwise). */
const ready: Promise<unknown> = import.meta.env.VITE_MOCKS === "1"
  ? import("./mocks").then((m) => m.install()).catch(() => undefined)
  : Promise.resolve();

let templatesCache: Promise<TemplateSummary[]> | undefined;

/** Every template, fetched once per page load (search filters on the client). */
export function loadTemplates(fresh = false): Promise<TemplateSummary[]> {
  if (fresh || !templatesCache) {
    templatesCache = ready.then(() => api.templates()).catch((e) => { templatesCache = undefined; throw e; });
  }
  return templatesCache;
}
export const loadTemplate = (id: string): Promise<TemplateDetail> => ready.then(() => api.template(id));
export const loadGitHub = (): Promise<GitHubStatus> => ready.then(() => api.github());
export const loadRepos = (refresh = false): Promise<GitHubRepo[]> => ready.then(() => api.repos(undefined, refresh));

/** Display order of the database step, matching what people reach for most. */
const DB_ORDER = ["postgres", "postgresql", "redis", "mongodb", "mongo", "mysql", "mariadb"];
export function sortDatabases(list: TemplateSummary[]) {
  const rank = (t: TemplateSummary) => { const i = DB_ORDER.indexOf(t.id.toLowerCase()); return i < 0 ? 99 : i; };
  return [...list].sort((a, b) => rank(a) - rank(b) || a.name.localeCompare(b.name));
}

/** Gallery order: databases people deploy most, then apps, then the rest by name. */
const POPULAR = ["postgres", "redis", "n8n", "mysql", "mongodb", "minio", "nodejs", "ghost", "umami", "metabase", "wordpress", "plausible"];
export function sortPopular(list: TemplateSummary[]) {
  const rank = (t: TemplateSummary) => { const i = POPULAR.indexOf(t.id.toLowerCase()); return i < 0 ? 99 : i; };
  return [...list].sort((a, b) => rank(a) - rank(b) || a.name.localeCompare(b.name));
}

export const CATEGORY_LABEL: Record<string, string> = { database: "Databases", storage: "Storage", app: "Apps" };
export const categoryLabel = (c: string) => CATEGORY_LABEL[c] || c.charAt(0).toUpperCase() + c.slice(1);

/**
 * Loose match used by every picker step: all query characters in order,
 * ranked by how early and how tightly they match. Returns -1 for no match.
 */
export function fuzzyScore(text: string, query: string): number {
  const t = text.toLowerCase(), q = query.trim().toLowerCase();
  if (!q) return 0;
  const at = t.indexOf(q);
  if (at === 0) return 1000;
  if (at > 0) return 800 - at;
  let ti = 0, gaps = 0, first = -1;
  for (const ch of q) {
    if (ch === " ") continue;
    const i = t.indexOf(ch, ti);
    if (i < 0) return -1;
    if (first < 0) first = i;
    gaps += i - ti;
    ti = i + 1;
  }
  return 400 - gaps * 4 - first;
}

export function fuzzyFilter<T>(items: T[], query: string, text: (t: T) => string): T[] {
  if (!query.trim()) return items;
  return items.map((it) => ({ it, s: fuzzyScore(text(it), query) })).filter((x) => x.s >= 0).sort((a, b) => b.s - a.s).map((x) => x.it);
}

/** "owner/name" for a pasted GitHub URL, or undefined when the text is not one. */
export function pastedRepo(text: string): string | undefined {
  const m = /^(?:https?:\/\/)?(?:www\.)?github\.com\/([\w.-]+)\/([\w.-]+?)(?:\.git)?\/?(?:[?#].*)?$/i.exec(text.trim());
  return m ? `${m[1]}/${m[2]}` : undefined;
}

/** The public page of an image on its registry, when there is a well known one. */
export function registryPage(ref: string): { label: string; href: string } | undefined {
  const s = ref.trim().split("@")[0];
  const noTag = s.replace(/:[\w][\w.-]*$/, "");
  const parts = noTag.split("/");
  const host = parts.length > 1 && /[.:]/.test(parts[0]) ? parts[0] : "docker.io";
  const path = host === "docker.io" ? noTag.replace(/^docker\.io\//, "") : parts.slice(1).join("/");
  if (!path) return undefined;
  if (host === "docker.io") {
    return path.includes("/") ? { label: `hub.docker.com/r/${path}`, href: `https://hub.docker.com/r/${path}` } : { label: `hub.docker.com/_/${path}`, href: `https://hub.docker.com/_/${path}` };
  }
  if (host === "ghcr.io") { const [o, ...r] = path.split("/"); return { label: `github.com/${o}/${r.join("/")}`, href: `https://github.com/${o}/${r.join("/")}/pkgs/container/${r.join("/")}` }; }
  if (host === "quay.io") return { label: `quay.io/repository/${path}`, href: `https://quay.io/repository/${path}` };
  if (host === "registry.gitlab.com") return { label: `gitlab.com/${path}`, href: `https://gitlab.com/${path}/container_registry` };
  if (host === "mcr.microsoft.com") return { label: `mcr.microsoft.com/${path}`, href: `https://mcr.microsoft.com/artifact/mar/${path}` };
  return undefined;
}
