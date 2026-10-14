-- 000008 platform: notifications, mediated messages, public content, settings, audit log,
-- vault access log, transactional outbox (to SQS), API idempotency keys, scheduled jobs.

-- Notification templates per event and channel, in English and Swahili.
CREATE TYPE notification_channel AS ENUM ('sms', 'whatsapp', 'email', 'web_push', 'in_app');
CREATE TABLE notification_templates (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  event_key   TEXT NOT NULL,         -- 'request.confirmed', 'offer.sent', 'document.expiring'
  channel     notification_channel NOT NULL,
  language    TEXT NOT NULL DEFAULT 'en' CHECK (language IN ('en', 'sw')),
  subject     TEXT,
  body        TEXT NOT NULL,         -- Go text/template
  is_active   BOOLEAN NOT NULL DEFAULT true,
  updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (event_key, channel, language)
);
CREATE TRIGGER notification_templates_updated_at BEFORE UPDATE ON notification_templates FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TYPE notification_status AS ENUM ('queued', 'sent', 'delivered', 'failed', 'read');
CREATE TABLE notifications (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id         UUID NOT NULL REFERENCES users(id),
  event_key       TEXT NOT NULL,
  channel         notification_channel NOT NULL,
  status          notification_status NOT NULL DEFAULT 'queued',
  title           TEXT,
  body            TEXT NOT NULL,
  data            JSONB NOT NULL DEFAULT '{}',   -- deep link target, ids
  provider_message_id TEXT,                      -- SMS/WhatsApp gateway id
  error           TEXT,
  attempts        SMALLINT NOT NULL DEFAULT 0,
  sent_at         TIMESTAMPTZ,
  read_at         TIMESTAMPTZ,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX notifications_inbox_idx ON notifications (user_id, created_at DESC) WHERE channel = 'in_app';
CREATE INDEX notifications_queue_idx ON notifications (status, created_at) WHERE status = 'queued';

CREATE TABLE web_push_subscriptions (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  endpoint   TEXT NOT NULL UNIQUE,
  p256dh     TEXT NOT NULL,
  auth       TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Management-mediated messaging (Could-have). Client and provider never message each
-- other directly; each conversation is between one party and management.
CREATE TABLE conversations (
  id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  participant_id UUID NOT NULL REFERENCES users(id),   -- the client or provider
  request_id   UUID REFERENCES service_requests(id),
  complaint_id UUID REFERENCES complaints(id),
  subject      TEXT,
  closed_at    TIMESTAMPTZ,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX conversations_participant_idx ON conversations (participant_id, created_at DESC);

CREATE TABLE messages (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  conversation_id UUID NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
  sender_id       UUID NOT NULL REFERENCES users(id),
  body            TEXT NOT NULL,
  channel         channel NOT NULL DEFAULT 'web',
  read_at         TIMESTAMPTZ,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX messages_conversation_idx ON messages (conversation_id, created_at);

-- Public site content (simple CMS for marketing pages, FAQs, local SEO pages).
CREATE TABLE content_pages (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  slug          TEXT NOT NULL,
  language      TEXT NOT NULL DEFAULT 'en' CHECK (language IN ('en', 'sw')),
  title         TEXT NOT NULL,
  meta_description TEXT,
  body_markdown TEXT NOT NULL,
  category_id   UUID REFERENCES service_categories(id),
  service_area_id UUID REFERENCES service_areas(id),
  is_published  BOOLEAN NOT NULL DEFAULT false,
  published_at  TIMESTAMPTZ,
  updated_by    UUID REFERENCES users(id),
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (slug, language)
);
CREATE TRIGGER content_pages_updated_at BEFORE UPDATE ON content_pages FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE faqs (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  category_id UUID REFERENCES service_categories(id),
  language    TEXT NOT NULL DEFAULT 'en' CHECK (language IN ('en', 'sw')),
  question    TEXT NOT NULL,
  answer      TEXT NOT NULL,
  sort_order  INT NOT NULL DEFAULT 0,
  is_published BOOLEAN NOT NULL DEFAULT true
);

-- Leads from the public site before a household registers (estate partners, enquiries).
CREATE TABLE leads (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  full_name   TEXT NOT NULL,
  phone_e164  TEXT,
  email       CITEXT,
  service_area_id UUID REFERENCES service_areas(id),
  category_id UUID REFERENCES service_categories(id),
  message     TEXT,
  source      TEXT,                      -- utm / partner code
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Tunable business rules (thresholds, offer expiry, appraisal weights, payout cycle).
CREATE TABLE app_settings (
  key         TEXT PRIMARY KEY,
  value       JSONB NOT NULL,
  description TEXT,
  updated_by  UUID REFERENCES users(id),
  updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
INSERT INTO app_settings (key, value, description) VALUES
  ('rating.complaint_threshold', '3', 'Ratings at or below this many stars open a complaint automatically'),
  ('offer.expiry_minutes', '120', 'How long a provider has to accept an offer'),
  ('checkin.max_distance_m', '300', 'Geo check-in tolerance from the client address'),
  ('appraisal.weights', '{"rating":0.5,"complaints":0.2,"spot_checks":0.2,"reliability":0.1}', 'Published appraisal weighting'),
  ('payout.cycle', '{"frequency":"weekly","weekday":5}', 'Provider payout cycle'),
  ('commission.default_pct', '20', 'Default commission when rate card pay is not explicit'),
  ('document.expiry_warning_days', '30', 'Warn providers this many days before a document expires');

-- Append-only audit trail of every state-changing action (§4 branch E, DPA 2019).
CREATE TABLE audit_log (
  id           BIGSERIAL PRIMARY KEY,
  occurred_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  actor_id     UUID REFERENCES users(id),
  actor_kind   user_kind,
  action       TEXT NOT NULL,           -- 'application.approve', 'assignment.create'
  entity_type  TEXT NOT NULL,
  entity_id    UUID,
  before_state JSONB,
  after_state  JSONB,
  reason       TEXT,
  ip_address   INET,
  request_id   TEXT                     -- correlation id from the HTTP request
);
CREATE INDEX audit_log_entity_idx ON audit_log (entity_type, entity_id, occurred_at DESC);
CREATE INDEX audit_log_actor_idx ON audit_log (actor_id, occurred_at DESC);

-- Every read of a vault document (presigned URL issued) is logged.
CREATE TABLE document_access_log (
  id          BIGSERIAL PRIMARY KEY,
  document_id UUID NOT NULL REFERENCES documents(id),
  accessed_by UUID NOT NULL REFERENCES users(id),
  purpose     TEXT NOT NULL,
  ip_address  INET,
  accessed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX document_access_log_document_idx ON document_access_log (document_id, accessed_at DESC);
CREATE INDEX document_access_log_user_idx ON document_access_log (accessed_by, accessed_at DESC);

-- Make audit tables append-only at the database level.
CREATE OR REPLACE FUNCTION forbid_mutation() RETURNS trigger AS $$
BEGIN
  RAISE EXCEPTION '% is append-only', TG_TABLE_NAME;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER audit_log_append_only BEFORE UPDATE OR DELETE ON audit_log FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
CREATE TRIGGER document_access_log_append_only BEFORE UPDATE OR DELETE ON document_access_log FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
CREATE TRIGGER ledger_entries_append_only BEFORE UPDATE OR DELETE ON ledger_entries FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

-- Transactional outbox: domain events written in the same transaction as the change,
-- then relayed to SQS by a poller (notifications, payouts, matching, appraisal).
CREATE TABLE outbox_events (
  id           BIGSERIAL PRIMARY KEY,
  event_type   TEXT NOT NULL,
  aggregate_type TEXT NOT NULL,
  aggregate_id UUID NOT NULL,
  payload      JSONB NOT NULL,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  published_at TIMESTAMPTZ,
  attempts     SMALLINT NOT NULL DEFAULT 0,
  last_error   TEXT
);
CREATE INDEX outbox_events_unpublished_idx ON outbox_events (id) WHERE published_at IS NULL;

-- Idempotency-Key header support for POSTs that move money or change state.
CREATE TABLE idempotency_keys (
  key           TEXT NOT NULL,
  user_id       UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  request_hash  BYTEA NOT NULL,
  response_code SMALLINT,
  response_body JSONB,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (user_id, key)
);
CREATE INDEX idempotency_keys_created_idx ON idempotency_keys (created_at);

-- USSD session state (job acceptance and check-in by short code).
CREATE TABLE ussd_sessions (
  session_id   TEXT PRIMARY KEY,         -- from the USSD gateway
  phone_e164   TEXT NOT NULL,
  user_id      UUID REFERENCES users(id),
  state        JSONB NOT NULL DEFAULT '{}',
  started_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  last_input_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  ended_at     TIMESTAMPTZ
);
