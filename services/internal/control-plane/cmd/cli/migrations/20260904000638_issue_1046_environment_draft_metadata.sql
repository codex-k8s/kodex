-- +goose Up
SET ROLE control_plane_owner;
ALTER TABLE control_plane.runtime_environment_drafts
    ADD COLUMN base_version_id uuid REFERENCES control_plane.runtime_environment_versions(id),
    ADD COLUMN saved_at timestamptz;

-- Историческую базу восстанавливаем только при доказанном совпадении версии set.
UPDATE control_plane.runtime_environment_drafts draft
SET base_version_id = environment.current_version_id
FROM control_plane.runtime_environment_sets environment
WHERE environment.organization_id = draft.organization_id
  AND environment.project_id = draft.project_id
  AND environment.ref = draft.environment_ref
  AND environment.version = draft.expected_environment_version;
UPDATE control_plane.runtime_environment_drafts draft
SET saved_at = COALESCE((
    SELECT max(audit.occurred_at) FROM control_plane.audit_events audit
    WHERE audit.organization_id = draft.organization_id AND audit.resource_ref = draft.ref
      AND audit.resource_kind = 'RUNTIME_ENVIRONMENT_DRAFT' AND audit.outcome = 'SUCCEEDED'
      AND audit.action IN ('controlplane.create_runtime_environment_draft', 'controlplane.save_runtime_environment_draft')
), draft.created_at);
ALTER TABLE control_plane.runtime_environment_drafts ALTER COLUMN saved_at SET NOT NULL;
ALTER TABLE control_plane.runtime_environment_drafts ALTER COLUMN saved_at SET DEFAULT clock_timestamp();

-- +goose StatementBegin
CREATE FUNCTION control_plane.protect_environment_draft_base() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF (NEW.organization_id, NEW.project_id, NEW.environment_ref, NEW.expected_environment_version,
        NEW.base_version_id, NEW.created_by, NEW.created_at)
       IS DISTINCT FROM
       (OLD.organization_id, OLD.project_id, OLD.environment_ref, OLD.expected_environment_version,
        OLD.base_version_id, OLD.created_by, OLD.created_at)
       OR NEW.saved_at < OLD.saved_at THEN
        RAISE EXCEPTION 'environment draft provenance is immutable';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER protect_environment_draft_base BEFORE UPDATE ON control_plane.runtime_environment_drafts
FOR EACH ROW EXECUTE FUNCTION control_plane.protect_environment_draft_base();
RESET ROLE;
