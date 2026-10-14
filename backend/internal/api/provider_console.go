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

const disciplinaryObj = `jsonb_build_object('id', d.id, 'provider_id', d.provider_id, 'complaint_id', d.complaint_id, 'kind', d.kind, 'status', d.status, 'reason', d.reason,
	  'effective_from', d.effective_from, 'effective_to', d.effective_to, 'decided_by', d.decided_by, 'second_authoriser_id', d.second_authoriser_id,
	  'authorised_at', d.authorised_at, 'created_at', d.created_at)`

const disciplinaryJSON = `SELECT ` + disciplinaryObj + ` FROM disciplinary_actions d`

func (s *Server) GetConsoleProviders(w http.ResponseWriter, r *http.Request, params gen.GetConsoleProvidersParams) {
	cur, err := decodeCursor(params.Cursor)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	b := &qb{}
	if params.Status != nil {
		b.add("p.status = ?::provider_status", string(*params.Status))
	}
	if params.CategoryId != nil {
		b.add("EXISTS (SELECT 1 FROM provider_categories c WHERE c.provider_id = p.user_id AND c.category_id = ?)", uuid.UUID(*params.CategoryId))
	}
	if params.ServiceAreaId != nil {
		b.add("(p.home_service_area_id = ? OR EXISTS (SELECT 1 FROM provider_service_areas a WHERE a.provider_id = p.user_id AND a.service_area_id = ?))", uuid.UUID(*params.ServiceAreaId))
	}
	if params.Q != nil && strings.TrimSpace(*params.Q) != "" {
		b.add("(u.full_name ILIKE ? OR u.phone_e164 ILIKE ?)", "%"+strings.TrimSpace(*params.Q)+"%")
	}
	pg, err := pagedRaw(r.Context(), s.DB, `SELECT p.user_id AS id, u.full_name, p.status, u.phone_e164 AS phone, p.rating_avg, p.appraisal_score, p.completed_jobs, p.home_service_area_id,
		p.created_at AS _t, p.user_id AS _id FROM providers p JOIN users u ON u.id = p.user_id WHERE `+b.where(), b.args, cur, pageLimit(params.Limit))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, pg)
}

func (s *Server) providerStaff(ctx context.Context, q db, id uuid.UUID) (map[string]any, error) {
	raw, err := rawOne(ctx, q, providerProfileJSON+` WHERE p.user_id = $1`, id)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	extra, err := rawOne(ctx, q, `SELECT jsonb_build_object('phone', u.phone_e164, 'suspended_reason', p.suspended_reason, 'max_concurrent_jobs', p.max_concurrent_jobs,
		'open_complaints', (SELECT count(*) FROM complaints c WHERE c.provider_id = p.user_id AND c.status NOT IN ('closed','withdrawn')))
		FROM providers p JOIN users u ON u.id = p.user_id WHERE p.user_id = $1`, id)
	if err != nil {
		return nil, err
	}
	var more map[string]any
	if err := json.Unmarshal(extra, &more); err != nil {
		return nil, err
	}
	for k, v := range more {
		out[k] = v
	}
	if out["documents"], err = rawAll(ctx, q, documentJSON+` WHERE d.owner_user_id = $1 AND d.purged_at IS NULL ORDER BY d.created_at DESC`, id); err != nil {
		return nil, err
	}
	if out["applications"], err = rawAll(ctx, q, `SELECT `+applicationSummaryObj+applicationSummaryFrom+` WHERE a.provider_id = $1 ORDER BY a.created_at DESC`, id); err != nil {
		return nil, err
	}
	if out["trainings"], err = rawAll(ctx, q, trainingJSON+` WHERE t.provider_id = $1 ORDER BY t.created_at DESC`, id); err != nil {
		return nil, err
	}
	if out["disciplinary_actions"], err = rawAll(ctx, q, disciplinaryJSON+` WHERE d.provider_id = $1 ORDER BY d.created_at DESC`, id); err != nil {
		return nil, err
	}
	contract, err := s.contractOffer(ctx, q, id)
	if err != nil {
		return nil, err
	}
	if contract["signed_at"] != nil {
		out["contract"] = contract
	}
	return compact(out), nil
}

