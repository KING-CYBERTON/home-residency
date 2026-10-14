package auth

import (
	"net/http"
	"strings"

	"github.com/KING-CYBERTON/home-residency/backend/internal/httpx"
)

const CookieName = "hr_session"

// TokenFromRequest reads the Bearer token, falling back to the session cookie used by the HTML portals.
func TokenFromRequest(r *http.Request) string {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
	}
	if c, err := r.Cookie(CookieName); err == nil {
		return c.Value
	}
	return ""
}

// Authenticate attaches the principal when a valid token is present. Whether a route needs one
// is decided by the policy middleware, so public routes still work when a stale token is sent.
func (s *Service) Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if tok := TokenFromRequest(r); tok != "" {
			if p, err := s.Lookup(r.Context(), tok); err == nil {
				r = r.WithContext(WithPrincipal(r.Context(), p))
			} else if he, ok := err.(*httpx.Error); !ok || he != httpx.ErrUnauthenticated {
				httpx.Fail(w, r, err)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
