package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/KING-CYBERTON/home-residency/backend/internal/api/gen"
	"github.com/KING-CYBERTON/home-residency/backend/internal/auth"
	dbpkg "github.com/KING-CYBERTON/home-residency/backend/internal/db"
	"github.com/KING-CYBERTON/home-residency/backend/internal/httpx"
)

// A complaint runs open → investigating → awaiting_provider_account → awaiting_decision → decided → closed.
// The investigator and the decider are different people, the provider is heard before any decision, and
// both sides are told the outcome before a case can close (report §5.4).

// complaintCols are the columns every view of a complaint shares. Flat names so the paged helpers
// can wrap them; client and provider are nested summaries.
const complaintCols = `c.id, c.reference, c.source, c.severity, c.status, c.request_id, c.summary, c.created_at, c.closed_at`

const complaintStaffCols = complaintCols + `, c.outcome_summary, c.visit_id, c.rating_id, c.investigator_id, c.provider_heard_at, c.investigation_report,
	c.decided_by, c.decided_at, c.interim_suspension, c.client_informed_at, c.provider_informed_at,
	(SELECT jsonb_build_object('id', u.id, 'full_name', u.full_name, 'phone', u.phone_e164, 'status', u.status, 'id_verification', cl.id_verification, 'created_at', u.created_at)
	   FROM users u JOIN clients cl ON cl.user_id = u.id WHERE u.id = c.client_id) AS client,
	(SELECT jsonb_build_object('id', p.user_id, 'full_name', u.full_name, 'status', p.status, 'phone', u.phone_e164, 'rating_avg', p.rating_avg,
	   'appraisal_score', p.appraisal_score, 'completed_jobs', p.completed_jobs, 'home_service_area_id', p.home_service_area_id)
	   FROM providers p JOIN users u ON u.id = p.user_id WHERE p.user_id = c.provider_id) AS provider`

const complaintEventsCol = `COALESCE((SELECT jsonb_agg(jsonb_build_object('id', e.id, 'actor_id', e.actor_id, 'event_type', e.event_type, 'body', e.body,
	  'document_id', e.document_id, 'is_internal', e.is_internal, 'created_at', e.created_at) ORDER BY e.created_at)
	FROM complaint_events e WHERE e.complaint_id = c.id), '[]'::jsonb) AS events`

const complaintActionsCol = `COALESCE((SELECT jsonb_agg(` + disciplinaryObj + ` ORDER BY d.created_at) FROM disciplinary_actions d WHERE d.complaint_id = c.id), '[]'::jsonb) AS actions`

// The complainant only sees the outcome once they have been told it.
const complaintClientCols = complaintCols + `, CASE WHEN c.client_informed_at IS NOT NULL THEN c.outcome_summary END AS outcome_summary`
const complaintProviderCols = complaintCols + `, CASE WHEN c.provider_informed_at IS NOT NULL THEN c.outcome_summary END AS outcome_summary`

const providerVisible = `(c.status IN ('awaiting_provider_account','awaiting_decision','decided','closed') OR c.provider_heard_at IS NOT NULL)`

// complaintStaff loads the full case, with the sealed fields decrypted for staff.
func (s *Server) complaintStaff(ctx context.Context, q db, id uuid.UUID) (map[string]any, error) {
	raw, err := rawOne(ctx, q, `SELECT to_jsonb(x) FROM (SELECT `+complaintStaffCols+`, `+complaintEventsCol+`, `+complaintActionsCol+` FROM complaints c WHERE c.id = $1) x`, id)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	var details, account []byte
	if err := q.QueryRow(ctx, `SELECT details_enc, provider_account_enc FROM complaints WHERE id = $1`, id).Scan(&details, &account); err != nil {
		return nil, err
	}
	if len(details) > 0 {
		if pt, err := s.Cipher.Decrypt(details); err == nil {
			m["details"] = string(pt)
		}
	}
	if len(account) > 0 {
		if pt, err := s.Cipher.Decrypt(account); err == nil {
			m["provider_account"] = string(pt)
		}
	}
	return m, nil
}

func (s *Server) respondComplaintStaff(w http.ResponseWriter, r *http.Request, id uuid.UUID, status int) {
	m, err := s.complaintStaff(r.Context(), s.DB, id)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, status, m)
}

type newComplaint struct {
	source     string
	severity   string
	clientID   *uuid.UUID
	providerID *uuid.UUID
	requestID  *uuid.UUID
	visitID    *uuid.UUID
	summary    string
	details    string
	evidence   []uuid.UUID
	raisedBy   uuid.UUID
}

