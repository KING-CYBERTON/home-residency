package api_test

import (
	"strings"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
)

func TestAdminAndCatalogue(t *testing.T) {
	h := newHarness(t)
	h.staff("admin", "admin@homend.test", "admin")
	h.staff("approver", "approver@homend.test", "approver")
	h.staff("finance", "finance@homend.test", "finance")
	h.staff("reporting", "reporting@homend.test", "reporting")
	h.staff("supervisor", "supervisor@homend.test", "supervisor")

	// ---- service areas, categories, services
	area := h.expect(201, "admin", "POST", "/console/service-areas", map[string]any{"slug": "kilimani", "name": "Kilimani", "is_active": true, "centre_lat": -1.29, "centre_lng": 36.78, "radius_km": 3})
	h.expect(409, "admin", "POST", "/console/service-areas", map[string]any{"slug": "kilimani", "name": "Again"})
	h.expect(422, "admin", "POST", "/console/service-areas", map[string]any{"name": "No slug"})
	h.expect(200, "admin", "PATCH", "/console/service-areas/"+area["id"].(string), map[string]any{"name": "Kilimani Estate"})
	if got := h.expectList(200, "supervisor", "GET", "/console/service-areas"); len(got) != 1 || got[0].(map[string]any)["name"] != "Kilimani Estate" {
		t.Fatalf("areas: %v", got)
	}
	h.expect(403, "supervisor", "POST", "/console/service-areas", map[string]any{"slug": "x", "name": "X"})

	cat := h.expect(201, "admin", "POST", "/console/categories", map[string]any{"slug": "cleaning", "name": "Cleaning", "engagement": "one_off", "is_active": true, "sort_order": 1})
	h.expect(422, "admin", "POST", "/console/categories", map[string]any{"slug": "bad", "name": "Bad", "engagement": "weekly"})
	svc := h.expect(201, "admin", "POST", "/console/services", map[string]any{"category_id": cat["id"], "slug": "std", "name": "Standard clean", "pricing_unit": "per_job",
		"scope_items": []string{"Kitchen", "Bathrooms"}, "is_active": true, "default_duration_minutes": 180})
	svcID := svc["id"].(string)
	h.expect(422, "admin", "POST", "/console/services", map[string]any{"category_id": "00000000-0000-0000-0000-000000000000", "slug": "x", "name": "X", "pricing_unit": "per_job"})

	// ---- rate card: a new price closes the one before it
	from1 := time.Now().Add(-48 * time.Hour).UTC().Format(time.RFC3339)
	r1 := h.expect(201, "finance", "POST", "/console/rate-card", map[string]any{"service_id": svcID, "client_price_minor": 250000, "provider_pay_minor": 200000, "valid_from": from1})
	h.expect(422, "finance", "POST", "/console/rate-card", map[string]any{"service_id": svcID, "client_price_minor": 1000, "provider_pay_minor": 5000, "valid_from": time.Now().UTC().Format(time.RFC3339)})
	from2 := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	h.expect(201, "finance", "POST", "/console/rate-card", map[string]any{"service_id": svcID, "client_price_minor": 300000, "provider_pay_minor": 240000, "valid_from": from2})
	card := h.expectList(200, "finance", "GET", "/console/rate-card?service_id="+svcID)
	if len(card) != 2 {
		t.Fatalf("rate card: %v", card)
	}
	cur := h.expectList(200, "finance", "GET", "/console/rate-card?service_id="+svcID+"&current_only=true")
	if len(cur) != 1 || cur[0].(map[string]any)["client_price_minor"] != 300000.0 {
		t.Fatalf("current price: %v", cur)
	}
	_ = r1
	if got := h.expectList(200, "admin", "GET", "/console/services"); got[0].(map[string]any)["current_price"].(map[string]any)["client_price_minor"] != 300000.0 {
		t.Fatalf("service price: %v", got)
	}
	h.expect(200, "admin", "PATCH", "/console/services/"+svcID, map[string]any{"name": "Standard home clean"})

	// ---- checklists: drafted by admin, published by an approver
	tpl := h.expect(201, "admin", "POST", "/console/checklists", map[string]any{"category_id": cat["id"], "items": []map[string]any{
		{"kind": "document", "document_type": "national_id", "label": "National ID", "is_mandatory": true},
		{"kind": "referee", "label": "Referees", "is_mandatory": true, "min_count": 2},
	}})
	if tpl["version"] != 1.0 || tpl["is_current"] != false || len(tpl["items"].([]any)) != 2 || tpl["state"] != "awaiting_approval" || tpl["created_by_name"] == nil {
		t.Fatalf("checklist: %v", tpl)
	}
	for name, items := range map[string][]map[string]any{
		"document without its type": {{"kind": "document", "label": "no type", "is_mandatory": true}},
		"empty name":                {{"kind": "interview", "label": " ", "is_mandatory": true}},
		"repeated name":             {{"kind": "interview", "label": "Interview", "is_mandatory": true}, {"kind": "practical_assessment", "label": "interview", "is_mandatory": true}},
		"expiry out of range":       {{"kind": "clearance", "label": "Good Conduct", "is_mandatory": true, "validity_months": 0}},
		"expiry on an interview":    {{"kind": "interview", "label": "Interview", "is_mandatory": true, "validity_months": 12}},
		"too many referees":         {{"kind": "referee", "label": "Referees", "is_mandatory": true, "min_count": 11}},
		"no items":                  {},
	} {
		t.Run("checklist refused: "+name, func(t *testing.T) {
			h.expect(422, "admin", "POST", "/console/checklists", map[string]any{"category_id": cat["id"], "items": items})
		})
	}
	h.expect(403, "finance", "POST", "/console/checklists/"+tpl["id"].(string)+"/publish", nil)
	pub := h.expect(200, "approver", "POST", "/console/checklists/"+tpl["id"].(string)+"/publish", nil)
	if pub["is_current"] != true {
		t.Fatalf("publish: %v", pub)
	}
	if pub["state"] != "current" || pub["approved_by_name"] == nil || pub["approved_at"] == nil {
		t.Fatalf("publish: %v", pub)
	}
	h.expect(409, "approver", "POST", "/console/checklists/"+tpl["id"].(string)+"/publish", nil)
	// A draft identical to the current version is refused.
	h.expect(422, "admin", "POST", "/console/checklists", map[string]any{"category_id": cat["id"], "items": []map[string]any{
		{"kind": "document", "document_type": "national_id", "label": "National ID", "is_mandatory": true},
		{"kind": "referee", "label": "Referees", "is_mandatory": true, "min_count": 2},
	}})
	v2 := h.expect(201, "admin", "POST", "/console/checklists", map[string]any{"category_id": cat["id"], "items": []map[string]any{{"kind": "referee", "label": "Referees", "is_mandatory": true}}})
	h.expect(200, "approver", "POST", "/console/checklists/"+v2["id"].(string)+"/publish", nil)
	lists := h.expectList(200, "approver", "GET", "/console/checklists?category_id="+cat["id"].(string))
	current := 0
	for _, l := range lists {
		if l.(map[string]any)["is_current"] == true {
			current++
		}
	}
	if len(lists) != 2 || current != 1 || lists[1].(map[string]any)["state"] != "replaced" {
		t.Fatalf("checklists: %v", lists)
	}

	// ---- plans, content, FAQs, templates
	plan := h.expect(201, "finance", "POST", "/console/subscription-plans", map[string]any{"code": "gold", "name": "Gold", "price_minor": 500000, "billing_period": "monthly", "benefits": map[string]any{"discount_pct": 10}, "is_active": true})
	h.expect(422, "finance", "POST", "/console/subscription-plans", map[string]any{"code": "bad", "name": "Bad", "price_minor": 1, "billing_period": "weekly"})
	h.expect(200, "finance", "PATCH", "/console/subscription-plans/"+plan["id"].(string), map[string]any{"price_minor": 450000})
	h.expect(204, "finance", "DELETE", "/console/subscription-plans/"+plan["id"].(string), nil)
	h.expect(204, "finance", "DELETE", "/console/subscription-plans/"+plan["id"].(string), nil) // already inactive: no change
	h.expect(404, "finance", "DELETE", "/console/subscription-plans/00000000-0000-0000-0000-000000000000", nil)

	page := h.expect(201, "admin", "POST", "/console/content-pages", map[string]any{"slug": "our-story", "title": "Our story", "body_markdown": "# Hello", "language": "en"})
	h.expect(404, "", "GET", "/public/pages/our-story", nil) // unpublished
	pg := h.expect(200, "admin", "PATCH", "/console/content-pages/"+page["id"].(string), map[string]any{"is_published": true})
	if pg["published_at"] == nil {
		t.Fatalf("published_at not set: %v", pg)
	}
	h.expect(200, "", "GET", "/public/pages/our-story", nil)
	if sw := h.expect(200, "", "GET", "/public/pages/our-story?lang=sw", nil); sw["language"] != "en" {
		t.Fatalf("expected the English fallback: %v", sw)
	}
	h.expect(404, "", "GET", "/public/pages/missing", nil)

	faq := h.expect(201, "admin", "POST", "/console/faqs", map[string]any{"question": "Is it safe?", "answer": "Every provider is vetted."})
	h.expect(200, "admin", "PATCH", "/console/faqs/"+faq["id"].(string), map[string]any{"sort_order": 2})
	h.expect(204, "admin", "DELETE", "/console/faqs/"+faq["id"].(string), nil)

	tmpl := h.expect(201, "admin", "POST", "/console/notification-templates", map[string]any{"event_key": "request.confirmed", "channel": "sms", "language": "en", "body": "Booking {{.Ref}} confirmed"})
	h.expect(409, "admin", "POST", "/console/notification-templates", map[string]any{"event_key": "request.confirmed", "channel": "sms", "language": "en", "body": "dup"})
	h.expect(200, "admin", "PATCH", "/console/notification-templates/"+tmpl["id"].(string), map[string]any{"is_active": false})
	h.expect(204, "admin", "DELETE", "/console/notification-templates/"+tmpl["id"].(string), nil)

	// "Deactivate" switches a category off and keeps it, even while services and history refer to it
	h.expect(204, "admin", "DELETE", "/console/categories/"+cat["id"].(string), nil)
	for _, c := range h.expectList(200, "admin", "GET", "/console/categories") {
		if c := c.(map[string]any); c["id"] == cat["id"] && c["is_active"] != false {
			t.Fatalf("deactivated category: %v", c)
		}
	}
	h.expect(200, "admin", "PATCH", "/console/categories/"+cat["id"].(string), map[string]any{"is_active": true})

	// ---- staff
	created := h.expect(201, "admin", "POST", "/console/staff", map[string]any{"full_name": "Grace Mwangi", "email": "grace@homend.test", "phone": "+254711000111", "roles": []string{"triage_officer"}})
	creds := created["initial_credentials"].(map[string]any)
	if creds["password"] == "" || creds["totp_secret"] == "" {
		t.Fatalf("no credentials: %v", created)
	}
	h.expect(409, "admin", "POST", "/console/staff", map[string]any{"full_name": "Grace Again", "email": "grace@homend.test", "roles": []string{"triage_officer"}})
	h.expect(422, "admin", "POST", "/console/staff", map[string]any{"full_name": "No Roles", "email": "x@homend.test", "roles": []string{}})
	h.expect(422, "admin", "POST", "/console/staff", map[string]any{"full_name": "Bad Role", "email": "y@homend.test", "roles": []string{"wizard"}})
	// the new account signs in with the credentials it was given
	login := h.expect(200, "", "POST", "/auth/staff/login", map[string]any{"email": "grace@homend.test", "password": creds["password"]})
	h.tokens["grace"] = login["token"].(string)
	code, _ := totp.GenerateCode(creds["totp_secret"].(string), time.Now())
	h.expect(200, "grace", "POST", "/auth/staff/mfa", map[string]any{"code": code})
	h.expect(200, "grace", "GET", "/console/requests?limit=1", nil)

	gid := created["id"].(string)
	if got := h.expectList(200, "admin", "GET", "/console/staff?role=triage_officer"); len(got) != 1 {
		t.Fatalf("staff by role: %v", got)
	}
	h.expect(200, "admin", "PATCH", "/console/staff/"+gid, map[string]any{"full_name": "Grace W. Mwangi", "phone": "+254711000222"})
	roles := h.expect(200, "admin", "PUT", "/console/staff/"+gid+"/roles", map[string]any{"roles": []string{"triage_officer", "supervisor"}})
	if len(roles["roles"].([]any)) != 2 {
		t.Fatalf("roles: %v", roles)
	}
	h.expect(422, "admin", "PUT", "/console/staff/"+gid+"/roles", map[string]any{"roles": []string{}})
	// roles changed, so her session was revoked
	h.expect(401, "grace", "GET", "/console/requests", nil)
	// the last admin cannot be demoted or suspended
	me := h.expectList(200, "admin", "GET", "/console/staff?role=admin")[0].(map[string]any)["id"].(string)
	h.expect(409, "admin", "PUT", "/console/staff/"+me+"/roles", map[string]any{"roles": []string{"reporting"}})
	h.expect(409, "admin", "PATCH", "/console/staff/"+me, map[string]any{"status": "suspended"})
	reset := h.expect(200, "admin", "POST", "/console/staff/"+gid+"/reset-mfa", nil)
	if reset["totp_secret"] == "" || reset["totp_secret"] == creds["totp_secret"] {
		t.Fatalf("reset: %v", reset)
	}
	h.expect(200, "admin", "PATCH", "/console/staff/"+gid, map[string]any{"status": "suspended"})
	h.expect(403, "", "POST", "/auth/staff/login", map[string]any{"email": "grace@homend.test", "password": creds["password"]})
	h.expect(404, "admin", "POST", "/console/staff/00000000-0000-0000-0000-000000000000/reset-mfa", nil)

	// ---- settings and audit
	set := h.expect(200, "admin", "PUT", "/console/settings/offer.expiry_minutes", map[string]any{"value": 90})
	if set["value"] != 90.0 || set["updated_by"] == nil {
		t.Fatalf("setting: %v", set)
	}
	h.expect(403, "reporting", "PUT", "/console/settings/offer.expiry_minutes", map[string]any{"value": 10})
	found := false
	for _, x := range h.expectList(200, "reporting", "GET", "/console/settings") {
		if x.(map[string]any)["key"] == "offer.expiry_minutes" && x.(map[string]any)["value"] == 90.0 {
			found = true
		}
	}
	if !found {
		t.Fatal("setting not listed")
	}
	log := h.expect(200, "admin", "GET", "/console/audit-log?action=setting.update", nil)
	if len(log["data"].([]any)) != 1 {
		t.Fatalf("audit: %v", log)
	}
	all := h.expect(200, "admin", "GET", "/console/audit-log?limit=3", nil)
	if all["meta"].(map[string]any)["next_cursor"] == nil {
		t.Fatal("audit log should paginate")
	}
	next := h.expect(200, "admin", "GET", "/console/audit-log?limit=3&cursor="+all["meta"].(map[string]any)["next_cursor"].(string), nil)
	if next["data"].([]any)[0].(map[string]any)["id"] == all["data"].([]any)[0].(map[string]any)["id"] {
		t.Fatal("cursor did not advance")
	}
	h.expect(403, "finance", "GET", "/console/audit-log", nil)

	// ---- leads
	h.expect(201, "", "POST", "/public/leads", map[string]any{"full_name": "Amina Odhiambo", "phone": "+254700111222", "message": "Interested in a nanny"})
	leads := h.expect(200, "admin", "GET", "/console/leads", nil)["data"].([]any)
	if len(leads) != 1 || leads[0].(map[string]any)["phone"] != "+254700111222" {
		t.Fatalf("leads: %v", leads)
	}

	// ---- reports
	for _, rep := range []string{"supply", "demand", "matching", "quality", "commercial", "compliance"} {
		out := h.expect(200, "reporting", "GET", "/console/reports/"+rep+"?from=2026-01-01&to=2026-12-31&group_by=month", nil)
		if out["report"] != rep || out["metrics"] == nil {
			t.Fatalf("report %s: %v", rep, out)
		}
		code, raw := h.call("reporting", "GET", "/console/reports/"+rep+"/export?from=2026-01-01&to=2026-12-31", nil)
		if code != 200 || !strings.HasPrefix(string(raw), "report,from,to,metric,value") {
			t.Fatalf("export %s: %d %s", rep, code, raw)
		}
	}
	h.expect(403, "supervisor", "GET", "/console/reports/supply", nil)
	h.expect(200, "reporting", "GET", "/console/reports/demand?category_id="+cat["id"].(string)+"&service_area_id="+area["id"].(string), nil)
	h.expect(422, "reporting", "GET", "/console/reports/demand?from=2026-12-31&to=2026-01-01", nil)
}
