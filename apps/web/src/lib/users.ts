import { z } from "zod";
import { request } from "./api";
import { operatorSchema, roleSchema, type Role } from "./session";

const managedUser = operatorSchema.extend({
  active: z.boolean(),
  created_at: z.string().datetime({ offset: true }),
});
const usersPage = z.object({
  items: z.array(managedUser),
  more: z.boolean(),
  next: z.string().optional(),
});
export type ManagedUser = z.infer<typeof managedUser>;
export const managedRoles = roleSchema.options;

export const users = {
  list: (cursor = "") =>
    request(`/auth/users?${new URLSearchParams({ cursor })}`, usersPage),
  create: (input: { username: string; password: string; role: Role }) =>
    request("/auth/users", managedUser, {
      method: "POST",
      body: JSON.stringify(input),
    }),
  update: (id: string, change: { role?: Role; active?: boolean }) =>
    request(`/auth/users/${encodeURIComponent(id)}`, managedUser, {
      method: "PATCH",
      body: JSON.stringify(change),
    }),
};
