-- +goose Up
INSERT INTO app_settings (key, value, description) VALUES
  ('vat.rate_pct', '16', 'VAT rate applied on top of rate-card prices'),
  ('session.client_ttl_hours', '720', 'Client and provider session lifetime'),
  ('session.staff_ttl_hours', '12', 'Staff session lifetime'),
  ('otp.ttl_minutes', '10', 'One-time code lifetime'),
  ('otp.max_attempts', '5', 'Wrong-code attempts before a code is burned')
ON CONFLICT (key) DO NOTHING;

-- +goose Down
DELETE FROM app_settings WHERE key IN ('vat.rate_pct', 'session.client_ttl_hours', 'session.staff_ttl_hours', 'otp.ttl_minutes', 'otp.max_attempts');
