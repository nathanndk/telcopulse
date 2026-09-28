import { test, expect } from "@playwright/test";
import { traceViewerBase, traceLinks } from "../../apps/web/src/lib/trace-links";

test("trace viewer configuration rejects unsafe destinations", () => {
  for (const value of [undefined, "", "javascript:alert(1)", "https://user:secret@example.com", "https://example.com?token=secret", "not a URL"]) {
    expect(traceViewerBase(value)).toBeNull();
  }
  const base = traceViewerBase("https://example.com/jaeger/");
  expect(base).toBe("https://example.com/jaeger");
  expect(traceLinks(base, "invalid", "TXN-example")?.original).toBeNull();
  expect(traceLinks(null, "a".repeat(32), "TXN-example")).toBeNull();
});

test("transaction links open the actual trace and filtered workflow attempts", async ({ page, request }) => {
  const app = process.env.PURCHASE_URL ?? process.env.WEB_URL ?? "http://localhost:3000";
  const response = await request.post(app + "/api/v1/transactions", {
    headers: { "Origin": app, "Idempotency-Key": crypto.randomUUID() },
    data: { customer_id: "cus-001", package_id: "pkg-10", payment_method: "E-Wallet", environment: "development" },
  });
  expect(response.status()).toBe(201);
  const transaction = await response.json();
  await page.goto(`/transactions/${transaction.id}`);
  await expect(page.getByRole("columnheader", { name: "Stage reference" })).toBeVisible();
  await expect(page.getByRole("columnheader", { name: "Span ID", exact: true })).toHaveCount(0);
  const original = page.getByRole("link", { name: "Open original trace" });
  await expect(original).toHaveAttribute("href", `http://localhost:16686/trace/${transaction.trace_id}`);
  const attempts = page.getByRole("link", { name: "Find workflow attempts" });
  const target = new URL((await attempts.getAttribute("href"))!);
  expect(JSON.parse(target.searchParams.get("tags")!)).toEqual({ "transaction.id": transaction.id });
  await expect.poll(async () => {
    const trace = await request.get(`http://localhost:16686/api/traces/${transaction.trace_id}`);
    if (!trace.ok()) return false;
    const body = await trace.json();
    return body.data?.some((item: { spans: { operationName: string }[] }) =>
      item.spans.some((span) => span.operationName === "POST /api/v1/transactions"),
    ) ?? false;
  }, { timeout: 15000 }).toBe(true);
  const popupPromise = page.waitForEvent("popup");
  await original.click();
  const popup = await popupPromise;
  await expect(popup.getByText("POST /api/v1/transactions", { exact: false }).first()).toBeVisible({ timeout: 15000 });
  await popup.close();
  const searchPromise = page.waitForEvent("popup");
  await attempts.click();
  const search = await searchPromise;
  await expect(search.getByRole("link", { name: /purchase.resume|POST \/api\/v1\/transactions/ }).first()).toBeVisible({ timeout: 15000 });
  await search.close();
});
