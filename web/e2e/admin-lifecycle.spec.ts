import { expect, type Page, test } from "@playwright/test";

test("admin UI creates, grants, revokes, and deletes resources", async ({ page }) => {
  await page.goto(".");
  await expect(page.getByRole("heading", { name: "Overview" })).toBeVisible();

  await page.getByRole("button", { name: "Nodes" }).click();
  await page.getByRole("button", { name: "添加节点" }).click();
  await page.getByLabel("节点名称").fill("edge-ui");
  await page.getByRole("button", { name: "下一步" }).click();
  await expect(page.getByText("接入字符串已生成")).toBeVisible();
  await page.getByRole("button", { name: "关闭" }).click();
  await expectRowVisible(page, "edge-ui");

  await page.getByRole("button", { name: "Users" }).click();
  await page.getByRole("button", { name: "Add User" }).click();
  await page.getByRole("textbox", { name: "Name*" }).fill("alice-ui");
  await page.getByRole("button", { name: "Create User" }).click();
  await expectRowVisible(page, "alice-ui");

  await page.getByRole("button", { name: "Proxies" }).click();
  await page.getByRole("button", { name: "Add Proxy" }).click();
  await page.getByRole("textbox", { name: "Name", exact: true }).fill("vless-ui");
  await page.getByRole("spinbutton", { name: "Port", exact: true }).fill("39091");
  await page.getByRole("button", { name: "Save Proxy" }).click();
  await expect(page.getByText("Proxy created.")).toBeVisible();
  await page.keyboard.press("Escape");
  await expectRowVisible(page, "vless-ui");

  await page.getByRole("button", { name: "Users" }).click();
  await openRowActions(page, "alice-ui");
  await page.getByRole("menuitem", { name: "Edit" }).click();
  await expect(page.getByRole("heading", { name: "User: alice-ui" })).toBeVisible();
  await page.getByRole("button", { name: "Grant Access" }).click();
  await page.getByRole("button", { name: "Grant Access" }).last().click();
  await expectRowVisible(page, "vless-ui@alice-ui");

  await openRowActions(page, "vless-ui@alice-ui");
  await page.getByRole("menuitem", { name: "Revoke" }).click();
  await expect(page.getByRole("heading", { name: "Revoke Access" })).toBeVisible();
  await page.getByRole("button", { name: "Revoke" }).click();
  await expectRowHidden(page, "vless-ui@alice-ui");
  await page.getByLabel("Show revoked").click();
  await expectRowVisible(page, "vless-ui@alice-ui");
  await page.keyboard.press("Escape");

  await page.getByRole("button", { name: "Proxies" }).click();
  await openRowActions(page, "vless-ui");
  await page.getByRole("menuitem", { name: "Delete" }).click();
  await expect(page.getByRole("heading", { name: "Delete Proxy" })).toBeVisible();
  await page.getByRole("button", { name: "Delete" }).click();
  await expectRowHidden(page, "vless-ui");
  await page.getByLabel("Show disabled").click();
  await expectRowVisible(page, "vless-ui");

  await page.getByRole("button", { name: "Users" }).click();
  await openRowActions(page, "alice-ui");
  await page.getByRole("menuitem", { name: "Delete" }).click();
  await expect(page.getByRole("heading", { name: "Delete User" })).toBeVisible();
  await page.getByRole("button", { name: "Delete" }).click();
  await expectRowHidden(page, "alice-ui");
  await page.getByLabel("Show disabled").click();
  await expectRowVisible(page, "alice-ui");

  await page.getByRole("button", { name: "Nodes" }).click();
  await openRowActions(page, "edge-ui");
  await page.getByRole("menuitem", { name: "Delete" }).click();
  await expect(page.getByRole("heading", { name: "Delete Node" })).toBeVisible();
  await page.getByRole("button", { name: "Delete" }).click();
  await expectRowHidden(page, "edge-ui");
  await page.getByLabel("Show disabled").click();
  await expectRowVisible(page, "edge-ui");
});

async function openRowActions(page: Page, rowText: string) {
  const row = page.getByRole("row").filter({ hasText: rowText }).first();
  await row.getByRole("button", { name: new RegExp(`Actions for .*${escapeRegExp(rowText)}`) }).click();
}

async function expectRowVisible(page: Page, rowText: string) {
  await expect(page.getByRole("row").filter({ hasText: rowText }).first()).toBeVisible();
}

async function expectRowHidden(page: Page, rowText: string) {
  await expect(page.getByRole("row").filter({ hasText: rowText })).toHaveCount(0);
}

function escapeRegExp(value: string) {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}
