package api

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/KING-CYBERTON/home-residency/backend/internal/api/gen"
	"github.com/KING-CYBERTON/home-residency/backend/internal/httpx"
)

// Report queries take $1 from, $2 to (exclusive), $3 category, $4 service area. A rate with an empty
// denominator is left out, not reported as zero.
var reportMetrics = map[string]string{
	"supply": `SELECT jsonb_build_object(
		'activated_providers', (SELECT count(*) FROM providers WHERE activated_at >= $1 AND activated_at < $2 AND ($4::uuid IS NULL OR home_service_area_id = $4)),
		'active_providers', (SELECT count(*) FROM providers WHERE status = 'active' AND ($4::uuid IS NULL OR home_service_area_id = $4)),
		'applications_decided', (SELECT count(*) FROM applications a WHERE a.decided_at >= $1 AND a.decided_at < $2 AND ($3::uuid IS NULL OR a.category_id = $3)),
		'vetting_cycle_days', (SELECT round(avg(extract(epoch FROM a.decided_at - a.submitted_at) / 86400)::numeric, 2) FROM applications a
			WHERE a.decided_at >= $1 AND a.decided_at < $2 AND a.submitted_at IS NOT NULL AND ($3::uuid IS NULL OR a.category_id = $3)),
		'pass_rate', (SELECT round(avg((a.status = 'approved')::int)::numeric, 4) FROM applications a
			WHERE a.decided_at >= $1 AND a.decided_at < $2 AND a.status IN ('approved','rejected') AND ($3::uuid IS NULL OR a.category_id = $3)),
		'retention_3m', (SELECT round(avg((status = 'active')::int)::numeric, 4) FROM providers WHERE activated_at IS NOT NULL AND activated_at < $2 - interval '3 months'))`,

	"demand": `WITH r AS (SELECT sr.* FROM service_requests sr WHERE sr.submitted_at >= $1 AND sr.submitted_at < $2
			AND ($3::uuid IS NULL OR sr.category_id = $3) AND ($4::uuid IS NULL OR sr.service_area_id = $4))
		SELECT jsonb_build_object(
		'registered_households', (SELECT count(*) FROM users WHERE kind = 'client' AND created_at >= $1 AND created_at < $2),
		'requests', (SELECT count(*) FROM r),
		'transacting_share', (SELECT round((SELECT count(DISTINCT client_id) FROM r)::numeric / NULLIF(count(*), 0), 4) FROM users WHERE kind = 'client' AND created_at < $2),
		'requests_per_household', (SELECT round(count(*)::numeric / NULLIF(count(DISTINCT client_id), 0), 2) FROM r),
		'repeat_rate', (SELECT round(avg((n >= 2)::int)::numeric, 4) FROM (
			SELECT count(*) AS n FROM service_requests x WHERE x.submitted_at < $2 AND x.client_id IN (SELECT client_id FROM r) GROUP BY x.client_id) c))`,

	"matching": `WITH r AS (SELECT sr.* FROM service_requests sr WHERE sr.submitted_at >= $1 AND sr.submitted_at < $2
			AND ($3::uuid IS NULL OR sr.category_id = $3) AND ($4::uuid IS NULL OR sr.service_area_id = $4)),
		settled AS (SELECT * FROM r WHERE status NOT IN ('submitted','triaging','awaiting_client_choice','awaiting_provider'))
		SELECT jsonb_build_object(
		'requests', (SELECT count(*) FROM r),
		'minutes_to_assignment', (SELECT round(avg(extract(epoch FROM confirmed_at - submitted_at) / 60)::numeric, 1) FROM r WHERE confirmed_at IS NOT NULL),
		'fill_rate', (SELECT round(avg((confirmed_at IS NOT NULL)::int)::numeric, 4) FROM settled),
		'cancellation_rate_client', (SELECT round(avg((cancelled_by = 'client')::int)::numeric, 4) FROM settled),
		'cancellation_rate_provider', (SELECT round(avg((cancelled_by = 'provider')::int)::numeric, 4) FROM settled),
		'unfilled_rate', (SELECT round(avg((status = 'unfilled')::int)::numeric, 4) FROM settled))`,

	"quality": `WITH r AS (SELECT sr.* FROM service_requests sr WHERE sr.submitted_at >= $1 AND sr.submitted_at < $2
			AND ($3::uuid IS NULL OR sr.category_id = $3) AND ($4::uuid IS NULL OR sr.service_area_id = $4))
		SELECT jsonb_build_object(
		'avg_rating', (SELECT round(avg(rt.stars)::numeric, 2) FROM ratings rt JOIN r ON r.id = rt.request_id),
		'ratings', (SELECT count(*) FROM ratings rt JOIN r ON r.id = rt.request_id),
		'complaints_per_100_jobs', (SELECT round(100.0 * (SELECT count(*) FROM complaints c JOIN r ON r.id = c.request_id) / NULLIF(count(*), 0), 2) FROM r WHERE r.completed_at IS NOT NULL),
		'spot_check_avg', (SELECT round(avg(total_score)::numeric, 2) FROM spot_checks WHERE checked_at >= $1 AND checked_at < $2),
		'disqualification_rate', (SELECT round((SELECT count(*) FROM disciplinary_actions WHERE kind = 'disqualification' AND status <> 'revoked' AND created_at >= $1 AND created_at < $2)::numeric
			/ NULLIF((SELECT count(*) FROM providers WHERE status IN ('active','suspended')), 0), 4)))`,

	"commercial": `WITH r AS (SELECT sr.* FROM service_requests sr WHERE sr.submitted_at >= $1 AND sr.submitted_at < $2
			AND ($3::uuid IS NULL OR sr.category_id = $3) AND ($4::uuid IS NULL OR sr.service_area_id = $4)),
		inv AS (SELECT i.* FROM invoices i JOIN r ON r.id = i.request_id WHERE i.status NOT IN ('draft','void')),
		rev AS (SELECT COALESCE(-sum(amount_minor), 0) AS v FROM ledger_entries WHERE account = 'platform_revenue' AND posted_at >= $1 AND posted_at < $2)
		SELECT jsonb_build_object(
		'billed_minor', (SELECT COALESCE(sum(total_minor), 0) FROM inv),
		'collected_minor', (SELECT COALESCE(sum(paid_minor), 0) FROM inv),
		'platform_revenue_minor', (SELECT v FROM rev),
		'take_rate', (SELECT round((SELECT v FROM rev)::numeric / NULLIF(sum(subtotal_minor), 0), 4) FROM inv),
		'contribution_per_job', (SELECT round((SELECT v FROM rev)::numeric / NULLIF(count(*), 0), 0) FROM r WHERE r.completed_at IS NOT NULL),
		'subscription_mrr', (SELECT COALESCE(sum(CASE p.billing_period WHEN 'monthly' THEN p.price_minor WHEN 'quarterly' THEN p.price_minor / 3.0 ELSE p.price_minor / 12.0 END), 0)
			FROM subscriptions s JOIN subscription_plans p ON p.id = s.plan_id WHERE s.status = 'active'))`,

	"compliance": `SELECT jsonb_build_object(
		'active_providers', (SELECT count(*) FROM providers WHERE status = 'active'),
		'pct_active_with_valid_clearance', (SELECT round(avg((EXISTS (SELECT 1 FROM documents d WHERE d.owner_user_id = p.user_id AND d.document_type = 'good_conduct_certificate'
			AND d.status = 'verified' AND (d.expires_on IS NULL OR d.expires_on >= current_date)))::int)::numeric, 4) FROM providers p WHERE p.status = 'active'),
		'documents_expiring_30d', (SELECT count(*) FROM documents d JOIN providers p ON p.user_id = d.owner_user_id AND p.status = 'active'
			WHERE d.status = 'verified' AND d.expires_on BETWEEN current_date AND current_date + 30),
		'documents_expired', (SELECT count(*) FROM documents d JOIN providers p ON p.user_id = d.owner_user_id AND p.status = 'active' WHERE d.status = 'expired'),
		'open_complaints', (SELECT count(*) FROM complaints WHERE status NOT IN ('closed','withdrawn')))`,
}

