package api_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/google/uuid"

	apispec "github.com/KING-CYBERTON/home-residency/backend/api"
)

// TestNoOperationIsUnimplemented calls every operation in the contract as the role that owns it
// and fails if any still answers 501.
func TestNoOperationIsUnimplemented(t *testing.T) {
	h := newHarness(t)
	h.newWorld()
	h.staff("all", "all@example.com", "admin", "triage_officer", "supervisor", "vetting_officer", "approver",
		"training_coordinator", "compliance", "complaints_manager", "reporting", "finance")
	doc, err := openapi3.NewLoader().LoadFromData(apispec.YAML)
	if err != nil {
		t.Fatal(err)
	}
	param := regexp.MustCompile(`\{[^}]+\}`)
	n := 0
	for path, item := range doc.Paths.Map() {
		who := ""
		switch {
		case strings.HasPrefix(path, "/console"):
			who = "all"
		case strings.HasPrefix(path, "/client"):
			who = "client"
		case strings.HasPrefix(path, "/provider"):
			who = "provider"
		}
		concrete := param.ReplaceAllStringFunc(path, func(m string) string {
			if m == "{slug}" {
				return "terms"
			}
			return uuid.NewString()
		})
		for method, op := range item.Operations() {
			if op.OperationID == "postAuthLogout" {
				continue // would end the session the next calls rely on
			}
			var body any
			if op.RequestBody != nil {
				body = map[string]any{}
			}
			code, raw := h.call(who, method, concrete, body)
			n++
			if code == 501 || code >= 500 {
				t.Errorf("%s %s answers %d: %s", method, path, code, raw)
			}
		}
	}
	t.Logf("%d operations exercised", n)
}
