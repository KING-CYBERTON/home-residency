package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/KING-CYBERTON/home-residency/backend/internal/api/gen"
	"github.com/KING-CYBERTON/home-residency/backend/internal/auth"
	dbpkg "github.com/KING-CYBERTON/home-residency/backend/internal/db"
	"github.com/KING-CYBERTON/home-residency/backend/internal/httpx"
)

// Sessions are listed so a person can see where they are signed in and end one they don't
// recognise, and so an admin can sign a colleague out after a lost phone or a shared computer.

// sessionsOf lists the user's sessions that are still signed in, newest first. current is the session
// making the request, so the list can mark "this browser".
func (s *Server) sessionsOf(ctx context.Context, userID, current uuid.UUID) ([]json.RawMessage, error) {
	return rawAll(ctx, s.DB, `
		SELECT jsonb_build_object('id', id, 'created_at', created_at, 'last_seen_at', last_seen_at, 'expires_at', expires_at,
		  'user_agent', user_agent, 'ip_address', host(ip_address), 'mfa_passed', mfa_passed, 'current', id = $2)
		FROM sessions WHERE user_id = $1 AND revoked_at IS NULL AND expires_at > now()
		ORDER BY last_seen_at DESC, id`, userID, current)
}

func (s *Server) GetMeSessions(w http.ResponseWriter, r *http.Request) {
	p := auth.MustPrincipal(r.Context())
	out, err := s.sessionsOf(r.Context(), p.UserID, p.SessionID)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (s *Server) DeleteMeSession(w http.ResponseWriter, r *http.Request, id gen.Id) {
	ctx := r.Context()
	p := auth.MustPrincipal(ctx)
	sid := uuid.UUID(id)
	if sid == p.SessionID {
		httpx.Fail(w, r, httpx.Conflict("This is the session you are using. Sign out instead."))
		return
	}
	err := dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE sessions SET revoked_at = now() WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL`, sid, p.UserID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return httpx.ErrNotFound
		}
		return audit(ctx, tx, "session.revoke", "user", p.UserID, nil, map[string]any{"session_id": sid}, "")
	})
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	httpx.NoContent(w)
}

// staffExists checks the id is a staff account, so admin tools cannot reach households or providers.
func (s *Server) staffExists(ctx context.Context, q db, id uuid.UUID) error {
	var n int
	if err := q.QueryRow(ctx, `SELECT count(*) FROM users WHERE id = $1 AND kind = 'staff'`, id).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return httpx.ErrNotFound
	}
	return nil
}

func (s *Server) GetConsoleStaffSessions(w http.ResponseWriter, r *http.Request, id gen.Id) {
	ctx := r.Context()
	if err := s.staffExists(ctx, s.DB, uuid.UUID(id)); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	out, err := s.sessionsOf(ctx, uuid.UUID(id), auth.MustPrincipal(ctx).SessionID)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (s *Server) PostConsoleStaffSignOut(w http.ResponseWriter, r *http.Request, id gen.Id) {
	reason, err := reasonOf(r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	ctx := r.Context()
	uid := uuid.UUID(id)
	err = dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		if err := s.staffExists(ctx, tx, uid); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `UPDATE sessions SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL`, uid)
		if err != nil {
			return err
		}
		return audit(ctx, tx, "staff.sign_out", "user", uid, nil, map[string]any{"sessions_ended": tag.RowsAffected()}, reason)
	})
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	httpx.NoContent(w)
}

// PostConsoleStaffResetPassword is the way back for a staff user who has forgotten their password.
// Staff sign in with a password and an authenticator code, so a reset by email alone would weaken
// that; an admin hands the temporary password over in person, as with a new account.
func (s *Server) PostConsoleStaffResetPassword(w http.ResponseWriter, r *http.Request, id gen.Id) {
	reason, err := reasonOf(r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	ctx := r.Context()
	uid := uuid.UUID(id)
	if auth.MustPrincipal(ctx).UserID == uid {
		httpx.Fail(w, r, httpx.Conflict("You cannot reset your own password here. Change it from My account."))
		return
	}
	pw, err := s.temporaryPassword()
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	hash, err := auth.HashPassword(pw)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	err = dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE users SET password_hash = $2, password_failed_attempts = 0, password_locked_until = NULL
			WHERE id = $1 AND kind = 'staff'`, uid, hash)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return httpx.ErrNotFound
		}
		if _, err := tx.Exec(ctx, `UPDATE sessions SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL`, uid); err != nil {
			return err
		}
		return audit(ctx, tx, "staff.reset_password", "user", uid, nil, nil, reason)
	})
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]string{"password": pw})
}
