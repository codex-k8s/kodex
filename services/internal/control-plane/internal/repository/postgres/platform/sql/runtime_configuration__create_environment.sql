-- name: runtime_configuration__create_environment :one
WITH inserted_environment AS (
    INSERT INTO control_plane.runtime_environment_sets
        (ref, organization_id, project_id, name, description, created_by)
    VALUES (@environment_ref, @organization_id::uuid, @project_id::uuid, @name, @description, @created_by::uuid)
    RETURNING id, ref
), inserted_version AS (
    INSERT INTO control_plane.runtime_environment_versions
        (ref, organization_id, environment_set_id, version_number, non_secret_values, secret_descriptors, digest, created_by)
    SELECT @version_ref, @organization_id::uuid, inserted_environment.id, 1,
           @non_secret_values, @secret_descriptors, @digest, @created_by::uuid
    FROM inserted_environment
    RETURNING id, environment_set_id
), updated_environment AS (
    UPDATE control_plane.runtime_environment_sets environment
    SET current_version_id = inserted_version.id
    FROM inserted_version
    WHERE environment.id = inserted_version.environment_set_id
    RETURNING environment.ref
)
SELECT ref FROM updated_environment;
