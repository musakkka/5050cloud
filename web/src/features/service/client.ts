// SPDX-License-Identifier: AGPL-3.0-only

// Small fetch client for the service panel features (docs/SPEC.md 11).
// Session cookie auth; every mutation carries X-5050-CSRF: 1 (11.7).

export class ApiError extends Error {
  constructor(public status: number, public code: string, message: string) {
    super(message);
  }
}

// `npm run dev:mocks`: richer fixtures for the service panel and deployment drawer
// (deployment/mocks.ts). The import is compiled out of production builds.
const mocksReady: Promise<unknown> = import.meta.env.VITE_MOCKS === "1"
  ? Promise.all([
      import("../deployment/mocks").then((m) => m.installServiceMocks()),
      import("../service-settings/mocks").then((m) => m.installSettingsMocks()),
    ]).catch(() => undefined)
  : Promise.resolve();

export async function req<T>(method: string, path: string, body?: unknown, raw?: BodyInit): Promise<T> {
  await mocksReady;
  const headers: Record<string, string> = { Accept: "application/json" };
  if (method !== "GET") headers["X-5050-CSRF"] = "1";
  let payload: BodyInit | undefined = raw;
  if (body !== undefined) {
    headers["Content-Type"] = "application/json";
    payload = JSON.stringify(body);
  }
  const r = await fetch(`/api/v1${path}`, { method, headers, body: payload, credentials: "same-origin" });
  if (r.status === 204) return undefined as T;
  const text = await r.text();
  let data: any = undefined;
  try { data = text ? JSON.parse(text) : undefined; } catch { data = undefined; }
  if (!r.ok) {
    throw new ApiError(r.status, data?.code || "internal", data?.message || text || `Request failed (${r.status})`);
  }
  return data as T;
}

export const get = <T>(p: string) => req<T>("GET", p);
export const post = <T>(p: string, b?: unknown) => req<T>("POST", p, b ?? {});
export const put = <T>(p: string, b: unknown) => req<T>("PUT", p, b);
export const patch = <T>(p: string, b: unknown) => req<T>("PATCH", p, b);
export const del = <T>(p: string, b?: unknown) => req<T>("DELETE", p, b);

/** Path prefix for a service in an environment. */
export const svcPath = (eid: string, sid: string) =>
  `/environments/${encodeURIComponent(eid)}/services/${encodeURIComponent(sid)}`;

export function qs(params: Record<string, string | number | undefined | null | false>): string {
  const u = new URLSearchParams();
  for (const [k, v] of Object.entries(params)) {
    if (v === undefined || v === null || v === false || v === "") continue;
    u.set(k, String(v));
  }
  const s = u.toString();
  return s ? `?${s}` : "";
}

export const errText = (e: unknown) => (e instanceof Error ? e.message : String(e));
