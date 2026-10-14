package api

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/KING-CYBERTON/home-residency/backend/internal/api/gen"
	"github.com/KING-CYBERTON/home-residency/backend/internal/httpx"
)

const serviceCols = `s.id, s.category_id, s.slug, s.name, s.description, s.scope_items, s.pricing_unit::text,
	s.min_quantity::float8, s.max_quantity::float8, s.default_duration_minutes, s.is_active`

const categoryCols = `c.id, c.slug, c.name, c.name_sw, c.description, c.engagement::text, c.vetting_tier::text,
	c.requires_client_id_verification, c.sop_markdown, c.is_active, c.sort_order`

func scanService(row pgx.Row) (serviceDTO, error) {
	var d serviceDTO
	var scope []byte
	err := row.Scan(&d.ID, &d.CategoryID, &d.Slug, &d.Name, &d.Description, &scope, &d.PricingUnit,
		&d.MinQuantity, &d.MaxQuantity, &d.DefaultDurationMinutes, &d.IsActive)
	if err != nil {
		return d, err
	}
	d.ScopeItems = []string{}
	_ = json.Unmarshal(scope, &d.ScopeItems)
	return d, nil
}

func scanCategory(row pgx.Row) (categoryDTO, error) {
	var d categoryDTO
	err := row.Scan(&d.ID, &d.Slug, &d.Name, &d.NameSw, &d.Description, &d.Engagement, &d.VettingTier,
		&d.RequiresClientIDVerification, &d.SopMarkdown, &d.IsActive, &d.SortOrder)
	return d, err
}

// currentRate returns the price in force now: an area-specific entry wins over the all-areas one.
func currentRate(ctx context.Context, q db, serviceID uuid.UUID, areaID *uuid.UUID) (*rateCardEntryDTO, error) {
	var d rateCardEntryDTO
	err := q.QueryRow(ctx, `
		SELECT id, service_id, service_area_id, client_price_minor, provider_pay_minor, placement_fee_minor, currency, valid_from, valid_to
		FROM rate_card_entries
		WHERE service_id = $1 AND (service_area_id = $2 OR service_area_id IS NULL)
		  AND valid_from <= now() AND (valid_to IS NULL OR valid_to > now())
		ORDER BY (service_area_id IS NOT NULL) DESC LIMIT 1`, serviceID, areaID).
		Scan(&d.ID, &d.ServiceID, &d.ServiceAreaID, &d.ClientPriceMinor, &d.ProviderPayMinor, &d.PlacementFeeMinor, &d.Currency, &d.ValidFrom, &d.ValidTo)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return &d, err
}

