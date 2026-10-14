package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/KING-CYBERTON/home-residency/backend/internal/api/gen"
	"github.com/KING-CYBERTON/home-residency/backend/internal/auth"
	dbpkg "github.com/KING-CYBERTON/home-residency/backend/internal/db"
	"github.com/KING-CYBERTON/home-residency/backend/internal/httpx"
)

const staffJSON = `
	SELECT jsonb_build_object('id', u.id, 'full_name', u.full_name, 'email', u.email, 'phone', u.phone_e164, 'status', u.status,
	  'roles', COALESCE((SELECT jsonb_agg(sr.role ORDER BY sr.role) FROM staff_roles sr WHERE sr.user_id = u.id), '[]'::jsonb),
	  'last_login_at', u.last_login_at, 'created_at', u.created_at)
	FROM users u`

func (s *Server) GetConsoleStaff(w http.ResponseWriter, r *http.Request, params gen.GetConsoleStaffParams) {
	b := &qb{}
	b.raw("u.kind = 'staff'")
	if params.Role != nil {
		b.add("EXISTS (SELECT 1 FROM staff_roles sr WHERE sr.user_id = u.id AND sr.role = ?::staff_role)", string(*params.Role))
	}
	if params.Status != nil {
		b.add("u.status = ?::account_status", string(*params.Status))
	}
	out, err := rawAll(r.Context(), s.DB, staffJSON+` WHERE `+b.where()+` ORDER BY u.full_name, u.id`, b.args...)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (s *Server) staffOne(r *http.Request, id uuid.UUID) (json.RawMessage, error) {
	return rawOne(r.Context(), s.DB, staffJSON+` WHERE u.id = $1 AND u.kind = 'staff'`, id)
}

// PostConsoleStaff creates the account with a generated password and authenticator secret. They are
// in the response once, for the admin to hand over; only hashes and ciphertext are stored.
func (s *Server) PostConsoleStaff(w http.ResponseWriter, r *http.Request) {
	in, err := httpx.Decode[gen.StaffCreate](r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	name := strings.TrimSpace(in.FullName)
	phone := ""
	if in.Phone != nil {
		phone = *in.Phone
		if !phoneRE.MatchString(phone) {
			httpx.Fail(w, r, httpx.Invalid("Phone must be in international format, e.g. +254712345678"))
			return
		}
	}
	if len(name) < 2 || len(in.Roles) == 0 {
		httpx.Fail(w, r, httpx.Invalid("full_name and at least one role are required"))
		return
	}
	roles := make([]string, len(in.Roles))
	for i, x := range in.Roles {
		roles[i] = string(x)
	}
	id, pw, secret, uri, err := s.createStaff(r.Context(), name, strings.ToLower(string(in.Email)), phone, roles)
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	raw, err := s.staffOne(r, id)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	m["initial_credentials"] = map[string]string{"password": pw, "totp_secret": secret, "totp_uri": uri}
	httpx.JSON(w, http.StatusCreated, m)
}

func (s *Server) PatchConsoleStaffById(w http.ResponseWriter, r *http.Request, id gen.Id) {
	in, err := httpx.Decode[gen.StaffUpdate](r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if in.Phone != nil && *in.Phone != "" && !phoneRE.MatchString(*in.Phone) {
		httpx.Fail(w, r, httpx.Invalid("Phone must be in international format"))
		return
	}
	if in.FullName != nil && len(strings.TrimSpace(*in.FullName)) < 2 {
		httpx.Fail(w, r, httpx.Invalid("full_name is too short"))
		return
	}
	ctx := r.Context()
	uid := uuid.UUID(id)
	var status *string
	if in.Status != nil {
		st := string(*in.Status)
		status = &st
		if p := auth.FromContext(ctx); p != nil && p.UserID == uid && st != "active" {
			httpx.Fail(w, r, httpx.Conflict("You cannot suspend or close your own account"))
			return
		}
	}
	err = dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE users SET full_name = COALESCE($2, full_name), phone_e164 = CASE WHEN $3::text IS NULL THEN phone_e164 ELSE NULLIF($3,'') END,
			status = COALESCE($4::account_status, status) WHERE id = $1 AND kind = 'staff'`, uid, in.FullName, in.Phone, status)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return httpx.ErrNotFound
		}
		if status != nil && *status != "active" {
			if _, err := tx.Exec(ctx, `UPDATE sessions SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL`, uid); err != nil {
				return err
			}
		}
		return audit(ctx, tx, "staff.update", "user", uid, nil, in, "")
	})
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	raw, err := s.staffOne(r, uid)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, raw)
}

func (s *Server) PutConsoleStaffRoles(w http.ResponseWriter, r *http.Request, id gen.Id) {
	in, err := httpx.Decode[gen.RolesInput](r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if len(in.Roles) == 0 {
		httpx.Fail(w, r, httpx.Invalid("A staff user needs at least one role"))
		return
	}
	ctx := r.Context()
	uid := uuid.UUID(id)
	roles := make([]string, 0, len(in.Roles))
	for _, x := range in.Roles {
		roles = append(roles, string(x))
	}
	err = dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		var old []string
		if err := tx.QueryRow(ctx, `SELECT COALESCE(array_agg(sr.role::text), '{}') FROM users u LEFT JOIN staff_roles sr ON sr.user_id = u.id
			WHERE u.id = $1 AND u.kind = 'staff' GROUP BY u.id`, uid).Scan(&old); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM staff_roles WHERE user_id = $1 AND NOT (role::text = ANY($2))`, uid, roles); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO staff_roles (user_id, role, granted_by) SELECT $1, r::staff_role, $3 FROM unnest($2::text[]) r
			ON CONFLICT DO NOTHING`, uid, roles, actorID(ctx)); err != nil {
			return err
		}
		var admins int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM staff_roles sr JOIN users u ON u.id = sr.user_id
			WHERE sr.role = 'admin' AND u.status = 'active'`).Scan(&admins); err != nil {
			return err
		}
		if admins == 0 {
			return httpx.Conflict("There must always be at least one active admin")
		}
		if _, err := tx.Exec(ctx, `UPDATE sessions SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL AND user_id <> $2`, uid, actorID(ctx)); err != nil {
			return err
		}
		return audit(ctx, tx, "staff.roles", "user", uid, map[string]any{"roles": old}, map[string]any{"roles": roles}, "")
	})
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	raw, err := s.staffOne(r, uid)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, raw)
}

