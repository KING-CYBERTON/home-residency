import { expect } from "@playwright/test";
import { existsSync, readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { API, STACK, staff, type StaffName } from "./env";
import { smsCode, totp } from "./otp";

export class ApiError extends Error {
  constructor(public status: number, public body: unknown, what: string) {
    super(`${what}: ${status} ${JSON.stringify(body)}`);
  }
}

/** Calls the JSON API the way the console and the Flutter app do (Bearer token, no Origin header). */
export async function call<T = any>(token: string | null, method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(API + path, {
    method,
    headers: { ...(token ? { Authorization: `Bearer ${token}` } : {}), ...(body !== undefined ? { "Content-Type": "application/json" } : {}) },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const text = await res.text();
  const json = text ? JSON.parse(text) : null;
  if (!res.ok) throw new ApiError(res.status, json, `${method} ${path}`);
  return json as T;
}

// The API refuses an authenticator code that was already used, so each staff member signs in once
// per run and the token is shared by the API helpers and the console pages (see ui.ts). Tokens and
// the last 30-second step used are kept in .stack/, so a restarted test worker reuses them.
const cacheFile = join(STACK, "staff-sessions.json");
type Cache = Partial<Record<StaffName, { token?: string; step?: number }>>;
const readCache = (): Cache => (existsSync(cacheFile) ? JSON.parse(readFileSync(cacheFile, "utf8")) : {});
const writeCache = (name: StaffName, v: { token?: string; step?: number }) => {
  const c = readCache();
  c[name] = { ...c[name], ...v };
  writeFileSync(cacheFile, JSON.stringify(c));
};

export async function staffToken(name: StaffName): Promise<string> {
  const cached = readCache()[name]?.token;
  if (cached) return cached;
  const s = staff(name);
  const login = await call<{ token: string }>(null, "POST", "/auth/staff/login", { email: s.email, password: s.password });
  await call(login.token, "POST", "/auth/staff/mfa", { code: await freshTotp(name) });
  writeCache(name, { token: login.token });
  return login.token;
}

/** A code from a 30-second step this staff member has not used yet. */
export async function freshTotp(name: StaffName): Promise<string> {
  let step = Math.floor(Date.now() / 30_000);
  if (readCache()[name]?.step === step) {
    await new Promise((r) => setTimeout(r, 30_000 - (Date.now() % 30_000) + 200));
    step = Math.floor(Date.now() / 30_000);
  }
  writeCache(name, { step });
  return totp(staff(name).totp);
}

/** Registers a household or provider by phone and SMS code, as the app does, and returns a session token. */
export async function register(kind: "client" | "provider", e164: string, fullName: string): Promise<{ token: string; userId: string }> {
  const otp = await call<{ otp_id: string }>(null, "POST", "/auth/otp/request", { phone: e164, purpose: "register" });
  const s = await call<{ token: string; user: { id: string } }>(null, "POST", "/auth/otp/verify", {
    otp_id: otp.otp_id,
    code: await smsCode(e164),
    kind,
    full_name: fullName,
    consents: [{ purpose: "privacy_notice", notice_version: "2026-10", granted: true }],
  });
  return { token: s.token, userId: s.user.id };
}

export async function expectStatus(p: Promise<unknown>, status: number) {
  await expect(p).rejects.toMatchObject({ status });
}
