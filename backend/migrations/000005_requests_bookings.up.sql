-- 000005 requests and bookings: client requests, triage shortlist, provider assignment,
-- visits (check-in / completion), scope variations, ratings, exception log.
-- Implements report §5.2. Requests are queued to management, never broadcast to providers.

CREATE TYPE request_status AS ENUM (
  'submitted',              -- in the triage queue
  'triaging',               -- picked up by a triage officer
  'awaiting_client_choice', -- shortlist shared with the client
  'awaiting_provider',      -- offer sent to the selected provider
  'confirmed',              -- provider accepted, client notified
  'in_progress',            -- at least one visit checked in
  'awaiting_rating',        -- work complete, mandatory rating outstanding
  'closed',                 -- rated and settled
  'cancelled',
  'unfilled'                -- no suitable provider found
);

CREATE TYPE party AS ENUM ('client', 'provider', 'management', 'system');

CREATE TABLE service_requests (
  id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  reference           TEXT NOT NULL UNIQUE DEFAULT ('REQ-' || to_char(now(), 'YYYY') || '-' || lpad(nextval('request_ref_seq')::text, 6, '0')),
  client_id           UUID NOT NULL REFERENCES clients(user_id),
  address_id          UUID NOT NULL REFERENCES addresses(id),
  service_area_id     UUID REFERENCES service_areas(id),
  category_id         UUID NOT NULL REFERENCES service_categories(id),
  service_id          UUID NOT NULL REFERENCES services(id),
  rate_card_entry_id  UUID NOT NULL REFERENCES rate_card_entries(id),
  quantity            NUMERIC(8,2) NOT NULL DEFAULT 1 CHECK (quantity > 0),
  -- Price snapshot at submission (rate card only, no free-form pricing).
  client_price_minor  BIGINT NOT NULL,
  provider_pay_minor  BIGINT NOT NULL,
  placement_fee_minor BIGINT NOT NULL DEFAULT 0,
  total_minor         BIGINT NOT NULL,
  currency            CHAR(3) NOT NULL DEFAULT 'KES',
  requested_start     TIMESTAMPTZ NOT NULL,
  requested_end       TIMESTAMPTZ,
  recurrence_rule     TEXT,               -- RFC 5545 RRULE for day-based recurring work
  client_notes        TEXT,
  status              request_status NOT NULL DEFAULT 'submitted',
  priority            SMALLINT NOT NULL DEFAULT 0,
  triage_officer_id   UUID REFERENCES users(id),
  offer_client_choice BOOLEAN NOT NULL DEFAULT false,
  replaces_request_id UUID REFERENCES service_requests(id),   -- replacement guarantee
  submitted_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
  triage_started_at   TIMESTAMPTZ,
  confirmed_at        TIMESTAMPTZ,
  completed_at        TIMESTAMPTZ,
  closed_at           TIMESTAMPTZ,
  cancelled_at        TIMESTAMPTZ,
  cancelled_by        party,
  cancel_reason       TEXT,
  created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
  CHECK (requested_end IS NULL OR requested_end > requested_start),
  CHECK (status <> 'cancelled' OR (cancelled_at IS NOT NULL AND cancelled_by IS NOT NULL))
);
CREATE INDEX service_requests_queue_idx ON service_requests (status, priority DESC, submitted_at)
  WHERE status IN ('submitted', 'triaging', 'awaiting_client_choice', 'awaiting_provider');
CREATE INDEX service_requests_client_idx ON service_requests (client_id, submitted_at DESC);
CREATE TRIGGER service_requests_updated_at BEFORE UPDATE ON service_requests FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Ranked shortlist produced by the matching engine. The engine proposes; it never assigns.
CREATE TABLE shortlist_entries (
  id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  request_id       UUID NOT NULL REFERENCES service_requests(id) ON DELETE CASCADE,
  provider_id      UUID NOT NULL REFERENCES providers(user_id),
  rank             SMALLINT NOT NULL,
  score            NUMERIC(6,3) NOT NULL,
  score_breakdown  JSONB NOT NULL,     -- {"skill":..,"distance_km":..,"rating":..,"availability":..,"workload":..}
  shown_to_client  BOOLEAN NOT NULL DEFAULT false,
  client_selected  BOOLEAN NOT NULL DEFAULT false,
  generated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (request_id, provider_id)
);
CREATE INDEX shortlist_entries_request_idx ON shortlist_entries (request_id, rank);

