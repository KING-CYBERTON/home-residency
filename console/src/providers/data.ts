import type { BaseRecord, DataProvider, LogicalFilter } from "@refinedev/core";

import { API_BASE, request } from "@/api/client";

type Page<T> = { data: T[]; meta: { next_cursor?: string | null; limit: number } };

function toQuery(filters: unknown[] | undefined): Record<string, string> {
  const q: Record<string, string> = {};
  for (const f of (filters ?? []) as LogicalFilter[]) {
    if (f && "field" in f && f.value !== undefined && f.value !== null && f.value !== "") q[f.field] = String(f.value);
  }
  return q;
}

/**
 * Refine data provider for /console/{resource}. Catalogue endpoints return plain
 * arrays; queue endpoints return { data, meta.next_cursor } pages, and the
 * cursor is passed through meta.cursor.
 */
export const dataProvider: DataProvider = {
  getApiUrl: () => API_BASE,

  async getList({ resource, filters, meta }) {
    const query: Record<string, string> = { ...toQuery(filters) };
    if (meta?.cursor) query.cursor = String(meta.cursor);
    if (meta?.limit) query.limit = String(meta.limit);
    const res = await request<BaseRecord[] | Page<BaseRecord>>("GET", `/console/${resource}`, { query });
    if (Array.isArray(res)) return { data: res as never, total: res.length };
    return { data: res.data as never, total: res.data.length, cursor: { next: res.meta.next_cursor ?? undefined } };
  },

  async getOne({ resource, id }) {
    return { data: await request("GET", `/console/${resource}/${id}`) };
  },

  async create({ resource, variables }) {
    return { data: await request("POST", `/console/${resource}`, { body: variables }) };
  },

  async update({ resource, id, variables }) {
    return { data: await request("PATCH", `/console/${resource}/${id}`, { body: variables }) };
  },

  async deleteOne({ resource, id }) {
    await request("DELETE", `/console/${resource}/${id}`);
    return { data: { id } as never };
  },

  async custom({ url, method, payload, query }) {
    const data = await request<BaseRecord>(method.toUpperCase(), url, {
      body: payload,
      query: query as Record<string, string>,
    });
    return { data: data as never };
  },
};
