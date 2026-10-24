import type { AuthProvider } from "@refinedev/core";

import { ApiError, fetchClient, sessionToken, type Schemas } from "@/api/client";

let cachedMe: Schemas["Me"] | null = null;
let inflight: Promise<Schemas["Me"] | null> | null = null;

async function loadMe(): Promise<Schemas["Me"] | null> {
  if (cachedMe) return cachedMe;
  if (!sessionToken.get()) return null;
  inflight ??= fetchClient
    .GET("/me")
    .then(({ data }) => {
      cachedMe = data && data.kind === "staff" ? data : null;
      if (!cachedMe) sessionToken.clear();
      return cachedMe;
    })
    .catch((err) => {
      if (err instanceof ApiError && (err.status === 401 || err.status === 403)) {
        sessionToken.clear();
        return null;
      }
      throw err;
    })
    .finally(() => {
      inflight = null;
    });
  return inflight;
}

/**
 * Sign-in is two steps (password, then TOTP) and is driven by the login page,
 * which calls login() with the session once the TOTP check has passed.
 */
export const authProvider: AuthProvider = {
  async login({ session }: { session: Schemas["Session"] }) {
    if (!session?.token || session.mfa_required) {
      return { success: false, error: new Error("Finish the authenticator check first") };
    }
    sessionToken.set(session.token);
    cachedMe = session.user;
    return { success: true, redirectTo: returnPath() };
  },

  async logout() {
    try {
      if (sessionToken.get()) await fetchClient.POST("/auth/logout");
    } catch {
      // The session may already be gone; signing out locally is what matters.
    }
    sessionToken.clear();
    cachedMe = null;
    return { success: true, redirectTo: "/login" };
  },

  async check() {
    const me = await loadMe().catch(() => null);
    return me ? { authenticated: true } : { authenticated: false, redirectTo: "/login", logout: true };
  },

  async onError(error) {
    if (error instanceof ApiError && error.status === 401 && error.code !== "invalid_credentials") {
      cachedMe = null;
      return { logout: true, redirectTo: "/login", error };
    }
    return { error };
  },

  async getIdentity() {
    return loadMe();
  },

  async getPermissions() {
    return (await loadMe())?.roles ?? [];
  },
};

/** Where to go after sign-in: the page that sent the user to /login, if it is one of ours. */
function returnPath(): string {
  return safeReturnPath(new URLSearchParams(window.location.search).get("to"));
}

/** Only same-site paths, so a crafted ?to= cannot send staff to another site. */
export function safeReturnPath(to: string | null): string {
  return to && to.startsWith("/") && !to.startsWith("//") && !to.includes("\\") && !to.startsWith("/login") ? to : "/";
}
