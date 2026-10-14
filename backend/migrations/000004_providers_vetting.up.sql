-- 000004 providers and vetting: provider profiles, applications, document vault metadata,
-- referees, clearance, assessments, approval, training, contracts, availability.
-- Implements report §5.1 (recruitment, vetting, onboarding).

CREATE TYPE provider_status AS ENUM (
  'applicant',      -- has an account, application not yet approved
  'approved',       -- approved by management, training pending
  'active',         -- trained, contracted, can be assigned
  'suspended',      -- expired document, safeguarding hold, disciplinary suspension
  'disqualified',   -- detached; access revoked (two-officer authorisation)
  'withdrawn'       -- left voluntarily
);

-- Pending the legal-model decision (report §7.3, §11 decision 1) the engagement basis is
-- stored per provider so either model can be run without a schema change.
CREATE TYPE provider_engagement_basis AS ENUM ('independent_contractor', 'employee', 'undecided');

CREATE TABLE providers (
  user_id              UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  status               provider_status NOT NULL DEFAULT 'applicant',
  engagement_basis     provider_engagement_basis NOT NULL DEFAULT 'undecided',
  national_id_enc      BYTEA,
  kra_pin_enc          BYTEA,
  date_of_birth        DATE,
  home_service_area_id UUID REFERENCES service_areas(id),
  home_lat             NUMERIC(9,6),
  home_lng             NUMERIC(9,6),
  bio                  TEXT,                -- shown to clients (no contact details)
  years_experience     SMALLINT,
  languages            TEXT[] NOT NULL DEFAULT '{en}',
  photo_document_id    UUID,                -- FK added below once documents exists
  payout_msisdn        TEXT CHECK (payout_msisdn ~ '^\+[1-9][0-9]{7,14}$'),
  appraisal_score      NUMERIC(5,2),        -- 0-100, recomputed by the appraisal job
  rating_avg           NUMERIC(3,2),        -- 1.00-5.00
  rating_count         INT NOT NULL DEFAULT 0,
  completed_jobs       INT NOT NULL DEFAULT 0,
  max_concurrent_jobs  SMALLINT NOT NULL DEFAULT 1,
  activated_at         TIMESTAMPTZ,
  suspended_reason     TEXT,
  created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX providers_status_idx ON providers (status);
CREATE TRIGGER providers_updated_at BEFORE UPDATE ON providers FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TYPE provider_category_status AS ENUM ('pending', 'approved', 'active', 'suspended', 'revoked');
CREATE TABLE provider_categories (
  provider_id  UUID NOT NULL REFERENCES providers(user_id) ON DELETE CASCADE,
  category_id  UUID NOT NULL REFERENCES service_categories(id),
  status       provider_category_status NOT NULL DEFAULT 'pending',
  approved_at  TIMESTAMPTZ,
  activated_at TIMESTAMPTZ,
  updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (provider_id, category_id)
);
CREATE INDEX provider_categories_matching_idx ON provider_categories (category_id, status);
CREATE TRIGGER provider_categories_updated_at BEFORE UPDATE ON provider_categories FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE provider_service_areas (
  provider_id     UUID NOT NULL REFERENCES providers(user_id) ON DELETE CASCADE,
  service_area_id UUID NOT NULL REFERENCES service_areas(id),
  PRIMARY KEY (provider_id, service_area_id)
);

-- Document vault metadata. Files live in a private, KMS-encrypted S3 bucket; the API
-- hands out short-lived presigned URLs and logs every access (document_access_log).
CREATE TYPE document_status AS ENUM ('pending_upload', 'uploaded', 'verified', 'rejected', 'expired', 'purged');
CREATE TABLE documents (
  id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  owner_user_id      UUID NOT NULL REFERENCES users(id),
  document_type      document_type NOT NULL,
  status             document_status NOT NULL DEFAULT 'pending_upload',
  s3_key             TEXT NOT NULL UNIQUE,
  original_filename  TEXT,
  mime_type          TEXT NOT NULL CHECK (mime_type IN ('image/jpeg', 'image/png', 'image/webp', 'application/pdf')),
  size_bytes         BIGINT CHECK (size_bytes > 0 AND size_bytes <= 10485760),
  sha256             BYTEA,
  document_number_enc BYTEA,
  issued_on          DATE,
  expires_on         DATE,
  verified_by        UUID REFERENCES users(id),
  verified_at        TIMESTAMPTZ,
  rejection_reason   TEXT,
  retention_until    DATE,                 -- set from the retention schedule per type
  purged_at          TIMESTAMPTZ,
  created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX documents_owner_idx ON documents (owner_user_id, document_type);
CREATE INDEX documents_expiry_idx ON documents (expires_on) WHERE status = 'verified';
CREATE INDEX documents_retention_idx ON documents (retention_until) WHERE purged_at IS NULL;
CREATE TRIGGER documents_updated_at BEFORE UPDATE ON documents FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE providers ADD CONSTRAINT providers_photo_fk FOREIGN KEY (photo_document_id) REFERENCES documents(id);

-- One application per provider per category. Vetter and approver must differ (§5.1 step 7).
CREATE TYPE application_status AS ENUM (
  'draft', 'submitted', 'incomplete', 'in_vetting', 'awaiting_approval',
  'approved', 'deferred', 'rejected', 'withdrawn'
);
CREATE TABLE applications (
  id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  reference         TEXT NOT NULL UNIQUE DEFAULT ('APP-' || to_char(now(), 'YYYY') || '-' || lpad(nextval('application_ref_seq')::text, 6, '0')),
  provider_id       UUID NOT NULL REFERENCES providers(user_id),
  category_id       UUID NOT NULL REFERENCES service_categories(id),
  template_id       UUID NOT NULL REFERENCES checklist_templates(id),
  status            application_status NOT NULL DEFAULT 'draft',
  answers           JSONB NOT NULL DEFAULT '{}',   -- category questionnaire answers
  submitted_at      TIMESTAMPTZ,
  vetting_officer_id UUID REFERENCES users(id),
  vetting_report    TEXT,
  vetting_completed_at TIMESTAMPTZ,
  decided_by        UUID REFERENCES users(id),
  decision_reason   TEXT,
  decided_at        TIMESTAMPTZ,
  is_reverification BOOLEAN NOT NULL DEFAULT false,
  created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT applications_vetter_not_approver CHECK (decided_by IS NULL OR decided_by IS DISTINCT FROM vetting_officer_id),
  CONSTRAINT applications_decision_has_reason CHECK (
    status NOT IN ('approved', 'deferred', 'rejected') OR (decided_by IS NOT NULL AND decision_reason IS NOT NULL)
  )
);
CREATE UNIQUE INDEX applications_one_open_per_category_idx ON applications (provider_id, category_id)
  WHERE status NOT IN ('approved', 'rejected', 'withdrawn');
CREATE INDEX applications_queue_idx ON applications (status, submitted_at);
CREATE TRIGGER applications_updated_at BEFORE UPDATE ON applications FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Per-application checklist state, instantiated from the template on submit.
CREATE TYPE checklist_item_status AS ENUM ('pending', 'submitted', 'satisfied', 'failed', 'waived');
CREATE TABLE application_items (
  id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  application_id   UUID NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
  template_item_id UUID NOT NULL REFERENCES checklist_template_items(id),
  status           checklist_item_status NOT NULL DEFAULT 'pending',
  document_id      UUID REFERENCES documents(id),
  notes            TEXT,
  reviewed_by      UUID REFERENCES users(id),
  reviewed_at      TIMESTAMPTZ,
  updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (application_id, template_item_id)
);
CREATE TRIGGER application_items_updated_at BEFORE UPDATE ON application_items FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Two traceable referees, conversation recorded (§5.1 step 4).
CREATE TYPE referee_outcome AS ENUM ('pending', 'positive', 'mixed', 'negative', 'unreachable');
CREATE TABLE referees (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  application_id  UUID NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
  full_name       TEXT NOT NULL,
  relationship    TEXT NOT NULL,          -- 'former employer', 'supervisor'
  phone_enc       BYTEA NOT NULL,
  employer_name   TEXT,
  employment_from DATE,
  employment_to   DATE,
  outcome         referee_outcome NOT NULL DEFAULT 'pending',
  statement_enc   BYTEA,                  -- notes of the conversation
  contacted_by    UUID REFERENCES users(id),
  contacted_at    TIMESTAMPTZ,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX referees_application_idx ON referees (application_id);

-- Independent confirmation of the Certificate of Good Conduct (§5.1 step 5, §7.5).
CREATE TYPE clearance_result AS ENUM ('pending', 'authentic', 'not_authentic', 'expired', 'adverse');
CREATE TABLE clearance_checks (
  id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  application_id   UUID NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
  document_id      UUID NOT NULL REFERENCES documents(id),
  agency_name      TEXT,
  agency_reference TEXT,
  result           clearance_result NOT NULL DEFAULT 'pending',
  requested_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
  confirmed_at     TIMESTAMPTZ,
  recorded_by      UUID REFERENCES users(id),
  notes            TEXT
);
CREATE INDEX clearance_checks_application_idx ON clearance_checks (application_id);

-- Interview and practical assessment scored against a rubric (§5.1 step 6).
CREATE TYPE assessment_kind AS ENUM ('interview', 'practical');
CREATE TABLE assessments (
  id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  application_id UUID NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
  kind           assessment_kind NOT NULL,
  rubric_version TEXT NOT NULL,
  scores         JSONB NOT NULL,            -- {"criterion": score, ...}
  total_score    NUMERIC(5,2) NOT NULL CHECK (total_score BETWEEN 0 AND 100),
  passed         BOOLEAN NOT NULL,
  assessed_by    UUID NOT NULL REFERENCES users(id),
  assessed_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  notes          TEXT
);
CREATE INDEX assessments_application_idx ON assessments (application_id);

-- Training enrolments: induction, category training and retraining after complaints.
CREATE TYPE training_status AS ENUM ('scheduled', 'attended', 'passed', 'failed', 'no_show', 'cancelled');
CREATE TABLE training_enrolments (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  provider_id   UUID NOT NULL REFERENCES providers(user_id),
  course_id     UUID NOT NULL REFERENCES training_courses(id),
  status        training_status NOT NULL DEFAULT 'scheduled',
  is_retraining BOOLEAN NOT NULL DEFAULT false,
  complaint_id  UUID,                       -- FK added in 000007
  scheduled_for TIMESTAMPTZ,
  attended_at   TIMESTAMPTZ,
  score         NUMERIC(5,2) CHECK (score BETWEEN 0 AND 100),
  recorded_by   UUID REFERENCES users(id),
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX training_enrolments_provider_idx ON training_enrolments (provider_id, status);
CREATE TRIGGER training_enrolments_updated_at BEFORE UPDATE ON training_enrolments FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Platform contract and code of conduct, signed by OTP (§5.1 step 9).
CREATE TABLE provider_contracts (
  id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  provider_id           UUID NOT NULL REFERENCES providers(user_id),
  contract_version      TEXT NOT NULL,
  code_of_conduct_version TEXT NOT NULL,
  engagement_basis      provider_engagement_basis NOT NULL,
  document_id           UUID REFERENCES documents(id),   -- rendered PDF copy
  signed_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
  signature_method      TEXT NOT NULL DEFAULT 'sms_otp',
  signature_ip          INET,
  terminated_at         TIMESTAMPTZ
);
CREATE INDEX provider_contracts_provider_idx ON provider_contracts (provider_id, signed_at DESC);

-- Weekly availability pattern plus explicit time off.
CREATE TABLE provider_availability (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  provider_id UUID NOT NULL REFERENCES providers(user_id) ON DELETE CASCADE,
  day_of_week SMALLINT NOT NULL CHECK (day_of_week BETWEEN 0 AND 6),  -- 0 = Sunday
  start_time  TIME NOT NULL,
  end_time    TIME NOT NULL,
  CHECK (end_time > start_time)
);
CREATE INDEX provider_availability_idx ON provider_availability (provider_id, day_of_week);

CREATE TABLE provider_time_off (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  provider_id UUID NOT NULL REFERENCES providers(user_id) ON DELETE CASCADE,
  period      TSTZRANGE NOT NULL,
  reason      TEXT,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  EXCLUDE USING gist (provider_id WITH =, period WITH &&)
);
