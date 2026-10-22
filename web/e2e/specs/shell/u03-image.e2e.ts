// SPDX-License-Identifier: AGPL-3.0-only

// U3: canvas + Create > Docker Image > staged card > Deploy > Active.
import { expect, test } from "@playwright/test";
import { apiGet, until } from "../../harness/api";
import { env } from "../../harness/env";
import { canvasCreate, card, deployStaged, newEmptyProject } from "../../harness/ui";

test("U3 Docker image from the canvas", async ({ page }) => {
  const { eid } = await newEmptyProject(page);
  await canvasCreate(page, "Docker Image");
  await page.getByLabel("Image", { exact: true }).fill(env.whoamiImage);
  await page.getByRole("button", { name: "Add to canvas" }).click();

  const c = card(page, "whoami");
  await expect(c).toBeVisible();
  await expect(c.getByText("New")).toBeVisible();          // staged tag
  await expect(page.getByText("1 change")).toBeVisible();   // banner count

  await deployStaged(page);
  await until(async () => {
    const cv = await apiGet<{ services: { config: { name: string }; status: string }[] }>(page, `/environments/${eid}/canvas`);
    return cv.services.find((s) => s.config.name === "whoami" && s.status === "online");
  }, 120_000);
  await expect(c.getByText("New")).toBeHidden();
});
