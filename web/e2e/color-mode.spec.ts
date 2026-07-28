import { expect, test } from "@playwright/test";

test("theme toggle stays in the top-right and persists the selected mode", async ({ page }) => {
  await page.goto("");

  const toggle = page.getByRole("button", { name: "Switch to dark mode" });
  await expect(toggle).toBeVisible();
  await expect(page.locator("html")).toHaveAttribute("data-mode", "light");

  const desktopBox = await toggle.boundingBox();
  expect(desktopBox).not.toBeNull();
  expect((desktopBox?.x ?? 0) + (desktopBox?.width ?? 0)).toBeGreaterThan(1200);

  await toggle.click();
  await expect(page.locator("html")).toHaveAttribute("data-mode", "dark");
  await expect(page.getByRole("button", { name: "Switch to light mode" })).toBeVisible();

  await page.reload();
  await expect(page.locator("html")).toHaveAttribute("data-mode", "dark");

  await page.setViewportSize({ width: 375, height: 812 });
  const mobileToggle = page.getByRole("button", { name: "Switch to light mode" });
  const mobileBox = await mobileToggle.boundingBox();
  expect(mobileBox).not.toBeNull();
  expect((mobileBox?.x ?? 0) + (mobileBox?.width ?? 0)).toBeLessThanOrEqual(375);
});
