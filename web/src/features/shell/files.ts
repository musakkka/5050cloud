// SPDX-License-Identifier: AGPL-3.0-only

// Files drawer API: a listing, upload, download and delete inside one running
// instance. A missing folder answers 404 with code "path_not_found"; a server
// without these routes answers 404 or 501 and the drawer says so. Paths are relative to the instance's working directory ("" is the
// root). The routes are listed in the console integration notes;
// under `npm run dev:mocks` the calls answer from mocks.ts.

import { req, svcPath } from "../service/client";

export interface FileEntry {
  name: string;
  type: "dir" | "file";
  size: number;
  /** Unix seconds. */
  modified: number;
}

export interface Listing {
  /** Absolute working directory of the instance, e.g. /app. */
  cwd: string;
  /** Relative path inside cwd; "" for the root. */
  path: string;
  entries: FileEntry[];
}

const MOCK = import.meta.env.VITE_MOCKS === "1";

const query = (replica: string, path: string) => `?replica=${encodeURIComponent(replica)}&path=${encodeURIComponent(path)}`;

export const joinPath = (dir: string, name: string) => (dir ? `${dir}/${name}` : name);

/** Absolute path of an entry, for the terminal and for display. */
export const absPath = (cwd: string, rel: string) => (rel ? `${cwd.replace(/\/$/, "")}/${rel}` : cwd);

export async function listFiles(eid: string, sid: string, replica: string, path: string): Promise<Listing> {
  if (MOCK) return (await import("./mocks")).mockListing(path);
  return req<Listing>("GET", `${svcPath(eid, sid)}/files${query(replica, path)}`);
}

export async function deleteFile(eid: string, sid: string, replica: string, path: string): Promise<void> {
  if (MOCK) return (await import("./mocks")).mockDelete(path);
  await req<void>("DELETE", `${svcPath(eid, sid)}/files${query(replica, path)}`);
}

/** Same-origin URL that the browser downloads as an attachment. */
export function downloadHref(eid: string, sid: string, replica: string, path: string): string {
  return `/api/v1${svcPath(eid, sid)}/files/content${query(replica, path)}`;
}

/** Uploads one file into `dir`. Reports progress from 0 to 100. */
export function uploadFile(eid: string, sid: string, replica: string, dir: string, file: File, onProgress: (pct: number) => void): Promise<void> {
  if (MOCK) return import("./mocks").then((m) => m.mockUpload(file, onProgress));
  return new Promise<void>((resolve, reject) => {
    const x = new XMLHttpRequest();
    x.open("POST", `/api/v1${svcPath(eid, sid)}/files${query(replica, dir)}`);
    x.withCredentials = true;
    x.setRequestHeader("X-5050-CSRF", "1");
    x.upload.onprogress = (e) => { if (e.lengthComputable) onProgress(Math.round((e.loaded / e.total) * 100)); };
    x.onload = () => {
      if (x.status >= 200 && x.status < 300) resolve();
      else reject(new Error(errorText(x.responseText, x.status)));
    };
    x.onerror = () => reject(new Error("Upload failed. Check the connection and try again."));
    const body = new FormData();
    body.append("file", file, file.name);
    x.send(body);
  });
}

function errorText(raw: string, status: number): string {
  try {
    const j = JSON.parse(raw) as { message?: string };
    if (j.message) return j.message;
  } catch { /* not JSON */ }
  return `Upload failed (${status}).`;
}

/** Sorting: folders first, then files with dotfiles first, each case-insensitive by name. */
export function sortEntries(entries: FileEntry[]): FileEntry[] {
  return [...entries].sort((a, b) => {
    if (a.type !== b.type) return a.type === "dir" ? -1 : 1;
    if (a.type === "file") {
      const da = a.name.startsWith(".") ? 0 : 1;
      const db = b.name.startsWith(".") ? 0 : 1;
      if (da !== db) return da - db;
    }
    return a.name.toLowerCase().localeCompare(b.name.toLowerCase());
  });
}

/** Badge for a row: amber folder, coloured extension marks, or a generic page glyph. */
export function fileBadge(e: FileEntry): { kind: "folder" } | { kind: "text"; text: string; cls: string } | { kind: "page" } {
  if (e.type === "dir") return { kind: "folder" };
  const ext = e.name.includes(".") ? e.name.split(".").pop()!.toLowerCase() : "";
  if (ext === "py") return { kind: "text", text: "PY", cls: "file-badge-py" };
  if (ext === "json") return { kind: "text", text: "{}", cls: "file-badge-json" };
  if (ext === "sh") return { kind: "text", text: "$", cls: "file-badge-sh" };
  if (ext === "md") return { kind: "text", text: "M↓", cls: "file-badge-md" };
  return { kind: "page" };
}

/** Local time as YYYY-MM-DD HH:mm. */
export function formatStamp(unixSeconds: number): string {
  const d = new Date(unixSeconds * 1000);
  const p = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`;
}
