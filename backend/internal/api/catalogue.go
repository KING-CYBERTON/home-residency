package api

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/KING-CYBERTON/home-residency/backend/internal/api/gen"
	dbpkg "github.com/KING-CYBERTON/home-residency/backend/internal/db"
	"github.com/KING-CYBERTON/home-residency/backend/internal/httpx"
)

// The catalogue, content and notification-template endpoints are plain create/read/update/delete
// over one table each. A resource describes the columns a request may set; the response is shaped
// in SQL, and the contract test checks it against openapi.yaml.

type col struct {
	name string
	kind string // text | int | num | bool | uuid | ts | json | enum:<type>
}

type resource struct {
	entity   string // audit entity type
	table    string
	sel      string // SELECT <jsonb> FROM <table> t (no WHERE)
	order    string
	cols     []col
	required []string
	after    string // optional statement run after a write: $1 = id, $2 = actor
}

func (c col) cast() string {
	switch {
	case c.kind == "uuid":
		return "::uuid"
	case c.kind == "ts":
		return "::timestamptz"
	case c.kind == "json":
		return "::jsonb"
	case strings.HasPrefix(c.kind, "enum:"):
		return "::" + strings.TrimPrefix(c.kind, "enum:")
	}
	return ""
}

// value converts a decoded JSON value to a query argument of the column's kind.
func (c col) value(v any) (any, error) {
	if v == nil {
		return nil, nil
	}
	bad := func() (any, error) { return nil, httpx.Invalid(c.name + " has the wrong type") }
	switch c.kind {
	case "int":
		f, ok := v.(float64)
		if !ok || f != math.Trunc(f) {
			return bad()
		}
		return int64(f), nil
	case "num":
		f, ok := v.(float64)
		if !ok {
			return bad()
		}
		return f, nil
	case "bool":
		b, ok := v.(bool)
		if !ok {
			return bad()
		}
		return b, nil
	case "json":
		b, err := json.Marshal(v)
		if err != nil {
			return bad()
		}
		return string(b), nil
	default:
		str, ok := v.(string)
		if !ok {
			return bad()
		}
		return str, nil
	}
}

func decodeMap(r *http.Request) (map[string]any, error) {
	m, err := httpx.Decode[map[string]any](r)
	if err != nil {
		return nil, err
	}
	if m == nil {
		return nil, httpx.BadRequest("Request body must be a JSON object")
	}
	return m, nil
}

func (s *Server) listResource(w http.ResponseWriter, r *http.Request, res resource) {
	out, err := rawAll(r.Context(), s.DB, res.sel+` ORDER BY `+res.order)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (s *Server) createResource(w http.ResponseWriter, r *http.Request, res resource) {
	in, err := decodeMap(r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	for _, k := range res.required {
		if v, ok := in[k]; !ok || v == nil || v == "" {
			httpx.Fail(w, r, httpx.Invalid(k+" is required"))
			return
		}
	}
	var names, marks []string
	var args []any
	for _, c := range res.cols {
		v, ok := in[c.name]
		if !ok {
			continue
		}
		val, err := c.value(v)
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		args = append(args, val)
		names = append(names, c.name)
		marks = append(marks, fmt.Sprintf("$%d%s", len(args), c.cast()))
	}
	ctx := r.Context()
	var id uuid.UUID
	err = dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, fmt.Sprintf(`INSERT INTO %s (%s) VALUES (%s) RETURNING id`, res.table, strings.Join(names, ","), strings.Join(marks, ",")), args...).Scan(&id); err != nil {
			return err
		}
		if res.after != "" {
			if _, err := tx.Exec(ctx, res.after, id, actorID(ctx)); err != nil {
				return err
			}
		}
		return audit(ctx, tx, res.entity+".create", res.entity, id, nil, in, "")
	})
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	raw, err := rawOne(ctx, s.DB, res.sel+` WHERE t.id = $1`, id)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, raw)
}

