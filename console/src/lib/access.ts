import { useGetIdentity } from "@refinedev/core";

import type { Schemas } from "@/api/client";
import { operationRoles } from "@/api/roles.gen";

export type Me = Schemas["Me"];
export type StaffRole = Schemas["StaffRole"];

/**
 * Mirrors the backend's policy (backend/internal/api/policy.go): an operation is
 * allowed when the user holds one of its x-roles, when it is open to "any staff",
 * or when it is a read open to "any staff (read)". Admin holds every staff role.
 */
export function canCall(roles: readonly string[] | undefined, op: string): boolean {
  const allowed = operationRoles[op];
  if (!allowed || !roles) return false;
  if (roles.includes("admin")) return true;
  if (allowed.includes("any staff")) return true;
  if (op.startsWith("GET ") && allowed.includes("any staff (read)")) return true;
  return allowed.some((r) => roles.includes(r));
}

export function useMe() {
  const { data } = useGetIdentity<Me>();
  return data;
}

/** True when the signed-in user may call the operation, e.g. useCan("POST /console/requests/{id}/claim"). */
export function useCan(op: string): boolean {
  const me = useMe();
  return canCall(me?.roles, op);
}

export const roleLabels: Record<StaffRole, string> = {
  admin: "Admin",
  vetting_officer: "Vetting officer",
  approver: "Approver",
  training_coordinator: "Training coordinator",
  triage_officer: "Triage officer",
  supervisor: "Supervisor",
  complaints_manager: "Complaints manager",
  finance: "Finance",
  compliance: "Compliance",
  reporting: "Reporting",
};
