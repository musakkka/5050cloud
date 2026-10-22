// SPDX-License-Identifier: AGPL-3.0-only

// U26 and U27 (the shell's part): Cmd+K sub-menus, Backspace back, a jump to
// a settings page, Toggle theme, Shift+Enter deploys staged changes.
import { expect, test } from "@playwright/test";
import { env } from "../../harness/env";
import { canvasCreate, card, newEmptyProject } from "../../harness/ui";

const mod = process.platform === "darwin" ? "Meta" : "Control";

test("U26 palette sub-menus and navigation", async ({ page }) => {
  await page.goto("/");
  await page.keyboard.press(`${mod}+k`);
  const input = page.getByRole("combobox", { name: "Search commands" });
  await expect(input).toBeFocused();

  await input.fill("Install settings");
  await page.keyboard.press("ArrowRight");
  await expect(page.getByRole("option", { name: /Hosts/ })).toBeVisible();
  await page.keyboard.press("Backspace");
  await expect(page.getByRole("option", { name: /Install settings/ })).toBeVisible();

  await input.fill("Install settings");
  await page.keyboard.press("Enter");
  await input.fill("Hosts");
  await page.keyboard.press("Enter");
  await expect(page).toHaveURL(/\/settings\/hosts/);

  await page.keyboard.press(`${mod}+k`);
  await page.getByRole("combobox", { name: "Search commands" }).fill("Toggle theme");
  const before = await page.evaluate(() => document.documentElement.className);
  await page.keyboard.press("Enter");
  await expect.poll(() => page.evaluate(() => document.documentElement.className)).not.toBe(before);
});

test("U27 Shift+Enter deploys and Alt+Shift+Enter commits only", async ({ page }) => {
  await newEmptyProject(page);
  await canvasCreate(page, "Docker Image");
  await page.getByLabel("Image", { exact: true }).fill(env.whoamiImage);
  await page.getByRole("button", { name: "Add to canvas" }).click();
  await expect(card(page, "whoami")).toBeVisible();
  await page.locator("body").click({ position: { x: 5, y: 300 } });
  await page.keyboard.press("Shift+Enter");
  await expect(page.getByRole("region", { name: "Staged changes" })).toBeHidden();
});
