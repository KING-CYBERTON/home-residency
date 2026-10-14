package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"

	"github.com/KING-CYBERTON/home-residency/backend/internal/auth"
	"github.com/KING-CYBERTON/home-residency/backend/internal/crypt"
	"github.com/KING-CYBERTON/home-residency/backend/internal/httpx"
)

// idempotent runs fn once per (user, Idempotency-Key). A retry with the same key and body replays the
// stored response; the same key with a different body is rejected. Without the header, fn just runs.
// fn returns the status and JSON body to send, or an error (which clears the key so the client can retry).
func (s *Server) idempotent(w http.ResponseWriter, r *http.Request, key *string, fn func(body []byte) (int, any, error)) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		httpx.Fail(w, r, httpx.BadRequest("Could not read request body"))
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	if key == nil || *key == "" {
		status, body, err := fn(raw)
		respond(w, r, status, body, err)
		return
	}
	ctx := r.Context()
	uid := auth.MustPrincipal(ctx).UserID
	hash := crypt.SHA256(r.Method + " " + r.URL.Path + "\n" + string(raw))
	tag, err := s.DB.Exec(ctx, `INSERT INTO idempotency_keys (key, user_id, request_hash) VALUES ($1,$2,$3) ON CONFLICT DO NOTHING`, *key, uid, hash)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if tag.RowsAffected() == 0 {
		var storedHash []byte
		var code *int16
		var stored []byte
		if err := s.DB.QueryRow(ctx, `SELECT request_hash, response_code, response_body FROM idempotency_keys WHERE key = $1 AND user_id = $2`, *key, uid).
			Scan(&storedHash, &code, &stored); err != nil {
			httpx.Fail(w, r, mapErr(err))
			return
		}
		switch {
		case !bytes.Equal(storedHash, hash):
			httpx.Fail(w, r, httpx.Invalid("This Idempotency-Key was already used with a different request"))
		case code == nil:
			httpx.Fail(w, r, httpx.Conflict("A request with this Idempotency-Key is still being processed"))
		default:
			w.Header().Set("Idempotent-Replay", "true")
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(int(*code))
			_, _ = w.Write(stored)
		}
		return
	}
	status, body, err := fn(raw)
	if err != nil {
		_, _ = s.DB.Exec(ctx, `DELETE FROM idempotency_keys WHERE key = $1 AND user_id = $2`, *key, uid)
		httpx.Fail(w, r, err)
		return
	}
	b, _ := json.Marshal(body)
	_, _ = s.DB.Exec(ctx, `UPDATE idempotency_keys SET response_code = $3, response_body = $4 WHERE key = $1 AND user_id = $2`, *key, uid, status, b)
	respond(w, r, status, body, nil)
}

func respond(w http.ResponseWriter, r *http.Request, status int, body any, err error) {
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, status, body)
}
