-- +goose Up
-- When a category's visits may start (local Nairobi time) and how far ahead they must be booked.
-- No hours means any time of day. Existing categories start with office hours and two hours' notice,
-- which staff can change per category.
ALTER TABLE service_categories
  ADD COLUMN IF NOT EXISTS opens_at  TIME,
  ADD COLUMN IF NOT EXISTS closes_at TIME,
  ADD COLUMN IF NOT EXISTS min_lead_minutes INT NOT NULL DEFAULT 0;
UPDATE service_categories SET opens_at = '07:00', closes_at = '19:00', min_lead_minutes = 120;

-- +goose Down
ALTER TABLE service_categories DROP COLUMN IF EXISTS min_lead_minutes, DROP COLUMN IF EXISTS closes_at, DROP COLUMN IF EXISTS opens_at;
