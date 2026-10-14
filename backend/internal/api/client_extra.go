package api

import (
	"encoding/json"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/KING-CYBERTON/home-residency/backend/internal/api/gen"
	"github.com/KING-CYBERTON/home-residency/backend/internal/auth"
	dbpkg "github.com/KING-CYBERTON/home-residency/backend/internal/db"
	"github.com/KING-CYBERTON/home-residency/backend/internal/httpx"
)

// ---- choosing from a shortlist

func (s *Server) GetClientRequestsShortlist(w http.ResponseWriter, r *http.Request, id gen.Id) {
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
	rows, err := s.DB.Query(ctx, `SELECT id, rank, provider_id FROM shortlist_entries WHERE request_id = $1 AND shown_to_client ORDER BY rank`, uuid.UUID(id))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	type entry struct {
		ID       uuid.UUID
		Rank     int
		Provider uuid.UUID
	}
	var es []entry
	for rows.Next() {
		var e entry
		if err := rows.Scan(&e.ID, &e.Rank, &e.Provider); err != nil {
			rows.Close()
			httpx.Fail(w, r, err)
			return
		}
		es = append(es, e)
	}
	rows.Close()
	out := []map[string]any{}
	for _, e := range es {
		pp, err := s.loadProviderPublic(ctx, s.DB, e.Provider)
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		out = append(out, map[string]any{"id": e.ID, "rank": e.Rank, "provider": pp})
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (s *Server) PostClientRequestsShortlistSelect(w http.ResponseWriter, r *http.Request, id gen.Id, entryId openapi_types.UUID) {
	ctx := r.Context()
	cid := auth.MustPrincipal(ctx).UserID
	err := dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		var status, ref string
		var officer *uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT status::text, triage_officer_id, reference FROM service_requests WHERE id = $1 AND client_id = $2 FOR UPDATE`, uuid.UUID(id), cid).
			Scan(&status, &officer, &ref); err != nil {
			return err
		}
		if status != "awaiting_client_choice" {
			return httpx.Conflict("There is no shortlist to choose from (the request is " + status + ")")
		}
		tag, err := tx.Exec(ctx, `UPDATE shortlist_entries SET client_selected = (id = $2) WHERE request_id = $1 AND shown_to_client
			AND EXISTS (SELECT 1 FROM shortlist_entries WHERE id = $2 AND request_id = $1 AND shown_to_client)`, uuid.UUID(id), uuid.UUID(entryId))
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return httpx.ErrNotFound
		}
		if officer != nil {
			if err := notify(ctx, tx, *officer, "shortlist.selected", "Client chose a provider for "+ref, "Send the offer to the provider they selected.", map[string]any{"request_id": id}); err != nil {
				return err
			}
		}
		return audit(ctx, tx, "request.shortlist_select", "service_request", uuid.UUID(id), nil, map[string]any{"entry_id": entryId}, "")
	})
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	d, _, err := s.loadRequest(ctx, s.DB, uuid.UUID(id), &cid)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, d)
}

// ---- identity verification

func (s *Server) PostClientIdVerification(w http.ResponseWriter, r *http.Request) {
	in, err := httpx.Decode[gen.IdVerificationInput](r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	number := strings.TrimSpace(in.NationalIdNumber)
	if len(number) < 6 || len(number) > 20 {
		httpx.Fail(w, r, httpx.Invalid("national_id_number looks wrong"))
		return
	}
	enc, err := s.Cipher.Encrypt([]byte(number))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	ctx := r.Context()
	cid := auth.MustPrincipal(ctx).UserID
	err = dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		var docType, docStatus string
		if err := tx.QueryRow(ctx, `SELECT document_type::text, status::text FROM documents WHERE id = $1 AND owner_user_id = $2`, uuid.UUID(in.DocumentId), cid).Scan(&docType, &docStatus); err != nil {
			return httpx.Invalid("document_id is not one of your documents")
		}
		if docType != "national_id" && docType != "passport" {
			return httpx.Invalid("Upload your national ID or passport first")
		}
		if docStatus != "uploaded" && docStatus != "verified" {
			return httpx.Invalid("Finish uploading the document first")
		}
		var cur string
		if err := tx.QueryRow(ctx, `SELECT id_verification::text FROM clients WHERE user_id = $1 FOR UPDATE`, cid).Scan(&cur); err != nil {
			return err
		}
		if cur == "verified" {
			return httpx.Conflict("Your identity is already verified")
		}
		if _, err := tx.Exec(ctx, `UPDATE clients SET national_id_enc = $2, id_verification = 'pending', id_verified_by = NULL, id_verified_at = NULL WHERE user_id = $1`, cid, enc); err != nil {
			return err
		}
		return audit(ctx, tx, "client.id_submit", "client", cid, map[string]any{"status": cur}, map[string]any{"status": "pending", "document_id": in.DocumentId}, "")
	})
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	httpx.JSON(w, http.StatusAccepted, map[string]string{"status": "pending"})
}

// ---- console: households

const clientSummaryCols = `u.id, u.full_name, u.phone_e164 AS phone, u.status, c.id_verification, u.created_at`

func (s *Server) GetConsoleClients(w http.ResponseWriter, r *http.Request, params gen.GetConsoleClientsParams) {
	cur, err := decodeCursor(params.Cursor)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	b := &qb{}
	if params.Status != nil {
		b.add("u.status = ?::account_status", string(*params.Status))
	}
	if params.IdVerification != nil {
		b.add("c.id_verification = ?::id_verification_status", string(*params.IdVerification))
	}
	if params.Q != nil && strings.TrimSpace(*params.Q) != "" {
		b.add("(u.full_name ILIKE ? OR u.phone_e164 LIKE ? OR u.email::text ILIKE ?)", "%"+strings.TrimSpace(*params.Q)+"%")
	}
	pg, err := pagedRaw(r.Context(), s.DB, `SELECT `+clientSummaryCols+`, u.created_at AS _t, u.id AS _id FROM users u JOIN clients c ON c.user_id = u.id WHERE `+b.where(),
		b.args, cur, pageLimit(params.Limit))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, pg)
}

func (s *Server) clientStaff(r *http.Request, id uuid.UUID) (map[string]any, error) {
	ctx := r.Context()
	raw, err := rawOne(ctx, s.DB, `SELECT jsonb_build_object('id', u.id, 'full_name', u.full_name, 'phone', u.phone_e164, 'status', u.status, 'id_verification', c.id_verification,
		'created_at', u.created_at, 'email', u.email,
		'request_count', (SELECT count(*) FROM service_requests WHERE client_id = u.id),
		'complaint_count', (SELECT count(*) FROM complaints WHERE client_id = u.id),
		'addresses', COALESCE((SELECT jsonb_agg(jsonb_build_object('id', a.id, 'label', a.label, 'service_area_id', a.service_area_id, 'estate', a.estate, 'street', a.street,
		    'house_number', a.house_number, 'landmark', a.landmark, 'lat', a.lat, 'lng', a.lng, 'is_default', a.is_default) ORDER BY a.is_default DESC, a.created_at)
		  FROM addresses a WHERE a.client_id = u.id AND a.archived_at IS NULL), '[]'::jsonb))
		FROM users u JOIN clients c ON c.user_id = u.id WHERE u.id = $1`, id)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	if sub, err := rawOne(ctx, s.DB, subscriptionJSON+` WHERE s.client_id = $1 AND s.status IN ('active','past_due') ORDER BY s.created_at DESC LIMIT 1`, id); err == nil {
		var sm map[string]any
		_ = json.Unmarshal(sub, &sm)
		m["subscription"] = sm
	}
	var enc []byte
	if s.DB.QueryRow(ctx, `SELECT household_notes_enc FROM clients WHERE user_id = $1`, id).Scan(&enc) == nil && len(enc) > 0 {
		if pt, err := s.Cipher.Decrypt(enc); err == nil {
			m["household_notes"] = string(pt)
		}
	}
	return m, nil
}

func (s *Server) GetConsoleClientsById(w http.ResponseWriter, r *http.Request, id gen.Id) {
	m, err := s.clientStaff(r, uuid.UUID(id))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	_ = audit(r.Context(), s.DB, "client.view", "client", uuid.UUID(id), nil, nil, "")
	httpx.JSON(w, http.StatusOK, m)
}

func (s *Server) PostConsoleClientsIdDecision(w http.ResponseWriter, r *http.Request, id gen.Id) {
	in, err := httpx.Decode[gen.IdDecision](r)
	if err != nil || (in.Decision != "verified" && in.Decision != "rejected") {
		httpx.Fail(w, r, httpx.Invalid("decision must be verified or rejected"))
		return
	}
	reason := ""
	if in.Reason != nil {
		reason = strings.TrimSpace(*in.Reason)
	}
	if in.Decision == "rejected" && len(reason) < 3 {
		httpx.Fail(w, r, httpx.Invalid("reason is required to reject"))
		return
	}
	ctx := r.Context()
	me := auth.MustPrincipal(ctx).UserID
	err = dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		var cur string
		if err := tx.QueryRow(ctx, `SELECT id_verification::text FROM clients WHERE user_id = $1 FOR UPDATE`, uuid.UUID(id)).Scan(&cur); err != nil {
			return err
		}
		if cur != "pending" {
			return httpx.Conflict("There is no pending identity check for this household (it is " + cur + ")")
		}
		if _, err := tx.Exec(ctx, `UPDATE clients SET id_verification = $2::id_verification_status, id_verified_by = $3, id_verified_at = now() WHERE user_id = $1`,
			uuid.UUID(id), string(in.Decision), me); err != nil {
			return err
		}
		msg := "Your identity check is complete."
		if in.Decision == "rejected" {
			msg = "We could not verify your ID: " + reason
		}
		if err := notify(ctx, tx, uuid.UUID(id), "client.id_"+string(in.Decision), "Identity check", msg, nil); err != nil {
			return err
		}
		return audit(ctx, tx, "client.id_decision", "client", uuid.UUID(id), map[string]any{"status": cur}, map[string]any{"status": in.Decision}, reason)
	})
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	m, err := s.clientStaff(r, uuid.UUID(id))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, m)
}

func (s *Server) setClientStatus(w http.ResponseWriter, r *http.Request, id uuid.UUID, from, to string) {
	in, err := httpx.Decode[gen.ReasonInput](r)
	if err != nil || len(strings.TrimSpace(in.Reason)) < 3 {
		httpx.Fail(w, r, httpx.Invalid("reason is required"))
		return
	}
	ctx := r.Context()
	err = dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		var cur string
		if err := tx.QueryRow(ctx, `SELECT status::text FROM users WHERE id = $1 AND kind = 'client' FOR UPDATE`, id).Scan(&cur); err != nil {
			return err
		}
		if cur != from {
			return httpx.Conflict("This household is " + cur)
		}
		if _, err := tx.Exec(ctx, `UPDATE users SET status = $2::account_status WHERE id = $1`, id, to); err != nil {
			return err
		}
		if to == "suspended" {
			if _, err := tx.Exec(ctx, `UPDATE sessions SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL`, id); err != nil {
				return err
			}
		}
		return audit(ctx, tx, "client."+map[string]string{"suspended": "suspend", "active": "reinstate"}[to], "client", id, map[string]any{"status": cur}, map[string]any{"status": to}, in.Reason)
	})
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	m, err := s.clientStaff(r, id)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, m)
}

func (s *Server) PostConsoleClientsSuspend(w http.ResponseWriter, r *http.Request, id gen.Id) {
	s.setClientStatus(w, r, uuid.UUID(id), "active", "suspended")
}

func (s *Server) PostConsoleClientsReinstate(w http.ResponseWriter, r *http.Request, id gen.Id) {
	s.setClientStatus(w, r, uuid.UUID(id), "suspended", "active")
}
