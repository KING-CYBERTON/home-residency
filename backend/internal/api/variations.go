package api

import (
	"encoding/json"
	"math"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/KING-CYBERTON/home-residency/backend/internal/api/gen"
	"github.com/KING-CYBERTON/home-residency/backend/internal/auth"
	dbpkg "github.com/KING-CYBERTON/home-residency/backend/internal/db"
	"github.com/KING-CYBERTON/home-residency/backend/internal/httpx"
)

// A variation changes the price of a job in flight: the provider proposes it, the client approves
// it (or triage decides for them), and the invoice created at the last check-out carries it. Once
// the invoice exists the price is fixed, so a variation can only be raised or decided before then.

const variationCols = `jsonb_build_object('id', v.id, 'request_id', v.request_id, 'visit_id', v.visit_id, 'raised_by', v.raised_by, 'description', v.description,
	'quantity', v.quantity, 'client_delta_minor', v.client_delta_minor, 'provider_delta_minor', v.provider_delta_minor, 'status', v.status,
	'client_approved_at', v.client_approved_at, 'decided_at', v.decided_at, 'created_at', v.created_at)`

const variationJSON = `SELECT ` + variationCols + ` FROM variations v`

// clientVariationJSON hides what the provider earns from a change.
const clientVariationJSON = `SELECT ` + variationCols + ` - 'provider_delta_minor' FROM variations v`

const exceptionJSON = `SELECT jsonb_build_object('id', x.id, 'kind', x.kind, 'request_id', x.request_id, 'visit_id', x.visit_id, 'provider_id', x.provider_id,
	'details', x.details, 'detected_at', x.detected_at, 'reviewed_by', x.reviewed_by, 'reviewed_at', x.reviewed_at, 'resolution', x.resolution) FROM job_exceptions x`

func variationsOpen(status string) bool { return status == "confirmed" || status == "in_progress" }

