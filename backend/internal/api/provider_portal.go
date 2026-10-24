package api

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
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

const providerProfileJSON = `
	SELECT jsonb_build_object('id', p.user_id, 'full_name', u.full_name, 'status', p.status, 'engagement_basis', p.engagement_basis,
	  'date_of_birth', p.date_of_birth, 'home_service_area_id', p.home_service_area_id, 'home_lat', p.home_lat, 'home_lng', p.home_lng,
	  'service_area_ids', COALESCE((SELECT jsonb_agg(a.service_area_id ORDER BY a.service_area_id) FROM provider_service_areas a WHERE a.provider_id = p.user_id), '[]'::jsonb),
	  'bio', p.bio, 'years_experience', p.years_experience, 'languages', to_jsonb(p.languages), 'photo_document_id', p.photo_document_id,
	  'payout_msisdn', p.payout_msisdn,
	  'categories', COALESCE((SELECT jsonb_agg(jsonb_build_object('category_id', c.category_id, 'status', c.status) ORDER BY c.category_id) FROM provider_categories c WHERE c.provider_id = p.user_id), '[]'::jsonb),
	  'rating_avg', p.rating_avg, 'rating_count', p.rating_count, 'appraisal_score', p.appraisal_score, 'completed_jobs', p.completed_jobs, 'activated_at', p.activated_at)
	FROM providers p JOIN users u ON u.id = p.user_id`

func (s *Server) GetProviderProfile(w http.ResponseWriter, r *http.Request) {
	raw, err := rawOne(r.Context(), s.DB, providerProfileJSON+` WHERE p.user_id = $1`, auth.MustPrincipal(r.Context()).UserID)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, raw)
}

