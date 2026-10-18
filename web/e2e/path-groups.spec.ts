import { expect, test } from "@playwright/test";

test("Path shortcuts share tri-state selection and preserve individual permissions", async ({ page }, testInfo) => {
  async function post(url: string, data: unknown) {
    const response = await page.request.post(url, { data });
    expect(response.ok(), await response.text()).toBeTruthy();
    return response.json();
  }
  const node = await post("/api/admin/nodes", { name: "groups-ui-node", public_host: "192.0.2.74" });
  const user = await post("/api/admin/users", { name: "groups-ui-alice" });
  for (const [index, name] of ["groups-ui-a", "groups-ui-b", "groups-ui-c"].entries()) {
    await post(`/api/admin/nodes/${node.id}/proxies`, { name, protocol: "vless_reality", listen_port: 45000 + index, enabled: true });
  }
  const paths = await (await page.request.get("/api/admin/paths")).json();
  const [a, b, c] = ["groups-ui-a", "groups-ui-b", "groups-ui-c"].map(name => paths.find((p: {proxy_name: string}) => p.proxy_name === name));
  const label = (p: typeof a) => p.display_name || `${p.proxy_name} · ${p.name}`;
  async function selected() {
    const rows = await (await page.request.get(`/api/admin/users/${user.id}/paths`)).json();
    return rows.map((r: {path_id: string}) => r.path_id).sort();
  }
  await page.goto("paths?tab=groups");
  await page.getByRole("button", { name: "Create group", exact: true }).click();
  await page.getByLabel("Group name", { exact: true }).fill("Alpha group");
  for (const p of [a,b]) await page.getByRole("checkbox", { name: label(p), exact: true }).check();
  await page.getByRole("button", { name: "Create group", exact: true }).last().click();
  await expect(page.getByRole("dialog")).toHaveCount(0);
  const change = {operation:"save",name:"Beta group",enabled:true,path_ids:[b.id,c.id]};
  const preview = await post("/api/admin/path-groups/preview", change);
  await post("/api/admin/path-groups/changes", {...change,preview_token:preview.preview_token});

  await page.goto("users");
  await page.getByRole("row", { name: /groups-ui-alice/ }).getByRole("button", { name: /actions/i }).click();
  await page.getByRole("menuitem", { name: "Manage access", exact: true }).click();
  const alpha = page.getByRole("checkbox", {name:"Alpha group",exact:true});
  const beta = page.getByRole("checkbox", {name:"Beta group",exact:true});
  const save = page.getByRole("button", {name:"Save",exact:true});
  const cancel = page.getByRole("button", {name:"Cancel",exact:true});
  let writes=0;
  page.on("request", request => { if(request.method()==="PATCH" && request.url().endsWith(`/users/${user.id}/paths`)) writes++; });
  await expect(save).toBeDisabled();
  await alpha.check();
  await expect(beta).toHaveAttribute("aria-checked","mixed");
  await page.getByRole("button",{name:"Expand Alpha group",exact:true}).click();
  await page.getByRole("button",{name:"Expand Beta group",exact:true}).click();
  const alphaPaths = page.getByRole("group",{name:"Alpha group Paths",exact:true});
  const betaPaths = page.getByRole("group",{name:"Beta group Paths",exact:true});
  await alphaPaths.getByRole("checkbox",{name:label(b),exact:true}).uncheck();
  await expect(alpha).toHaveAttribute("aria-checked","mixed");
  await expect(beta).toHaveAttribute("aria-checked","false");
  await expect(betaPaths.getByRole("checkbox",{name:label(b),exact:true})).not.toBeChecked();
  await beta.check(); await expect(alpha).toBeChecked();
  await beta.uncheck(); await expect(alpha).toHaveAttribute("aria-checked","mixed");
  await alpha.click(); // Mixed → all.
  await expect(alpha).toBeChecked();
  await expect(beta).toHaveAttribute("aria-checked","mixed");
  expect(writes).toBe(0);expect(await selected()).toEqual([]);
  await page.keyboard.press("Escape");
  await expect(page.getByRole("dialog")).toHaveCount(1);

  // Save failure retains the draft for retry; Cancel discards it.
  await page.route(`**/api/admin/users/${user.id}/paths`, route => route.request().method() === "PATCH"
    ? route.fulfill({status:422,contentType:"text/plain",body:"Batch rejected"}) : route.continue());
  await save.click();
  await expect(page.getByText("Batch rejected",{exact:false})).toBeVisible();
  await expect(save).toBeEnabled();await expect(alpha).toBeChecked();
  expect(await selected()).toEqual([]);
  await page.unroute(`**/api/admin/users/${user.id}/paths`);
  await cancel.click();await expect(page.getByRole("dialog")).toHaveCount(0);
  expect(await selected()).toEqual([]);

  await page.getByRole("row", { name: /groups-ui-alice/ }).getByRole("button", { name: /actions/i }).click();
  await page.getByRole("menuitem", { name: "Manage access", exact: true }).click();
  await expect(alpha).not.toBeChecked();await expect(save).toBeDisabled();
  await alpha.check();
  await page.getByRole("button",{name:"Expand Alpha group",exact:true}).click();
  for (const width of [698,390,1440]) {
    await page.setViewportSize({width,height:824});
    const geometry = await page.getByRole("dialog").evaluate(el => {
      const rect=el.getBoundingClientRect(); return {left:rect.left,right:rect.right,overflow:el.scrollWidth-el.clientWidth};
    });
    expect(geometry.left).toBeGreaterThanOrEqual(0);expect(geometry.right).toBeLessThanOrEqual(width);expect(geometry.overflow).toBe(0);
    await page.screenshot({path:testInfo.outputPath(`path-shortcuts-${width}.png`)});
  }
  await save.click();await expect(page.getByRole("dialog")).toHaveCount(0);
  expect(await selected()).toEqual([a.id,b.id].sort());
  expect(writes).toBe(2); // One rejected save, one successful save.
  const beforeEdit=await selected();
  await page.goto("paths?tab=groups");
  await page.getByRole("button",{name:"Alpha group",exact:true}).click();
  await page.getByRole("checkbox",{name:label(a),exact:true}).uncheck();
  await page.getByRole("checkbox",{name:label(c),exact:true}).check();
  await page.getByRole("button",{name:"Save changes",exact:true}).click();
  await expect(page.getByRole("dialog")).toHaveCount(0);
  expect(await selected()).toEqual(beforeEdit);
  await page.getByRole("button",{name:"Actions for Alpha group",exact:true}).click();
  await page.getByRole("menuitem",{name:"Archive group",exact:true}).click();
  await expect(page.getByRole("button",{name:"Alpha group",exact:true})).toHaveCount(0);
  expect(await selected()).toEqual(beforeEdit);
});