// PostConsoleStaffResetMfa issues a fresh authenticator secret and signs the user out everywhere.
func (s *Server) PostConsoleStaffResetMfa(w http.ResponseWriter, r *http.Request, id gen.Id) {
	ctx := r.Context()
	uid := uuid.UUID(id)
	var email string
	if err := s.DB.QueryRow(ctx, `SELECT email FROM users WHERE id = $1 AND kind = 'staff'`, uid).Scan(&email); err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	secret, uri, err := auth.NewTOTP(email)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	enc, err := s.Cipher.Encrypt([]byte(secret))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	err = dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE users SET totp_secret_enc = $2 WHERE id = $1`, uid, enc); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE sessions SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL`, uid); err != nil {
			return err
		}
		return audit(ctx, tx, "staff.reset_mfa", "user", uid, nil, nil, "")
	})
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]string{"totp_secret": secret, "totp_uri": uri})
}

// ---- settings ----

const settingJSON = `SELECT jsonb_build_object('key', a.key, 'value', a.value, 'description', a.description, 'updated_by', a.updated_by, 'updated_at', a.updated_at) FROM app_settings a`

func (s *Server) GetConsoleSettings(w http.ResponseWriter, r *http.Request) {
	out, err := rawAll(r.Context(), s.DB, settingJSON+` ORDER BY a.key`)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (s *Server) PutConsoleSettingsById(w http.ResponseWriter, r *http.Request, key string) {
	in, err := httpx.Decode[gen.SettingUpdate](r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if in.Value == nil || strings.TrimSpace(key) == "" || len(key) > 100 {
		httpx.Fail(w, r, httpx.Invalid("A setting needs a key and a value"))
		return
	}
	val, _ := json.Marshal(in.Value)
	ctx := r.Context()
	err = dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		var before json.RawMessage
		_ = tx.QueryRow(ctx, `SELECT value FROM app_settings WHERE key = $1 FOR UPDATE`, key).Scan(&before)
		if _, err := tx.Exec(ctx, `INSERT INTO app_settings (key, value, updated_by) VALUES ($1, $2::jsonb, $3)
			ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_by = EXCLUDED.updated_by, updated_at = now()`, key, string(val), actorID(ctx)); err != nil {
			return err
		}
		// The audit log is keyed by uuid, so a setting is identified by a stable id derived from its key.
		return audit(ctx, tx, "setting.update", "app_setting", uuid.NewSHA1(uuid.NameSpaceOID, []byte(key)),
			map[string]any{"key": key, "value": before}, map[string]any{"key": key, "value": in.Value}, "")
	})
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	raw, err := rawOne(ctx, s.DB, settingJSON+` WHERE a.key = $1`, key)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, raw)
}

// ---- audit log and leads ----

func (s *Server) GetConsoleAuditLog(w http.ResponseWriter, r *http.Request, params gen.GetConsoleAuditLogParams) {
	cur, err := decodeCursor(params.Cursor)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	b := &qb{}
	if params.EntityType != nil {
		b.add("l.entity_type = ?", *params.EntityType)
	}
	if params.EntityId != nil {
		b.add("l.entity_id = ?", uuid.UUID(*params.EntityId))
	}
	if params.ActorId != nil {
		b.add("l.actor_id = ?", uuid.UUID(*params.ActorId))
	}
	if params.Action != nil {
		b.add("l.action = ?", *params.Action)
	}
	if params.From != nil {
		b.add("l.occurred_at >= ?", params.From.Time)
	}
	if params.To != nil {
		b.add("l.occurred_at < ?", params.To.Time.AddDate(0, 0, 1))
	}
	pg, err := pagedRaw(r.Context(), s.DB, `SELECT l.id, l.occurred_at, l.actor_id, l.actor_kind, l.action, l.entity_type, l.entity_id,
		l.before_state, l.after_state, l.reason, host(l.ip_address) AS ip_address,
		l.occurred_at AS _t, `+strings.Replace(bigUUID, "%s", "l.id", 1)+` AS _id FROM audit_log l WHERE `+b.where(), b.args, cur, pageLimit(params.Limit))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, pg)
}

func (s *Server) GetConsoleLeads(w http.ResponseWriter, r *http.Request, params gen.GetConsoleLeadsParams) {
	cur, err := decodeCursor(params.Cursor)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	b := &qb{}
	if params.From != nil {
		b.add("l.created_at >= ?", params.From.Time)
	}
	if params.To != nil {
		b.add("l.created_at < ?", params.To.Time.AddDate(0, 0, 1))
	}
	pg, err := pagedRaw(r.Context(), s.DB, `SELECT l.id, l.full_name, l.phone_e164 AS phone, l.email, l.service_area_id, l.category_id, l.message, l.source,
		l.created_at, l.created_at AS _t, l.id AS _id FROM leads l WHERE `+b.where(), b.args, cur, pageLimit(params.Limit))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, pg)
}
