-- +goose NO TRANSACTION
-- +goose Up
-- Prices include VAT and totals are whole shillings (M-Pesa moves nothing smaller). An invoice keeps
-- the rate it was issued at and the VAT-free part of the provider's work, so the ledger split made
-- when it is paid and the provider's earning made when the job closes use the same numbers.
ALTER TYPE invoice_line_kind ADD VALUE IF NOT EXISTS 'rounding';
ALTER TABLE invoices
  ADD COLUMN IF NOT EXISTS vat_rate_pct  NUMERIC(5,2),
  ADD COLUMN IF NOT EXISTS work_net_minor BIGINT NOT NULL DEFAULT 0;

-- +goose Down
-- The 'rounding' enum value stays: Postgres cannot drop one value from an enum.
ALTER TABLE invoices DROP COLUMN IF EXISTS work_net_minor, DROP COLUMN IF EXISTS vat_rate_pct;
