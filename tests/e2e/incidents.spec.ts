import { test, expect } from "@playwright/test";
import { randomUUID } from "node:crypto";

test("major incident generates an audited structured postmortem", async ({ page, request }) => {
  const fields = {
    title: `Major postmortem ${randomUUID().slice(0, 8)}`,
    severity: "SEV-2",
    owner: "incident-commander",
    impact: "Forty synthetic purchases failed during the payment outage",
    root_cause: "Payment connection pool exhausted",
    mitigation: "Limited concurrency and restored capacity",
    resolution: "Business success rate recovered",
    postmortem_notes: "",
    evidence: [{ kind: "metric", summary: "Business SLI fell" }],
    action_items: [{ title: "Cap payment concurrency", owner: "platform", priority: "P1", status: "Open", done: false }],
  };
  const created = await request.post("/api/v1/incidents", {
    headers: { "Idempotency-Key": randomUUID() },
    data: { ...fields, environment: "development", service: "payment-service" },
  });
  expect(created.status()).toBe(201);
  let current = await created.json();
  for (const state of ["Acknowledged", "Investigating", "Identified", "Mitigating", "Monitoring", "Resolved"]) {
    const response = await request.put(`/api/v1/incidents/${current.id}`, {
      data: { ...fields, state, expected_version: current.version, note: `Reviewed ${state}`,
        ...(state === "Resolved" ? { recovery_validation: { observation: "Successful synthetic purchase and business success rate recovered", source_url: "http://localhost:3001/transactions", observed_at: new Date().toISOString() } } : {}),
      },
    });
    expect(response.status()).toBe(200);
    current = await response.json();
  }
  const bypass = await request.put(`/api/v1/incidents/${current.id}`, {
    data: { ...fields, postmortem_notes: "Legacy note", state: "Postmortem", expected_version: current.version, note: "Bypass report" },
  });
  expect(bypass.status()).toBe(422);

  await page.goto(`/incidents/${current.id}`);
  await expect(page.locator("#recovery")).toHaveCount(0);
  await expect(page.getByRole("button", { name: "Generate postmortem" })).toBeVisible();
  await page.getByRole("button", { name: "Generate postmortem" }).click();
  const dialog = page.getByRole("dialog", { name: "Generate postmortem" });
  await dialog.getByLabel("Summary").fill("Payment purchase degradation and recovery");
  await dialog.getByLabel("Detection").fill("Business success rate alert fired");
  await dialog.getByLabel("Contributing factors").fill("Connection pool sizing was too low");
  await dialog.getByLabel("What went well").fill("Traces linked the timeout to payment");
  await dialog.getByLabel("What went wrong").fill("Rollback decision took too long");
  await dialog.getByLabel("Audit note").fill("Commander approved retrospective");
  await dialog.getByRole("button", { name: "Generate and audit" }).click();
  await expect(dialog).not.toBeVisible();
  await expect(page.getByText("Payment purchase degradation and recovery")).toBeVisible();
  await expect(page.getByText("Connection pool sizing was too low")).toBeVisible();
  await page.screenshot({ path: "/private/tmp/telcopulse-postmortem.png", fullPage: true });

  const detailResponse = await request.get(`/api/v1/incidents/${current.id}`);
  expect(detailResponse.status()).toBe(200);
  const detail = await detailResponse.json();
  expect(detail.incident.state).toBe("Postmortem");
  expect(detail.incident.version).toBe(8);
  expect(detail.postmortem.impact).toBe(fields.impact);
  expect(detail.postmortem.root_cause).toBe(fields.root_cause);
  expect(detail.postmortem.mitigation).toBe(fields.mitigation);
  expect(detail.postmortem.recovery_validation).toMatchObject({
    observation: "Successful synthetic purchase and business success rate recovered",
    validated_by: "local-operator",
  });
  expect(detail.postmortem.action_items[0].priority).toBe("P1");
  expect(detail.postmortem.timeline).toHaveLength(8);
  expect(detail.history[7].action).toBe("postmortem_generated");

  const index = await request.get(`/api/v1/incidents/postmortems?environment=development&search=${encodeURIComponent(fields.root_cause)}`);
  expect(index.status()).toBe(200);
  expect((await index.json()).items).toEqual(expect.arrayContaining([expect.objectContaining({
    incident_id: current.id,
    root_cause: fields.root_cause,
    mitigation: fields.mitigation,
  })]));
  await page.goto("/rca");
  await page.getByRole("link", { name: "RCA reports" }).click();
  await page.getByLabel("Search RCA reports").fill(fields.root_cause);
  const reportCard = page.locator(".report-card").filter({ hasText: fields.title });
  await expect(reportCard).toBeVisible();
  await expect(reportCard.getByText(fields.mitigation)).toBeVisible();
  await expect(reportCard.getByText(fields.root_cause)).toBeVisible();
  await page.screenshot({ path: "/private/tmp/telcopulse-rca-reports-desktop.png", fullPage: true });
  await page.setViewportSize({ width: 390, height: 844 });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  await page.screenshot({ path: "/private/tmp/telcopulse-rca-reports-mobile.png", fullPage: true });
  await reportCard.getByRole("link", { name: "Full report" }).click();
  await expect(page).toHaveURL(new RegExp(`/incidents/${current.id}#postmortem$`));
  await expect(page.getByText(fields.mitigation).last()).toBeVisible();
});