func (s *Server) PatchProviderProfile(w http.ResponseWriter, r *http.Request) {
	in, err := httpx.Decode[gen.ProviderProfileUpdate](r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if (in.HomeLat == nil) != (in.HomeLng == nil) {
		httpx.Fail(w, r, httpx.Invalid("Give both home_lat and home_lng, or neither"))
		return
	}
	if in.HomeLat != nil && (*in.HomeLat < -90 || *in.HomeLat > 90 || *in.HomeLng < -180 || *in.HomeLng > 180) {
		httpx.Fail(w, r, httpx.Invalid("home_lat/home_lng out of range"))
		return
	}
	if in.PayoutMsisdn != nil && !phoneRE.MatchString(*in.PayoutMsisdn) {
		httpx.Fail(w, r, httpx.Invalid("payout_msisdn must be in international format"))
		return
	}
	if in.YearsExperience != nil && (*in.YearsExperience < 0 || *in.YearsExperience > 70) {
		httpx.Fail(w, r, httpx.Invalid("years_experience is out of range"))
		return
	}
	nid, err := s.encryptOpt(in.NationalIdNumber)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	kra, err := s.encryptOpt(in.KraPin)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	ctx := r.Context()
	me := auth.MustPrincipal(ctx).UserID
	var dob *time.Time
	if in.DateOfBirth != nil {
		dob = &in.DateOfBirth.Time
	}
	err = dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		if in.PhotoDocumentId != nil {
			var n int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM documents WHERE id = $1 AND owner_user_id = $2 AND document_type = 'passport_photo' AND status IN ('uploaded','verified')`,
				uuid.UUID(*in.PhotoDocumentId), me).Scan(&n); err != nil {
				return err
			}
			if n == 0 {
				return httpx.Invalid("photo_document_id must be one of your uploaded passport photos")
			}
		}
		var langs []string
		if in.Languages != nil {
			langs = *in.Languages
		}
		if _, err := tx.Exec(ctx, `
			UPDATE providers SET
			  date_of_birth = COALESCE($2, date_of_birth), national_id_enc = COALESCE($3, national_id_enc), kra_pin_enc = COALESCE($4, kra_pin_enc),
			  home_service_area_id = COALESCE($5, home_service_area_id), home_lat = COALESCE($6, home_lat), home_lng = COALESCE($7, home_lng),
			  bio = COALESCE($8, bio), years_experience = COALESCE($9, years_experience), languages = COALESCE($10, languages),
			  photo_document_id = COALESCE($11, photo_document_id), payout_msisdn = COALESCE($12, payout_msisdn)
			WHERE user_id = $1`, me, dob, nid, kra, uuidPtr(in.HomeServiceAreaId), in.HomeLat, in.HomeLng, trimPtr(in.Bio), in.YearsExperience,
			nilIfEmpty(langs), uuidPtr(in.PhotoDocumentId), in.PayoutMsisdn); err != nil {
			return err
		}
		if in.ServiceAreaIds != nil {
			if _, err := tx.Exec(ctx, `DELETE FROM provider_service_areas WHERE provider_id = $1`, me); err != nil {
				return err
			}
			for _, a := range *in.ServiceAreaIds {
				if _, err := tx.Exec(ctx, `INSERT INTO provider_service_areas (provider_id, service_area_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`, me, uuid.UUID(a)); err != nil {
					return err
				}
			}
		}
		return audit(ctx, tx, "provider.profile_update", "provider", me, nil, nil, "")
	})
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	s.GetProviderProfile(w, r)
}

func uuidPtr(u *gen.Id) *uuid.UUID {
	if u == nil {
		return nil
	}
	v := uuid.UUID(*u)
	return &v
}

func nilIfEmpty(s []string) any {
	if len(s) == 0 {
		return nil
	}
	return s
}

// ---- checklist

func (s *Server) GetProviderChecklistsById(w http.ResponseWriter, r *http.Request, categoryId gen.Id) {
	raw, err := s.currentChecklist(r.Context(), s.DB, uuid.UUID(categoryId))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, raw)
}

const checklistItemObj = `jsonb_build_object('id', i.id, 'kind', i.kind, 'document_type', i.document_type, 'label', i.label, 'is_mandatory', i.is_mandatory,
	'min_count', i.min_count, 'validity_months', i.validity_months, 'sort_order', i.sort_order)`

const checklistJSON = `
	SELECT jsonb_build_object('id', t.id, 'category_id', t.category_id, 'version', t.version, 'is_current', t.is_current, 'state', 'current', 'approved_by', t.approved_by,
	  'created_at', t.created_at,
	  'items', COALESCE((SELECT jsonb_agg(` + checklistItemObj + ` ORDER BY i.sort_order, i.label) FROM checklist_template_items i WHERE i.template_id = t.id), '[]'::jsonb))
	FROM checklist_templates t`

func (s *Server) currentChecklist(ctx context.Context, q db, categoryID uuid.UUID) (json.RawMessage, error) {
	return rawOne(ctx, q, checklistJSON+` WHERE t.category_id = $1 AND t.is_current`, categoryID)
}

// ---- applications

const applicationSummaryObj = `jsonb_build_object('id', a.id, 'reference', a.reference, 'provider_id', a.provider_id, 'provider_name', u.full_name,
	'category_id', a.category_id, 'status', a.status, 'submitted_at', a.submitted_at, 'vetting_officer_id', a.vetting_officer_id,
	'is_reverification', a.is_reverification,
	'checklist_progress', jsonb_build_object(
	  'satisfied', (SELECT count(*) FROM application_items x WHERE x.application_id = a.id AND x.status IN ('satisfied','waived')),
	  'total', (SELECT count(*) FROM application_items x WHERE x.application_id = a.id)))`

const applicationSummaryFrom = ` FROM applications a JOIN users u ON u.id = a.provider_id`

// applicationOfficerObj adds the vetting officer's name, which only staff see.
const applicationOfficerObj = ` || jsonb_build_object('vetting_officer_name', (SELECT o.full_name FROM users o WHERE o.id = a.vetting_officer_id))`

func (s *Server) PostProviderApplications(w http.ResponseWriter, r *http.Request) {
	in, err := httpx.Decode[gen.ApplicationCreate](r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	ctx := r.Context()
	me := auth.MustPrincipal(ctx).UserID
	var id uuid.UUID
	err = dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		var pstatus string
		if err := tx.QueryRow(ctx, `SELECT status::text FROM providers WHERE user_id = $1 FOR UPDATE`, me).Scan(&pstatus); err != nil {
			return err
		}
		if pstatus == "suspended" || pstatus == "disqualified" || pstatus == "withdrawn" {
			return httpx.RuleViolation("Your account is " + pstatus + ", so you cannot apply for a category")
		}
		var active bool
		if err := tx.QueryRow(ctx, `SELECT is_active FROM service_categories WHERE id = $1`, uuid.UUID(in.CategoryId)).Scan(&active); err != nil {
			return err
		}
		if !active {
			return httpx.RuleViolation("This category is not taking applications yet")
		}
		var already int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM provider_categories WHERE provider_id = $1 AND category_id = $2 AND status IN ('approved','active')`, me, uuid.UUID(in.CategoryId)).Scan(&already); err != nil {
			return err
		}
		if already > 0 {
			return httpx.Conflict("You are already approved for this category")
		}
		var tpl uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT id FROM checklist_templates WHERE category_id = $1 AND is_current`, uuid.UUID(in.CategoryId)).Scan(&tpl); err != nil {
			return httpx.RuleViolation("Applications for this category are not open yet")
		}
		answers := []byte("{}")
		if in.Answers != nil {
			answers, _ = json.Marshal(*in.Answers)
			if err := s.fillProfileFromAnswers(ctx, tx, me, *in.Answers); err != nil {
				return err
			}
		}
		if err := tx.QueryRow(ctx, `INSERT INTO applications (provider_id, category_id, template_id, answers) VALUES ($1,$2,$3,$4) RETURNING id`,
			me, uuid.UUID(in.CategoryId), tpl, answers).Scan(&id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO application_items (application_id, template_item_id) SELECT $1, id FROM checklist_template_items WHERE template_id = $2`, id, tpl); err != nil {
			return err
		}
		return audit(ctx, tx, "application.create", "application", id, nil, map[string]any{"category_id": in.CategoryId}, "")
	})
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	s.respondApplication(w, r, id, false, http.StatusCreated)
}

