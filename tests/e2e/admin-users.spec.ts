import { test, expect } from "@playwright/test";
import { existsSync, readFileSync } from "node:fs";
import { resolve } from "node:path";
import { randomUUID } from "node:crypto";

const passwordFile = resolve(process.cwd(), ".secrets/local-admin-password");
const adminPassword = process.env.TELCOPULSE_TEST_ADMIN_PASSWORD ??
  (existsSync(passwordFile) ? readFileSync(passwordFile, "utf8").trimEnd() : "");

test("Administrator manages accounts and current roles take effect immediately", async ({ page, request }) => {
  test.skip(process.env.TELCOPULSE_ADMIN_USERS_LIVE !== "1" || !adminPassword,
    "requires a protected local stack and test administrator credential");

  const username = `rolecheck${randomUUID().slice(0, 8)}`;
  const password = `SecureLocalPass-${randomUUID()}`;
  await page.goto("/settings");
  await page.getByLabel("Username").fill("local-admin");
  await page.getByLabel("Password").fill(adminPassword);
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await expect(page.locator(".profile")).toContainText("Administrator");
  const origin = new URL(page.url()).origin;
  const admin = await (await page.request.get("/api/v1/auth/me")).json();

  await page.getByRole("button", { name: "Create user" }).click();
  const dialog = page.getByRole("dialog");
  await dialog.getByLabel("Username").fill(username);
  await dialog.getByLabel("Initial password").fill(password);
  await dialog.getByRole("button", { name: "Create user" }).click();
  await expect(dialog).not.toBeVisible();
  const row = page.getByRole("row").filter({ hasText: username });
  await expect(row).toBeVisible();
  await expect(row.getByRole("combobox", { name: `Role for ${username}` })).toHaveValue("Viewer");

  const login = await request.post("/api/v1/auth/login", {
    headers: { Origin: origin },
    data: { username, password },
  });
  expect(login.status()).toBe(200);
  expect((await login.json()).role).toBe("Viewer");
  expect((await request.get("/api/v1/auth/users")).status()).toBe(403);

  await row.getByRole("combobox", { name: `Role for ${username}` }).selectOption("Operator");
  await Promise.all([
    page.waitForResponse((response) => response.url().includes("/api/v1/auth/users/") && response.request().method() === "PATCH" && response.status() === 200),
    row.getByRole("button", { name: "Save changes" }).click(),
  ]);
  await expect(row.getByRole("button", { name: "Save changes" })).toBeDisabled();
  const changed = await (await request.get("/api/v1/auth/me")).json();
  expect(changed.role).toBe("Operator");

  await row.getByRole("checkbox", { name: "Active" }).uncheck();
  await Promise.all([
    page.waitForResponse((response) => response.url().includes("/api/v1/auth/users/") && response.request().method() === "PATCH" && response.status() === 200),
    row.getByRole("button", { name: "Save changes" }).click(),
  ]);
  await expect(row.getByRole("button", { name: "Save changes" })).toBeDisabled();
  expect((await request.get("/api/v1/auth/me")).status()).toBe(401);

  const lastAdmin = await page.request.patch(`/api/v1/auth/users/${admin.id}`, {
    headers: { Origin: origin },
    data: { role: "Viewer" },
  });
  expect(lastAdmin.status()).toBe(409);
  expect((await (await page.request.get("/api/v1/auth/me")).json()).role).toBe("Administrator");
});