func (s *Server) updateResource(w http.ResponseWriter, r *http.Request, res resource, id uuid.UUID) {
	in, err := decodeMap(r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	for _, k := range res.required {
		if v, present := in[k]; present && (v == nil || v == "") {
			httpx.Fail(w, r, httpx.Invalid(k+" cannot be empty"))
			return
		}
	}
	sets := []string{}
	args := []any{id}
	for _, c := range res.cols {
		v, ok := in[c.name]
		if !ok {
			continue
		}
		val, err := c.value(v)
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		args = append(args, val)
		sets = append(sets, fmt.Sprintf("%s = $%d%s", c.name, len(args), c.cast()))
	}
	ctx := r.Context()
	err = dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		var before json.RawMessage
		if err := tx.QueryRow(ctx, res.sel+` WHERE t.id = $1 FOR UPDATE OF t`, id).Scan(&before); err != nil {
			return err
		}
		if len(sets) > 0 {
			if _, err := tx.Exec(ctx, fmt.Sprintf(`UPDATE %s SET %s WHERE id = $1`, res.table, strings.Join(sets, ", ")), args...); err != nil {
				return err
			}
		}
		if res.after != "" {
			if _, err := tx.Exec(ctx, res.after, id, actorID(ctx)); err != nil {
				return err
			}
		}
		return audit(ctx, tx, res.entity+".update", res.entity, id, before, in, "")
	})
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	raw, err := rawOne(ctx, s.DB, res.sel+` WHERE t.id = $1`, id)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, raw)
}

func (s *Server) deleteResource(w http.ResponseWriter, r *http.Request, res resource, id uuid.UUID) {
	ctx := r.Context()
	err := dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		var before json.RawMessage
		if err := tx.QueryRow(ctx, res.sel+` WHERE t.id = $1 FOR UPDATE OF t`, id).Scan(&before); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, fmt.Sprintf(`DELETE FROM %s WHERE id = $1`, res.table), id); err != nil {
			return err
		}
		return audit(ctx, tx, res.entity+".delete", res.entity, id, before, nil, "")
	})
	if pe, ok := err.(*pgconn.PgError); ok && pe.Code == "23503" {
		err = httpx.Conflict("It is still in use. Deactivate it instead of deleting it.")
	}
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

const stampless = `to_jsonb(t) - 'created_at' - 'updated_at'`

var (
	areaRes = resource{
		entity: "service_area", table: "service_areas", order: "t.name",
		sel:      `SELECT ` + stampless + ` FROM service_areas t`,
		required: []string{"slug", "name"},
		cols:     []col{{"slug", "text"}, {"name", "text"}, {"county", "text"}, {"is_active", "bool"}, {"centre_lat", "num"}, {"centre_lng", "num"}, {"radius_km", "num"}},
	}
	categoryRes = resource{
		entity: "category", table: "service_categories", order: "t.sort_order, t.name",
		sel:      `SELECT ` + stampless + ` FROM service_categories t`,
		required: []string{"slug", "name", "engagement"},
		cols: []col{{"slug", "text"}, {"name", "text"}, {"name_sw", "text"}, {"description", "text"}, {"engagement", "enum:engagement_type"},
			{"vetting_tier", "enum:vetting_tier"}, {"requires_client_id_verification", "bool"}, {"sop_markdown", "text"}, {"is_active", "bool"}, {"sort_order", "int"}},
	}
	serviceRes = resource{
		entity: "service", table: "services", order: "t.sort_order, t.name",
		sel: `SELECT ` + stampless + ` || jsonb_build_object('current_price', (
			SELECT to_jsonb(r) - 'created_by' - 'created_at' FROM rate_card_entries r
			WHERE r.service_id = t.id AND r.service_area_id IS NULL AND r.valid_from <= now() AND (r.valid_to IS NULL OR r.valid_to > now())
			ORDER BY r.valid_from DESC LIMIT 1)) FROM services t`,
		required: []string{"category_id", "slug", "name", "pricing_unit"},
		cols: []col{{"category_id", "uuid"}, {"slug", "text"}, {"name", "text"}, {"description", "text"}, {"scope_items", "json"}, {"pricing_unit", "enum:pricing_unit"},
			{"min_quantity", "num"}, {"max_quantity", "num"}, {"default_duration_minutes", "int"}, {"is_active", "bool"}, {"sort_order", "int"}},
	}
	planRes = resource{
		entity: "subscription_plan", table: "subscription_plans", order: "t.price_minor",
		sel:      `SELECT to_jsonb(t) - 'created_at' FROM subscription_plans t`,
		required: []string{"code", "name", "price_minor", "billing_period"},
		cols:     []col{{"code", "text"}, {"name", "text"}, {"description", "text"}, {"price_minor", "int"}, {"billing_period", "text"}, {"benefits", "json"}, {"is_active", "bool"}},
	}
	pageRes = resource{
		entity: "content_page", table: "content_pages", order: "t.slug, t.language",
		sel:      `SELECT to_jsonb(t) - 'created_at' - 'updated_by' FROM content_pages t`,
		required: []string{"slug", "title", "body_markdown"},
		cols: []col{{"slug", "text"}, {"language", "text"}, {"title", "text"}, {"meta_description", "text"}, {"body_markdown", "text"},
			{"category_id", "uuid"}, {"service_area_id", "uuid"}, {"is_published", "bool"}},
		after: `UPDATE content_pages SET updated_by = $2, published_at = CASE WHEN is_published THEN COALESCE(published_at, now()) ELSE NULL END WHERE id = $1`,
	}
	faqRes = resource{
		entity: "faq", table: "faqs", order: "t.sort_order, t.question",
		sel:      `SELECT to_jsonb(t) FROM faqs t`,
		required: []string{"question", "answer"},
		cols:     []col{{"category_id", "uuid"}, {"language", "text"}, {"question", "text"}, {"answer", "text"}, {"sort_order", "int"}, {"is_published", "bool"}},
	}
	templateRes = resource{
		entity: "notification_template", table: "notification_templates", order: "t.event_key, t.channel, t.language",
		sel:      `SELECT to_jsonb(t) - 'updated_at' FROM notification_templates t`,
		required: []string{"event_key", "channel", "language", "body"},
		cols:     []col{{"event_key", "text"}, {"channel", "enum:notification_channel"}, {"language", "text"}, {"subject", "text"}, {"body", "text"}, {"is_active", "bool"}},
	}
)

