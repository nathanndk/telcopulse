import { test, expect } from "@playwright/test";
import { existsSync, readFileSync } from "node:fs";
import { resolve } from "node:path";

const passwordFile = resolve(process.cwd(), ".secrets/local-admin-password");
const password = process.env.TELCOPULSE_TEST_ADMIN_PASSWORD ??
  (existsSync(passwordFile) ? readFileSync(passwordFile, "utf8").trimEnd() : "");

test("operator signs in, sees role, and revokes the session on sign out", async ({ page, request }) => {
  const status = await request.get("/api/v1/auth/status");
  expect(status.ok()).toBeTruthy();
  test.skip(!(await status.json()).required || !password, "requires local protected mode and test administrator credential");

  await page.goto("/incidents");
  await expect(page.getByRole("heading", { name: "Operator sign in" })).toBeVisible();
  await page.getByLabel("Username").fill("local-admin");
  await page.getByLabel("Password").fill(password);
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Incidents" })).toBeVisible();
  await expect(page.locator(".profile")).toContainText("Administrator");
  await expect(page.getByRole("button", { name: "Create incident" })).toBeVisible();

  await page.getByRole("button", { name: "Sign out" }).click();
  await expect(page.getByRole("heading", { name: "Operator sign in" })).toBeVisible();
  const meStatus = await page.evaluate(async () =>
    (await fetch("/api/v1/auth/me")).status,
  );
  expect(meStatus).toBe(401);
});

test("viewer presentation hides incident and simulation mutations", async ({ page, request }) => {
  const status = await request.get("/api/v1/auth/status");
  expect(status.ok()).toBeTruthy();
  test.skip(!(await status.json()).required || !password, "requires local protected mode and test administrator credential");

  await page.goto("/incidents");
  await page.getByLabel("Username").fill("local-admin");
  await page.getByLabel("Password").fill(password);
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await expect(page.locator(".profile")).toContainText("Administrator");
  await page.route("**/api/v1/auth/me", async (route) => {
    await route.fulfill({ json: { id: "USR-0123456789abcdef01234567", username: "viewer-test", role: "Viewer" } });
  });
  await page.reload();
  await expect(page.locator(".profile")).toContainText("Viewer");
  await expect(page.getByRole("button", { name: "Create incident" })).toHaveCount(0);
  await page.goto("/simulator");
  await expect(page.getByRole("button", { name: "Inject failure" })).toHaveCount(0);
  await expect(page.getByRole("button", { name: "Viewer role cannot run transactions" })).toBeDisabled();
});
