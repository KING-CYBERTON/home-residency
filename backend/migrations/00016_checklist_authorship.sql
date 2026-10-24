-- +goose Up
-- Who drafted each checklist version and when it was made current, so the console can say
-- "Drafted by Faith, waiting for an approver" and "Made current by Peter on 3 Nov".
ALTER TABLE checklist_templates
  ADD COLUMN created_by  UUID REFERENCES users(id),
  ADD COLUMN approved_at TIMESTAMPTZ;

UPDATE checklist_templates SET approved_at = created_at WHERE is_current AND approved_at IS NULL;

-- +goose Down
ALTER TABLE checklist_templates DROP COLUMN approved_at, DROP COLUMN created_by;
