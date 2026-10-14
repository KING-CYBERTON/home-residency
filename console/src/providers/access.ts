import type { AccessControlProvider } from "@refinedev/core";

import { canCall } from "@/lib/access";

import { authProvider } from "./auth";

/**
 * Resources declare the operation that backs their list page in meta.op;
 * a resource is shown only to roles that may call it.
 */
export const accessControlProvider: AccessControlProvider = {
  async can({ params }) {
    const op = params?.resource?.meta?.op as string | undefined;
    if (!op) return { can: true };
    const roles = (await authProvider.getPermissions?.()) as string[] | undefined;
    return canCall(roles, op) ? { can: true } : { can: false, reason: "Your roles do not include this area" };
  },
  options: { buttons: { enableAccessControl: true, hideIfUnauthorized: true } },
};
