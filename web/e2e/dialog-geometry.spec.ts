import { expect, type Page, test } from "@playwright/test";

// Kumo >= 2.7 makes `<Dialog size>` a fixed width instead of a min-width floor,
// so a dialog no longer grows to fit a wide row — it clips or overflows. These
// assertions pin the widths and prove no content overflows its dialog.
// Totals are border-box and include the 48px of `p-6`.
const SIZE_WIDTH = { sm: 288, base: 384, lg: 512, xl: 768 } as const;

async function dialogGeometry(page: Page) {
  const dialog = page.locator('[role="dialog"], [role="alertdialog"]').last();
  await expect(dialog).toBeVisible();
  return await dialog.evaluate((el) => {
    const overflowing: string[] = [];
    el.querySelectorAll("*").forEach((node) => {
      const child = node as HTMLElement;
      // Visually-hidden inputs (Switch/Checkbox) are 1px wide by design, and
      // `truncate` spans report scrollWidth > clientWidth as their ellipsis.
      if (child.clientWidth <= 1) return;
      const cs = getComputedStyle(child);
      if (cs.overflowX === "auto" || cs.overflowX === "scroll") return;
      if (cs.textOverflow === "ellipsis") return;
      if (child.scrollWidth - child.clientWidth > 1) {
        overflowing.push(`${child.tagName.toLowerCase()}.${child.className.toString().slice(0, 60)} ${child.clientWidth}<${child.scrollWidth}`);
      }
    });
    return { width: Math.round(el.getBoundingClientRect().width), overflowing };
  });
}

async function openRowActions(page: Page, name: string) {
  await page.getByRole("row", { name: new RegExp(name) }).getByRole("button", { name: /actions/i }).click();
}

test("dialogs honour their size and never overflow their content", async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });

  await page.goto("nodes");
  await expect(page.getByRole("heading", { name: "Nodes", exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Enroll", exact: true }).click();
  const enroll = await dialogGeometry(page);
  expect(enroll.width).toBe(SIZE_WIDTH.lg);
  expect(enroll.overflowing).toEqual([]);

  await page.getByLabel("Node name").fill("geometry-node");
  await page.getByLabel("Public host").fill("203.0.113.50");
  await page.getByRole("button", { name: "Generate bootstrap" }).click();
  await expect(page.getByText("Install command", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Done", exact: true }).click();

  // EditNodeDialog is the tightest layout in the app: a four-column hosts
  // repeater. At `base` its host input collapsed to 87px and overflowed.
  await openRowActions(page, "geometry-node");
  await page.getByRole("menuitem", { name: "Edit" }).click();
  await expect(page.getByRole("heading", { name: /Edit geometry-node/ })).toBeVisible();
  const edit = await dialogGeometry(page);
  expect(edit.width).toBe(SIZE_WIDTH.lg);
  expect(edit.overflowing).toEqual([]);
  // The host field must stay wide enough for a real hostname (measured 185px).
  const hostWidth = await page.locator('input[name="hosts.0.host"]').evaluate((el) => el.getBoundingClientRect().width);
  expect(hostWidth).toBeGreaterThan(190);
  await page.keyboard.press("Escape");

  // SoftDeleteDialog is the canonical confirm, reused across pages.
  await openRowActions(page, "geometry-node");
  await page.getByRole("menuitem", { name: "Delete", exact: true }).click();
  const confirm = await dialogGeometry(page);
  expect(confirm.width).toBe(SIZE_WIDTH.sm);
  expect(confirm.overflowing).toEqual([]);
  await page.keyboard.press("Escape");

  // Both `grid-cols-2` forms: two labelled fields need `lg`, not `base`.
  await page.goto("proxies");
  await page.getByRole("button", { name: "Create", exact: true }).click();
  await expect(page.getByLabel("Listen port", { exact: true })).toBeVisible();
  const proxy = await dialogGeometry(page);
  expect(proxy.width).toBe(SIZE_WIDTH.lg);
  expect(proxy.overflowing).toEqual([]);
  await page.keyboard.press("Escape");

  await page.goto("users");
  await page.getByRole("button", { name: "Create", exact: true }).click();
  await expect(page.getByLabel("Name", { exact: true })).toBeVisible();
  const user = await dialogGeometry(page);
  expect(user.width).toBe(SIZE_WIDTH.lg);
  expect(user.overflowing).toEqual([]);
});
