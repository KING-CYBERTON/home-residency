-- +goose Up
-- Renaming an area or category slug moves its public pages. The old addresses keep working with a
-- permanent redirect: a trigger records every rename, and chains collapse so each old slug points
-- straight at the current one.
CREATE TABLE slug_redirects (
  kind       TEXT NOT NULL CHECK (kind IN ('area', 'category')),
  old_slug   TEXT NOT NULL,
  new_slug   TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (kind, old_slug)
);

-- +goose StatementBegin
CREATE FUNCTION record_slug_rename() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.slug IS DISTINCT FROM OLD.slug THEN
    -- earlier names now lead to the new one, and a slug in use again is no longer a redirect
    UPDATE slug_redirects SET new_slug = NEW.slug WHERE kind = TG_ARGV[0] AND new_slug = OLD.slug;
    DELETE FROM slug_redirects WHERE kind = TG_ARGV[0] AND old_slug = NEW.slug;
    INSERT INTO slug_redirects (kind, old_slug, new_slug) VALUES (TG_ARGV[0], OLD.slug, NEW.slug)
      ON CONFLICT (kind, old_slug) DO UPDATE SET new_slug = EXCLUDED.new_slug, created_at = now();
  END IF;
  RETURN NEW;
END $$;
-- +goose StatementEnd

CREATE TRIGGER service_areas_slug_rename AFTER UPDATE OF slug ON service_areas
  FOR EACH ROW EXECUTE FUNCTION record_slug_rename('area');
CREATE TRIGGER service_categories_slug_rename AFTER UPDATE OF slug ON service_categories
  FOR EACH ROW EXECUTE FUNCTION record_slug_rename('category');

-- QA round 2: the Kasarani area was created as "kasa" with no radius. Fix it where it exists;
-- the trigger above keeps /areas/kasa and /services/*/kasa working.
UPDATE service_areas SET slug = 'kasarani', name = CASE WHEN lower(name) = 'kasa' THEN 'Kasarani' ELSE name END
  WHERE slug = 'kasa' AND NOT EXISTS (SELECT 1 FROM service_areas WHERE slug = 'kasarani');
UPDATE service_areas SET centre_lat = COALESCE(centre_lat, -1.221900), centre_lng = COALESCE(centre_lng, 36.897500), radius_km = 5
  WHERE slug = 'kasarani' AND radius_km IS NULL;

-- +goose Down
DROP TRIGGER IF EXISTS service_categories_slug_rename ON service_categories;
DROP TRIGGER IF EXISTS service_areas_slug_rename ON service_areas;
DROP FUNCTION IF EXISTS record_slug_rename();
DROP TABLE IF EXISTS slug_redirects;
