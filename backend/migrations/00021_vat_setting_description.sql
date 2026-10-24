-- +goose Up
-- Prices have included VAT since 00017; the setting's description still said it was added on top.
UPDATE app_settings SET description = 'VAT rate already included in rate-card prices' WHERE key = 'vat.rate_pct';

-- +goose Down
UPDATE app_settings SET description = 'VAT rate applied on top of rate-card prices' WHERE key = 'vat.rate_pct';