// fillProfileFromAnswers checks the profile questions on an application (date_of_birth,
// years_experience, home_service_area_id, languages) and copies them to the provider profile where
// the profile has nothing yet, so the provider does not type them twice. Other answers are kept
// on the application only.
func (s *Server) fillProfileFromAnswers(ctx context.Context, tx pgx.Tx, me uuid.UUID, a map[string]any) error {
	var dob *time.Time
	if v, ok := a["date_of_birth"].(string); ok && v != "" {
		d, err := time.Parse("2006-01-02", v)
		if err != nil {
			return httpx.Invalid("date_of_birth must be a date (YYYY-MM-DD)")
		}
		if d.AddDate(18, 0, 0).After(s.Now()) {
			return httpx.Invalid("You must be at least 18 to apply")
		}
		if d.Before(s.Now().AddDate(-90, 0, 0)) {
			return httpx.Invalid("Check your date of birth")
		}
		dob = &d
	}
	var years *int
	switch v := a["years_experience"].(type) {
	case float64:
		n := int(v)
		years = &n
	case string:
		if v != "" {
			n, err := strconv.Atoi(v)
			if err != nil {
				return httpx.Invalid("years_experience must be a whole number")
			}
			years = &n
		}
	}
	if years != nil && (*years < 0 || *years > 60) {
		return httpx.Invalid("years_experience must be between 0 and 60")
	}
	var area *uuid.UUID
	if v, ok := a["home_service_area_id"].(string); ok && v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			return httpx.Invalid("home_service_area_id is not a service area")
		}
		area = &id
	}
	var langs []string
	if v, ok := a["languages"].([]any); ok {
		for _, l := range v {
			if str, ok := l.(string); ok && strings.TrimSpace(str) != "" {
				langs = append(langs, strings.TrimSpace(str))
			}
		}
	}
	_, err := tx.Exec(ctx, `UPDATE providers SET
		date_of_birth = COALESCE(date_of_birth, $2),
		years_experience = COALESCE(years_experience, $3),
		home_service_area_id = COALESCE(home_service_area_id, $4),
		languages = CASE WHEN $5::text[] IS NOT NULL AND languages = '{en}' THEN $5 ELSE languages END
		WHERE user_id = $1`, me, dob, years, area, langs)
	return mapErr(err)
}

func (s *Server) GetProviderApplications(w http.ResponseWriter, r *http.Request) {
	out, err := rawAll(r.Context(), s.DB, `SELECT `+applicationSummaryObj+applicationSummaryFrom+` WHERE a.provider_id = $1 ORDER BY a.created_at DESC`, auth.MustPrincipal(r.Context()).UserID)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (s *Server) GetProviderApplicationsById(w http.ResponseWriter, r *http.Request, id gen.Id) {
	var owner uuid.UUID
	if err := s.DB.QueryRow(r.Context(), `SELECT provider_id FROM applications WHERE id = $1`, uuid.UUID(id)).Scan(&owner); err != nil || owner != auth.MustPrincipal(r.Context()).UserID {
		httpx.Fail(w, r, httpx.ErrNotFound)
		return
	}
	s.respondApplication(w, r, uuid.UUID(id), false, http.StatusOK)
}

func (s *Server) respondApplication(w http.ResponseWriter, r *http.Request, id uuid.UUID, staff bool, status int) {
	app, err := s.loadApplication(r.Context(), s.DB, id, staff)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, status, app)
}

