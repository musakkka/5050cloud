// SPDX-License-Identifier: AGPL-3.0-only

// U15: Settings > Networking > Custom Domain: add a name, see the DNS records
// with copy buttons, watch the status. On a real install E2E_CUSTOM_DOMAIN
// names a domain that points at it and the status must reach Active; without
// it the name does not point here and the status stays "Waiting for DNS".
import { expect, test } from "@playwright/test";
import { apiGet, until } from "../../harness/api";
import { env } from "../../harness/env";
import { canvasCreate, card, deployStaged, newEmptyProject } from "../../harness/ui";

interface Dom { domain: string; status: string; dns?: { type: string; value: string }[] }

test("U15 custom domain", async ({ page, context }) => {
  await context.grantPermissions(["clipboard-read", "clipboard-write"]).catch(() => undefined);
  const real = process.env.E2E_CUSTOM_DOMAIN || "";
  const name = real || `u15-${Date.now().toString(36)}.example.com`;

  const { eid } = await newEmptyProject(page);
  await canvasCreate(page, "Docker Image");
  await page.getByLabel("Image", { exact: true }).fill(env.whoamiImage);
  await page.getByRole("button", { name: "Add to canvas" }).click();
  await deployStaged(page);
  await card(page, "whoami").click();
  await page.getByRole("tab", { name: "Settings" }).click();

  await page.getByRole("button", { name: "Custom Domain" }).click();
  await page.getByPlaceholder("app.example.com").fill(name);
  await page.getByRole("button", { name: "Add", exact: true }).click();

  const row = page.locator("div.px-3.py-2").filter({ hasText: name });
  await expect(row).toBeVisible();
  if (!real) {
    // The name points nowhere: the records are shown and the status is Waiting for DNS.
    await expect(row.getByText("Waiting for DNS").first()).toBeVisible();
    const copies = row.getByRole("button", { name: "Copy" });
    await expect(copies.nth(2)).toBeVisible(); // host and value of at least one record, plus the URL copy
    const value = await row.locator("tbody tr").first().locator("td").nth(2).innerText();
    await row.locator("tbody tr").first().getByRole("button", { name: "Copy" }).last().click();
    expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(value.trim());
    await expect(row.getByRole("button", { name: "Retry certificate" })).toBeHidden();
  } else {
    await until(async () => {
      const list = await apiGet<Dom[]>(page, `/environments/${eid}/services/${await serviceId(page, eid)}/domains`);
      return list.find((d) => d.domain === name && d.status === "active");
    }, 180_000, 5000);
    await expect(row.getByText("Active")).toBeVisible({ timeout: 30_000 });
  }

  // Delete removes the row.
  await row.getByRole("button", { name: "Delete" }).click();
  await page.getByRole("button", { name: "Delete", exact: true }).last().click();
  await expect(row).toBeHidden();
});

async function serviceId(page: import("@playwright/test").Page, eid: string): Promise<string> {
  const cv = await apiGet<{ services: { id: string; config: { name: string } }[] }>(page, `/environments/${eid}/canvas`);
  return cv.services.find((s) => s.config.name === "whoami")!.id;
}
