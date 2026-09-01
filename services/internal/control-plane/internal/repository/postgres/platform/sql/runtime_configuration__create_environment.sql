-- name: runtime_configuration__create_environment :one
WITH inserted_environment AS (
    INSERT INTO control_plane.runtime_environment_sets
        (ref, organization_id, project_id, name, description, created_by)
    VALUES (@environment_ref, @organization_id::uuid, @project_id::uuid, @name, @description, @created_by::uuid)
    RETURNING id, ref
), inserted_version AS (
    INSERT INTO control_plane.runtime_environment_versions
        (ref, organization_id, environment_set_id, version_number, non_secret_values, secret_descriptors,
         role_image_artifact_id, selected_tools, core_digest, resource_policy, volume_policy,
         network_policy, kubernetes_access_profile, resources_digest, volumes_digest, network_digest,
         rbac_digest, digest, created_by)
    SELECT @version_ref, @organization_id::uuid, inserted_environment.id, 1,
           @non_secret_values, @secret_descriptors, @image_artifact_id::uuid, @selected_tools,
           @core_digest, @resource_policy, @volume_policy, @network_policy, @kubernetes_access_profile,
           @resources_digest, @volumes_digest, @network_digest, @rbac_digest, @digest, @created_by::uuid
    FROM inserted_environment
    RETURNING id, environment_set_id
)
SELECT inserted_environment.id,
       inserted_version.id,
       inserted_environment.ref
FROM inserted_environment
JOIN inserted_version ON inserted_version.environment_set_id = inserted_environment.id;
