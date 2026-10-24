package api

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/KING-CYBERTON/home-residency/backend/internal/api/gen"
	"github.com/KING-CYBERTON/home-residency/backend/internal/auth"
	dbpkg "github.com/KING-CYBERTON/home-residency/backend/internal/db"
	"github.com/KING-CYBERTON/home-residency/backend/internal/httpx"
	"github.com/KING-CYBERTON/home-residency/backend/internal/storage"
)

const (
	uploadTTL   = 15 * time.Minute
	downloadTTL = 5 * time.Minute
)

const documentObj = `jsonb_build_object('id', d.id, 'owner_user_id', d.owner_user_id, 'document_type', d.document_type, 'status', d.status,
	  'original_filename', d.original_filename, 'mime_type', d.mime_type, 'size_bytes', d.size_bytes, 'issued_on', d.issued_on,
	  'expires_on', d.expires_on, 'verified_by', d.verified_by, 'verified_at', d.verified_at, 'rejection_reason', d.rejection_reason,
	  'created_at', d.created_at)`

const documentJSON = `SELECT ` + documentObj + ` FROM documents d`

// vaultReaders may open any vault document (report §4: need-to-know access, every read logged).
var vaultReaders = []string{"vetting_officer", "approver", "compliance", "supervisor", "complaints_manager"}

// clientDocTypes are the only vault documents a household uploads.
var clientDocTypes = map[string]bool{"national_id": true, "passport": true, "other": true}

func (s *Server) PostDocumentsUploads(w http.ResponseWriter, r *http.Request) {
	in, err := httpx.Decode[gen.UploadRequest](r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	p := auth.MustPrincipal(r.Context())
	switch {
	case p.Kind == auth.KindStaff:
		httpx.Fail(w, r, httpx.ErrForbidden)
		return
	case in.SizeBytes <= 0 || in.SizeBytes > 10<<20:
		httpx.Fail(w, r, httpx.Invalid("size_bytes must be between 1 byte and 10 MB"))
		return
	case p.Kind == auth.KindClient && !clientDocTypes[string(in.DocumentType)]:
		httpx.Fail(w, r, httpx.Invalid("This document type is not accepted from households"))
		return
	case in.DocumentType == "signed_contract":
		httpx.Fail(w, r, httpx.Invalid("Contracts are signed in the app, not uploaded"))
		return
	}
	if in.IssuedOn != nil && in.ExpiresOn != nil && in.ExpiresOn.Before(in.IssuedOn.Time) {
		httpx.Fail(w, r, httpx.Invalid("expires_on is before issued_on"))
		return
	}
	numEnc, err := s.encryptOpt(in.DocumentNumber)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	ctx := r.Context()
	id := uuid.New()
	key := "vault/" + p.UserID.String() + "/" + id.String()
	var issued, expires *time.Time
	if in.IssuedOn != nil {
		issued = &in.IssuedOn.Time
	}
	if in.ExpiresOn != nil {
		expires = &in.ExpiresOn.Time
	}
	if _, err := s.DB.Exec(ctx, `
		INSERT INTO documents (id, owner_user_id, document_type, s3_key, original_filename, mime_type, size_bytes, document_number_enc, issued_on, expires_on)
		VALUES ($1,$2,$3::document_type,$4,$5,$6,$7,$8,$9,$10)`,
		id, p.UserID, string(in.DocumentType), key, trimPtr(in.OriginalFilename), string(in.MimeType), in.SizeBytes, numEnc, issued, expires); err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	url, headers, err := s.Store.PresignPut(ctx, key, string(in.MimeType), int64(in.SizeBytes), uploadTTL)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	doc, err := rawOne(ctx, s.DB, documentJSON+` WHERE d.id = $1`, id)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{"document": doc, "upload_url": url, "upload_method": "PUT", "upload_headers": headers,
		"expires_at": s.Now().Add(uploadTTL).UTC()})
}

// PostDocumentsComplete checks that what arrived in the bucket is what the ticket promised.
func (s *Server) PostDocumentsComplete(w http.ResponseWriter, r *http.Request, id gen.Id) {
	ctx := r.Context()
	p := auth.MustPrincipal(ctx)
	var key, mime, status string
	var size int64
	if err := s.DB.QueryRow(ctx, `SELECT s3_key, mime_type, COALESCE(size_bytes,0), status::text FROM documents WHERE id = $1 AND owner_user_id = $2`, uuid.UUID(id), p.UserID).
		Scan(&key, &mime, &size, &status); err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	if status != "pending_upload" {
		httpx.Fail(w, r, httpx.Conflict("This document was already confirmed (status: "+status+")"))
		return
	}
	gotSize, gotType, err := s.Store.Head(ctx, key)
	if errors.Is(err, storage.ErrNotFound) {
		httpx.Fail(w, r, httpx.Invalid("The file has not been uploaded yet"))
		return
	}
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if gotSize != size || (gotType != "" && gotType != mime) {
		httpx.Fail(w, r, httpx.Invalid("The uploaded file does not match the size or type declared"))
		return
	}
	if _, err := s.DB.Exec(ctx, `UPDATE documents SET status = 'uploaded' WHERE id = $1`, uuid.UUID(id)); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	raw, err := rawOne(ctx, s.DB, documentJSON+` WHERE d.id = $1`, uuid.UUID(id))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, raw)
}

