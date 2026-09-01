-- name: runtime_configuration__rollback_environment :one
WITH source AS (
    SELECT source_version.id, source_version.non_secret_values, source_version.secret_descriptors,
           source_version.role_image_artifact_id, source_version.selected_tools, source_version.core_digest,
           source_version.resource_policy, source_version.volume_policy, source_version.network_policy,
           source_version.kubernetes_access_profile, source_version.resources_digest, source_version.volumes_digest,
           source_version.network_digest, source_version.rbac_digest, source_version.digest
    FROM control_plane.runtime_environment_versions source_version
    WHERE source_version.environment_set_id = @environment_id::uuid AND source_version.ref = @source_ref
), inserted_version AS (
    INSERT INTO control_plane.runtime_environment_versions
        (ref, organization_id, environment_set_id, version_number, parent_version_id,
         non_secret_values, secret_descriptors, role_image_artifact_id, selected_tools,
         core_digest, resource_policy, volume_policy, network_policy, kubernetes_access_profile,
         resources_digest, volumes_digest, network_digest, rbac_digest, digest, created_by)
    SELECT @version_ref, @organization_id::uuid, @environment_id::uuid, @version_number,
           source.id, source.non_secret_values, source.secret_descriptors,
           source.role_image_artifact_id, source.selected_tools, source.core_digest,
           source.resource_policy, source.volume_policy, source.network_policy, source.kubernetes_access_profile,
           source.resources_digest, source.volumes_digest, source.network_digest, source.rbac_digest,
           source.digest, @created_by::uuid
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
