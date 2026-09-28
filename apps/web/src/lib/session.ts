import { z } from "zod";
import { request } from "./api";

export const roleSchema = z.enum([
  "Viewer",
  "Operator",
  "Incident Commander",
  "Engineer",
  "Administrator",
]);
export type Role = z.infer<typeof roleSchema>;
export const operatorSchema = z.object({
  id: z.string().regex(/^USR-[a-f0-9]{24}$/),
  username: z.string().min(1),
  role: roleSchema,
});
export type Operator = z.infer<typeof operatorSchema>;
export const sessionAPI = {
  status: () => request("/auth/status", z.object({ required: z.boolean() })),
  me: () => request("/auth/me", operatorSchema),
  login: (username: string, password: string) =>
    request("/auth/login", operatorSchema, {
      method: "POST",
      body: JSON.stringify({ username, password }),
    }),
  logout: () =>
    request("/auth/logout", z.object({ status: z.literal("signed out") }), {
      method: "POST",
    }),
};
export const canCreateIncident = (role: Role | undefined) =>
  role === "Operator" || role === "Incident Commander" || role === "Administrator";
export const canEditIncident = (role: Role | undefined) =>
  role === "Operator" || role === "Incident Commander" || role === "Administrator";
export const canEscalateIncident = (role: Role | undefined) =>
  role === "Incident Commander" || role === "Administrator";
export const canGeneratePostmortem = canEscalateIncident;
export const canInjectFailure = (role: Role | undefined) =>
  role === "Engineer" || role === "Administrator";
export const canReplayDeadLetter = canInjectFailure;
export const canRunPurchase = (role: Role | undefined) => role !== "Viewer";