func (s *Server) respondProviderStaff(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	out, err := s.providerStaff(r.Context(), s.DB, id)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (s *Server) GetConsoleProvidersById(w http.ResponseWriter, r *http.Request, id gen.Id) {
	s.respondProviderStaff(w, r, uuid.UUID(id))
}

func (s *Server) PatchConsoleProvidersById(w http.ResponseWriter, r *http.Request, id gen.Id) {
	in, err := httpx.Decode[gen.ProviderStaffUpdate](r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if in.MaxConcurrentJobs != nil && (*in.MaxConcurrentJobs < 1 || *in.MaxConcurrentJobs > 20) {
		httpx.Fail(w, r, httpx.Invalid("max_concurrent_jobs must be between 1 and 20"))
		return
	}
	ctx := r.Context()
	err = dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		var basis *string
		if in.EngagementBasis != nil {
			b := string(*in.EngagementBasis)
			basis = &b
		}
		tag, err := tx.Exec(ctx, `UPDATE providers SET engagement_basis = COALESCE($2::provider_engagement_basis, engagement_basis),
			max_concurrent_jobs = COALESCE($3, max_concurrent_jobs), bio = COALESCE($4, bio) WHERE user_id = $1`, uuid.UUID(id), basis, in.MaxConcurrentJobs, trimPtr(in.Bio))
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return httpx.ErrNotFound
		}
		if in.ServiceAreaIds != nil {
			if _, err := tx.Exec(ctx, `DELETE FROM provider_service_areas WHERE provider_id = $1`, uuid.UUID(id)); err != nil {
				return err
			}
			for _, a := range *in.ServiceAreaIds {
				if _, err := tx.Exec(ctx, `INSERT INTO provider_service_areas (provider_id, service_area_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`, uuid.UUID(id), uuid.UUID(a)); err != nil {
					return err
				}
			}
		}
		return audit(ctx, tx, "provider.update", "provider", uuid.UUID(id), nil, in, "")
	})
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	s.respondProviderStaff(w, r, uuid.UUID(id))
}

// activationBlockers lists what stands between an approved provider and being assignable for a category.
func (s *Server) activationBlockers(ctx context.Context, q db, providerID, categoryID uuid.UUID) ([]string, error) {
	blockers := []string{}
	var catStatus *string
	_ = q.QueryRow(ctx, `SELECT status::text FROM provider_categories WHERE provider_id = $1 AND category_id = $2`, providerID, categoryID).Scan(&catStatus)
	if catStatus == nil || (*catStatus != "approved" && *catStatus != "active") {
		blockers = append(blockers, "the category has not been approved through vetting")
	}
	rows, err := q.Query(ctx, `
		SELECT c.name FROM training_courses c
		WHERE c.is_active AND (c.is_induction OR c.category_id = $2)
		  AND NOT EXISTS (SELECT 1 FROM training_enrolments e WHERE e.provider_id = $1 AND e.course_id = c.id AND e.status = 'passed')
		ORDER BY c.name`, providerID, categoryID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return nil, err
		}
		blockers = append(blockers, "training not passed: "+name)
	}
	rows.Close()
	cv := s.settingText(ctx, q, "contract.current_version", "2026-10")
	var signed int
	if err := q.QueryRow(ctx, `SELECT count(*) FROM provider_contracts WHERE provider_id = $1 AND contract_version = $2 AND terminated_at IS NULL`, providerID, cv).Scan(&signed); err != nil {
		return nil, err
	}
	if signed == 0 {
		blockers = append(blockers, "the current contract is not signed")
	}
	expired, err := s.expiredMandatoryDocs(ctx, q, providerID)
	if err != nil {
		return nil, err
	}
	if expired > 0 {
		blockers = append(blockers, "a mandatory document has expired")
	}
	return blockers, nil
}

