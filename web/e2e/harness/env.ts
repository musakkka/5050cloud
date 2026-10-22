// SPDX-License-Identifier: AGPL-3.0-only

export const env = {
  consoleUrl: process.env.E2E_CONSOLE_URL || "http://127.0.0.1:5173",
  setupToken: process.env.E2E_SETUP_TOKEN || "",
  ownerEmail: process.env.E2E_OWNER_EMAIL || "owner@example.com",
  ownerPassword: process.env.E2E_OWNER_PASSWORD || "e2e-owner-password-1",
  ownerName: process.env.E2E_OWNER_NAME || "Ada Owner",
  /** Pinned so U3 does not depend on a moving tag. */
  whoamiImage: process.env.E2E_WHOAMI_IMAGE || "traefik/whoami:v1.10.1",
  smtpUrl: process.env.E2E_SMTP_URL || "",
};

/** Where U1 stores the owner's signed-in browser state. */
export const OWNER_STATE = "report/owner.json";
/** Where U1 stores the TOTP secret it enabled, for later logins. */
export const OWNER_TOTP = "report/owner-totp.txt";
