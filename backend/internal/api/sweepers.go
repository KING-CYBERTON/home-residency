package api

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	dbpkg "github.com/KING-CYBERTON/home-residency/backend/internal/db"
)

// RunSweepers runs the periodic jobs that the SQS consumers and EventBridge schedules will own once
// they exist. Each pass is idempotent, so running it on every instance is safe.
func (s *Server) RunSweepers(ctx context.Context) {
	fast := time.NewTicker(30 * time.Second)
	slow := time.NewTicker(time.Hour)
	defer fast.Stop()
	defer slow.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-fast.C:
			s.ReprocessCallbacks(ctx)
		case <-slow.C:
			s.ExpireDocuments(ctx)
			s.LiftExpiredSuspensions(ctx)
			s.RecomputeAllAppraisals(ctx)
		}
	}
}

// ExpireDocuments marks lapsed documents, suspends providers whose mandatory documents lapsed
// (report §5.1: an expired mandatory document is an automatic suspension) and warns those about to.
func (s *Server) ExpireDocuments(ctx context.Context) {
	if _, err := s.DB.Exec(ctx, `UPDATE documents SET status = 'expired' WHERE status = 'verified' AND expires_on < current_date`); err != nil {
		slog.Error("expire documents", "err", err)
		return
	}
	rows, err := s.DB.Query(ctx, `SELECT user_id FROM providers WHERE status IN ('active','approved')`)
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
		err := dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
			n, err := s.expiredMandatoryDocs(ctx, tx, id)
			if err != nil || n == 0 {
				return err
			}
			if err := s.suspendProvider(ctx, tx, id, "A mandatory document has expired"); err != nil {
				return err
			}
			return audit(ctx, tx, "provider.auto_suspend", "provider", id, nil, map[string]any{"status": "suspended"}, "mandatory document expired")
		})
		if err != nil {
			slog.Error("auto-suspend", "provider", id, "err", err)
		}
	}
	days := int(s.setting(ctx, s.DB, "document.expiry_warning_days", 30))
	_, _ = s.DB.Exec(ctx, `
		INSERT INTO notifications (user_id, event_key, channel, status, title, body, data, sent_at)
		SELECT d.owner_user_id, 'document.expiring', 'in_app', 'delivered', 'Document expiring',
		       'Your ' || replace(d.document_type::text, '_', ' ') || ' expires on ' || to_char(d.expires_on, 'DD Mon YYYY') || '. Upload a renewed copy.',
		       jsonb_build_object('document_id', d.id), now()
		FROM documents d JOIN providers p ON p.user_id = d.owner_user_id
		WHERE d.status = 'verified' AND d.expires_on BETWEEN current_date AND current_date + $1::int AND p.status IN ('active','approved')
		  AND NOT EXISTS (SELECT 1 FROM notifications n WHERE n.event_key = 'document.expiring' AND n.data->>'document_id' = d.id::text)`, days)
}

// LiftExpiredSuspensions ends timed suspensions whose date has passed, unless something else still blocks the provider.
func (s *Server) LiftExpiredSuspensions(ctx context.Context) {
	rows, err := s.DB.Query(ctx, `SELECT DISTINCT provider_id FROM disciplinary_actions WHERE kind = 'suspension' AND status = 'in_effect' AND effective_to IS NOT NULL AND effective_to < now()`)
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
		err := dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
			b, err := s.reinstateBlockers(ctx, tx, id)
			if err != nil || len(b) > 0 {
				return err
			}
			if err := s.liftSuspension(ctx, tx, id); err != nil {
				return err
			}
			return audit(ctx, tx, "provider.auto_reinstate", "provider", id, nil, map[string]any{"status": "active"}, "suspension period ended")
		})
		if err != nil {
			slog.Error("lift suspension", "provider", id, "err", err)
		}
	}
}
