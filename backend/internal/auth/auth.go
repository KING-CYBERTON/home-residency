// Package auth owns sessions, one-time codes, staff passwords and TOTP.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
	"golang.org/x/crypto/argon2"

	"github.com/KING-CYBERTON/home-residency/backend/internal/crypt"
	"github.com/KING-CYBERTON/home-residency/backend/internal/httpx"
)

type Kind string

const (
	KindClient   Kind = "client"
	KindProvider Kind = "provider"
	KindStaff    Kind = "staff"
)

// Principal is the authenticated caller attached to the request context.
type Principal struct {
	UserID    uuid.UUID
	SessionID uuid.UUID
	Kind      Kind
	Roles     []string
	MFAPassed bool
}

func (p *Principal) HasAnyRole(roles ...string) bool {
	for _, r := range roles {
		for _, have := range p.Roles {
			if r == have {
				return true
			}
		}
	}
	return false
}

type ctxKey struct{}

func WithPrincipal(ctx context.Context, p *Principal) context.Context {
	return context.WithValue(ctx, ctxKey{}, p)
}

func FromContext(ctx context.Context) *Principal {
	p, _ := ctx.Value(ctxKey{}).(*Principal)
	return p
}

// MustPrincipal is for handlers behind the auth middleware, where a principal always exists.
func MustPrincipal(ctx context.Context) *Principal {
	p := FromContext(ctx)
	if p == nil {
		panic("auth: no principal in context; route is missing the auth middleware")
	}
	return p
}

type Service struct {
	DB     *pgxpool.Pool
	Pepper []byte
	Now    func() time.Time
}

func New(db *pgxpool.Pool, pepper []byte) *Service {
	return &Service{DB: db, Pepper: pepper, Now: time.Now}
}

// ---- sessions

type NewSession struct {
	Token     string
	ID        uuid.UUID
	ExpiresAt time.Time
}

func (s *Service) CreateSession(ctx context.Context, q Querier, userID uuid.UUID, ttl time.Duration, mfaPassed bool, userAgent, ip string) (NewSession, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return NewSession{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	exp := s.Now().Add(ttl)
	var id uuid.UUID
	err := q.QueryRow(ctx, `
		INSERT INTO sessions (user_id, token_hash, user_agent, ip_address, mfa_passed, expires_at)
		VALUES ($1, $2, NULLIF($3,''), NULLIF($4,'')::inet, $5, $6) RETURNING id`,
		userID, crypt.SHA256(token), userAgent, ip, mfaPassed, exp).Scan(&id)
	if err != nil {
		return NewSession{}, err
	}
	return NewSession{Token: token, ID: id, ExpiresAt: exp}, nil
}

// Querier is satisfied by *pgxpool.Pool and pgx.Tx.
type Querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// Lookup resolves a bearer token to a principal, or ErrUnauthenticated.
func (s *Service) Lookup(ctx context.Context, token string) (*Principal, error) {
	var p Principal
	var kind, status string
	err := s.DB.QueryRow(ctx, `
		SELECT s.id, s.user_id, s.mfa_passed, u.kind::text, u.status::text,
		       COALESCE((SELECT array_agg(role::text) FROM staff_roles WHERE user_id = u.id), '{}')
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = $1 AND s.revoked_at IS NULL AND s.expires_at > now()`,
		crypt.SHA256(token)).Scan(&p.SessionID, &p.UserID, &p.MFAPassed, &kind, &status, &p.Roles)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.ErrUnauthenticated
	}
	if err != nil {
		return nil, err
	}
	if status != "active" {
		return nil, httpx.ErrUnauthenticated
	}
	p.Kind = Kind(kind)
	_, _ = s.DB.Exec(ctx, `UPDATE sessions SET last_seen_at = now() WHERE id = $1 AND last_seen_at < now() - interval '1 minute'`, p.SessionID)
	return &p, nil
}

func (s *Service) Revoke(ctx context.Context, sessionID uuid.UUID) error {
	_, err := s.DB.Exec(ctx, `UPDATE sessions SET revoked_at = now() WHERE id = $1`, sessionID)
	return err
}

func (s *Service) PromoteMFA(ctx context.Context, sessionID uuid.UUID, ttl time.Duration) (time.Time, error) {
	exp := s.Now().Add(ttl)
	_, err := s.DB.Exec(ctx, `UPDATE sessions SET mfa_passed = true, expires_at = $2 WHERE id = $1`, sessionID, exp)
	return exp, err
}

// ---- one-time codes

func (s *Service) NewCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}

func (s *Service) HashCode(otpID uuid.UUID, code string) []byte {
	return crypt.HMAC(s.Pepper, "otp", otpID.String(), code)
}

func (s *Service) CodeMatches(otpID uuid.UUID, code string, stored []byte) bool {
	return subtle.ConstantTimeCompare(s.HashCode(otpID, code), stored) == 1
}

// ---- staff passwords (argon2id) and TOTP

const (
	argonTime    = 2
	argonMemory  = 64 * 1024
	argonThreads = 2
	argonKeyLen  = 32
)

func HashPassword(pw string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(pw), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

func VerifyPassword(pw, encoded string) bool {
	var ver, m, t, p int
	var saltB, keyB string
	if _, err := fmt.Sscanf(encoded, "$argon2id$v=%d$m=%d,t=%d,p=%d$%s", &ver, &m, &t, &p, &saltB); err != nil {
		return false
	}
	// Sscanf %s stops at whitespace only, so split the trailing "salt$key" ourselves.
	for i := 0; i < len(saltB); i++ {
		if saltB[i] == '$' {
			keyB = saltB[i+1:]
			saltB = saltB[:i]
			break
		}
	}
	salt, err1 := base64.RawStdEncoding.DecodeString(saltB)
	want, err2 := base64.RawStdEncoding.DecodeString(keyB)
	if err1 != nil || err2 != nil || len(want) == 0 {
		return false
	}
	got := argon2.IDKey([]byte(pw), salt, uint32(t), uint32(m), uint8(p), uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

// DummyVerify burns the same CPU as a real check so unknown emails are not distinguishable by timing.
func DummyVerify() {
	argon2.IDKey([]byte("x"), []byte("0123456789abcdef"), argonTime, argonMemory, argonThreads, argonKeyLen)
}

func ValidTOTP(secret, code string) bool { return totp.Validate(code, secret) }

// MatchTOTP checks code against the steps either side of now and returns the 30-second time step
// it matched, so callers can refuse a step that was already used.
func MatchTOTP(secret, code string, now time.Time) (step int64, ok bool) {
	for _, skew := range []int64{0, -1, 1} {
		t := now.Add(time.Duration(skew) * 30 * time.Second)
		valid, err := totp.ValidateCustom(code, secret, t, totp.ValidateOpts{Period: 30, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1})
		if err == nil && valid {
			return t.Unix() / 30, true
		}
	}
	return 0, false
}

func NewTOTP(accountEmail string) (secret, uri string, err error) {
	k, err := totp.Generate(totp.GenerateOpts{Issuer: "Homend Residence", AccountName: accountEmail})
	if err != nil {
		return "", "", err
	}
	return k.Secret(), k.URL(), nil
}
