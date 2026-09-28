import { test, expect } from "@playwright/test";
import { randomUUID } from "node:crypto";

test("operator can inspect a reported rollback and filter deployment activity", async ({ page }) => {
  const record = {
    id: "ci-payment-example-0001",
    service: "payment-service",
    version: "1.4.0",
    commit_sha: "abcdef0123456789abcdef0123456789abcdef01",
    environment: "staging",
    deployer: "ci:jenkins",
    status: "Rolled Back",
    occurred_at: "2026-09-27T11:50:00Z",
  };
  await page.route("**/api/v1/deployments?**", (route) => route.fulfill({
    contentType: "application/json",
    body: JSON.stringify([record]),
  }));
  await page.route("**/api/v1/deployments/ci-payment-example-0001", (route) => route.fulfill({
    contentType: "application/json",
    body: JSON.stringify({ ...record, events: [
      { event_id: "event-pending", actor: "ci:jenkins", status: "Pending", occurred_at: "2026-09-27T11:40:00Z" },
      { event_id: "event-completed", actor: "ci:gitlab", status: "Completed", occurred_at: "2026-09-27T11:45:00Z" },
      { event_id: "event-rollback", actor: "operator:rollback", status: "Rolled Back", occurred_at: "2026-09-27T11:50:00Z" },
    ] }),
  }));
  await page.goto("/deployments");
  await page.getByLabel("Environment").selectOption("staging");
  await expect(page.getByRole("link", { name: record.id })).toBeVisible();
  await page.getByRole("textbox", { name: "Filter deployments by service" }).last().fill("subscriber");
  await expect(page.getByText("No deployments in this view")).toBeVisible();
  await page.getByRole("textbox", { name: "Filter deployments by service" }).last().fill("payment");
  await page.getByRole("link", { name: record.id }).click();
  await expect(page.getByRole("heading", { name: "payment-service 1.4.0" })).toBeVisible();
  await expect(page.locator(".incident-timeline li")).toHaveCount(3);
  await expect(page.getByText("operator:rollback")).toBeVisible();
  await page.setViewportSize({ width: 390, height: 844 });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
});

test("incident investigation presents same-service deployment evidence as a lead", async ({ page, request }) => {
  const response = await request.post("/api/v1/incidents", {
    headers: { "Idempotency-Key": randomUUID() },
    data: { title: "Synthetic deployment correlation verification", severity: "SEV-4", environment: "staging", service: "payment-service", owner: "browser-verification", impact: "Synthetic verification only" },
  });
  expect(response.status()).toBe(201);
  const incident = await response.json();
  const deployment = {
    id: "ci-payment-example-incident", service: "payment-service", version: "1.4.0",
    commit_sha: "abcdef0123456789abcdef0123456789abcdef01", environment: "staging",
    deployer: "ci:gitlab", status: "Completed", occurred_at: new Date().toISOString(),
  };
  await page.route("**/api/v1/deployments?**", (route) => route.fulfill({ contentType: "application/json", body: JSON.stringify([deployment]) }));
  await page.goto(`/incidents/${incident.id}`);
  await expect(page.getByRole("heading", { name: "Deployment evidence" })).toBeVisible();
  await expect(page.getByText("Timing is a lead, not a root-cause conclusion.")).toBeVisible();
  await expect(page.getByRole("link", { name: deployment.id })).toHaveAttribute("href", `/deployments/${deployment.id}`);
});

test("business success incident includes cross-service release candidates", async ({ page, request }) => {
  const response = await request.post("/api/v1/incidents", {
    headers: { "Idempotency-Key": randomUUID() },
    data: { title: "BusinessSuccessRateLow · api-gateway", severity: "SEV-4", environment: "staging", service: "api-gateway", owner: "browser-verification", impact: "Synthetic cross-service verification only" },
  });
  expect(response.status()).toBe(201);
  const incident = await response.json();
  const marker = {
    id: "sim-deploy-browser-example", service: "payment-service", version: "1.4.0-sim-bad",
    commit_sha: "abcdef0123456789abcdef0123456789abcdef01", environment: "staging",
    deployer: "sim:local", status: "Rolled Back", occurred_at: new Date().toISOString(),
  };
  let requestedService: string | null = null;
  await page.route("**/api/v1/deployments?**", (route) => {
    requestedService = new URL(route.request().url()).searchParams.get("service");
    return route.fulfill({ contentType: "application/json", body: JSON.stringify([marker]) });
  });
  await page.goto(`/incidents/${incident.id}`);
  await expect(page.getByText("all services in this environment, because business success is a cross-service metric", { exact: false })).toBeVisible();
  await expect(page.getByRole("link", { name: marker.id })).toBeVisible();
  expect(requestedService).toBe("");
});
