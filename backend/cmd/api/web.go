package main

import (
	"github.com/go-chi/chi/v5"

	"github.com/KING-CYBERTON/home-residency/backend/internal/api"
	"github.com/KING-CYBERTON/home-residency/backend/internal/config"
	"github.com/KING-CYBERTON/home-residency/backend/internal/storage"
	"github.com/KING-CYBERTON/home-residency/backend/internal/web"
	"github.com/KING-CYBERTON/home-residency/backend/internal/web/client"
	"github.com/KING-CYBERTON/home-residency/backend/internal/web/provider"
	"github.com/KING-CYBERTON/home-residency/backend/internal/web/public"
)

// mountWeb attaches the server-rendered surfaces (ADR-0001) to srv's router. Each surface lives in
// its own package under internal/web and owns its URL prefix; this is the one place they are wired.
func mountWeb(srv *api.Server, cfg config.Config) error {
	opts := web.Options{BaseURL: cfg.PublicBaseURL}
	if s3, ok := srv.Store.(*storage.S3); ok {
		opts.StorageOrigin = s3.Origin()
	}
	site, err := web.New(opts)
	if err != nil {
		return err
	}
	pub, err := public.New(srv, site)
	if err != nil {
		return err
	}
	clientPortal, err := client.New(client.Deps{Site: site, API: srv, Auth: srv.Auth, CookieSecure: cfg.CookieSecure, PushKey: cfg.VAPIDPublicKey})
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
			pub.Routes(r)                                            // /, /services/..., /areas/..., /pricing, /contact, /sitemap.xml
			r.Mount(web.ClientPortalPath, clientPortal.Routes())     // internal/web/client
			r.Mount(web.ProviderPortalPath, providerPortal.Routes()) // internal/web/provider
			r.Mount(web.JoinPath, providerPortal.JoinRoutes())       // recruitment, internal/web/provider
		})
	}
	return nil
}