// openComplaint records a case. Safeguarding and loss complaints take the provider out of circulation
// at once; management can lift that when the case allows.
func (s *Server) openComplaint(ctx context.Context, tx pgx.Tx, in newComplaint) (uuid.UUID, error) {
	summary := strings.TrimSpace(in.summary)
	if len(summary) < 5 || len(summary) > 500 {
		return uuid.Nil, httpx.Invalid("summary must be 5 to 500 characters")
	}
	if in.requestID != nil {
		var client uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT client_id FROM service_requests WHERE id = $1`, *in.requestID).Scan(&client); err != nil {
			return uuid.Nil, httpx.Invalid("Unknown request_id")
		}
		if in.clientID == nil {
			in.clientID = &client
		}
		if in.providerID == nil {
			var p uuid.UUID
			if tx.QueryRow(ctx, `SELECT provider_id FROM assignments WHERE request_id = $1 AND status IN ('accepted','completed') ORDER BY created_at DESC LIMIT 1`, *in.requestID).Scan(&p) == nil {
				in.providerID = &p
			}
		}
	}
	if in.visitID != nil {
		var req uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT request_id FROM visits WHERE id = $1`, *in.visitID).Scan(&req); err != nil {
			return uuid.Nil, httpx.Invalid("Unknown visit_id")
		}
		if in.requestID != nil && *in.requestID != req {
			return uuid.Nil, httpx.Invalid("visit_id does not belong to request_id")
		}
	}
	var detailsEnc []byte
	if d := strings.TrimSpace(in.details); d != "" {
		var err error
		if detailsEnc, err = s.Cipher.Encrypt([]byte(d)); err != nil {
			return uuid.Nil, err
		}
	}
	interim := in.providerID != nil && (in.severity == "safeguarding" || in.severity == "loss")
	var id uuid.UUID
	var ref string
	if err := tx.QueryRow(ctx, `
		INSERT INTO complaints (source, severity, client_id, provider_id, request_id, visit_id, summary, details_enc, raised_by, interim_suspension)
		VALUES ($1::complaint_source,$2::complaint_severity,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING id, reference`,
		in.source, in.severity, in.clientID, in.providerID, in.requestID, in.visitID, summary, detailsEnc, in.raisedBy, interim).Scan(&id, &ref); err != nil {
		return uuid.Nil, err
	}
	for _, doc := range in.evidence {
		if _, err := tx.Exec(ctx, `INSERT INTO complaint_events (complaint_id, actor_id, event_type, document_id, is_internal)
			SELECT $1, $2, 'evidence', d.id, true FROM documents d WHERE d.id = $3`, id, in.raisedBy, doc); err != nil {
			return uuid.Nil, err
		}
	}
	if interim {
		if err := s.suspendProvider(ctx, tx, *in.providerID, "Interim suspension while complaint "+ref+" is reviewed"); err != nil {
			return uuid.Nil, err
		}
	}
	if err := audit(ctx, tx, "complaint.open", "complaint", id, nil, map[string]any{"severity": in.severity, "source": in.source, "interim_suspension": interim}, ""); err != nil {
		return uuid.Nil, err
	}
	return id, emit(ctx, tx, "complaint.opened", "complaint", id, map[string]any{"severity": in.severity})
}

// ---- client

func (s *Server) PostClientComplaints(w http.ResponseWriter, r *http.Request) {
	in, err := httpx.Decode[gen.ComplaintCreate](r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	ctx := r.Context()
	cid := auth.MustPrincipal(ctx).UserID
	c := newComplaint{source: "client", severity: string(in.Severity), clientID: &cid, summary: in.Summary, raisedBy: cid,
		requestID: uuidPtr(in.RequestId), visitID: uuidPtr(in.VisitId)}
	if in.Details != nil {
		c.details = *in.Details
	}
	if in.EvidenceDocumentIds != nil {
		for _, d := range *in.EvidenceDocumentIds {
			c.evidence = append(c.evidence, uuid.UUID(d))
		}
	}
	var id uuid.UUID
	err = dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		if c.requestID != nil {
			var n int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM service_requests WHERE id = $1 AND client_id = $2`, *c.requestID, cid).Scan(&n); err != nil {
				return err
			}
			if n == 0 {
				return httpx.Invalid("request_id is not one of your bookings")
			}
		}
		for _, d := range c.evidence {
			var n int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM documents WHERE id = $1 AND owner_user_id = $2`, d, cid).Scan(&n); err != nil {
				return err
			}
			if n == 0 {
				return httpx.Invalid("evidence_document_ids must be your own uploads")
			}
		}
		id, err = s.openComplaint(ctx, tx, c)
		return err
	})
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	raw, err := rawOne(ctx, s.DB, `SELECT to_jsonb(x) FROM (SELECT `+complaintClientCols+` FROM complaints c WHERE c.id = $1) x`, id)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, raw)
}

