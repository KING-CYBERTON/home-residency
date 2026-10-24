package api_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"
)

// upload runs the three-step vault upload: ticket, PUT to the signed URL, confirm.
func (h *harness) upload(who, docType string, content []byte, extra map[string]any) string {
	h.t.Helper()
	body := map[string]any{"document_type": docType, "mime_type": "application/pdf", "size_bytes": len(content), "original_filename": docType + ".pdf"}
	for k, v := range extra {
		body[k] = v
	}
	ticket := h.expect(201, who, "POST", "/documents/uploads", body)
	doc := ticket["document"].(map[string]any)
	if doc["status"] != "pending_upload" {
		h.t.Fatalf("ticket: %v", ticket)
	}
	h.expect(422, who, "POST", "/documents/"+doc["id"].(string)+"/complete", nil) // nothing uploaded yet
	req, _ := http.NewRequest(http.MethodPut, ticket["upload_url"].(string), bytes.NewReader(content))
	for k, v := range ticket["upload_headers"].(map[string]any) {
		req.Header.Set(k, v.(string))
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		h.t.Fatalf("PUT to storage: %d", resp.StatusCode)
	}
	done := h.expect(200, who, "POST", "/documents/"+doc["id"].(string)+"/complete", nil)
	if done["status"] != "uploaded" {
		h.t.Fatalf("complete: %v", done)
	}
	return doc["id"].(string)
}

