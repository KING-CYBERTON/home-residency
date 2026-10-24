-- +goose Up
-- Leads are followed up in the console: a status, an owner and notes. The contact form's
-- "Not sure yet" and "Other / not listed" choices are kept, so they read differently from a lead
-- that gave no service or area at all.
CREATE TYPE lead_status AS ENUM ('new', 'contacted', 'qualified', 'converted', 'closed');
ALTER TABLE leads
  ADD COLUMN category_not_sure BOOLEAN NOT NULL DEFAULT false,
  ADD COLUMN area_not_listed   BOOLEAN NOT NULL DEFAULT false,
  ADD COLUMN area_name         TEXT,
  ADD COLUMN status            lead_status NOT NULL DEFAULT 'new',
  ADD COLUMN assigned_to       UUID REFERENCES users(id),
  ADD COLUMN notes             TEXT,
  ADD COLUMN contacted_at      TIMESTAMPTZ,
  ADD COLUMN updated_at        TIMESTAMPTZ NOT NULL DEFAULT now();
CREATE TRIGGER leads_updated_at BEFORE UPDATE ON leads FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE INDEX leads_status_idx ON leads (status, created_at DESC);

-- +goose Down
DROP INDEX leads_status_idx;
DROP TRIGGER leads_updated_at ON leads;
ALTER TABLE leads
  DROP COLUMN updated_at, DROP COLUMN contacted_at, DROP COLUMN notes, DROP COLUMN assigned_to,
  DROP COLUMN status, DROP COLUMN area_name, DROP COLUMN area_not_listed, DROP COLUMN category_not_sure;
DROP TYPE lead_status;
