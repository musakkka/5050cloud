// SPDX-License-Identifier: AGPL-3.0-only

/**
 * Checks a Docker image reference ("nginx:1.27", "ghcr.io/o/r:tag",
 * "img@sha256:..."). Returns a sentence when it is not valid, or "" when it is.
 */
export function imageError(ref: string): string {
  const s = ref.trim();
  if (!s) return "Enter an image name.";
  if (/\s/.test(s)) return "An image name has no spaces.";
  if (s.length > 255) return "That image name is too long.";
  const m = /^(?:([a-zA-Z0-9.-]+(?::[0-9]+)?)\/)?([a-z0-9]+(?:[._-][a-z0-9]+)*(?:\/[a-z0-9]+(?:[._-][a-z0-9]+)*)*)(?::([\w][\w.-]{0,127}))?(?:@(sha256:[a-f0-9]{64}))?$/.exec(s);
  if (!m) {
    if (/[A-Z]/.test(s.split(":")[0].split("/").slice(-1)[0])) return "Repository names are lowercase.";
    return "That is not a valid image name. Example: nginx:1.27 or ghcr.io/owner/app:latest";
  }
  return "";
}

/** The registry host of an image reference, "docker.io" when none is given. */
export function imageRegistry(ref: string): string {
  const first = ref.trim().split("/")[0];
  return ref.includes("/") && (/[.:]/.test(first) || first === "localhost") ? first : "docker.io";
}

/** The default service name for an image: the last path part without tag. */
export function imageName(ref: string): string {
  const last = ref.trim().split("@")[0].split("/").pop() || "service";
  return last.split(":")[0].slice(0, 32) || "service";
}

/** Normalises a pasted GitHub URL or "owner/name" into "owner/name" or a full https URL. */
export function repoRef(input: string): string {
  const s = input.trim().replace(/\.git$/, "").replace(/\/$/, "");
  const m = /^(?:https?:\/\/)?(?:www\.)?github\.com\/([\w.-]+)\/([\w.-]+)/.exec(s);
  if (m) return `https://github.com/${m[1]}/${m[2]}`;
  return s;
}
