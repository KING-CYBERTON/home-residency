package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/KING-CYBERTON/home-residency/backend/internal/api/gen"
	"github.com/KING-CYBERTON/home-residency/backend/internal/auth"
	dbpkg "github.com/KING-CYBERTON/home-residency/backend/internal/db"
	"github.com/KING-CYBERTON/home-residency/backend/internal/httpx"
)

func (s *Server) GetConsoleApplications(w http.ResponseWriter, r *http.Request, params gen.GetConsoleApplicationsParams) {
	cur, err := decodeCursor(params.Cursor)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	b := &qb{}
	if params.Status != nil {
		b.add("a.status = ?::application_status", string(*params.Status))
	} else {
		b.raw("a.status <> 'draft'")
	}
	if params.CategoryId != nil {
		b.add("a.category_id = ?", uuid.UUID(*params.CategoryId))
	}
	if params.VettingOfficerId != nil {
		b.add("a.vetting_officer_id = ?", uuid.UUID(*params.VettingOfficerId))
	}
	if params.Reverification != nil {
		b.add("a.is_reverification = ?", *params.Reverification)
	}
	if params.Q != nil && strings.TrimSpace(*params.Q) != "" {
		b.add("(u.full_name ILIKE ? OR a.reference ILIKE ?)", "%"+strings.TrimSpace(*params.Q)+"%")
	}
	// Oldest first: a work queue is worked from the front.
	pg, err := pagedRawDir(r.Context(), s.DB, `SELECT `+applicationSummaryObj+applicationOfficerObj+` AS summary, COALESCE(a.submitted_at, a.created_at) AS _t, a.id AS _id`+
		applicationSummaryFrom+` WHERE `+b.where(), b.args, cur, pageLimit(params.Limit), true)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	// The page rows carry the summary under one key; flatten them to the ApplicationSummary shape.
	flat, err := flattenSummary(pg)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, flat)
}

func (s *Server) GetConsoleApplicationsById(w http.ResponseWriter, r *http.Request, id gen.Id) {
	s.respondApplication(w, r, uuid.UUID(id), true, http.StatusOK)
}

// appState locks an application and returns what the state checks need.
type appState struct {
	status   string
	officer  *uuid.UUID
	provider uuid.UUID
	category uuid.UUID
	reverify bool
}

func lockApplication(ctx context.Context, tx pgx.Tx, id uuid.UUID) (appState, error) {
	var a appState
	err := tx.QueryRow(ctx, `SELECT status::text, vetting_officer_id, provider_id, category_id, is_reverification FROM applications WHERE id = $1 FOR UPDATE`, id).
		Scan(&a.status, &a.officer, &a.provider, &a.category, &a.reverify)
	return a, mapErr(err)
}

// requireOfficer: only the officer who claimed the application works it (admins may step in on item reviews).
func requireOfficer(a appState, me uuid.UUID, admin bool) error {
	if a.status != "in_vetting" {
		return httpx.Conflict("This application is not being vetted (status: " + a.status + ")")
	}
	if a.officer == nil || (*a.officer != me && !admin) {
		return httpx.RuleViolation("This application is assigned to another vetting officer")
	}
	return nil
}

func (s *Server) PostConsoleApplicationsClaim(w http.ResponseWriter, r *http.Request, id gen.Id) {
	ctx := r.Context()
	me := auth.MustPrincipal(ctx).UserID
	err := dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		a, err := lockApplication(ctx, tx, uuid.UUID(id))
		if err != nil {
			return err
		}
		if a.status != "submitted" {
			return httpx.Conflict("Only a submitted application can be claimed (status: " + a.status + ")")
		}
		if _, err := tx.Exec(ctx, `UPDATE applications SET status = 'in_vetting', vetting_officer_id = $2 WHERE id = $1`, uuid.UUID(id), me); err != nil {
			return err
		}
		return audit(ctx, tx, "application.claim", "application", uuid.UUID(id), map[string]any{"status": a.status}, map[string]any{"status": "in_vetting"}, "")
	})
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	s.respondApplication(w, r, uuid.UUID(id), true, http.StatusOK)
}

