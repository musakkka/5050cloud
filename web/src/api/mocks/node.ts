// SPDX-License-Identifier: AGPL-3.0-only

// MSW for vitest: `server.listen()` in a test, then fetch against
// http://localhost/api/v1/... and get the generated fixtures.
import { setupServer } from "msw/node";
import { handlers } from "./handlers.gen";

export const server = setupServer(...handlers);
