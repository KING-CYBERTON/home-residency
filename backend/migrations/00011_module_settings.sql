-- +goose Up
INSERT INTO app_settings (key, value, description) VALUES
  ('contract.current_version', '"2026-10"', 'Provider contract version offered for signing'),
  ('contract.code_of_conduct_version', '"2026-10"', 'Code of conduct version offered for signing')
ON CONFLICT (key) DO NOTHING;

-- +goose Down
DELETE FROM app_settings WHERE key IN ('contract.current_version', 'contract.code_of_conduct_version');
