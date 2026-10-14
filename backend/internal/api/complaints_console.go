package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/KING-CYBERTON/home-residency/backend/internal/api/gen"
	"github.com/KING-CYBERTON/home-residency/backend/internal/auth"
	dbpkg "github.com/KING-CYBERTON/home-residency/backend/internal/db"
	"github.com/KING-CYBERTON/home-residency/backend/internal/httpx"
)

func (s *Server) GetConsoleComplaints(w http.ResponseWriter, r *http.Request, params gen.GetConsoleComplaintsParams) {
	cur, err := decodeCursor(params.Cursor)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	b := &qb{}
	if params.Status != nil {
		b.add("c.status = ?::complaint_status", string(*params.Status))
	}
	if params.Severity != nil {
		b.add("c.severity = ?::complaint_severity", string(*params.Severity))
	}
	if params.ProviderId != nil {
		b.add("c.provider_id = ?", uuid.UUID(*params.ProviderId))
	}
	if params.InvestigatorId != nil {
		b.add("c.investigator_id = ?", uuid.UUID(*params.InvestigatorId))
	}
	if params.From != nil {
		b.add("c.created_at >= ?", params.From.Time)
	}
	if params.To != nil {
		b.add("c.created_at < ?", params.To.Time.AddDate(0, 0, 1))
	}
	pg, err := pagedRaw(r.Context(), s.DB, `SELECT `+complaintStaffCols+`, c.created_at AS _t, c.id AS _id FROM complaints c WHERE `+b.where(), b.args, cur, pageLimit(params.Limit))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, pg)
}

func (s *Server) GetConsoleComplaintsById(w http.ResponseWriter, r *http.Request, id gen.Id) {
	m, err := s.complaintStaff(r.Context(), s.DB, uuid.UUID(id))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	_ = audit(r.Context(), s.DB, "complaint.view", "complaint", uuid.UUID(id), nil, nil, "")
	httpx.JSON(w, http.StatusOK, m)
}

func (s *Server) PostConsoleComplaints(w http.ResponseWriter, r *http.Request) {
	in, err := httpx.Decode[gen.ComplaintStaffCreate](r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	ctx := r.Context()
	me := auth.MustPrincipal(ctx).UserID
	source := "management"
	if in.Source != nil {
		source = string(*in.Source)
	}
	c := newComplaint{source: source, severity: string(in.Severity), summary: in.Summary, raisedBy: me, clientID: uuidPtr(in.ClientId), providerID: uuidPtr(in.ProviderId),
		requestID: uuidPtr(in.RequestId), visitID: uuidPtr(in.VisitId), evidence: uuids(in.EvidenceDocumentIds)}
	if in.Details != nil {
		c.details = *in.Details
	}
	if c.clientID == nil && c.providerID == nil && c.requestID == nil {
		httpx.Fail(w, r, httpx.Invalid("Say who it is about: give a client_id, provider_id or request_id"))
		return
	}
	var id uuid.UUID
	err = dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) (err error) {
		id, err = s.openComplaint(ctx, tx, c)
		return err
	})
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	s.respondComplaintStaff(w, r, id, http.StatusCreated)
}