// loadApplication builds the Application resource. Staff also see the vetting report, the referees'
// statements, clearance checks and assessments, none of which the applicant sees.
func (s *Server) loadApplication(ctx context.Context, q db, id uuid.UUID, staff bool) (map[string]any, error) {
	officer := ""
	if staff {
		officer = applicationOfficerObj
	}
	raw, err := rawOne(ctx, q, `SELECT `+applicationSummaryObj+officer+` || jsonb_build_object('answers', a.answers, 'decided_by', a.decided_by,
		'decision_reason', a.decision_reason, 'decided_at', a.decided_at, 'vetting_report', a.vetting_report)`+applicationSummaryFrom+` WHERE a.id = $1`, id)
	if err != nil {
		return nil, err
	}
	var app map[string]any
	if err := json.Unmarshal(raw, &app); err != nil {
		return nil, err
	}
	if !staff {
		delete(app, "vetting_report")
	}
	items, err := rawAll(ctx, q, `
		SELECT jsonb_build_object('id', x.id, 'template_item', `+checklistItemObj+`, 'status', x.status, 'document_id', x.document_id, 'notes', x.notes,
		  'reviewed_by', x.reviewed_by, 'reviewed_at', x.reviewed_at)
		FROM application_items x JOIN checklist_template_items i ON i.id = x.template_item_id WHERE x.application_id = $1 ORDER BY i.sort_order, i.label`, id)
	if err != nil {
		return nil, err
	}
	app["items"] = items

	rows, err := q.Query(ctx, `SELECT id, full_name, relationship, phone_enc, employer_name, employment_from, employment_to, outcome::text, statement_enc, contacted_by, contacted_at
		FROM referees WHERE application_id = $1 ORDER BY created_at`, id)
	if err != nil {
		return nil, err
	}
	type refRow struct {
		id                    uuid.UUID
		name, rel, outcome    string
		phoneEnc, stmtEnc     []byte
		employer              *string
		from, to, contactedAt *time.Time
		contactedBy           *uuid.UUID
	}
	var rr []refRow
	for rows.Next() {
		var x refRow
		if err := rows.Scan(&x.id, &x.name, &x.rel, &x.phoneEnc, &x.employer, &x.from, &x.to, &x.outcome, &x.stmtEnc, &x.contactedBy, &x.contactedAt); err != nil {
			rows.Close()
			return nil, err
		}
		rr = append(rr, x)
	}
	rows.Close()
	referees := []map[string]any{}
	for _, x := range rr {
		phone, err := s.Cipher.Decrypt(x.phoneEnc)
		if err != nil {
			return nil, err
		}
		m := map[string]any{"id": x.id, "full_name": x.name, "relationship": x.rel, "phone": string(phone), "employer_name": x.employer,
			"employment_from": dateOrNil(x.from), "employment_to": dateOrNil(x.to)}
		if staff {
			m["outcome"], m["contacted_by"], m["contacted_at"] = x.outcome, x.contactedBy, x.contactedAt
			if x.stmtEnc != nil {
				st, err := s.Cipher.Decrypt(x.stmtEnc)
				if err != nil {
					return nil, err
				}
				m["statement"] = string(st)
			} else {
				m["statement"] = nil
			}
		} else {
			m["outcome"] = "pending" // the applicant is not told what a referee said
		}
		referees = append(referees, m)
	}
	app["referees"] = referees

	app["clearance_checks"], app["assessments"] = []json.RawMessage{}, []json.RawMessage{}
	if staff {
		if app["clearance_checks"], err = rawAll(ctx, q, clearanceJSON+` WHERE c.application_id = $1 ORDER BY c.requested_at`, id); err != nil {
			return nil, err
		}
		if app["assessments"], err = rawAll(ctx, q, `
			SELECT jsonb_build_object('id', x.id, 'kind', x.kind, 'rubric_version', x.rubric_version, 'scores', x.scores, 'total_score', x.total_score,
			  'passed', x.passed, 'notes', x.notes, 'assessed_by', x.assessed_by, 'assessed_at', x.assessed_at)
			FROM assessments x WHERE x.application_id = $1 ORDER BY x.assessed_at`, id); err != nil {
			return nil, err
		}
	}
	status, _ := app["status"].(string)
	missing := []string{}
	if status == "draft" || status == "incomplete" || status == "deferred" {
		if missing, err = s.submissionGaps(ctx, q, id); err != nil {
			return nil, err
		}
	}
	app["missing"] = missing
	return compact(app), nil
}

const clearanceJSON = `
	SELECT jsonb_build_object('id', c.id, 'document_id', c.document_id, 'agency_name', c.agency_name, 'agency_reference', c.agency_reference, 'result', c.result,
	  'requested_at', c.requested_at, 'confirmed_at', c.confirmed_at, 'recorded_by', c.recorded_by, 'notes', c.notes)
	FROM clearance_checks c`

func dateOrNil(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.Format("2006-01-02")
}

// documentKinds are the checklist kinds the applicant satisfies by attaching a file.
func isDocumentKind(kind string) bool {
	return kind == "document" || kind == "trade_licence" || kind == "medical"
}

