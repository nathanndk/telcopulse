import { test, expect, type APIRequestContext } from "@playwright/test";
import { randomUUID } from "node:crypto";

type PurchaseOutcome = { id: string; status: string; error_code?: string; duration_ms: number };

async function terminalOutcome(request: APIRequestContext, initial: PurchaseOutcome): Promise<PurchaseOutcome> {
  if (initial.status !== "PROCESSING") return initial;
  await expect.poll(async () => {
    const response = await request.get(`/api/v1/transactions/${initial.id}`);
    return (await response.json()).status;
  }, { timeout: 20000 }).not.toBe("PROCESSING");
  return (await (await request.get(`/api/v1/transactions/${initial.id}`)).json()) as PurchaseOutcome;
}

test("bad deployment links the failed purchase to a reported release and rollback", async ({ page, request }) => {
  let runID = "";
  try {
    await page.goto("/simulator");
    await page.getByRole("banner").getByLabel("Environment", { exact: true }).selectOption("staging");
    await page.getByRole("button", { name: "Inject failure", exact: true }).click();
    const dialog = page.getByRole("dialog");
    await dialog.getByLabel("Failure scenario").selectOption("bad-deployment");
    await expect(dialog.getByLabel("Database delay (ms)")).toHaveCount(0);
    await dialog.getByLabel("Selection percentage").fill("100");
    await dialog.getByLabel("Duration in seconds").fill("90");
    await dialog.getByLabel("Simulation reason").fill("Browser synthetic release verification");
    const started = page.waitForResponse((response) => response.url().endsWith("/api/v1/simulations") && response.request().method() === "POST");
    await dialog.getByRole("button", { name: "Start failure simulation" }).click();
    const response = await started;
    expect(response.status()).toBe(201);
    const run = await response.json();
    runID = run.id;
    expect(run.deployment_id).toMatch(/^sim-deploy-/);
    await expect(dialog).not.toBeVisible();
    await expect.poll(async () => (await (await request.get(`/api/v1/simulations/${runID}`)).json()).deployment_report).toBe("Completed");
    await page.getByRole("button", { name: "Start synthetic transaction" }).click();
    await expect(page.getByText("BAD_DEPLOYMENT_PAYMENT_FAILURE:", { exact: false })).toBeVisible();
    const row = page.getByRole("row").filter({ hasText: runID });
    await row.getByRole("link", { name: runID, exact: true }).click();
    await expect(page.getByText("Synthetic payment release 1.4.0-sim-bad", { exact: false })).toBeVisible();
    await page.getByRole("link", { name: run.deployment_id }).click();
    await expect(page.getByRole("heading", { name: "payment-service 1.4.0-sim-bad" })).toBeVisible();
    await expect(page.locator(".incident-timeline li")).toHaveCount(1);
    await page.goto("/simulator");
    await page.getByRole("banner").getByLabel("Environment", { exact: true }).selectOption("staging");
    await page.getByRole("row").filter({ hasText: runID }).getByRole("button", { name: "Stop simulation" }).click();
    await dialog.getByLabel("Stop reason").fill("Browser synthetic rollback verification");
    await dialog.getByRole("button", { name: "Confirm stop" }).click();
    await expect.poll(async () => (await (await request.get(`/api/v1/deployments/${run.deployment_id}`)).json()).status).toBe("Rolled Back");
    await page.getByRole("button", { name: "Start synthetic transaction" }).click();
    await expect(page.getByText("Package activation completed", { exact: true })).toBeVisible();
  } finally {
    if (runID) {
      const stopped = await request.post(`/api/v1/simulations/${runID}/stop`, { data: { reason: "Ensure browser synthetic release cleanup" } });
      expect(stopped.ok()).toBeTruthy();
    }
  }
});