func (s *Server) PostConsoleComplaintsAssign(w http.ResponseWriter, r *http.Request, id gen.Id) {
	in, err := httpx.Decode[gen.ComplaintAssign](r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	ctx := r.Context()
	err = dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		var ok bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM staff_roles sr JOIN users u ON u.id = sr.user_id
			WHERE sr.user_id = $1 AND sr.role = 'supervisor' AND u.status = 'active')`, uuid.UUID(in.InvestigatorId)).Scan(&ok); err != nil {
			return err
		}
		if !ok {
			return httpx.Invalid("The investigator must be an active supervisor")
		}
		var status, ref string
		if err := tx.QueryRow(ctx, `SELECT status::text, reference FROM complaints WHERE id = $1 FOR UPDATE`, uuid.UUID(id)).Scan(&status, &ref); err != nil {
			return err
		}
		if status != "open" && status != "investigating" && status != "awaiting_provider_account" {
			return httpx.Conflict("A case that is " + status + " cannot be reassigned")
		}
		next := status
		if status == "open" {
			next = "investigating"
		}
		if _, err := tx.Exec(ctx, `UPDATE complaints SET investigator_id = $2, status = $3::complaint_status WHERE id = $1`, uuid.UUID(id), uuid.UUID(in.InvestigatorId), next); err != nil {
			return err
		}
		if err := notify(ctx, tx, uuid.UUID(in.InvestigatorId), "complaint.assigned", "Case "+ref+" assigned to you", "", map[string]any{"complaint_id": id}); err != nil {
			return err
		}
		return audit(ctx, tx, "complaint.assign", "complaint", uuid.UUID(id), map[string]any{"status": status}, map[string]any{"investigator_id": in.InvestigatorId}, "")
	})
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	s.respondComplaintStaff(w, r, uuid.UUID(id), http.StatusOK)
}

func (s *Server) PostConsoleComplaintsEvents(w http.ResponseWriter, r *http.Request, id gen.Id) {
	in, err := httpx.Decode[gen.ComplaintEventCreate](r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	body := ""
	if in.Body != nil {
		body = strings.TrimSpace(*in.Body)
	}
	if string(in.EventType) == "note" && body == "" {
		httpx.Fail(w, r, httpx.Invalid("A note needs a body"))
		return
	}
	if string(in.EventType) == "evidence" && in.DocumentId == nil {
		httpx.Fail(w, r, httpx.Invalid("Evidence needs a document_id"))
		return
	}
	ctx := r.Context()
	internal := true
	if in.IsInternal != nil {
		internal = *in.IsInternal
	}
	var eid uuid.UUID
	err = dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		var status string
		if err := tx.QueryRow(ctx, `SELECT status::text FROM complaints WHERE id = $1 FOR UPDATE`, uuid.UUID(id)).Scan(&status); err != nil {
			return err
		}
		if status == "closed" || status == "withdrawn" {
			return httpx.Conflict("This case is " + status)
		}
		if err := tx.QueryRow(ctx, `INSERT INTO complaint_events (complaint_id, actor_id, event_type, body, document_id, is_internal) VALUES ($1,$2,$3,NULLIF($4,''),$5,$6) RETURNING id`,
			uuid.UUID(id), actorID(ctx), string(in.EventType), body, uuidPtr(in.DocumentId), internal).Scan(&eid); err != nil {
			return err
		}
		return audit(ctx, tx, "complaint.event", "complaint", uuid.UUID(id), nil, map[string]any{"event_id": eid, "type": in.EventType}, "")
	})
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	raw, err := rawOne(ctx, s.DB, `SELECT jsonb_build_object('id', e.id, 'actor_id', e.actor_id, 'event_type', e.event_type, 'body', e.body, 'document_id', e.document_id,
		'is_internal', e.is_internal, 'created_at', e.created_at) FROM complaint_events e WHERE e.id = $1`, eid)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, raw)
}

func (s *Server) PostConsoleComplaintsRequestProviderAccount(w http.ResponseWriter, r *http.Request, id gen.Id) {
	ctx := r.Context()
	err := dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		var status, ref string
		var provider *uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT status::text, provider_id, reference FROM complaints WHERE id = $1 FOR UPDATE`, uuid.UUID(id)).Scan(&status, &provider, &ref); err != nil {
			return err
		}
		if provider == nil {
			return httpx.Conflict("This complaint is not about a provider")
		}
		if status != "investigating" {
			return httpx.Conflict("Assign an investigator first, and ask only while investigating (the case is " + status + ")")
		}
		if _, err := tx.Exec(ctx, `UPDATE complaints SET status = 'awaiting_provider_account', provider_heard_at = NULL WHERE id = $1`, uuid.UUID(id)); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO complaint_events (complaint_id, actor_id, event_type, body, is_internal) VALUES ($1,$2,'status_change','Asked the provider for their account',true)`,
			uuid.UUID(id), actorID(ctx)); err != nil {
			return err
		}
		if err := notify(ctx, tx, *provider, "complaint.account_requested", "Please give your account of case "+ref, "A complaint was raised about a job. We need to hear your side before any decision.",
			map[string]any{"complaint_id": id}); err != nil {
			return err
		}
		return audit(ctx, tx, "complaint.request_account", "complaint", uuid.UUID(id), nil, nil, "")
	})
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	s.respondComplaintStaff(w, r, uuid.UUID(id), http.StatusOK)
}

// PostConsoleComplaintsProviderAccount lets staff record what the provider said by phone, or that
// they did not respond, so a silent provider cannot hold a case open.
func (s *Server) PostConsoleComplaintsProviderAccount(w http.ResponseWriter, r *http.Request, id gen.Id) {
	in, err := httpx.Decode[gen.ProviderAccountInput](r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	ctx := r.Context()
	if err := dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		return s.recordProviderAccount(ctx, tx, uuid.UUID(id), in.Account, uuids(in.DocumentIds), false)
	}); err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	s.respondComplaintStaff(w, r, uuid.UUID(id), http.StatusCreated)
}

func (s *Server) PostConsoleComplaintsReport(w http.ResponseWriter, r *http.Request, id gen.Id) {
	in, err := httpx.Decode[gen.InvestigationReport](r)
	if err != nil || len(strings.TrimSpace(in.InvestigationReport)) < 20 {
		httpx.Fail(w, r, httpx.Invalid("The report must be at least 20 characters"))
		return
	}
	ctx := r.Context()
	me := auth.MustPrincipal(ctx).UserID
	err = dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		var status string
		var investigator, provider *uuid.UUID
		var heard *time.Time
		if err := tx.QueryRow(ctx, `SELECT status::text, investigator_id, provider_id, provider_heard_at FROM complaints WHERE id = $1 FOR UPDATE`, uuid.UUID(id)).
			Scan(&status, &investigator, &provider, &heard); err != nil {
			return err
		}
		if investigator == nil || *investigator != me {
			return httpx.Forbidden("not_investigator", "Only the assigned investigator can submit the report")
		}
		if status != "investigating" {
			return httpx.Conflict("A report can be submitted while investigating (the case is " + status + ")")
		}
		if provider != nil && heard == nil {
			return httpx.RuleViolation("The provider must be heard before the report is submitted")
		}
		if _, err := tx.Exec(ctx, `UPDATE complaints SET investigation_report = $2, status = 'awaiting_decision' WHERE id = $1`, uuid.UUID(id), strings.TrimSpace(in.InvestigationReport)); err != nil {
			return err
		}
		return audit(ctx, tx, "complaint.report", "complaint", uuid.UUID(id), map[string]any{"status": status}, map[string]any{"status": "awaiting_decision"}, "")
	})
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	s.respondComplaintStaff(w, r, uuid.UUID(id), http.StatusCreated)
}

// ---- discipline

// applyDiscipline records a sanction on the ladder and takes the immediate steps it implies.
func (s *Server) applyDiscipline(ctx context.Context, tx pgx.Tx, providerID uuid.UUID, complaintID *uuid.UUID, kind, reason string, until *time.Time, course *uuid.UUID) (uuid.UUID, error) {
	reason = strings.TrimSpace(reason)
	if len(reason) < 3 {
		return uuid.Nil, httpx.Invalid("reason is required")
	}
	var status string
	if err := tx.QueryRow(ctx, `SELECT status::text FROM providers WHERE user_id = $1 FOR UPDATE`, providerID).Scan(&status); err != nil {
		return uuid.Nil, err
	}
	if status == "disqualified" || status == "withdrawn" {
		return uuid.Nil, httpx.Conflict("This provider is " + status)
	}
	if until != nil && !until.After(s.Now()) {
		return uuid.Nil, httpx.Invalid("effective_to must be in the future")
	}
	state := "in_effect"
	switch kind {
	case "warning", "no_action":
		state = "completed"
	case "disqualification":
		state = "pending_authorisation"
	case "retraining":
		if course == nil {
			return uuid.Nil, httpx.Invalid("retraining_course_id is required for retraining")
		}
	}
	if kind != "suspension" {
		until = nil
	}
	var id uuid.UUID
	if err := tx.QueryRow(ctx, `INSERT INTO disciplinary_actions (provider_id, complaint_id, kind, status, reason, effective_to, decided_by)
		VALUES ($1,$2,$3::disciplinary_action_kind,$4::disciplinary_action_status,$5,$6,$7) RETURNING id`,
		providerID, complaintID, kind, state, reason, until, *actorID(ctx)).Scan(&id); err != nil {
		return uuid.Nil, err
	}
	if kind == "retraining" {
		if _, err := tx.Exec(ctx, `INSERT INTO training_enrolments (provider_id, course_id, status, is_retraining, complaint_id) VALUES ($1,$2,'scheduled',true,$3)`,
			providerID, *course, complaintID); err != nil {
			return uuid.Nil, httpx.Invalid("Unknown retraining course")
		}
	}
	if kind == "suspension" || kind == "retraining" || kind == "disqualification" {
		if err := s.suspendProvider(ctx, tx, providerID, reason); err != nil {
			return uuid.Nil, err
		}
	}
	if err := notify(ctx, tx, providerID, "provider.disciplinary", "Decision on your account: "+strings.ReplaceAll(kind, "_", " "), reason, map[string]any{"action_id": id}); err != nil {
		return uuid.Nil, err
	}
	return id, audit(ctx, tx, "disciplinary."+kind, "disciplinary_action", id, nil, map[string]any{"provider_id": providerID, "complaint_id": complaintID, "status": state}, reason)
}

func (s *Server) PostConsoleComplaintsDecision(w http.ResponseWriter, r *http.Request, id gen.Id) {
	in, err := httpx.Decode[gen.ComplaintDecision](r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	outcome := strings.TrimSpace(in.OutcomeSummary)
	if len(outcome) < 10 {
		httpx.Fail(w, r, httpx.Invalid("outcome_summary must be at least 10 characters"))
		return
	}
	ctx := r.Context()
	me := auth.MustPrincipal(ctx).UserID
	err = dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		var status, ref string
		var investigator, provider, request *uuid.UUID
		var heard *time.Time
		if err := tx.QueryRow(ctx, `SELECT status::text, investigator_id, provider_id, request_id, provider_heard_at, reference FROM complaints WHERE id = $1 FOR UPDATE`, uuid.UUID(id)).
			Scan(&status, &investigator, &provider, &request, &heard, &ref); err != nil {
			return err
		}
		if status != "awaiting_decision" {
			return httpx.Conflict("A decision can be made once the investigation report is in (the case is " + status + ")")
		}
		if investigator != nil && *investigator == me {
			return httpx.Conflict("The investigator cannot decide their own case")
		}
		if provider != nil && heard == nil {
			return httpx.RuleViolation("The provider must be heard before a decision")
		}
		if provider == nil && string(in.Action) != "no_action" {
			return httpx.Invalid("Only no_action can be decided when the case is not about a provider")
		}
		if provider != nil {
			if _, err := s.applyDiscipline(ctx, tx, *provider, ptrUUID(uuid.UUID(id)), string(in.Action), in.Reason, in.SuspensionUntil, uuidPtr(in.RetrainingCourseId)); err != nil {
				return err
			}
		}
		if in.Refund != nil && in.Refund.PaymentId != nil && in.Refund.AmountMinor != nil {
			var paid int64
			var pstatus string
			if err := tx.QueryRow(ctx, `SELECT amount_minor, status::text FROM payments WHERE id = $1`, uuid.UUID(*in.Refund.PaymentId)).Scan(&paid, &pstatus); err != nil {
				return httpx.Invalid("Unknown payment for the refund")
			}
			if pstatus != "succeeded" || *in.Refund.AmountMinor <= 0 || *in.Refund.AmountMinor > paid {
				return httpx.Invalid("The refund must be positive and no more than the payment")
			}
			if _, err := tx.Exec(ctx, `INSERT INTO refunds (payment_id, amount_minor, reason, complaint_id, status, requested_by) VALUES ($1,$2,$3,$4,'requested',$5)`,
				uuid.UUID(*in.Refund.PaymentId), *in.Refund.AmountMinor, "Complaint "+ref, uuid.UUID(id), me); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE complaints SET status = 'decided', decided_by = $2, decided_at = now(), outcome_summary = $3 WHERE id = $1`, uuid.UUID(id), me, outcome); err != nil {
			return err
		}
		// A cleared provider is paid: the hold the low rating put on the earning ends with the case.
		if request != nil && (string(in.Action) == "no_action" || string(in.Action) == "warning") {
			if _, err := tx.Exec(ctx, `UPDATE provider_earnings SET status = 'pending', hold_reason = NULL WHERE request_id = $1 AND status = 'held'`, *request); err != nil {
				return err
			}
			if err := s.releaseEarnings(ctx, tx, *request); err != nil {
				return err
			}
		}
		return audit(ctx, tx, "complaint.decision", "complaint", uuid.UUID(id), map[string]any{"status": status}, map[string]any{"status": "decided", "action": in.Action}, in.Reason)
	})
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	s.respondComplaintStaff(w, r, uuid.UUID(id), http.StatusOK)
}

