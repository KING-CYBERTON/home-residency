// Package api implements the generated ServerInterface. Handlers translate HTTP to the domain
// rules in internal/domain and SQL; operations that are specified in openapi.yaml but not built
// yet fall through to gen.Unimplemented and answer 501 with the standard error envelope.
package api

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	apispec "github.com/KING-CYBERTON/home-residency/backend/api"
	"github.com/KING-CYBERTON/home-residency/backend/internal/api/gen"
	"github.com/KING-CYBERTON/home-residency/backend/internal/auth"
	"github.com/KING-CYBERTON/home-residency/backend/internal/config"
	"github.com/KING-CYBERTON/home-residency/backend/internal/crypt"
	"github.com/KING-CYBERTON/home-residency/backend/internal/httpx"
	"github.com/KING-CYBERTON/home-residency/backend/internal/mpesa"
	"github.com/KING-CYBERTON/home-residency/backend/internal/sms"
	"github.com/KING-CYBERTON/home-residency/backend/internal/storage"
)

const BaseURL = "/api/v1"

type Server struct {
	gen.Unimplemented

	DB     *pgxpool.Pool
	Auth   *auth.Service
	SMS    sms.Sender
	Cipher crypt.Cipher
	Mpesa  mpesa.Client
	Store  storage.Store
	Cfg    config.Config
	Now    func() time.Time

	// Web mounts the server-rendered HTML surfaces (internal/web) on the root router. Set by
	// cmd/api; nil in the API tests.
	Web func(chi.Router)
}

func NewServer(db *pgxpool.Pool, cfg config.Config, sender sms.Sender, cipher crypt.Cipher, pay mpesa.Client, store storage.Store) *Server {
	return &Server{
		DB: db, Cfg: cfg, SMS: sender, Cipher: cipher, Mpesa: pay, Store: store, Now: time.Now,
		Auth: auth.New(db, crypt.SHA256("pepper:"+cfg.FieldKey)),
	}
}

// Handler builds the full HTTP handler: middleware, the generated routes under /api/v1,
// the spec itself at /openapi.yaml and the health probe at /healthz.
func (s *Server) Handler() (http.Handler, error) {
	policy, err := NewPolicy(apispec.YAML, BaseURL)
	if err != nil {
		return nil, err
	}
	r := chi.NewRouter()
	r.Use(httpx.RequestIDMiddleware, httpx.Recover, httpx.Logging, httpx.SecurityHeaders, s.Auth.Authenticate)
	r.Get("/openapi.yaml", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		_, _ = w.Write(apispec.YAML)
	})
	if l, ok := s.Store.(*storage.Local); ok {
		r.Handle("/storage/*", l.Handler()) // development only: S3 serves these in production
	}
	if s.Web != nil {
		s.Web(r)
	}
	gen.HandlerWithOptions(s, gen.ChiServerOptions{
		BaseURL:     BaseURL,
		BaseRouter:  r,
		Middlewares: []gen.MiddlewareFunc{policy.Middleware},
		ErrorHandlerFunc: func(w http.ResponseWriter, req *http.Request, err error) {
			httpx.Fail(w, req, httpx.BadRequest(err.Error()))
		},
	})
	return r, nil
}

func (s *Server) GetHealthz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	ok := s.DB.Ping(ctx) == nil
	status, code := "ok", http.StatusOK
	if !ok {
		status, code = "degraded", http.StatusServiceUnavailable
	}
	httpx.JSON(w, code, map[string]any{"status": status, "db": ok, "version": Version})
}

var Version = "dev"
