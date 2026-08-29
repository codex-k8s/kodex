-- +goose Up
SET ROLE control_plane_owner;

ALTER TABLE control_plane.runtime_environment_versions
    ADD COLUMN role_image_artifact_id uuid REFERENCES control_plane.image_artifacts(id),
    ADD COLUMN selected_tools jsonb NOT NULL DEFAULT '[]'::jsonb
        CHECK (jsonb_typeof(selected_tools) = 'array' AND jsonb_array_length(selected_tools) <= 128);

CREATE INDEX runtime_environment_versions_image_artifact
    ON control_plane.runtime_environment_versions (role_image_artifact_id)
    WHERE role_image_artifact_id IS NOT NULL;

RESET ROLE;

-- +goose Down
SET ROLE control_plane_owner;

DROP INDEX IF EXISTS control_plane.runtime_environment_versions_image_artifact;
ALTER TABLE control_plane.runtime_environment_versions
    DROP COLUMN selected_tools,
    DROP COLUMN role_image_artifact_id;

RESET ROLE;
