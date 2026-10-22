// SPDX-License-Identifier: AGPL-3.0-only

// The UI suite (docs/SPEC.md 13.8). It runs against a real install:
//   E2E_CONSOLE_URL   console URL, e.g. https://console.203-0-113-7.sslip.io
//   E2E_SETUP_TOKEN   the installer's one-time setup token (only U1 uses it)
//   E2E_OWNER_EMAIL / E2E_OWNER_PASSWORD   the owner U1 creates (defaults below)
//   E2E_BROWSER_CHANNEL  "chrome" to use an installed Chrome instead of Playwright's Chromium
// Spec files end in .e2e.ts so the console's Vitest run never picks them up.
import { defineConfig, devices } from "@playwright/test";

const baseURL = process.env.E2E_CONSOLE_URL || "http://127.0.0.1:5173";
const channel = process.env.E2E_BROWSER_CHANNEL || undefined;

export default defineConfig({
  testDir: ".",
  testMatch: "**/*.e2e.ts",
  outputDir: "report/results",
  fullyParallel: false,
  workers: 1,
  retries: process.env.CI ? 1 : 0,
  timeout: 120_000,
  expect: { timeout: 15_000 },
  reporter: [["list"], ["html", { outputFolder: "report/html", open: "never" }]],
  use: { baseURL, trace: "retain-on-failure", screenshot: "only-on-failure", ignoreHTTPSErrors: !!process.env.E2E_INSECURE_TLS },
  projects: [
    // U1 creates the owner and saves the signed-in state every later spec reuses.
    { name: "setup", testMatch: "specs/shell/u01-setup.e2e.ts", use: { ...devices["Desktop Chrome"], channel } },
    { name: "desktop", dependencies: ["setup"], testIgnore: ["specs/shell/u01-setup.e2e.ts", "**/*.mobile.e2e.ts"], use: { ...devices["Desktop Chrome"], channel, storageState: "report/owner.json" } },
    { name: "mobile", dependencies: ["setup"], testMatch: "**/*.mobile.e2e.ts", use: { ...devices["Pixel 7"], viewport: { width: 375, height: 812 }, channel, storageState: "report/owner.json" } },
  ],
});
