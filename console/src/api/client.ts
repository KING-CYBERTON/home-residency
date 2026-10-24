import createFetchClient, { type Middleware } from "openapi-fetch";
import createQueryClient from "openapi-react-query";

import type { components, paths } from "./schema";

export type Schemas = components["schemas"];

/** Same-origin in every environment: CloudFront routes /api/* to the Go service. */
export const API_BASE: string = import.meta.env.VITE_API_BASE_URL ?? "/api/v1";

const TOKEN_KEY = "hr_console_token";

/**
 * The session token lives in sessionStorage, so it is scoped to one tab and
 * gone when the tab closes. Staff consoles are used on shared office machines.
 */
export const sessionToken = {
  get: (): string | null => {
    try {
      return sessionStorage.getItem(TOKEN_KEY);
    } catch {
      return null;
    }
  },
  set: (token: string) => sessionStorage.setItem(TOKEN_KEY, token),
  clear: () => sessionStorage.removeItem(TOKEN_KEY),
};

/** The API's error envelope plus the HTTP status. */
export class ApiError extends Error {
  status: number;
  code: string;
  details?: Record<string, unknown>;
  requestId?: string;

  constructor(status: number, body: Partial<Schemas["Error"]> | undefined) {
    super(body?.message || `Request failed (${status})`);
    this.status = status;
    this.code = body?.code ?? "http_error";
    this.details = body?.details;
    this.requestId = body?.request_id;
  }

  /** Refine reads statusCode from errors it is handed. */
  get statusCode() {
    return this.status;
  }
}

let onUnauthenticated: () => void = () => {};
/** Called once when any request comes back 401, so the app can send the user to sign in. */
export function setUnauthenticatedHandler(fn: () => void) {
  onUnauthenticated = fn;
}

export async function toApiError(response: Response): Promise<ApiError> {
  let body: Partial<Schemas["Error"]> | undefined;
  try {
    body = await response.clone().json();
  } catch {
    body = undefined;
  }
  return new ApiError(response.status, body);
}

export function authHeaders(): Record<string, string> {
  const token = sessionToken.get();
  return token ? { Authorization: `Bearer ${token}` } : {};
}

/** A 401 means the session has ended, except a wrong current password when changing it, which leaves you signed in. */
function handleUnauthenticated(response: Response, err: ApiError) {
  if (response.status === 401 && !response.url.includes("/auth/staff/") && err.code !== "invalid_credentials") {
    sessionToken.clear();
    onUnauthenticated();
  }
}

const authMiddleware: Middleware = {
  onRequest({ request }) {
    const token = sessionToken.get();
    if (token) request.headers.set("Authorization", `Bearer ${token}`);
    return request;
  },
  async onResponse({ response }) {
    if (response.ok) return response;
    // Throwing here makes every failed call reject with an ApiError, so
    // queries and mutations surface the API's message and status.
    const err = await toApiError(response);
    handleUnauthenticated(response, err);
    throw err;
  },
};

export const fetchClient = createFetchClient<paths>({ baseUrl: API_BASE });
fetchClient.use(authMiddleware);

/** Typed TanStack Query hooks for every operation in openapi.yaml. */
export const $api = createQueryClient(fetchClient);

type Query = Record<string, string | number | boolean | undefined | null>;

/** Untyped request for the Refine data provider, with the same auth and error handling. */
export async function request<T>(
  method: string,
  path: string,
  opts: { query?: Query; body?: unknown } = {},
): Promise<T> {
  const url = new URL(API_BASE + path, window.location.origin);
  for (const [k, v] of Object.entries(opts.query ?? {})) {
    if (v !== undefined && v !== null && v !== "") url.searchParams.set(k, String(v));
  }
  const response = await fetch(url, {
    method,
    headers: { ...authHeaders(), ...(opts.body !== undefined ? { "Content-Type": "application/json" } : {}) },
    body: opts.body !== undefined ? JSON.stringify(opts.body) : undefined,
  });
  if (!response.ok) {
    const err = await toApiError(response);
    handleUnauthenticated(response, err);
    throw err;
  }
  if (response.status === 204) return undefined as T;
  return (await response.json()) as T;
}

/** Downloads a file from an authenticated endpoint (e.g. CSV export). */
export async function download(path: string, query: Query, filename: string) {
  const url = new URL(API_BASE + path, window.location.origin);
  for (const [k, v] of Object.entries(query)) {
    if (v !== undefined && v !== null && v !== "") url.searchParams.set(k, String(v));
  }
  const response = await fetch(url, { headers: authHeaders() });
  if (!response.ok) {
    const err = await toApiError(response);
    handleUnauthenticated(response, err);
    throw err;
  }
  const blob = await response.blob();
  const href = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = href;
  a.download = filename;
  a.click();
  URL.revokeObjectURL(href);
}

export function errorMessage(err: unknown): string {
  if (err instanceof ApiError) {
    const fields = err.details && typeof err.details === "object" ? Object.entries(err.details) : [];
    const extra = fields.length ? ` (${fields.map(([k, v]) => `${k}: ${String(v)}`).join("; ")})` : "";
    return err.message + extra;
  }
  if (err instanceof Error) return err.message;
  return "Something went wrong";
}
