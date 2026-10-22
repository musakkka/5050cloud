// SPDX-License-Identifier: AGPL-3.0-only

// labels.spec (SPEC 13.8): Railway's labels from labels.json must appear in
// the rendered console. Each row maps to the screens that show it; rows of
// screens other packages own are added to SCREENS by those packages.
import { readFileSync } from "node:fs";
import { expect, test, type Page } from "@playwright/test";
import { newEmptyProject } from "../../harness/ui";

interface Row { id: string; ui: boolean; labels: { text: string }[] }
const rows: Row[] = JSON.parse(readFileSync(new URL("../../labels.json", import.meta.url), "utf8")).rows;

/** Visible text plus accessible names and titles of everything on the page. */
async function strings(page: Page): Promise<string> {
  return page.evaluate(() => {
    const out = [document.body.innerText];
    document.querySelectorAll("[aria-label],[title],option").forEach((el) => out.push(el.getAttribute("aria-label") || "", el.getAttribute("title") || "", (el as HTMLElement).innerText || ""));
    return out.join("\n");
  });
}

/** How to reach the screen that shows each row's labels. Dynamic labels ("N changes") are skipped. */
const SCREENS: Record<string, { skip?: string[]; open: (page: Page) => Promise<void> }> = {
  D1: { open: async (p) => { await p.goto("/"); await p.getByRole("heading", { name: "Projects" }).waitFor(); } },
  D2: { open: async (p) => { await p.goto("/new"); } },
  D5: { open: async (p) => { await newEmptyProject(p); await p.getByRole("button", { name: "+ Create" }).click(); } },
  D35: { open: async (p) => { await p.goto("/"); } },
};

for (const row of rows.filter((r) => r.ui && r.labels.length)) {
  const s = SCREENS[row.id];
  test(`labels ${row.id}`, async ({ page }) => {
    test.skip(!s, `row ${row.id} is not mapped to a screen yet`);
    await s!.open(page);
    const text = await strings(page);
    for (const l of row.labels) {
      if (s!.skip?.includes(l.text) || /^N /.test(l.text)) continue;
      expect.soft(text, `label "${l.text}" of ${row.id}`).toContain(l.text);
    }
  });
}
