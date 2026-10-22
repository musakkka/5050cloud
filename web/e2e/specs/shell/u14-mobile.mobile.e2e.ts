// SPDX-License-Identifier: AGPL-3.0-only

// U14 (the shell's part): 375 px dashboard and canvas list, no horizontal scroll.
import { expect, test } from "@playwright/test";

test("U14 dashboard and canvas at 375 px", async ({ page }) => {
  await page.goto("/");
  await page.getByRole("heading", { name: "Projects" }).waitFor();
  const overflow = () => page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
  expect(await overflow()).toBeLessThanOrEqual(0);
  const first = page.locator("a[href^='/project/']").first();
  if (await first.count()) {
    await first.click();
    await page.waitForURL(/\/project\//);
    expect(await overflow()).toBeLessThanOrEqual(0);
  }
});