func (s *Server) PostConsoleApplicationsReturn(w http.ResponseWriter, r *http.Request, id gen.Id) {
	in, err := httpx.Decode[gen.PostConsoleApplicationsReturnJSONBody](r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	msg := strings.TrimSpace(in.Message)
	if len(msg) < 3 {
		httpx.Fail(w, r, httpx.Invalid("message must say what is missing"))
		return
	}
	ctx := r.Context()
	me := auth.MustPrincipal(ctx).UserID
	err = dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		a, err := lockApplication(ctx, tx, uuid.UUID(id))
		if err != nil {
			return err
		}
		if err := requireOfficer(a, me, false); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE applications SET status = 'incomplete' WHERE id = $1`, uuid.UUID(id)); err != nil {
			return err
		}
		if in.ItemIds != nil {
			for _, item := range *in.ItemIds {
				if tag, err := tx.Exec(ctx, `UPDATE application_items SET status = 'pending', notes = $3, reviewed_by = NULL, reviewed_at = NULL WHERE id = $1 AND application_id = $2`,
					uuid.UUID(item), uuid.UUID(id), msg); err != nil || tag.RowsAffected() == 0 {
					if err != nil {
						return err
					}
					return httpx.Invalid("item_ids contains an item from another application")
				}
			}
		}
		if err := notify(ctx, tx, a.provider, "application.incomplete", "More information needed", msg, map[string]any{"application_id": id}); err != nil {
			return err
		}
		return audit(ctx, tx, "application.return", "application", uuid.UUID(id), map[string]any{"status": a.status}, map[string]any{"status": "incomplete"}, msg)
	})
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	s.respondApplication(w, r, uuid.UUID(id), true, http.StatusOK)
}

func (s *Server) PatchConsoleApplicationsItemsById(w http.ResponseWriter, r *http.Request, id gen.Id, itemId gen.Id) {
	in, err := httpx.Decode[gen.ItemReview](r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	notes := strings.TrimSpace(deref(in.Notes))
	if (in.Status == "failed" || in.Status == "waived") && len(notes) < 3 {
		httpx.Fail(w, r, httpx.Invalid("notes are required when an item is failed or waived"))
		return
	}
	ctx := r.Context()
	p := auth.MustPrincipal(ctx)
	err = dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		a, err := lockApplication(ctx, tx, uuid.UUID(id))
		if err != nil {
			return err
		}
		if err := requireOfficer(a, p.UserID, p.HasAnyRole("admin")); err != nil {
			return err
		}
		var kind string
		var docStatus *string
		if err := tx.QueryRow(ctx, `SELECT i.kind::text, d.status::text FROM application_items x JOIN checklist_template_items i ON i.id = x.template_item_id
			LEFT JOIN documents d ON d.id = x.document_id WHERE x.id = $1 AND x.application_id = $2 FOR UPDATE OF x`, uuid.UUID(itemId), uuid.UUID(id)).Scan(&kind, &docStatus); err != nil {
			return err
		}
		if in.Status == "satisfied" && isDocumentKind(kind) && (docStatus == nil || *docStatus != "verified") {
			return httpx.RuleViolation("Verify the document before marking this item satisfied")
		}
		if _, err := tx.Exec(ctx, `UPDATE application_items SET status = $2::checklist_item_status, notes = NULLIF($3,''), reviewed_by = $4, reviewed_at = now() WHERE id = $1`,
			uuid.UUID(itemId), string(in.Status), notes, p.UserID); err != nil {
			return err
		}
		return audit(ctx, tx, "application.item_review", "application_item", uuid.UUID(itemId), nil, map[string]any{"status": in.Status}, notes)
	})
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	raw, err := rawOne(ctx, s.DB, `
		SELECT jsonb_build_object('id', x.id, 'template_item', `+checklistItemObj+`, 'status', x.status, 'document_id', x.document_id, 'notes', x.notes,
		  'reviewed_by', x.reviewed_by, 'reviewed_at', x.reviewed_at)
		FROM application_items x JOIN checklist_template_items i ON i.id = x.template_item_id WHERE x.id = $1`, uuid.UUID(itemId))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, raw)
}

func (s *Server) PostConsoleApplicationsRefereesRecord(w http.ResponseWriter, r *http.Request, id gen.Id, refereeId gen.Id) {
	in, err := httpx.Decode[gen.RefereeRecord](r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if in.Outcome == "pending" || len(strings.TrimSpace(in.Statement)) < 3 {
		httpx.Fail(w, r, httpx.Invalid("Record the outcome and what the referee said"))
		return
	}
	enc, err := s.Cipher.Encrypt([]byte(strings.TrimSpace(in.Statement)))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	ctx := r.Context()
	p := auth.MustPrincipal(ctx)
	contacted := s.Now()
	if in.ContactedAt != nil {
		contacted = *in.ContactedAt
	}
	err = dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		a, err := lockApplication(ctx, tx, uuid.UUID(id))
		if err != nil {
			return err
		}
		if err := requireOfficer(a, p.UserID, false); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `UPDATE referees SET outcome = $3::referee_outcome, statement_enc = $4, contacted_by = $5, contacted_at = $6 WHERE id = $1 AND application_id = $2`,
			uuid.UUID(refereeId), uuid.UUID(id), string(in.Outcome), enc, p.UserID, contacted)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return httpx.ErrNotFound
		}
		return audit(ctx, tx, "referee.record", "referee", uuid.UUID(refereeId), nil, map[string]any{"outcome": in.Outcome}, "")
	})
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	app, err := s.loadApplication(ctx, s.DB, uuid.UUID(id), true)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	for _, x := range app["referees"].([]map[string]any) {
		if x["id"] == uuid.UUID(refereeId) {
			httpx.JSON(w, http.StatusOK, x)
			return
		}
	}
	httpx.Fail(w, r, httpx.ErrNotFound)
}

func (s *Server) PostConsoleApplicationsClearanceChecks(w http.ResponseWriter, r *http.Request, id gen.Id) {
	in, err := httpx.Decode[gen.ClearanceCheckCreate](r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	ctx := r.Context()
	p := auth.MustPrincipal(ctx)
	var cid uuid.UUID
	err = dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		a, err := lockApplication(ctx, tx, uuid.UUID(id))
		if err != nil {
			return err
		}
		if err := requireOfficer(a, p.UserID, false); err != nil {
			return err
		}
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM documents WHERE id = $1 AND owner_user_id = $2 AND status IN ('uploaded','verified')`, uuid.UUID(in.DocumentId), a.provider).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			return httpx.Invalid("document_id is not an uploaded document of this applicant")
		}
		if err := tx.QueryRow(ctx, `INSERT INTO clearance_checks (application_id, document_id, agency_name, agency_reference) VALUES ($1,$2,$3,$4) RETURNING id`,
			uuid.UUID(id), uuid.UUID(in.DocumentId), trimPtr(in.AgencyName), trimPtr(in.AgencyReference)).Scan(&cid); err != nil {
			return err
		}
		return audit(ctx, tx, "clearance.request", "clearance_check", cid, nil, map[string]any{"document_id": in.DocumentId}, "")
	})
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	raw, err := rawOne(ctx, s.DB, clearanceJSON+` WHERE c.id = $1`, cid)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, raw)
}