// submissionGaps lists what the applicant still has to supply before the application can be submitted.
func (s *Server) submissionGaps(ctx context.Context, q db, appID uuid.UUID) ([]string, error) {
	rows, err := q.Query(ctx, `
		SELECT i.kind::text, i.label, i.min_count, x.document_id IS NOT NULL AND d.status IN ('uploaded','verified'),
		  (SELECT count(*) FROM referees r WHERE r.application_id = x.application_id)
		FROM application_items x JOIN checklist_template_items i ON i.id = x.template_item_id LEFT JOIN documents d ON d.id = x.document_id
		WHERE x.application_id = $1 AND i.is_mandatory ORDER BY i.sort_order, i.label`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	gaps := []string{}
	for rows.Next() {
		var kind, label string
		var minCount int
		var hasDoc bool
		var refs int
		if err := rows.Scan(&kind, &label, &minCount, &hasDoc, &refs); err != nil {
			return nil, err
		}
		switch {
		case isDocumentKind(kind) && !hasDoc:
			gaps = append(gaps, label)
		case kind == "referee" && refs < minCount:
			gaps = append(gaps, label)
		}
	}
	return gaps, rows.Err()
}

// lockOwnApplication loads the applicant's application for update and checks it can still be edited.
func lockOwnApplication(ctx context.Context, tx pgx.Tx, id, me uuid.UUID, editable ...string) (string, error) {
	var owner uuid.UUID
	var status string
	if err := tx.QueryRow(ctx, `SELECT provider_id, status::text FROM applications WHERE id = $1 FOR UPDATE`, id).Scan(&owner, &status); err != nil || owner != me {
		return "", httpx.ErrNotFound
	}
	for _, e := range editable {
		if status == e {
			return status, nil
		}
	}
	return status, httpx.Conflict("This application cannot be changed while it is " + status)
}

func (s *Server) PatchProviderApplicationsById(w http.ResponseWriter, r *http.Request, id gen.Id) {
	in, err := httpx.Decode[gen.ApplicationUpdate](r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	ctx := r.Context()
	me := auth.MustPrincipal(ctx).UserID
	err = dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		if _, err := lockOwnApplication(ctx, tx, uuid.UUID(id), me, "draft", "incomplete", "deferred"); err != nil {
			return err
		}
		if in.Answers != nil {
			b, _ := json.Marshal(*in.Answers)
			if _, err := tx.Exec(ctx, `UPDATE applications SET answers = $2 WHERE id = $1`, uuid.UUID(id), b); err != nil {
				return err
			}
		}
		if in.ItemDocuments != nil {
			for _, link := range *in.ItemDocuments {
				var kind string
				var wantType *string
				if err := tx.QueryRow(ctx, `SELECT i.kind::text, i.document_type::text FROM application_items x JOIN checklist_template_items i ON i.id = x.template_item_id
					WHERE x.id = $1 AND x.application_id = $2`, uuid.UUID(link.ApplicationItemId), uuid.UUID(id)).Scan(&kind, &wantType); err != nil {
					return httpx.Invalid("application_item_id does not belong to this application")
				}
				if !isDocumentKind(kind) {
					return httpx.Invalid("That checklist item is not completed by attaching a document")
				}
				var docType, docStatus string
				if err := tx.QueryRow(ctx, `SELECT document_type::text, status::text FROM documents WHERE id = $1 AND owner_user_id = $2`, uuid.UUID(link.DocumentId), me).Scan(&docType, &docStatus); err != nil {
					return httpx.Invalid("document_id is not one of your documents")
				}
				if docStatus != "uploaded" && docStatus != "verified" {
					return httpx.Invalid("Finish uploading the document before attaching it")
				}
				if wantType != nil && *wantType != docType {
					return httpx.Invalid("This item needs a " + strings.ReplaceAll(*wantType, "_", " "))
				}
				if _, err := tx.Exec(ctx, `UPDATE application_items SET document_id = $2, status = 'submitted', notes = NULL, reviewed_by = NULL, reviewed_at = NULL WHERE id = $1`,
					uuid.UUID(link.ApplicationItemId), uuid.UUID(link.DocumentId)); err != nil {
					return err
				}
			}
		}
		return audit(ctx, tx, "application.update", "application", uuid.UUID(id), nil, nil, "")
	})
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	s.respondApplication(w, r, uuid.UUID(id), false, http.StatusOK)
}

