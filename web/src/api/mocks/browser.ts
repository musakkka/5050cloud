// SPDX-License-Identifier: AGPL-3.0-only

// Starts the MSW service worker so the console runs against generated
// fixtures with no 5050d (`npm run dev:mocks`). See README.md here.
import { http, HttpResponse, type HttpHandler } from "msw";
import { setupWorker } from "msw/browser";
import { handlers, wsHandlers } from "./handlers.gen";
import { canvasHandlers } from "../../features/canvas/mocks";

/**
 * The generated fixtures set every boolean to true, which makes the install
 * read-only and pending a restore. Demo mode wants a healthy install, so the
 * /me and /install bodies are passed through with those flags cleared.
 * Add `?mock=readonly` to the URL to keep the read-only fixture.
 */
function healthy(path: string, pick: (body: Record<string, any>) => Record<string, any> | undefined) {
  const gen = handlers.find((h) => (h as HttpHandler).info?.header === `GET */api/v1/${path}`) as HttpHandler | undefined;
  return http.get(`*/api/v1/${path}`, async (ctx) => {
    if (!gen || new URLSearchParams(location.search).get("mock") === "readonly") return undefined;
    const r = (await (gen as any).resolver(ctx)) as Response;
    const body = (await r.json()) as Record<string, any>;
    const install = pick(body);
    if (install) { install.readOnly = false; delete install.restore; }
    return HttpResponse.json(body);
  });
}

const overrides = [healthy("me", (b) => b.install), healthy("install", (b) => b)];

export const worker = setupWorker(...overrides, ...canvasHandlers, ...handlers, ...wsHandlers);

/** Starts the worker; requests without a handler pass through to the network. */
export function startMocks(): Promise<unknown> {
  return worker.start({ onUnhandledRequest: "bypass", quiet: true });
}