test("Monitoring shows a measured recovery window and keeps resolution manual", async ({ page, request }) => {
  const fields = {
    title: `Recovery sample ${randomUUID().slice(0, 8)}`,
    severity: "SEV-3", owner: "on-call", impact: "Synthetic payment checks",
    root_cause: "Synthetic payment fault", mitigation: "Fault stopped",
    resolution: "", postmortem_notes: "", evidence: [], action_items: [],
  };
  const created = await request.post("/api/v1/incidents", {
    headers: { "Idempotency-Key": randomUUID() },
    data: { ...fields, environment: "development", service: "payment-service" },
  });
  expect(created.status()).toBe(201);
  let incident = await created.json();
  for (const state of ["Acknowledged", "Investigating", "Identified", "Mitigating", "Monitoring"]) {
    const response = await request.put(`/api/v1/incidents/${incident.id}`, {
      data: { ...fields, state, expected_version: incident.version, note: `Moved to ${state}` },
    });
    expect(response.status()).toBe(200);
    incident = await response.json();
  }
  const live = await request.get(`/api/v1/incidents/${incident.id}/recovery-assessment`);
  expect(live.status()).toBe(200);
  const assessment = await live.json();
  expect(assessment).toMatchObject({ applicable: true, scope: "purchase_path_environment", status: "collecting", earlier_total: expect.any(Number), recent_total: expect.any(Number) });
  expect(assessment.monitoring_at).toBeTruthy();
  await page.goto(`/incidents/${incident.id}`);
  const recovery = page.locator("#recovery");
  await expect(recovery.getByText("Recovery unknown")).toBeVisible();
  await expect(recovery.getByText(/Monitoring has not covered two full/)).toBeVisible();

  const now = Date.now();
  await page.route(`**/api/v1/incidents/${incident.id}/recovery-assessment`, (route) => route.fulfill({
    status: 200, contentType: "application/json",
    body: JSON.stringify({ ...assessment, status: "meets_target", evaluated_at: new Date(now).toISOString(), monitoring_at: new Date(now - 15*60_000).toISOString(), earlier_start: new Date(now - 10*60_000).toISOString(), split_at: new Date(now - 5*60_000).toISOString(), window_end: new Date(now).toISOString(), earlier_total: 5, earlier_success: 5, earlier_failed: 0, recent_total: 5, recent_success: 5, recent_failed: 0 }),
  }));
  await recovery.getByRole("button", { name: "Refresh assessment" }).click();
  await expect(recovery.getByText("Measured sample meets target")).toBeVisible();
  await recovery.getByRole("button", { name: "Review metric evidence" }).click();
  const dialog = page.getByRole("dialog", { name: "Edit incident" });
  await expect(dialog.getByLabel("Evidence summary")).toHaveValue(/Synthetic purchase recovery assessment/);
  await dialog.getByLabel("Audit note").fill("Reviewed sustained synthetic purchase sample");
  await dialog.getByRole("button", { name: "Save incident" }).click();
  await expect(dialog).not.toBeVisible();
  const detail = await (await request.get(`/api/v1/incidents/${incident.id}`)).json();
  expect(detail.incident.state).toBe("Monitoring");
  expect(detail.incident.evidence).toEqual(expect.arrayContaining([expect.objectContaining({ kind: "metric", summary: expect.stringContaining("status=meets_target") })]));
});

test("RCA report library keeps filters available after API failure", async ({ page }) => {
  await page.route("**/api/v1/incidents/postmortems?**", (route) => route.fulfill({
    status: 503,
    contentType: "application/json",
    body: JSON.stringify({ error: "Report backend unavailable" }),
  }));
  await page.goto("/rca/reports");
  await expect(page.getByText("Report backend unavailable")).toBeVisible();
  await expect(page.getByLabel("Search RCA reports")).toBeEnabled();
  await page.unroute("**/api/v1/incidents/postmortems?**");
  await page.getByRole("button", { name: "Try again" }).click();
  await expect(page.getByRole("heading", { name: "Postmortem library" })).toBeVisible();
});

test("incident register searches and filters persisted incidents", async ({ page, request }) => {
  const title = `Filter investigation ${randomUUID().slice(0, 8)}`;
  const response = await request.post("/api/v1/incidents", {
    headers: { "Idempotency-Key": randomUUID() },
    data: { title, severity: "SEV-3", owner: "filter-verification", impact: "Synthetic filter check", environment: "development", service: "payment-service" },
  });
  expect(response.status()).toBe(201);
  const incident = await response.json();
  const api = await request.get(`/api/v1/incidents?environment=development&search=${encodeURIComponent(title)}&severity=SEV-3&service=payment-service&owner=VERIFICATION&since=${encodeURIComponent(new Date(Date.now() - 86400000).toISOString())}`);
  expect(api.status()).toBe(200);
  expect((await api.json()).items).toEqual(expect.arrayContaining([expect.objectContaining({ id: incident.id })]));
  expect((await request.get("/api/v1/incidents?environment=development&since=yesterday")).status()).toBe(422);

  await page.goto("/incidents");
  await page.getByLabel("Search incidents").fill(title);
  await page.getByLabel("Incident severity").selectOption("SEV-3");
  await page.getByLabel("Filter incidents by service").fill("payment-service");
  await page.getByLabel("Filter incidents by owner").fill("VERIFICATION");
  await page.getByLabel("Incident detected time").selectOption("24h");
  await expect(page.getByRole("link", { name: title })).toBeVisible();
  await expect(page.locator("tbody tr")).toHaveCount(1);
  await page.screenshot({ path: "/private/tmp/telcopulse-incident-filters-desktop.png", fullPage: true });
  await page.getByLabel("Filter incidents by service").fill("package-service");
  await expect(page.getByText("No incidents in this view")).toBeVisible();
  await page.getByRole("button", { name: "Clear filters" }).click();
  await expect(page.getByLabel("Incident severity")).toHaveValue("");
  await page.setViewportSize({ width: 390, height: 844 });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  await page.screenshot({ path: "/private/tmp/telcopulse-incident-filters-mobile.png", fullPage: true });
});

test("incident saved views persist filters and can be updated and deleted", async ({ page, request }) => {
  const name = `Triage ${randomUUID().slice(0, 8)}`;
  await page.goto("/incidents");
  await page.getByLabel("Incident state").selectOption("Investigating");
  await page.getByLabel("Incident severity").selectOption("SEV-2");
  await page.getByLabel("Filter incidents by service").fill("payment-service");
  await page.getByLabel("Incident detected time").selectOption("24h");
  await page.getByLabel("Sort incidents").selectOption("detected_asc");
  await page.locator(".incident-column-menu summary").click();
  await page.locator(".incident-column-menu").getByLabel("Owner", { exact: true }).uncheck();
  await page.getByLabel("Saved view name").fill(name);
  await page.getByRole("button", { name: "Save view" }).click();
  await expect(page.getByRole("status")).toHaveText("View saved");
  await expect(page.getByLabel("Saved incident view")).toHaveValue(/^VIEW-/);
  const id = await page.getByLabel("Saved incident view").inputValue();
  try {
    await page.reload();
    await expect(page.getByLabel("Saved incident view").locator(`option[value="${id}"]`)).toHaveCount(1);
    await page.getByLabel("Saved incident view").selectOption(id);
    await expect(page.getByLabel("Incident state")).toHaveValue("Investigating");
    await expect(page.getByLabel("Incident severity")).toHaveValue("SEV-2");
    await expect(page.getByLabel("Filter incidents by service")).toHaveValue("payment-service");
    await expect(page.getByLabel("Incident detected time")).toHaveValue("24h");
    await expect(page.getByLabel("Sort incidents")).toHaveValue("detected_asc");
    await expect(page.locator("thead").getByText("Owner", { exact: true })).toHaveCount(0);
    await page.getByLabel("Incident severity").selectOption("SEV-1");
    await page.getByRole("button", { name: "Update view" }).click();
    await expect(page.getByRole("status")).toHaveText("View updated");
    await page.reload();
    await page.getByLabel("Saved incident view").selectOption(id);
    await expect(page.getByLabel("Incident severity")).toHaveValue("SEV-1");
    await page.getByRole("button", { name: "Delete view" }).click();
    await expect(page.getByLabel("Saved incident view")).toHaveValue("");
    await expect(page.getByLabel("Saved incident view").locator(`option[value="${id}"]`)).toHaveCount(0);
  } finally {
    await request.delete(`/api/v1/incidents/saved-views/${id}`);
  }
});

