-- +goose Up
-- The dashboard counts today's open visits; without this it reads every visit ever booked.
CREATE INDEX IF NOT EXISTS visits_open_schedule_idx ON visits (scheduled_start) WHERE status IN ('scheduled', 'checked_in');
-- Draft payout batches and open complaints by severity are counted on every dashboard load too.
CREATE INDEX IF NOT EXISTS payout_batches_status_idx ON payout_batches (status);

-- +goose Down
DROP INDEX IF EXISTS payout_batches_status_idx;
DROP INDEX IF EXISTS visits_open_schedule_idx;
