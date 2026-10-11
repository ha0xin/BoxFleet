import { expect, test } from "@playwright/test";

for (const failure of ["gateway", "network"] as const) {
  test(`node inventory summarizes ${failure} failures and recovers on retry`, async ({ page }, testInfo) => {
    await page.setViewportSize({ width: 589, height: 824 });
    let unavailable = true;
    await page.route("**/api/admin/nodes?*", route => {
      if (!unavailable) return route.fulfill({ json: { nodes: [], total: 0, limit: 50, offset: 0 } });
      if (failure === "network") return route.abort("connectionfailed");
      return route.fulfill({ status: 502, contentType: "text/html", body: "<!DOCTYPE html><html><body><h1>Cloudflare 502 Bad Gateway</h1></body></html>" });
    });
    await page.goto("nodes");
    const alert = page.getByRole("alert").filter({ hasText: failure === "gateway" ? "temporarily unavailable" : "Unable to reach the server" });
    await expect(alert).toBeVisible();
    await expect(page.getByText("<!DOCTYPE html>", { exact: false })).toHaveCount(0);
    await expect(page.getByRole("button", { name: "Try again", exact: true })).toBeVisible();
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    await page.screenshot({ path: testInfo.outputPath(`node-${failure}-error.png`) });
    unavailable = false;
    await page.getByRole("button", { name: "Try again", exact: true }).click();
    await expect(page.getByText("No nodes match this filter.", { exact: true })).toBeVisible();
    await expect(alert).toHaveCount(0);
  });
}