func (s *Server) expiredMandatoryDocs(ctx context.Context, q db, providerID uuid.UUID) (int, error) {
	var n int
	err := q.QueryRow(ctx, `
		SELECT count(*) FROM documents d WHERE d.owner_user_id = $1 AND d.expires_on IS NOT NULL AND d.expires_on < current_date AND d.status IN ('verified','expired')
		  AND d.document_type IN (SELECT i.document_type FROM checklist_template_items i JOIN checklist_templates t ON t.id = i.template_id AND t.is_current
		        JOIN provider_categories pc ON pc.category_id = t.category_id AND pc.provider_id = $1 AND pc.status IN ('approved','active')
		        WHERE i.is_mandatory AND i.kind = 'document')`, providerID).Scan(&n)
	return n, err
}

func (s *Server) PostConsoleProvidersActivate(w http.ResponseWriter, r *http.Request, id gen.Id) {
	in, err := httpx.Decode[gen.ProviderActivate](r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if len(in.CategoryIds) == 0 {
		httpx.Fail(w, r, httpx.Invalid("category_ids is required"))
		return
	}
	ctx := r.Context()
	err = dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		var status string
		if err := tx.QueryRow(ctx, `SELECT status::text FROM providers WHERE user_id = $1 FOR UPDATE`, uuid.UUID(id)).Scan(&status); err != nil {
			return err
		}
		if status != "approved" && status != "active" {
			return httpx.RuleViolation("Only an approved provider can be activated (status: " + status + ")")
		}
		for _, c := range in.CategoryIds {
			blockers, err := s.activationBlockers(ctx, tx, uuid.UUID(id), uuid.UUID(c))
			if err != nil {
				return err
			}
			if len(blockers) > 0 {
				return httpx.RuleViolation("Provider is not ready to be activated").With("blockers", blockers).With("category_id", c)
			}
			if _, err := tx.Exec(ctx, `UPDATE provider_categories SET status = 'active', activated_at = COALESCE(activated_at, now()) WHERE provider_id = $1 AND category_id = $2`, uuid.UUID(id), uuid.UUID(c)); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE providers SET status = 'active', activated_at = COALESCE(activated_at, now()) WHERE user_id = $1`, uuid.UUID(id)); err != nil {
			return err
		}
		if err := notify(ctx, tx, uuid.UUID(id), "provider.activated", "You are live", "You can now receive job offers.", nil); err != nil {
			return err
		}
		return audit(ctx, tx, "provider.activate", "provider", uuid.UUID(id), map[string]any{"status": status}, map[string]any{"status": "active", "category_ids": in.CategoryIds}, "")
	})
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	s.respondProviderStaff(w, r, uuid.UUID(id))
}

// suspendProvider takes a provider out of circulation: no new offers, live offers are withdrawn and
// booked work is flagged for triage to reassign. The caller records why in the audit log.
func (s *Server) suspendProvider(ctx context.Context, tx pgx.Tx, providerID uuid.UUID, reason string) error {
	tag, err := tx.Exec(ctx, `UPDATE providers SET status = 'suspended', suspended_reason = $2 WHERE user_id = $1 AND status IN ('active','approved')`, providerID, reason)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return nil // already suspended, disqualified or not yet approved: nothing to take away
	}
	rows, err := tx.Query(ctx, `UPDATE assignments SET status = 'withdrawn', ended_at = now(), ended_reason = 'Provider suspended' WHERE provider_id = $1 AND status = 'offered' RETURNING request_id`, providerID)
	if err != nil {
		return err
	}
	var reqs []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		reqs = append(reqs, id)
	}
	rows.Close()
	for _, id := range reqs {
		if _, err := tx.Exec(ctx, `UPDATE service_requests SET status = 'triaging' WHERE id = $1 AND status = 'awaiting_provider'`, id); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO job_exceptions (kind, request_id, visit_id, provider_id, details)
		SELECT 'other', v.request_id, v.id, v.provider_id, jsonb_build_object('reason', 'provider_suspended', 'detail', $2::text)
		FROM visits v WHERE v.provider_id = $1 AND v.status = 'scheduled' AND v.scheduled_start > now()`, providerID, reason); err != nil {
		return err
	}
	return notify(ctx, tx, providerID, "provider.suspended", "Account suspended", reason, nil)
}

func (s *Server) PostConsoleProvidersSuspend(w http.ResponseWriter, r *http.Request, id gen.Id) {
	in, err := httpx.Decode[gen.SuspendInput](r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	reason := strings.TrimSpace(in.Reason)
	if len(reason) < 3 {
		httpx.Fail(w, r, httpx.Invalid("reason is required"))
		return
	}
	if in.Until != nil && !in.Until.After(s.Now()) {
		httpx.Fail(w, r, httpx.Invalid("until must be in the future"))
		return
	}
	ctx := r.Context()
	err = dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		var status string
		if err := tx.QueryRow(ctx, `SELECT status::text FROM providers WHERE user_id = $1 FOR UPDATE`, uuid.UUID(id)).Scan(&status); err != nil {
			return err
		}
		if status != "active" && status != "approved" {
			return httpx.Conflict("Only an active or approved provider can be suspended (status: " + status + ")")
		}
		if err := s.suspendProvider(ctx, tx, uuid.UUID(id), reason); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO disciplinary_actions (provider_id, kind, status, reason, effective_to, decided_by) VALUES ($1,'suspension','in_effect',$2,$3,$4)`,
			uuid.UUID(id), reason, in.Until, *actorID(ctx)); err != nil {
			return err
		}
		return audit(ctx, tx, "provider.suspend", "provider", uuid.UUID(id), map[string]any{"status": status}, map[string]any{"status": "suspended", "until": in.Until}, reason)
	})
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	s.respondProviderStaff(w, r, uuid.UUID(id))
}

// reinstateBlockers lists why a suspended provider cannot go back to work yet.
func (s *Server) reinstateBlockers(ctx context.Context, q db, providerID uuid.UUID) ([]string, error) {
	b := []string{}
	var n int
	if err := q.QueryRow(ctx, `
		SELECT count(*) FROM disciplinary_actions a WHERE a.provider_id = $1 AND a.kind = 'retraining' AND a.status = 'in_effect'
		  AND NOT EXISTS (SELECT 1 FROM training_enrolments e WHERE e.provider_id = a.provider_id AND e.is_retraining AND e.status = 'passed' AND e.created_at >= a.created_at
		                    AND (a.complaint_id IS NULL OR e.complaint_id IS NOT DISTINCT FROM a.complaint_id))`, providerID).Scan(&n); err != nil {
		return nil, err
	}
	if n > 0 {
		b = append(b, "required retraining has not been passed")
	}
	if err := q.QueryRow(ctx, `SELECT count(*) FROM complaints WHERE provider_id = $1 AND severity IN ('safeguarding','loss') AND status NOT IN ('closed','withdrawn')`, providerID).Scan(&n); err != nil {
		return nil, err
	}
	if n > 0 {
		b = append(b, "a safeguarding or loss case is still open")
	}
	if n, err := s.expiredMandatoryDocs(ctx, q, providerID); err != nil {
		return nil, err
	} else if n > 0 {
		b = append(b, "a mandatory document is expired")
	}
	if err := q.QueryRow(ctx, `SELECT count(*) FROM disciplinary_actions WHERE provider_id = $1 AND kind = 'disqualification' AND status IN ('pending_authorisation','in_effect')`, providerID).Scan(&n); err != nil {
		return nil, err
	}
	if n > 0 {
		b = append(b, "a disqualification is pending or in effect")
	}
	return b, nil
}

func (s *Server) liftSuspension(ctx context.Context, tx pgx.Tx, providerID uuid.UUID) error {
	if _, err := tx.Exec(ctx, `UPDATE providers SET status = 'active', suspended_reason = NULL WHERE user_id = $1 AND status = 'suspended'`, providerID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE disciplinary_actions SET status = 'completed' WHERE provider_id = $1 AND kind IN ('suspension','retraining') AND status = 'in_effect'`, providerID); err != nil {
		return err
	}
	return notify(ctx, tx, providerID, "provider.reinstated", "Suspension lifted", "You can receive job offers again.", nil)
}

func (s *Server) PostConsoleProvidersReinstate(w http.ResponseWriter, r *http.Request, id gen.Id) {
	reason, err := reasonOf(r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	ctx := r.Context()
	err = dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		var status string
		if err := tx.QueryRow(ctx, `SELECT status::text FROM providers WHERE user_id = $1 FOR UPDATE`, uuid.UUID(id)).Scan(&status); err != nil {
			return err
		}
		if status != "suspended" {
			return httpx.Conflict("Only a suspended provider can be reinstated (status: " + status + ")")
		}
		blockers, err := s.reinstateBlockers(ctx, tx, uuid.UUID(id))
		if err != nil {
			return err
		}
		if len(blockers) > 0 {
			return httpx.RuleViolation("The provider cannot be reinstated yet").With("blockers", blockers)
		}
		if err := s.liftSuspension(ctx, tx, uuid.UUID(id)); err != nil {
			return err
		}
		return audit(ctx, tx, "provider.reinstate", "provider", uuid.UUID(id), map[string]any{"status": status}, map[string]any{"status": "active"}, reason)
	})
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	s.respondProviderStaff(w, r, uuid.UUID(id))
}