test("incident table sorts server-side with cursor-scoped paging", async ({ page, request }) => {
  const marker = `Table sort ${randomUUID().slice(0, 8)}`;
  const now = Date.now();
  const created: { id: string; title: string }[] = [];
  for (const [suffix, age, severity] of [["older", 48, "SEV-3"], ["middle", 24, "SEV-1"], ["newer", 1, "SEV-2"]] as const) {
    const response = await request.post("/api/v1/incidents", {
      headers: { "Idempotency-Key": randomUUID() },
      data: { title: `${marker} ${suffix}`, severity, owner: "sort-verification", impact: "Synthetic sort check", environment: "development", service: "payment-service", detected_at: new Date(now - age * 3600000).toISOString() },
    });
    expect(response.status()).toBe(201);
    created.push(await response.json());
  }
  const first = await request.get(`/api/v1/incidents?environment=development&search=${encodeURIComponent(marker)}&sort=detected_asc&limit=1`);
  expect(first.status()).toBe(200);
  const pageOne = await first.json();
  expect(pageOne.items[0].id).toBe(created[0].id);
  const second = await request.get(`/api/v1/incidents?environment=development&search=${encodeURIComponent(marker)}&sort=detected_asc&limit=1&cursor=${encodeURIComponent(pageOne.next)}`);
  expect(second.status()).toBe(200);
  expect((await second.json()).items[0].id).toBe(created[1].id);
  const wrongSort = await request.get(`/api/v1/incidents?environment=development&search=${encodeURIComponent(marker)}&sort=severity_asc&limit=1&cursor=${encodeURIComponent(pageOne.next)}`);
  expect(wrongSort.status()).toBe(422);
  const priority = await request.get(`/api/v1/incidents?environment=development&search=${encodeURIComponent(marker)}&sort=severity_asc&limit=3`);
  expect(priority.status()).toBe(200);
  expect((await priority.json()).items.map((item: { id: string }) => item.id)).toEqual([created[1].id, created[2].id, created[0].id]);
  const priorityFirst = await request.get(`/api/v1/incidents?environment=development&search=${encodeURIComponent(marker)}&sort=severity_asc&limit=1`);
  const priorityPage = await priorityFirst.json();
  expect(priorityPage.items[0].id).toBe(created[1].id);
  const prioritySecond = await request.get(`/api/v1/incidents?environment=development&search=${encodeURIComponent(marker)}&sort=severity_asc&limit=1&cursor=${encodeURIComponent(priorityPage.next)}`);
  expect(prioritySecond.status()).toBe(200);
  expect((await prioritySecond.json()).items[0].id).toBe(created[2].id);
  const newest = await request.get(`/api/v1/incidents?environment=development&search=${encodeURIComponent(marker)}&sort=detected_desc&limit=3`);
  expect(newest.status()).toBe(200);
  expect((await newest.json()).items.map((item: { id: string }) => item.id)).toEqual([created[2].id, created[1].id, created[0].id]);
  expect((await request.get(`/api/v1/incidents?environment=development&sort=unsafe`)).status()).toBe(422);
  await page.goto("/incidents");
  await page.getByLabel("Search incidents").fill(marker);
  await page.getByLabel("Sort incidents").selectOption("detected_asc");
  await expect(page.locator("tbody tr").first().getByRole("link", { name: created[0].title })).toBeVisible();
  await page.getByLabel("Sort incidents").selectOption("severity_asc");
  await expect(page.locator("tbody tr").first().getByRole("link", { name: created[1].title })).toBeVisible();
  await expect(page.locator("thead").getByText("Actions", { exact: true })).toBeVisible();
  await expect(page.locator("tbody tr").first().getByRole("link", { name: "View details" })).toBeVisible();
});

test("incident purchase impact links observed failed transactions without claiming all affected customers", async ({ page, request }) => {
  const incidentResponse = await request.post("/api/v1/incidents", {
    headers: { "Idempotency-Key": randomUUID() },
    data: { title: `Purchase impact ${randomUUID().slice(0, 8)}`, severity: "SEV-3", owner: "impact-verification", impact: "Outcome correlation under review", environment: "development", service: "payment-service" },
  });
  expect(incidentResponse.status()).toBe(201);
  const incident = await incidentResponse.json();
  const purchaseResponse = await request.post("/api/v1/transactions", {
    headers: { "Idempotency-Key": randomUUID() },
    data: { customer_id: "cus-003", package_id: "pkg-25", payment_method: "Pulsa", environment: "development" },
  });
  expect([200, 201]).toContain(purchaseResponse.status());
  const purchase = await purchaseResponse.json();
  expect(purchase.status).toBe("FAILED");
  expect(purchase.error_code).toBe("INSUFFICIENT_BALANCE");
  const endpoint = `/api/v1/incidents/${incident.id}/purchase-impact`;
  const response = await request.get(`${endpoint}?window=2h`);
  expect(response.status()).toBe(200);
  const impact = await response.json();
  expect(impact.applicable).toBe(true);
  expect(impact.scope).toBe("purchase_path_environment");
  expect(impact.failed).toBeGreaterThanOrEqual(1);
  expect(impact.failed_customers).toBeGreaterThanOrEqual(1);
  expect(impact.items).toEqual(expect.arrayContaining([expect.objectContaining({ id: purchase.id, customer_id: "cus-003", error_code: "INSUFFICIENT_BALANCE" })]));
  expect(JSON.stringify(impact)).not.toContain("628123450789");
  expect((await request.get(`${endpoint}?window=bad`)).status()).toBe(422);
  expect((await request.get(`${endpoint}?window=2h&cursor=bad`)).status()).toBe(422);
  await page.goto(`/incidents/${incident.id}`);
  const table = page.getByRole("table", { name: "Observed failed purchases" });
  await expect(table.getByRole("link", { name: purchase.id })).toBeVisible();
  await expect(page.getByText("Distinct customer IDs count only observed failed purchases.", { exact: false })).toBeVisible();
  await page.setViewportSize({ width: 390, height: 844 });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  await page.screenshot({ path: "/private/tmp/telcopulse-purchase-impact-mobile.png", fullPage: true });
});