test("simulation controls recover a lost start response, inject failure and stop", async ({
  page,
  request,
}) => {
  const reason = `Browser controlled failure ${randomUUID().slice(0, 8)}`;
  let runID = "";
  const keys: string[] = [];
  try {
    await page.goto("/simulator");
    await page
      .getByRole("banner")
      .getByLabel("Environment", { exact: true })
      .selectOption("staging");
    await page
      .getByRole("button", { name: "Inject failure", exact: true })
      .click();
    const dialog = page.getByRole("dialog");
    await dialog.getByLabel("Selection percentage").fill("100");
    await dialog.getByLabel("Duration in seconds").fill("180");
    await dialog
      .getByRole("button", { name: "Start failure simulation", exact: true })
      .click();
    await expect(
      dialog.getByText("Explain why this failure is needed"),
    ).toBeVisible();
    await dialog.getByLabel("Simulation reason").fill(reason);
    await page.route("**/api/v1/simulations", async (route) => {
      if (route.request().method() !== "POST") return route.continue();
      keys.push(route.request().headers()["idempotency-key"]);
      const response = await route.fetch();
      expect(response.status()).toBe(keys.length === 1 ? 201 : 200);
      const run = await response.json();
      runID = run.id;
      if (keys.length === 1) await route.abort("failed");
      else await route.fulfill({ response });
    });
    await dialog
      .getByRole("button", { name: "Start failure simulation", exact: true })
      .click();
    await expect(
      dialog.getByRole("button", { name: "Retry same start request" }),
    ).toBeEnabled();
    await expect(dialog.getByLabel("Selection percentage")).toBeDisabled();
    await dialog
      .getByRole("button", { name: "Close", exact: true })
      .first()
      .click();
    // A pending command retains its original environment when the global filter changes.
    await page
      .getByRole("banner")
      .getByLabel("Environment", { exact: true })
      .selectOption("development");
    await page
      .getByRole("button", { name: "Resume simulation request" })
      .click();
    await expect(
      dialog.getByText("staging · payment-decline · local-operator", {
        exact: true,
      }),
    ).toBeVisible();
    await dialog
      .getByRole("button", { name: "Retry same start request" })
      .click();
    await expect(dialog).not.toBeVisible();
    expect(keys).toHaveLength(2);
    expect(keys[0]).toBe(keys[1]);
    await page.unroute("**/api/v1/simulations");
    await page
      .getByRole("banner")
      .getByLabel("Environment", { exact: true })
      .selectOption("staging");
    const runRow = page.getByRole("row").filter({ hasText: runID });
    await expect(runRow.getByText("Active", { exact: true })).toBeVisible();
    await expect(
      page.getByRole("button", { name: "Inject failure", exact: true }),
    ).toBeDisabled();
    await page
      .getByRole("button", { name: "Start synthetic transaction" })
      .click();
    await expect(
      page.getByText("Purchase failed", { exact: true }),
    ).toBeVisible();
    await expect(
      page.getByText("SIMULATED_PAYMENT_DECLINED:", { exact: false }),
    ).toBeVisible();
    const failedHref = await page
      .getByRole("link", { name: "Investigate transaction", exact: true })
      .getAttribute("href");
    await runRow.getByRole("button", { name: "Stop simulation" }).click();
    await dialog
      .getByLabel("Stop reason")
      .fill("Browser verification mitigation");
    await dialog.getByRole("button", { name: "Confirm stop" }).click();
    await expect(dialog).not.toBeVisible();
    await expect(runRow.getByText("Stopped", { exact: true })).toBeVisible();
    await page
      .getByRole("button", { name: "Start synthetic transaction" })
      .click();
    await expect(
      page.getByText("Package activation completed", { exact: true }),
    ).toBeVisible();
    const runs = await (
      await request.get("/api/v1/simulations?environment=staging")
    ).json();
    expect(
      runs.filter((run: { reason: string }) => run.reason === reason),
    ).toHaveLength(1);
    await runRow.getByRole("link", { name: runID, exact: true }).click();
    await expect(
      page.getByRole("heading", {
        name: "Simulation investigation",
        exact: true,
      }),
    ).toBeVisible();
    await expect(
      page.getByText("Simulation started", { exact: true }),
    ).toBeVisible();
    await expect(
      page.getByText("Browser verification mitigation", { exact: true }),
    ).toBeVisible();
    await expect(
      page.getByText("Selected for injection: 1", { exact: true }),
    ).toBeVisible();
    const detail = await (
      await request.get(`/api/v1/simulations/${runID}`)
    ).json();
    expect(detail.audit).toHaveLength(2);
    expect(detail.audit.map((entry: { actor: string }) => entry.actor)).toEqual(
      ["local-operator", "local-operator"],
    );
    await page.screenshot({
      path: "/private/tmp/telcopulse-simulation-investigation.png",
      fullPage: true,
    });
    const failedID = failedHref!.split("/").pop()!;
    await page
      .getByRole("link", { name: `Investigate ${failedID}`, exact: true })
      .click();
    await expect(
      page.getByRole("heading", {
        name: "Purchase investigation",
        exact: true,
      }),
    ).toBeVisible();
    await page.route(`**/api/v1/simulations/${runID}?**`, (route) =>
      route.fulfill({
        status: 503,
        contentType: "application/json",
        body: JSON.stringify({ error: "simulation detail unavailable" }),
      }),
    );
    await page.goto(`/simulations/${runID}`);
    await expect(
      page.getByText("simulation detail unavailable", { exact: true }),
    ).toBeVisible();
    await page.unroute(`**/api/v1/simulations/${runID}?**`);
    await page.getByRole("button", { name: "Try again", exact: true }).click();
    await expect(
      page.getByRole("heading", { name: "Simulation audit", exact: true }),
    ).toBeVisible();
    await page.setViewportSize({ width: 390, height: 844 });
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBeTruthy();
    await page.evaluate(() => window.scrollTo(0, 0));
    await page.screenshot({
      path: "/private/tmp/telcopulse-simulation-controls.png",
      fullPage: true,
    });
  } finally {
    if (runID) {
      const stopped = await request.post(`/api/v1/simulations/${runID}/stop`, {
        data: { reason: "Ensure browser verification cleanup" },
      });
      expect(stopped.ok()).toBeTruthy();
    }
  }
});

