-- +goose Up
-- Households and providers may also sign in with email and password (staff always did), so the
-- service can run before an SMS gateway is approved. Wrong passwords are counted per account and
-- lock it for a while; a reset link sent by email sets a new password.
COMMENT ON COLUMN users.password_hash IS 'argon2id; required for staff, optional for clients and providers';
ALTER TABLE users ADD COLUMN password_failed_attempts SMALLINT NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN password_locked_until TIMESTAMPTZ;

-- Only the SHA-256 of the emailed token is stored, like session tokens.
CREATE TABLE password_resets (
  id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  token_hash   BYTEA NOT NULL UNIQUE,
  expires_at   TIMESTAMPTZ NOT NULL,
  used_at      TIMESTAMPTZ,
  requested_ip INET,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX password_resets_user_idx ON password_resets (user_id, created_at DESC);

-- +goose Down
DROP TABLE password_resets;
ALTER TABLE users DROP COLUMN password_locked_until;
ALTER TABLE users DROP COLUMN password_failed_attempts;
COMMENT ON COLUMN users.password_hash IS NULL;
