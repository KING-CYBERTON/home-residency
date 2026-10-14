-- +goose Up
-- 000003 catalogue: service categories, services, rate card, vetting checklists.
-- Everything is data-driven so the pilot can switch lines on/off with is_active and no
-- code change (report §7.1: launch with one or two lines).

CREATE TYPE engagement_type AS ENUM (
  'live_in',       -- live-in domestic worker placement (long-running)
  'day_based',     -- recurring day-based worker ("day bugs" in the source report)
  'one_off'        -- single job: cleaning, plumbing, moving, pest control
);

CREATE TYPE vetting_tier AS ENUM ('standard', 'trade', 'enhanced');  -- enhanced = caregiving (§7.5)

CREATE TABLE service_categories (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  slug            TEXT NOT NULL UNIQUE,           -- 'cleaning', 'caregiver', 'plumber'
  name            TEXT NOT NULL,
  name_sw         TEXT,
  description     TEXT,
  engagement      engagement_type NOT NULL,
  vetting_tier    vetting_tier NOT NULL DEFAULT 'standard',
  requires_client_id_verification BOOLEAN NOT NULL DEFAULT false,  -- true for live-in
  sop_markdown    TEXT,                           -- service standard ratings are judged against
  is_active       BOOLEAN NOT NULL DEFAULT false,
  sort_order      INT NOT NULL DEFAULT 0,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TRIGGER service_categories_updated_at BEFORE UPDATE ON service_categories FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- A bookable service within a category, e.g. "Standard 3-bedroom clean".
CREATE TYPE pricing_unit AS ENUM ('per_job', 'per_hour', 'per_day', 'per_month', 'per_room');
CREATE TABLE services (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  category_id   UUID NOT NULL REFERENCES service_categories(id),
  slug          TEXT NOT NULL,
  name          TEXT NOT NULL,
  description   TEXT,
  scope_items   JSONB NOT NULL DEFAULT '[]',     -- checklist of what is included
  pricing_unit  pricing_unit NOT NULL,
  min_quantity  NUMERIC(8,2) NOT NULL DEFAULT 1,
  max_quantity  NUMERIC(8,2),
  default_duration_minutes INT,
  is_active     BOOLEAN NOT NULL DEFAULT false,
  sort_order    INT NOT NULL DEFAULT 0,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (category_id, slug)
);
CREATE TRIGGER services_updated_at BEFORE UPDATE ON services FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Rate card: versioned prices. Clients only ever book at a rate-card price (no free-form
-- pricing, §5.2 step 2). Bookings copy the rate row id so history survives price changes.
-- provider_pay_minor carries the provider's share; it must respect the wage floor where
-- the platform is the employer (§7.3).
CREATE TABLE rate_card_entries (
  id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  service_id         UUID NOT NULL REFERENCES services(id),
  service_area_id    UUID REFERENCES service_areas(id),   -- NULL = applies everywhere
  client_price_minor BIGINT NOT NULL CHECK (client_price_minor >= 0),
  provider_pay_minor BIGINT NOT NULL CHECK (provider_pay_minor >= 0),
  placement_fee_minor BIGINT NOT NULL DEFAULT 0 CHECK (placement_fee_minor >= 0), -- live-in/day-based
  currency           CHAR(3) NOT NULL DEFAULT 'KES',
  valid_from         TIMESTAMPTZ NOT NULL DEFAULT now(),
  valid_to           TIMESTAMPTZ,
  created_by         UUID REFERENCES users(id),
  created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
  CHECK (provider_pay_minor <= client_price_minor),
  CHECK (valid_to IS NULL OR valid_to > valid_from),
  -- No overlapping validity windows for the same service + area.
  EXCLUDE USING gist (
    service_id WITH =,
    COALESCE(service_area_id, '00000000-0000-0000-0000-000000000000'::uuid) WITH =,
    tstzrange(valid_from, valid_to) WITH &&
  )
);
CREATE INDEX rate_card_service_idx ON rate_card_entries (service_id, valid_from DESC);

-- Vetting checklist per category (report §5.1 step 2, §7.5).
CREATE TYPE checklist_item_kind AS ENUM (
  'document',          -- upload required (document_type says which)
  'referee',           -- traceable referee check
  'clearance',         -- Certificate of Good Conduct independently verified
  'interview',
  'practical_assessment',
  'trade_licence',
  'medical',
  'training'
);

CREATE TYPE document_type AS ENUM (
  'national_id', 'passport', 'kra_pin', 'good_conduct_certificate', 'nssf_card', 'shif_card',
  'academic_certificate', 'trade_certificate', 'trade_licence', 'medical_certificate',
  'first_aid_certificate', 'reference_letter', 'passport_photo', 'signed_contract', 'other'
);

CREATE TABLE checklist_templates (
  id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  category_id  UUID NOT NULL REFERENCES service_categories(id),
  version      INT NOT NULL,
  is_current   BOOLEAN NOT NULL DEFAULT false,
  approved_by  UUID REFERENCES users(id),
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (category_id, version)
);
CREATE UNIQUE INDEX checklist_templates_current_idx ON checklist_templates (category_id) WHERE is_current;

CREATE TABLE checklist_template_items (
  id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  template_id      UUID NOT NULL REFERENCES checklist_templates(id) ON DELETE CASCADE,
  kind             checklist_item_kind NOT NULL,
  document_type    document_type,
  label            TEXT NOT NULL,
  is_mandatory     BOOLEAN NOT NULL DEFAULT true,
  min_count        SMALLINT NOT NULL DEFAULT 1,     -- e.g. 2 referees
  validity_months  SMALLINT,                        -- drives expiry/re-verification
  sort_order       INT NOT NULL DEFAULT 0,
  CHECK (kind <> 'document' OR document_type IS NOT NULL)
);
CREATE INDEX checklist_template_items_template_idx ON checklist_template_items (template_id);

-- Training courses (induction + category training, §5.1 step 8).
CREATE TABLE training_courses (
  id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  code         TEXT NOT NULL UNIQUE,
  name         TEXT NOT NULL,
  category_id  UUID REFERENCES service_categories(id),  -- NULL = general induction
  is_induction BOOLEAN NOT NULL DEFAULT false,
  pass_mark    SMALLINT NOT NULL DEFAULT 70 CHECK (pass_mark BETWEEN 0 AND 100),
  is_active    BOOLEAN NOT NULL DEFAULT true,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE IF EXISTS training_courses;
DROP TABLE IF EXISTS checklist_template_items;
DROP TABLE IF EXISTS checklist_templates;
DROP TYPE IF EXISTS document_type;
DROP TYPE IF EXISTS checklist_item_kind;
DROP TABLE IF EXISTS rate_card_entries;
DROP TABLE IF EXISTS services;
DROP TYPE IF EXISTS pricing_unit;
DROP TABLE IF EXISTS service_categories;
DROP TYPE IF EXISTS vetting_tier;
DROP TYPE IF EXISTS engagement_type;
