import http from "k6/http";
import crypto from "k6/crypto";
import { check, sleep } from "k6";
import { Counter, Rate, Trend } from "k6/metrics";

// This workload deliberately targets only the local Compose UI proxy.
const base = __ENV.BASE_URL || "http://web:3000";
const environment = __ENV.ENVIRONMENT || "staging";
const expectation = __ENV.EXPECT || "healthy";
const failureCode = __ENV.FAILURE_CODE || "BAD_DEPLOYMENT_PAYMENT_FAILURE";
const rate = Number(__ENV.RATE || 1);
const durationSeconds = Number(__ENV.DURATION_SECONDS || 15);
const allocatedVUs = Number(__ENV.VUS || 8);
const minTargetRate = Number(__ENV.MIN_TARGET_RATE || (expectation === "healthy" ? 0.99 : 0.5));

if (!["http://web:3000", "http://localhost:3000", "http://localhost:3001", "http://127.0.0.1:3000", "http://127.0.0.1:3001"].includes(base) ||
    !["development", "staging"].includes(environment) ||
    !["healthy", "degraded"].includes(expectation) ||
    !Number.isInteger(rate) || rate < 1 || rate > 20 ||
    !Number.isInteger(durationSeconds) || durationSeconds < 5 || durationSeconds > 300 ||
    !Number.isInteger(allocatedVUs) || allocatedVUs < 1 || allocatedVUs > 50 ||
    !Number.isFinite(minTargetRate) || minTargetRate <= 0 || minTargetRate > 1 ||
    !/^[A-Z][A-Z0-9_]{2,79}$/.test(failureCode)) {
  throw new Error("invalid local load configuration");
}

const targetOutcomes = new Rate("business_target_outcome_rate");
const businessFailures = new Rate("business_failure_rate");
const terminalOutcomes = new Counter("business_terminal_outcomes");
const unresolved = new Counter("business_unresolved_outcomes");
const purchaseDuration = new Trend("business_purchase_duration_ms", true);

export const options = {
  scenarios: {
    purchases: {
      executor: "constant-arrival-rate",
      rate,
      timeUnit: "1s",
      duration: `${durationSeconds}s`,
      preAllocatedVUs: allocatedVUs,
      maxVUs: allocatedVUs,
      gracefulStop: "20s",
    },
  },
  thresholds: {
    business_target_outcome_rate: [`rate >= ${minTargetRate}`],
    business_terminal_outcomes: ["count >= 5"],
    business_unresolved_outcomes: ["count == 0"],
    http_req_failed: ["rate == 0"],
    dropped_iterations: ["count == 0"],
  },
};

function newKey() {
  return Array.from(new Uint8Array(crypto.randomBytes(16)), (byte) => byte.toString(16).padStart(2, "0")).join("");
}

function parseTransaction(response) {
  if (![200, 201, 202].includes(response.status)) return null;
  try {
    const value = response.json();
    if (typeof value.id !== "string" || !/^TXN-[a-f0-9]{24}$/.test(value.id) ||
        !["PROCESSING", "SUCCESS", "FAILED"].includes(value.status)) return null;
    return value;
  } catch (_) {
    return null;
  }
}

export default function () {
  const startedAt = Date.now();
  const response = http.post(`${base}/api/v1/transactions`, JSON.stringify({
    customer_id: "cus-001",
    package_id: "pkg-10",
    payment_method: "E-Wallet",
    environment,
  }), {
    headers: { "Content-Type": "application/json", "Idempotency-Key": newKey() },
    timeout: "15s",
    tags: { name: "POST /api/v1/transactions" },
  });
  let transaction = parseTransaction(response);
  const deadline = Date.now() + 12_000;
  while (transaction && transaction.status === "PROCESSING" && Date.now() < deadline) {
    sleep(0.5);
    transaction = parseTransaction(http.get(`${base}/api/v1/transactions/${transaction.id}`, {
      timeout: "10s", tags: { name: "GET /api/v1/transactions/{id}" },
    }));
  }
  const terminal = transaction && (transaction.status === "SUCCESS" || transaction.status === "FAILED");
  check(transaction, { "purchase reaches a terminal outcome": () => Boolean(terminal) });
  if (!terminal) {
    unresolved.add(1);
    return;
  }
  terminalOutcomes.add(1);
  purchaseDuration.add(Date.now() - startedAt);
  businessFailures.add(transaction.status === "FAILED");
  targetOutcomes.add(expectation === "healthy"
    ? transaction.status === "SUCCESS"
    : transaction.status === "FAILED" && transaction.error_code === failureCode);
}