func (s *Server) PatchConsoleClearanceChecksById(w http.ResponseWriter, r *http.Request, id gen.Id) {
	in, err := httpx.Decode[gen.ClearanceCheckUpdate](r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	ctx := r.Context()
	p := auth.MustPrincipal(ctx)
	err = dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		var appID, docID uuid.UUID
		var cur string
		if err := tx.QueryRow(ctx, `SELECT application_id, document_id, result::text FROM clearance_checks WHERE id = $1 FOR UPDATE`, uuid.UUID(id)).Scan(&appID, &docID, &cur); err != nil {
			return err
		}
		a, err := lockApplication(ctx, tx, appID)
		if err != nil {
			return err
		}
		if err := requireOfficer(a, p.UserID, false); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE clearance_checks SET result = $2::clearance_result, agency_reference = COALESCE($3, agency_reference), notes = COALESCE($4, notes),
			confirmed_at = CASE WHEN $2 = 'pending' THEN NULL ELSE now() END, recorded_by = $5 WHERE id = $1`,
			uuid.UUID(id), string(in.Result), trimPtr(in.AgencyReference), trimPtr(in.Notes), p.UserID); err != nil {
			return err
		}
		if in.Result == "not_authentic" {
			// A forged or void certificate is not a document the vault can stand behind.
			if _, err := tx.Exec(ctx, `UPDATE documents SET status = 'rejected', rejection_reason = 'Issuing agency could not confirm it is authentic', verified_by = NULL, verified_at = NULL WHERE id = $1`, docID); err != nil {
				return err
			}
		}
		return audit(ctx, tx, "clearance.record", "clearance_check", uuid.UUID(id), map[string]any{"result": cur}, map[string]any{"result": in.Result}, deref(in.Notes))
	})
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	raw, err := rawOne(ctx, s.DB, clearanceJSON+` WHERE c.id = $1`, uuid.UUID(id))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, raw)
}

func (s *Server) PostConsoleApplicationsAssessments(w http.ResponseWriter, r *http.Request, id gen.Id) {
	in, err := httpx.Decode[gen.AssessmentCreate](r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if in.TotalScore < 0 || in.TotalScore > 100 || strings.TrimSpace(in.RubricVersion) == "" || len(in.Scores) == 0 {
		httpx.Fail(w, r, httpx.Invalid("total_score must be 0-100 and the rubric version and scores are required"))
		return
	}
	ctx := r.Context()
	p := auth.MustPrincipal(ctx)
	var aid uuid.UUID
	err = dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		a, err := lockApplication(ctx, tx, uuid.UUID(id))
		if err != nil {
			return err
		}
		if err := requireOfficer(a, p.UserID, false); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `INSERT INTO assessments (application_id, kind, rubric_version, scores, total_score, passed, assessed_by, notes)
			VALUES ($1,$2::assessment_kind,$3,$4,$5,$6,$7,$8) RETURNING id`,
			uuid.UUID(id), string(in.Kind), in.RubricVersion, rawJSON(in.Scores), in.TotalScore, in.Passed, p.UserID, trimPtr(in.Notes)).Scan(&aid); err != nil {
			return err
		}
		return audit(ctx, tx, "assessment.record", "assessment", aid, nil, map[string]any{"kind": in.Kind, "passed": in.Passed}, "")
	})
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	raw, err := rawOne(ctx, s.DB, `SELECT jsonb_build_object('id', x.id, 'kind', x.kind, 'rubric_version', x.rubric_version, 'scores', x.scores, 'total_score', x.total_score,
		'passed', x.passed, 'notes', x.notes, 'assessed_by', x.assessed_by, 'assessed_at', x.assessed_at) FROM assessments x WHERE x.id = $1`, aid)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, raw)
}

// PostConsoleApplicationsCompleteVetting hands the file to an approver once every mandatory item is settled.
func (s *Server) PostConsoleApplicationsCompleteVetting(w http.ResponseWriter, r *http.Request, id gen.Id) {
	in, err := httpx.Decode[gen.VettingComplete](r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	report := strings.TrimSpace(in.VettingReport)
	if len(report) < 10 {
		httpx.Fail(w, r, httpx.Invalid("The vetting report must summarise the findings"))
		return
	}
	ctx := r.Context()
	p := auth.MustPrincipal(ctx)
	err = dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		a, err := lockApplication(ctx, tx, uuid.UUID(id))
		if err != nil {
			return err
		}
		if err := requireOfficer(a, p.UserID, false); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT i.label, x.status::text FROM application_items x JOIN checklist_template_items i ON i.id = x.template_item_id
			WHERE x.application_id = $1 AND i.is_mandatory AND x.status NOT IN ('satisfied','waived') ORDER BY i.sort_order, i.label`, uuid.UUID(id))
		if err != nil {
			return err
		}
		outstanding := []string{}
		for rows.Next() {
			var label, st string
			if err := rows.Scan(&label, &st); err != nil {
				rows.Close()
				return err
			}
			outstanding = append(outstanding, label+" ("+st+")")
		}
		rows.Close()
		if len(outstanding) > 0 {
			return httpx.RuleViolation("Mandatory checks are not settled yet").With("outstanding", outstanding)
		}
		if _, err := tx.Exec(ctx, `UPDATE applications SET status = 'awaiting_approval', vetting_report = $2, vetting_completed_at = now() WHERE id = $1`, uuid.UUID(id), report); err != nil {
			return err
		}
		return audit(ctx, tx, "application.complete_vetting", "application", uuid.UUID(id), map[string]any{"status": a.status}, map[string]any{"status": "awaiting_approval"}, "")
	})
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	s.respondApplication(w, r, uuid.UUID(id), true, http.StatusOK)
}