// canReadDocument is the owner or a staff member whose role needs vault access.
func (s *Server) canReadDocument(ctx context.Context, id uuid.UUID) (owner uuid.UUID, p *auth.Principal, err error) {
	p = auth.MustPrincipal(ctx)
	if err = s.DB.QueryRow(ctx, `SELECT owner_user_id FROM documents WHERE id = $1 AND purged_at IS NULL`, id).Scan(&owner); err != nil {
		return owner, p, mapErr(err)
	}
	if owner == p.UserID || (p.Kind == auth.KindStaff && p.HasAnyRole(vaultReaders...)) {
		return owner, p, nil
	}
	return owner, p, httpx.ErrNotFound // do not confirm that someone else's document exists
}

func (s *Server) GetDocumentsById(w http.ResponseWriter, r *http.Request, id gen.Id) {
	if _, _, err := s.canReadDocument(r.Context(), uuid.UUID(id)); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	raw, err := rawOne(r.Context(), s.DB, documentJSON+` WHERE d.id = $1`, uuid.UUID(id))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, raw)
}

func (s *Server) GetDocumentsDownloadUrl(w http.ResponseWriter, r *http.Request, id gen.Id, params gen.GetDocumentsDownloadUrlParams) {
	ctx := r.Context()
	owner, p, err := s.canReadDocument(ctx, uuid.UUID(id))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	purpose := ""
	if params.XAccessPurpose != nil {
		purpose = *params.XAccessPurpose
		if dec, err := url.PathUnescape(purpose); err == nil {
			purpose = dec
		}
		purpose = strings.TrimSpace(purpose)
	} else if params.Purpose != nil {
		purpose = strings.TrimSpace(*params.Purpose)
	}
	if p.Kind == auth.KindStaff && len(purpose) < 3 {
		httpx.Fail(w, r, httpx.Invalid("purpose is required for staff and is written to the access log"))
		return
	}
	if purpose == "" {
		purpose = "owner download"
	}
	var key, status string
	if err := s.DB.QueryRow(ctx, `SELECT s3_key, status::text FROM documents WHERE id = $1`, uuid.UUID(id)).Scan(&key, &status); err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	if status == "pending_upload" {
		httpx.Fail(w, r, httpx.Conflict("This document has no file yet"))
		return
	}
	var ip any
	if a := clientIP(r); a != "" {
		ip = a
	}
	// Log first: a link is never issued without a record of who asked and why.
	if _, err := s.DB.Exec(ctx, `INSERT INTO document_access_log (document_id, accessed_by, purpose, ip_address) VALUES ($1,$2,$3,$4::inet)`, uuid.UUID(id), p.UserID, purpose, ip); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	link, err := s.Store.PresignGet(ctx, key, downloadTTL)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	_ = owner
	httpx.JSON(w, http.StatusOK, map[string]any{"url": link, "expires_at": s.Now().Add(downloadTTL).UTC()})
}

// ---- staff review of documents

func (s *Server) PostConsoleDocumentsVerify(w http.ResponseWriter, r *http.Request, id gen.Id) {
	in, err := httpx.Decode[gen.DocumentVerify](r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	ctx := r.Context()
	err = dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		var status, docType string
		var owner uuid.UUID
		var issued, expires *time.Time
		if err := tx.QueryRow(ctx, `SELECT status::text, document_type::text, owner_user_id, issued_on, expires_on FROM documents WHERE id = $1 FOR UPDATE`, uuid.UUID(id)).
			Scan(&status, &docType, &owner, &issued, &expires); err != nil {
			return err
		}
		if status != "uploaded" && status != "rejected" {
			return httpx.Conflict("Only an uploaded document can be verified (status: " + status + ")")
		}
		if in.IssuedOn != nil {
			issued = &in.IssuedOn.Time
		}
		if in.ExpiresOn != nil {
			expires = &in.ExpiresOn.Time
		}
		if expires == nil && issued != nil {
			// The checklist says how long this kind of document stays valid.
			var months *int
			_ = tx.QueryRow(ctx, `
				SELECT max(i.validity_months) FROM checklist_template_items i JOIN checklist_templates t ON t.id = i.template_id AND t.is_current
				WHERE i.document_type = $1::document_type AND i.validity_months IS NOT NULL`, docType).Scan(&months)
			if months != nil {
				e := issued.AddDate(0, *months, 0)
				expires = &e
			}
		}
		if issued != nil && expires != nil && expires.Before(*issued) {
			return httpx.Invalid("expires_on is before issued_on")
		}
		if expires != nil && expires.Before(s.Now()) {
			return httpx.RuleViolation("This document has already expired and cannot be verified")
		}
		if _, err := tx.Exec(ctx, `UPDATE documents SET status = 'verified', verified_by = $2, verified_at = now(), rejection_reason = NULL, issued_on = $3, expires_on = $4 WHERE id = $1`,
			uuid.UUID(id), *actorID(ctx), issued, expires); err != nil {
			return err
		}
		if err := notify(ctx, tx, owner, "document.verified", "Document verified", "Your "+strings.ReplaceAll(docType, "_", " ")+" was verified.", map[string]any{"document_id": id}); err != nil {
			return err
		}
		return audit(ctx, tx, "document.verify", "document", uuid.UUID(id), map[string]any{"status": status}, map[string]any{"status": "verified", "expires_on": expires}, deref(in.Notes))
	})
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	raw, err := rawOne(ctx, s.DB, documentJSON+` WHERE d.id = $1`, uuid.UUID(id))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, raw)
}

