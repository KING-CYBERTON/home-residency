-- +goose Up
-- 000006 payments: invoices, M-Pesa collections, refunds, provider payouts, a small
-- double-entry ledger for reconciliation, and household subscription plans.
-- Default taken (report §7.8 open): the platform collects client money via M-Pesa
-- (Daraja STK Push) into a segregated client-funds account and pays providers by B2C
-- on a published cycle, capturing commission on settlement.

CREATE TYPE invoice_status AS ENUM ('draft', 'issued', 'partially_paid', 'paid', 'void', 'refunded');
CREATE TABLE invoices (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  reference     TEXT NOT NULL UNIQUE DEFAULT ('INV-' || to_char(now(), 'YYYY') || '-' || lpad(nextval('invoice_ref_seq')::text, 6, '0')),
  client_id     UUID NOT NULL REFERENCES clients(user_id),
  request_id    UUID REFERENCES service_requests(id),
  subscription_id UUID,                          -- FK added below
  status        invoice_status NOT NULL DEFAULT 'draft',
  subtotal_minor BIGINT NOT NULL DEFAULT 0,
  vat_minor     BIGINT NOT NULL DEFAULT 0,
  total_minor   BIGINT NOT NULL DEFAULT 0,
  paid_minor    BIGINT NOT NULL DEFAULT 0,
  currency      CHAR(3) NOT NULL DEFAULT 'KES',
  due_at        TIMESTAMPTZ,
  issued_at     TIMESTAMPTZ,
  paid_at       TIMESTAMPTZ,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  CHECK (total_minor = subtotal_minor + vat_minor),
  CHECK (paid_minor >= 0)
);
CREATE INDEX invoices_client_idx ON invoices (client_id, created_at DESC);
CREATE INDEX invoices_request_idx ON invoices (request_id);
CREATE TRIGGER invoices_updated_at BEFORE UPDATE ON invoices FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TYPE invoice_line_kind AS ENUM ('service', 'placement_fee', 'variation', 'subscription', 'deposit', 'discount', 'adjustment');
CREATE TABLE invoice_lines (
  id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  invoice_id   UUID NOT NULL REFERENCES invoices(id) ON DELETE CASCADE,
  kind         invoice_line_kind NOT NULL,
  description  TEXT NOT NULL,
  quantity     NUMERIC(8,2) NOT NULL DEFAULT 1,
  unit_minor   BIGINT NOT NULL,
  amount_minor BIGINT NOT NULL,
  variation_id UUID REFERENCES variations(id),
  visit_id     UUID REFERENCES visits(id)
);
CREATE INDEX invoice_lines_invoice_idx ON invoice_lines (invoice_id);

