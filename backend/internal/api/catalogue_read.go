package api

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/KING-CYBERTON/home-residency/backend/internal/httpx"
)

// Read-side catalogue for the server-rendered public site (internal/web/public). These are the same
// queries and rules the JSON /public endpoints use, exposed as Go calls so the HTML handlers do not
// go through HTTP.

type (
	ServiceArea  = serviceAreaDTO
	Category     = categoryDTO
	Service      = serviceDTO
	RateCardRate = rateCardEntryDTO
	FAQ          = faqDTO
	Plan         = planDTO
)

// PublicAreas lists the active service areas, by name.
func (s *Server) PublicAreas(ctx context.Context) ([]ServiceArea, error) {
	rows, err := s.DB.Query(ctx, `
		SELECT id, slug, name, county, is_active, centre_lat::float8, centre_lng::float8, radius_km::float8
		FROM service_areas WHERE is_active ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ServiceArea{}
	for rows.Next() {
		var a ServiceArea
		if err := rows.Scan(&a.ID, &a.Slug, &a.Name, &a.County, &a.IsActive, &a.CentreLat, &a.CentreLng, &a.RadiusKm); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// PublicCategories lists the active categories in display order.
func (s *Server) PublicCategories(ctx context.Context) ([]Category, error) {
	rows, err := s.DB.Query(ctx, `SELECT `+categoryCols+` FROM service_categories c WHERE c.is_active ORDER BY c.sort_order, c.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Category{}
	for rows.Next() {
		c, err := scanCategory(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// PublicServices lists active services with the price in force now, for one category (or all when
// categoryID is nil). An area-specific rate wins over the all-areas rate when areaID is given.
func (s *Server) PublicServices(ctx context.Context, categoryID, areaID *uuid.UUID) ([]Service, error) {
	return s.servicesWithPrices(ctx, categoryID, areaID)
}

// PublicFAQs lists published FAQs for a category plus the general ones (categoryID nil: all).
func (s *Server) PublicFAQs(ctx context.Context, categoryID *uuid.UUID, lang string) ([]FAQ, error) {
	return s.faqs(ctx, categoryID, lang)
}

// PublicPlans lists active subscription plans, cheapest first.
func (s *Server) PublicPlans(ctx context.Context) ([]Plan, error) {
	rows, err := s.DB.Query(ctx, `
		SELECT id, code, name, description, price_minor, billing_period, benefits, is_active
		FROM subscription_plans WHERE is_active ORDER BY price_minor`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Plan{}
	for rows.Next() {
		var p Plan
		var benefits []byte
		if err := rows.Scan(&p.ID, &p.Code, &p.Name, &p.Description, &p.PriceMinor, &p.BillingPeriod, &benefits, &p.IsActive); err != nil {
			return nil, err
		}
		p.Benefits = decodeBenefits(benefits)
		out = append(out, p)
	}
	return out, rows.Err()
}

// Lead is an enquiry from the public site before the household registers.
type Lead struct {
	FullName      string
	Phone         *string // E.164
	Email         *string
	ServiceAreaID *uuid.UUID
	CategoryID    *uuid.UUID
	Message       *string
	Source        *string
	// The visitor chose "Not sure yet" or "Other / not listed" rather than leaving the field out.
	CategoryNotSure, AreaNotListed bool
	AreaName                       *string
}

// CreateLead validates and stores a lead. Validation failures are *httpx.Error with a message fit
// to show the visitor.
func (s *Server) CreateLead(ctx context.Context, l Lead) (uuid.UUID, any, error) {
	l.FullName = strings.TrimSpace(l.FullName)
	if len(l.FullName) < 2 {
		return uuid.Nil, nil, httpx.Invalid("full_name is required")
	}
	if l.Phone != nil && !phoneRE.MatchString(*l.Phone) {
		return uuid.Nil, nil, httpx.Invalid("Phone must be in international format")
	}
	if l.Phone == nil && l.Email == nil {
		return uuid.Nil, nil, httpx.Invalid("Give a phone number or an email so we can reply")
	}
	if l.CategoryID != nil {
		l.CategoryNotSure = false
	}
	if l.ServiceAreaID != nil {
		l.AreaNotListed, l.AreaName = false, nil
	}
	if l.AreaName != nil {
		if n := strings.TrimSpace(*l.AreaName); n == "" {
			l.AreaName = nil
		} else if len(n) > 120 {
			return uuid.Nil, nil, httpx.Invalid("area_name is too long")
		} else {
			l.AreaName, l.AreaNotListed = &n, true
		}
	}
	var id uuid.UUID
	var created any
	err := s.DB.QueryRow(ctx, `
		INSERT INTO leads (full_name, phone_e164, email, service_area_id, category_id, message, source, category_not_sure, area_not_listed, area_name)
		VALUES ($1,$2,$3::citext,$4,$5,$6,$7,$8,$9,$10) RETURNING id, created_at`,
		l.FullName, l.Phone, l.Email, l.ServiceAreaID, l.CategoryID, l.Message, l.Source, l.CategoryNotSure, l.AreaNotListed, l.AreaName).Scan(&id, &created)
	return id, created, mapErr(err)
}

// PublicVATPct is the VAT rate included in rate-card prices.
func (s *Server) PublicVATPct(ctx context.Context) float64 {
	return s.setting(ctx, s.DB, "vat.rate_pct", 16)
}

// PublicSlugRedirect is the current slug for a renamed area or category ("area" or "category"),
// or "" when the slug was never renamed.
func (s *Server) PublicSlugRedirect(ctx context.Context, kind, slug string) (string, error) {
	var to string
	err := s.DB.QueryRow(ctx, `SELECT new_slug FROM slug_redirects WHERE kind = $1 AND old_slug = $2`, kind, slug).Scan(&to)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return to, err
}

// PublicPricedAreas maps each category to the active areas where at least one of its active services
// has a current price, either for that area or for all areas. Area pages exist only for these.
func (s *Server) PublicPricedAreas(ctx context.Context) (map[uuid.UUID]map[uuid.UUID]bool, error) {
	rows, err := s.DB.Query(ctx, `
		SELECT DISTINCT sv.category_id, a.id
		FROM services sv
		JOIN rate_card_entries r ON r.service_id = sv.id AND r.valid_from <= now() AND (r.valid_to IS NULL OR r.valid_to > now())
		JOIN service_areas a ON a.is_active AND (r.service_area_id = a.id OR r.service_area_id IS NULL)
		WHERE sv.is_active`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uuid.UUID]map[uuid.UUID]bool{}
	for rows.Next() {
		var cat, area uuid.UUID
		if err := rows.Scan(&cat, &area); err != nil {
			return nil, err
		}
		if out[cat] == nil {
			out[cat] = map[uuid.UUID]bool{}
		}
		out[cat][area] = true
	}
	return out, rows.Err()
}
