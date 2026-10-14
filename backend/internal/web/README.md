# internal/web: shared foundation for the HTML surfaces

The public site, client portal and provider portal are Go `html/template` + HTMX + Tailwind (ADR-0001).
This package owns what they share; each surface lives in its own package (`internal/web/public`,
`internal/web/client`, `internal/web/provider`) and is wired in `cmd/api/web.go`.

| Surface | Prefix | Package |
|---|---|---|
| Public site | `/`, `/services/…`, `/areas/…`, `/pricing`, `/sitemap.xml` | `web/public` |
| Client portal | `web.ClientPortalPath` (`/account`) | `web/client` |
| Provider portal | `web.ProviderPortalPath` (`/provider`) | `web/provider` |
| Recruitment | `web.JoinPath` (`/join`) | `web/provider` |

## Layout contract

```go
site, _ := web.New(web.Options{BaseURL: cfg.PublicBaseURL})
t, _ := site.Templates(myTemplatesFS, "templates/*.html")    // clone of the base layout + your files
t, _ = site.TemplatesWithFuncs(template.FuncMap{...}, myTemplatesFS, "templates/*.html") // plus your own funcs
site.Render(w, r, t, http.StatusOK, &web.Page{Title: "Jobs", NoIndex: true, Data: rows})
site.RenderBlock(w, r, t, "job-row", http.StatusOK, row)      // HTMX fragment
```

Blocks a page may define: `title`, `head` (manifest link, theme-color), `header` (defaults to the public
nav), `content`, `footer` (defaults to the public footer), `scripts`.

Template functions: `asset "css/app.css"` (cache-busted URL; also `site.Asset` in Go), `absURL`, `money`
(integer cents to `KES 2,500`), `jsonld`, `deref`, `siteName`, `year`, `clientPath`, `providerPath`, `joinPath`.

The layout already loads `css/app.css`, `js/htmx.min.js`, `img/favicon.svg`; icons for manifests are
`img/icon-192.png` and `img/icon-512.png`. No web fonts: the system font stack keeps pages light.

## Rules every route gets from `site.Middleware`

- A strict CSP: same-origin scripts and styles only, no inline script or `style=""`. HTMX runs with
  `allowEval: false` and no injected indicator styles (`.htmx-indicator` is in `app.css`).
- Cross-site form protection with Go's `http.CrossOriginProtection` (Sec-Fetch-Site / Origin), so
  forms need no CSRF token.
- `Cache-Control: no-store` by default. Pages that look the same for everyone call `web.Cacheable`.

## Styles

Edit `styles/app.css` (Tailwind v4, brand colours under `@theme`) or use classes in any template
under `internal/web/`, then run `make css` in `backend/` and commit `static/css/app.css`. CI fails
if the committed CSS is stale.
