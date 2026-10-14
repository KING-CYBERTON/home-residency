package api

import (
	"strings"
	"testing"

	apispec "github.com/KING-CYBERTON/home-residency/backend/api"
)

// Every operation in openapi.yaml must have an access rule, and every staff-only operation must name
// the roles that may call it. A new endpoint added without x-roles would otherwise be unreachable (or
// worse, reachable by the wrong people), so this fails the build.
func TestEveryConsoleOperationNamesItsRoles(t *testing.T) {
	p, err := NewPolicy(apispec.YAML, BaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.rules) < 200 {
		t.Fatalf("only %d operations loaded", len(p.rules))
	}
	for key, r := range p.rules {
		path := strings.SplitN(key, " ", 2)[1]
		if !strings.HasPrefix(path, "/console/") {
			continue
		}
		if !r.anyStaff && !r.anyStaffRead && len(r.roles) == 0 {
			t.Errorf("%s has no x-roles", key)
		}
	}
}

func TestPublicAndWebhookRules(t *testing.T) {
	p, _ := NewPolicy(apispec.YAML, BaseURL)
	for _, k := range []string{"GET /healthz", "POST /auth/otp/request", "GET /public/categories", "POST /public/leads"} {
		if !p.rules[k].public {
			t.Errorf("%s should be public", k)
		}
	}
	for _, k := range []string{"POST /webhooks/mpesa/stk", "POST /webhooks/ussd"} {
		if !p.rules[k].webhook {
			t.Errorf("%s should be a webhook", k)
		}
	}
	if p.rules["GET /me"].public || p.rules["POST /client/requests"].public {
		t.Error("authenticated routes marked public")
	}
}