// PostConsoleApplicationsDecision is the approver's call. The database also refuses a decision by the
// officer who vetted the file, so the rule holds even if this check is bypassed.
func (s *Server) PostConsoleApplicationsDecision(w http.ResponseWriter, r *http.Request, id gen.Id) {
	in, err := httpx.Decode[gen.ApplicationDecision](r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	reason := strings.TrimSpace(in.Reason)
	if len(reason) < 3 {
		httpx.Fail(w, r, httpx.Invalid("A decision needs a reason"))
		return
	}
	ctx := r.Context()
	me := auth.MustPrincipal(ctx).UserID
	err = dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		a, err := lockApplication(ctx, tx, uuid.UUID(id))
		if err != nil {
			return err
		}
		if a.status != "awaiting_approval" {
			return httpx.Conflict("Only an application awaiting approval can be decided (status: " + a.status + ")")
		}
		if a.officer != nil && *a.officer == me {
			return httpx.RuleViolation("The officer who vetted an application cannot approve it")
		}
		next := map[gen.ApplicationDecisionDecision]string{"approve": "approved", "defer": "deferred", "reject": "rejected"}[in.Decision]
		if _, err := tx.Exec(ctx, `UPDATE applications SET status = $2::application_status, decided_by = $3, decision_reason = $4, decided_at = now() WHERE id = $1`,
			uuid.UUID(id), next, me, reason); err != nil {
			return err
		}
		title, body := "Application "+next, reason
		if next == "approved" {
			// Approval opens the category as 'approved'. An active category stays active on re-verification.
			if _, err := tx.Exec(ctx, `
				INSERT INTO provider_categories (provider_id, category_id, status, approved_at) VALUES ($1,$2,'approved', now())
				ON CONFLICT (provider_id, category_id) DO UPDATE SET status = CASE WHEN provider_categories.status = 'active' THEN 'active'::provider_category_status ELSE 'approved' END,
				  approved_at = now()`, a.provider, a.category); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `UPDATE providers SET status = 'approved' WHERE user_id = $1 AND status = 'applicant'`, a.provider); err != nil {
				return err
			}
			title, body = "Application approved", "You are approved. Next: training, then sign your contract."
		}
		if err := notify(ctx, tx, a.provider, "application."+next, title, body, map[string]any{"application_id": id}); err != nil {
			return err
		}
		return audit(ctx, tx, "application."+string(in.Decision), "application", uuid.UUID(id), map[string]any{"status": a.status}, map[string]any{"status": next}, reason)
	})
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	s.respondApplication(w, r, uuid.UUID(id), true, http.StatusOK)
}

