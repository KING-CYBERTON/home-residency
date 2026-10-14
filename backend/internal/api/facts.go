package api

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/KING-CYBERTON/home-residency/backend/internal/domain"
)

// requestWindow is the time a job occupies the provider: requested_end, or the service default.
func requestWindow(start time.Time, end *time.Time, defaultMinutes *int) (time.Time, time.Time) {
	if end != nil {
		return start, *end
	}
	if defaultMinutes != nil && *defaultMinutes > 0 {
		return start, start.Add(time.Duration(*defaultMinutes) * time.Minute)
	}
	return start, start.Add(domain.DefaultVisitLength)
}

// providerFacts gathers what domain.CheckAssignable needs. excludeAssignment leaves one offer out of
// the live-job count, so accepting an offer does not count it against the provider twice.
func (s *Server) providerFacts(ctx context.Context, q db, providerID, categoryID uuid.UUID, start, end time.Time, excludeAssignment *uuid.UUID) (domain.ProviderFacts, error) {
	var f domain.ProviderFacts
	err := q.QueryRow(ctx, `
		SELECT p.status::text, COALESCE(pc.status::text, ''),
		  (SELECT count(*) FROM documents d
		    WHERE d.owner_user_id = p.user_id AND d.expires_on IS NOT NULL AND d.expires_on < current_date
		      AND d.status IN ('verified','expired')
		      AND d.document_type IN (SELECT i.document_type FROM checklist_template_items i
		                              JOIN checklist_templates t ON t.id = i.template_id
		                              WHERE t.category_id = $2 AND t.is_current AND i.is_mandatory AND i.kind = 'document')),
		  (SELECT count(*) FROM complaints c WHERE c.provider_id = p.user_id AND c.severity IN ('safeguarding','loss')
		    AND c.status NOT IN ('closed','withdrawn')),
		  (SELECT count(*) FROM assignments a WHERE a.provider_id = p.user_id AND a.status IN ('offered','accepted')
		    AND ($5::uuid IS NULL OR a.id <> $5)),
		  p.max_concurrent_jobs,
		  EXISTS (SELECT 1 FROM visits v WHERE v.provider_id = p.user_id AND v.status IN ('scheduled','checked_in')
		            AND tstzrange(v.scheduled_start, v.scheduled_end) && tstzrange($3, $4))
		  OR EXISTS (SELECT 1 FROM provider_time_off o WHERE o.provider_id = p.user_id AND o.period && tstzrange($3, $4))
		FROM providers p
		LEFT JOIN provider_categories pc ON pc.provider_id = p.user_id AND pc.category_id = $2
		WHERE p.user_id = $1`, providerID, categoryID, start, end, excludeAssignment).
		Scan(&f.Status, &f.CategoryStatus, &f.ExpiredMandatoryDocs, &f.OpenSafeguardingCases, &f.LiveJobs, &f.MaxConcurrentJobs, &f.BusyAtRequestedTime)
	return f, mapErr(err)
}