// reportSeries returns "SELECT bucket, vals" over service_requests sr, with %s as the bucket expression.
var reportSeries = map[string]string{
	"demand": `SELECT %s AS bucket, jsonb_build_object('requests', count(*), 'households', count(DISTINCT sr.client_id)) AS vals`,
	"matching": `SELECT %s AS bucket, jsonb_build_object('requests', count(*), 'confirmed', count(sr.confirmed_at), 'cancelled', count(*) FILTER (WHERE sr.status = 'cancelled'),
		'unfilled', count(*) FILTER (WHERE sr.status = 'unfilled')) AS vals`,
	"quality":    `SELECT %s AS bucket, jsonb_build_object('ratings', count(rt.id), 'avg_rating', COALESCE(round(avg(rt.stars)::numeric, 2), 0)) AS vals`,
	"commercial": `SELECT %s AS bucket, jsonb_build_object('billed_minor', COALESCE(sum(i.total_minor), 0), 'collected_minor', COALESCE(sum(i.paid_minor), 0)) AS vals`,
}

type reportOut struct {
	Report  string             `json:"report"`
	From    string             `json:"from"`
	To      string             `json:"to"`
	GroupBy *string            `json:"group_by,omitempty"`
	Metrics map[string]float64 `json:"metrics"`
	Series  []map[string]any   `json:"series,omitempty"`
}