func ptrUUID(u uuid.UUID) *uuid.UUID { return &u }

func (s *Server) PostConsoleComplaintsCommunicate(w http.ResponseWriter, r *http.Request, id gen.Id) {
	in, err := httpx.Decode[gen.ComplaintCommunicate](r)
	if err != nil || len(strings.TrimSpace(in.Message)) < 3 {
		httpx.Fail(w, r, httpx.Invalid("message is required"))
		return
	}
	if in.To != "client" && in.To != "provider" {
		httpx.Fail(w, r, httpx.Invalid("to must be client or provider"))
		return
	}
	msg := strings.TrimSpace(in.Message)
	wantSMS := false
	if in.Channels != nil {
		for _, c := range *in.Channels {
			if c == "sms" {
				wantSMS = true
			}
		}
	}
	ctx := r.Context()
	var phone string
	err = dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		var status, ref string
		var client, provider *uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT status::text, client_id, provider_id, reference FROM complaints WHERE id = $1 FOR UPDATE`, uuid.UUID(id)).Scan(&status, &client, &provider, &ref); err != nil {
			return err
		}
		if status != "decided" && status != "closed" {
			return httpx.Conflict("Tell people the outcome once it is decided (the case is " + status + ")")
		}
		target, col := client, "client_informed_at"
		if in.To == "provider" {
			target, col = provider, "provider_informed_at"
		}
		if target == nil {
			return httpx.Conflict("This case has no " + string(in.To))
		}
		if _, err := tx.Exec(ctx, `UPDATE complaints SET `+col+` = now() WHERE id = $1`, uuid.UUID(id)); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO complaint_events (complaint_id, actor_id, event_type, body, is_internal) VALUES ($1,$2,$3,$4,false)`,
			uuid.UUID(id), actorID(ctx), "message_"+string(in.To), msg); err != nil {
			return err
		}
		if err := notify(ctx, tx, *target, "complaint.outcome", "Outcome of case "+ref, msg, map[string]any{"complaint_id": id}); err != nil {
			return err
		}
		if wantSMS {
			_ = tx.QueryRow(ctx, `SELECT COALESCE(phone_e164,'') FROM users WHERE id = $1`, *target).Scan(&phone)
		}
		return audit(ctx, tx, "complaint.communicate", "complaint", uuid.UUID(id), nil, map[string]any{"to": in.To}, "")
	})
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	if phone != "" {
		_ = s.SMS.Send(ctx, phone, "Homend Residence: "+msg)
	}
	s.respondComplaintStaff(w, r, uuid.UUID(id), http.StatusOK)
}