// PostConsoleProvidersReverification opens a fresh application so an active provider is re-checked.
func (s *Server) PostConsoleProvidersReverification(w http.ResponseWriter, r *http.Request, id gen.Id) {
	in, err := httpx.Decode[gen.PostConsoleProvidersReverificationJSONBody](r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	ctx := r.Context()
	var appID uuid.UUID
	err = dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM provider_categories WHERE provider_id = $1 AND category_id = $2 AND status IN ('approved','active')`, uuid.UUID(id), uuid.UUID(in.CategoryId)).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			return httpx.RuleViolation("The provider is not approved for that category")
		}
		var tpl uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT id FROM checklist_templates WHERE category_id = $1 AND is_current`, uuid.UUID(in.CategoryId)).Scan(&tpl); err != nil {
			return httpx.RuleViolation("That category has no current checklist")
		}
		if err := tx.QueryRow(ctx, `INSERT INTO applications (provider_id, category_id, template_id, is_reverification) VALUES ($1,$2,$3,true) RETURNING id`,
			uuid.UUID(id), uuid.UUID(in.CategoryId), tpl).Scan(&appID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO application_items (application_id, template_item_id) SELECT $1, id FROM checklist_template_items WHERE template_id = $2`, appID, tpl); err != nil {
			return err
		}
		if err := notify(ctx, tx, uuid.UUID(id), "application.reverification", "Re-verification needed", "Please refresh your documents so we can re-verify your profile.", map[string]any{"application_id": appID}); err != nil {
			return err
		}
		return audit(ctx, tx, "application.reverification", "application", appID, nil, map[string]any{"provider_id": id}, "")
	})
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	s.respondApplication(w, r, appID, true, http.StatusCreated)
}

// flattenSummary lifts each row's "summary" object up to the row itself.
func flattenSummary(pg page[json.RawMessage]) (page[json.RawMessage], error) {
	for i, raw := range pg.Data {
		var row struct {
			Summary json.RawMessage `json:"summary"`
		}
		if err := json.Unmarshal(raw, &row); err != nil {
			return pg, err
		}
		pg.Data[i] = row.Summary
	}
	return pg, nil
}

var _ = time.Now