// GetConsoleProvidersTimeline is the audit trail of the provider and the records that belong to them.
func (s *Server) GetConsoleProvidersTimeline(w http.ResponseWriter, r *http.Request, id gen.Id, params gen.GetConsoleProvidersTimelineParams) {
	cur, err := decodeCursor(params.Cursor)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	pg, err := pagedRaw(r.Context(), s.DB, `
		SELECT l.id, l.occurred_at, l.actor_id, l.actor_kind, l.action, l.entity_type, l.entity_id, l.before_state, l.after_state, l.reason, host(l.ip_address) AS ip_address,
		  l.occurred_at AS _t, `+strings.Replace(bigUUID, "%s", "l.id", 1)+` AS _id
		FROM audit_log l WHERE l.entity_id = $1
		   OR l.entity_id IN (SELECT id FROM applications WHERE provider_id = $1)
		   OR l.entity_id IN (SELECT id FROM documents WHERE owner_user_id = $1)
		   OR l.entity_id IN (SELECT id FROM complaints WHERE provider_id = $1)
		   OR l.entity_id IN (SELECT id FROM disciplinary_actions WHERE provider_id = $1)
		   OR l.entity_id IN (SELECT id FROM training_enrolments WHERE provider_id = $1)
		   OR l.entity_id IN (SELECT id FROM assignments WHERE provider_id = $1)
		   OR l.entity_id IN (SELECT id FROM provider_contracts WHERE provider_id = $1)`, []any{uuid.UUID(id)}, cur, pageLimit(params.Limit))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, pg)
}

