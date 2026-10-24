// SPDX-License-Identifier: AGPL-3.0-only

// Host calls for install settings > Hosts (SPEC 11.2 and the host metrics route). Built on the
// shared request helper in src/api/client.ts, so errors arrive as ApiError with the server's sentence.
import { request } from "../../api/client";
import type * as T from "../../api/types.gen";

export type HostMetricsRange = "1h" | "6h" | "24h" | "7d";

const enc = encodeURIComponent;

export const hostsApi = {
  list: () => request<T.Host[]>("/hosts"),
  joinToken: (b: T.CreateJoinTokenRequest) => request<T.JoinToken>("/hosts/join-tokens", { body: b }),
  update: (id: string, b: T.UpdateHostRequest) => request<T.Host>(`/hosts/${enc(id)}`, { method: "PATCH", body: b }),
  maintenance: (id: string, on: boolean) => request<T.Host>(`/hosts/${enc(id)}/maintenance`, { body: { on } as T.MaintenanceRequest }),
  readopt: (id: string) => request<T.Host>(`/hosts/${enc(id)}/readopt`, { body: {} }),
  clearConflicts: (id: string) => request<T.Host>(`/hosts/${enc(id)}/conflicts`, { method: "DELETE" }),
  remove: (id: string, force = false) => request<void>(`/hosts/${enc(id)}${force ? "?force=1" : ""}`, { method: "DELETE" }),
  metrics: (id: string, range: HostMetricsRange, perReplica: boolean) =>
    request<T.MetricsResponse>(`/hosts/${enc(id)}/metrics?range=${range}${perReplica ? "&view=replica" : ""}`),
};

/** A readable message for any thrown value. */
export const messageOf = (e: unknown) => (e instanceof Error ? e.message : String(e));
