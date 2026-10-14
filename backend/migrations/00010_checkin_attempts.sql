-- +goose Up
-- Door codes are four digits, so wrong attempts are counted and capped until the client generates a new code.
ALTER TABLE visits ADD COLUMN checkin_attempts SMALLINT NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE visits DROP COLUMN checkin_attempts;