// ---- training

const courseJSON = `SELECT jsonb_build_object('id', c.id, 'code', c.code, 'name', c.name, 'category_id', c.category_id, 'is_induction', c.is_induction, 'pass_mark', c.pass_mark, 'is_active', c.is_active) FROM training_courses c`

func (s *Server) GetConsoleTrainingCourses(w http.ResponseWriter, r *http.Request) {
	out, err := rawAll(r.Context(), s.DB, courseJSON+` ORDER BY c.is_induction DESC, c.name`)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func validateCourse(in gen.TrainingCourseInput) error {
	if strings.TrimSpace(in.Code) == "" || strings.TrimSpace(in.Name) == "" {
		return httpx.Invalid("code and name are required")
	}
	if in.PassMark != nil && (*in.PassMark < 0 || *in.PassMark > 100) {
		return httpx.Invalid("pass_mark must be between 0 and 100")
	}
	return nil
}

func (s *Server) PostConsoleTrainingCourses(w http.ResponseWriter, r *http.Request) {
	in, err := httpx.Decode[gen.TrainingCourseInput](r)
	if err == nil {
		err = validateCourse(in)
	}
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	ctx := r.Context()
	var id uuid.UUID
	err = dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `INSERT INTO training_courses (code, name, category_id, is_induction, pass_mark, is_active)
			VALUES ($1,$2,$3,COALESCE($4,false),COALESCE($5,70),COALESCE($6,true)) RETURNING id`,
			strings.TrimSpace(in.Code), strings.TrimSpace(in.Name), uuidPtr(in.CategoryId), in.IsInduction, in.PassMark, in.IsActive).Scan(&id); err != nil {
			return err
		}
		return audit(ctx, tx, "training_course.create", "training_course", id, nil, in, "")
	})
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	raw, err := rawOne(ctx, s.DB, courseJSON+` WHERE c.id = $1`, id)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, raw)
}

