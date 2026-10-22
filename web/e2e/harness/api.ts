// SPDX-License-Identifier: AGPL-3.0-only

// Read-only API helpers for assertions. Specs drive the UI; they only read
// state through the API (docs/SPEC.md 13.8).
import type { APIRequestContext, Page } from "@playwright/test";

export async function apiGet<T>(req: APIRequestContext | Page, path: string): Promise<T> {
  const r = "request" in req ? req.request : req;
  const res = await r.get(`/api/v1${path}`);
  if (!res.ok()) throw new Error(`GET ${path}: ${res.status()} ${await res.text()}`);
  return (await res.json()) as T;
}

/** Polls fn until it returns a truthy value or the timeout passes. */
export async function until<T>(fn: () => Promise<T | undefined | null | false>, timeoutMs = 60_000, everyMs = 1000): Promise<T> {
  const end = Date.now() + timeoutMs;
  let last: unknown;
  while (Date.now() < end) {
    try { const v = await fn(); if (v) return v; } catch (e) { last = e; }
    await new Promise((r) => setTimeout(r, everyMs));
  }
  throw new Error(`timed out${last ? `: ${String(last)}` : ""}`);
}
