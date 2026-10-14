-- 000002 identity: users, sessions, OTP, staff roles, client profiles, addresses, consents.

-- One users table for every person who can sign in. Clients and providers sign in with
-- phone + SMS OTP (low-end phones, no passwords to forget). Staff sign in with email +
-- password + TOTP.
CREATE TABLE users (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  kind            user_kind NOT NULL,
  status          account_status NOT NULL DEFAULT 'pending_verification',
  full_name       TEXT NOT NULL,
  phone_e164      TEXT UNIQUE CHECK (phone_e164 ~ '^\+[1-9][0-9]{7,14}$'),
  phone_verified_at TIMESTAMPTZ,
  email           CITEXT UNIQUE,
  email_verified_at TIMESTAMPTZ,
  password_hash   TEXT,                 -- argon2id, staff only
  totp_secret_enc BYTEA,                -- staff only
  preferred_language TEXT NOT NULL DEFAULT 'en' CHECK (preferred_language IN ('en', 'sw')),
  last_login_at   TIMESTAMPTZ,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT users_contact_required CHECK (phone_e164 IS NOT NULL OR email IS NOT NULL),
  CONSTRAINT staff_have_password CHECK (kind <> 'staff' OR (email IS NOT NULL AND password_hash IS NOT NULL))
);
CREATE INDEX users_kind_status_idx ON users (kind, status);
CREATE TRIGGER users_updated_at BEFORE UPDATE ON users FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE staff_roles (
  user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  role       staff_role NOT NULL,
  granted_by UUID REFERENCES users(id),
  granted_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (user_id, role)
);

-- Opaque session tokens. The HTML portals put the token in an HttpOnly cookie; the JSON
-- API accepts it as a Bearer token. Only the SHA-256 of the token is stored.
CREATE TABLE sessions (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  token_hash  BYTEA NOT NULL UNIQUE,
  user_agent  TEXT,
  ip_address  INET,
  mfa_passed  BOOLEAN NOT NULL DEFAULT false,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  expires_at  TIMESTAMPTZ NOT NULL,
  revoked_at  TIMESTAMPTZ
);
CREATE INDEX sessions_user_idx ON sessions (user_id) WHERE revoked_at IS NULL;

CREATE TYPE otp_purpose AS ENUM ('login', 'register', 'verify_phone', 'job_checkin');
CREATE TABLE otp_codes (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  phone_e164  TEXT NOT NULL,
  purpose     otp_purpose NOT NULL,
  code_hash   BYTEA NOT NULL,
  attempts    SMALLINT NOT NULL DEFAULT 0,
  expires_at  TIMESTAMPTZ NOT NULL,
  consumed_at TIMESTAMPTZ,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX otp_codes_lookup_idx ON otp_codes (phone_e164, purpose, created_at DESC);

-- Client (household) profile. Identity verification is mandatory before a live-in
-- placement (report §5.2 step 1).
CREATE TYPE id_verification_status AS ENUM ('none', 'pending', 'verified', 'rejected');
CREATE TABLE clients (
  user_id               UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  national_id_enc       BYTEA,
  id_verification       id_verification_status NOT NULL DEFAULT 'none',
  id_verified_by        UUID REFERENCES users(id),
  id_verified_at        TIMESTAMPTZ,
  household_notes_enc   BYTEA,        -- household composition, pets, special needs
  referral_code         TEXT UNIQUE,
  referred_by_client_id UUID REFERENCES clients(user_id),
  created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at            TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TRIGGER clients_updated_at BEFORE UPDATE ON clients FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Catchments / service areas. Pilot runs in one; the public site lists the active ones.
CREATE TABLE service_areas (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  slug        TEXT NOT NULL UNIQUE,     -- e.g. 'kilimani' for SEO pages
  name        TEXT NOT NULL,
  county      TEXT NOT NULL DEFAULT 'Nairobi',
  is_active   BOOLEAN NOT NULL DEFAULT false,
  centre_lat  NUMERIC(9,6),
  centre_lng  NUMERIC(9,6),
  radius_km   NUMERIC(5,2),
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TRIGGER service_areas_updated_at BEFORE UPDATE ON service_areas FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Client service addresses. Gate/access instructions are sensitive and encrypted.
CREATE TABLE addresses (
  id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  client_id        UUID NOT NULL REFERENCES clients(user_id) ON DELETE CASCADE,
  label            TEXT NOT NULL DEFAULT 'Home',
  service_area_id  UUID REFERENCES service_areas(id),
  estate           TEXT,
  street           TEXT,
  house_number     TEXT,
  landmark         TEXT,
  lat              NUMERIC(9,6),
  lng              NUMERIC(9,6),
  access_details_enc BYTEA,           -- gate codes, guard names, parking
  is_default       BOOLEAN NOT NULL DEFAULT false,
  archived_at      TIMESTAMPTZ,
  created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX addresses_client_idx ON addresses (client_id) WHERE archived_at IS NULL;
CREATE UNIQUE INDEX addresses_one_default_idx ON addresses (client_id) WHERE is_default AND archived_at IS NULL;
CREATE TRIGGER addresses_updated_at BEFORE UPDATE ON addresses FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Data Protection Act consent records: what notice version was accepted, when, for what.
CREATE TABLE consents (
  id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id        UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  purpose        TEXT NOT NULL,       -- 'privacy_notice', 'vetting_checks', 'health_data', 'marketing'
  notice_version TEXT NOT NULL,
  granted        BOOLEAN NOT NULL,
  ip_address     INET,
  created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX consents_user_purpose_idx ON consents (user_id, purpose, created_at DESC);
