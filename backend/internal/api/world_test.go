package api_test

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// world is a small seeded marketplace shared by the module tests: one area, category and service,
// an active provider and a client with a default address.
type world struct {
	areaID, catID, svcID, addrID string
	clientID, providerID         string
}

func (h *harness) newWorld() *world {
	h.t.Helper()
	ctx := context.Background()
	w := &world{}
	must := func(err error) {
		h.t.Helper()
		if err != nil {
			h.t.Fatal(err)
		}
	}
	must(h.pool.QueryRow(ctx, `INSERT INTO service_areas (slug, name, is_active, centre_lat, centre_lng, radius_km) VALUES ('kilimani','Kilimani',true,-1.2921,36.7872,3) RETURNING id`).Scan(&w.areaID))
	must(h.pool.QueryRow(ctx, `INSERT INTO service_categories (slug, name, engagement, is_active) VALUES ('cleaning','Cleaning','one_off',true) RETURNING id`).Scan(&w.catID))
	must(h.pool.QueryRow(ctx, `INSERT INTO services (category_id, slug, name, pricing_unit, scope_items, is_active, default_duration_minutes)
		VALUES ($1,'standard-clean','Standard clean','per_job','["Kitchen","Bathrooms"]',true,180) RETURNING id`, w.catID).Scan(&w.svcID))
	h.sql(`INSERT INTO rate_card_entries (service_id, client_price_minor, provider_pay_minor, valid_from) VALUES ($1, 250000, 200000, now() - interval '1 day')`, w.svcID)
	w.clientID = h.register("client", "+254700000001", "Wanjiru Kamau", "client")
	w.providerID = h.register("provider", "+254700000002", "Peter Otieno", "provider")
	h.sql(`UPDATE providers SET status = 'active', max_concurrent_jobs = 2, home_lat = -1.2900, home_lng = 36.7900, activated_at = now() WHERE user_id = $1`, w.providerID)
	h.sql(`INSERT INTO provider_categories (provider_id, category_id, status) VALUES ($1,$2,'active')`, w.providerID, w.catID)
	a := h.expect(201, "client", "POST", "/client/addresses", map[string]any{"label": "Home", "service_area_id": w.areaID, "lat": -1.2921, "lng": 36.7872})
	w.addrID = a["id"].(string)
	return w
}

// accepted books and assigns a job and has the provider accept it, leaving one visit scheduled.
func (h *harness) accepted(w *world, triage string) (reqID, assignID, visitID string) {
	h.t.Helper()
	r := h.expect(201, "client", "POST", "/client/requests", map[string]any{"accept_terms": true, "service_id": w.svcID, "address_id": w.addrID,
		"requested_start": time.Now().Add(30 * time.Minute).UTC().Format(time.RFC3339)})
	reqID = r["id"].(string)
	h.expect(200, triage, "POST", "/console/requests/"+reqID+"/claim", nil)
	a := h.expect(201, triage, "POST", "/console/requests/"+reqID+"/assignments", map[string]any{"provider_id": w.providerID, "selection_reason": "Closest"})
	assignID = a["id"].(string)
	h.expect(200, "provider", "POST", "/provider/offers/"+assignID+"/accept", nil)
	for _, v := range h.expectList(200, "provider", "GET", "/provider/visits") {
		if v.(map[string]any)["request_id"] == reqID {
			visitID = v.(map[string]any)["id"].(string)
		}
	}
	return reqID, assignID, visitID
}

// checkIn checks the provider in with the client's door code.
func (h *harness) checkIn(visitID string) {
	h.t.Helper()
	code := h.expect(200, "client", "GET", "/client/visits/"+visitID+"/checkin-code", nil)["code"].(string)
	h.expect(200, "provider", "POST", "/provider/visits/"+visitID+"/check-in", map[string]any{"client_event_id": uuid.NewString(), "method": "code", "code": code,
		"device_recorded_at": time.Now().UTC().Format(time.RFC3339)})
}

func (h *harness) checkOut(visitID string) {
	h.t.Helper()
	h.expect(200, "provider", "POST", "/provider/visits/"+visitID+"/check-out", map[string]any{"client_event_id": uuid.NewString(), "device_recorded_at": time.Now().Add(2 * time.Second).UTC().Format(time.RFC3339)})
}

// runJob books, assigns, accepts, works and checks out a job, leaving it awaiting the client's rating.
func (h *harness) runJob(w *world, triage string) (reqID, visitID string) {
	h.t.Helper()
	reqID, _, visitID = h.accepted(w, triage)
	h.checkIn(visitID)
	h.checkOut(visitID)
	return reqID, visitID
}

func (h *harness) reqOfVisit(visitID string) string {
	h.t.Helper()
	var id string
	if err := h.pool.QueryRow(context.Background(), `SELECT request_id FROM visits WHERE id = $1`, visitID).Scan(&id); err != nil {
		h.t.Fatal(err)
	}
	return id
}
