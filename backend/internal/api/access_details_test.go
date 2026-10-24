package api_test

import (
	"testing"
	"time"
)

// Gate codes follow the visit, not the time the household first asked for: a job booked for later in
// the week and moved to this afternoon releases them, and one moved back out hides them again.
func TestAccessDetailsFollowRescheduledVisit(t *testing.T) {
	h := newHarness(t)
	w := h.newWorld()
	h.staff("triage", "triage@homend.test", "triage_officer")
	h.expect(200, "client", "PATCH", "/client/addresses/"+w.addrID, map[string]any{"access_details": "Gate code 4321"})

	r := h.expect(201, "client", "POST", "/client/requests", map[string]any{"accept_terms": true, "service_id": w.svcID, "address_id": w.addrID,
		"requested_start": time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)})
	reqID := r["id"].(string)
	h.expect(200, "triage", "POST", "/console/requests/"+reqID+"/claim", nil)
	a := h.expect(201, "triage", "POST", "/console/requests/"+reqID+"/assignments", map[string]any{"provider_id": w.providerID, "selection_reason": "Closest"})
	job := h.expect(200, "provider", "POST", "/provider/offers/"+a["id"].(string)+"/accept", nil)
	if job["address"].(map[string]any)["access_details"] != nil {
		t.Fatal("two days out, the gate details must stay hidden")
	}
	var visitID string
	for _, v := range h.expectList(200, "provider", "GET", "/provider/visits") {
		if v.(map[string]any)["request_id"] == reqID {
			visitID = v.(map[string]any)["id"].(string)
		}
	}
	accessDetails := func() any {
		return h.expect(200, "provider", "GET", "/provider/jobs/"+a["id"].(string), nil)["address"].(map[string]any)["access_details"]
	}

	soon := time.Now().Add(30 * time.Minute).UTC()
	h.expect(200, "triage", "PATCH", "/console/visits/"+visitID, map[string]any{"scheduled_start": soon.Format(time.RFC3339), "scheduled_end": soon.Add(3 * time.Hour).Format(time.RFC3339)})
	if accessDetails() == nil {
		t.Fatal("moved to this afternoon, so the gate details should be released")
	}

	later := time.Now().Add(72 * time.Hour).UTC()
	h.expect(200, "triage", "PATCH", "/console/visits/"+visitID, map[string]any{"scheduled_start": later.Format(time.RFC3339), "scheduled_end": later.Add(3 * time.Hour).Format(time.RFC3339)})
	if accessDetails() != nil {
		t.Fatal("moved back out, so the gate details should be hidden again")
	}
}
