package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/KING-CYBERTON/home-residency/backend/internal/api/gen"
	"github.com/KING-CYBERTON/home-residency/backend/internal/auth"
	"github.com/KING-CYBERTON/home-residency/backend/internal/httpx"
)

// appraisalDays is the rolling window the published appraisal scores.
const appraisalDays = 90

var defaultWeights = map[string]float64{"rating": 0.5, "complaints": 0.2, "spot_checks": 0.2, "reliability": 0.1}

// RecomputeAppraisal scores a provider over the last 90 days from the published weights and stores a
// snapshot. A component with no evidence yet is left out and the others share its weight, so a new
// provider is not marked down for having no spot check.
func (s *Server) RecomputeAppraisal(ctx context.Context, q db, providerID uuid.UUID) error {
	weights := defaultWeights
	var raw json.RawMessage
	if q.QueryRow(ctx, `SELECT value FROM app_settings WHERE key = 'appraisal.weights'`).Scan(&raw) == nil {
		var w map[string]float64
		if json.Unmarshal(raw, &w) == nil && len(w) > 0 {
			weights = w
		}
	}
	end := s.Now().UTC()
	start := end.AddDate(0, 0, -appraisalDays)
	var rating, spot, reliability *float64
	var upheld int
	if err := q.QueryRow(ctx, `
		SELECT
		  (SELECT avg(stars)::float8 / 5 * 100 FROM ratings WHERE provider_id = $1 AND created_at >= $2),
		  (SELECT count(*) FROM disciplinary_actions d WHERE d.provider_id = $1 AND d.created_at >= $2 AND d.kind <> 'no_action' AND d.status <> 'revoked'),
		  (SELECT avg(total_score)::float8 FROM spot_checks WHERE provider_id = $1 AND checked_at >= $2),
		  (SELECT CASE WHEN count(*) = 0 THEN NULL ELSE 100.0 * count(*) FILTER (WHERE status IN ('completed','client_confirmed')) / count(*) END
		     FROM visits WHERE provider_id = $1 AND scheduled_start >= $2 AND scheduled_start < now() AND status <> 'cancelled')`, providerID, start).
		Scan(&rating, &upheld, &spot, &reliability); err != nil {
		return err
	}
	if rating == nil && spot == nil && reliability == nil {
		return nil // no evidence yet; the complaints component alone would read as a perfect score
	}
	complaints := float64(100 - 25*upheld)
	if complaints < 0 {
		complaints = 0
	}
	comp := map[string]*float64{"rating": rating, "complaints": &complaints, "spot_checks": spot, "reliability": reliability}
	var sum, wsum float64
	for k, v := range comp {
		if v != nil {
			sum += *v * weights[k]
			wsum += weights[k]
		}
	}
	if wsum == 0 {
		return nil
	}
	total := round2(sum / wsum)
	wb, _ := json.Marshal(weights)
	if _, err := q.Exec(ctx, `
		INSERT INTO appraisal_snapshots (provider_id, period_start, period_end, rating_component, complaint_component, spot_check_component, reliability_component, weights, total_score)
		VALUES ($1,$2::date,$3::date,$4,$5,$6,$7,$8,$9)
		ON CONFLICT (provider_id, period_end) DO UPDATE SET rating_component = EXCLUDED.rating_component, complaint_component = EXCLUDED.complaint_component,
		  spot_check_component = EXCLUDED.spot_check_component, reliability_component = EXCLUDED.reliability_component, weights = EXCLUDED.weights,
		  total_score = EXCLUDED.total_score, computed_at = now()`,
		providerID, start, end, round2Ptr(rating), complaints, round2Ptr(spot), round2Ptr(reliability), wb, total); err != nil {
		return err
	}
	_, err := q.Exec(ctx, `UPDATE providers SET appraisal_score = $2 WHERE user_id = $1`, providerID, total)
	return err
}

func round2Ptr(f *float64) *float64 {
	if f == nil {
		return nil
	}
	v := round2(*f)
	return &v
}

func (s *Server) appraisal(ctx context.Context, q db, providerID uuid.UUID) (json.RawMessage, error) {
	var n int
	if err := q.QueryRow(ctx, `SELECT count(*) FROM providers WHERE user_id = $1`, providerID).Scan(&n); err != nil || n == 0 {
		return nil, httpx.ErrNotFound
	}
	if err := s.RecomputeAppraisal(ctx, q, providerID); err != nil {
		return nil, err
	}
	return rawOne(ctx, q, `
		SELECT jsonb_build_object('provider_id', a.provider_id, 'total_score', a.total_score,
		  'components', jsonb_build_object('rating', a.rating_component, 'complaints', a.complaint_component, 'spot_checks', a.spot_check_component, 'reliability', a.reliability_component),
		  'weights', a.weights, 'period_start', a.period_start, 'period_end', a.period_end,
		  'history', COALESCE((SELECT jsonb_agg(jsonb_build_object('period_end', h.period_end, 'total_score', h.total_score) ORDER BY h.period_end DESC)
		     FROM (SELECT period_end, total_score FROM appraisal_snapshots WHERE provider_id = a.provider_id ORDER BY period_end DESC LIMIT 12) h), '[]'::jsonb))
		FROM appraisal_snapshots a WHERE a.provider_id = $1 ORDER BY a.period_end DESC LIMIT 1`, providerID)
}

func (s *Server) respondAppraisal(w http.ResponseWriter, r *http.Request, providerID uuid.UUID) {
	raw, err := s.appraisal(r.Context(), s.DB, providerID)
	if err != nil {
		// A provider with no evidence yet still gets a well-formed, empty appraisal.
		if err == httpx.ErrNotFound {
			var n int
			if s.DB.QueryRow(r.Context(), `SELECT count(*) FROM providers WHERE user_id = $1`, providerID).Scan(&n) == nil && n > 0 {
				httpx.JSON(w, http.StatusOK, map[string]any{"provider_id": providerID, "total_score": nil, "components": map[string]any{}, "weights": defaultWeights,
					"history": []any{}})
				return
			}
		}
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, raw)
}

func (s *Server) GetProviderAppraisal(w http.ResponseWriter, r *http.Request) {
	s.respondAppraisal(w, r, auth.MustPrincipal(r.Context()).UserID)
}

func (s *Server) GetConsoleProvidersAppraisal(w http.ResponseWriter, r *http.Request, id gen.Id) {
	s.respondAppraisal(w, r, uuid.UUID(id))
}

// RecomputeAllAppraisals is the nightly job.
func (s *Server) RecomputeAllAppraisals(ctx context.Context) {
	rows, err := s.DB.Query(ctx, `SELECT user_id FROM providers WHERE status IN ('active','suspended')`)
	if err != nil {
		return
	}
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	for _, id := range ids {
		_ = s.RecomputeAppraisal(ctx, s.DB, id)
	}
}

var _ = time.Now
