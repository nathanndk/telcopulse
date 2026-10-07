import http from "k6/http";
import crypto from "k6/crypto";
import { sleep } from "k6";
import { Counter, Rate, Trend } from "k6/metrics";

// Steady synthetic purchases inside the kind cluster for game days and canary
// analysis. Logs in once as the bootstrap administrator (protected mode) and
// sends the session cookie and the exact configured Origin on every request.
const base = __ENV.BASE_URL || "http://web.telcopulse.svc:3000";
const origin = __ENV.ORIGIN || "https://telcopulse.kind.invalid";
const rate = Number(__ENV.RATE || 3);
const durationSeconds = Number(__ENV.DURATION_SECONDS || 120);
if (!/^http:\/\/web\.telcopulse(\.svc)?:3000$/.test(base) || !Number.isInteger(rate) || rate < 1 || rate > 50 ||
    !Number.isInteger(durationSeconds) || durationSeconds < 10 || durationSeconds > 3600) {
  throw new Error("invalid kind load configuration");
}

const success = new Counter("purchase_success");
const technical = new Counter("purchase_technical_failure");
const unavailable = new Counter("purchase_http_error");
const unresolved = new Counter("purchase_unresolved");
const failureRate = new Rate("purchase_not_successful_rate");
const endToEnd = new Trend("purchase_end_to_end_ms", true);

export const options = {
  summaryTrendStats: ["med", "p(95)", "p(99)", "max"],
  scenarios: {
    purchases: {
      executor: "constant-arrival-rate", rate, timeUnit: "1s", duration: `${durationSeconds}s`,
      preAllocatedVUs: Math.max(5, rate * 2), maxVUs: 100, gracefulStop: "30s",
    },
  },
};

export function setup() {
  const response = http.post(`${base}/api/v1/auth/login`, JSON.stringify({ username: "local-admin", password: __ENV.ADMIN_PASSWORD }),
    { headers: { "Content-Type": "application/json", Origin: origin } });
  if (response.status !== 200) throw new Error(`login failed with ${response.status}`);
  const name = Object.keys(response.cookies)[0];
  return { cookie: `${name}=${response.cookies[name][0].value}` };
}

function key() {
  return Array.from(new Uint8Array(crypto.randomBytes(16)), (b) => b.toString(16).padStart(2, "0")).join("");
}

export default function (session) {
  const headers = { "Content-Type": "application/json", Origin: origin, Cookie: session.cookie };
  const started = Date.now();
  const response = http.post(`${base}/api/v1/transactions`, JSON.stringify({
    customer_id: "cus-001", package_id: "pkg-10", payment_method: "E-Wallet", environment: "development",
  }), { headers: { ...headers, "Idempotency-Key": key() }, timeout: "20s", tags: { name: "POST /api/v1/transactions" } });
  if (![200, 201, 202].includes(response.status)) {
    unavailable.add(1);
    failureRate.add(true);
    return;
  }
  let transaction = response.json();
  const deadline = Date.now() + 30_000;
  while (transaction.status === "PROCESSING" && Date.now() < deadline) {
    sleep(1);
    const poll = http.get(`${base}/api/v1/transactions/${transaction.id}`, { headers, timeout: "10s", tags: { name: "GET /api/v1/transactions/{id}" } });
    if (poll.status === 200) transaction = poll.json();
  }
  if (transaction.status === "SUCCESS") {
    success.add(1);
    endToEnd.add(Date.now() - started);
    failureRate.add(false);
  } else if (transaction.status === "FAILED") {
    technical.add(transaction.error_code === "INSUFFICIENT_BALANCE" ? 0 : 1);
    endToEnd.add(Date.now() - started);
    failureRate.add(true);
  } else {
    unresolved.add(1);
    failureRate.add(true);
  }
}

export function handleSummary(data) {
  return { stdout: JSON.stringify(data.metrics) };
}
