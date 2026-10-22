// SPDX-License-Identifier: AGPL-3.0-only

// Starts the MSW service worker so the console runs against generated
// fixtures with no 5050d (`npm run dev:mocks`). See README.md here.
import { setupWorker } from "msw/browser";
import { handlers, wsHandlers } from "./handlers.gen";

export const worker = setupWorker(...handlers, ...wsHandlers);

/** Starts the worker; requests without a handler pass through to the network. */
export function startMocks(): Promise<unknown> {
  return worker.start({ onUnhandledRequest: "bypass", quiet: true });
}
