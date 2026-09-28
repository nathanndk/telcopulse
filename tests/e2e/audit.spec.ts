import { test, expect } from "@playwright/test";
import { randomUUID } from "node:crypto";

test("audit register shows an incident change with scoped filters and a source link", async ({ page, request }) => {
  const fields = {
    title: `Audit register ${randomUUID().slice(0, 8)}`,
    severity: "SEV-3", owner: "audit-browser", impact: "Synthetic audit verification only",
    environment: "development", service: "payment-service",
  };
  const created = await request.post("/api/v1/incidents", { headers: { "Idempotency-Key": randomUUID() }, data: fields });
  expect(created.status()).toBe(201);
  const incident = await created.json();
  const changed = await request.put(`/api/v1/incidents/${incident.id}`, {
    data: { title: fields.title, severity: fields.severity, owner: fields.owner, impact: fields.impact,
      state: "Acknowledged", expected_version: incident.version, note: "Audit register verification" },
  });
  expect(changed.status()).toBe(200);

  const response = await request.get(`/api/v1/audit?environment=development&source=incident&resource=${encodeURIComponent(incident.id)}&limit=1`);
  expect(response.status()).toBe(200);
  const result = await response.json();
  expect(result.items).toHaveLength(1);
  expect(result.items[0]).toMatchObject({ source: "incident", resource_id: incident.id, action: "updated", changes: [{ field: "state", before: "Detected", after: "Acknowledged" }] });
  expect(result.more).toBe(true);
  const next = await request.get(`/api/v1/audit?environment=development&source=incident&resource=${encodeURIComponent(incident.id)}&limit=1&cursor=${encodeURIComponent(result.next)}`);
  expect(next.status()).toBe(200);
  expect((await next.json()).items[0].action).toBe("created");
  const reused = await request.get(`/api/v1/audit?environment=staging&source=incident&resource=${encodeURIComponent(incident.id)}&limit=1&cursor=${encodeURIComponent(result.next)}`);
  expect(reused.status()).toBe(422);

  await page.goto("/audit");
  await expect(page.getByRole("heading", { name: "Audit Log" })).toBeVisible();
  await page.getByRole("combobox", { name: "Audit source" }).selectOption("incident");
  await page.getByRole("textbox", { name: "Filter audit by resource ID" }).fill(incident.id);
  const row = page.getByRole("row").filter({ has: page.getByRole("link", { name: incident.id }) }).filter({ hasText: "Acknowledged" });
  await expect(row).toBeVisible();
  await expect(row.getByText("Detected → Acknowledged", { exact: false })).toBeVisible();
  await page.getByRole("textbox", { name: "Filter audit by action" }).fill("created");
  await expect(row).toHaveCount(0);
  await expect(page.getByRole("row").filter({ has: page.getByRole("link", { name: incident.id }) })).toBeVisible();
  await page.setViewportSize({ width: 390, height: 844 });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await page.getByRole("link", { name: incident.id }).click();
  await expect(page).toHaveURL(new RegExp(`/incidents/${incident.id}$`));
});

test("audit register reports source failure on mobile", async ({ page }) => {
  await page.route("**/api/v1/audit?**", (route) => route.fulfill({ status: 503, contentType: "application/json", body: '{"error":"operations audit unavailable"}' }));
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/audit");
  await expect(page.getByText("Unable to load this view")).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
});
