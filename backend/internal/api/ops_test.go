package api_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestJobsVariationsAndSync(t *testing.T) {
	h := newHarness(t)
	w := h.newWorld()
	h.staff("triage", "triage@homend.test", "triage_officer")

	reqID, assignID, visitID := h.accepted(w, "triage")

	// ---- the provider sees the job
	jobs := h.expect(200, "provider", "GET", "/provider/jobs", nil)["data"].([]any)
	if len(jobs) != 1 || jobs[0].(map[string]any)["assignment_id"] != assignID {
		t.Fatalf("jobs: %v", jobs)
	}
	h.expect(200, "provider", "GET", "/provider/jobs/"+assignID, nil)
	h.expect(403, "client", "GET", "/provider/jobs/"+assignID, nil)
	h.expect(404, "provider", "GET", "/provider/jobs/"+uuid.NewString(), nil)
	if got := h.expect(200, "provider", "GET", "/provider/jobs?status=closed", nil)["data"].([]any); len(got) != 0 {
		t.Fatalf("status filter: %v", got)
	}

	// ---- variations: propose, approve, reject, decide for the client
	h.expect(422, "provider", "POST", "/provider/jobs/"+assignID+"/variations", map[string]any{"description": "x"})
	h.expect(404, "provider", "POST", "/provider/jobs/"+uuid.NewString()+"/variations", map[string]any{"description": "Extra room needs cleaning"})
	v1 := h.expect(201, "provider", "POST", "/provider/jobs/"+assignID+"/variations", map[string]any{"description": "Extra bedroom to clean", "visit_id": visitID})
	if v1["client_delta_minor"] != 250000.0 || v1["provider_delta_minor"] != 200000.0 || v1["status"] != "pending" {
		t.Fatalf("variation: %v", v1)
	}
	v2 := h.expect(201, "provider", "POST", "/provider/jobs/"+assignID+"/variations", map[string]any{"description": "Balcony as well", "quantity": 2})
	v3 := h.expect(201, "provider", "POST", "/provider/jobs/"+assignID+"/variations", map[string]any{"description": "Windows inside and out"})
	list := h.expectList(200, "client", "GET", "/client/requests/"+reqID+"/variations")
	if len(list) != 3 {
		t.Fatalf("client variations: %v", list)
	}
	if _, leaked := list[0].(map[string]any)["provider_delta_minor"]; leaked {
		t.Fatal("the client must not see the provider's share")
	}
	if ev := h.expect(200, "client", "GET", "/notifications?unread=true", nil)["data"].([]any); len(ev) == 0 {
		t.Fatal("the client was not told about the change")
	}
	h.expect(403, "provider", "POST", "/client/variations/"+v1["id"].(string)+"/approve", nil)
	h.expect(200, "client", "POST", "/client/variations/"+v1["id"].(string)+"/approve", nil)
	h.expect(409, "client", "POST", "/client/variations/"+v1["id"].(string)+"/approve", nil)
	h.expect(422, "client", "POST", "/client/variations/"+v2["id"].(string)+"/reject", map[string]any{"reason": "no"})
	rej := h.expect(200, "client", "POST", "/client/variations/"+v2["id"].(string)+"/reject", map[string]any{"reason": "Not needed today"})
	if rej["status"] != "rejected" {
		t.Fatalf("reject: %v", rej)
	}
	// triage sees what is still waiting and can answer for a client who cannot
	queue := h.expect(200, "triage", "GET", "/console/variations", nil)["data"].([]any)
	if len(queue) != 1 || queue[0].(map[string]any)["id"] != v3["id"] {
		t.Fatalf("queue: %v", queue)
	}
	h.expect(422, "triage", "POST", "/console/variations/"+v3["id"].(string)+"/decision", map[string]any{"decision": "maybe"})
	h.expect(422, "triage", "POST", "/console/variations/"+v3["id"].(string)+"/decision", map[string]any{"decision": "reject"})
	h.expect(200, "triage", "POST", "/console/variations/"+v3["id"].(string)+"/decision", map[string]any{"decision": "reject", "reason": "Out of scope for this visit"})
	if got := h.expect(200, "triage", "GET", "/console/variations?status=rejected", nil)["data"].([]any); len(got) != 2 {
		t.Fatalf("rejected: %v", got)
	}
	staffView := h.expect(200, "triage", "GET", "/console/requests/"+reqID, nil)
	if len(staffView["variations"].([]any)) != 3 {
		t.Fatalf("request variations: %v", staffView["variations"])
	}

	// a change nobody answered falls away at the last check-out
	late := h.expect(201, "provider", "POST", "/provider/jobs/"+assignID+"/variations", map[string]any{"description": "Oven deep clean"})
	h.checkIn(visitID)
	h.checkOut(visitID)
	if got := h.expect(200, "triage", "GET", "/console/variations?status=withdrawn", nil)["data"].([]any); len(got) != 1 || got[0].(map[string]any)["id"] != late["id"] {
		t.Fatalf("withdrawn: %v", got)
	}
	h.expect(409, "provider", "POST", "/provider/jobs/"+assignID+"/variations", map[string]any{"description": "Too late for this"})

	// the approved change is on the invoice: 2500 service + 2500 variation, VAT included
	inv := h.expect(200, "client", "GET", "/client/invoices", nil)["data"].([]any)[0].(map[string]any)
	if inv["total_minor"] != 500000.0 || inv["vat_minor"] != 68966.0 || inv["subtotal_minor"] != 431034.0 {
		t.Fatalf("invoice: %v", inv)
	}

	// ---- earnings carry the change: the provider earns 2000 + 2000 of the 5000 less its VAT
	h.expect(201, "client", "POST", "/client/requests/"+reqID+"/rating", map[string]any{"stars": 5})
	earn := h.expect(200, "provider", "GET", "/provider/earnings", nil)["data"].([]any)
	if len(earn) != 1 || earn[0].(map[string]any)["net_minor"] != 400000.0 || earn[0].(map[string]any)["gross_minor"] != 431034.0 {
		t.Fatalf("earnings: %v", earn)
	}
	if got := h.expect(200, "provider", "GET", "/provider/earnings?status=held", nil)["data"].([]any); len(got) != 0 {
		t.Fatalf("held: %v", got)
	}
	sum := h.expect(200, "provider", "GET", "/provider/earnings/summary", nil)
	if sum["pending_minor"] != 400000.0 || sum["held_minor"] != 0.0 || sum["next_payout_date"] == nil {
		t.Fatalf("summary: %v", sum)
	}
	if got := h.expect(200, "provider", "GET", "/provider/payouts", nil)["data"].([]any); len(got) != 0 {
		t.Fatalf("payouts: %v", got)
	}

	// ---- offline sync: out-of-order events, a replay and a bad door code
	_, assign2, visit2 := h.accepted(w, "triage")
	code := h.expect(200, "client", "GET", "/client/visits/"+visit2+"/checkin-code", nil)["code"].(string)
	now := time.Now().UTC()
	inID, outID, badID, noteID := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	batch := map[string]any{"events": []map[string]any{
		{"client_event_id": outID, "visit_id": visit2, "event_type": "check_out", "device_recorded_at": now.Add(2 * time.Minute).Format(time.RFC3339), "payload": map[string]any{"notes": "All done"}},
		{"client_event_id": inID, "visit_id": visit2, "event_type": "check_in", "device_recorded_at": now.Format(time.RFC3339), "payload": map[string]any{"method": "code", "code": code}},
		{"client_event_id": badID, "visit_id": uuid.NewString(), "event_type": "check_in", "device_recorded_at": now.Format(time.RFC3339), "payload": map[string]any{"method": "code", "code": "0000"}},
		{"client_event_id": noteID, "visit_id": visit2, "event_type": "note", "device_recorded_at": now.Format(time.RFC3339), "payload": map[string]any{"notes": "Keys left with the guard"}},
	}}
	res := h.expect(200, "provider", "POST", "/provider/sync", batch)["results"].([]any)
	status := map[string]string{}
	for _, r := range res {
		status[r.(map[string]any)["client_event_id"].(string)] = r.(map[string]any)["status"].(string)
	}
	if status[inID] != "applied" || status[outID] != "applied" || status[badID] != "rejected" || status[noteID] != "applied" {
		t.Fatalf("sync results: %v", res)
	}
	for _, r := range res {
		if r.(map[string]any)["status"] == "rejected" && r.(map[string]any)["error"] == nil {
			t.Fatalf("a rejected event must say why: %v", r)
		}
	}
	again := h.expect(200, "provider", "POST", "/provider/sync", batch)["results"].([]any)
	for _, r := range again {
		if m := r.(map[string]any); m["client_event_id"] != badID && m["status"] != "duplicate" {
			t.Fatalf("replay should be duplicate: %v", again)
		}
	}
	if st := h.expect(200, "client", "GET", "/client/requests/"+h.reqOfVisit(visit2), nil)["status"]; st != "awaiting_rating" {
		t.Fatalf("job after sync: %v", st)
	}
	h.expect(422, "provider", "POST", "/provider/sync", map[string]any{"events": []any{}})
	_ = assign2
}
