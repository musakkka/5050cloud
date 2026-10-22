// SPDX-License-Identifier: AGPL-3.0-only

import { expect, type Page } from "@playwright/test";

/** Creates an empty project from /new and returns its id and environment id from the URL. */
export async function newEmptyProject(page: Page): Promise<{ pid: string; eid: string }> {
  await page.goto("/new");
  await page.getByRole("button", { name: "Empty Project" }).click();
  await page.waitForURL(/\/project\/[^/?]+\?environment=/);
  const u = new URL(page.url());
  const pid = u.pathname.split("/")[2];
  const eid = u.searchParams.get("environment")!;
  await expect(page.getByRole("button", { name: "Add a service" })).toBeVisible();
  return { pid, eid };
}

/** Opens the canvas + Create menu and picks an item. */
export async function canvasCreate(page: Page, item: string) {
  await page.getByRole("button", { name: "+ Create" }).click();
  await page.getByRole("menuitem", { name: item, exact: true }).click();
}

/** Presses Deploy on the staged changes banner. */
export async function deployStaged(page: Page) {
  const banner = page.getByRole("region", { name: "Staged changes" });
  await expect(banner).toBeVisible();
  await banner.getByRole("button", { name: "Deploy" }).click();
  await expect(banner).toBeHidden();
}

export const card = (page: Page, name: string) => page.locator(`[data-testid="service-card"][data-name="${name}"]`);
