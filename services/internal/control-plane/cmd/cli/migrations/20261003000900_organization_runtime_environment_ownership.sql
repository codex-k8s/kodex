-- +goose Up
SET ROLE control_plane_owner;

-- +goose StatementBegin
DO $$
BEGIN
    UPDATE control_plane.permission_registry
    SET resource_kinds = ARRAY['ORGANIZATION', 'PROJECT', 'RUNTIME_ENVIRONMENT']
    WHERE permission_key = 'environment.privileged.manage'
      AND resource_kinds = ARRAY['PROJECT', 'RUNTIME_ENVIRONMENT'];
    IF NOT FOUND THEN
        RAISE EXCEPTION 'environment privileged permission definition precondition mismatch' USING ERRCODE = '23514';
    END IF;
END $$;
-- +goose StatementEnd

ALTER TABLE control_plane.runtime_environment_sets ADD COLUMN scope_kind text;
UPDATE control_plane.runtime_environment_sets environment
SET scope_kind = CASE WHEN project_id IS NOT NULL THEN 'PROJECT' ELSE 'ORGANIZATION' END;
ALTER TABLE control_plane.runtime_environment_sets
    ALTER COLUMN scope_kind SET NOT NULL,
    ADD CONSTRAINT runtime_environment_sets_scope_check CHECK (
        (scope_kind = 'PROJECT' AND project_id IS NOT NULL) OR
        (scope_kind = 'ORGANIZATION' AND project_id IS NULL)
    );

ALTER TABLE control_plane.runtime_environment_drafts
    ALTER COLUMN project_id DROP NOT NULL,
    ADD COLUMN scope_kind text;
UPDATE control_plane.runtime_environment_drafts SET scope_kind = 'PROJECT';
ALTER TABLE control_plane.runtime_environment_drafts
    ALTER COLUMN scope_kind SET NOT NULL,
    ADD CONSTRAINT runtime_environment_drafts_scope_check CHECK (
        (scope_kind = 'PROJECT' AND project_id IS NOT NULL) OR
        (scope_kind = 'ORGANIZATION' AND project_id IS NULL)
    );

-- Владелец и область никогда не меняются при публикации, повторе или восстановлении.
-- +goose StatementBegin
CREATE FUNCTION control_plane.enforce_runtime_environment_owner() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'UPDATE' AND (
        NEW.organization_id IS DISTINCT FROM OLD.organization_id OR
        NEW.project_id IS DISTINCT FROM OLD.project_id OR
        NEW.scope_kind IS DISTINCT FROM OLD.scope_kind OR
        NEW.ref IS DISTINCT FROM OLD.ref OR
        NEW.created_by IS DISTINCT FROM OLD.created_by
    ) THEN
        RAISE EXCEPTION 'runtime environment owner is immutable' USING ERRCODE = '23514';
    END IF;
    IF NEW.scope_kind = 'PROJECT' AND NOT EXISTS (
        SELECT 1 FROM control_plane.projects project
        WHERE project.id = NEW.project_id AND project.organization_id = NEW.organization_id
    ) THEN
        RAISE EXCEPTION 'runtime environment project owner mismatch' USING ERRCODE = '23514';
    END IF;
    IF TG_TABLE_NAME = 'runtime_environment_drafts' THEN
        IF TG_OP = 'UPDATE' AND (
            NEW.environment_ref IS DISTINCT FROM OLD.environment_ref OR
            NEW.expected_environment_version IS DISTINCT FROM OLD.expected_environment_version OR
            NEW.base_version_id IS DISTINCT FROM OLD.base_version_id
        ) THEN
            RAISE EXCEPTION 'runtime environment draft base is immutable' USING ERRCODE = '23514';
        END IF;
        IF NEW.environment_ref <> '' AND NOT EXISTS (
            SELECT 1 FROM control_plane.runtime_environment_sets environment
            JOIN control_plane.runtime_environment_versions revision
              ON revision.id = NEW.base_version_id AND revision.environment_set_id = environment.id
            WHERE environment.ref = NEW.environment_ref
              AND environment.organization_id = NEW.organization_id
              AND environment.project_id IS NOT DISTINCT FROM NEW.project_id
              AND environment.scope_kind = NEW.scope_kind
        ) THEN
            RAISE EXCEPTION 'runtime environment draft base owner mismatch' USING ERRCODE = '23514';
        END IF;
    END IF;
    RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER runtime_environment_sets_owner BEFORE INSERT OR UPDATE
    ON control_plane.runtime_environment_sets FOR EACH ROW EXECUTE FUNCTION control_plane.enforce_runtime_environment_owner();
CREATE TRIGGER runtime_environment_drafts_owner BEFORE INSERT OR UPDATE
    ON control_plane.runtime_environment_drafts FOR EACH ROW EXECUTE FUNCTION control_plane.enforce_runtime_environment_owner();

-- +goose StatementBegin
CREATE FUNCTION control_plane.enforce_runtime_environment_image_owner() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    environment control_plane.runtime_environment_sets%ROWTYPE;
BEGIN
    SELECT * INTO environment FROM control_plane.runtime_environment_sets
    WHERE id = NEW.environment_set_id AND organization_id = NEW.organization_id;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'runtime environment revision owner mismatch' USING ERRCODE = '23514';
    END IF;
    IF NEW.role_image_artifact_id IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM control_plane.image_artifacts artifact
        JOIN control_plane.role_image_recipes recipe ON recipe.id = artifact.recipe_id
        WHERE artifact.id = NEW.role_image_artifact_id
          AND artifact.organization_id = environment.organization_id
          AND artifact.project_id IS NOT DISTINCT FROM environment.project_id
          AND artifact.scope_kind = environment.scope_kind
          AND recipe.organization_id = artifact.organization_id
          AND recipe.project_id IS NOT DISTINCT FROM artifact.project_id
          AND recipe.scope_kind = artifact.scope_kind
    ) THEN
        RAISE EXCEPTION 'runtime environment image owner mismatch' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER runtime_environment_versions_image_owner BEFORE INSERT
    ON control_plane.runtime_environment_versions FOR EACH ROW EXECUTE FUNCTION control_plane.enforce_runtime_environment_image_owner();

-- +goose StatementBegin
CREATE FUNCTION control_plane.enforce_runtime_environment_binding_owner() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM control_plane.agents agent
        JOIN control_plane.runtime_environment_sets environment
          ON environment.id = NEW.environment_set_id AND environment.organization_id = NEW.organization_id
        WHERE agent.id = NEW.agent_id AND agent.organization_id = NEW.organization_id
          AND agent.project_id IS NOT DISTINCT FROM environment.project_id
          AND ((environment.scope_kind = 'PROJECT' AND agent.project_id IS NOT NULL) OR
               (environment.scope_kind = 'ORGANIZATION' AND agent.project_id IS NULL AND agent.system_key = 'system-assistant'))
          AND (NEW.environment_version_id IS NULL OR EXISTS (
              SELECT 1 FROM control_plane.runtime_environment_versions revision
              WHERE revision.id = NEW.environment_version_id
                AND revision.environment_set_id = environment.id
                AND revision.organization_id = environment.organization_id
          ))
    ) THEN
        RAISE EXCEPTION 'runtime environment binding owner mismatch' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER agent_runtime_environment_bindings_owner BEFORE INSERT OR UPDATE
    ON control_plane.agent_runtime_environment_bindings FOR EACH ROW EXECUTE FUNCTION control_plane.enforce_runtime_environment_binding_owner();

RESET ROLE;

-- +goose Down
SELECT 1 / 0;
