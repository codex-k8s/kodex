-- name: runtime_configuration__rollback_environment :one
WITH source AS (
    SELECT source_version.id, source_version.non_secret_values, source_version.secret_descriptors, source_version.digest
    FROM control_plane.runtime_environment_versions source_version
    WHERE source_version.environment_set_id = @environment_id::uuid AND source_version.ref = @source_ref
), inserted_version AS (
    INSERT INTO control_plane.runtime_environment_versions
        (ref, organization_id, environment_set_id, version_number, parent_version_id,
         non_secret_values, secret_descriptors, digest, created_by)
    SELECT @version_ref, @organization_id::uuid, @environment_id::uuid, @version_number,
           source.id, source.non_secret_values, source.secret_descriptors, source.digest, @created_by::uuid
    FROM source
    RETURNING id
), updated_environment AS (
    UPDATE control_plane.runtime_environment_sets environment
    SET current_version_id = inserted_version.id,
        version = environment.version + 1,
        updated_at = clock_timestamp()
    FROM inserted_version
    WHERE environment.id = @environment_id::uuid
    RETURNING environment.ref
)
SELECT ref FROM updated_environment;
