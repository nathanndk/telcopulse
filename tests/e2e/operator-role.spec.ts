import { test, expect } from "@playwright/test";
import { existsSync, readFileSync } from "node:fs";
import { resolve } from "node:path";
import { randomUUID } from "node:crypto";

const passwordFile = resolve(process.cwd(), ".secrets/local-admin-password");
const password = process.env.TELCOPULSE_TEST_ADMIN_PASSWORD ??
  (existsSync(passwordFile) ? readFileSync(passwordFile, "utf8").trimEnd() : "");

test("Operator can acknowledge and append evidence but cannot change severity", async ({ page }) => {
  test.skip(process.env.TELCOPULSE_OPERATOR_RBAC_LIVE !== "1" || !password,
    "requires a protected local stack with local-admin assigned Operator");

  await page.goto("/incidents");
  await page.getByLabel("Username").fill("local-admin");
  await page.getByLabel("Password").fill(password);
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await expect(page.locator(".profile")).toContainText("Operator");
  const origin = new URL(page.url()).origin;

  const created = await page.request.post("/api/v1/incidents", {
    headers: { "Idempotency-Key": randomUUID(), Origin: origin },
    data: {
      title: `Operator permissions ${randomUUID().slice(0, 8)}`,
      severity: "SEV-4",
      service: "payment-service",
      environment: "development",
      evidence: [{ kind: "note", summary: "Original evidence" }],
      action_items: [{ title: "Commander action", owner: "commander", done: false }],
    },
  });
  expect(created.status()).toBe(201);
  const incident = await created.json();
  await page.goto(`/incidents/${incident.id}`);
  await expect(page.getByRole("button", { name: "Escalate", exact: true })).toHaveCount(0);
  const deniedEscalation = await page.request.post(`/api/v1/incidents/${incident.id}/escalations`, {
    headers: { Origin: origin },
    data: { expected_version: incident.version, team: "Payment Operations", reason: "Operator cannot escalate" },
  });
  expect(deniedEscalation.status()).toBe(403);
  await page.getByRole("button", { name: "Edit incident", exact: true }).click();
  const dialog = page.getByRole("dialog");
  await expect(dialog.getByLabel("Severity", { exact: true })).toHaveCount(0);
  await expect(dialog.getByLabel("Title", { exact: true })).toHaveCount(0);
  await expect(dialog.getByRole("button", { name: "Add action item" })).toHaveCount(0);
  await expect(dialog.getByRole("button", { name: "Remove evidence 1" })).toHaveCount(0);
  await expect(dialog.getByText("Original evidence")).toBeVisible();
  await dialog.getByLabel("State", { exact: true }).selectOption("Acknowledged");
  await dialog.getByLabel("Owner", { exact: true }).fill("on-call-operator");
  await dialog.getByRole("button", { name: "Add evidence" }).click();
  await dialog.getByLabel("Evidence summary").fill("Operator triage note");
  await dialog.getByLabel("Audit note").fill("Acknowledge and assign incident");
  await dialog.getByRole("button", { name: "Save incident" }).click();
  await expect(dialog).not.toBeVisible();

  const detail = await (await page.request.get(`/api/v1/incidents/${incident.id}`)).json();
  expect(detail.incident).toMatchObject({
    state: "Acknowledged",
    owner: "on-call-operator",
    severity: "SEV-4",
    evidence: [
      { kind: "note", summary: "Original evidence" },
      { kind: "note", summary: "Operator triage note" },
    ],
    action_items: [{ title: "Commander action", owner: "commander", done: false }],
  });
  expect(detail.history.at(-1).actor).toMatch(/^operator:/);

  const forbidden = await page.request.put(`/api/v1/incidents/${incident.id}`, {
    headers: { Origin: origin },
    data: {
      title: detail.incident.title,
      severity: "SEV-1",
      owner: detail.incident.owner,
      impact: detail.incident.impact,
      root_cause: detail.incident.root_cause,
      mitigation: detail.incident.mitigation,
      resolution: detail.incident.resolution,
      postmortem_notes: detail.incident.postmortem_notes,
      related_deployment: detail.incident.related_deployment,
      affected_transactions: detail.incident.affected_transactions,
      affected_users: detail.incident.affected_users,
      error_rate: detail.incident.error_rate,
      success_rate: detail.incident.success_rate,
      latency_ms: detail.incident.latency_ms,
      evidence: detail.incident.evidence,
      action_items: detail.incident.action_items,
      state: detail.incident.state,
      expected_version: detail.incident.version,
      note: "Unauthorized severity change",
    },
  });
  expect(forbidden.status()).toBe(403);
  const after = await (await page.request.get(`/api/v1/incidents/${incident.id}`)).json();
  expect(after.incident.version).toBe(detail.incident.version);
  expect(after.incident.severity).toBe("SEV-4");
  expect(after.history).toHaveLength(detail.history.length);
});
