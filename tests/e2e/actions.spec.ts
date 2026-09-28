import { test, expect } from "@playwright/test";
import { randomUUID } from "node:crypto";

test("corrective actions are filterable across incidents and link to audited editing", async ({ page, request }) => {
  const token = randomUUID().slice(0, 8);
  const incidentTitle = `Action register ${token}`;
  const openTitle = `Repair purchase capacity ${token}`;
  const completedTitle = `Document recovery ${token}`;
  const created = await request.post("/api/v1/incidents", {
    headers: { "Idempotency-Key": randomUUID() },
    data: {
      title: incidentTitle,
      severity: "SEV-3",
      owner: "register-test",
      impact: "Synthetic register verification only",
      environment: "development",
      service: "payment-service",
      action_items: [
        { title: openTitle, owner: "platform", priority: "P1", status: "Open", done: false, due_at: new Date(Date.now() - 86_400_000).toISOString() },
        { title: completedTitle, owner: "network", priority: "P2", status: "Completed", done: true },
      ],
    },
  });
  expect(created.status()).toBe(201);
  const incident = await created.json();
  const api = await request.get(`/api/v1/incidents/actions?environment=development&status=all&search=${encodeURIComponent(token)}&limit=25`);
  expect(api.status()).toBe(200);
  expect((await api.json()).items.map((item: { title: string }) => item.title)).toEqual([openTitle, completedTitle]);
  const firstPage = await request.get(`/api/v1/incidents/actions?environment=development&status=all&search=${encodeURIComponent(token)}&limit=1`);
  const firstResult = await firstPage.json();
  expect(firstResult.items[0].title).toBe(openTitle);
  expect(firstResult.more).toBe(true);
  const secondPage = await request.get(`/api/v1/incidents/actions?environment=development&status=all&search=${encodeURIComponent(token)}&limit=1&cursor=${encodeURIComponent(firstResult.next)}`);
  expect((await secondPage.json()).items[0].title).toBe(completedTitle);

  await page.goto("/rca");
  await expect(page.getByRole("heading", { name: "RCA & Actions" })).toBeVisible();
  await page.getByRole("textbox", { name: "Search actions or incidents" }).fill(token);
  await expect(page.getByRole("link", { name: openTitle, exact: true })).toBeVisible();
  await expect(page.getByText(completedTitle)).toHaveCount(0);
  const openRow = page.getByRole("row").filter({ has: page.getByRole("link", { name: openTitle, exact: true }) });
  await expect(openRow.getByText("Overdue")).toBeVisible();
  await page.getByRole("combobox", { name: "Action status" }).selectOption("all");
  await expect(page.getByRole("link", { name: completedTitle, exact: true })).toBeVisible();
  await page.getByRole("combobox", { name: "Action priority" }).selectOption("P1");
  await expect(page.getByText(completedTitle)).toHaveCount(0);
  await page.getByRole("combobox", { name: "Action priority" }).selectOption("");
  await page.getByRole("textbox", { name: "Filter actions by owner" }).fill("network");
  await expect(page.getByRole("link", { name: completedTitle, exact: true })).toBeVisible();
  await expect(page.getByText(openTitle)).toHaveCount(0);
  await page.setViewportSize({ width: 390, height: 844 });
  expect(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth)).toBe(false);
  await page.getByRole("link", { name: completedTitle, exact: true }).click();
  await expect(page).toHaveURL(new RegExp(`/incidents/${incident.id}#action-items$`));
  await expect(page.getByRole("heading", { name: "Follow-up actions" })).toBeVisible();
});

test("corrective-action register shows source failure without horizontal page overflow", async ({ page }) => {
  await page.route("**/api/v1/incidents/actions?**", (route) => route.fulfill({
    status: 503,
    contentType: "application/json",
    body: '{"error":"incident service unavailable"}',
  }));
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/rca");
  await expect(page.getByText("Unable to load this view")).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth)).toBe(false);
});