func (s *Server) PostConsoleComplaintsClose(w http.ResponseWriter, r *http.Request, id gen.Id) {
	ctx := r.Context()
	err := dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		var status string
		var client, provider *uuid.UUID
		var clientTold, providerTold *time.Time
		var interim bool
		if err := tx.QueryRow(ctx, `SELECT status::text, client_id, provider_id, client_informed_at, provider_informed_at, interim_suspension FROM complaints WHERE id = $1 FOR UPDATE`, uuid.UUID(id)).
			Scan(&status, &client, &provider, &clientTold, &providerTold, &interim); err != nil {
			return err
		}
		if status != "decided" {
			return httpx.Conflict("Only a decided case can be closed (the case is " + status + ")")
		}
		if (client != nil && clientTold == nil) || (provider != nil && providerTold == nil) {
			return httpx.RuleViolation("Tell both the client and the provider the outcome before closing")
		}
		if _, err := tx.Exec(ctx, `UPDATE complaints SET status = 'closed', closed_at = now() WHERE id = $1`, uuid.UUID(id)); err != nil {
			return err
		}
		// An interim suspension ends with the case unless the decision itself keeps the provider out.
		if interim && provider != nil {
			var kept int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM disciplinary_actions WHERE complaint_id = $1 AND kind IN ('suspension','retraining','disqualification') AND status IN ('in_effect','pending_authorisation')`,
				uuid.UUID(id)).Scan(&kept); err != nil {
				return err
			}
			if kept == 0 {
				blockers, err := s.reinstateBlockers(ctx, tx, *provider)
				if err != nil {
					return err
				}
				if len(blockers) == 0 {
					if err := s.liftSuspension(ctx, tx, *provider); err != nil {
						return err
					}
				}
			}
		}
		return audit(ctx, tx, "complaint.close", "complaint", uuid.UUID(id), map[string]any{"status": status}, map[string]any{"status": "closed"}, "")
	})
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	s.respondComplaintStaff(w, r, uuid.UUID(id), http.StatusOK)
}

func (s *Server) GetConsoleProvidersDisciplinaryActions(w http.ResponseWriter, r *http.Request, id gen.Id) {
	out, err := rawAll(r.Context(), s.DB, disciplinaryJSON+` WHERE d.provider_id = $1 ORDER BY d.created_at DESC`, uuid.UUID(id))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (s *Server) PostConsoleProvidersDisciplinaryActions(w http.ResponseWriter, r *http.Request, id gen.Id) {
	in, err := httpx.Decode[gen.DisciplinaryCreate](r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	ctx := r.Context()
	var aid uuid.UUID
	err = dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) (err error) {
		aid, err = s.applyDiscipline(ctx, tx, uuid.UUID(id), uuidPtr(in.ComplaintId), string(in.Kind), in.Reason, in.EffectiveTo, uuidPtr(in.RetrainingCourseId))
		return err
	})
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	raw, err := rawOne(ctx, s.DB, disciplinaryJSON+` WHERE d.id = $1`, aid)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, raw)
}

// PostConsoleDisciplinaryActionsAuthorise is the second officer's sign-off on a disqualification.
func (s *Server) PostConsoleDisciplinaryActionsAuthorise(w http.ResponseWriter, r *http.Request, id gen.Id) {
	ctx := r.Context()
	me := auth.MustPrincipal(ctx).UserID
	err := dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		var kind, status, reason string
		var provider, decidedBy uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT kind::text, status::text, provider_id, decided_by, reason FROM disciplinary_actions WHERE id = $1 FOR UPDATE`, uuid.UUID(id)).
			Scan(&kind, &status, &provider, &decidedBy, &reason); err != nil {
			return err
		}
		if kind != "disqualification" || status != "pending_authorisation" {
			return httpx.Conflict("Only a pending disqualification needs authorising")
		}
		if decidedBy == me {
			return httpx.Conflict("A second officer must authorise a disqualification")
		}
		if _, err := tx.Exec(ctx, `UPDATE disciplinary_actions SET status = 'in_effect', second_authoriser_id = $2, authorised_at = now() WHERE id = $1`, uuid.UUID(id), me); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE providers SET status = 'disqualified', suspended_reason = $2 WHERE user_id = $1`, provider, reason); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE provider_categories SET status = 'revoked' WHERE provider_id = $1 AND status <> 'revoked'`, provider); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE sessions SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL`, provider); err != nil {
			return err
		}
		// Work already booked goes back to triage for a replacement.
		rows, err := tx.Query(ctx, `UPDATE assignments SET status = 'withdrawn', ended_at = now(), ended_reason = 'Provider disqualified'
			WHERE provider_id = $1 AND status IN ('offered','accepted') RETURNING request_id`, provider)
		if err != nil {
			return err
		}
		var reqs []uuid.UUID
		for rows.Next() {
			var rid uuid.UUID
			if err := rows.Scan(&rid); err != nil {
				rows.Close()
				return err
			}
			reqs = append(reqs, rid)
		}
		rows.Close()
		for _, rid := range reqs {
			if _, err := tx.Exec(ctx, `UPDATE service_requests SET status = 'triaging', confirmed_at = NULL WHERE id = $1 AND status IN ('awaiting_provider','confirmed','in_progress')`, rid); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `UPDATE visits SET status = 'cancelled' WHERE request_id = $1 AND provider_id = $2 AND status = 'scheduled'`, rid, provider); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO job_exceptions (kind, request_id, provider_id, details) VALUES ('other',$1,$2,'{"reason":"provider_disqualified"}')`, rid, provider); err != nil {
				return err
			}
		}
		var complaint *uuid.UUID
		_ = tx.QueryRow(ctx, `SELECT complaint_id FROM disciplinary_actions WHERE id = $1`, uuid.UUID(id)).Scan(&complaint)
		if complaint != nil {
			if _, err := tx.Exec(ctx, `INSERT INTO complaint_events (complaint_id, actor_id, event_type, body, is_internal) VALUES ($1,$2,'notice','Written notice of disqualification issued to the provider',true)`, *complaint, me); err != nil {
				return err
			}
		}
		if err := notify(ctx, tx, provider, "provider.disqualified", "Your account has been closed", "Following a review of a complaint, your Homend Residence account has been closed. "+reason, nil); err != nil {
			return err
		}
		return audit(ctx, tx, "disciplinary.authorise", "disciplinary_action", uuid.UUID(id), map[string]any{"status": status}, map[string]any{"status": "in_effect"}, reason)
	})
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	raw, err := rawOne(ctx, s.DB, disciplinaryJSON+` WHERE d.id = $1`, uuid.UUID(id))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, raw)
}

