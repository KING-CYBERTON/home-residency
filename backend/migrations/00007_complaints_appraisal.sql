-- +goose Up
-- 000007 complaints and appraisal: complaint cases, investigation timeline, disciplinary
-- ladder, two-officer disqualification, supervisor spot checks, appraisal snapshots.
-- Implements report §5.4 and §7.10.

CREATE TYPE complaint_severity AS ENUM ('quality', 'conduct', 'safeguarding', 'loss');  -- §5.4 step 2
CREATE TYPE complaint_status AS ENUM (
  'open', 'investigating', 'awaiting_provider_account', 'awaiting_decision',
  'decided', 'closed', 'withdrawn'
);
CREATE TYPE complaint_source AS ENUM ('client', 'low_rating', 'supervisor', 'provider', 'management');

CREATE TABLE complaints (
  id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  reference         TEXT NOT NULL UNIQUE DEFAULT ('CMP-' || to_char(now(), 'YYYY') || '-' || lpad(nextval('complaint_ref_seq')::text, 6, '0')),
  source            complaint_source NOT NULL,
  severity          complaint_severity NOT NULL,
  status            complaint_status NOT NULL DEFAULT 'open',
  client_id         UUID REFERENCES clients(user_id),
  provider_id       UUID REFERENCES providers(user_id),
  request_id        UUID REFERENCES service_requests(id),
  visit_id          UUID REFERENCES visits(id),
  rating_id         UUID REFERENCES ratings(id),
  summary           TEXT NOT NULL,
  details_enc       BYTEA,               -- may contain safeguarding detail
  raised_by         UUID REFERENCES users(id),
  investigator_id   UUID REFERENCES users(id),
  provider_heard_at TIMESTAMPTZ,         -- provider must be heard before a decision
  provider_account_enc BYTEA,
  investigation_report TEXT,
  decided_by        UUID REFERENCES users(id),
  decided_at        TIMESTAMPTZ,
  outcome_summary   TEXT,
  client_informed_at   TIMESTAMPTZ,      -- client told what was done (§5.4 step 5)
  provider_informed_at TIMESTAMPTZ,
  interim_suspension BOOLEAN NOT NULL DEFAULT false,   -- auto for safeguarding/loss
  closed_at         TIMESTAMPTZ,
  created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT complaints_heard_before_decision CHECK (
    status NOT IN ('decided', 'closed') OR provider_id IS NULL OR provider_heard_at IS NOT NULL
  ),
  CONSTRAINT complaints_investigator_not_decider CHECK (
    decided_by IS NULL OR decided_by IS DISTINCT FROM investigator_id
  )
);
CREATE INDEX complaints_queue_idx ON complaints (status, severity, created_at);
CREATE INDEX complaints_provider_idx ON complaints (provider_id, created_at DESC);
CREATE TRIGGER complaints_updated_at BEFORE UPDATE ON complaints FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Case timeline: notes, evidence, status changes, communications.
CREATE TABLE complaint_events (
  id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  complaint_id UUID NOT NULL REFERENCES complaints(id) ON DELETE CASCADE,
  actor_id     UUID REFERENCES users(id),
  event_type   TEXT NOT NULL,          -- 'note','status_change','evidence','provider_account','message_client','message_provider'
  body         TEXT,
  document_id  UUID REFERENCES documents(id),
  data         JSONB NOT NULL DEFAULT '{}',
  is_internal  BOOLEAN NOT NULL DEFAULT true,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX complaint_events_complaint_idx ON complaint_events (complaint_id, created_at);

-- Disciplinary ladder (§5.4 step 4): warning, retraining, suspension, disqualification.
CREATE TYPE disciplinary_action_kind AS ENUM ('warning', 'retraining', 'suspension', 'disqualification', 'no_action');
CREATE TYPE disciplinary_action_status AS ENUM ('pending_authorisation', 'in_effect', 'completed', 'revoked');
CREATE TABLE disciplinary_actions (
  id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  provider_id      UUID NOT NULL REFERENCES providers(user_id),
  complaint_id     UUID REFERENCES complaints(id),
  kind             disciplinary_action_kind NOT NULL,
  status           disciplinary_action_status NOT NULL,
  reason           TEXT NOT NULL,
  effective_from   TIMESTAMPTZ NOT NULL DEFAULT now(),
  effective_to     TIMESTAMPTZ,          -- suspensions
  decided_by       UUID NOT NULL REFERENCES users(id),
  second_authoriser_id UUID REFERENCES users(id),  -- required for disqualification
  authorised_at    TIMESTAMPTZ,
  notice_document_id UUID REFERENCES documents(id), -- written notice
  created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT disqualification_two_officers CHECK (
    kind <> 'disqualification' OR status = 'pending_authorisation'
    OR (second_authoriser_id IS NOT NULL AND second_authoriser_id <> decided_by)
  )
);
CREATE INDEX disciplinary_actions_provider_idx ON disciplinary_actions (provider_id, created_at DESC);

ALTER TABLE training_enrolments ADD CONSTRAINT training_enrolments_complaint_fk FOREIGN KEY (complaint_id) REFERENCES complaints(id);
ALTER TABLE refunds ADD CONSTRAINT refunds_complaint_fk FOREIGN KEY (complaint_id) REFERENCES complaints(id);

-- Supervisor spot checks (Should-have) feed the appraisal score.
CREATE TABLE spot_checks (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  provider_id   UUID NOT NULL REFERENCES providers(user_id),
  visit_id      UUID REFERENCES visits(id),
  supervisor_id UUID NOT NULL REFERENCES users(id),
  checked_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  method        TEXT NOT NULL CHECK (method IN ('on_site', 'phone_client', 'phone_provider')),
  scores        JSONB NOT NULL,
  total_score   NUMERIC(5,2) NOT NULL CHECK (total_score BETWEEN 0 AND 100),
  notes         TEXT,
  complaint_id  UUID REFERENCES complaints(id)   -- if the check raised one
);
CREATE INDEX spot_checks_provider_idx ON spot_checks (provider_id, checked_at DESC);

-- Appraisal score history. Weighting is published (§7.10) and stored with each snapshot.
CREATE TABLE appraisal_snapshots (
  id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  provider_id       UUID NOT NULL REFERENCES providers(user_id),
  period_start      DATE NOT NULL,
  period_end        DATE NOT NULL,
  rating_component  NUMERIC(5,2),
  complaint_component NUMERIC(5,2),
  spot_check_component NUMERIC(5,2),
  reliability_component NUMERIC(5,2),   -- punctuality, no-shows
  weights           JSONB NOT NULL,     -- {"rating":0.5,"complaints":0.2,"spot_checks":0.2,"reliability":0.1}
  total_score       NUMERIC(5,2) NOT NULL,
  computed_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (provider_id, period_end)
);

-- +goose Down
DROP TABLE IF EXISTS appraisal_snapshots;
DROP TABLE IF EXISTS spot_checks;
ALTER TABLE IF EXISTS refunds DROP CONSTRAINT IF EXISTS refunds_complaint_fk;
ALTER TABLE IF EXISTS training_enrolments DROP CONSTRAINT IF EXISTS training_enrolments_complaint_fk;
DROP TABLE IF EXISTS disciplinary_actions;
DROP TYPE IF EXISTS disciplinary_action_status;
DROP TYPE IF EXISTS disciplinary_action_kind;
DROP TABLE IF EXISTS complaint_events;
DROP TABLE IF EXISTS complaints;
DROP TYPE IF EXISTS complaint_source;
DROP TYPE IF EXISTS complaint_status;
DROP TYPE IF EXISTS complaint_severity;
