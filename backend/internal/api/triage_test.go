package api_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestTriageExtrasAndHouseholds(t *testing.T) {
	h := newHarness(t)
	w := h.newWorld()
	h.staff("triage", "triage@homend.test", "triage_officer")
	h.staff("supervisor", "supervisor@homend.test", "supervisor")
	h.staff("compliance", "compliance@homend.test", "compliance")
	h.staff("cmgr", "cmgr@homend.test", "complaints_manager")

	// a second provider so there is a shortlist to choose from
	p2 := h.register("provider2", "+254700000003", "Mary Atieno", "provider")
	h.sql(`UPDATE providers SET status = 'active', max_concurrent_jobs = 2, home_lat = -1.2950, home_lng = 36.7800, activated_at = now() WHERE user_id = $1`, p2)
	h.sql(`INSERT INTO provider_categories (provider_id, category_id, status) VALUES ($1,$2,'active')`, p2, w.catID)

	// ---- shortlist: share, choose, assign
	r := h.expect(201, "client", "POST", "/client/requests", map[string]any{"service_id": w.svcID, "address_id": w.addrID,
		"requested_start": time.Now().Add(30 * time.Minute).UTC().Format(time.RFC3339)})
	reqID := r["id"].(string)
	h.expect(200, "triage", "POST", "/console/requests/"+reqID+"/claim", nil)
	h.expect(422, "triage", "POST", "/console/requests/"+reqID+"/share-shortlist", map[string]any{"entry_ids": []string{uuid.NewString(), uuid.NewString()}})
	sl := h.expectList(200, "triage", "POST", "/console/requests/"+reqID+"/shortlist")
	if len(sl) != 2 {
		t.Fatalf("shortlist: %v", sl)
	}
	e1, e2 := sl[0].(map[string]any)["id"].(string), sl[1].(map[string]any)["id"].(string)
	h.expect(422, "triage", "POST", "/console/requests/"+reqID+"/share-shortlist", map[string]any{"entry_ids": []string{e1}})
	h.expect(422, "triage", "POST", "/console/requests/"+reqID+"/share-shortlist", map[string]any{"entry_ids": []string{e1, e1}})
	if got := h.expectList(200, "client", "GET", "/client/requests/"+reqID+"/shortlist"); len(got) != 0 {
		t.Fatalf("nothing is shared yet: %v", got)
	}
	shared := h.expect(200, "triage", "POST", "/console/requests/"+reqID+"/share-shortlist", map[string]any{"entry_ids": []string{e1, e2}})
	if shared["status"] != "awaiting_client_choice" {
		t.Fatalf("shared: %v", shared["status"])
	}
	cl := h.expectList(200, "client", "GET", "/client/requests/"+reqID+"/shortlist")
	if len(cl) != 2 || cl[0].(map[string]any)["provider"].(map[string]any)["phone"] != nil {
		t.Fatalf("client shortlist: %v", cl)
	}
	h.expect(404, "client", "POST", "/client/requests/"+reqID+"/shortlist/"+uuid.NewString()+"/select", nil)
	h.expect(200, "client", "POST", "/client/requests/"+reqID+"/shortlist/"+e2+"/select", nil)
	var picked string
	for _, e := range h.expectList(200, "triage", "GET", "/console/requests/"+reqID+"/shortlist") {
		if e.(map[string]any)["client_selected"] == true {
			picked = e.(map[string]any)["provider"].(map[string]any)["id"].(string)
		}
	}
	if picked == "" {
		t.Fatal("selection not recorded")
	}
	h.expect(403, "provider", "GET", "/client/requests/"+reqID+"/shortlist", nil)
	a := h.expect(201, "triage", "POST", "/console/requests/"+reqID+"/assignments", map[string]any{"provider_id": picked, "selection_reason": "Client's choice"})
	who := "provider"
	if picked == p2 {
		who = "provider2"
	}
	h.expect(200, who, "POST", "/provider/offers/"+a["id"].(string)+"/accept", nil)

	// ---- cancel by management
	r2 := h.expect(201, "client", "POST", "/client/requests", map[string]any{"service_id": w.svcID, "address_id": w.addrID,
		"requested_start": time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)})
	h.expect(422, "triage", "POST", "/console/requests/"+r2["id"].(string)+"/cancel", map[string]any{"reason": "Client phoned", "cancelled_by": "nobody"})
	c := h.expect(200, "triage", "POST", "/console/requests/"+r2["id"].(string)+"/cancel", map[string]any{"reason": "Client phoned to cancel", "cancelled_by": "client", "waive_fee": true})
	if c["status"] != "cancelled" || c["cancelled_by"] != "client" {
		t.Fatalf("cancel: %v", c)
	}
	h.expect(409, "triage", "POST", "/console/requests/"+r2["id"].(string)+"/cancel", map[string]any{"reason": "Again please", "cancelled_by": "client"})

	// ---- visits: schedule, move, miss, check in by hand
	reqB, _, visitB := h.accepted(w, "triage")
	day := time.Now().Add(72 * time.Hour).UTC().Truncate(time.Hour)
	sched := h.expect(201, "triage", "POST", "/console/requests/"+reqB+"/visits", map[string]any{"visits": []map[string]any{
		{"scheduled_start": day.Format(time.RFC3339), "scheduled_end": day.Add(3 * time.Hour).Format(time.RFC3339)},
		{"scheduled_start": day.Add(24 * time.Hour).Format(time.RFC3339), "scheduled_end": day.Add(27 * time.Hour).Format(time.RFC3339)},
	}})
	_ = sched
	h.expect(409, "triage", "POST", "/console/requests/"+reqB+"/visits", map[string]any{"visits": []map[string]any{
		{"scheduled_start": day.Add(time.Hour).Format(time.RFC3339), "scheduled_end": day.Add(2 * time.Hour).Format(time.RFC3339)}}})
	h.expect(422, "triage", "POST", "/console/requests/"+reqB+"/visits", map[string]any{"visits": []map[string]any{
		{"scheduled_start": day.Format(time.RFC3339), "scheduled_end": day.Add(-time.Hour).Format(time.RFC3339)}}})
	visits := h.expectList(200, "provider", "GET", "/provider/visits")
	var later, latest string
	for _, v := range visits {
		m := v.(map[string]any)
		if m["request_id"] == reqB && m["id"] != visitB {
			if later == "" {
				later = m["id"].(string)
			} else {
				latest = m["id"].(string)
			}
		}
	}
	if later == "" || latest == "" {
		t.Fatalf("scheduled visits: %v", visits)
	}
	moved := h.expect(200, "triage", "PATCH", "/console/visits/"+later, map[string]any{"scheduled_start": day.Add(96 * time.Hour).Format(time.RFC3339), "scheduled_end": day.Add(99 * time.Hour).Format(time.RFC3339)})
	if moved["scheduled_start"] == nil {
		t.Fatalf("moved: %v", moved)
	}
	h.expect(422, "triage", "PATCH", "/console/visits/"+later, map[string]any{"status": "missed"})
	h.expect(422, "triage", "PATCH", "/console/visits/"+later, map[string]any{"status": "completed", "reason": "not allowed"})
	missed := h.expect(200, "supervisor", "PATCH", "/console/visits/"+later, map[string]any{"status": "missed", "reason": "Provider did not arrive"})
	if missed["status"] != "missed" {
		t.Fatalf("missed: %v", missed)
	}
	h.expect(409, "triage", "PATCH", "/console/visits/"+later, map[string]any{"status": "cancelled", "reason": "Already missed"})
	h.expect(200, "triage", "PATCH", "/console/visits/"+latest, map[string]any{"status": "cancelled", "reason": "Client away"})
	by := h.expect(201, "supervisor", "POST", "/console/visits/"+visitB+"/manual-check-in", nil)
	if by["status"] != "checked_in" || by["checkin_method"] != "console" {
		t.Fatalf("manual check-in: %v", by)
	}
	h.expect(409, "supervisor", "POST", "/console/visits/"+visitB+"/manual-check-in", nil)
	h.expect(403, "cmgr", "POST", "/console/visits/"+visitB+"/manual-check-in", nil)
	h.checkOut(visitB)
	if st := h.expect(200, "triage", "GET", "/console/requests/"+reqB, nil)["status"]; st != "awaiting_rating" {
		t.Fatalf("request after the only open visit is done: %v", st)
	}

	// ---- exceptions: the no-show is flagged, and resolving it can open a complaint
	ex := h.expect(200, "supervisor", "GET", "/console/exceptions?kind=no_show&open=true", nil)["data"].([]any)
	if len(ex) != 1 {
		t.Fatalf("exceptions: %v", ex)
	}
	exID := ex[0].(map[string]any)["id"].(string)
	h.expect(422, "supervisor", "POST", "/console/exceptions/"+exID+"/resolve", map[string]any{"resolution": ""})
	done := h.expect(200, "supervisor", "POST", "/console/exceptions/"+exID+"/resolve", map[string]any{"resolution": "Provider was ill; client rebooked", "open_complaint": true})
	if done["reviewed_at"] == nil || done["resolution"] == nil {
		t.Fatalf("resolved: %v", done)
	}
	h.expect(409, "supervisor", "POST", "/console/exceptions/"+exID+"/resolve", map[string]any{"resolution": "Twice is too many"})
	if open := h.expect(200, "supervisor", "GET", "/console/exceptions?kind=no_show&open=true", nil)["data"].([]any); len(open) != 0 {
		t.Fatalf("still open: %v", open)
	}
	if cs := h.expect(200, "cmgr", "GET", "/console/complaints?status=open", nil)["data"].([]any); len(cs) != 1 {
		t.Fatalf("complaint from the exception: %v", cs)
	}

	// ---- households: identity checks, suspension
	clients := h.expect(200, "triage", "GET", "/console/clients?q=Wanjiru", nil)["data"].([]any)
	if len(clients) != 1 {
		t.Fatalf("client search: %v", clients)
	}
	h.expect(200, "cmgr", "GET", "/console/clients?status=active&id_verification=none", nil)
	detail := h.expect(200, "triage", "GET", "/console/clients/"+w.clientID, nil)
	if detail["request_count"].(float64) < 3 || len(detail["addresses"].([]any)) != 1 || detail["phone"] != "+254700000001" {
		t.Fatalf("client detail: %v", detail)
	}
	if _, leaked := detail["addresses"].([]any)[0].(map[string]any)["access_details"]; leaked {
		t.Fatal("gate codes must not be shown to staff")
	}
	h.expect(404, "triage", "GET", "/console/clients/"+uuid.NewString(), nil)
	h.expect(422, "client", "POST", "/client/id-verification", map[string]any{"national_id_number": "12", "document_id": uuid.NewString()})
	h.expect(422, "client", "POST", "/client/id-verification", map[string]any{"national_id_number": "12345678", "document_id": uuid.NewString()})
	photo := h.upload("client", "other", []byte("%PDF-1.4 pin"), nil)
	h.expect(422, "client", "POST", "/client/id-verification", map[string]any{"national_id_number": "12345678", "document_id": photo})
	idDoc := h.upload("client", "national_id", []byte("%PDF-1.4 id card"), nil)
	if got := h.expect(202, "client", "POST", "/client/id-verification", map[string]any{"national_id_number": "12345678", "document_id": idDoc}); got["status"] != "pending" {
		t.Fatalf("id verification: %v", got)
	}
	h.expect(422, "compliance", "POST", "/console/clients/"+w.clientID+"/id-decision", map[string]any{"decision": "rejected"})
	h.expect(200, "compliance", "POST", "/console/clients/"+w.clientID+"/id-decision", map[string]any{"decision": "rejected", "reason": "Photo is blurred"})
	h.expect(409, "compliance", "POST", "/console/clients/"+w.clientID+"/id-decision", map[string]any{"decision": "verified"})
	h.expect(202, "client", "POST", "/client/id-verification", map[string]any{"national_id_number": "12345678", "document_id": idDoc})
	ok := h.expect(200, "triage", "POST", "/console/clients/"+w.clientID+"/id-decision", map[string]any{"decision": "verified"})
	if ok["id_verification"] != "verified" {
		t.Fatalf("verified: %v", ok)
	}
	h.expect(409, "client", "POST", "/client/id-verification", map[string]any{"national_id_number": "12345678", "document_id": idDoc})
	h.expect(403, "triage", "POST", "/console/clients/"+w.clientID+"/suspend", map[string]any{"reason": "Abusive to staff"})
	h.expect(422, "cmgr", "POST", "/console/clients/"+w.clientID+"/suspend", map[string]any{"reason": "x"})
	if s := h.expect(200, "cmgr", "POST", "/console/clients/"+w.clientID+"/suspend", map[string]any{"reason": "Abusive to staff"}); s["status"] != "suspended" {
		t.Fatalf("suspend: %v", s)
	}
	h.expect(401, "client", "GET", "/client/addresses", nil) // sessions end with the suspension
	h.expect(409, "cmgr", "POST", "/console/clients/"+w.clientID+"/suspend", map[string]any{"reason": "Twice"})
	if s := h.expect(200, "cmgr", "POST", "/console/clients/"+w.clientID+"/reinstate", map[string]any{"reason": "Apologised"}); s["status"] != "active" {
		t.Fatalf("reinstate: %v", s)
	}
}
