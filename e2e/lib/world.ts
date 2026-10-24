import { call, register } from "./api";
import { newPhone } from "./env";
import { one, sql } from "./db";

// Preconditions for specs that test one feature in depth. The whole route to an active provider
// (application, vetting, training, contract, activation) is covered through the UI by journey.spec;
// here it is shortened to what the Go world fixture does, so each spec starts from a known state.

export type Person = { userId: string; token: string; phone: { local: string; e164: string }; name: string };

export async function areaId(slug = "kilimani"): Promise<string> {
  return (await one<{ id: string }>(`SELECT id FROM service_areas WHERE slug = $1`, slug)).id;
}

export async function service(slug = "standard-clean-1br"): Promise<{ id: string; categoryId: string; name: string }> {
  const s = await one<{ id: string; category_id: string; name: string }>(`SELECT id, category_id, name FROM services WHERE slug = $1`, slug);
  return { id: s.id, categoryId: s.category_id, name: s.name };
}

export async function activeProvider(name: string, opts: { area?: string; category?: string } = {}): Promise<Person> {
  const phone = newPhone();
  const p = await register("provider", phone.e164, name);
  const area = await areaId(opts.area ?? "kilimani");
  const cat = (await one<{ id: string }>(`SELECT id FROM service_categories WHERE slug = $1`, opts.category ?? "cleaning")).id;
  await sql(
    `UPDATE providers SET status = 'active', max_concurrent_jobs = 3, home_service_area_id = $2, payout_msisdn = $3,
       engagement_basis = 'independent_contractor', activated_at = now() WHERE user_id = $1`,
    p.userId, area, phone.e164,
  );
  await sql(`INSERT INTO provider_categories (provider_id, category_id, status) VALUES ($1,$2,'active')`, p.userId, cat);
  await sql(`INSERT INTO provider_service_areas (provider_id, service_area_id) VALUES ($1,$2)`, p.userId, area);
  return { ...p, phone, name };
}

export async function client(name: string, area = "kilimani"): Promise<Person & { addressId: string }> {
  const phone = newPhone();
  const c = await register("client", phone.e164, name);
  const a = await call<{ id: string }>(c.token, "POST", "/client/addresses", {
    label: "Home", service_area_id: await areaId(area), estate: "Kilimani Gardens", house_number: "A1",
    access_details: "Gate code 1234", lat: -1.2898, lng: 36.7833,
  });
  return { ...c, phone, name, addressId: a.id };
}

/** A booking that triage has assigned and the provider has accepted, starting `startInMinutes` from now. */
export async function acceptedJob(ops: string, c: Person & { addressId: string }, p: Person, startInMinutes = 30) {
  const svc = await service();
  const req = await call<{ id: string; reference: string }>(c.token, "POST", "/client/requests", {
    accept_terms: true,
    service_id: svc.id, address_id: c.addressId, requested_start: new Date(Date.now() + startInMinutes * 60_000).toISOString(),
  });
  await call(ops, "POST", `/console/requests/${req.id}/claim`);
  const a = await call<{ id: string }>(ops, "POST", `/console/requests/${req.id}/assignments`, { provider_id: p.userId, selection_reason: "Closest available" });
  await call(p.token, "POST", `/provider/offers/${a.id}/accept`);
  const visit = await one<{ id: string }>(`SELECT id FROM visits WHERE request_id = $1`, req.id);
  return { requestId: req.id, reference: req.reference, assignmentId: a.id, visitId: visit.id };
}

/** The provider checks in with the household's door code and checks out, through the API as the apps do. */
export async function workDone(c: Person, p: Person, visitId: string) {
  const { code } = await call<{ code: string }>(c.token, "GET", `/client/visits/${visitId}/checkin-code`);
  const now = () => new Date().toISOString();
  await call(p.token, "POST", `/provider/visits/${visitId}/check-in`, { client_event_id: crypto.randomUUID(), method: "code", code, device_recorded_at: now() });
  await call(p.token, "POST", `/provider/visits/${visitId}/check-out`, { client_event_id: crypto.randomUUID(), notes: "Done", device_recorded_at: now() });
}
