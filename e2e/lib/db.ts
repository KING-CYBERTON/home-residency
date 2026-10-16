import pg from "pg";
import { DATABASE_URL } from "./env";

let pool: pg.Pool | undefined;

/** Direct database access, for preconditions and for checks the UI does not show. */
export async function sql<T = Record<string, unknown>>(text: string, ...params: unknown[]): Promise<T[]> {
  pool ??= new pg.Pool({ connectionString: DATABASE_URL, max: 2 });
  return (await pool.query(text, params)).rows as T[];
}

export async function one<T = Record<string, unknown>>(text: string, ...params: unknown[]): Promise<T> {
  const rows = await sql<T>(text, ...params);
  if (rows.length !== 1) throw new Error(`expected one row, got ${rows.length}: ${text}`);
  return rows[0];
}

export async function closeDb() {
  await pool?.end();
  pool = undefined;
}
