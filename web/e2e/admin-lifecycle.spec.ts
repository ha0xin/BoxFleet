import { expect, type Page, test } from "@playwright/test";

test("admin UI creates, grants, revokes, and deletes resources", async ({ page }) => {
  await page.goto(".");
  await expect(page.getByRole("heading", { name: "BoxFleet Admin", exact: true })).toBeVisible();

  await page.getByRole("button", { name: "Nodes", exact: true }).click();
  await page.getByRole("button", { name: "Enroll", exact: true }).click();
  await page.getByLabel("Node name").fill("edge-ui");
  await page.getByLabel("Public host").fill("203.0.113.50");
  await page.getByRole("button", { name: "Generate bootstrap" }).click();
  await expect(page.getByText("Install command", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Done", exact: true }).click();
  await expectRowVisible(page, "edge-ui");

  await page.getByRole("button", { name: "Users", exact: true }).click();
  await page.getByRole("button", { name: "Create", exact: true }).click();
  await page.getByLabel("Name", { exact: true }).fill("alice-ui");
  await page.getByRole("button", { name: "Create user" }).click();
  await expectRowVisible(page, "alice-ui");

  await page.getByRole("button", { name: "Proxies", exact: true }).click();
  await page.getByRole("button", { name: "Create", exact: true }).click();
  await page.getByLabel("Name", { exact: true }).fill("vless-ui");
  await page.getByLabel("Listen port", { exact: true }).fill("39091");
  await page.getByRole("button", { name: "Create proxy" }).click();
  await expectRowVisible(page, "vless-ui");

  await page.getByRole("button", { name: "Users", exact: true }).click();
  await openRowActions(page, "alice-ui");
  await page.getByRole("menuitem", { name: "Manage access" }).click();
  await expect(page.getByRole("heading", { name: "Manage access" })).toBeVisible();
  await page.getByRole("checkbox", { name: "edge-ui / vless-ui" }).check();
  await page.getByRole("button", { name: "Grant access (1)" }).click();
  await expect(page.getByRole("button", { name: "Revoke vless-ui" })).toBeVisible();
  await page.getByRole("button", { name: "Revoke vless-ui" }).click();
  await expect(page.getByRole("button", { name: "Revoke vless-ui" })).toHaveCount(0);
  await page.getByRole("button", { name: "Done", exact: true }).click();

  await page.getByRole("button", { name: "Proxies", exact: true }).click();
  await openRowActions(page, "vless-ui");
  await page.getByRole("menuitem", { name: "Delete" }).click();
  await expect(page.getByRole("heading", { name: "Delete proxy" })).toBeVisible();
  await page.getByRole("button", { name: "Delete", exact: true }).click();
  await expectRowHidden(page, "vless-ui");
  await selectDeletedFilter(page);
  await expectRowVisible(page, "vless-ui");

  await page.getByRole("button", { name: "Users", exact: true }).click();
  await openRowActions(page, "alice-ui");
  await page.getByRole("menuitem", { name: "Delete" }).click();
  await expect(page.getByRole("heading", { name: "Delete user" })).toBeVisible();
  await page.getByRole("button", { name: "Delete", exact: true }).click();
  await expectRowHidden(page, "alice-ui");
  await selectDeletedFilter(page);
  await expectRowVisible(page, "alice-ui");

  await page.getByRole("button", { name: "Nodes", exact: true }).click();
  await openRowActions(page, "edge-ui");
  await page.getByRole("menuitem", { name: "Delete" }).click();
  await expect(page.getByRole("heading", { name: "Delete node" })).toBeVisible();
  await page.getByRole("button", { name: "Delete", exact: true }).click();
  await expectRowHidden(page, "edge-ui");
  await selectDeletedFilter(page);
  await expectRowVisible(page, "edge-ui");
});

async function selectDeletedFilter(page: Page) {
  await page.getByRole("button", { name: "Filter", exact: true }).click();
  await page.getByRole("menuitemradio", { name: "Deleted", exact: true }).click();
  await page.keyboard.press("Escape");
}

async function openRowActions(page: Page, rowText: string) {
  const row = page.getByRole("row").filter({ hasText: rowText });
  await expect(row).toHaveCount(1);
  await row.getByRole("button", { name: `Actions for ${rowText}` }).click();
}

async function expectRowVisible(page: Page, rowText: string) {
  const row = page.getByRole("row").filter({ hasText: rowText });
  await expect(row).toHaveCount(1);
  await expect(row).toBeVisible();
}

async function expectRowHidden(page: Page, rowText: string) {
  await expect(page.getByRole("row").filter({ hasText: rowText })).toHaveCount(0);
}
