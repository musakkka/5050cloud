// SPDX-License-Identifier: AGPL-3.0-only

// U1: set up the owner from the installer link, log out, log in.
// The 2FA half of U1 (enable 2FA, log in with a TOTP code) lives with the
// account screens (WP8c, specs/account/); harness/totp.ts computes the code.
import { expect, test } from "@playwright/test";
import { env, OWNER_STATE } from "../../harness/env";

test("U1 owner setup, logout and login", async ({ page }) => {
  const status = await (await page.request.get("/api/v1/setup")).json();
  if (status.needsSetup) {
    test.skip(!env.setupToken, "E2E_SETUP_TOKEN is required on a fresh install");
    await page.goto(`/setup?token=${encodeURIComponent(env.setupToken)}`);
    await page.getByLabel("Name").fill(env.ownerName);
    await page.getByLabel("Email").fill(env.ownerEmail);
    await page.getByLabel("Password").fill(env.ownerPassword);
    await page.getByRole("button", { name: "Create account" }).click();
    await expect(page.getByRole("heading", { name: "Projects" })).toBeVisible();

    await page.getByRole("button", { name: "Account menu" }).click();
    await page.getByRole("button", { name: "Log out" }).click();
    await expect(page).toHaveURL(/\/login/);
  }

  await page.goto("/login");
  await page.getByLabel("Email").fill(env.ownerEmail);
  await page.getByLabel("Password").fill(env.ownerPassword);
  await page.getByRole("button", { name: "Log in" }).click();
  await expect(page.getByRole("heading", { name: "Projects" })).toBeVisible();
  await page.context().storageState({ path: OWNER_STATE });
});