test("simulation history failure does not permit a new run", async ({
  page,
}) => {
  await page.route("**/api/v1/simulations?**", (route) =>
    route.fulfill({
      status: 503,
      contentType: "application/json",
      body: JSON.stringify({ error: "simulation history unavailable" }),
    }),
  );
  await page.goto("/simulator");
  await expect(
    page.getByText("simulation history unavailable", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Inject failure", exact: true }),
  ).toBeDisabled();
  await page.unroute("**/api/v1/simulations?**");
  await page.getByRole("button", { name: "Try again", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "Inject failure", exact: true }),
  ).toBeEnabled();
});

test("database latency can be reviewed, observed and stopped", async ({
  page,
  request,
}) => {
  let runID = "";
  try {
    await page.goto("/simulator");
    await page
      .getByRole("banner")
      .getByLabel("Environment", { exact: true })
      .selectOption("staging");
    await page
      .getByRole("button", { name: "Inject failure", exact: true })
      .click();
    const dialog = page.getByRole("dialog");
    await dialog
      .getByLabel("Failure scenario")
      .selectOption("database-latency");
    await dialog.getByLabel("Selection percentage").fill("100");
    await dialog.getByLabel("Database delay (ms)").fill("4200");
    await dialog.getByLabel("Duration in seconds").fill("60");
    await dialog
      .getByLabel("Simulation reason")
      .fill("Browser database latency verification");
    await expect(
      dialog.getByText(
        "Selected new payment reservations execute a real bounded PostgreSQL delay.",
        { exact: false },
      ),
    ).toBeVisible();
    const started = page.waitForResponse(
      (response) =>
        response.url().endsWith("/api/v1/simulations") &&
        response.request().method() === "POST",
    );
    await dialog
      .getByRole("button", { name: "Start failure simulation", exact: true })
      .click();
    const response = await started;
    expect(response.status()).toBe(201);
    const run = await response.json();
    runID = run.id;
    expect(run.delay_ms).toBe(4200);
    await expect(dialog).not.toBeVisible();
    const purchase = page.waitForResponse(
      (response) =>
        response.url().endsWith("/api/v1/transactions") &&
        response.request().method() === "POST",
    );
    await page
      .getByRole("button", { name: "Start synthetic transaction" })
      .click();
    const outcome = await terminalOutcome(request, await (await purchase).json());
    expect(outcome.status).toBe("SUCCESS");
    expect(outcome.duration_ms).toBeGreaterThanOrEqual(4100);
    const row = page.getByRole("row").filter({ hasText: runID });
    await row.getByRole("button", { name: "Stop simulation" }).click();
    await dialog
      .getByLabel("Stop reason")
      .fill("Restore normal database latency");
    await dialog.getByRole("button", { name: "Confirm stop" }).click();
    await expect(dialog).not.toBeVisible();
    await row.getByRole("link", { name: runID, exact: true }).click();
    await expect(
      page.getByText("Database delay: 4200 ms per selected new reservation", {
        exact: true,
      }),
    ).toBeVisible();
    await expect(
      page.getByText("Selected for injection: 1", { exact: true }),
    ).toBeVisible();
  } finally {
    if (runID)
      expect(
        (
          await request.post(`/api/v1/simulations/${runID}/stop`, {
            data: { reason: "Ensure latency test cleanup" },
          })
        ).ok(),
      ).toBeTruthy();
  }
});

