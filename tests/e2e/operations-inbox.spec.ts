import { expect, test } from "@playwright/test";

test("operations inbox links live records and keeps partial source failures visible", async ({ page }) => {
  const incident = {
    id: "INC-0123456789abcdef01234567", title: "Payment service degradation", severity: "SEV-2", state: "Investigating",
    owner: "Ops", owning_team: "Payments", escalation_level: 0, service: "payment-service", environment: "development",
    version: 2, created_at: "2026-09-28T09:00:00Z", detected_at: "2026-09-28T09:00:00Z", updated_at: "2026-09-28T09:10:00Z",
    affected_users: null, affected_transactions: 4,
  };
  await page.route("**/api/v1/incidents/overview?*", route => {
    const environment = new URL(route.request().url()).searchParams.get("environment");
    return route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(environment === "development"
      ? { active: 1, critical: 1, items: [incident] }
      : { active: 0, critical: 0, items: [] }) });
  });
  let auditUnavailable = false;
  await page.route("**/api/v1/audit?*", route => route.fulfill(auditUnavailable
    ? { status: 503, contentType: "application/json", body: JSON.stringify({ error: "operations audit unavailable" }) }
    : { status: 200, contentType: "application/json", body: JSON.stringify({ items: [{
      id: "incident:INC-0123456789abcdef01234567:2", source: "incident", environment: "development",
      resource_id: incident.id, actor: "local-operator", action: "investigating", note: "", at: "2026-09-28T09:10:00Z", changes: [],
    }], more: false }) }));
  await page.goto("/");
  const trigger = page.getByRole("button", { name: "Operations inbox, 1 active incident" });
  await expect(trigger).toBeVisible();
  await trigger.focus();
  await page.keyboard.press("Enter");
  const dialog = page.getByRole("dialog", { name: "Operations inbox" });
  await expect(dialog.getByText("Payment service degradation")).toBeVisible();
  await expect(dialog.getByRole("link", { name: /Payment service degradation/ })).toHaveAttribute("href", `/incidents/${incident.id}`);
  await expect(dialog.getByRole("link", { name: /incident · investigating/ })).toHaveAttribute("href", `/incidents/${incident.id}`);
  auditUnavailable = true;
  await dialog.getByRole("button", { name: "Refresh" }).click();
  await expect(dialog.getByText("Audit source unavailable. Recent changes are unknown.")).toBeVisible();
  await expect(dialog.getByText("Payment service degradation")).toBeVisible();
  await page.keyboard.press("Escape");
  await page.getByLabel("Environment").selectOption("staging");
  await page.getByRole("button", { name: "Operations inbox, 0 active incidents" }).click();
  await expect(dialog.getByText("No active incidents recorded in this environment.", { exact: false })).toBeVisible();
  await expect(dialog.getByText("Audit source unavailable. Recent changes are unknown.")).toBeVisible();
  await page.keyboard.press("Escape");
  await page.setViewportSize({ width: 390, height: 844 });
  await expect(page.getByRole("button", { name: "Operations inbox, 0 active incidents" })).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBeTruthy();
});