CREATE TYPE payment_method AS ENUM ('mpesa_stk', 'mpesa_paybill', 'card', 'bank_transfer', 'manual');
CREATE TYPE payment_status AS ENUM ('initiated', 'pending', 'succeeded', 'failed', 'cancelled', 'reversed');
CREATE TABLE payments (
  id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  invoice_id            UUID NOT NULL REFERENCES invoices(id),
  client_id             UUID NOT NULL REFERENCES clients(user_id),
  method                payment_method NOT NULL,
  status                payment_status NOT NULL DEFAULT 'initiated',
  amount_minor          BIGINT NOT NULL CHECK (amount_minor > 0),
  currency              CHAR(3) NOT NULL DEFAULT 'KES',
  payer_msisdn          TEXT,
  mpesa_checkout_request_id TEXT UNIQUE,     -- Daraja STK CheckoutRequestID
  mpesa_receipt_number  TEXT UNIQUE,         -- e.g. QJK3ABC123
  failure_reason        TEXT,
  idempotency_key       TEXT UNIQUE,
  recorded_by           UUID REFERENCES users(id),   -- manual payments
  initiated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
  completed_at          TIMESTAMPTZ,
  updated_at            TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX payments_invoice_idx ON payments (invoice_id);
CREATE INDEX payments_pending_idx ON payments (initiated_at) WHERE status IN ('initiated', 'pending');
CREATE TRIGGER payments_updated_at BEFORE UPDATE ON payments FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Raw Daraja callbacks (STK result, C2B confirmation, B2C result, timeouts), stored
-- before processing. Callbacks are enqueued to SQS and processed idempotently.
CREATE TABLE mpesa_callbacks (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  callback_type TEXT NOT NULL CHECK (callback_type IN ('stk_result', 'c2b_validation', 'c2b_confirmation', 'b2c_result', 'b2c_timeout', 'reversal_result')),
  correlation_id TEXT,                 -- CheckoutRequestID / ConversationID / TransID
  payload       JSONB NOT NULL,
  source_ip     INET,
  received_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  processed_at  TIMESTAMPTZ,
  process_error TEXT
);
CREATE INDEX mpesa_callbacks_unprocessed_idx ON mpesa_callbacks (received_at) WHERE processed_at IS NULL;
CREATE INDEX mpesa_callbacks_correlation_idx ON mpesa_callbacks (correlation_id);

CREATE TYPE refund_status AS ENUM ('requested', 'approved', 'processing', 'completed', 'rejected', 'failed');
CREATE TABLE refunds (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  payment_id    UUID NOT NULL REFERENCES payments(id),
  amount_minor  BIGINT NOT NULL CHECK (amount_minor > 0),
  reason        TEXT NOT NULL,
  complaint_id  UUID,                  -- FK added in 000007
  status        refund_status NOT NULL DEFAULT 'requested',
  requested_by  UUID NOT NULL REFERENCES users(id),
  approved_by   UUID REFERENCES users(id),
  mpesa_conversation_id TEXT UNIQUE,
  completed_at  TIMESTAMPTZ,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  CHECK (approved_by IS NULL OR approved_by <> requested_by)
);

-- Provider earnings accrue per completed visit, then are paid in batches.
CREATE TYPE earning_status AS ENUM ('pending', 'held', 'payable', 'in_payout', 'paid', 'cancelled');
CREATE TABLE provider_earnings (
  id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  provider_id       UUID NOT NULL REFERENCES providers(user_id),
  request_id        UUID NOT NULL REFERENCES service_requests(id),
  visit_id          UUID REFERENCES visits(id),
  variation_id      UUID REFERENCES variations(id),
  gross_minor       BIGINT NOT NULL,       -- client amount attributable
  commission_minor  BIGINT NOT NULL,       -- platform take
  withholding_minor BIGINT NOT NULL DEFAULT 0,  -- WHT / statutory deductions (§7.8, C6)
  net_minor         BIGINT NOT NULL,
  status            earning_status NOT NULL DEFAULT 'pending',
  hold_reason       TEXT,                  -- e.g. open complaint
  payout_id         UUID,                  -- FK added below
  earned_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
  CHECK (net_minor = gross_minor - commission_minor - withholding_minor)
);
CREATE INDEX provider_earnings_payable_idx ON provider_earnings (provider_id, status);
CREATE TRIGGER provider_earnings_updated_at BEFORE UPDATE ON provider_earnings FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TYPE payout_status AS ENUM ('draft', 'approved', 'processing', 'paid', 'failed', 'cancelled');
CREATE TABLE payout_batches (
  id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  cycle_start  DATE NOT NULL,
  cycle_end    DATE NOT NULL,
  status       payout_status NOT NULL DEFAULT 'draft',
  total_minor  BIGINT NOT NULL DEFAULT 0,
  prepared_by  UUID NOT NULL REFERENCES users(id),
  approved_by  UUID REFERENCES users(id),
  approved_at  TIMESTAMPTZ,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  CHECK (cycle_end >= cycle_start),
  CHECK (approved_by IS NULL OR approved_by <> prepared_by)   -- segregation of duties
);

CREATE TABLE payouts (
  id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  batch_id              UUID NOT NULL REFERENCES payout_batches(id),
  provider_id           UUID NOT NULL REFERENCES providers(user_id),
  amount_minor          BIGINT NOT NULL CHECK (amount_minor > 0),
  msisdn                TEXT NOT NULL,
  status                payout_status NOT NULL DEFAULT 'draft',
  mpesa_conversation_id TEXT UNIQUE,
  mpesa_receipt_number  TEXT UNIQUE,
  failure_reason        TEXT,
  attempts              SMALLINT NOT NULL DEFAULT 0,
  paid_at               TIMESTAMPTZ,
  created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (batch_id, provider_id)
);
CREATE TRIGGER payouts_updated_at BEFORE UPDATE ON payouts FOR EACH ROW EXECUTE FUNCTION set_updated_at();
ALTER TABLE provider_earnings ADD CONSTRAINT provider_earnings_payout_fk FOREIGN KEY (payout_id) REFERENCES payouts(id);

-- Minimal double-entry ledger. Every money movement posts balanced entries in one
-- transaction (sum of a journal's entries = 0). Keeps client funds visibly segregated.
CREATE TYPE ledger_account AS ENUM (
  'client_funds',        -- money held for clients (segregated bank / paybill)
  'provider_payable',    -- owed to providers
  'platform_revenue',    -- commission, placement fees, subscriptions
  'vat_payable',
  'withholding_payable',
  'mpesa_clearing',      -- in flight with Safaricom
  'refunds'
);
CREATE TABLE ledger_entries (
  id           BIGSERIAL PRIMARY KEY,
  journal_id   UUID NOT NULL,
  account      ledger_account NOT NULL,
  amount_minor BIGINT NOT NULL,          -- debit positive, credit negative
  currency     CHAR(3) NOT NULL DEFAULT 'KES',
  client_id    UUID REFERENCES clients(user_id),
  provider_id  UUID REFERENCES providers(user_id),
  invoice_id   UUID REFERENCES invoices(id),
  payment_id   UUID REFERENCES payments(id),
  payout_id    UUID REFERENCES payouts(id),
  refund_id    UUID REFERENCES refunds(id),
  memo         TEXT,
  posted_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX ledger_entries_journal_idx ON ledger_entries (journal_id);
CREATE INDEX ledger_entries_account_idx ON ledger_entries (account, posted_at);

-- Household subscriptions (Should-have, §6.1). Plans are data so pricing can be tested.
CREATE TABLE subscription_plans (
  id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  code           TEXT NOT NULL UNIQUE,
  name           TEXT NOT NULL,
  description    TEXT,
  price_minor    BIGINT NOT NULL CHECK (price_minor >= 0),
  billing_period TEXT NOT NULL CHECK (billing_period IN ('monthly', 'quarterly', 'annual')),
  benefits       JSONB NOT NULL DEFAULT '{}',  -- {"replacement_guarantee_days":90,"discount_pct":10}
  is_active      BOOLEAN NOT NULL DEFAULT false,
  created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TYPE subscription_status AS ENUM ('active', 'past_due', 'cancelled', 'expired');
CREATE TABLE subscriptions (
  id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  client_id            UUID NOT NULL REFERENCES clients(user_id),
  plan_id              UUID NOT NULL REFERENCES subscription_plans(id),
  status               subscription_status NOT NULL DEFAULT 'active',
  current_period_start DATE NOT NULL,
  current_period_end   DATE NOT NULL,
  cancel_at_period_end BOOLEAN NOT NULL DEFAULT false,
  cancelled_at         TIMESTAMPTZ,
  created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX subscriptions_one_active_idx ON subscriptions (client_id) WHERE status IN ('active', 'past_due');
CREATE TRIGGER subscriptions_updated_at BEFORE UPDATE ON subscriptions FOR EACH ROW EXECUTE FUNCTION set_updated_at();
ALTER TABLE invoices ADD CONSTRAINT invoices_subscription_fk FOREIGN KEY (subscription_id) REFERENCES subscriptions(id);

-- +goose Down
ALTER TABLE IF EXISTS invoices DROP CONSTRAINT IF EXISTS invoices_subscription_fk;
DROP TABLE IF EXISTS subscriptions;
DROP TYPE IF EXISTS subscription_status;
DROP TABLE IF EXISTS subscription_plans;
DROP TABLE IF EXISTS ledger_entries;
DROP TYPE IF EXISTS ledger_account;
ALTER TABLE IF EXISTS provider_earnings DROP CONSTRAINT IF EXISTS provider_earnings_payout_fk;
DROP TABLE IF EXISTS payouts;
DROP TABLE IF EXISTS payout_batches;
DROP TYPE IF EXISTS payout_status;
DROP TABLE IF EXISTS provider_earnings;
DROP TYPE IF EXISTS earning_status;
DROP TABLE IF EXISTS refunds;
DROP TYPE IF EXISTS refund_status;
DROP TABLE IF EXISTS mpesa_callbacks;
DROP TABLE IF EXISTS payments;
DROP TYPE IF EXISTS payment_status;
DROP TYPE IF EXISTS payment_method;
DROP TABLE IF EXISTS invoice_lines;
DROP TYPE IF EXISTS invoice_line_kind;
DROP TABLE IF EXISTS invoices;
DROP TYPE IF EXISTS invoice_status;
