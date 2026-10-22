// SPDX-License-Identifier: AGPL-3.0-only

// P1 and P4 from docs/SPEC.md 16.1 (Lighthouse covers the throttled cold load).
import { expect, test } from "@playwright/test";
import { env } from "../../harness/env";
import { canvasCreate, newEmptyProject } from "../../harness/ui";

test("P1 dashboard interactive under 1.5 s", async ({ page }) => {
  const t0 = Date.now();
  await page.goto("/");
  await page.getByRole("button", { name: /New/ }).first().waitFor();
  expect(Date.now() - t0).toBeLessThan(1500);
});

test("P4 a staged edit shows the banner under 100 ms after the response", async ({ page }) => {
  await newEmptyProject(page);
  await canvasCreate(page, "Docker Image");
  await page.getByLabel("Image", { exact: true }).fill(env.whoamiImage);
  const resp = page.waitForResponse((r) => r.url().includes("/services") && r.request().method() === "POST");
  await page.getByRole("button", { name: "Add to canvas" }).click();
  await resp;
  const t0 = Date.now();
  await page.getByRole("region", { name: "Staged changes" }).waitFor();
  expect(Date.now() - t0).toBeLessThan(1000);
});