func (s *Server) GetClientComplaints(w http.ResponseWriter, r *http.Request, params gen.GetClientComplaintsParams) {
	cur, err := decodeCursor(params.Cursor)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	pg, err := pagedRaw(r.Context(), s.DB, `SELECT `+complaintClientCols+`, c.created_at AS _t, c.id AS _id FROM complaints c WHERE c.client_id = $1`,
		[]any{auth.MustPrincipal(r.Context()).UserID}, cur, pageLimit(params.Limit))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, pg)
}

func (s *Server) GetClientComplaintsById(w http.ResponseWriter, r *http.Request, id gen.Id) {
	raw, err := rawOne(r.Context(), s.DB, `SELECT to_jsonb(x) FROM (SELECT `+complaintClientCols+` FROM complaints c WHERE c.id = $1 AND c.client_id = $2) x`,
		uuid.UUID(id), auth.MustPrincipal(r.Context()).UserID)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, raw)
}

// ---- provider

func (s *Server) GetProviderComplaints(w http.ResponseWriter, r *http.Request) {
	out, err := rawAll(r.Context(), s.DB, `SELECT to_jsonb(x) FROM (SELECT `+complaintProviderCols+` FROM complaints c WHERE c.provider_id = $1 AND `+providerVisible+`
		ORDER BY c.created_at DESC LIMIT 100) x`, auth.MustPrincipal(r.Context()).UserID)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

// recordProviderAccount stores the provider's side of the story and marks them as heard.
func (s *Server) recordProviderAccount(ctx context.Context, tx pgx.Tx, id uuid.UUID, account string, docs []uuid.UUID, byProvider bool) error {
	account = strings.TrimSpace(account)
	if len(account) < 3 || len(account) > 8000 {
		return httpx.Invalid("account must be 3 to 8000 characters")
	}
	var status string
	var provider *uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT status::text, provider_id FROM complaints WHERE id = $1 FOR UPDATE`, id).Scan(&status, &provider); err != nil {
		return err
	}
	if provider == nil {
		return httpx.Conflict("This complaint is not about a provider")
	}
	if byProvider && *provider != auth.MustPrincipal(ctx).UserID {
		return httpx.ErrNotFound
	}
	if status != "awaiting_provider_account" && status != "investigating" {
		return httpx.Conflict("The provider's account is not being collected (the case is " + status + ")")
	}
	enc, err := s.Cipher.Encrypt([]byte(account))
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE complaints SET provider_account_enc = $2, provider_heard_at = now(), status = 'investigating' WHERE id = $1`, id, enc); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO complaint_events (complaint_id, actor_id, event_type, data, is_internal) VALUES ($1,$2,'provider_account',$3,true)`,
		id, actorID(ctx), mustJSON(map[string]any{"by_provider": byProvider, "documents": len(docs)})); err != nil {
		return err
	}
	for _, d := range docs {
		var q string
		if byProvider {
			q = `INSERT INTO complaint_events (complaint_id, actor_id, event_type, document_id, is_internal)
				SELECT $1, $2, 'evidence', d.id, true FROM documents d WHERE d.id = $3 AND d.owner_user_id = $2`
		} else {
			q = `INSERT INTO complaint_events (complaint_id, actor_id, event_type, document_id, is_internal)
				SELECT $1, $2, 'evidence', d.id, true FROM documents d WHERE d.id = $3`
		}
		if _, err := tx.Exec(ctx, q, id, actorID(ctx), d); err != nil {
			return err
		}
	}
	return audit(ctx, tx, "complaint.provider_account", "complaint", id, map[string]any{"status": status}, map[string]any{"by_provider": byProvider}, "")
}

func uuids(in *[]openapi_types.UUID) []uuid.UUID {
	var out []uuid.UUID
	if in != nil {
		for _, u := range *in {
			out = append(out, uuid.UUID(u))
		}
	}
	return out
}

func (s *Server) PostProviderComplaintsAccount(w http.ResponseWriter, r *http.Request, id gen.Id) {
	in, err := httpx.Decode[gen.ProviderAccountInput](r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	docs := uuids(in.DocumentIds)
	ctx := r.Context()
	if err := dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		return s.recordProviderAccount(ctx, tx, uuid.UUID(id), in.Account, docs, true)
	}); err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	raw, err := rawOne(ctx, s.DB, `SELECT to_jsonb(x) FROM (SELECT `+complaintProviderCols+` FROM complaints c WHERE c.id = $1) x`, uuid.UUID(id))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, raw)
}
