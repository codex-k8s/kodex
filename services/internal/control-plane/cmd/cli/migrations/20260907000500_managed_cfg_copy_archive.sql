-- +goose Up
SET ROLE control_plane_owner;
ALTER TABLE control_plane.managed_configuration_sets
 ADD COLUMN archived boolean NOT NULL DEFAULT false,
 ADD COLUMN copy_provenance jsonb,
 ADD CONSTRAINT managed_configuration_archive_kind CHECK (NOT archived OR kind IN ('ROLE_IMAGE','INTEGRATION_DEFINITION'));
UPDATE control_plane.managed_configuration_sets configuration
SET archived=true
FROM control_plane.managed_role_image_recipes mapping
JOIN control_plane.role_image_recipes recipe ON recipe.id=mapping.recipe_id AND recipe.organization_id=mapping.organization_id
WHERE configuration.id=mapping.configuration_set_id AND configuration.organization_id=mapping.organization_id AND recipe.state='ARCHIVED';
-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_configuration_copy_provenance() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.copy_provenance IS DISTINCT FROM OLD.copy_provenance THEN
  RAISE EXCEPTION 'configuration copy provenance is immutable';
 END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER configuration_copy_provenance_guard BEFORE UPDATE ON control_plane.managed_configuration_sets
 FOR EACH ROW EXECUTE FUNCTION control_plane.guard_configuration_copy_provenance();
RESET ROLE;
-- +goose Down
SET ROLE control_plane_owner;
DROP TRIGGER configuration_copy_provenance_guard ON control_plane.managed_configuration_sets;
DROP FUNCTION control_plane.guard_configuration_copy_provenance();
ALTER TABLE control_plane.managed_configuration_sets DROP CONSTRAINT managed_configuration_archive_kind, DROP COLUMN copy_provenance, DROP COLUMN archived;
RESET ROLE;