func (s *Server) PostProviderApplicationsReferees(w http.ResponseWriter, r *http.Request, id gen.Id) {
	in, err := httpx.Decode[gen.RefereeInput](r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if strings.TrimSpace(in.FullName) == "" || strings.TrimSpace(in.Relationship) == "" || !phoneRE.MatchString(in.Phone) {
		httpx.Fail(w, r, httpx.Invalid("full_name, relationship and a phone in international format are required"))
		return
	}
	if err := s.checkReferee(in); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	enc, err := s.Cipher.Encrypt([]byte(in.Phone))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	ctx := r.Context()
	me := auth.MustPrincipal(ctx).UserID
	var refID uuid.UUID
	var from, to *time.Time
	if in.EmploymentFrom != nil {
		from = &in.EmploymentFrom.Time
	}
	if in.EmploymentTo != nil {
		to = &in.EmploymentTo.Time
	}
	err = dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		if _, err := lockOwnApplication(ctx, tx, uuid.UUID(id), me, "draft", "incomplete", "deferred"); err != nil {
			return err
		}
		var own *string
		if err := tx.QueryRow(ctx, `SELECT phone_e164 FROM users WHERE id = $1`, me).Scan(&own); err != nil {
			return err
		}
		if own != nil && *own == in.Phone {
			return httpx.Invalid("A referee must be someone else: this is your own phone number")
		}
		var dup int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM referees WHERE application_id = $1 AND lower(full_name) = lower($2)`, uuid.UUID(id), strings.TrimSpace(in.FullName)).Scan(&dup); err != nil {
			return err
		}
		if dup > 0 {
			return httpx.Invalid("You have already added a referee with this name")
		}
		return tx.QueryRow(ctx, `INSERT INTO referees (application_id, full_name, relationship, phone_enc, employer_name, employment_from, employment_to)
			VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id`, uuid.UUID(id), strings.TrimSpace(in.FullName), strings.TrimSpace(in.Relationship), enc, trimPtr(in.EmployerName), from, to).Scan(&refID)
	})
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	app, err := s.loadApplication(ctx, s.DB, uuid.UUID(id), false)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	for _, x := range app["referees"].([]map[string]any) {
		if x["id"] == refID {
			httpx.JSON(w, http.StatusCreated, x)
			return
		}
	}
	httpx.Fail(w, r, httpx.ErrNotFound)
}

// minEmployment is the shortest working relationship a referee can speak to.
const minEmployment = 30 * 24 * time.Hour

// checkReferee rejects referees that cannot vouch for the applicant: a first name only, or an
// employment period that has not happened yet, runs backwards or is too short to judge someone.
func (s *Server) checkReferee(in gen.RefereeInput) error {
	if len(strings.Fields(in.FullName)) < 2 {
		return httpx.Invalid("Give the referee's full name (first name and surname)")
	}
	today := s.Now()
	var from, to *time.Time
	if in.EmploymentFrom != nil {
		from = &in.EmploymentFrom.Time
	}
	if in.EmploymentTo != nil {
		to = &in.EmploymentTo.Time
	}
	switch {
	case from != nil && from.After(today):
		return httpx.Invalid("employment_from cannot be in the future")
	case to != nil && to.After(today):
		return httpx.Invalid("employment_to cannot be in the future; leave it empty if you still work there")
	case from != nil && to != nil && !to.After(*from):
		return httpx.Invalid("employment_to must be after employment_from")
	case from != nil && to != nil && to.Sub(*from) < minEmployment:
		return httpx.Invalid("The employment period must be at least a month")
	case from == nil && to != nil:
		return httpx.Invalid("Give the date the employment started")
	}
	return nil
}

func (s *Server) DeleteProviderApplicationsRefereesById(w http.ResponseWriter, r *http.Request, id gen.Id, refereeId gen.Id) {
	ctx := r.Context()
	me := auth.MustPrincipal(ctx).UserID
	err := dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		if _, err := lockOwnApplication(ctx, tx, uuid.UUID(id), me, "draft"); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `DELETE FROM referees WHERE id = $1 AND application_id = $2`, uuid.UUID(refereeId), uuid.UUID(id))
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return httpx.ErrNotFound
		}
		return nil
	})
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	httpx.NoContent(w)
}

func (s *Server) PostProviderApplicationsSubmit(w http.ResponseWriter, r *http.Request, id gen.Id) {
	ctx := r.Context()
	me := auth.MustPrincipal(ctx).UserID
	err := dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		status, err := lockOwnApplication(ctx, tx, uuid.UUID(id), me, "draft", "incomplete", "deferred")
		if err != nil {
			return err
		}
		gaps, err := s.submissionGaps(ctx, tx, uuid.UUID(id))
		if err != nil {
			return err
		}
		if len(gaps) > 0 {
			return httpx.RuleViolation("The application is not complete yet").With("missing", gaps)
		}
		if _, err := tx.Exec(ctx, `UPDATE applications SET status = 'submitted', submitted_at = now() WHERE id = $1`, uuid.UUID(id)); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE application_items SET status = 'submitted' WHERE application_id = $1 AND status = 'pending' AND document_id IS NOT NULL`, uuid.UUID(id)); err != nil {
			return err
		}
		return audit(ctx, tx, "application.submit", "application", uuid.UUID(id), map[string]any{"status": status}, map[string]any{"status": "submitted"}, "")
	})
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	s.respondApplication(w, r, uuid.UUID(id), false, http.StatusOK)
}