test("incident purchase impact distinguishes unsupported services and source errors", async ({ page, request }) => {
  const incidentResponse = await request.post("/api/v1/incidents", {
    headers: { "Idempotency-Key": randomUUID() },
    data: { title: `Notification impact ${randomUUID().slice(0, 8)}`, severity: "SEV-3", owner: "impact-verification", impact: "Scope check", environment: "development", service: "notification-service" },
  });
  expect(incidentResponse.status()).toBe(201);
  const incident = await incidentResponse.json();
  const unsupported = await request.get(`/api/v1/incidents/${incident.id}/purchase-impact?window=2h`);
  expect(unsupported.status()).toBe(200);
  expect((await unsupported.json()).applicable).toBe(false);
  await page.goto(`/incidents/${incident.id}`);
  await expect(page.getByText("No purchase-path correlation for this service")).toBeVisible();
  await page.route("**/purchase-impact?**", (route) => route.fulfill({ status: 503, contentType: "application/json", body: JSON.stringify({ error: "Purchase evidence unavailable" }) }));
  await page.reload();
  await expect(page.getByText("Purchase evidence unavailable")).toBeVisible();
});

test("incident logs show source status and bounded indexed evidence", async ({ page, request }) => {
  const response = await request.post("/api/v1/incidents", {
    headers: { "Idempotency-Key": randomUUID() },
    data: { title: `Log evidence ${randomUUID().slice(0, 8)}`, severity: "SEV-3", owner: "log-verification", impact: "Synthetic log panel check", environment: "development", service: "payment-service" },
  });
  expect(response.status()).toBe(201);
  const incident = await response.json();
  const live = await request.get(`/api/v1/incidents/${incident.id}/logs?window=24h`);
  expect(live.status()).toBe(200);
  expect((await live.json()).configured).toBe(false);
  expect((await request.get(`/api/v1/incidents/${incident.id}/logs?window=forever`)).status()).toBe(422);

  await page.goto(`/incidents/${incident.id}`);
  await expect(page.getByText("Splunk log search is not configured")).toBeVisible();
  let populated = false;
  await page.route(`**/api/v1/incidents/${incident.id}/logs?**`, (route) => {
    if (new URL(route.request().url()).searchParams.get("window") === "72h") {
      return route.fulfill({ status: 503, contentType: "application/json", body: JSON.stringify({ error: "log search unavailable" }) });
    }
    return route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({
      configured: true,
      source: "splunk",
      window_start: "2026-09-28T00:00:00Z",
      window_end: "2026-09-28T02:00:00Z",
      items: populated ? [{ at: "2026-09-28 01:00:00 UTC", level: "ERROR", service: "payment-service", message: "Database connection timeout", trace_id: "0123456789abcdef", transaction_id: "txn-log-test", error_code: "DB_TIMEOUT" }] : [],
    }) });
  });
  await page.getByLabel("Log evidence window").selectOption("2h");
  await expect(page.getByText("No indexed logs in this window")).toBeVisible();
  populated = true;
  await page.locator("section.panel").filter({ has: page.getByRole("heading", { name: "Logs evidence" }) }).getByRole("button", { name: "Refresh" }).click();
  await expect(page.getByText("Database connection timeout")).toBeVisible();
  await expect(page.getByText("DB_TIMEOUT")).toBeVisible();
  await page.screenshot({ path: "/private/tmp/telcopulse-incident-logs-desktop.png", fullPage: true });
  await page.getByLabel("Log evidence window").selectOption("72h");
  await expect(page.getByText("log search unavailable")).toBeVisible();
  await page.setViewportSize({ width: 390, height: 844 });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  await page.screenshot({ path: "/private/tmp/telcopulse-incident-logs-mobile.png", fullPage: true });
});

test("incident traces show bounded dependency evidence and source failures", async ({ page, request }) => {
  const response = await request.post("/api/v1/incidents", {
    headers: { "Idempotency-Key": randomUUID() },
    data: { title: `Trace evidence ${randomUUID().slice(0, 8)}`, severity: "SEV-3", environment: "development", service: "payment-service" },
  });
  expect(response.status()).toBe(201);
  const incident = await response.json();
  const live = await request.get(`/api/v1/incidents/${incident.id}/traces?window=2h`);
  expect(live.status()).toBe(200);
  expect((await live.json()).source).toBe("jaeger");
  expect((await request.get(`/api/v1/incidents/${incident.id}/traces?window=forever`)).status()).toBe(422);

  await page.route(`**/api/v1/incidents/${incident.id}/traces?**`, (route) => {
    const window = new URL(route.request().url()).searchParams.get("window");
    if (window === "72h") return route.fulfill({ status: 503, contentType: "application/json", body: JSON.stringify({ error: "trace search unavailable" }) });
    return route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({
      configured: true, source: "jaeger", window_start: "2026-09-28T00:00:00Z", window_end: "2026-09-28T02:00:00Z",
      items: window === "24h" ? [] : [{ trace_id: "0123456789abcdef0123456789abcdef", started_at: "2026-09-28T01:00:00Z", nodes: [
        { name: "payment-service", kind: "service", max_duration_ms: 980.2, error: true },
        { name: "postgresql", kind: "database", max_duration_ms: 940.1, error: true },
      ], edges: [{ from: "payment-service", to: "postgresql" }] }],
    }) });
  });
  await page.goto(`/incidents/${incident.id}`);
  await expect(page.getByText("No matching traces in this window")).toBeVisible();
  await page.getByLabel("Trace evidence window").selectOption("2h");
  await expect(page.getByText("payment-service → postgresql")).toBeVisible();
  await expect(page.getByText("Max span 940.1 ms")).toBeVisible();
  await expect(page.getByRole("link", { name: "Open trace" })).toHaveAttribute("href", "http://localhost:16686/trace/0123456789abcdef0123456789abcdef");
  await page.screenshot({ path: "/private/tmp/telcopulse-incident-traces-desktop.png", fullPage: true });
  await page.getByLabel("Trace evidence window").selectOption("72h");
  await expect(page.getByText("trace search unavailable")).toBeVisible();
  await page.setViewportSize({ width: 390, height: 844 });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  await page.screenshot({ path: "/private/tmp/telcopulse-incident-traces-mobile.png", fullPage: true });
});

