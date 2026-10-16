import crypto from "node:crypto";
import fs from "node:fs";
import path from "node:path";
import { expect } from "@playwright/test";
import { STACK } from "./env";

/** The latest SMS code sent to a phone. With SMS_PROVIDER=log the API writes every SMS to its log. */
export async function smsCode(e164: string): Promise<string> {
  let code: string | undefined;
  await expect
    .poll(() => {
      const lines = fs.readFileSync(path.join(STACK, "api.log"), "utf8").split("\n");
      for (let i = lines.length - 1; i >= 0; i--) {
        if (!lines[i].includes(`"to":"${e164}"`)) continue;
        code = lines[i].match(/code is (\d{6})/)?.[1];
        if (code) return code;
      }
      return undefined;
    }, { message: `SMS code for ${e164}` })
    .toBeTruthy();
  return code!;
}

/** RFC 6238 code for a base32 secret (staff authenticator). */
export function totp(secret: string, at = Date.now()): string {
  const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567";
  let bits = "";
  for (const c of secret.replace(/=+$/, "")) bits += alphabet.indexOf(c).toString(2).padStart(5, "0");
  const key = Buffer.from(bits.match(/.{8}/g)!.map((b) => parseInt(b, 2)));
  const counter = Buffer.alloc(8);
  counter.writeBigUInt64BE(BigInt(Math.floor(at / 30_000)));
  const h = crypto.createHmac("sha1", key).update(counter).digest();
  const o = h[19] & 15;
  return String((h.readUInt32BE(o) & 0x7fffffff) % 1_000_000).padStart(6, "0");
}

/**
 * The API sends at most one code a minute to a phone. A person waits; the tests wait too, rather
 * than relaxing the limit.
 */
export async function waitForNextCode(e164: string) {
  const { sql } = await import("./db");
  const [row] = await sql<{ wait: number }>(
    `SELECT GREATEST(0, 61 - EXTRACT(EPOCH FROM now() - max(created_at)))::float8 AS wait FROM otp_codes WHERE phone_e164 = $1`, e164);
  if (row?.wait) await new Promise((r) => setTimeout(r, row.wait * 1000));
}