func (s *Server) PostProviderApplicationsWithdraw(w http.ResponseWriter, r *http.Request, id gen.Id) {
	ctx := r.Context()
	me := auth.MustPrincipal(ctx).UserID
	err := dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		status, err := lockOwnApplication(ctx, tx, uuid.UUID(id), me, "draft", "submitted", "incomplete", "in_vetting", "awaiting_approval", "deferred")
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE applications SET status = 'withdrawn' WHERE id = $1`, uuid.UUID(id)); err != nil {
			return err
		}
		return audit(ctx, tx, "application.withdraw", "application", uuid.UUID(id), map[string]any{"status": status}, map[string]any{"status": "withdrawn"}, "")
	})
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	s.respondApplication(w, r, uuid.UUID(id), false, http.StatusOK)
}

// ---- documents, training, contract

func (s *Server) GetProviderDocuments(w http.ResponseWriter, r *http.Request) {
	out, err := rawAll(r.Context(), s.DB, documentJSON+` WHERE d.owner_user_id = $1 AND d.purged_at IS NULL ORDER BY d.created_at DESC`, auth.MustPrincipal(r.Context()).UserID)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

const trainingJSON = `
	SELECT jsonb_build_object('id', t.id, 'provider_id', t.provider_id,
	  'course', jsonb_build_object('id', c.id, 'code', c.code, 'name', c.name, 'category_id', c.category_id, 'is_induction', c.is_induction, 'pass_mark', c.pass_mark, 'is_active', c.is_active),
	  'status', t.status, 'is_retraining', t.is_retraining, 'complaint_id', t.complaint_id, 'scheduled_for', t.scheduled_for, 'attended_at', t.attended_at, 'score', t.score)
	FROM training_enrolments t JOIN training_courses c ON c.id = t.course_id`

func (s *Server) GetProviderTrainings(w http.ResponseWriter, r *http.Request) {
	out, err := rawAll(r.Context(), s.DB, trainingJSON+` WHERE t.provider_id = $1 ORDER BY t.scheduled_for DESC NULLS LAST, t.created_at DESC`, auth.MustPrincipal(r.Context()).UserID)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (s *Server) contractOffer(ctx context.Context, q db, providerID uuid.UUID) (map[string]any, error) {
	cv := s.settingText(ctx, q, "contract.current_version", "2026-10")
	cc := s.settingText(ctx, q, "contract.code_of_conduct_version", "2026-10")
	var basis string
	if err := q.QueryRow(ctx, `SELECT engagement_basis::text FROM providers WHERE user_id = $1`, providerID).Scan(&basis); err != nil {
		return nil, err
	}
	out := map[string]any{"contract_version": cv, "code_of_conduct_version": cc, "engagement_basis": basis, "document_url": nil, "signed_at": nil}
	var id uuid.UUID
	var signed time.Time
	err := q.QueryRow(ctx, `SELECT id, signed_at FROM provider_contracts WHERE provider_id = $1 AND contract_version = $2 AND code_of_conduct_version = $3 AND terminated_at IS NULL
		ORDER BY signed_at DESC LIMIT 1`, providerID, cv, cc).Scan(&id, &signed)
	if err == nil {
		out["id"], out["signed_at"] = id, signed
	}
	return out, nil
}

func (s *Server) settingText(ctx context.Context, q db, key, def string) string {
	var raw json.RawMessage
	if q.QueryRow(ctx, `SELECT value FROM app_settings WHERE key = $1`, key).Scan(&raw) != nil {
		return def
	}
	var v string
	if json.Unmarshal(raw, &v) != nil {
		return def
	}
	return v
}

func (s *Server) GetProviderContract(w http.ResponseWriter, r *http.Request) {
	out, err := s.contractOffer(r.Context(), s.DB, auth.MustPrincipal(r.Context()).UserID)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

// PostProviderContractSign records the provider's agreement, proven by a code sent to their own phone
// or by their account password.
func (s *Server) PostProviderContractSign(w http.ResponseWriter, r *http.Request) {
	in, err := httpx.Decode[gen.ContractSign](r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	ctx := r.Context()
	me := auth.MustPrincipal(ctx).UserID
	offer, err := s.contractOffer(ctx, s.DB, me)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if in.ContractVersion != offer["contract_version"] || in.CodeOfConductVersion != offer["code_of_conduct_version"] {
		httpx.Fail(w, r, httpx.Conflict("These are not the current contract and code of conduct. Reload and read the latest versions."))
		return
	}
	if offer["signed_at"] != nil {
		httpx.Fail(w, r, httpx.Conflict("You have already signed this version"))
		return
	}
	var phone *string
	var status string
	if err := s.DB.QueryRow(ctx, `SELECT u.phone_e164, p.status::text FROM users u JOIN providers p ON p.user_id = u.id WHERE u.id = $1`, me).Scan(&phone, &status); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if status != "approved" && status != "active" {
		httpx.Fail(w, r, httpx.RuleViolation("You can sign once your application is approved"))
		return
	}
	method := "phone_code"
	switch {
	case in.Password != nil:
		// Without SMS the provider signs by entering their account password again.
		method = "password"
		if err := s.checkPassword(ctx, me, *in.Password); err != nil {
			httpx.Fail(w, r, err)
			return
		}
	case in.OtpId != nil && in.Code != nil:
		otpPhone, purpose, err := s.checkOTP(ctx, uuid.UUID(*in.OtpId), *in.Code)
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		if purpose != "verify_phone" || phone == nil || otpPhone != *phone {
			httpx.Fail(w, r, errInvalidCode)
			return
		}
	default:
		httpx.Fail(w, r, httpx.Invalid("Confirm with the code sent to your phone or with your password"))
		return
	}
	err = dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `
			INSERT INTO provider_contracts (provider_id, contract_version, code_of_conduct_version, engagement_basis, signature_ip)
			SELECT $1, $2, $3, engagement_basis, $4::inet FROM providers WHERE user_id = $1 RETURNING id`, me, in.ContractVersion, in.CodeOfConductVersion, nullIfEmpty(clientIP(r))).Scan(&id); err != nil {
			return err
		}
		return audit(ctx, tx, "contract.sign", "provider_contract", id, nil, map[string]any{"contract_version": in.ContractVersion, "code_of_conduct_version": in.CodeOfConductVersion, "signed_with": method}, "")
	})
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	out, err := s.contractOffer(ctx, s.DB, me)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// ---- availability and time off

var hhmm = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)

func (s *Server) availability(ctx context.Context, q db, providerID uuid.UUID) (map[string]any, error) {
	weekly, err := rawAll(ctx, q, `
		SELECT jsonb_build_object('day_of_week', day_of_week, 'start_time', to_char(start_time, 'HH24:MI'), 'end_time', to_char(end_time, 'HH24:MI'))
		FROM provider_availability WHERE provider_id = $1 ORDER BY day_of_week, start_time`, providerID)
	return map[string]any{"weekly": weekly}, err
}

func (s *Server) GetProviderAvailability(w http.ResponseWriter, r *http.Request) {
	out, err := s.availability(r.Context(), s.DB, auth.MustPrincipal(r.Context()).UserID)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (s *Server) PutProviderAvailability(w http.ResponseWriter, r *http.Request) {
	in, err := httpx.Decode[gen.Availability](r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	for i, a := range in.Weekly {
		if a.DayOfWeek < 0 || a.DayOfWeek > 6 || !hhmm.MatchString(a.StartTime) || !hhmm.MatchString(a.EndTime) || a.EndTime <= a.StartTime {
			httpx.Fail(w, r, httpx.Invalid("Each window needs a day 0-6 (0 = Sunday) and start_time before end_time as HH:MM"))
			return
		}
		for _, b := range in.Weekly[:i] {
			if a.DayOfWeek == b.DayOfWeek && a.StartTime < b.EndTime && b.StartTime < a.EndTime {
				httpx.Fail(w, r, httpx.Invalid("Two windows on the same day overlap"))
				return
			}
		}
	}
	ctx := r.Context()
	me := auth.MustPrincipal(ctx).UserID
	err = dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM provider_availability WHERE provider_id = $1`, me); err != nil {
			return err
		}
		for _, a := range in.Weekly {
			if _, err := tx.Exec(ctx, `INSERT INTO provider_availability (provider_id, day_of_week, start_time, end_time) VALUES ($1,$2,$3::time,$4::time)`, me, a.DayOfWeek, a.StartTime, a.EndTime); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	s.GetProviderAvailability(w, r)
}

