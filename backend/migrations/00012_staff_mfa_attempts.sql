-- +goose Up
-- Staff TOTP: wrong codes are counted and capped (an admin MFA reset clears the lock), and the last
-- accepted time step is kept so a code cannot be used twice.
ALTER TABLE users ADD COLUMN mfa_failed_attempts SMALLINT NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN mfa_last_step BIGINT;

-- +goose Down
ALTER TABLE users DROP COLUMN mfa_last_step;
ALTER TABLE users DROP COLUMN mfa_failed_attempts;