func (s *Server) PostConsoleDocumentsReject(w http.ResponseWriter, r *http.Request, id gen.Id) {
	reason, err := reasonOf(r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	ctx := r.Context()
	err = dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		var status, docType string
		var owner uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT status::text, document_type::text, owner_user_id FROM documents WHERE id = $1 FOR UPDATE`, uuid.UUID(id)).Scan(&status, &docType, &owner); err != nil {
			return err
		}
		if status != "uploaded" && status != "verified" {
			return httpx.Conflict("Only an uploaded or verified document can be rejected (status: " + status + ")")
		}
		if _, err := tx.Exec(ctx, `UPDATE documents SET status = 'rejected', rejection_reason = $2, verified_by = NULL, verified_at = NULL WHERE id = $1`, uuid.UUID(id), reason); err != nil {
			return err
		}
		// A checklist item that relied on it goes back to the provider.
		if _, err := tx.Exec(ctx, `UPDATE application_items SET status = 'pending', document_id = NULL, notes = $2, reviewed_by = NULL, reviewed_at = NULL
			WHERE document_id = $1 AND status <> 'failed'`, uuid.UUID(id), "Document rejected: "+reason); err != nil {
			return err
		}
		if err := notify(ctx, tx, owner, "document.rejected", "Document rejected", "Your "+strings.ReplaceAll(docType, "_", " ")+" was rejected: "+reason, map[string]any{"document_id": id}); err != nil {
			return err
		}
		return audit(ctx, tx, "document.reject", "document", uuid.UUID(id), map[string]any{"status": status}, map[string]any{"status": "rejected"}, reason)
	})
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	raw, err := rawOne(ctx, s.DB, documentJSON+` WHERE d.id = $1`, uuid.UUID(id))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, raw)
}

func (s *Server) GetConsoleDocumentsAccessLog(w http.ResponseWriter, r *http.Request, id gen.Id, params gen.GetConsoleDocumentsAccessLogParams) {
	cur, err := decodeCursor(params.Cursor)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	pg, err := pagedRaw(r.Context(), s.DB, `SELECT l.id, l.document_id, l.accessed_by, u.full_name AS accessed_by_name, l.purpose, host(l.ip_address) AS ip_address, l.accessed_at,
		l.accessed_at AS _t, `+strings.Replace(bigUUID, "%s", "l.id", 1)+` AS _id FROM document_access_log l LEFT JOIN users u ON u.id = l.accessed_by WHERE l.document_id = $1`, []any{uuid.UUID(id)}, cur, pageLimit(params.Limit))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, pg)
}

// GetConsoleComplianceExpiringDocuments lists mandatory documents of active providers that have lapsed
// or lapse soon, soonest first.
func (s *Server) GetConsoleComplianceExpiringDocuments(w http.ResponseWriter, r *http.Request, params gen.GetConsoleComplianceExpiringDocumentsParams) {
	cur, err := decodeCursor(params.Cursor)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	days := int(s.setting(r.Context(), s.DB, "document.expiry_warning_days", 30))
	if params.WithinDays != nil && *params.WithinDays > 0 {
		days = *params.WithinDays
	}
	pg, err := pagedRawDir(r.Context(), s.DB, `
		SELECT `+documentObj+` AS document,
		  jsonb_build_object('id', p.user_id, 'full_name', u.full_name, 'status', p.status, 'phone', u.phone_e164, 'rating_avg', p.rating_avg,
		    'appraisal_score', p.appraisal_score, 'completed_jobs', p.completed_jobs, 'home_service_area_id', p.home_service_area_id) AS provider,
		  (d.expires_on - current_date) AS days_left, d.expires_on::timestamptz AS _t, d.id AS _id
		FROM documents d JOIN providers p ON p.user_id = d.owner_user_id JOIN users u ON u.id = p.user_id
		WHERE d.status = 'verified' AND d.expires_on IS NOT NULL AND d.expires_on <= current_date + $1::int
		  AND p.status IN ('active','approved')
		  AND d.document_type IN (SELECT i.document_type FROM checklist_template_items i JOIN checklist_templates t ON t.id = i.template_id AND t.is_current
		        JOIN provider_categories pc ON pc.category_id = t.category_id AND pc.provider_id = p.user_id AND pc.status IN ('approved','active')
		        WHERE i.is_mandatory AND i.kind = 'document')`, []any{days}, cur, pageLimit(params.Limit), true)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, pg)
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
