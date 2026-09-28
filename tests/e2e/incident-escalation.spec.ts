import { test, expect } from "@playwright/test";
import { randomUUID } from "node:crypto";

test("Commander escalation records a team handoff and audited revision", async ({ page, request }) => {
  const created = await request.post("/api/v1/incidents", {
    headers: { "Idempotency-Key": randomUUID() },
    data: {
      title: `Escalation verification ${randomUUID().slice(0, 8)}`,
      severity: "SEV-3",
      service: "payment-service",
      environment: "development",
    },
  });
  expect(created.status()).toBe(201);
  const incident = await created.json();

  await page.goto(`/incidents/${incident.id}`);
  await page.getByRole("button", { name: "Escalate", exact: true }).click();
  const dialog = page.getByRole("dialog");
  await expect(dialog.getByText("This does not send an external on-call notification.", { exact: false })).toBeVisible();
  await dialog.getByLabel("Owning team").fill("Payment Operations");
  await dialog.getByLabel("Assign owner (optional)").fill("payment-oncall");
  await dialog.getByLabel("Severity", { exact: true }).selectOption("SEV-2");
  await dialog.getByLabel("Escalation reason").fill("Business success rate falling after deployment");
  await dialog.getByRole("button", { name: "Record escalation" }).click();
  await expect(dialog).not.toBeVisible();
  await expect(page.getByText("Owning team: Payment Operations")).toBeVisible();
  await expect(page.getByText("Revision 2 · Escalated to Payment Operations · level 1")).toBeVisible();

  const detail = await (await request.get(`/api/v1/incidents/${incident.id}`)).json();
  expect(detail.incident).toMatchObject({
    owning_team: "Payment Operations",
    owner: "payment-oncall",
    severity: "SEV-2",
    escalation_level: 1,
    version: 2,
  });
  expect(detail.incident.escalated_at).toBeTruthy();
  expect(detail.history.at(-1)).toMatchObject({
    action: "escalated",
    note: "Business success rate falling after deployment",
    before: { escalation_level: 0, severity: "SEV-3" },
    after: { escalation_level: 1, severity: "SEV-2", owning_team: "Payment Operations" },
  });

  const stale = await request.post(`/api/v1/incidents/${incident.id}/escalations`, {
    data: { expected_version: 1, team: "Network Operations", reason: "Stale handoff" },
  });
  expect(stale.status()).toBe(409);
  const downgrade = await request.post(`/api/v1/incidents/${incident.id}/escalations`, {
    data: { expected_version: 2, team: "Network Operations", severity: "SEV-4", reason: "Invalid downgrade" },
  });
  expect(downgrade.status()).toBe(422);
});
