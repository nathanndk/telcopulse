import { test, expect } from "@playwright/test";

test("service health shows live shared-runtime telemetry and recovers from outage", async ({
  page,
  request,
}) => {
  const response = await request.get("/api/v1/services/health");
  expect(response.ok()).toBeTruthy();
  const snapshot = await response.json();
  expect(snapshot.scope).toBe("shared-runtime");
  expect(snapshot.items).toHaveLength(7);
  const auth = snapshot.items.find((item: { id: string }) => item.id === "auth-service");
  expect(auth.up).toBe(1);
  expect(auth.status).not.toBe("Critical");
  expect(
    snapshot.items.find((item: { id: string }) => item.id === "payment-service")
      .up,
  ).toBe(1);
  await page.goto("/services");
  await expect(
    page.getByText("Shared runtime · Both transaction environments", {
      exact: false,
    }),
  ).toBeVisible();
  const payment = page.getByRole("row").filter({ hasText: "payment-service" });
  await expect(payment).toBeVisible();
  await expect(
    page.getByRole("columnheader", { name: "HTTP 5xx" }),
  ).toBeVisible();
  await page.route("**/api/v1/services/health", (route) =>
    route.fulfill({
      status: 503,
      contentType: "application/json",
      body: JSON.stringify({ error: "service telemetry unavailable" }),
    }),
  );
  await page.reload();
  await expect(
    page.getByText("service telemetry unavailable", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("row").filter({ hasText: "payment-service" }),
  ).toHaveCount(0);
  await page.unroute("**/api/v1/services/health");
  await page.getByRole("button", { name: "Try again", exact: true }).click();
  await expect(payment).toBeVisible();
  await page.goto("/");
  const health = page.locator(".health-panel");
  await expect(health.getByRole("columnheader", { name: "P95" })).toBeVisible();
  const table = health.locator('[data-slot="table-container"]');
  expect(
    await table.evaluate(
      (element) => element.scrollWidth <= element.clientWidth,
    ),
  ).toBeTruthy();
  await page.screenshot({
    path: "/private/tmp/telcopulse-live-overview.png",
    fullPage: true,
  });
  await page.goto("/services");
  await expect(payment).toBeVisible();
  await page.setViewportSize({ width: 390, height: 844 });
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBeTruthy();
  await page.screenshot({
    path: "/private/tmp/telcopulse-service-health.png",
    fullPage: true,
  });
});