func (s *Server) PostProviderJobsVariations(w http.ResponseWriter, r *http.Request, id gen.Id) {
	in, err := httpx.Decode[gen.VariationCreate](r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	desc := strings.TrimSpace(in.Description)
	if len(desc) < 3 {
		httpx.Fail(w, r, httpx.Invalid("Describe the change in at least a few words"))
		return
	}
	qty := 1.0
	if in.Quantity != nil {
		qty = float64(*in.Quantity)
		if qty <= 0 || qty > 1000 {
			httpx.Fail(w, r, httpx.Invalid("quantity must be positive"))
			return
		}
	}
	ctx := r.Context()
	pid := auth.MustPrincipal(ctx).UserID
	var vid uuid.UUID
	var clientID uuid.UUID
	var ref string
	err = dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		var reqID uuid.UUID
		var status string
		var defEntry *uuid.UUID
		if err := tx.QueryRow(ctx, `
			SELECT r.id, r.status::text, r.rate_card_entry_id, r.client_id, r.reference
			FROM assignments a JOIN service_requests r ON r.id = a.request_id
			WHERE a.id = $1 AND a.provider_id = $2 AND a.status = 'accepted' FOR UPDATE OF r`, uuid.UUID(id), pid).Scan(&reqID, &status, &defEntry, &clientID, &ref); err != nil {
			return err
		}
		if !variationsOpen(status) {
			return httpx.Conflict("A variation can only be proposed while the job is confirmed or in progress (it is " + status + ")")
		}
		entry := defEntry
		if in.RateCardEntryId != nil {
			e := uuid.UUID(*in.RateCardEntryId)
			entry = &e
		}
		if entry == nil {
			return httpx.Invalid("rate_card_entry_id is required: the job has no price to copy")
		}
		var price, pay int64
		if err := tx.QueryRow(ctx, `SELECT client_price_minor, provider_pay_minor FROM rate_card_entries WHERE id = $1`, *entry).Scan(&price, &pay); err != nil {
			return httpx.Invalid("Unknown rate card entry")
		}
		if in.VisitId != nil {
			var n int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM visits WHERE id = $1 AND request_id = $2 AND provider_id = $3`, uuid.UUID(*in.VisitId), reqID, pid).Scan(&n); err != nil {
				return err
			}
			if n == 0 {
				return httpx.Invalid("visit_id does not belong to this job")
			}
		}
		if err := tx.QueryRow(ctx, `
			INSERT INTO variations (request_id, visit_id, raised_by, raised_by_user_id, description, rate_card_entry_id, quantity, client_delta_minor, provider_delta_minor)
			VALUES ($1,$2,'provider',$3,$4,$5,$6,$7,$8) RETURNING id`,
			reqID, uuidPtr(in.VisitId), pid, desc, *entry, qty, int64(math.Round(float64(price)*qty)), int64(math.Round(float64(pay)*qty))).Scan(&vid); err != nil {
			return err
		}
		if err := notify(ctx, tx, clientID, "variation.proposed", "Change to your booking "+ref, desc, map[string]any{"variation_id": vid, "request_id": reqID}); err != nil {
			return err
		}
		return audit(ctx, tx, "variation.propose", "variation", vid, nil, in, "")
	})
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	raw, err := rawOne(ctx, s.DB, variationJSON+` WHERE v.id = $1`, vid)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, raw)
}

func (s *Server) GetClientRequestsVariations(w http.ResponseWriter, r *http.Request, id gen.Id) {
	ctx := r.Context()
	cid := auth.MustPrincipal(ctx).UserID
	var n int
	if err := s.DB.QueryRow(ctx, `SELECT count(*) FROM service_requests WHERE id = $1 AND client_id = $2`, uuid.UUID(id), cid).Scan(&n); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if n == 0 {
		httpx.Fail(w, r, httpx.ErrNotFound)
		return
	}
	out, err := rawAll(ctx, s.DB, clientVariationJSON+` WHERE v.request_id = $1 ORDER BY v.created_at`, uuid.UUID(id))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

// decideVariation moves a pending variation to approved or rejected. clientSide marks the client's own
// answer; staff may decide for the household.
func (s *Server) decideVariation(w http.ResponseWriter, r *http.Request, id uuid.UUID, approve, clientSide bool, reason string) {
	ctx := r.Context()
	me := auth.MustPrincipal(ctx).UserID
	var providerID uuid.UUID
	err := dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		var reqID, owner uuid.UUID
		var status, reqStatus, ref string
		if err := tx.QueryRow(ctx, `
			SELECT v.request_id, v.status::text, r.status::text, r.client_id, r.reference
			FROM variations v JOIN service_requests r ON r.id = v.request_id WHERE v.id = $1 FOR UPDATE OF v`, id).Scan(&reqID, &status, &reqStatus, &owner, &ref); err != nil {
			return err
		}
		if clientSide && owner != me {
			return httpx.ErrNotFound
		}
		if status != "pending" {
			return httpx.Conflict("This variation is already " + status)
		}
		if !variationsOpen(reqStatus) {
			return httpx.Conflict("The price of this job is fixed (it is " + reqStatus + ")")
		}
		newStatus := "rejected"
		if approve {
			newStatus = "approved"
		}
		if _, err := tx.Exec(ctx, `UPDATE variations SET status = $2::variation_status, decided_by = $3, decided_at = now(),
			client_approved_at = CASE WHEN $2 = 'approved' AND $4 THEN now() ELSE client_approved_at END WHERE id = $1`, id, newStatus, me, clientSide); err != nil {
			return err
		}
		_ = tx.QueryRow(ctx, `SELECT provider_id FROM assignments WHERE request_id = $1 AND status = 'accepted'`, reqID).Scan(&providerID)
		if providerID != uuid.Nil {
			if err := notify(ctx, tx, providerID, "variation."+newStatus, "Change "+newStatus+" on "+ref, reason, map[string]any{"variation_id": id, "request_id": reqID}); err != nil {
				return err
			}
		}
		return audit(ctx, tx, "variation."+newStatus, "variation", id, map[string]any{"status": status}, map[string]any{"status": newStatus}, reason)
	})
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	sel := variationJSON
	if clientSide {
		sel = clientVariationJSON
	}
	raw, err := rawOne(ctx, s.DB, sel+` WHERE v.id = $1`, id)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, raw)
}

func (s *Server) PostClientVariationsApprove(w http.ResponseWriter, r *http.Request, id gen.Id) {
	s.decideVariation(w, r, uuid.UUID(id), true, true, "")
}

func (s *Server) PostClientVariationsReject(w http.ResponseWriter, r *http.Request, id gen.Id) {
	in, err := httpx.Decode[gen.ReasonInput](r)
	if err != nil || len(strings.TrimSpace(in.Reason)) < 3 {
		httpx.Fail(w, r, httpx.Invalid("reason is required"))
		return
	}
	s.decideVariation(w, r, uuid.UUID(id), false, true, strings.TrimSpace(in.Reason))
}

func (s *Server) GetConsoleVariations(w http.ResponseWriter, r *http.Request, params gen.GetConsoleVariationsParams) {
	cur, err := decodeCursor(params.Cursor)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	status := "pending"
	if params.Status != nil {
		status = string(*params.Status)
	}
	pg, err := pagedRawDir(r.Context(), s.DB, `SELECT v.id, v.request_id, v.visit_id, v.raised_by, v.description, v.quantity, v.client_delta_minor, v.provider_delta_minor, v.status,
		v.client_approved_at, v.decided_at, v.created_at, v.created_at AS _t, v.id AS _id FROM variations v WHERE v.status = $1::variation_status`,
		[]any{status}, cur, pageLimit(params.Limit), true)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, pg)
}

func (s *Server) PostConsoleVariationsDecision(w http.ResponseWriter, r *http.Request, id gen.Id) {
	in, err := httpx.Decode[struct {
		Decision string `json:"decision"`
		Reason   string `json:"reason"`
	}](r)
	if err != nil || (in.Decision != "approve" && in.Decision != "reject") {
		httpx.Fail(w, r, httpx.Invalid("decision must be approve or reject"))
		return
	}
	if in.Decision == "reject" && len(strings.TrimSpace(in.Reason)) < 3 {
		httpx.Fail(w, r, httpx.Invalid("reason is required to reject"))
		return
	}
	s.decideVariation(w, r, uuid.UUID(id), in.Decision == "approve", false, strings.TrimSpace(in.Reason))
}

func rawList(raws []json.RawMessage) []json.RawMessage {
	if raws == nil {
		return []json.RawMessage{}
	}
	return raws
}