test("Kafka consumer lag can be reviewed, observed and stopped", async ({
  page,
  request,
}) => {
  let runID = "";
  try {
    await page.goto("/simulator");
    await page
      .getByRole("banner")
      .getByLabel("Environment", { exact: true })
      .selectOption("staging");
    await page
      .getByRole("button", { name: "Inject failure", exact: true })
      .click();
    const dialog = page.getByRole("dialog");
    await dialog
      .getByLabel("Failure scenario")
      .selectOption("kafka-consumer-lag");
    await dialog.getByLabel("Selection percentage").fill("100");
    await dialog.getByLabel("Notification delay (ms)").fill("4200");
    await dialog.getByLabel("Duration in seconds").fill("60");
    await dialog
      .getByLabel("Simulation reason")
      .fill("Browser Kafka consumer lag verification");
    await expect(
      dialog.getByText(
        "Selected notifications wait before delivery and Kafka offset commit.",
        { exact: false },
      ),
    ).toBeVisible();
    const started = page.waitForResponse(
      (response) =>
        response.url().endsWith("/api/v1/simulations") &&
        response.request().method() === "POST",
    );
    await dialog
      .getByRole("button", { name: "Start failure simulation", exact: true })
      .click();
    const response = await started;
    expect(response.status()).toBe(201);
    const run = await response.json();
    runID = run.id;
    expect(run.delay_ms).toBe(4200);
    await expect(dialog).not.toBeVisible();
    const purchase = page.waitForResponse(
      (response) =>
        response.url().endsWith("/api/v1/transactions") &&
        response.request().method() === "POST",
    );
    await page
      .getByRole("button", { name: "Start synthetic transaction" })
      .click();
    const outcome = await terminalOutcome(request, await (await purchase).json());
    expect(outcome.status).toBe("SUCCESS");
    expect(outcome.duration_ms).toBeLessThan(4000);
    const row = page.getByRole("row").filter({ hasText: runID });
    await row.getByRole("button", { name: "Stop simulation" }).click();
    await dialog
      .getByLabel("Stop reason")
      .fill("Restore normal Kafka consumer lag");
    await dialog.getByRole("button", { name: "Confirm stop" }).click();
    await expect(dialog).not.toBeVisible();
    await row.getByRole("link", { name: runID, exact: true }).click();
    await expect(
      page.getByText(
        "Notification delay: 4200 ms per selected event while the run is active",
        {
          exact: true,
        },
      ),
    ).toBeVisible();
    await expect(
      page.getByText("Selected for injection: 1", { exact: true }),
    ).toBeVisible();
  } finally {
    if (runID)
      expect(
        (
          await request.post(`/api/v1/simulations/${runID}/stop`, {
            data: { reason: "Ensure latency test cleanup" },
          })
        ).ok(),
      ).toBeTruthy();
  }
});

test("database timeout can be reviewed, observed and stopped", async ({
  page,
  request,
}) => {
  let runID = "";
  try {
    await page.goto("/simulator");
    await page
      .getByRole("banner")
      .getByLabel("Environment", { exact: true })
      .selectOption("staging");
    await page
      .getByRole("button", { name: "Inject failure", exact: true })
      .click();
    const dialog = page.getByRole("dialog");
    await dialog
      .getByLabel("Failure scenario")
      .selectOption("database-timeout");
    await dialog.getByLabel("Selection percentage").fill("100");
    await dialog.getByLabel("Statement timeout (ms)").fill("4200");
    await dialog.getByLabel("Duration in seconds").fill("60");
    await dialog
      .getByLabel("Simulation reason")
      .fill("Browser database timeout verification");
    await expect(
      dialog.getByText(
        "Selected payment queries reach a real PostgreSQL statement timeout.",
        { exact: false },
      ),
    ).toBeVisible();
    const started = page.waitForResponse(
      (response) =>
        response.url().endsWith("/api/v1/simulations") &&
        response.request().method() === "POST",
    );
    await dialog
      .getByRole("button", { name: "Start failure simulation", exact: true })
      .click();
    const response = await started;
    expect(response.status()).toBe(201);
    const run = await response.json();
    runID = run.id;
    expect(run.delay_ms).toBe(4200);
    await expect(dialog).not.toBeVisible();
    const purchase = page.waitForResponse(
      (response) =>
        response.url().endsWith("/api/v1/transactions") &&
        response.request().method() === "POST",
    );
    await page
      .getByRole("button", { name: "Start synthetic transaction" })
      .click();
    const outcome = await terminalOutcome(request, await (await purchase).json());
    expect(outcome.status).toBe("FAILED");
    expect(outcome.error_code).toBe("DB_TIMEOUT");
    expect(outcome.duration_ms).toBeGreaterThanOrEqual(4100);
    const row = page.getByRole("row").filter({ hasText: runID });
    await row.getByRole("button", { name: "Stop simulation" }).click();
    await dialog
      .getByLabel("Stop reason")
      .fill("Restore normal database timeout");
    await dialog.getByRole("button", { name: "Confirm stop" }).click();
    await expect(dialog).not.toBeVisible();
    await row.getByRole("link", { name: runID, exact: true }).click();
    await expect(
      page.getByText(
        "Statement timeout: 4200 ms per selected new reservation",
        {
          exact: true,
        },
      ),
    ).toBeVisible();
    await expect(
      page.getByText("Selected for injection: 1", { exact: true }),
    ).toBeVisible();
  } finally {
    if (runID)
      expect(
        (
          await request.post(`/api/v1/simulations/${runID}/stop`, {
            data: { reason: "Ensure latency test cleanup" },
          })
        ).ok(),
      ).toBeTruthy();
  }
});
