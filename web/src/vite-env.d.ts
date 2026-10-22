// SPDX-License-Identifier: AGPL-3.0-only

/// <reference types="vite/client" />

interface ImportMetaEnv {
  /** "1" starts the MSW mocks (npm run dev:mocks). */
  readonly VITE_MOCKS?: string;
}