-- Management's selection of a provider and the offer sent to them. Selection reason is
-- mandatory (§5.2 step 5). Providers can answer on the web, by USSD or by SMS.
CREATE TYPE assignment_status AS ENUM ('offered', 'accepted', 'declined', 'expired', 'withdrawn', 'replaced', 'completed');
CREATE TYPE channel AS ENUM ('web', 'app', 'ussd', 'sms', 'whatsapp', 'console');
CREATE TABLE assignments (
  id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  request_id       UUID NOT NULL REFERENCES service_requests(id),
  provider_id      UUID NOT NULL REFERENCES providers(user_id),
  status           assignment_status NOT NULL DEFAULT 'offered',
  selected_by      UUID NOT NULL REFERENCES users(id),
  selection_reason TEXT NOT NULL,
  offered_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
  offer_expires_at TIMESTAMPTZ NOT NULL,
  responded_at     TIMESTAMPTZ,
  response_channel channel,
  decline_reason   TEXT,
  ended_at         TIMESTAMPTZ,
  ended_reason     TEXT,
  created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX assignments_one_live_per_request_idx ON assignments (request_id) WHERE status IN ('offered', 'accepted');
CREATE INDEX assignments_provider_idx ON assignments (provider_id, status);
CREATE TRIGGER assignments_updated_at BEFORE UPDATE ON assignments FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Each attendance. One-off jobs have one visit; day-based work has one per occurrence;
-- a live-in placement has one visit for the start day (ongoing billing is monthly).
-- client_event_id makes offline check-in/out idempotent when the device syncs later.
CREATE TYPE visit_status AS ENUM ('scheduled', 'checked_in', 'completed', 'client_confirmed', 'missed', 'cancelled');
CREATE TYPE checkin_method AS ENUM ('geo', 'code', 'ussd', 'console');
CREATE TABLE visits (
  id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  request_id          UUID NOT NULL REFERENCES service_requests(id),
  assignment_id       UUID NOT NULL REFERENCES assignments(id),
  provider_id         UUID NOT NULL REFERENCES providers(user_id),
  scheduled_start     TIMESTAMPTZ NOT NULL,
  scheduled_end       TIMESTAMPTZ NOT NULL,
  status              visit_status NOT NULL DEFAULT 'scheduled',
  checkin_code_hash   BYTEA,             -- code the client reads out to the provider
  checked_in_at       TIMESTAMPTZ,
  checkin_method      checkin_method,
  checkin_lat         NUMERIC(9,6),
  checkin_lng         NUMERIC(9,6),
  checkin_distance_m  INT,
  checked_out_at      TIMESTAMPTZ,
  provider_notes      TEXT,
  client_confirmed_at TIMESTAMPTZ,
  device_recorded_at  TIMESTAMPTZ,       -- when the offline device captured the event
  created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
  CHECK (scheduled_end > scheduled_start)
);
CREATE INDEX visits_provider_schedule_idx ON visits (provider_id, scheduled_start);
CREATE INDEX visits_request_idx ON visits (request_id);
CREATE TRIGGER visits_updated_at BEFORE UPDATE ON visits FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Idempotency for events captured offline by the provider PWA / USSD and synced later.
CREATE TABLE visit_events (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  visit_id        UUID NOT NULL REFERENCES visits(id),
  client_event_id UUID NOT NULL UNIQUE,  -- generated on the device
  event_type      TEXT NOT NULL CHECK (event_type IN ('check_in', 'check_out', 'note')),
  payload         JSONB NOT NULL DEFAULT '{}',
  device_recorded_at TIMESTAMPTZ NOT NULL,
  received_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
  channel         channel NOT NULL
);
CREATE INDEX visit_events_visit_idx ON visit_events (visit_id);

-- Scope or price variations must be approved in-platform first (§5.2 step 9).
CREATE TYPE variation_status AS ENUM ('pending', 'approved', 'rejected', 'withdrawn');
CREATE TABLE variations (
  id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  request_id         UUID NOT NULL REFERENCES service_requests(id),
  visit_id           UUID REFERENCES visits(id),
  raised_by          party NOT NULL,
  raised_by_user_id  UUID NOT NULL REFERENCES users(id),
  description        TEXT NOT NULL,
  rate_card_entry_id UUID REFERENCES rate_card_entries(id),
  quantity           NUMERIC(8,2),
  client_delta_minor BIGINT NOT NULL DEFAULT 0,
  provider_delta_minor BIGINT NOT NULL DEFAULT 0,
  status             variation_status NOT NULL DEFAULT 'pending',
  client_approved_at TIMESTAMPTZ,
  decided_by         UUID REFERENCES users(id),
  decided_at         TIMESTAMPTZ,
  created_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX variations_request_idx ON variations (request_id);

-- Mandatory rating closes the job and feeds appraisal (§5.2 step 10).
CREATE TABLE ratings (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  request_id  UUID NOT NULL REFERENCES service_requests(id),
  visit_id    UUID REFERENCES visits(id),
  client_id   UUID NOT NULL REFERENCES clients(user_id),
  provider_id UUID NOT NULL REFERENCES providers(user_id),
  stars       SMALLINT NOT NULL CHECK (stars BETWEEN 1 AND 5),
  sub_scores  JSONB NOT NULL DEFAULT '{}',   -- {"punctuality":5,"quality":4,"conduct":5}
  comment     TEXT,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX ratings_one_per_visit_idx ON ratings (visit_id) WHERE visit_id IS NOT NULL;
CREATE UNIQUE INDEX ratings_one_per_request_idx ON ratings (request_id) WHERE visit_id IS NULL;
CREATE INDEX ratings_provider_idx ON ratings (provider_id, created_at DESC);

-- Daily exception review (§5.2 step 12): late arrival, low rating, dispute, no-show.
CREATE TYPE exception_kind AS ENUM ('late_arrival', 'no_show', 'low_rating', 'scope_dispute', 'offer_expired', 'unfilled', 'geo_mismatch', 'other');
CREATE TABLE job_exceptions (
  id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  kind         exception_kind NOT NULL,
  request_id   UUID REFERENCES service_requests(id),
  visit_id     UUID REFERENCES visits(id),
  provider_id  UUID REFERENCES providers(user_id),
  details      JSONB NOT NULL DEFAULT '{}',
  detected_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  reviewed_by  UUID REFERENCES users(id),
  reviewed_at  TIMESTAMPTZ,
  resolution   TEXT
);
CREATE INDEX job_exceptions_open_idx ON job_exceptions (detected_at) WHERE reviewed_at IS NULL;