const timeOffJSON = `SELECT jsonb_build_object('id', t.id, 'starts_at', lower(t.period), 'ends_at', upper(t.period), 'reason', t.reason) FROM provider_time_off t`

func (s *Server) GetProviderTimeOff(w http.ResponseWriter, r *http.Request) {
	out, err := rawAll(r.Context(), s.DB, timeOffJSON+` WHERE t.provider_id = $1 AND upper(t.period) > now() ORDER BY lower(t.period)`, auth.MustPrincipal(r.Context()).UserID)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (s *Server) PostProviderTimeOff(w http.ResponseWriter, r *http.Request) {
	in, err := httpx.Decode[gen.TimeOffInput](r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if !in.EndsAt.After(in.StartsAt) || !in.EndsAt.After(s.Now()) {
		httpx.Fail(w, r, httpx.Invalid("ends_at must be after starts_at and in the future"))
		return
	}
	ctx := r.Context()
	me := auth.MustPrincipal(ctx).UserID
	var id uuid.UUID
	err = dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		var jobs int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM visits WHERE provider_id = $1 AND status = 'scheduled' AND tstzrange(scheduled_start, scheduled_end) && tstzrange($2, $3)`, me, in.StartsAt, in.EndsAt).Scan(&jobs); err != nil {
			return err
		}
		if jobs > 0 {
			return httpx.Conflict("You have a booked visit in this period. Ask management to reassign it first.")
		}
		return tx.QueryRow(ctx, `INSERT INTO provider_time_off (provider_id, period, reason) VALUES ($1, tstzrange($2,$3), $4) RETURNING id`, me, in.StartsAt, in.EndsAt, trimPtr(in.Reason)).Scan(&id)
	})
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	raw, err := rawOne(ctx, s.DB, timeOffJSON+` WHERE t.id = $1`, id)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, raw)
}

func (s *Server) DeleteProviderTimeOffById(w http.ResponseWriter, r *http.Request, id gen.Id) {
	tag, err := s.DB.Exec(r.Context(), `DELETE FROM provider_time_off WHERE id = $1 AND provider_id = $2`, uuid.UUID(id), auth.MustPrincipal(r.Context()).UserID)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if tag.RowsAffected() == 0 {
		httpx.Fail(w, r, httpx.ErrNotFound)
		return
	}
	httpx.NoContent(w)
}
