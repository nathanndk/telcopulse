import { test, expect } from "@playwright/test";

const windowEnd = new Date().toISOString();
const windowStart = new Date(Date.now() - 30 * 24 * 60 * 60 * 1000).toISOString();
const overview = {
  total: 0,
  success: 0,
  failed: 0,
  success_rate: 0,
  p95_ms: 0,
  per_minute: 0,
  series: [],
  purchase_slo: {
    window_start: windowStart,
    window_end: windowEnd,
    target_percent: 99.9,
    total: 0,
    success: 0,
    failed: 0,
    current_percent: null,
    allowed_failures: 0,
    budget_remaining: 0,
    budget_consumed_percent: null,
  },
};

test("live SLO follows the selected environment's durable overview", async ({ page, request }) => {
  for (const environment of ["development", "staging"] as const) {
    const response = await request.get(`/api/v1/overview?environment=${environment}`);
    expect(response.ok()).toBe(true);
    const data = await response.json();
    if (environment === "development") {
      await page.goto("/");
    } else {
      await page.getByRole("combobox", { name: "Environment" }).selectOption(environment);
    }
    const panel = page.locator("section.panel").filter({
      has: page.getByRole("heading", { name: "Synthetic purchase success objective" }),
    });
    await expect(panel.getByText(`${data.purchase_slo.target_percent.toFixed(1)}%`, { exact: true })).toBeVisible();
    if (data.purchase_slo.current_percent === null) {
      await expect(panel.getByText("No completed purchases")).toBeVisible();
    } else {
      await expect(panel.getByText(`${data.purchase_slo.current_percent.toFixed(2)}%`, { exact: true })).toBeVisible();
      await expect(panel.getByText(`${data.purchase_slo.success} of ${data.purchase_slo.total} succeeded`)).toBeVisible();
    }
  }
});

test("purchase SLO distinguishes no traffic from an exhausted error budget", async ({ page }) => {
  let exhausted = false;
  await page.route("**/api/v1/auth/status", (route) =>
    route.fulfill({ status: 200, contentType: "application/json", body: '{"required":false}' }),
  );
  await page.route("**/api/v1/overview?**", (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        ...overview,
        purchase_slo: exhausted
          ? {
              ...overview.purchase_slo,
              total: 1000,
              success: 998,
              failed: 2,
              current_percent: 99.8,
              allowed_failures: 1,
              budget_remaining: 0,
              budget_consumed_percent: 200,
            }
          : overview.purchase_slo,
      }),
    }),
  );
  await page.goto("/");
  const panel = page.locator("section.panel").filter({
    has: page.getByRole("heading", { name: "Synthetic purchase success objective" }),
  });
  await expect(panel.getByText("No completed purchases")).toBeVisible();
  await expect(panel.getByText("No measurement")).toBeVisible();
  exhausted = true;
  await page.reload();
  await expect(panel.getByText("99.80%", { exact: true })).toBeVisible();
  await expect(panel.getByText("200%", { exact: true })).toBeVisible();
  await expect(panel.getByText("2 failed outcomes")).toBeVisible();
  await page.setViewportSize({ width: 390, height: 844 });
  expect(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth)).toBe(false);
});