func TestVettingToActivation(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	w := h.newWorld()
	h.staff("vetter", "vetter@homend.test", "vetting_officer")
	h.staff("approver", "approver@homend.test", "approver")
	h.staff("compliance", "compliance@homend.test", "compliance")
	h.staff("trainer", "trainer@homend.test", "training_coordinator")
	h.staff("admin", "admin@homend.test", "admin")

	// a checklist for the category: a certificate, one referee, a clearance and an interview
	var tpl string
	_ = h.pool.QueryRow(ctx, `INSERT INTO checklist_templates (category_id, version, is_current) VALUES ($1,1,true) RETURNING id`, w.catID).Scan(&tpl)
	h.sql(`INSERT INTO checklist_template_items (template_id, kind, document_type, label, sort_order, validity_months) VALUES ($1,'document','good_conduct_certificate','Certificate of Good Conduct',1,12)`, tpl)
	h.sql(`INSERT INTO checklist_template_items (template_id, kind, label, min_count, sort_order) VALUES ($1,'referee','Two referees',1,2)`, tpl)
	h.sql(`INSERT INTO checklist_template_items (template_id, kind, label, sort_order) VALUES ($1,'interview','Interview',3)`, tpl)

	h.register("mary", "+254700000044", "Mary Achieng", "provider")
	cl := h.expect(200, "mary", "GET", "/provider/checklists/"+w.catID, nil)
	if len(cl["items"].([]any)) != 3 {
		t.Fatalf("checklist: %v", cl)
	}
	h.expect(404, "mary", "GET", "/provider/checklists/"+w.areaID, nil)
	h.expect(200, "mary", "PATCH", "/provider/profile", map[string]any{"bio": "Ten years in Nairobi homes", "years_experience": 10, "payout_msisdn": "+254700000044",
		"national_id_number": "12345678", "service_area_ids": []string{w.areaID}, "languages": []string{"en", "sw"}})
	h.expect(422, "mary", "PATCH", "/provider/profile", map[string]any{"payout_msisdn": "0700"})
	var enc []byte
	_ = h.pool.QueryRow(ctx, `SELECT national_id_enc FROM providers WHERE user_id = (SELECT id FROM users WHERE phone_e164 = '+254700000044')`).Scan(&enc)
	if bytes.Contains(enc, []byte("12345678")) {
		t.Fatal("national id stored in plain text")
	}

	app := h.expect(201, "mary", "POST", "/provider/applications", map[string]any{"category_id": w.catID, "answers": map[string]any{"live_in_experience": true}})
	appID := app["id"].(string)
	h.expect(409, "mary", "POST", "/provider/applications", map[string]any{"category_id": w.catID}) // one open application per category
	if len(app["missing"].([]any)) != 2 {
		t.Fatalf("missing: %v", app["missing"])
	}
	bad := h.expect(422, "mary", "POST", "/provider/applications/"+appID+"/submit", nil)
	if m := bad["details"].(map[string]any)["missing"].([]any); len(m) != 2 {
		t.Fatalf("submit gaps: %v", bad)
	}

	docID := h.upload("mary", "good_conduct_certificate", []byte("%PDF-1.4 test certificate"), nil)
	var certItem string
	for _, it := range app["items"].([]any) {
		if it.(map[string]any)["template_item"].(map[string]any)["document_type"] == "good_conduct_certificate" {
			certItem = it.(map[string]any)["id"].(string)
		}
	}
	// a passport photo cannot stand in for the certificate
	photo := h.upload("mary", "passport_photo", []byte("%PDF-1.4 photo"), nil)
	h.expect(422, "mary", "PATCH", "/provider/applications/"+appID, map[string]any{"item_documents": []any{map[string]any{"application_item_id": certItem, "document_id": photo}}})
	h.expect(200, "mary", "PATCH", "/provider/applications/"+appID, map[string]any{"item_documents": []any{map[string]any{"application_item_id": certItem, "document_id": docID}}})
	ref := h.expect(201, "mary", "POST", "/provider/applications/"+appID+"/referees", map[string]any{"full_name": "Mrs Njeri", "relationship": "former employer", "phone": "+254711000111"})
	extra := h.expect(201, "mary", "POST", "/provider/applications/"+appID+"/referees", map[string]any{"full_name": "Mr Kip", "relationship": "supervisor", "phone": "+254711000222"})
	h.expect(204, "mary", "DELETE", "/provider/applications/"+appID+"/referees/"+extra["id"].(string), nil)
	if ref["outcome"] != "pending" {
		t.Fatalf("referee: %v", ref)
	}
	sub := h.expect(200, "mary", "POST", "/provider/applications/"+appID+"/submit", nil)
	if sub["status"] != "submitted" {
		t.Fatalf("submit: %v", sub)
	}
	h.expect(409, "mary", "PATCH", "/provider/applications/"+appID, map[string]any{"answers": map[string]any{}})
	if _, has := sub["vetting_report"]; has {
		t.Fatal("applicant must not see the vetting report")
	}

	// another provider cannot see Mary's file or her documents
	h.register("other", "+254700000045", "Other Provider", "provider")
	h.expect(404, "other", "GET", "/provider/applications/"+appID, nil)
	h.expect(404, "other", "GET", "/documents/"+docID, nil)
	h.expect(401, "", "GET", "/documents/"+docID, nil)

	// ---- vetting
	h.expect(403, "approver", "POST", "/console/applications/"+appID+"/claim", nil) // only vetting officers claim
	q := h.expect(200, "vetter", "GET", "/console/applications?status=submitted", nil)
	if len(q["data"].([]any)) != 1 || q["data"].([]any)[0].(map[string]any)["provider_name"] != "Mary Achieng" {
		t.Fatalf("queue: %v", q)
	}
	h.expect(200, "vetter", "POST", "/console/applications/"+appID+"/claim", nil)
	h.expect(409, "vetter", "POST", "/console/applications/"+appID+"/claim", nil)
	full := h.expect(200, "vetter", "GET", "/console/applications/"+appID, nil)
	if full["referees"].([]any)[0].(map[string]any)["phone"] != "+254711000111" {
		t.Fatalf("staff must see referee phone: %v", full["referees"])
	}
	items := map[string]string{}
	for _, it := range full["items"].([]any) {
		m := it.(map[string]any)
		items[m["template_item"].(map[string]any)["kind"].(string)] = m["id"].(string)
	}

	// a document item cannot be satisfied until the document itself is verified
	h.expect(422, "vetter", "PATCH", "/console/applications/"+appID+"/items/"+items["document"], map[string]any{"status": "satisfied"})
	h.expect(422, "vetter", "PATCH", "/console/applications/"+appID+"/items/"+items["document"], map[string]any{"status": "failed"}) // needs a reason
	dl := h.expect(200, "vetter", "GET", "/documents/"+docID+"/download-url?purpose=vetting+review", nil)
	if !strings.Contains(dl["url"].(string), "/storage/vault/") {
		t.Fatalf("download url: %v", dl)
	}
	h.expect(422, "vetter", "GET", "/documents/"+docID+"/download-url", nil) // staff must give a purpose
	body := h.fetch(dl["url"].(string))
	if !bytes.Contains(body, []byte("test certificate")) {
		t.Fatalf("download body: %q", body)
	}
	v := h.expect(200, "vetter", "POST", "/console/documents/"+docID+"/verify", map[string]any{"issued_on": time.Now().AddDate(0, -1, 0).Format("2006-01-02")})
	if v["status"] != "verified" || v["expires_on"] == nil {
		t.Fatalf("verify should set the expiry from the checklist's validity: %v", v)
	}
	h.expect(200, "vetter", "PATCH", "/console/applications/"+appID+"/items/"+items["document"], map[string]any{"status": "satisfied"})

	// the file cannot go to approval with the referee and interview outstanding
	h.expect(422, "vetter", "POST", "/console/applications/"+appID+"/complete-vetting", map[string]any{"vetting_report": "All documents genuine, references good."})
	h.expect(422, "vetter", "POST", "/console/applications/"+appID+"/referees/"+ref["id"].(string)+"/record", map[string]any{"outcome": "pending", "statement": "x"})
	h.expect(200, "vetter", "POST", "/console/applications/"+appID+"/referees/"+ref["id"].(string)+"/record", map[string]any{"outcome": "positive", "statement": "Honest, punctual, worked 4 years."})
	cc := h.expect(201, "vetter", "POST", "/console/applications/"+appID+"/clearance-checks", map[string]any{"document_id": docID, "agency_name": "DCI"})
	h.expect(200, "vetter", "PATCH", "/console/clearance-checks/"+cc["id"].(string), map[string]any{"result": "authentic", "agency_reference": "DCI/2026/1"})
	h.expect(201, "vetter", "POST", "/console/applications/"+appID+"/assessments", map[string]any{"kind": "interview", "rubric_version": "v1", "scores": map[string]any{"attitude": 80}, "total_score": 80, "passed": true})
	h.expect(200, "vetter", "PATCH", "/console/applications/"+appID+"/items/"+items["referee"], map[string]any{"status": "satisfied"})
	h.expect(200, "vetter", "PATCH", "/console/applications/"+appID+"/items/"+items["interview"], map[string]any{"status": "satisfied", "notes": "Passed with 80"})
	h.expect(403, "approver", "POST", "/console/applications/"+appID+"/complete-vetting", map[string]any{"vetting_report": "x"})
	done := h.expect(200, "vetter", "POST", "/console/applications/"+appID+"/complete-vetting", map[string]any{"vetting_report": "All documents genuine, references good."})
	if done["status"] != "awaiting_approval" {
		t.Fatalf("complete: %v", done)
	}

	// the vetter cannot approve; a different approver can, with a reason
	h.expect(403, "vetter", "POST", "/console/applications/"+appID+"/decision", map[string]any{"decision": "approve", "reason": "Looks good"})
	h.expect(422, "approver", "POST", "/console/applications/"+appID+"/decision", map[string]any{"decision": "approve", "reason": ""})
	ok := h.expect(200, "approver", "POST", "/console/applications/"+appID+"/decision", map[string]any{"decision": "approve", "reason": "Meets the checklist"})
	if ok["status"] != "approved" {
		t.Fatalf("decision: %v", ok)
	}
	prof := h.expect(200, "mary", "GET", "/provider/profile", nil)
	if prof["status"] != "approved" || prof["categories"].([]any)[0].(map[string]any)["status"] != "approved" {
		t.Fatalf("profile after approval: %v", prof)
	}

	// ---- training, contract, activation
	maryID := prof["id"].(string)
	h.expect(422, "approver", "POST", "/console/providers/"+maryID+"/activate", map[string]any{"category_ids": []string{w.catID}}) // not trained or contracted yet
	// The induction seeded by migration 00015 would be a second requirement; this test makes its own.
	h.sql(`UPDATE training_courses SET is_active = false WHERE code = 'GEN-IND'`)
	course := h.expect(201, "trainer", "POST", "/console/training-courses", map[string]any{"code": "IND-1", "name": "Induction", "is_induction": true, "pass_mark": 70})
	h.expect(409, "trainer", "POST", "/console/training-courses", map[string]any{"code": "IND-1", "name": "Duplicate"})
	en := h.expect(201, "trainer", "POST", "/console/trainings", map[string]any{"provider_id": maryID, "course_id": course["id"]})
	h.expect(409, "trainer", "POST", "/console/trainings", map[string]any{"provider_id": maryID, "course_id": course["id"]})
	h.expect(422, "trainer", "PATCH", "/console/trainings/"+en["id"].(string), map[string]any{"status": "passed", "score": 50})
	h.expect(200, "trainer", "PATCH", "/console/trainings/"+en["id"].(string), map[string]any{"status": "passed", "score": 88})
	if tr := h.expectList(200, "mary", "GET", "/provider/trainings"); len(tr) != 1 || tr[0].(map[string]any)["status"] != "passed" {
		t.Fatalf("trainings: %v", tr)
	}

	offer := h.expect(200, "mary", "GET", "/provider/contract", nil)
	if offer["signed_at"] != nil {
		t.Fatalf("contract: %v", offer)
	}
	_, _ = h.pool.Exec(context.Background(), `UPDATE otp_codes SET created_at = created_at - interval '5 minutes' WHERE phone_e164 = '+254700000044'`)
	otp := h.expect(200, "", "POST", "/auth/otp/request", map[string]any{"phone": "+254700000044", "purpose": "verify_phone"})
	code := regexp.MustCompile(`\b(\d{6})\b`).FindStringSubmatch(h.sms.Last().Body)[1]
	sign := map[string]any{"contract_version": offer["contract_version"], "code_of_conduct_version": offer["code_of_conduct_version"], "otp_id": otp["otp_id"], "code": "000000"}
	h.expect(400, "mary", "POST", "/provider/contract/sign", sign)
	sign["code"] = code
	signed := h.expect(200, "mary", "POST", "/provider/contract/sign", sign)
	if signed["signed_at"] == nil {
		t.Fatalf("signed: %v", signed)
	}
	act := h.expect(200, "approver", "POST", "/console/providers/"+maryID+"/activate", map[string]any{"category_ids": []string{w.catID}})
	if act["status"] != "active" || act["contract"] == nil || len(act["trainings"].([]any)) != 1 {
		t.Fatalf("activate: %v", act)
	}

	// ---- directory, suspension, appraisal, timeline
	list := h.expect(200, "vetter", "GET", "/console/providers?status=active&q=mary", nil)
	if len(list["data"].([]any)) != 1 {
		t.Fatalf("provider search: %v", list)
	}
	h.expect(403, "trainer", "POST", "/console/providers/"+maryID+"/suspend", map[string]any{"reason": "x"})
	h.expect(422, "compliance", "POST", "/console/providers/"+maryID+"/suspend", map[string]any{"reason": ""})
	sus := h.expect(200, "compliance", "POST", "/console/providers/"+maryID+"/suspend", map[string]any{"reason": "Document under review"})
	if sus["status"] != "suspended" || sus["suspended_reason"] != "Document under review" {
		t.Fatalf("suspend: %v", sus)
	}
	h.expect(409, "compliance", "POST", "/console/providers/"+maryID+"/suspend", map[string]any{"reason": "again please"})
	h.expect(200, "compliance", "POST", "/console/providers/"+maryID+"/reinstate", map[string]any{"reason": "Cleared"})
	h.expect(200, "admin", "PATCH", "/console/providers/"+maryID, map[string]any{"max_concurrent_jobs": 3, "engagement_basis": "independent_contractor"})
	ap := h.expect(200, "vetter", "GET", "/console/providers/"+maryID+"/appraisal", nil)
	if ap["provider_id"] != maryID {
		t.Fatalf("appraisal: %v", ap)
	}
	h.expect(200, "mary", "GET", "/provider/appraisal", nil)
	tl := h.expect(200, "admin", "GET", "/console/providers/"+maryID+"/timeline", nil)
	if len(tl["data"].([]any)) < 5 {
		t.Fatalf("timeline: %v", tl)
	}
	al := h.expect(200, "compliance", "GET", "/console/documents/"+docID+"/access-log", nil)
	if len(al["data"].([]any)) != 1 {
		t.Fatalf("access log: %v", al)
	}
	h.expect(403, "vetter", "GET", "/console/documents/"+docID+"/access-log", nil)

	// ---- expiry: a lapsing mandatory document shows up for compliance and the nightly job suspends
	h.sql(`UPDATE documents SET expires_on = current_date + 10 WHERE id = $1`, docID)
	ex := h.expect(200, "compliance", "GET", "/console/compliance/expiring-documents?within_days=30", nil)
	if len(ex["data"].([]any)) != 1 || ex["data"].([]any)[0].(map[string]any)["days_left"] != 10.0 {
		t.Fatalf("expiring: %v", ex)
	}
	h.sql(`UPDATE documents SET expires_on = current_date - 1 WHERE id = $1`, docID)
	h.app.ExpireDocuments(ctx)
	if p := h.expect(200, "mary", "GET", "/provider/profile", nil); p["status"] != "suspended" {
		t.Fatalf("a lapsed mandatory document must suspend the provider: %v", p["status"])
	}
	if d := h.expect(200, "mary", "GET", "/documents/"+docID, nil); d["status"] != "expired" {
		t.Fatalf("document: %v", d)
	}
	rv := h.expect(422, "compliance", "POST", "/console/providers/"+maryID+"/reinstate", map[string]any{"reason": "Try"})
	if rv["details"] == nil {
		t.Fatalf("reinstate blockers: %v", rv)
	}
	rev := h.expect(201, "compliance", "POST", "/console/providers/"+maryID+"/reverification", map[string]any{"category_id": w.catID})
	if rev["is_reverification"] != true || rev["status"] != "draft" {
		t.Fatalf("reverification: %v", rev)
	}
}

func (h *harness) fetch(url string) []byte {
	h.t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		h.t.Fatalf("GET %s: %d %s", url, resp.StatusCode, b)
	}
	return b
}