test("incident infrastructure evidence distinguishes unconfigured, empty, measured and source failure", async ({ page, request }) => {
  const response = await request.post("/api/v1/incidents", {
    headers: { "Idempotency-Key": randomUUID() },
    data: { title: `Container evidence ${randomUUID().slice(0, 8)}`, severity: "SEV-3", environment: "development", service: "payment-service" },
  });
  expect(response.status()).toBe(201);
  const incident = await response.json();
  const live = await request.get(`/api/v1/incidents/${incident.id}/infrastructure?window=2h`);
  expect(live.status()).toBe(200);
  expect(await live.json()).toMatchObject({ configured: false, source: "datadog", limited: false, series: [] });
  expect((await request.get(`/api/v1/incidents/${incident.id}/infrastructure?window=forever`)).status()).toBe(422);

  let phase: "unconfigured" | "empty" | "measured" = "unconfigured";
  await page.route(`**/api/v1/incidents/${incident.id}/infrastructure?**`, (route) => {
    const window = new URL(route.request().url()).searchParams.get("window");
    if (window === "72h") return route.fulfill({ status: 503, contentType: "application/json", body: JSON.stringify({ error: "infrastructure query unavailable" }) });
    return route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({
      configured: phase !== "unconfigured", source: "datadog", window_start: "2026-09-28T00:00:00Z", window_end: "2026-09-28T02:00:00Z", limited: false,
      series: phase === "measured" ? [{ key: "restarts", label: "Container restarts", unit: "count", namespace: "telcopulse-dev", pod: "payment-abc", points: [
        { at: "2026-09-28T01:00:00Z", value: 0 }, { at: "2026-09-28T01:05:00Z", value: 1 },
      ] }] : [],
    }) });
  });
  await page.goto(`/incidents/${incident.id}`);
  const panel = page.locator("section.panel").filter({ has: page.getByRole("heading", { name: "Container and Kubernetes evidence" }) });
  await expect(panel.getByText("Datadog is not configured")).toBeVisible();
  phase = "empty";
  await panel.getByRole("button", { name: "Refresh" }).click();
  await expect(panel.getByText("No matching container measurements")).toBeVisible();
  phase = "measured";
  await panel.getByRole("button", { name: "Refresh" }).click();
  await expect(panel.getByText("telcopulse-dev / payment-abc")).toBeVisible();
  await expect(panel.getByText("Container restarts")).toBeVisible();
  await expect(panel.getByText("1", { exact: true })).toBeVisible();
  await page.screenshot({ path: "/private/tmp/telcopulse-datadog-evidence-desktop.png", fullPage: true });
  await page.getByLabel("Infrastructure evidence window").selectOption("72h");
  await expect(panel.getByText("infrastructure query unavailable")).toBeVisible();
  await page.setViewportSize({ width: 390, height: 844 });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  await page.screenshot({ path: "/private/tmp/telcopulse-datadog-evidence-mobile.png", fullPage: true });
});

test("incident-period metrics preserve source scope and missing-data states", async ({ page, request }) => {
  const response = await request.post("/api/v1/incidents", {
    headers: { "Idempotency-Key": randomUUID() },
    data: { title: `Metric evidence ${randomUUID().slice(0, 8)}`, severity: "SEV-3", environment: "development", service: "payment-service" },
  });
  expect(response.status()).toBe(201);
  const incident = await response.json();
  const live = await request.get(`/api/v1/incidents/${incident.id}/metrics?window=2h`);
  expect(live.status()).toBe(200);
  const observed = await live.json();
  expect(observed).toMatchObject({ configured: true, source: "prometheus" });
  expect(observed.series).toHaveLength(5);
  expect(observed.series.find((series: { key: string }) => series.key === "business_success").scope).toBe("environment");
  expect(observed.series.find((series: { key: string }) => series.key === "http_rps").scope).toBe("shared-runtime");
  expect((await request.get(`/api/v1/incidents/${incident.id}/metrics?window=forever`)).status()).toBe(422);

  let populated = false;
  await page.route(`**/api/v1/incidents/${incident.id}/metrics?**`, (route) => {
    const window = new URL(route.request().url()).searchParams.get("window");
    if (window === "72h") return route.fulfill({ status: 503, contentType: "application/json", body: JSON.stringify({ error: "metric query unavailable" }) });
    const sample = populated ? [{ at: "2026-09-28T01:00:00Z", value: 0.92 }, { at: "2026-09-28T01:05:00Z", value: 0.87 }] : [];
    return route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({
      configured: true, source: "prometheus", window_start: "2026-09-28T00:45:00Z", window_end: "2026-09-28T02:00:00Z", step_seconds: 60,
      series: [
        { key: "business_success", label: "Business success", unit: "fraction", scope: "environment", points: sample },
        { key: "business_tpm", label: "Transactions per minute", unit: "transactions/min", scope: "environment", points: [] },
        { key: "http_rps", label: "HTTP requests per second", unit: "requests/s", scope: "shared-runtime", points: [] },
        { key: "http_error", label: "HTTP 5xx fraction", unit: "fraction", scope: "shared-runtime", points: [] },
        { key: "http_p95", label: "HTTP P95 latency", unit: "ms", scope: "shared-runtime", points: [] },
      ],
    }) });
  });
  await page.goto(`/incidents/${incident.id}`);
  await expect(page.getByText("No measured samples in this period")).toBeVisible();
  populated = true;
  const panel = page.locator("section.panel").filter({ has: page.getByRole("heading", { name: "Incident-period metrics" }) });
  await panel.getByRole("button", { name: "Refresh" }).click();
  await expect(panel.getByText("87.0%")).toBeVisible();
  await expect(panel.getByText("Incident environment · synthetic business outcomes").first()).toBeVisible();
  await expect(panel.getByText("Affected service · shared development/staging process").first()).toBeVisible();
  await page.screenshot({ path: "/private/tmp/telcopulse-incident-metrics-desktop.png", fullPage: true });
  await page.getByLabel("Metric evidence window").selectOption("72h");
  await expect(page.getByText("metric query unavailable")).toBeVisible();
  await page.setViewportSize({ width: 390, height: 844 });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  await page.screenshot({ path: "/private/tmp/telcopulse-incident-metrics-mobile.png", fullPage: true });
});

