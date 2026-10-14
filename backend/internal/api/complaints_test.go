package api_test

import (
	"context"
	"testing"
)

func TestComplaintsAndDiscipline(t *testing.T) {
	h := newHarness(t)
	w := h.newWorld()
	h.staff("cmgr", "cm@example.com", "complaints_manager")
	h.staff("cmgr2", "cm2@example.com", "complaints_manager")
	h.staff("supervisor", "sup@example.com", "supervisor")
	h.staff("triage", "tr@example.com", "triage_officer")
	_, visit := h.runJob(w, "triage")
	reqID := h.reqOfVisit(visit)

	// a safeguarding complaint from the client suspends the provider at once
	c := h.expect(201, "client", "POST", "/client/complaints", map[string]any{"request_id": reqID, "visit_id": visit, "severity": "safeguarding", "summary": "Unsafe behaviour"})
	cid := c["id"].(string)
	if c["status"] != "open" {
		t.Fatalf("new complaint: %v", c)
	}
	if st := h.expect(200, "triage", "GET", "/console/providers/"+w.providerID, nil)["status"]; st != "suspended" {
		t.Fatalf("interim suspension expected, provider is %v", st)
	}
	if n := len(h.expect(200, "client", "GET", "/client/complaints", nil)["data"].([]any)); n != 1 {
		t.Fatalf("client complaints: %d", n)
	}
	h.expect(200, "client", "GET", "/client/complaints/"+cid, nil)
	h.expect(403, "provider", "GET", "/client/complaints/"+cid, nil)
	h.expect(403, "triage", "GET", "/console/complaints/"+cid, nil)
	h.expect(200, "cmgr", "GET", "/console/complaints/"+cid, nil)
	if l := h.expect(200, "cmgr", "GET", "/console/complaints?status=open", nil)["data"].([]any); len(l) != 1 {
		t.Fatalf("console list: %v", l)
	}

	// the lifecycle cannot be skipped
	h.expect(409, "cmgr", "POST", "/console/complaints/"+cid+"/decision", map[string]any{"outcome_summary": "Too early to decide", "action": "warning", "reason": "too early"})
	inv := h.staffID("supervisor")
	h.expect(200, "cmgr", "POST", "/console/complaints/"+cid+"/assign", map[string]any{"investigator_id": inv})
	h.expect(201, "supervisor", "POST", "/console/complaints/"+cid+"/events", map[string]any{"event_type": "note", "body": "Spoke to the client", "is_internal": true})
	h.expect(200, "supervisor", "POST", "/console/complaints/"+cid+"/request-provider-account", nil)
	if n := len(h.expectList(200, "provider", "GET", "/provider/complaints")); n != 1 {
		t.Fatalf("provider complaints: %d", n)
	}
	h.expect(200, "provider", "POST", "/provider/complaints/"+cid+"/account", map[string]any{"account": "I was polite throughout"})
	h.expect(409, "cmgr", "POST", "/console/complaints/"+cid+"/decision", map[string]any{"outcome_summary": "Too early to decide", "action": "warning", "reason": "no report yet"})
	h.expect(201, "supervisor", "POST", "/console/complaints/"+cid+"/report", map[string]any{"investigation_report": "Substantiated by two witnesses"})

	// disqualification waits for a second officer
	d := h.expect(200, "cmgr", "POST", "/console/complaints/"+cid+"/decision", map[string]any{"outcome_summary": "Substantiated", "action": "disqualification", "reason": "Serious conduct breach"})
	if d["status"] != "decided" {
		t.Fatalf("decision: %v", d)
	}
	acts := h.expectList(200, "cmgr", "GET", "/console/providers/"+w.providerID+"/disciplinary-actions")
	var pending string
	for _, a := range acts {
		if m := a.(map[string]any); m["kind"] == "disqualification" {
			pending, _ = m["id"].(string)
		}
	}
	if pending == "" {
		t.Fatalf("no disqualification recorded: %v", acts)
	}
	if st := h.expect(200, "triage", "GET", "/console/providers/"+w.providerID, nil)["status"]; st == "disqualified" {
		t.Fatal("disqualified before authorisation")
	}
	h.expect(409, "cmgr", "POST", "/console/disciplinary-actions/"+pending+"/authorise", nil)
	h.expect(200, "cmgr2", "POST", "/console/disciplinary-actions/"+pending+"/authorise", nil)
	if st := h.expect(200, "triage", "GET", "/console/providers/"+w.providerID, nil)["status"]; st != "disqualified" {
		t.Fatalf("provider should be disqualified, is %v", st)
	}
	h.expect(200, "cmgr", "POST", "/console/disciplinary-actions/"+pending+"/revoke", map[string]any{"reason": "Appeal upheld"})

	// both sides are told before closing
	h.expect(422, "cmgr", "POST", "/console/complaints/"+cid+"/close", nil)
	h.expect(200, "cmgr", "POST", "/console/complaints/"+cid+"/communicate", map[string]any{"to": "client", "message": "We have acted on your complaint"})
	h.expect(200, "cmgr", "POST", "/console/complaints/"+cid+"/communicate", map[string]any{"to": "provider", "message": "A decision has been made"})
	if cl := h.expect(200, "cmgr", "POST", "/console/complaints/"+cid+"/close", nil); cl["status"] != "closed" {
		t.Fatalf("close: %v", cl)
	}

	// staff can also open one, and record a spot check that raises a complaint
	h.expect(201, "supervisor", "POST", "/console/complaints", map[string]any{"client_id": w.clientID, "provider_id": w.providerID, "severity": "quality", "summary": "Phoned in"})
	h.expect(201, "supervisor", "POST", "/console/spot-checks", map[string]any{"provider_id": w.providerID, "method": "phone_client", "scores": map[string]any{"punctuality": 2}, "total_score": 2, "raise_complaint": true})
	if l := h.expect(200, "supervisor", "GET", "/console/spot-checks", nil)["data"].([]any); len(l) != 1 {
		t.Fatalf("spot checks: %v", l)
	}
	h.expect(403, "triage", "GET", "/console/spot-checks", nil)
}

func (h *harness) staffID(who string) string {
	h.t.Helper()
	var id string
	if err := h.pool.QueryRow(context.Background(), `SELECT id::text FROM users WHERE full_name = $1`, who).Scan(&id); err != nil {
		h.t.Fatal(err)
	}
	return id
}
