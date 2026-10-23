package api

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/KING-CYBERTON/home-residency/backend/internal/api/gen"
	"github.com/KING-CYBERTON/home-residency/backend/internal/auth"
	"github.com/KING-CYBERTON/home-residency/backend/internal/crypt"
	dbpkg "github.com/KING-CYBERTON/home-residency/backend/internal/db"
	"github.com/KING-CYBERTON/home-residency/backend/internal/email"
	"github.com/KING-CYBERTON/home-residency/backend/internal/httpx"
)

// Households and providers can sign in with email and password as well as with a phone code, so the
// service works before (or without) an SMS gateway. Staff keep their own login with TOTP.

const (
	minPasswordLen = 8
	maxPasswordLen = 128
	// maxPasswordFailures wrong passwords in a row lock the account for passwordLockout.
	maxPasswordFailures = 10
	passwordLockout     = 15 * time.Minute
	resetTokenTTL       = time.Hour
)

var (
	errBadCredentials = httpx.NewError(http.StatusUnauthorized, "invalid_credentials", "Email or password is wrong")
	errPasswordLocked = httpx.ErrRateLimited.With("reason", "too many wrong passwords; try again in 15 minutes or reset your password")
	errResetToken     = httpx.NewError(http.StatusBadRequest, "invalid_token", "This reset link is invalid or has expired. Ask for a new one.")
)

func checkNewPassword(pw string) error {
	if n := len([]rune(pw)); n < minPasswordLen || n > maxPasswordLen {
		return httpx.Invalid(fmt.Sprintf("Password must be %d to %d characters", minPasswordLen, maxPasswordLen))
	}
	return nil
}

func normEmail(e string) string { return strings.ToLower(strings.TrimSpace(e)) }

