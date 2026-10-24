-- +goose Up
-- The audit log keeps the browser or app that made each change next to its address.
ALTER TABLE audit_log ADD COLUMN IF NOT EXISTS user_agent TEXT;

-- +goose Down
ALTER TABLE audit_log DROP COLUMN IF EXISTS user_agent;