test("incident register opens persisted investigation and paged audit history", async ({
  page,
  request,
}) => {
  const fields = {
    title: `Browser incident ${randomUUID().slice(0, 8)}`,
    severity: "SEV-4",
    owner: "browser-verification",
    impact: "Synthetic verification only",
    evidence: [
      {
        kind: "metric",
        summary: "Recorded evidence",
        url: "https://example.com/evidence",
      },
    ],
    action_items: [
      { title: "Review synthetic run", owner: "local-test", done: false },
    ],
  };
  const response = await request.post("/api/v1/incidents", {
    headers: { "Idempotency-Key": randomUUID() },
    data: { ...fields, environment: "development", service: "payment-service" },
  });
  expect(response.status()).toBe(201);
  let item = await response.json();
  for (let n = 0; n < 10; n++) {
    const updated = await request.put(`/api/v1/incidents/${item.id}`, {
      data: {
        ...fields,
        state: "Detected",
        expected_version: item.version,
        note: `Verification edit ${n + 1}`,
      },
    });
    expect(updated.status()).toBe(200);
    item = await updated.json();
  }
  await page.goto("/incidents");
  await page.getByLabel("Incident state").selectOption("Detected");
  await page.getByRole("link", { name: fields.title, exact: true }).click();
  await expect(page.getByRole("heading", { name: fields.title })).toBeVisible();
  await expect(page.getByText("Affected users: Unknown")).toBeVisible();
  await expect(
    page.getByText("Recorded evidence", { exact: true }).last(),
  ).toBeVisible();
  await expect(
    page.getByRole("link", { name: "Open evidence" }),
  ).toHaveAttribute("href", "https://example.com/evidence");
  await expect(page.locator(".incident-timeline li")).toHaveCount(10);
  await page.getByRole("button", { name: "Later revisions" }).click();
  await expect(page.locator(".incident-timeline li")).toHaveCount(1);
  await expect(page.getByText("Revision 11 · Details updated")).toBeVisible();
  await page.getByRole("button", { name: "Earlier revisions" }).click();
  await expect(page.locator(".incident-timeline li")).toHaveCount(10);
  await page.evaluate(() => window.scrollTo(0, 0));
  await page.screenshot({
    path: "/private/tmp/telcopulse-incident-desktop.png",
    fullPage: true,
  });
  await page.setViewportSize({ width: 390, height: 844 });
  await expect(page.getByRole("heading", { name: fields.title })).toBeVisible();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBe(true);
  await page.evaluate(() => window.scrollTo(0, 0));
  await page.screenshot({
    path: "/private/tmp/telcopulse-incident-mobile.png",
    fullPage: true,
  });
});

test("incident list keeps filters usable after API failure", async ({
  page,
}) => {
  await page.route("**/api/v1/incidents?**", (route) =>
    route.fulfill({
      status: 503,
      contentType: "application/json",
      body: JSON.stringify({ error: "Incident backend unavailable" }),
    }),
  );
  await page.goto("/incidents");
  await expect(page.getByText("Incident backend unavailable")).toBeVisible();
  await expect(page.getByLabel("Incident state")).toBeEnabled();
  await page.unroute("**/api/v1/incidents?**");
  await page.getByRole("button", { name: "Try again" }).click();
  await expect(page.getByRole("table")).toBeVisible();
});

test("operator edits enforce lifecycle gates and reject stale drafts", async ({
  page,
  request,
}) => {
  const fields = {
    title: `Editor verification ${randomUUID().slice(0, 8)}`,
    severity: "SEV-4",
    owner: "",
    impact: "Synthetic incident",
    evidence: [{ kind: "note", summary: "Keep this evidence" }],
    action_items: [
      { title: "Keep this action", owner: "local-test", done: false },
    ],
    affected_users: 42,
  };
  const create = await request.post("/api/v1/incidents", {
    headers: { "Idempotency-Key": randomUUID() },
    data: { ...fields, environment: "development", service: "payment-service" },
  });
  expect(create.status()).toBe(201);
  const incident = await create.json();
  await page.goto(`/incidents/${incident.id}`);
  await page
    .getByRole("button", { name: "Edit incident", exact: true })
    .click();
  const dialog = page.getByRole("dialog");
  await dialog
    .getByLabel("State", { exact: true })
    .selectOption("Acknowledged");
  await dialog
    .getByRole("button", { name: "Save incident", exact: true })
    .click();
  await expect(dialog.getByText("Required for Acknowledged")).toBeVisible();
  await expect(
    dialog.getByText("Explain this change for the audit history"),
  ).toBeVisible();
  await dialog.getByLabel("Owner", { exact: true }).fill("on-call");
  await dialog.getByLabel("Audit note").fill("Accept incident");
  await dialog
    .getByRole("button", { name: "Save incident", exact: true })
    .click();
  await expect(dialog).not.toBeVisible();
  let saved = (
    await (await request.get(`/api/v1/incidents/${incident.id}`)).json()
  ).incident;
  expect(saved.state).toBe("Acknowledged");
  expect(saved.affected_users).toBe(42);
  expect(saved.error_rate).toBeNull();
  expect(saved.latency_ms).toBeNull();
  expect(saved.evidence).toEqual(fields.evidence);
  expect(saved.action_items).toMatchObject([{ ...fields.action_items[0], priority: "P2", status: "Open" }]);
  for (const state of [
    "Investigating",
    "Identified",
    "Mitigating",
    "Monitoring",
    "Resolved",
    "Postmortem",
  ]) {
    await page
      .getByRole("button", { name: "Edit incident", exact: true })
      .click();
    await dialog.getByLabel("State", { exact: true }).selectOption(state);
    await dialog
      .getByLabel("Root cause", { exact: true })
      .fill("Controlled test");
    await dialog
      .getByLabel("Mitigation", { exact: true })
      .fill("Synthetic recovery");
    await dialog
      .getByLabel("Resolution", { exact: true })
      .fill("Verified recovery");
    if (state === "Resolved") {
      await dialog.getByLabel("Recovery observation").fill("A successful synthetic purchase completed after mitigation");
      await dialog.getByLabel("Evidence URL", { exact: true }).fill("http://localhost:3001/transactions");
      await dialog.getByLabel("Observed at").fill(new Date(Date.now() + 10000).toLocaleString("sv-SE").replace(" ", "T"));
    }
    await dialog.getByLabel("Postmortem notes").fill("Review complete");
    await dialog.getByLabel("Audit note").fill(`Move to ${state}`);
    await dialog
      .getByRole("button", { name: "Save incident", exact: true })
      .click();
    await expect(dialog).not.toBeVisible();
  }
  saved = (await (await request.get(`/api/v1/incidents/${incident.id}`)).json())
    .incident;
  expect(saved.state).toBe("Postmortem");
  expect(saved.version).toBe(8);
  await page
    .getByRole("button", { name: "Edit incident", exact: true })
    .click();
  await dialog.getByLabel("Title", { exact: true }).fill("My unsaved draft");
  await dialog.getByLabel("Audit note").fill("Attempt stale change");
  const {
    id,
    environment,
    service,
    version,
    created_at,
    detected_at,
    updated_at,
    owning_team,
    escalation_level,
    escalated_at,
    acknowledged_at,
    resolved_at,
    recovery_validation,
    ...editable
  } = saved;
  void id;
  void environment;
  void service;
  void created_at;
  void detected_at;
  void updated_at;
  void owning_team;
  void escalation_level;
  void escalated_at;
  void acknowledged_at;
  void resolved_at;
  void recovery_validation;
  const concurrent = await request.put(`/api/v1/incidents/${incident.id}`, {
    data: {
      ...editable,
      title: "Another operator updated this",
      expected_version: version,
      note: "Concurrent edit",
    },
  });
  expect(concurrent.status()).toBe(200);
  await dialog
    .getByRole("button", { name: "Save incident", exact: true })
    .click();
  await expect(
    dialog.getByText(/This incident changed while you were editing/),
  ).toBeVisible();
  await expect(dialog.getByLabel("Title", { exact: true })).toHaveValue(
    "My unsaved draft",
  );
  await expect(
    dialog.getByRole("button", { name: "Save incident", exact: true }),
  ).toBeDisabled();
  await dialog
    .getByRole("button", { name: "Discard draft and reload latest" })
    .click();
  await expect(dialog.getByLabel("Title", { exact: true })).toHaveValue(
    "Another operator updated this",
  );
  await page.setViewportSize({ width: 390, height: 844 });
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBe(true);
  await page.screenshot({
    path: "/private/tmp/telcopulse-incident-editor.png",
  });
  await dialog.getByRole("button", { name: "Cancel", exact: true }).click();
});

