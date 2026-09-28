import { test, expect } from "@playwright/test";

test("Services exposes a redacted, paginated Kafka dead-letter register", async ({ page, request }) => {
  const live = await request.get("/api/v1/notifications/dead-letters?limit=2");
  expect(live.ok()).toBeTruthy();
  const observed = await live.json();
  expect(Array.isArray(observed.items)).toBeTruthy();
  for (const item of observed.items) {
    expect(item.payload_sha256).toMatch(/^[a-f0-9]{64}$/);
    expect(item).not.toHaveProperty("payload");
  }

  let fail = false;
  const record = (offset: number, publication: string) => ({
    source_topic: "telcopulse.purchase.completed.v1",
    source_partition: 1,
    source_offset: offset,
    reason: "invalid purchase event",
    created_at: "2026-09-28T09:12:13Z",
    payload_bytes: 29,
    payload_sha256: "a".repeat(64),
    publication,
  });
  await page.route("**/api/v1/notifications/dead-letters?*", (route) => {
    const url = new URL(route.request().url());
    if (fail) return route.fulfill({ status: 503, contentType: "application/json", body: JSON.stringify({ error: "dead-letter register unavailable" }) });
    if (url.searchParams.get("reason") === "event identity conflict") {
      return route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ items: [], more: false }) });
    }
    const body = url.searchParams.get("cursor") === "next-cursor"
      ? { items: [record(10, "UNTRACKED")], more: false }
      : { items: [record(12, "PUBLISHED"), record(11, "PENDING")], more: true, next: "next-cursor" };
    return route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(body) });
  });
  await page.goto("/services");
  const table = page.getByRole("table", { name: "Kafka dead-letter records" });
  await expect(page.getByRole("heading", { name: "Kafka dead-letter register" })).toBeVisible();
  await expect(table.getByRole("row")).toHaveCount(3);
  await expect(page.getByText("Published", { exact: true })).toBeVisible();
  await expect(page.getByText("Pending", { exact: true })).toBeVisible();
  await expect(page.locator("main")).not.toContainText("private-raw-payload");
  await page.getByRole("button", { name: "Next", exact: true }).click();
  await expect(table.getByRole("row")).toHaveCount(2);
  await expect(page.getByText("Not tracked", { exact: true })).toBeVisible();
  await page.getByLabel("Dead-letter reason").selectOption("event identity conflict");
  await expect(page.getByText("No quarantined purchase events")).toBeVisible();
  fail = true;
  await page.getByRole("button", { name: "Refresh" }).last().click();
  await expect(page.getByText("dead-letter register unavailable", { exact: true })).toBeVisible();
  await page.setViewportSize({ width: 390, height: 844 });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBeTruthy();
});

test("operator reprocessing confirms scope and retries one uncertain command", async ({ page }) => {
  const record = {
    source_topic: "telcopulse.purchase.completed.v1",
    source_partition: 1,
    source_offset: 12,
    reason: "invalid purchase event",
    created_at: "2026-09-28T09:12:13Z",
    payload_bytes: 29,
    payload_sha256: "a".repeat(64),
    publication: "PUBLISHED",
  };
  await page.route("**/api/v1/notifications/dead-letters?*", route => route.fulfill({
    status: 200, contentType: "application/json",
    body: JSON.stringify({ items: [record], more: false }),
  }));
  const keys: string[] = [];
  await page.route("**/api/v1/notifications/dead-letters/1/12/replay", route => {
    keys.push(route.request().headers()["idempotency-key"]);
    if (keys.length === 1) return route.fulfill({ status: 503, contentType: "application/json", body: JSON.stringify({ error: "dead-letter replay unavailable" }) });
    return route.fulfill({
      status: 201, contentType: "application/json",
      body: JSON.stringify({ source_topic: record.source_topic, source_partition: 1, source_offset: 12,
        status: "REJECTED", reason: "invalid purchase event", transaction_id: "", actor: "local-operator", attempted_at: "2026-09-28T09:15:00Z" }),
    });
  });
  await page.goto("/services");
  await page.getByRole("button", { name: "Reprocess" }).click();
  const dialog = page.getByRole("dialog");
  await expect(dialog.getByText("No new customer purchase is started.", { exact: false })).toBeVisible();
  await dialog.getByRole("button", { name: "Confirm reprocessing" }).click();
  await expect(dialog.getByText("Retry here to confirm the same command.", { exact: false })).toBeVisible();
  await dialog.getByRole("button", { name: "Confirm reprocessing" }).click();
  await expect(dialog.getByText("Event still rejected")).toBeVisible();
  await expect(dialog.getByText("invalid purchase event", { exact: true })).toBeVisible();
  expect(keys).toHaveLength(2);
  expect(keys[0]).toBe(keys[1]);
  expect(keys[0]).toMatch(/^[a-f0-9-]{36}$/);
});

test("reprocessing history pages audited outcomes without exposing retained bytes", async ({ page }) => {
  const record = {
    source_topic: "telcopulse.purchase.completed.v1", source_partition: 1, source_offset: 12,
    reason: "invalid purchase event", created_at: "2026-09-28T09:12:13Z", payload_bytes: 29,
    payload_sha256: "a".repeat(64), publication: "PUBLISHED", replay_status: "REJECTED",
    replay_actor: "local-operator", replayed_at: "2026-09-28T09:15:00Z",
  };
  await page.route("**/api/v1/notifications/dead-letters?*", route => route.fulfill({
    status: 200, contentType: "application/json", body: JSON.stringify({ items: [record], more: false }),
  }));
  let fail = false;
  await page.route("**/api/v1/notifications/dead-letters/1/12/replays?*", route => {
    if (fail) return route.fulfill({ status: 503, contentType: "application/json", body: JSON.stringify({ error: "replay history unavailable" }) });
    const cursor = new URL(route.request().url()).searchParams.get("cursor");
    const outcome = (status: string, at: string) => ({ source_topic: record.source_topic, source_partition: 1, source_offset: 12,
      status, reason: status === "REJECTED" ? "invalid purchase event" : "", transaction_id: status === "REJECTED" ? "" : "TXN-0123456789abcdef01234567",
      actor: "local-operator", attempted_at: at });
    const body = cursor ? { items: [outcome("DELIVERED", "2026-09-28T09:14:00Z")], more: false }
      : { items: [outcome("REJECTED", "2026-09-28T09:15:00Z")], more: true, next: "older-page" };
    return route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(body) });
  });
  await page.goto("/services");
  await page.getByRole("button", { name: "History" }).click();
  const dialog = page.getByRole("dialog", { name: "Reprocessing history" });
  await expect(dialog.getByText("REJECTED", { exact: true })).toBeVisible();
  await expect(dialog.getByText("invalid purchase event", { exact: true })).toBeVisible();
  await dialog.getByRole("button", { name: "Next" }).click();
  await expect(dialog.getByText("DELIVERED", { exact: true })).toBeVisible();
  await expect(dialog.getByRole("link", { name: "TXN-0123456789abcdef01234567" })).toHaveAttribute("href", "/transactions/TXN-0123456789abcdef01234567");
  expect(await page.locator("main").textContent()).not.toContain("private-raw-payload");
  fail = true;
  await dialog.getByRole("button", { name: "Refresh history" }).click();
  await expect(dialog.getByText("replay history unavailable", { exact: true })).toBeVisible();
  await dialog.getByRole("button", { name: "Try again" }).click();
  await expect(dialog.getByText("replay history unavailable", { exact: true })).toBeVisible();
});
