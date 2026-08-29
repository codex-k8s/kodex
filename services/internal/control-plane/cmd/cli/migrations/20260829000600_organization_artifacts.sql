-- +goose Up
SET ROLE control_plane_owner;

ALTER TABLE control_plane.artifacts
    ALTER COLUMN project_id DROP NOT NULL;
ALTER TABLE control_plane.artifact_download_grants
    ALTER COLUMN project_id DROP NOT NULL;
ALTER TABLE control_plane.attachment_sets
    ALTER COLUMN project_id DROP NOT NULL;
ALTER TABLE control_plane.attachment_bindings
    ALTER COLUMN project_id DROP NOT NULL;

ALTER TABLE control_plane.access_bindings
    DROP CONSTRAINT access_bindings_check1,
    ADD CONSTRAINT access_bindings_scope_shape CHECK (
        (scope_kind = 'ORGANIZATION' AND project_id IS NULL AND resource_kind IS NULL AND resource_id IS NULL) OR
        (scope_kind = 'PROJECT' AND project_id IS NOT NULL AND resource_kind IS NULL AND resource_id IS NULL) OR
        (scope_kind = 'RESOURCE_KIND' AND resource_kind IS NOT NULL AND resource_id IS NULL) OR
        (scope_kind = 'RESOURCE_INSTANCE' AND resource_kind IS NOT NULL AND resource_id IS NOT NULL AND
         (project_id IS NOT NULL OR resource_kind IN ('INTEGRATION', 'ARTIFACT')))
    );

CREATE UNIQUE INDEX artifacts_organization_owner_file_revision
    ON control_plane.artifacts (organization_id, created_by, file_name, revision)
    WHERE project_id IS NULL;

UPDATE control_plane.agents
SET capabilities = array_append(capabilities, 'platform.artifact.manage'),
    version = version + 1,
    updated_at = clock_timestamp()
WHERE system_key = 'system-assistant'
  AND NOT ('platform.artifact.manage' = ANY(capabilities));

RESET ROLE;

-- +goose Down
SET ROLE control_plane_owner;

DROP INDEX control_plane.artifacts_organization_owner_file_revision;
ALTER TABLE control_plane.access_bindings
    DROP CONSTRAINT access_bindings_scope_shape,
    ADD CONSTRAINT access_bindings_check1 CHECK (
        (scope_kind = 'ORGANIZATION' AND project_id IS NULL AND resource_kind IS NULL AND resource_id IS NULL) OR
        (scope_kind = 'PROJECT' AND project_id IS NOT NULL AND resource_kind IS NULL AND resource_id IS NULL) OR
        (scope_kind = 'RESOURCE_KIND' AND resource_kind IS NOT NULL AND resource_id IS NULL) OR
        (scope_kind = 'RESOURCE_INSTANCE' AND project_id IS NOT NULL AND resource_kind IS NOT NULL AND resource_id IS NOT NULL)
    );
UPDATE control_plane.agents
SET capabilities = array_remove(capabilities, 'platform.artifact.manage'),
    version = version + 1,
    updated_at = clock_timestamp()
WHERE system_key = 'system-assistant';
ALTER TABLE control_plane.attachment_bindings
    ALTER COLUMN project_id SET NOT NULL;
ALTER TABLE control_plane.attachment_sets
    ALTER COLUMN project_id SET NOT NULL;
ALTER TABLE control_plane.artifact_download_grants
    ALTER COLUMN project_id SET NOT NULL;
ALTER TABLE control_plane.artifacts
    ALTER COLUMN project_id SET NOT NULL;

RESET ROLE;