func reportRange(from, to *time.Time) (time.Time, time.Time) {
	end := time.Now().UTC().Truncate(24 * time.Hour)
	if to != nil {
		end = to.UTC().Truncate(24 * time.Hour)
	}
	start := end.AddDate(0, 0, -30)
	if from != nil {
		start = from.UTC().Truncate(24 * time.Hour)
	}
	return start, end
}

func (s *Server) buildReport(ctx context.Context, report string, from, to time.Time, cat, area *uuid.UUID, groupBy string) (*reportOut, error) {
	q, ok := reportMetrics[report]
	if !ok {
		return nil, httpx.ErrNotFound
	}
	if to.Before(from) {
		return nil, httpx.Invalid("to must not be before from")
	}
	args := []any{from, to.AddDate(0, 0, 1), cat, area}
	var raw json.RawMessage
	qargs := args
	if report == "compliance" {
		qargs = nil // a point-in-time report: it takes no date range or filters
	}
	if err := s.DB.QueryRow(ctx, q, qargs...).Scan(&raw); err != nil {
		return nil, err
	}
	out := &reportOut{Report: report, From: from.Format("2006-01-02"), To: to.Format("2006-01-02"), Metrics: map[string]float64{}}
	var m map[string]*float64
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	for k, v := range m {
		if v != nil {
			out.Metrics[k] = *v
		}
	}
	tmpl, hasSeries := reportSeries[report]
	if groupBy != "" && hasSeries {
		out.GroupBy = &groupBy
		var bucket, joins string
		switch groupBy {
		case "day", "week", "month":
			bucket = fmt.Sprintf("date_trunc('%s', sr.submitted_at)::date::text", groupBy)
		case "category":
			bucket, joins = "c.slug", " JOIN service_categories c ON c.id = sr.category_id"
		case "service_area":
			bucket, joins = "a.slug", " JOIN service_areas a ON a.id = sr.service_area_id"
		default:
			return nil, httpx.Invalid("Unknown group_by")
		}
		switch report {
		case "quality":
			joins += " LEFT JOIN ratings rt ON rt.request_id = sr.id"
		case "commercial":
			joins += " LEFT JOIN invoices i ON i.request_id = sr.id AND i.status NOT IN ('draft','void')"
		}
		rows, err := s.DB.Query(ctx, fmt.Sprintf(tmpl, bucket)+` FROM service_requests sr`+joins+`
			WHERE sr.submitted_at >= $1 AND sr.submitted_at < $2 AND ($3::uuid IS NULL OR sr.category_id = $3) AND ($4::uuid IS NULL OR sr.service_area_id = $4)
			GROUP BY 1 ORDER BY 1`, args...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var b string
			var vals map[string]float64
			if err := rows.Scan(&b, &vals); err != nil {
				return nil, err
			}
			out.Series = append(out.Series, map[string]any{"bucket": b, "values": vals})
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *Server) GetConsoleReportsById(w http.ResponseWriter, r *http.Request, report gen.GetConsoleReportsByIdParamsReport, params gen.GetConsoleReportsByIdParams) {
	var f, t *time.Time
	if params.From != nil {
		f = &params.From.Time
	}
	if params.To != nil {
		t = &params.To.Time
	}
	from, to := reportRange(f, t)
	group := ""
	if params.GroupBy != nil {
		group = string(*params.GroupBy)
	}
	var cat, area *uuid.UUID
	if params.CategoryId != nil {
		c := uuid.UUID(*params.CategoryId)
		cat = &c
	}
	if params.ServiceAreaId != nil {
		a := uuid.UUID(*params.ServiceAreaId)
		area = &a
	}
	out, err := s.buildReport(r.Context(), string(report), from, to, cat, area, group)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

// GetConsoleReportsExport streams a report as flat CSV rows: section, bucket, name, value.
func (s *Server) GetConsoleReportsExport(w http.ResponseWriter, r *http.Request, report gen.GetConsoleReportsExportParamsReport, params gen.GetConsoleReportsExportParams) {
	var f, t *time.Time
	if params.From != nil {
		f = &params.From.Time
	}
	if params.To != nil {
		t = &params.To.Time
	}
	from, to := reportRange(f, t)
	out, err := s.buildReport(r.Context(), string(report), from, to, nil, nil, "")
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s-%s-%s.csv"`, out.Report, out.From, out.To))
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"report", "from", "to", "metric", "value"})
	names := make([]string, 0, len(out.Metrics))
	for k := range out.Metrics {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		_ = cw.Write([]string{out.Report, out.From, out.To, k, strconv.FormatFloat(out.Metrics[k], 'f', -1, 64)})
	}
	cw.Flush()
}