func (s *Server) GetConsoleServiceAreas(w http.ResponseWriter, r *http.Request) {
	s.listResource(w, r, areaRes)
}
func (s *Server) PostConsoleServiceAreas(w http.ResponseWriter, r *http.Request) {
	s.createResource(w, r, areaRes)
}
func (s *Server) PatchConsoleServiceAreasById(w http.ResponseWriter, r *http.Request, id gen.Id) {
	s.updateResource(w, r, areaRes, uuid.UUID(id))
}
func (s *Server) DeleteConsoleServiceAreasById(w http.ResponseWriter, r *http.Request, id gen.Id) {
	s.deleteResource(w, r, areaRes, uuid.UUID(id))
}

func (s *Server) GetConsoleCategories(w http.ResponseWriter, r *http.Request) {
	s.listResource(w, r, categoryRes)
}
func (s *Server) PostConsoleCategories(w http.ResponseWriter, r *http.Request) {
	s.createResource(w, r, categoryRes)
}
func (s *Server) PatchConsoleCategoriesById(w http.ResponseWriter, r *http.Request, id gen.Id) {
	s.updateResource(w, r, categoryRes, uuid.UUID(id))
}
func (s *Server) DeleteConsoleCategoriesById(w http.ResponseWriter, r *http.Request, id gen.Id) {
	s.deleteResource(w, r, categoryRes, uuid.UUID(id))
}

func (s *Server) GetConsoleServices(w http.ResponseWriter, r *http.Request) {
	s.listResource(w, r, serviceRes)
}
func (s *Server) PostConsoleServices(w http.ResponseWriter, r *http.Request) {
	s.createResource(w, r, serviceRes)
}
func (s *Server) PatchConsoleServicesById(w http.ResponseWriter, r *http.Request, id gen.Id) {
	s.updateResource(w, r, serviceRes, uuid.UUID(id))
}
func (s *Server) DeleteConsoleServicesById(w http.ResponseWriter, r *http.Request, id gen.Id) {
	s.deleteResource(w, r, serviceRes, uuid.UUID(id))
}

func (s *Server) GetConsoleSubscriptionPlans(w http.ResponseWriter, r *http.Request) {
	s.listResource(w, r, planRes)
}
func (s *Server) PostConsoleSubscriptionPlans(w http.ResponseWriter, r *http.Request) {
	s.createResource(w, r, planRes)
}
func (s *Server) PatchConsoleSubscriptionPlansById(w http.ResponseWriter, r *http.Request, id gen.Id) {
	s.updateResource(w, r, planRes, uuid.UUID(id))
}
func (s *Server) DeleteConsoleSubscriptionPlansById(w http.ResponseWriter, r *http.Request, id gen.Id) {
	s.deleteResource(w, r, planRes, uuid.UUID(id))
}