func (s *Server) PatchConsoleTrainingCoursesById(w http.ResponseWriter, r *http.Request, id gen.Id) {
	in, err := httpx.Decode[gen.TrainingCourseInput](r)
	if err == nil {
		err = validateCourse(in)
	}
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	ctx := r.Context()
	err = dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE training_courses SET code = $2, name = $3, category_id = $4, is_induction = COALESCE($5, is_induction),
			pass_mark = COALESCE($6, pass_mark), is_active = COALESCE($7, is_active) WHERE id = $1`,
			uuid.UUID(id), strings.TrimSpace(in.Code), strings.TrimSpace(in.Name), uuidPtr(in.CategoryId), in.IsInduction, in.PassMark, in.IsActive)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return httpx.ErrNotFound
		}
		return audit(ctx, tx, "training_course.update", "training_course", uuid.UUID(id), nil, in, "")
	})
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	raw, err := rawOne(ctx, s.DB, courseJSON+` WHERE c.id = $1`, uuid.UUID(id))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, raw)
}

func (s *Server) GetConsoleTrainings(w http.ResponseWriter, r *http.Request, params gen.GetConsoleTrainingsParams) {
	cur, err := decodeCursor(params.Cursor)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	b := &qb{}
	if params.Status != nil {
		b.add("t.status = ?::training_status", string(*params.Status))
	}
	if params.CourseId != nil {
		b.add("t.course_id = ?", uuid.UUID(*params.CourseId))
	}
	if params.ProviderId != nil {
		b.add("t.provider_id = ?", uuid.UUID(*params.ProviderId))
	}
	if params.From != nil {
		b.add("t.scheduled_for >= ?", params.From.Time)
	}
	if params.To != nil {
		b.add("t.scheduled_for < ?", params.To.Time.AddDate(0, 0, 1))
	}
	pg, err := pagedRaw(r.Context(), s.DB, `SELECT t.id, t.provider_id,
		jsonb_build_object('id', c.id, 'code', c.code, 'name', c.name, 'category_id', c.category_id, 'is_induction', c.is_induction, 'pass_mark', c.pass_mark, 'is_active', c.is_active) AS course,
		t.status, t.is_retraining, t.complaint_id, t.scheduled_for, t.attended_at, t.score, COALESCE(t.scheduled_for, t.created_at) AS _t, t.id AS _id
		FROM training_enrolments t JOIN training_courses c ON c.id = t.course_id WHERE `+b.where(), b.args, cur, pageLimit(params.Limit))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, pg)
}

func (s *Server) PostConsoleTrainings(w http.ResponseWriter, r *http.Request) {
	in, err := httpx.Decode[gen.TrainingEnrolmentCreate](r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	ctx := r.Context()
	var id uuid.UUID
	err = dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		var active bool
		if err := tx.QueryRow(ctx, `SELECT is_active FROM training_courses WHERE id = $1`, uuid.UUID(in.CourseId)).Scan(&active); err != nil {
			return err
		}
		if !active {
			return httpx.RuleViolation("That course is not running")
		}
		var pstatus string
		if err := tx.QueryRow(ctx, `SELECT status::text FROM providers WHERE user_id = $1`, uuid.UUID(in.ProviderId)).Scan(&pstatus); err != nil {
			return err
		}
		if pstatus == "disqualified" || pstatus == "withdrawn" {
			return httpx.RuleViolation("This provider can no longer be enrolled (status: " + pstatus + ")")
		}
		var open int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM training_enrolments WHERE provider_id = $1 AND course_id = $2 AND status IN ('scheduled','attended')`, uuid.UUID(in.ProviderId), uuid.UUID(in.CourseId)).Scan(&open); err != nil {
			return err
		}
		if open > 0 {
			return httpx.Conflict("This provider already has an open place on that course")
		}
		retraining := in.IsRetraining != nil && *in.IsRetraining
		if err := tx.QueryRow(ctx, `INSERT INTO training_enrolments (provider_id, course_id, is_retraining, complaint_id, scheduled_for, recorded_by) VALUES ($1,$2,$3,$4,$5,$6) RETURNING id`,
			uuid.UUID(in.ProviderId), uuid.UUID(in.CourseId), retraining, uuidPtr(in.ComplaintId), in.ScheduledFor, *actorID(ctx)).Scan(&id); err != nil {
			return err
		}
		if err := notify(ctx, tx, uuid.UUID(in.ProviderId), "training.scheduled", "Training scheduled", "You have been enrolled on a training course.", map[string]any{"enrolment_id": id}); err != nil {
			return err
		}
		return audit(ctx, tx, "training.enrol", "training_enrolment", id, nil, in, "")
	})
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	raw, err := rawOne(ctx, s.DB, trainingJSON+` WHERE t.id = $1`, id)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, raw)
}

