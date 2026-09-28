import { test, expect } from "@playwright/test";
import { existsSync, readFileSync } from "node:fs";
import { resolve } from "node:path";
import { randomUUID } from "node:crypto";

const passwordFile = resolve(process.cwd(), ".secrets/local-admin-password");
const password = process.env.TELCOPULSE_TEST_ADMIN_PASSWORD ??
  (existsSync(passwordFile) ? readFileSync(passwordFile, "utf8").trimEnd() : "");

test("Engineer simulation commands keep the authenticated audit actor", async ({ page }) => {
  test.skip(process.env.TELCOPULSE_ENGINEER_AUDIT_LIVE !== "1" || !password,
    "requires a protected local stack with local-admin assigned Engineer");

  await page.goto("/simulator");
  await page.getByLabel("Username").fill("local-admin");
  await page.getByLabel("Password").fill(password);
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await expect(page.locator(".profile")).toContainText("Engineer");
  const origin = new URL(page.url()).origin;
  const me = await (await page.request.get("/api/v1/auth/me")).json();
  const actor = `operator:${me.id}`;
  const headers = {
    Origin: origin,
    "X-Operator-Actor": "operator:USR-ffffffffffffffffffffffff",
    "X-Operator-Role": "Administrator",
  };
  let runID = "";
  try {
    const started = await page.request.post("/api/v1/simulations", {
      headers: { ...headers, "Idempotency-Key": randomUUID() },
      data: {
        scenario: "payment-decline",
        delay_ms: 0,
        environment: "development",
        percentage: 1,
        duration_seconds: 30,
        reason: "Verify authenticated simulation audit",
      },
    });
    expect(started.status()).toBe(201);
    runID = (await started.json()).id;
    const initial = await (await page.request.get(`/api/v1/simulations/${runID}`)).json();
    expect(initial.audit).toMatchObject([{ action: "started", actor }]);

    const stopped = await page.request.post(`/api/v1/simulations/${runID}/stop`, {
      headers,
      data: { reason: "Finish authenticated audit verification" },
    });
    expect(stopped.status()).toBe(200);
    const final = await (await page.request.get(`/api/v1/simulations/${runID}`)).json();
    expect(final.audit).toMatchObject([
      { action: "started", actor },
      { action: "stopped", actor },
    ]);
    expect(final.run.active).toBe(false);
  } finally {
    if (runID) {
      await page.request.post(`/api/v1/simulations/${runID}/stop`, {
        headers,
        data: { reason: "Test cleanup" },
      });
    }
  }
});