// checkPassword verifies pw for the account under a row lock, so parallel guesses cannot all slip
// under the failure cap. Ten failures in a row lock the account for 15 minutes; a correct password
// or a reset clears the count.
func (s *Server) checkPassword(ctx context.Context, userID uuid.UUID, pw string) error {
	var bad, locked bool
	err := dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		var hash *string
		var failed int
		var until *time.Time
		if err := tx.QueryRow(ctx, `SELECT password_hash, password_failed_attempts, password_locked_until FROM users WHERE id = $1 FOR UPDATE`, userID).
			Scan(&hash, &failed, &until); err != nil {
			return err
		}
		if until != nil && until.After(s.Now()) {
			locked = true
			return nil
		}
		if hash == nil || !auth.VerifyPassword(pw, *hash) {
			bad = true
			failed++
			var lockUntil *time.Time
			if failed >= maxPasswordFailures {
				t := s.Now().Add(passwordLockout)
				lockUntil, failed = &t, 0
				if err := audit(ctx, tx, "user.password_locked", "user", userID, nil, nil, ""); err != nil {
					return err
				}
			}
			_, err := tx.Exec(ctx, `UPDATE users SET password_failed_attempts = $2, password_locked_until = $3 WHERE id = $1`, userID, failed, lockUntil)
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE users SET password_failed_attempts = 0, password_locked_until = NULL WHERE id = $1`, userID)
		return err
	})
	switch {
	case err != nil:
		return err
	case locked:
		return errPasswordLocked
	case bad:
		return errBadCredentials
	}
	return nil
}

func (s *Server) PostAuthRegister(w http.ResponseWriter, r *http.Request) {
	body, err := httpx.Decode[gen.PasswordRegister](r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if err := checkNewPassword(body.Password); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	hash, err := auth.HashPassword(body.Password)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	ctx := r.Context()
	id, err := s.createAccount(ctx, r, newAccount{
		kind: string(body.Kind), name: body.FullName, email: normEmail(string(body.Email)), passwordHash: hash, consents: body.Consents,
	})
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	s.startClientSession(w, r, id)
}

func (s *Server) PostAuthLogin(w http.ResponseWriter, r *http.Request) {
	body, err := httpx.Decode[gen.PasswordLogin](r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	ctx := r.Context()
	var id uuid.UUID
	var hasPassword bool
	var status string
	err = s.DB.QueryRow(ctx, `SELECT id, password_hash IS NOT NULL, status::text FROM users WHERE email = $1 AND kind IN ('client','provider')`,
		normEmail(string(body.Email))).Scan(&id, &hasPassword, &status)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !hasPassword) {
		auth.DummyVerify() // unknown emails take as long as wrong passwords
		httpx.Fail(w, r, errBadCredentials)
		return
	}
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if err := s.checkPassword(ctx, id, body.Password); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if status != "active" {
		httpx.Fail(w, r, httpx.Forbidden("account_suspended", "This account is not active. Contact Homend Residence."))
		return
	}
	_, _ = s.DB.Exec(ctx, `UPDATE users SET last_login_at = now() WHERE id = $1`, id)
	s.startClientSession(w, r, id)
}

// startClientSession signs a household or provider in with the same session length as a phone code.
func (s *Server) startClientSession(w http.ResponseWriter, r *http.Request, userID uuid.UUID) {
	ctx := r.Context()
	ttl := time.Duration(s.setting(ctx, s.DB, "session.client_ttl_hours", 720)) * time.Hour
	sess, err := s.Auth.CreateSession(ctx, s.DB, userID, ttl, true, r.UserAgent(), clientIP(r))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	s.respondSession(w, r, sess, false, userID)
}

func (s *Server) PostAuthPasswordForgot(w http.ResponseWriter, r *http.Request) {
	body, err := httpx.Decode[gen.PasswordForgot](r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	// The answer is the same whether or not the email has an account; failures are only logged.
	if err := s.sendPasswordReset(r.Context(), normEmail(string(body.Email)), clientIP(r)); err != nil {
		slog.ErrorContext(r.Context(), "password reset email not sent", "err", err)
	}
	httpx.NoContent(w)
}

func (s *Server) sendPasswordReset(ctx context.Context, addr, ip string) error {
	var id uuid.UUID
	var kind, name string
	err := s.DB.QueryRow(ctx, `SELECT id, kind::text, full_name FROM users
		WHERE email = $1 AND kind IN ('client','provider') AND status IN ('active','pending_verification')`, addr).Scan(&id, &kind, &name)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	// One link a minute and five an hour per account, so the endpoint cannot be used to flood an inbox.
	var lastMinute, lastHour int
	if err := s.DB.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE created_at > now() - interval '1 minute'), count(*)
		FROM password_resets WHERE user_id = $1 AND created_at > now() - interval '1 hour'`, id).Scan(&lastMinute, &lastHour); err != nil {
		return err
	}
	if lastMinute > 0 || lastHour >= 5 {
		return nil
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	if _, err := s.DB.Exec(ctx, `INSERT INTO password_resets (user_id, token_hash, expires_at, requested_ip) VALUES ($1,$2,$3,NULLIF($4,'')::inet)`,
		id, crypt.SHA256(token), s.Now().Add(resetTokenTTL), ip); err != nil {
		return err
	}
	// The portals are served by this binary at fixed paths (web.ClientPortalPath, web.ProviderPortalPath).
	portal := "/account"
	if kind == "provider" {
		portal = "/provider"
	}
	link := strings.TrimRight(s.Cfg.PublicBaseURL, "/") + portal + "/reset-password?token=" + token
	return s.Email.Send(ctx, email.Message{
		To:      addr,
		Subject: "Reset your Homend Residence password",
		Text: fmt.Sprintf("Hello %s,\n\nUse this link to choose a new password. It works once, for the next hour:\n\n%s\n\n"+
			"If you did not ask for this, ignore this email and your password stays the same.\n\nHomend Residence", name, link),
	})
}

func (s *Server) PostAuthPasswordReset(w http.ResponseWriter, r *http.Request) {
	body, err := httpx.Decode[gen.PasswordReset](r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if err := checkNewPassword(body.Password); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	hash, err := auth.HashPassword(body.Password)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	ctx := r.Context()
	err = dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		var id uuid.UUID
		err := tx.QueryRow(ctx, `UPDATE password_resets SET used_at = now()
			WHERE token_hash = $1 AND used_at IS NULL AND expires_at > now() RETURNING user_id`, crypt.SHA256(body.Token)).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return errResetToken
		}
		if err != nil {
			return err
		}
		// Opening the link proves the address, so it is confirmed and a never-activated account is activated.
		tag, err := tx.Exec(ctx, `UPDATE users SET password_hash = $2, password_failed_attempts = 0, password_locked_until = NULL,
			email_verified_at = COALESCE(email_verified_at, now()),
			status = CASE WHEN status = 'pending_verification' THEN 'active'::account_status ELSE status END
			WHERE id = $1 AND kind IN ('client','provider') AND status IN ('active','pending_verification')`, id, hash)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return errResetToken
		}
		if _, err := tx.Exec(ctx, `UPDATE password_resets SET used_at = now() WHERE user_id = $1 AND used_at IS NULL`, id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE sessions SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL`, id); err != nil {
			return err
		}
		return audit(ctx, tx, "user.password_reset", "user", id, nil, nil, "")
	})
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.NoContent(w)
}

func (s *Server) PostMePassword(w http.ResponseWriter, r *http.Request) {
	body, err := httpx.Decode[gen.PasswordChange](r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if err := checkNewPassword(body.NewPassword); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	ctx := r.Context()
	p := auth.MustPrincipal(ctx)
	var addr *string
	var hasPassword bool
	if err := s.DB.QueryRow(ctx, `SELECT email::text, password_hash IS NOT NULL FROM users WHERE id = $1`, p.UserID).Scan(&addr, &hasPassword); err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	if addr == nil {
		httpx.Fail(w, r, httpx.RuleViolation("Add an email address to your account first: you sign in with it and your password"))
		return
	}
	if hasPassword {
		current := ""
		if body.CurrentPassword != nil {
			current = *body.CurrentPassword
		}
		if err := s.checkPassword(ctx, p.UserID, current); err != nil {
			httpx.Fail(w, r, err)
			return
		}
	}
	hash, err := auth.HashPassword(body.NewPassword)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	err = dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE users SET password_hash = $2 WHERE id = $1`, p.UserID, hash); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE sessions SET revoked_at = now() WHERE user_id = $1 AND id <> $2 AND revoked_at IS NULL`, p.UserID, p.SessionID); err != nil {
			return err
		}
		return audit(ctx, tx, "user.password_change", "user", p.UserID, nil, nil, "")
	})
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.NoContent(w)
}
