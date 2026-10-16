import fs from "node:fs";
import path from "node:path";

export const STACK = process.env.E2E_STACK ?? path.resolve(import.meta.dirname, "../.stack");
export const SITE = process.env.E2E_SITE_URL ?? "http://localhost:8080";
export const CONSOLE = process.env.E2E_CONSOLE_URL ?? "http://localhost:4173";
export const API = `${SITE}/api/v1`;
export const DATABASE_URL =
  process.env.E2E_DATABASE_URL ?? `${process.env.E2E_PG_URL ?? "postgres://postgres:postgres@localhost:5432"}/${process.env.E2E_DB ?? "homend_e2e"}`;

export type StaffName = "ops" | "approver" | "supervisor2";
export type Staff = { email: string; password: string; totp: string };

export function staff(name: StaffName): Staff {
  const all = JSON.parse(fs.readFileSync(path.join(STACK, "staff.json"), "utf8")) as Record<StaffName, Staff>;
  return all[name];
}

/** A fresh Kenyan mobile number per call (07xx xxx xxx), so runs and specs never collide on a phone. */
let seq = 0;
export function newPhone(): { local: string; e164: string } {
  const n = String((Math.floor(Date.now() / 1000) * 10 + (seq++ % 10)) % 1_000_000).padStart(6, "0");
  return { local: `0711 ${n.slice(0, 3)} ${n.slice(3)}`, e164: `+254711${n}` };
}
