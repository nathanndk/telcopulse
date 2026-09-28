import { test, expect } from "@playwright/test";
test("successful purchase can be investigated and found in the explorer", async ({
  page,
}) => {
  await page.goto("/simulator");
  await expect(page.getByLabel("Customer / MSISDN")).toBeVisible();
  await page
    .getByLabel("Payment method", { exact: true })
    .selectOption("E-Wallet");
  await page
    .getByRole("button", { name: "Start synthetic transaction" })
    .click();
  await expect(
    page.getByText("Package activation completed", { exact: true }),
  ).toBeVisible();
  await page
    .getByRole("link", { name: "Investigate transaction", exact: true })
    .click();
  await expect(
    page.getByRole("heading", { name: "Purchase completed" }),
  ).toBeVisible();
  await expect(page.getByRole("heading", { name: "Synthetic notification" })).toBeVisible();
  await expect(page.getByText("Delivered", { exact: true })).toBeVisible({ timeout: 15000 });
  await expect(
    page.getByRole("heading", { name: "Correlated evidence" }),
  ).toBeVisible();
  await expect(page.getByText("62812*****123", { exact: true })).toBeVisible();
  await expect(page.locator("main")).not.toContainText("628123450123");
  const id = page.url().split("/").pop()!;
  await page.goto(`/transactions?search=${id}`);
  await expect(
    page.getByRole("link", { name: `Investigate ${id}` }),
  ).toBeVisible();
});
test("insufficient balance is a business failure with no activation stage", async ({
  page,
}) => {
  await page.goto("/simulator");
  await page.getByLabel("Customer / MSISDN").selectOption("cus-003");
  await page.getByLabel("Internet package").selectOption("pkg-25");
  await page
    .getByLabel("Payment method", { exact: true })
    .selectOption("Pulsa");
  await page
    .getByRole("button", { name: "Start synthetic transaction" })
    .click();
  await expect(
    page.getByText("Purchase failed", { exact: true }),
  ).toBeVisible();
  await page
    .getByRole("link", { name: "Investigate transaction", exact: true })
    .click();
  await expect(
    page.getByRole("heading", { name: "Purchase investigation" }),
  ).toBeVisible();
  await expect(
    page.getByText("INSUFFICIENT_BALANCE", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByText("Activate data entitlement", { exact: true }),
  ).toHaveCount(0);
});

test("a failed simulator purchase can be replayed as a linked new attempt", async ({ page, request }) => {
  await page.goto("/simulator");
  await page.getByLabel("Customer / MSISDN").selectOption("cus-003");
  await page.getByLabel("Internet package").selectOption("pkg-25");
  await page.getByLabel("Payment method", { exact: true }).selectOption("Pulsa");
  const firstResponse = page.waitForResponse(response => response.url().endsWith("/api/v1/transactions") && response.request().method() === "POST");
  await page.getByRole("button", { name: "Start synthetic transaction" }).click();
  const original = await (await firstResponse).json();
  expect(original.status).toBe("FAILED");
  await page.getByRole("button", { name: "Replay failed transaction" }).click();
  const dialog = page.getByRole("dialog");
  await expect(dialog.getByText("The failed transaction remains unchanged", { exact: false })).toBeVisible();
  const replayResponse = page.waitForResponse(response => response.url().endsWith("/api/v1/transactions") && response.request().method() === "POST");
  await dialog.getByRole("button", { name: "Start linked replay" }).click();
  const response = await replayResponse;
  expect(response.status()).toBe(201);
  const replay = await response.json();
  expect(replay.status).toBe("FAILED");
  expect(replay.id).not.toBe(original.id);
  expect(replay.trace_id).not.toBe(original.trace_id);
  expect(replay.replay_of).toBe(original.id);
  await expect(page).toHaveURL(new RegExp(`/transactions/${replay.id}$`));
  await expect(page.getByRole("link", { name: original.id })).toBeVisible();
  await expect(page.getByRole("button", { name: "Replay failed transaction" })).toBeVisible();
  const unchanged = await request.get(`/api/v1/transactions/${original.id}`);
  expect(unchanged.ok()).toBeTruthy();
  expect((await unchanged.json()).status).toBe("FAILED");
});
test("command navigation and mobile layout work", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/");
  await page.getByRole("button", { name: "Open navigation" }).click();
  await page
    .getByRole("button", { name: "Customer Simulator", exact: true })
    .click();
  await expect(
    page.getByRole("heading", { name: "Customer Simulator", exact: true }),
  ).toBeVisible();
  await expect(page.getByLabel("Customer / MSISDN")).toBeVisible();
  const overflow = await page.evaluate(
    () => document.documentElement.scrollWidth > window.innerWidth,
  );
  expect(overflow).toBe(false);
});
test("API failure offers recovery instead of fabricated data", async ({
  page,
}) => {
  await page.route("**/api/v1/overview*", (route) =>
    route.fulfill({
      status: 503,
      contentType: "application/json",
      body: JSON.stringify({ error: "database unavailable" }),
    }),
  );
  await page.goto("/");
  await expect(page.getByText("Unable to load this view")).toBeVisible();
  await expect(page.getByRole("button", { name: "Try again" })).toBeVisible();
});