// PatchConsoleTrainingsById records attendance and the result. A score decides pass or fail against
// the course's pass mark, so a coordinator cannot pass someone below it.
func (s *Server) PatchConsoleTrainingsById(w http.ResponseWriter, r *http.Request, id gen.Id) {
	in, err := httpx.Decode[gen.TrainingResult](r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if in.Score != nil && (*in.Score < 0 || *in.Score > 100) {
		httpx.Fail(w, r, httpx.Invalid("score must be between 0 and 100"))
		return
	}
	ctx := r.Context()
	err = dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		var cur string
		var passMark int
		var provider uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT t.status::text, c.pass_mark, t.provider_id FROM training_enrolments t JOIN training_courses c ON c.id = t.course_id WHERE t.id = $1 FOR UPDATE OF t`, uuid.UUID(id)).
			Scan(&cur, &passMark, &provider); err != nil {
			return err
		}
		if cur == "passed" || cur == "cancelled" {
			return httpx.Conflict("This enrolment is already " + cur)
		}
		status := string(in.Status)
		switch status {
		case "passed", "failed":
			if in.Score == nil {
				return httpx.Invalid("score is required to record a pass or fail")
			}
			if status == "passed" && int(*in.Score) < passMark {
				return httpx.RuleViolation("The score is below the course pass mark")
			}
			if status == "failed" && int(*in.Score) >= passMark {
				return httpx.RuleViolation("The score meets the pass mark, so this cannot be recorded as a fail")
			}
		}
		attended := in.AttendedAt
		if attended == nil && (status == "attended" || status == "passed" || status == "failed") {
			n := s.Now()
			attended = &n
		}
		if _, err := tx.Exec(ctx, `UPDATE training_enrolments SET status = $2::training_status, score = COALESCE($3, score), attended_at = COALESCE($4, attended_at), recorded_by = $5 WHERE id = $1`,
			uuid.UUID(id), status, in.Score, attended, *actorID(ctx)); err != nil {
			return err
		}
		if status == "passed" || status == "failed" {
			if err := notify(ctx, tx, provider, "training."+status, "Training result", "Your training result: "+status+".", map[string]any{"enrolment_id": id}); err != nil {
				return err
			}
		}
		return audit(ctx, tx, "training.result", "training_enrolment", uuid.UUID(id), map[string]any{"status": cur}, map[string]any{"status": status, "score": in.Score}, "")
	})
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	raw, err := rawOne(ctx, s.DB, trainingJSON+` WHERE t.id = $1`, uuid.UUID(id))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, raw)
}

var (
	_ = auth.KindStaff
	_ = time.Now
)