func (s *Server) PostConsoleDisciplinaryActionsRevoke(w http.ResponseWriter, r *http.Request, id gen.Id) {
	reason, err := reasonOf(r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	ctx := r.Context()
	err = dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		var kind, status string
		var provider uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT kind::text, status::text, provider_id FROM disciplinary_actions WHERE id = $1 FOR UPDATE`, uuid.UUID(id)).Scan(&kind, &status, &provider); err != nil {
			return err
		}
		if status != "pending_authorisation" && status != "in_effect" {
			return httpx.Conflict("An action that is " + status + " cannot be revoked")
		}
		if _, err := tx.Exec(ctx, `UPDATE disciplinary_actions SET status = 'revoked' WHERE id = $1`, uuid.UUID(id)); err != nil {
			return err
		}
		var pstatus string
		if err := tx.QueryRow(ctx, `SELECT status::text FROM providers WHERE user_id = $1 FOR UPDATE`, provider).Scan(&pstatus); err != nil {
			return err
		}
		// A revoked disqualification leaves the provider suspended: reinstating them is a separate, checked step.
		if pstatus == "disqualified" && kind == "disqualification" {
			if _, err := tx.Exec(ctx, `UPDATE providers SET status = 'suspended' WHERE user_id = $1`, provider); err != nil {
				return err
			}
		} else if pstatus == "suspended" && kind != "warning" && kind != "no_action" {
			blockers, err := s.reinstateBlockers(ctx, tx, provider)
			if err != nil {
				return err
			}
			if len(blockers) == 0 {
				if err := s.liftSuspension(ctx, tx, provider); err != nil {
					return err
				}
			}
		}
		return audit(ctx, tx, "disciplinary.revoke", "disciplinary_action", uuid.UUID(id), map[string]any{"status": status}, map[string]any{"status": "revoked"}, reason)
	})
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	raw, err := rawOne(ctx, s.DB, disciplinaryJSON+` WHERE d.id = $1`, uuid.UUID(id))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, raw)
}

// ---- spot checks

const spotCols = `sc.id, sc.provider_id, sc.visit_id, sc.supervisor_id, sc.checked_at, sc.method, sc.scores, sc.total_score, sc.notes, sc.complaint_id`

func (s *Server) GetConsoleSpotChecks(w http.ResponseWriter, r *http.Request, params gen.GetConsoleSpotChecksParams) {
	cur, err := decodeCursor(params.Cursor)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	b := &qb{}
	if params.ProviderId != nil {
		b.add("sc.provider_id = ?", uuid.UUID(*params.ProviderId))
	}
	if params.From != nil {
		b.add("sc.checked_at >= ?", params.From.Time)
	}
	if params.To != nil {
		b.add("sc.checked_at < ?", params.To.Time.AddDate(0, 0, 1))
	}
	pg, err := pagedRaw(r.Context(), s.DB, `SELECT `+spotCols+`, sc.checked_at AS _t, sc.id AS _id FROM spot_checks sc WHERE `+b.where(), b.args, cur, pageLimit(params.Limit))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, pg)
}

func (s *Server) PostConsoleSpotChecks(w http.ResponseWriter, r *http.Request) {
	in, err := httpx.Decode[gen.SpotCheckCreate](r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if in.TotalScore < 0 || in.TotalScore > 100 || len(in.Scores) == 0 {
		httpx.Fail(w, r, httpx.Invalid("Give criterion scores and a total between 0 and 100"))
		return
	}
	ctx := r.Context()
	me := auth.MustPrincipal(ctx).UserID
	var id uuid.UUID
	err = dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM providers WHERE user_id = $1`, uuid.UUID(in.ProviderId)).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			return httpx.Invalid("Unknown provider")
		}
		var reqID *uuid.UUID
		if in.VisitId != nil {
			var r uuid.UUID
			if err := tx.QueryRow(ctx, `SELECT request_id FROM visits WHERE id = $1 AND provider_id = $2`, uuid.UUID(*in.VisitId), uuid.UUID(in.ProviderId)).Scan(&r); err != nil {
				return httpx.Invalid("visit_id is not one of this provider's visits")
			}
			reqID = &r
		}
		var complaint *uuid.UUID
		if in.RaiseComplaint != nil && *in.RaiseComplaint {
			cid, err := s.openComplaint(ctx, tx, newComplaint{source: "supervisor", severity: "quality", providerID: ptrUUID(uuid.UUID(in.ProviderId)), requestID: reqID, visitID: uuidPtr(in.VisitId),
				summary: "Spot check scored " + trimFloat(float64(in.TotalScore)) + " out of 100", raisedBy: me})
			if err != nil {
				return err
			}
			complaint = &cid
		}
		if err := tx.QueryRow(ctx, `INSERT INTO spot_checks (provider_id, visit_id, supervisor_id, method, scores, total_score, notes, complaint_id)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id`, uuid.UUID(in.ProviderId), uuidPtr(in.VisitId), me, string(in.Method), mustJSON(in.Scores), in.TotalScore, trimPtr(in.Notes), complaint).Scan(&id); err != nil {
			return err
		}
		if err := s.RecomputeAppraisal(ctx, tx, uuid.UUID(in.ProviderId)); err != nil {
			return err
		}
		return audit(ctx, tx, "spot_check.create", "spot_check", id, nil, map[string]any{"provider_id": in.ProviderId, "total_score": in.TotalScore}, "")
	})
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	raw, err := rawOne(ctx, s.DB, `SELECT to_jsonb(x) FROM (SELECT `+spotCols+` FROM spot_checks sc WHERE sc.id = $1) x`, id)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, raw)
}

func trimFloat(f float64) string {
	s := strings.TrimRight(strings.TrimRight(fmtFloat(f), "0"), ".")
	if s == "" {
		return "0"
	}
	return s
}

func fmtFloat(f float64) string { return strconv.FormatFloat(f, 'f', 2, 64) }
