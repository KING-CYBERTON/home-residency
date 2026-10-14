package main

import (
	"github.com/go-chi/chi/v5"

	"github.com/KING-CYBERTON/home-residency/backend/internal/api"
	"github.com/KING-CYBERTON/home-residency/backend/internal/config"
	"github.com/KING-CYBERTON/home-residency/backend/internal/web"
)

// mountWeb attaches the server-rendered surfaces (ADR-0001) to srv's router. Each surface lives in
// its own package under internal/web and owns its URL prefix; this is the one place they are wired.
func mountWeb(srv *api.Server, cfg config.Config) error {
	site, err := web.New(web.Options{BaseURL: cfg.PublicBaseURL})
	if err != nil {
		return err
	}
	srv.Web = func(r chi.Router) {
		r.NotFound(site.NotFound)
		r.Handle("/static/*", site.StaticHandler())
		r.Group(func(r chi.Router) {
			r.Use(site.Middleware)
			// Public site: /, /services/..., /areas/..., /pricing, /sitemap.xml (internal/web/public)
			// Client portal:   r.Mount(web.ClientPortalPath, ...)   (internal/web/client)
			// Provider portal: r.Mount(web.ProviderPortalPath, ...) (internal/web/provider)
			// Recruitment:     r.Mount(web.JoinPath, ...)           (internal/web/provider)
		})
	}
	return nil
}