test("manual creation recovers a committed request after a lost response", async ({
  page,
  request,
}) => {
  const title = `Manual incident ${randomUUID().slice(0, 8)}`;
  let first = true;
  let createdID = "";
  const keys: string[] = [];
  await page.route("**/api/v1/incidents", async (route) => {
    if (route.request().method() !== "POST") return route.continue();
    keys.push(route.request().headers()["idempotency-key"]);
    if (first) {
      first = false;
      const response = await route.fetch();
      expect(response.status()).toBe(201);
      createdID = (await response.json()).id;
      await route.abort("failed");
    } else await route.continue();
  });
  await page.goto("/incidents");
  await page
    .getByRole("button", { name: "Create incident", exact: true })
    .click();
  const dialog = page.getByRole("dialog");
  await dialog.getByLabel("Title", { exact: true }).fill(title);
  await dialog.getByLabel("Affected service").selectOption("payment-service");
  await dialog.getByLabel("Severity", { exact: true }).selectOption("SEV-4");
  await dialog
    .getByLabel("Impact", { exact: true })
    .fill("Synthetic browser verification");
  await page.setViewportSize({ width: 390, height: 844 });
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBe(true);
  await page.screenshot({
    path: "/private/tmp/telcopulse-create-incident.png",
  });
  await dialog
    .getByRole("button", { name: "Record incident", exact: true })
    .click();
  await expect(dialog.getByText(/The result is uncertain/)).toBeVisible();
  await expect(dialog.getByLabel("Title", { exact: true })).toBeDisabled();
  await dialog.getByRole("button", { name: "Close and keep request" }).click();
  await page.getByRole("button", { name: "Resume incident creation" }).click();
  await dialog.getByRole("button", { name: "Retry same request" }).click();
  await expect(page).toHaveURL(new RegExp(`/incidents/${createdID}$`));
  await expect(
    page.getByRole("heading", { name: title, exact: true }),
  ).toBeVisible();
  expect(keys).toHaveLength(2);
  expect(keys[0]).toBe(keys[1]);
  const detail = await (
    await request.get(`/api/v1/incidents/${createdID}`)
  ).json();
  expect(detail.incident.version).toBe(1);
  expect(detail.history).toHaveLength(1);
});

test("impact editing preserves unknowns, measured zero and audit snapshots", async ({
  page,
  request,
}) => {
  const created = await request.post("/api/v1/incidents", {
    headers: { "Idempotency-Key": randomUUID() },
    data: {
      title: "Impact measurement verification",
      severity: "SEV-4",
      service: "payment-service",
      environment: "development",
    },
  });
  expect(created.status()).toBe(201);
  const { id } = await created.json();
  await page.goto(`/incidents/${id}`);
  await page
    .getByRole("button", { name: "Edit incident", exact: true })
    .click();
  const dialog = page.getByRole("dialog");
  await dialog.getByLabel("Affected users", { exact: true }).fill("0");
  await dialog.getByLabel("Affected transactions", { exact: true }).fill("37");
  await dialog.getByLabel("Error rate (0–1)", { exact: true }).fill("1.5");
  await dialog.getByLabel("Success rate (0–1)", { exact: true }).fill("0.95");
  await dialog.getByLabel("Latency (ms)", { exact: true }).fill("4200");
  await dialog
    .getByLabel("Related deployment", { exact: true })
    .fill("payment-v1.4.0");
  await dialog.getByLabel("Audit note").fill("Record observed impact");
  await dialog
    .getByRole("button", { name: "Save incident", exact: true })
    .click();
  await expect(
    dialog.getByLabel("Error rate (0–1)", { exact: true }),
  ).toHaveAttribute("aria-invalid", "true");
  await dialog.getByLabel("Error rate (0–1)", { exact: true }).fill("0.05");
  await dialog
    .getByRole("button", { name: "Save incident", exact: true })
    .click();
  await expect(dialog).not.toBeVisible();
  let detail = await (await request.get(`/api/v1/incidents/${id}`)).json();
  expect(detail.incident).toMatchObject({
    affected_users: 0,
    affected_transactions: 37,
    error_rate: 0.05,
    success_rate: 0.95,
    latency_ms: 4200,
    related_deployment: "payment-v1.4.0",
  });
  expect(detail.history[1].before.affected_users).toBeNull();
  expect(detail.history[1].after.affected_users).toBe(0);
  await page
    .getByRole("button", { name: "Edit incident", exact: true })
    .click();
  await dialog.getByLabel("Affected users", { exact: true }).fill("");
  await dialog.getByLabel("Latency (ms)", { exact: true }).fill("");
  await dialog.getByLabel("Audit note").fill("Withdraw uncertain measurements");
  await dialog
    .getByRole("button", { name: "Save incident", exact: true })
    .click();
  await expect(dialog).not.toBeVisible();
  detail = await (await request.get(`/api/v1/incidents/${id}`)).json();
  expect(detail.incident.affected_users).toBeNull();
  expect(detail.incident.latency_ms).toBeNull();
  expect(detail.incident.affected_transactions).toBe(37);
  await expect(page.getByText("Affected users: Unknown")).toBeVisible();
});

