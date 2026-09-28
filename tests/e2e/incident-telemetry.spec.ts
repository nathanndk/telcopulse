import { test, expect } from "@playwright/test";
import { randomUUID } from "node:crypto";

test("incident captures a frozen live metric snapshot with audit history", async ({
  page,
  request,
}) => {
  const created = await request.post("/api/v1/incidents", {
    headers: { "Idempotency-Key": randomUUID() },
    data: {
      title: "Live metric evidence verification",
      severity: "SEV-4",
      service: "payment-service",
      environment: "staging",
    },
  });
  expect(created.status()).toBe(201);
  const incident = await created.json();
  await page.goto(`/incidents/${incident.id}`);
  const panel = page.locator("section").filter({
    has: page.getByRole("heading", {
      name: "Live service telemetry",
      exact: true,
      includeHidden: true,
    }),
  });
  await expect(
    panel.getByText("payment-service", { exact: true }),
  ).toBeVisible();
  await expect(
    panel.getByText("Current shared-runtime HTTP measurements", {
      exact: false,
    }),
  ).toBeVisible();
  await panel.getByRole("button", { name: "Review metric evidence" }).click();
  const dialog = page.getByRole("dialog");
  const evidence = dialog.getByLabel("Evidence summary", { exact: true });
  const frozen = await evidence.inputValue();
  expect(frozen).toContain("service=payment-service");
  expect(frozen).toContain("scope=shared-runtime (development and staging)");
  expect(frozen).toContain("not incident-time impact");
  const before = await (
    await request.get(`/api/v1/incidents/${incident.id}`)
  ).json();
  expect(before.incident.version).toBe(1);
  expect(before.incident.evidence ?? []).toHaveLength(0);
  await page.route("**/api/v1/services/health", (route) =>
    route.fulfill({
      status: 503,
      contentType: "application/json",
      body: JSON.stringify({ error: "telemetry test outage" }),
    }),
  );
  await page.waitForResponse(
    (response) =>
      response.url().endsWith("/services/health") && response.status() === 503,
    { timeout: 25000 },
  );
  await expect(
    panel.getByText("telemetry test outage", { exact: true }),
  ).toBeAttached();
  await expect(evidence).toHaveValue(frozen);
  await dialog
    .getByLabel("Audit note")
    .fill("Preserve current HTTP evidence for investigation");
  await dialog
    .getByRole("button", { name: "Save incident", exact: true })
    .click();
  await expect(dialog).not.toBeVisible();
  const saved = await (
    await request.get(`/api/v1/incidents/${incident.id}`)
  ).json();
  expect(saved.incident.evidence).toEqual([
    { kind: "metric", summary: frozen },
  ]);
  expect(saved.incident.state).toBe("Detected");
  expect(saved.incident.error_rate).toBeNull();
  expect(saved.incident.latency_ms).toBeNull();
  expect(saved.history[1].after.evidence[0].summary).toBe(frozen);
  expect(saved.history[1].before.evidence ?? []).toHaveLength(0);
  await expect(
    panel.getByText("telemetry test outage", { exact: true }),
  ).toBeVisible();
  await expect(
    panel.getByRole("button", { name: "Review metric evidence" }),
  ).toBeDisabled();
  await page.unroute("**/api/v1/services/health");
  await panel.getByRole("button", { name: "Try again", exact: true }).click();
  await expect(
    panel.getByRole("button", { name: "Review metric evidence" }),
  ).toBeEnabled();
  await page.screenshot({
    path: "/private/tmp/telcopulse-incident-telemetry.png",
    fullPage: true,
  });
});
