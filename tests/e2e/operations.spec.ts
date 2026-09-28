import { test, expect } from "@playwright/test";

test("operational response matches live incident and deployment aggregates", async ({ page, request }) => {
  for (const environment of ["development", "staging"] as const) {
    const incidentResponse = await request.get(`/api/v1/incidents/operations?environment=${environment}`);
    const deploymentResponse = await request.get(`/api/v1/deployments/operations?environment=${environment}`);
    expect(incidentResponse.ok()).toBe(true);
    expect(deploymentResponse.ok()).toBe(true);
    const incidents = await incidentResponse.json();
    const deployments = await deploymentResponse.json();
    if (environment === "development") {
      await page.goto("/");
    } else {
      await page.getByRole("combobox", { name: "Environment" }).selectOption(environment);
    }
    const panel = page.locator("section.panel").filter({
      has: page.getByRole("heading", { name: "Operational response" }),
    });
    await expect(panel.getByText("Incidents", { exact: true }).locator("..").locator("dd")).toHaveText(String(incidents.incident_count));
    await expect(panel.getByText("SEV-1", { exact: true }).locator("..").locator("dd")).toHaveText(String(incidents.sev1_count));
    await expect(panel.getByText("Deployment failure rate", { exact: true }).locator("..").locator("dd")).toHaveText(
      deployments.failure_rate_percent === null ? "—" : `${deployments.failure_rate_percent.toFixed(1)}%`,
    );
  }
});

test("operational metrics preserve no-sample and partial-failure states", async ({ page }) => {
  const windowEnd = new Date().toISOString();
  const windowStart = new Date(Date.now() - 30 * 24 * 60 * 60 * 1000).toISOString();
  await page.route("**/api/v1/incidents/operations?**", (route) =>
    route.fulfill({ status: 503, contentType: "application/json", body: '{"error":"incident service unavailable"}' }),
  );
  await page.route("**/api/v1/deployments/operations?**", (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ window_start: windowStart, window_end: windowEnd, evaluated: 0, failed: 0, failure_rate_percent: null }),
    }),
  );
  await page.goto("/");
  const panel = page.locator("section.panel").filter({
    has: page.getByRole("heading", { name: "Operational response" }),
  });
  await expect(panel.getByText("Incidents", { exact: true }).locator("..").locator("dd")).toHaveText("—");
  await expect(panel.getByText("MTTA", { exact: true }).locator("..").locator("dd")).toHaveText("—");
  await expect(panel.getByText("Deployment failure rate", { exact: true }).locator("..").locator("dd")).toHaveText("—");
  await expect(panel.getByText("0 failed / 0 evaluated")).toBeVisible();
  await expect(panel.getByText("incident service unavailable")).toBeVisible();
  await page.setViewportSize({ width: 390, height: 844 });
  expect(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth)).toBe(false);
});
