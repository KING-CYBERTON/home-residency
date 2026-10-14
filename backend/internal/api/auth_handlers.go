package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/KING-CYBERTON/home-residency/backend/internal/api/gen"
	"github.com/KING-CYBERTON/home-residency/backend/internal/auth"
	dbpkg "github.com/KING-CYBERTON/home-residency/backend/internal/db"
	"github.com/KING-CYBERTON/home-residency/backend/internal/httpx"
)

var phoneRE = regexp.MustCompile(`^\+[1-9][0-9]{7,14}$`)

var errInvalidCode = httpx.NewError(http.StatusBadRequest, "invalid_code", "That code is wrong or has expired")

func (s *Server) PostAuthOtpRequest(w http.ResponseWriter, r *http.Request) {
	body, err := httpx.Decode[gen.OtpRequest](r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if !phoneRE.MatchString(body.Phone) {
		httpx.Fail(w, r, httpx.Invalid("Phone must be in international format, e.g. +254712345678"))
		return
	}
	ctx := r.Context()
	var lastMinute, lastHour int
	if err := s.DB.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE created_at > now() - interval '1 minute'), count(*)
		FROM otp_codes WHERE phone_e164 = $1 AND created_at > now() - interval '1 hour'`, body.Phone).Scan(&lastMinute, &lastHour); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if lastMinute > 0 || lastHour >= 5 {
		httpx.Fail(w, r, httpx.ErrRateLimited)
		return
	}
	code, err := s.Auth.NewCode()
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	id := uuid.New()
	ttl := time.Duration(s.setting(ctx, s.DB, "otp.ttl_minutes", 10)) * time.Minute
	exp := s.Now().Add(ttl)
	if _, err := s.DB.Exec(ctx, `INSERT INTO otp_codes (id, phone_e164, purpose, code_hash, expires_at) VALUES ($1,$2,$3,$4,$5)`,
		id, body.Phone, string(body.Purpose), s.Auth.HashCode(id, code), exp); err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	if err := s.SMS.Send(ctx, body.Phone, fmt.Sprintf("Your Homend Residence code is %s. It expires in %d minutes.", code, int(ttl.Minutes()))); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"otp_id": id, "expires_at": exp, "resend_after_seconds": 60})
}

func (s *Server) PostAuthOtpVerify(w http.ResponseWriter, r *http.Request) {
	body, err := httpx.Decode[gen.OtpVerify](r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	ctx := r.Context()
	phone, purpose, err := s.checkOTP(ctx, body.OtpId, body.Code)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	var userID uuid.UUID
	if purpose == "register" {
		userID, err = s.registerUser(ctx, r, phone, body)
	} else {
		userID, err = s.activateExisting(ctx, phone)
	}
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	ttl := time.Duration(s.setting(ctx, s.DB, "session.client_ttl_hours", 720)) * time.Hour
	sess, err := s.Auth.CreateSession(ctx, s.DB, userID, ttl, true, r.UserAgent(), clientIP(r))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	s.respondSession(w, r, sess, false, userID)
}

// checkOTP spends a one-time code: it counts the attempt first, atomically, so guessing cannot outrun
// the limit, then checks the code and consumes it. It returns the phone and purpose the code was issued for.
func (s *Server) checkOTP(ctx context.Context, otpID uuid.UUID, code string) (phone, purpose string, err error) {
	maxAttempts := int(s.setting(ctx, s.DB, "otp.max_attempts", 5))
	var stored []byte
	err = s.DB.QueryRow(ctx, `
		UPDATE otp_codes SET attempts = attempts + 1
		WHERE id = $1 AND consumed_at IS NULL AND expires_at > now() AND attempts < $2
		RETURNING phone_e164, purpose::text, code_hash`, otpID, maxAttempts).Scan(&phone, &purpose, &stored)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", errInvalidCode
	}
	if err != nil {
		return "", "", err
	}
	if !s.Auth.CodeMatches(otpID, code, stored) {
		return "", "", errInvalidCode
	}
	tag, err := s.DB.Exec(ctx, `UPDATE otp_codes SET consumed_at = now() WHERE id = $1 AND consumed_at IS NULL`, otpID)
	if err != nil || tag.RowsAffected() != 1 {
		return "", "", errInvalidCode
	}
	return phone, purpose, nil
}

func (s *Server) registerUser(ctx context.Context, r *http.Request, phone string, body gen.OtpVerify) (uuid.UUID, error) {
	name := ""
	if body.FullName != nil {
		name = strings.TrimSpace(*body.FullName)
	}
	if len(name) < 2 {
		return uuid.Nil, httpx.Invalid("full_name is required to register")
	}
	if body.Kind == nil || (*body.Kind != "client" && *body.Kind != "provider") {
		return uuid.Nil, httpx.Invalid("kind must be client or provider")
	}
	kind := string(*body.Kind)
	var id uuid.UUID
	err := dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `
			INSERT INTO users (kind, status, full_name, phone_e164, phone_verified_at)
			VALUES ($1::user_kind, 'active', $2, $3, now()) RETURNING id`, kind, name, phone).Scan(&id); err != nil {
			return err
		}
		if kind == "client" {
			if _, err := tx.Exec(ctx, `INSERT INTO clients (user_id) VALUES ($1)`, id); err != nil {
				return err
			}
		} else if _, err := tx.Exec(ctx, `INSERT INTO providers (user_id) VALUES ($1)`, id); err != nil {
			return err
		}
		if body.Consents != nil {
			for _, c := range *body.Consents {
				if _, err := tx.Exec(ctx, `INSERT INTO consents (user_id, purpose, notice_version, granted, ip_address) VALUES ($1,$2,$3,$4,NULLIF($5,'')::inet)`,
					id, string(c.Purpose), c.NoticeVersion, c.Granted, clientIP(r)); err != nil {
					return err
				}
			}
		}
		return audit(ctx, tx, "user.register", "user", id, nil, map[string]any{"kind": kind}, "")
	})
	if err != nil {
		var he *httpx.Error
		if e := mapErr(err); errors.As(e, &he) && he.Code == "invalid_state_transition" {
			return uuid.Nil, httpx.Conflict("An account with this phone number already exists. Sign in instead.")
		}
		return uuid.Nil, err
	}
	return id, nil
}

func (s *Server) activateExisting(ctx context.Context, phone string) (uuid.UUID, error) {
	var id uuid.UUID
	var kind, status string
	err := s.DB.QueryRow(ctx, `SELECT id, kind::text, status::text FROM users WHERE phone_e164 = $1`, phone).Scan(&id, &kind, &status)
	if errors.Is(err, pgx.ErrNoRows) || kind == "staff" {
		return uuid.Nil, errInvalidCode // same answer as a wrong code: no account enumeration
	}
	if err != nil {
		return uuid.Nil, err
	}
	switch status {
	case "suspended", "closed":
		return uuid.Nil, httpx.Forbidden("account_suspended", "This account is not active. Contact Homend Residence.")
	case "pending_verification":
		if _, err := s.DB.Exec(ctx, `UPDATE users SET status = 'active', phone_verified_at = now() WHERE id = $1`, id); err != nil {
			return uuid.Nil, err
		}
	}
	_, _ = s.DB.Exec(ctx, `UPDATE users SET last_login_at = now() WHERE id = $1`, id)
	return id, nil
}

func (s *Server) PostAuthStaffLogin(w http.ResponseWriter, r *http.Request) {
	body, err := httpx.Decode[gen.StaffLogin](r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	ctx := r.Context()
	var id uuid.UUID
	var hash *string
	var status string
	err = s.DB.QueryRow(ctx, `SELECT id, password_hash, status::text FROM users WHERE email = $1 AND kind = 'staff'`, string(body.Email)).Scan(&id, &hash, &status)
	bad := httpx.NewError(http.StatusUnauthorized, "invalid_credentials", "Email or password is wrong")
	if errors.Is(err, pgx.ErrNoRows) || hash == nil {
		auth.DummyVerify()
		httpx.Fail(w, r, bad)
		return
	}
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if !auth.VerifyPassword(body.Password, *hash) {
		httpx.Fail(w, r, bad)
		return
	}
	if status != "active" {
		httpx.Fail(w, r, httpx.Forbidden("account_suspended", "This account is not active"))
		return
	}
	// A password alone only buys a short-lived session that can do nothing but finish the TOTP step.
	sess, err := s.Auth.CreateSession(ctx, s.DB, id, 15*time.Minute, false, r.UserAgent(), clientIP(r))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	s.respondSession(w, r, sess, true, id)
}

func (s *Server) PostAuthStaffMfa(w http.ResponseWriter, r *http.Request) {
	body, err := httpx.Decode[gen.MfaVerify](r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	ctx := r.Context()
	p := auth.MustPrincipal(ctx)
	if p.Kind != auth.KindStaff {
		httpx.Fail(w, r, httpx.ErrForbidden)
		return
	}
	var enc []byte
	if err := s.DB.QueryRow(ctx, `SELECT totp_secret_enc FROM users WHERE id = $1`, p.UserID).Scan(&enc); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if enc == nil {
		httpx.Fail(w, r, httpx.Forbidden("mfa_not_enrolled", "No authenticator is enrolled for this account. Ask an admin to reset MFA."))
		return
	}
	secret, err := s.Cipher.Decrypt(enc)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if !auth.ValidTOTP(string(secret), body.Code) {
		httpx.Fail(w, r, httpx.NewError(http.StatusUnauthorized, "invalid_code", "That code is wrong"))
		return
	}
	ttl := time.Duration(s.setting(ctx, s.DB, "session.staff_ttl_hours", 12)) * time.Hour
	exp, err := s.Auth.PromoteMFA(ctx, p.SessionID, ttl)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	_, _ = s.DB.Exec(ctx, `UPDATE users SET last_login_at = now() WHERE id = $1`, p.UserID)
	s.respondSession(w, r, auth.NewSession{Token: auth.TokenFromRequest(r), ID: p.SessionID, ExpiresAt: exp}, false, p.UserID)
}

func (s *Server) PostAuthLogout(w http.ResponseWriter, r *http.Request) {
	p := auth.MustPrincipal(r.Context())
	if err := s.Auth.Revoke(r.Context(), p.SessionID); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: auth.CookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: s.Cfg.CookieSecure, SameSite: http.SameSiteLaxMode})
	httpx.NoContent(w)
}

func (s *Server) respondSession(w http.ResponseWriter, r *http.Request, sess auth.NewSession, mfaRequired bool, userID uuid.UUID) {
	me, err := s.loadMe(r.Context(), s.DB, userID)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: auth.CookieName, Value: sess.Token, Path: "/", Expires: sess.ExpiresAt,
		HttpOnly: true, Secure: s.Cfg.CookieSecure, SameSite: http.SameSiteLaxMode})
	httpx.JSON(w, http.StatusOK, sessionDTO{Token: sess.Token, ExpiresAt: sess.ExpiresAt, MFARequired: mfaRequired, User: me})
}

func (s *Server) loadMe(ctx context.Context, q db, id uuid.UUID) (meDTO, error) {
	var m meDTO
	err := q.QueryRow(ctx, `
		SELECT u.id, u.kind::text, u.status::text, u.full_name, u.phone_e164, u.email::text, u.preferred_language, u.created_at,
		       COALESCE((SELECT array_agg(role::text ORDER BY role) FROM staff_roles WHERE user_id = u.id), '{}'),
		       (SELECT status::text FROM providers WHERE user_id = u.id),
		       (SELECT id_verification::text FROM clients WHERE user_id = u.id)
		FROM users u WHERE u.id = $1`, id).
		Scan(&m.ID, &m.Kind, &m.Status, &m.FullName, &m.Phone, &m.Email, &m.PreferredLanguage, &m.CreatedAt, &m.Roles, &m.ProviderStatus, &m.IDVerification)
	return m, mapErr(err)
}

func (s *Server) GetMe(w http.ResponseWriter, r *http.Request) {
	me, err := s.loadMe(r.Context(), s.DB, auth.MustPrincipal(r.Context()).UserID)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, me)
}

func (s *Server) PatchMe(w http.ResponseWriter, r *http.Request) {
	body, err := httpx.Decode[gen.MeUpdate](r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	ctx := r.Context()
	id := auth.MustPrincipal(ctx).UserID
	var name, email, lang *string
	if body.FullName != nil {
		n := strings.TrimSpace(*body.FullName)
		if len(n) < 2 {
			httpx.Fail(w, r, httpx.Invalid("full_name is too short"))
			return
		}
		name = &n
	}
	if body.Email != nil {
		e := string(*body.Email)
		email = &e
	}
	if body.PreferredLanguage != nil {
		l := string(*body.PreferredLanguage)
		lang = &l
	}
	_, err = s.DB.Exec(ctx, `
		UPDATE users SET full_name = COALESCE($2, full_name),
		  email = COALESCE($3::citext, email),
		  email_verified_at = CASE WHEN $3::citext IS NOT NULL AND $3::citext IS DISTINCT FROM email THEN NULL ELSE email_verified_at END,
		  preferred_language = COALESCE($4, preferred_language) WHERE id = $1`, id, name, email, lang)
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	me, err := s.loadMe(ctx, s.DB, id)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, me)
}

// CreateStaff creates an active staff user with a random password and a fresh TOTP secret.
// It backs the `api create-staff` command; the secrets are returned once and never stored in plain text.
func (s *Server) CreateStaff(ctx context.Context, name, email string, roles []string) (password, totpSecret, totpURI string, err error) {
	_, password, totpSecret, totpURI, err = s.createStaff(ctx, name, email, "", roles)
	return
}

func (s *Server) createStaff(ctx context.Context, name, email, phone string, roles []string) (id uuid.UUID, password, totpSecret, totpURI string, err error) {
	if len(roles) == 0 {
		return id, "", "", "", errors.New("at least one role is required")
	}
	pw, err := s.Auth.NewCode()
	if err != nil {
		return id, "", "", "", err
	}
	pw = "Hr-" + uuid.NewString()[:13] + pw[:2]
	hash, err := auth.HashPassword(pw)
	if err != nil {
		return id, "", "", "", err
	}
	secret, uri, err := auth.NewTOTP(email)
	if err != nil {
		return id, "", "", "", err
	}
	enc, err := s.Cipher.Encrypt([]byte(secret))
	if err != nil {
		return id, "", "", "", err
	}
	err = dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `INSERT INTO users (kind, status, full_name, email, email_verified_at, password_hash, totp_secret_enc, phone_e164)
			VALUES ('staff','active',$1,$2,now(),$3,$4,NULLIF($5,'')) RETURNING id`, name, email, hash, enc, phone).Scan(&id); err != nil {
			return err
		}
		for _, role := range roles {
			if _, err := tx.Exec(ctx, `INSERT INTO staff_roles (user_id, role) VALUES ($1, $2::staff_role)`, id, role); err != nil {
				return err
			}
		}
		return audit(ctx, tx, "staff.create", "user", id, nil, map[string]any{"email": email, "roles": roles}, "created from the command line")
	})
	return id, pw, secret, uri, mapErr(err)
}
