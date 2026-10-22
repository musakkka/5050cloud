// SPDX-License-Identifier: AGPL-3.0-only

// Config paths and JSON merge patches (RFC 7396) for Settings fields.

export function getPath(obj: unknown, path: string): unknown {
  let cur: any = obj;
  for (const p of path.split(".")) {
    if (cur === null || cur === undefined) return undefined;
    cur = cur[p];
  }
  return cur;
}

/** A merge patch that sets `path` to `value`. Empty values become null (back to the default). */
export function patchFor(path: string, value: unknown): Record<string, unknown> {
  const parts = path.split(".");
  const empty = value === "" || value === undefined || (Array.isArray(value) && value.length === 0);
  const root: Record<string, unknown> = {};
  let cur = root;
  for (let i = 0; i < parts.length - 1; i++) {
    const next: Record<string, unknown> = {};
    cur[parts[i]] = next;
    cur = next;
  }
  cur[parts[parts.length - 1]] = empty ? null : value;
  return root;
}

const norm = (v: unknown) => (v === undefined || v === null || v === "" || (Array.isArray(v) && v.length === 0) ? undefined : JSON.stringify(v));

/** True when the staged config differs from the committed one at `path`. */
export const isStaged = (committed: unknown, staged: unknown, path: string) =>
  staged !== undefined && norm(getPath(committed, path)) !== norm(getPath(staged, path));

/** Docker image reference check: [registry/]name[:tag][@digest]. */
export function validImage(ref: string): string | null {
  const r = ref.trim();
  if (!r) return "Enter an image, for example nginx:1.27";
  if (/\s/.test(r)) return "Image names cannot contain spaces";
  const re = /^(?:[a-z0-9.-]+(?::\d+)?\/)?[a-z0-9]+(?:[._-][a-z0-9]+)*(?:\/[a-z0-9]+(?:[._-][a-z0-9]+)*)*(?::[A-Za-z0-9_][A-Za-z0-9_.-]{0,127})?(?:@sha256:[a-f0-9]{64})?$/;
  return re.test(r) ? null : "Not a valid image name. Use lowercase, for example ghcr.io/owner/app:latest";
}

/** Accepts owner/name or an https GitHub URL; returns the value to store or an error. */
export function normaliseRepo(input: string): { repo?: string; error?: string } {
  const t = input.trim().replace(/\.git$/, "").replace(/\/$/, "");
  if (/^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+$/.test(t)) return { repo: t };
  if (/^https:\/\/[^\s/]+\/[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+$/.test(t)) return { repo: t };
  return { error: "Use owner/name or a public https repo URL" };
}