func (s *Server) GetConsoleContentPages(w http.ResponseWriter, r *http.Request) {
	s.listResource(w, r, pageRes)
}
func (s *Server) PostConsoleContentPages(w http.ResponseWriter, r *http.Request) {
	s.createResource(w, r, pageRes)
}
func (s *Server) PatchConsoleContentPagesById(w http.ResponseWriter, r *http.Request, id gen.Id) {
	s.updateResource(w, r, pageRes, uuid.UUID(id))
}
func (s *Server) DeleteConsoleContentPagesById(w http.ResponseWriter, r *http.Request, id gen.Id) {
	s.deleteResource(w, r, pageRes, uuid.UUID(id))
}

func (s *Server) GetConsoleFaqs(w http.ResponseWriter, r *http.Request) { s.listResource(w, r, faqRes) }
func (s *Server) PostConsoleFaqs(w http.ResponseWriter, r *http.Request) {
	s.createResource(w, r, faqRes)
}
func (s *Server) PatchConsoleFaqsById(w http.ResponseWriter, r *http.Request, id gen.Id) {
	s.updateResource(w, r, faqRes, uuid.UUID(id))
}
func (s *Server) DeleteConsoleFaqsById(w http.ResponseWriter, r *http.Request, id gen.Id) {
	s.deleteResource(w, r, faqRes, uuid.UUID(id))
}

func (s *Server) GetConsoleNotificationTemplates(w http.ResponseWriter, r *http.Request) {
	s.listResource(w, r, templateRes)
}
func (s *Server) PostConsoleNotificationTemplates(w http.ResponseWriter, r *http.Request) {
	s.createResource(w, r, templateRes)
}
func (s *Server) PatchConsoleNotificationTemplatesById(w http.ResponseWriter, r *http.Request, id gen.Id) {
	s.updateResource(w, r, templateRes, uuid.UUID(id))
}
func (s *Server) DeleteConsoleNotificationTemplatesById(w http.ResponseWriter, r *http.Request, id gen.Id) {
	s.deleteResource(w, r, templateRes, uuid.UUID(id))
}

// ---- rate card ----

const rateJSON = `SELECT to_jsonb(r) - 'created_by' - 'created_at' FROM rate_card_entries r`

