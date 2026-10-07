import http from "k6/http";
import crypto from "k6/crypto";
import { sleep } from "k6";
import { Counter, Rate, Trend } from "k6/metrics";

// One constant-arrival-rate step of the capacity test (tests/load/capacity.py).
// Unlike purchases.js it reports instead of failing, so a step past the knee
// still yields measurements. Local Compose web endpoint only.
const base = __ENV.BASE_URL || "http://web:3000";
const rate = Number(__ENV.RATE || 5);
const durationSeconds = Number(__ENV.DURATION_SECONDS || 45);
const maxVUs = Number(__ENV.MAX_VUS || 300);
if (base !== "http://web:3000" || !Number.isInteger(rate) || rate < 1 || rate > 200 ||
    !Number.isInteger(durationSeconds) || durationSeconds < 10 || durationSeconds > 600 ||
    !Number.isInteger(maxVUs) || maxVUs < 1 || maxVUs > 1000) {
  throw new Error("invalid capacity step configuration");
}

const completed = new Counter("purchase_completed");
const unresolved = new Counter("purchase_unresolved");
const technicalFailures = new Rate("purchase_technical_failure_rate");
const rejected = new Counter("purchase_rejected");
const endToEnd = new Trend("purchase_end_to_end_ms", true);

export const options = {
  discardResponseBodies: false,
  summaryTrendStats: ["avg", "med", "p(90)", "p(95)", "p(99)", "max"],
  scenarios: {
    step: {
      executor: "constant-arrival-rate",
      rate,
      timeUnit: "1s",
      duration: `${durationSeconds}s`,
      preAllocatedVUs: Math.min(maxVUs, Math.max(10, rate * 2)),
      maxVUs,
      gracefulStop: "30s",
    },
  },
};

function key() {
  return Array.from(new Uint8Array(crypto.randomBytes(16)), (b) => b.toString(16).padStart(2, "0")).join("");
}

function parse(response) {
  if (![200, 201, 202].includes(response.status)) return null;
  try { return response.json(); } catch (_) { return null; }
}

export default function () {
  const started = Date.now();
  const response = http.post(`${base}/api/v1/transactions`, JSON.stringify({
    customer_id: "cus-001", package_id: "pkg-10", payment_method: "E-Wallet", environment: "staging",
  }), { headers: { "Content-Type": "application/json", "Idempotency-Key": key() }, timeout: "20s", tags: { name: "POST /api/v1/transactions" } });
  if (response.status === 429 || response.status === 503) {
    rejected.add(1);
    return;
  }
  let transaction = parse(response);
  const deadline = Date.now() + 20_000;
  while (transaction && transaction.status === "PROCESSING" && Date.now() < deadline) {
    sleep(0.5);
    transaction = parse(http.get(`${base}/api/v1/transactions/${transaction.id}`, { timeout: "10s", tags: { name: "GET /api/v1/transactions/{id}" } }));
  }
  if (!transaction || !["SUCCESS", "FAILED"].includes(transaction.status)) {
    unresolved.add(1);
    return;
  }
  completed.add(1);
  endToEnd.add(Date.now() - started);
  technicalFailures.add(transaction.status === "FAILED" && transaction.error_code !== "INSUFFICIENT_BALANCE");
}

export function handleSummary(data) {
  return { stdout: JSON.stringify(data.metrics) };
}
