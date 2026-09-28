import { z } from "zod";
import {
  customerSchema,
  packageSchema,
  transactionSchema,
  transactionPageSchema,
  notificationStatusSchema,
  overviewSchema,
  type Environment,
  type Purchase,
} from "./contracts";
export class APIError extends Error {
  constructor(
    message: string,
    public status: number,
  ) {
    super(message);
    this.name = "APIError";
  }
}
export const SESSION_EXPIRED_EVENT = "telcopulse:session-expired";
export async function request<T>(
  path: string,
  schema: z.ZodType<T>,
  init?: RequestInit,
): Promise<T> {
  const response = await fetch(`/api/v1${path}`, {
    ...init,
    headers: { "Content-Type": "application/json", ...init?.headers },
    signal: init?.signal ?? AbortSignal.timeout(12000),
  });
  const body: unknown = await response.json().catch(() => null);
  if (!response.ok) {
    if (
      response.status === 401 &&
      path !== "/auth/login" &&
      path !== "/auth/me" &&
      typeof window !== "undefined"
    ) {
      window.dispatchEvent(new Event(SESSION_EXPIRED_EVENT));
    }
    const error = z.object({ error: z.string() }).safeParse(body);
    throw new APIError(
      error.success
        ? error.data.error
        : `API request failed (${response.status})`,
      response.status,
    );
  }
  const parsed = schema.safeParse(body);
  if (!parsed.success)
    throw new Error(
      "The API returned an unexpected response. Refresh or contact your administrator.",
    );
  return parsed.data;
}
export const api = {
  customers: () => request("/customers", z.array(customerSchema)),
  packages: () => request("/packages", z.array(packageSchema)),
  overview: (environment: Environment) =>
    request(`/overview?environment=${environment}`, overviewSchema),
  transactions: (
    environment: Environment,
    page = 1,
    status = "",
    search = "",
    size = 20,
  ) =>
    request(
      `/transactions?${new URLSearchParams({ environment, page: String(page), page_size: String(size), status, search })}`,
      transactionPageSchema,
    ),
  transaction: (id: string) =>
    request(`/transactions/${encodeURIComponent(id)}`, transactionSchema),
  notificationStatus: (id: string) =>
    request(`/transactions/${encodeURIComponent(id)}/notification`, notificationStatusSchema),
  purchase: (purchase: Purchase, key: string) =>
    request("/transactions", transactionSchema, {
      method: "POST",
      headers: { "Idempotency-Key": key },
      body: JSON.stringify(purchase),
    }),
};
export const money = (n: number) =>
  new Intl.NumberFormat("id-ID", {
    style: "currency",
    currency: "IDR",
    maximumFractionDigits: 0,
  }).format(n);
export const duration = (n: number) =>
  n >= 1000 ? `${(n / 1000).toFixed(2)} s` : `${n.toFixed(1)} ms`;
export const timestamp = (s: string) =>
  new Intl.DateTimeFormat("en-GB", {
    day: "2-digit",
    month: "short",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    timeZone: "Asia/Jakarta",
  }).format(new Date(s));