func (s *Server) GetPublicServiceAreas(w http.ResponseWriter, r *http.Request) {
	rows, err := s.DB.Query(r.Context(), `
		SELECT id, slug, name, county, is_active, centre_lat::float8, centre_lng::float8, radius_km::float8
		FROM service_areas WHERE is_active ORDER BY name`)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	defer rows.Close()
	out := []serviceAreaDTO{}
	for rows.Next() {
		var a serviceAreaDTO
		if err := rows.Scan(&a.ID, &a.Slug, &a.Name, &a.County, &a.IsActive, &a.CentreLat, &a.CentreLng, &a.RadiusKm); err != nil {
			httpx.Fail(w, r, err)
			return
		}
		out = append(out, a)
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (s *Server) GetPublicCategories(w http.ResponseWriter, r *http.Request) {
	rows, err := s.DB.Query(r.Context(), `SELECT `+categoryCols+` FROM service_categories c WHERE c.is_active ORDER BY c.sort_order, c.name`)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	defer rows.Close()
	out := []categoryDTO{}
	for rows.Next() {
		c, err := scanCategory(rows)
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		out = append(out, c)
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (s *Server) GetPublicCategoriesById(w http.ResponseWriter, r *http.Request, slug string) {
	ctx := r.Context()
	cat, err := scanCategory(s.DB.QueryRow(ctx, `SELECT `+categoryCols+` FROM service_categories c WHERE c.slug = $1 AND c.is_active`, slug))
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	services, err := s.servicesWithPrices(ctx, &cat.ID, nil)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	faqs, err := s.faqs(ctx, &cat.ID, "en")
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"category": cat, "services": services, "faqs": faqs})
}

func (s *Server) servicesWithPrices(ctx context.Context, categoryID, areaID *uuid.UUID) ([]serviceDTO, error) {
	rows, err := s.DB.Query(ctx, `
		SELECT `+serviceCols+` FROM services s JOIN service_categories c ON c.id = s.category_id
		WHERE s.is_active AND c.is_active AND ($1::uuid IS NULL OR s.category_id = $1) ORDER BY c.sort_order, s.sort_order, s.name`, categoryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []serviceDTO{}
	for rows.Next() {
		d, err := scanService(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	rows.Close()
	for i := range out {
		if out[i].CurrentPrice, err = currentRate(ctx, s.DB, out[i].ID, areaID); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *Server) GetPublicRateCard(w http.ResponseWriter, r *http.Request, params gen.GetPublicRateCardParams) {
	var area, cat *uuid.UUID
	if params.ServiceAreaId != nil {
		a := uuid.UUID(*params.ServiceAreaId)
		area = &a
	}
	if params.CategoryId != nil {
		c := uuid.UUID(*params.CategoryId)
		cat = &c
	}
	out, err := s.servicesWithPrices(r.Context(), cat, area)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (s *Server) GetPublicSubscriptionPlans(w http.ResponseWriter, r *http.Request) {
	rows, err := s.DB.Query(r.Context(), `
		SELECT id, code, name, description, price_minor, billing_period, benefits, is_active
		FROM subscription_plans WHERE is_active ORDER BY price_minor`)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	defer rows.Close()
	out := []planDTO{}
	for rows.Next() {
		var p planDTO
		var benefits []byte
		if err := rows.Scan(&p.ID, &p.Code, &p.Name, &p.Description, &p.PriceMinor, &p.BillingPeriod, &benefits, &p.IsActive); err != nil {
			httpx.Fail(w, r, err)
			return
		}
		p.Benefits = map[string]any{}
		_ = json.Unmarshal(benefits, &p.Benefits)
		out = append(out, p)
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (s *Server) faqs(ctx context.Context, categoryID *uuid.UUID, lang string) ([]faqDTO, error) {
	rows, err := s.DB.Query(ctx, `
		SELECT id, category_id, language, question, answer, sort_order, is_published FROM faqs
		WHERE is_published AND language = $2 AND ($1::uuid IS NULL OR category_id = $1 OR category_id IS NULL)
		ORDER BY sort_order, question`, categoryID, lang)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []faqDTO{}
	for rows.Next() {
		var f faqDTO
		if err := rows.Scan(&f.ID, &f.CategoryID, &f.Language, &f.Question, &f.Answer, &f.SortOrder, &f.IsPublished); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, nil
}

func (s *Server) GetPublicFaqs(w http.ResponseWriter, r *http.Request, params gen.GetPublicFaqsParams) {
	var cat *uuid.UUID
	if params.CategoryId != nil {
		c := uuid.UUID(*params.CategoryId)
		cat = &c
	}
	lang := "en"
	if params.Lang != nil {
		lang = string(*params.Lang)
	}
	out, err := s.faqs(r.Context(), cat, lang)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (s *Server) PostPublicLeads(w http.ResponseWriter, r *http.Request) {
	body, err := httpx.Decode[gen.LeadInput](r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	name := strings.TrimSpace(body.FullName)
	if len(name) < 2 {
		httpx.Fail(w, r, httpx.Invalid("full_name is required"))
		return
	}
	if body.Phone != nil && !phoneRE.MatchString(*body.Phone) {
		httpx.Fail(w, r, httpx.Invalid("Phone must be in international format"))
		return
	}
	if body.Phone == nil && body.Email == nil {
		httpx.Fail(w, r, httpx.Invalid("Give a phone number or an email so we can reply"))
		return
	}
	var email *string
	if body.Email != nil {
		e := string(*body.Email)
		email = &e
	}
	var id uuid.UUID
	var created any
	err = s.DB.QueryRow(r.Context(), `
		INSERT INTO leads (full_name, phone_e164, email, service_area_id, category_id, message, source)
		VALUES ($1,$2,$3::citext,$4,$5,$6,$7) RETURNING id, created_at`,
		name, body.Phone, email, body.ServiceAreaId, body.CategoryId, body.Message, body.Source).Scan(&id, &created)
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	out := map[string]any{"id": id, "created_at": created, "full_name": name}
	for k, v := range map[string]any{"phone": body.Phone, "email": email, "service_area_id": body.ServiceAreaId, "category_id": body.CategoryId, "message": body.Message, "source": body.Source} {
		if !isNilPtr(v) {
			out[k] = v
		}
	}
	httpx.JSON(w, http.StatusCreated, out)
}

// isNilPtr reports whether v is a nil pointer, so optional fields are omitted instead of sent as null.
func isNilPtr(v any) bool {
	rv := reflect.ValueOf(v)
	return rv.Kind() == reflect.Ptr && rv.IsNil()
}
