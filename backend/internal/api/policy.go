package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/go-chi/chi/v5"

	"github.com/KING-CYBERTON/home-residency/backend/internal/auth"
	"github.com/KING-CYBERTON/home-residency/backend/internal/httpx"
)

// Policy derives who may call each operation from the OpenAPI document itself, so the contract
// and the access rules cannot drift: `security: []` means public, a webhookSignature requirement
// means webhook, and `x-roles` lists the staff roles allowed on /console routes.
type Policy struct {
	rules   map[string]rule // key: "GET /client/requests/{id}"
	baseURL string
}

type rule struct {
	public, webhook bool
	roles           []string
	anyStaff        bool // x-roles contains "any staff"
	anyStaffRead    bool // x-roles contains "any staff (read)": any staff may GET, roles needed to write
}

func NewPolicy(spec []byte, baseURL string) (*Policy, error) {
	doc, err := openapi3.NewLoader().LoadFromData(spec)
	if err != nil {
		return nil, fmt.Errorf("load openapi: %w", err)
	}
	p := &Policy{rules: map[string]rule{}, baseURL: baseURL}
	for path, item := range doc.Paths.Map() {
		for method, op := range item.Operations() {
			var r rule
			if op.Security != nil {
				if len(*op.Security) == 0 {
					r.public = true
				}
				for _, req := range *op.Security {
					if _, ok := req["webhookSignature"]; ok {
						r.webhook = true
					}
				}
			}
			if raw, ok := op.Extensions["x-roles"]; ok {
				b, _ := json.Marshal(raw)
				var roles []string
				if err := json.Unmarshal(b, &roles); err != nil {
					return nil, fmt.Errorf("%s %s: bad x-roles: %w", method, path, err)
				}
				for _, role := range roles {
					switch role {
					case "any staff":
						r.anyStaff = true
					case "any staff (read)":
						r.anyStaffRead = true
					default:
						r.roles = append(r.roles, role)
					}
				}
			}
			p.rules[method+" "+path] = r
		}
	}
	return p, nil
}

// Middleware is applied per operation by the generated router, after chi has matched the route.
func (p *Policy) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pattern := strings.TrimPrefix(chi.RouteContext(r.Context()).RoutePattern(), p.baseURL)
		rl, ok := p.rules[r.Method+" "+pattern]
		if !ok {
			httpx.Fail(w, r, httpx.ErrNotFound)
			return
		}
		w = wrapNotImplemented(w)
		if rl.public || rl.webhook {
			next.ServeHTTP(w, r)
			return
		}
		pr := auth.FromContext(r.Context())
		if pr == nil {
			httpx.Fail(w, r, httpx.ErrUnauthenticated)
			return
		}
		if err := p.authorise(pr, rl, r.Method, pattern); err != nil {
			httpx.Fail(w, r, err)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (p *Policy) authorise(pr *auth.Principal, rl rule, method, pattern string) error {
	if pr.Kind == auth.KindStaff && !pr.MFAPassed && pattern != "/auth/staff/mfa" && pattern != "/auth/logout" {
		return httpx.Forbidden("mfa_required", "Complete the authenticator check first")
	}
	switch {
	case strings.HasPrefix(pattern, "/client/"):
		if pr.Kind != auth.KindClient {
			return httpx.ErrForbidden
		}
	case strings.HasPrefix(pattern, "/provider/"):
		if pr.Kind != auth.KindProvider {
			return httpx.ErrForbidden
		}
	case strings.HasPrefix(pattern, "/console/"):
		if pr.Kind != auth.KindStaff {
			return httpx.ErrForbidden
		}
		// Only the roles listed in x-roles may call the operation. Admin is deliberately not implied,
		// so segregation-of-duties actions (approve, authorise, decide) stay with their named roles.
		if rl.anyStaff || (rl.anyStaffRead && method == http.MethodGet) || pr.HasAnyRole(rl.roles...) {
			return nil
		}
		return httpx.ErrForbidden
	}
	return nil
}

// wrapNotImplemented turns the generated stub's bare 501 into the standard error envelope.
func wrapNotImplemented(w http.ResponseWriter) http.ResponseWriter {
	return &niWriter{ResponseWriter: w}
}

type niWriter struct {
	http.ResponseWriter
	swallow bool
}

func (n *niWriter) WriteHeader(code int) {
	if code == http.StatusNotImplemented && n.Header().Get("Content-Type") == "" {
		n.swallow = true
		httpx.JSON(n.ResponseWriter, code, httpx.ErrNotImplemented)
		return
	}
	n.ResponseWriter.WriteHeader(code)
}

func (n *niWriter) Write(b []byte) (int, error) {
	if n.swallow {
		return len(b), nil
	}
	return n.ResponseWriter.Write(b)
}