func (s *Server) GetConsoleRateCard(w http.ResponseWriter, r *http.Request, params gen.GetConsoleRateCardParams) {
	b := &qb{}
	if params.ServiceId != nil {
		b.add("r.service_id = ?", uuid.UUID(*params.ServiceId))
	}
	if params.ServiceAreaId != nil {
		b.add("r.service_area_id = ?", uuid.UUID(*params.ServiceAreaId))
	}
	if params.CurrentOnly != nil && *params.CurrentOnly {
		b.raw("r.valid_from <= now() AND (r.valid_to IS NULL OR r.valid_to > now())")
	}
	out, err := rawAll(r.Context(), s.DB, rateJSON+` WHERE `+b.where()+` ORDER BY r.service_id, r.valid_from DESC`, b.args...)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

// PostConsoleRateCard adds a price. An open-ended price for the same service and area that started
// earlier is closed at the new start, so a price change is one request.
func (s *Server) PostConsoleRateCard(w http.ResponseWriter, r *http.Request) {
	in, err := decodeMap(r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	cols := []col{{"service_id", "uuid"}, {"service_area_id", "uuid"}, {"client_price_minor", "int"}, {"provider_pay_minor", "int"},
		{"placement_fee_minor", "int"}, {"valid_from", "ts"}, {"valid_to", "ts"}}
	for _, k := range []string{"service_id", "client_price_minor", "provider_pay_minor", "valid_from"} {
		if in[k] == nil {
			httpx.Fail(w, r, httpx.Invalid(k+" is required"))
			return
		}
	}
	var names, marks []string
	var args []any
	for _, c := range cols {
		v, ok := in[c.name]
		if !ok {
			continue
		}
		val, err := c.value(v)
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		args = append(args, val)
		names = append(names, c.name)
		marks = append(marks, fmt.Sprintf("$%d%s", len(args), c.cast()))
	}
	ctx := r.Context()
	var id uuid.UUID
	err = dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE rate_card_entries SET valid_to = $3::timestamptz
			WHERE service_id = $1::uuid AND service_area_id IS NOT DISTINCT FROM $2::uuid AND valid_to IS NULL AND valid_from < $3::timestamptz`,
			in["service_id"], in["service_area_id"], in["valid_from"]); err != nil {
			return err
		}
		args = append(args, actorID(ctx))
		if err := tx.QueryRow(ctx, fmt.Sprintf(`INSERT INTO rate_card_entries (%s, created_by) VALUES (%s, $%d) RETURNING id`,
			strings.Join(names, ","), strings.Join(marks, ","), len(args)), args...).Scan(&id); err != nil {
			return err
		}
		return audit(ctx, tx, "rate_card.create", "rate_card_entry", id, nil, in, "")
	})
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	raw, err := rawOne(ctx, s.DB, rateJSON+` WHERE r.id = $1`, id)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, raw)
}

// ---- onboarding checklists ----

const tplChecklistJSON = `
	SELECT to_jsonb(t) || jsonb_build_object('items', COALESCE((
	  SELECT jsonb_agg(to_jsonb(i) - 'template_id' ORDER BY i.sort_order, i.id) FROM checklist_template_items i WHERE i.template_id = t.id), '[]'::jsonb))
	FROM checklist_templates t`

func (s *Server) GetConsoleChecklists(w http.ResponseWriter, r *http.Request, params gen.GetConsoleChecklistsParams) {
	b := &qb{}
	if params.CategoryId != nil {
		b.add("t.category_id = ?", uuid.UUID(*params.CategoryId))
	}
	out, err := rawAll(r.Context(), s.DB, tplChecklistJSON+` WHERE `+b.where()+` ORDER BY t.category_id, t.version DESC`, b.args...)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

// PostConsoleChecklists drafts the next version of a category's checklist. It does not apply until
// an approver publishes it.
func (s *Server) PostConsoleChecklists(w http.ResponseWriter, r *http.Request) {
	in, err := httpx.Decode[gen.ChecklistTemplateInput](r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if len(in.Items) == 0 {
		httpx.Fail(w, r, httpx.Invalid("A checklist needs at least one item"))
		return
	}
	ctx := r.Context()
	var id uuid.UUID
	err = dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `INSERT INTO checklist_templates (category_id, version)
			SELECT $1, COALESCE(max(version), 0) + 1 FROM checklist_templates WHERE category_id = $1 RETURNING id`, uuid.UUID(in.CategoryId)).Scan(&id); err != nil {
			return err
		}
		for n, it := range in.Items {
			if strings.TrimSpace(it.Label) == "" {
				return httpx.Invalid("Every item needs a label")
			}
			order := n
			if it.SortOrder != nil {
				order = *it.SortOrder
			}
			minCount := 1
			if it.MinCount != nil {
				minCount = *it.MinCount
			}
			if _, err := tx.Exec(ctx, `INSERT INTO checklist_template_items (template_id, kind, document_type, label, is_mandatory, min_count, validity_months, sort_order)
				VALUES ($1, $2::checklist_item_kind, NULLIF($3,'')::document_type, $4, $5, $6, $7, $8)`,
				id, string(it.Kind), docType(it.DocumentType), strings.TrimSpace(it.Label), it.IsMandatory, minCount, it.ValidityMonths, order); err != nil {
				return err
			}
		}
		return audit(ctx, tx, "checklist.draft", "checklist_template", id, nil, in, "")
	})
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	raw, err := rawOne(ctx, s.DB, tplChecklistJSON+` WHERE t.id = $1`, id)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, raw)
}

func (s *Server) PostConsoleChecklistsPublish(w http.ResponseWriter, r *http.Request, id gen.Id) {
	ctx := r.Context()
	err := dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		var cat uuid.UUID
		var cur bool
		if err := tx.QueryRow(ctx, `SELECT category_id, is_current FROM checklist_templates WHERE id = $1 FOR UPDATE`, uuid.UUID(id)).Scan(&cat, &cur); err != nil {
			return err
		}
		if cur {
			return httpx.Conflict("This version is already published")
		}
		if _, err := tx.Exec(ctx, `UPDATE checklist_templates SET is_current = false WHERE category_id = $1 AND is_current`, cat); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE checklist_templates SET is_current = true, approved_by = $2 WHERE id = $1`, uuid.UUID(id), actorID(ctx)); err != nil {
			return err
		}
		return audit(ctx, tx, "checklist.publish", "checklist_template", uuid.UUID(id), nil, map[string]any{"category_id": cat}, "")
	})
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	raw, err := rawOne(ctx, s.DB, tplChecklistJSON+` WHERE t.id = $1`, uuid.UUID(id))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, raw)
}

func docType(d *gen.DocumentType) string {
	if d == nil {
		return ""
	}
	return string(*d)
}
