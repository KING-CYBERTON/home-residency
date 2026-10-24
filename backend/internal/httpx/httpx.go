// Package httpx holds the JSON helpers, error envelope and middleware shared by every handler.
package httpx

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"strings"
	"time"
)

type ctxKey int

const (
	requestIDKey ctxKey = 1
	clientKey    ctxKey = 2
)

// Client is where a request came from, kept on its context so the audit log can record it.
type Client struct {
	IP        string // empty when it cannot be parsed
	UserAgent string
}

// ClientOf returns the address and browser of the request that ctx belongs to.
func ClientOf(ctx context.Context) Client {
	c, _ := ctx.Value(clientKey).(Client)
	return c
}

// ClientIP is the caller's address. Behind the ALB the last X-Forwarded-For entry is the address
// the load balancer saw; anything before it was supplied by the caller and cannot be trusted.
func ClientIP(r *http.Request) string {
	if xf := r.Header.Get("X-Forwarded-For"); xf != "" {
		parts := strings.Split(xf, ",")
		return strings.TrimSpace(parts[len(parts)-1])
	}
	host := r.RemoteAddr
	if i := strings.LastIndex(host, ":"); i > 0 {
		host = host[:i]
	}
	return strings.Trim(host, "[]")
}

// Error is the API error envelope (components/schemas/Error in the spec).
type Error struct {
	Status  int            `json:"-"`
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
	ReqID   string         `json:"request_id,omitempty"`
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

func NewError(status int, code, msg string) *Error {
	return &Error{Status: status, Code: code, Message: msg}
}

func (e *Error) With(k string, v any) *Error {
	c := *e
	c.Details = map[string]any{}
	for dk, dv := range e.Details {
		c.Details[dk] = dv
	}
	c.Details[k] = v
	return &c
}

var (
	ErrBadRequest      = NewError(http.StatusBadRequest, "bad_request", "Malformed request")
	ErrUnauthenticated = NewError(http.StatusUnauthorized, "unauthenticated", "Not signed in or session expired")
	ErrForbidden       = NewError(http.StatusForbidden, "forbidden", "You are not allowed to do this")
	ErrNotFound        = NewError(http.StatusNotFound, "not_found", "Not found")
	ErrConflict        = NewError(http.StatusConflict, "invalid_state_transition", "Not allowed in the current state")
	ErrGone            = NewError(http.StatusGone, "gone", "No longer available")
	ErrRateLimited     = NewError(http.StatusTooManyRequests, "rate_limited", "Too many attempts, try again later")
	ErrNotImplemented  = NewError(http.StatusNotImplemented, "not_implemented", "This endpoint is specified but not built yet")
)

func Invalid(msg string) *Error {
	return NewError(http.StatusUnprocessableEntity, "validation_failed", msg)
}
func RuleViolation(msg string) *Error {
	return NewError(http.StatusUnprocessableEntity, "rule_violation", msg)
}
func BadRequest(msg string) *Error { return NewError(http.StatusBadRequest, "bad_request", msg) }
func Conflict(msg string) *Error {
	return NewError(http.StatusConflict, "invalid_state_transition", msg)
}
func Forbidden(code, msg string) *Error { return NewError(http.StatusForbidden, code, msg) }

func RequestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey).(string)
	return id
}

func JSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}

func NoContent(w http.ResponseWriter) { w.WriteHeader(http.StatusNoContent) }

// Fail writes err as the error envelope. Unknown errors become 500 and are logged, never echoed.
func Fail(w http.ResponseWriter, r *http.Request, err error) {
	var he *Error
	if !errors.As(err, &he) {
		slog.ErrorContext(r.Context(), "unhandled error", "err", err, "path", r.URL.Path, "request_id", RequestID(r.Context()))
		he = NewError(http.StatusInternalServerError, "internal", "Something went wrong")
	}
	out := *he
	out.ReqID = RequestID(r.Context())
	JSON(w, he.Status, out)
}

// Decode reads a JSON body (max 1 MiB) into T and rejects unknown trailing data.
func Decode[T any](r *http.Request) (T, error) {
	var v T
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	if err := dec.Decode(&v); err != nil {
		return v, BadRequest("Request body must be valid JSON: " + err.Error())
	}
	return v, nil
}

func RequestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-Id")
		if id == "" || len(id) > 64 {
			b := make([]byte, 8)
			_, _ = rand.Read(b)
			id = hex.EncodeToString(b)
		}
		w.Header().Set("X-Request-Id", id)
		ctx := context.WithValue(r.Context(), requestIDKey, id)
		c := Client{UserAgent: r.UserAgent()}
		if len(c.UserAgent) > 512 {
			c.UserAgent = c.UserAgent[:512]
		}
		if ip := net.ParseIP(ClientIP(r)); ip != nil {
			c.IP = ip.String()
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(ctx, clientKey, c)))
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (s *statusWriter) WriteHeader(c int) { s.status = c; s.ResponseWriter.WriteHeader(c) }

func Logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: 200}
		next.ServeHTTP(sw, r)
		slog.InfoContext(r.Context(), "http", "method", r.Method, "path", r.URL.Path, "status", sw.status,
			"ms", time.Since(start).Milliseconds(), "request_id", RequestID(r.Context()))
	})
}

func Recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if p := recover(); p != nil {
				slog.ErrorContext(r.Context(), "panic", "panic", p, "stack", string(debug.Stack()))
				Fail(w, r, errors.New("panic"))
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}
