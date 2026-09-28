import { test, expect } from "@playwright/test";
import { randomUUID } from "node:crypto";

test("workspace search opens a persisted incident and respects environment", async ({ page, request }) => {
  const marker = `Search ${randomUUID().slice(0, 8)}`;
  const create = async (environment: "development" | "staging") => {
    const response = await request.post("/api/v1/incidents", {
      headers: { "Idempotency-Key": randomUUID() },
      data: { title: marker, severity: "SEV-4", environment, service: "payment-service", owner: "search-test", impact: "Synthetic workspace search verification" },
    });
    expect(response.status()).toBe(201);
    return response.json();
  };
  const development = await create("development");
  const staging = await create("staging");

  const api = await request.get(`/api/v1/search?environment=development&q=${encodeURIComponent(marker)}`);
  expect(api.status()).toBe(200);
  const results = await api.json();
  expect(results.items.map((item: { resource_id: string }) => item.resource_id)).toContain(development.id);
  expect(results.items.map((item: { resource_id: string }) => item.resource_id)).not.toContain(staging.id);

  await page.goto("/");
  await page.getByRole("button", { name: /Search the workspace/ }).click();
  await page.getByRole("textbox", { name: "Search workspace" }).fill(marker);
  const result = page.locator(".workspace-result").filter({ hasText: marker });
  await expect(result).toBeVisible();
  await expect(result).toContainText("incident");
  await page.getByRole("textbox", { name: "Search workspace" }).press("Enter");
  await expect(page).toHaveURL(new RegExp(`/incidents/${development.id}$`));

  await page.getByRole("combobox", { name: "Environment" }).selectOption("staging");
  await page.getByRole("button", { name: /Search the workspace/ }).click();
  await page.getByRole("textbox", { name: "Search workspace" }).fill(marker);
  await expect(page.locator(".workspace-result").filter({ hasText: marker })).toBeVisible();
  await expect(page.locator(".workspace-result").filter({ hasText: development.id })).toHaveCount(0);
  await page.setViewportSize({ width: 390, height: 844 });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await page.locator(".workspace-result").filter({ hasText: marker }).click();
  await expect(page).toHaveURL(new RegExp(`/incidents/${staging.id}$`));
});

test("workspace search reports empty and failure states", async ({ page }) => {
  await page.route("**/api/v1/search?**", (route) => route.fulfill({ contentType: "application/json", body: '{"items":[]}' }));
  await page.goto("/");
  await page.getByRole("button", { name: /Search the workspace/ }).click();
  await page.getByRole("textbox", { name: "Search workspace" }).fill("no-such-record-12345");
  await expect(page.getByText("No matching records in development.")).toBeVisible();
  await page.unroute("**/api/v1/search?**");
  await page.route("**/api/v1/search?**", (route) => route.fulfill({ status: 503, contentType: "application/json", body: '{"error":"workspace search unavailable"}' }));
  await page.getByRole("textbox", { name: "Search workspace" }).fill("another-unavailable-query");
  await expect(page.getByText("Search is unavailable. Try again.")).toBeVisible();
  await expect(page.getByRole("button", { name: "Retry search" })).toBeVisible();
});
