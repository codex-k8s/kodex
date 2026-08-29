-- name: runtime_configuration__publish_environment :one
WITH inserted_version AS (
    INSERT INTO control_plane.runtime_environment_versions
        (ref, organization_id, environment_set_id, version_number, parent_version_id,
         non_secret_values, secret_descriptors, role_image_artifact_id, selected_tools, digest, created_by)
    VALUES (@version_ref, @organization_id::uuid, @environment_id::uuid, @version_number,
            @parent_version_id::uuid, @non_secret_values, @secret_descriptors,
            @image_artifact_id::uuid, @selected_tools, @digest, @created_by::uuid)
    RETURNING id
), updated_environment AS (
    UPDATE control_plane.runtime_environment_sets environment
    SET current_version_id = inserted_version.id,
        name = @name,
        description = @description,
        version = environment.version + 1,
        updated_at = clock_timestamp()
    FROM inserted_version
    WHERE environment.id = @environment_id::uuid
    RETURNING environment.ref
)
SELECT ref FROM updated_environment;
