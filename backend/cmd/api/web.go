package main

import (
	"github.com/go-chi/chi/v5"

	"github.com/KING-CYBERTON/home-residency/backend/internal/api"
	"github.com/KING-CYBERTON/home-residency/backend/internal/config"
	"github.com/KING-CYBERTON/home-residency/backend/internal/web"
	"github.com/KING-CYBERTON/home-residency/backend/internal/web/provider"
	"github.com/KING-CYBERTON/home-residency/backend/internal/web/public"
)

// mountWeb attaches the server-rendered surfaces (ADR-0001) to srv's router. Each surface lives in
// its own package under internal/web and owns its URL prefix; this is the one place they are wired.
func mountWeb(srv *api.Server, cfg config.Config) error {
	site, err := web.New(web.Options{BaseURL: cfg.PublicBaseURL})
	if err != nil {
		return err
	}
	pub, err := public.New(srv, site)
	if err != nil {
		return err
	}
	providerPortal, err := provider.New(srv, site, provider.Options{CookieSecure: cfg.CookieSecure})
	if err != nil {
		return err
	}
	srv.Web = func(r chi.Router) {
		r.NotFound(site.NotFound)
		r.Handle("/static/*", site.StaticHandler())
		r.Group(func(r chi.Router) {
			r.Use(site.Middleware)
			pub.Routes(r) // /, /services/..., /areas/..., /pricing, /contact, /sitemap.xml
			// Client portal:   r.Mount(web.ClientPortalPath, ...)   (internal/web/client)
			r.Mount(web.ProviderPortalPath, providerPortal.Routes()) // internal/web/provider
			r.Mount(web.JoinPath, providerPortal.JoinRoutes())       // recruitment, internal/web/provider
		})
	}
	return nil
}