test("evidence and action edits validate, persist and remain auditable", async ({
  page,
  request,
}) => {
  const create = await request.post("/api/v1/incidents", {
    headers: { "Idempotency-Key": randomUUID() },
    data: {
      title: "Evidence collection verification",
      severity: "SEV-4",
      service: "payment-service",
      environment: "development",
    },
  });
  expect(create.status()).toBe(201);
  const { id } = await create.json();
  await page.goto(`/incidents/${id}`);
  await page
    .getByRole("button", { name: "Edit incident", exact: true })
    .click();
  const dialog = page.getByRole("dialog");
  await dialog
    .getByRole("button", { name: "Add evidence", exact: true })
    .click();
  await dialog
    .getByLabel("Evidence kind", { exact: true })
    .selectOption("trace");
  await dialog
    .getByLabel("Evidence summary", { exact: true })
    .fill("Failed payment trace");
  await dialog
    .getByLabel("Evidence URL (optional)", { exact: true })
    .fill("javascript:alert(1)");
  await dialog
    .getByRole("button", { name: "Add action item", exact: true })
    .click();
  await dialog
    .getByLabel("Action title", { exact: true })
    .fill("Review dependency timeout");
  await dialog.getByLabel("Audit note").fill("Attach investigation evidence");
  await dialog
    .getByRole("button", { name: "Save incident", exact: true })
    .click();
  await expect(
    dialog.getByText("Use an HTTP(S) URL without credentials"),
  ).toBeVisible();
  await expect(dialog.getByText("An action owner is required")).toBeVisible();
  await dialog
    .getByLabel("Evidence URL (optional)", { exact: true })
    .fill("https://example.com/trace");
  await dialog.getByLabel("Action owner", { exact: true }).fill("payment-team");
  await dialog.getByLabel("Action priority", { exact: true }).selectOption("P1");
  await dialog.getByLabel("Action status", { exact: true }).selectOption("In Progress");
  await dialog
    .getByLabel("Due timestamp (ISO 8601, optional)", { exact: true })
    .fill("2026-10-01T09:00:00+07:00");
  await dialog
    .getByRole("button", { name: "Save incident", exact: true })
    .click();
  await expect(dialog).not.toBeVisible();
  await expect(page.getByText("In Progress", { exact: true })).toBeVisible();
  await expect(page.getByText("P1", { exact: true })).toBeVisible();
  let detail = await (await request.get(`/api/v1/incidents/${id}`)).json();
  expect(detail.incident.evidence[0]).toMatchObject({
    kind: "trace",
    summary: "Failed payment trace",
    url: "https://example.com/trace",
  });
  expect(detail.incident.action_items[0]).toMatchObject({
    owner: "payment-team",
    done: false,
    priority: "P1",
    status: "In Progress",
  });
  expect(new Date(detail.incident.action_items[0].due_at).toISOString()).toBe(
    "2026-10-01T02:00:00.000Z",
  );
  await page
    .getByRole("button", { name: "Edit incident", exact: true })
    .click();
  await dialog.getByLabel("Action status", { exact: true }).selectOption("Completed");
  await dialog.getByRole("button", { name: "Remove evidence 1" }).click();
  await dialog
    .getByLabel("Audit note")
    .fill("Complete follow-up; withdraw evidence");
  await page.setViewportSize({ width: 390, height: 844 });
  await dialog
    .getByLabel("Action title", { exact: true })
    .scrollIntoViewIfNeeded();
  await page.screenshot({
    path: "/private/tmp/telcopulse-incident-collections.png",
  });
  await dialog
    .getByRole("button", { name: "Save incident", exact: true })
    .click();
  await expect(dialog).not.toBeVisible();
  await expect(page.getByText("Completed", { exact: true })).toBeVisible();
  detail = await (await request.get(`/api/v1/incidents/${id}`)).json();
  expect(detail.incident.evidence).toEqual([]);
  expect(detail.incident.action_items[0].done).toBe(true);
  expect(detail.incident.action_items[0].status).toBe("Completed");
  expect(detail.incident.action_items[0].priority).toBe("P1");
  expect(detail.history[2].before.evidence).toHaveLength(1);
  expect(detail.history[2].before.action_items[0].done).toBe(false);
  expect(detail.history[2].before.action_items[0].status).toBe("In Progress");
  expect(detail.history[2].after.action_items[0].done).toBe(true);
  expect(detail.history[2].after.action_items[0].status).toBe("Completed");
});

test("overview shows persisted active incidents and never treats failure as zero", async ({
  page,
  request,
}) => {
  const title = `Overview critical ${randomUUID().slice(0, 8)}`;
  const response = await request.post("/api/v1/incidents", {
    headers: { "Idempotency-Key": randomUUID() },
    data: {
      title,
      severity: "SEV-1",
      environment: "development",
      service: "payment-service",
    },
  });
  expect(response.status()).toBe(201);
  const item = await response.json();
  const snapshot = await request.get(
    "/api/v1/incidents/overview?environment=development",
  );
  expect(snapshot.status()).toBe(200);
  const data = await snapshot.json();
  expect(data.active).toBeGreaterThan(0);
  expect(data.critical).toBeGreaterThan(0);
  expect(data.items[0].id).toBe(item.id);
  await page.goto("/");
  const metric = page
    .locator(".metric")
    .filter({ has: page.getByText("Active incidents", { exact: true }) });
  await expect(metric.locator("strong")).toHaveText(String(data.active));
  await page.getByRole("link", { name: title, exact: true }).click();
  await expect(page).toHaveURL(new RegExp(`/incidents/${item.id}$`));
  await page.route("**/api/v1/incidents/overview?**", (route) =>
    route.fulfill({
      status: 503,
      contentType: "application/json",
      body: JSON.stringify({ error: "Incident summary unavailable" }),
    }),
  );
  await page.goto("/");
  await expect(metric.locator("strong")).toHaveText("—");
  await expect(page.getByText("Incident summary unavailable")).toBeVisible();
  await page.unroute("**/api/v1/incidents/overview?**");
  await page.getByRole("button", { name: "Try again", exact: true }).click();
  await expect(
    page.getByRole("link", { name: title, exact: true }),
  ).toBeVisible();
  await page.screenshot({
    path: "/private/tmp/telcopulse-overview-incidents.png",
    fullPage: true,
  });
});
