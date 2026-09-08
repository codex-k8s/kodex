-- Отдельная disposable fixture: новая immutable revision без image pin.
WITH original AS (
    SELECT version.id, version.organization_id, version.environment_set_id, version.version_number,
           version.non_secret_values, version.secret_descriptors, version.selected_tools,
           version.core_digest, version.resource_policy, version.volume_policy,
           version.network_policy, version.kubernetes_access_profile, version.resources_digest,
           version.volumes_digest, version.network_digest, version.rbac_digest, version.digest, version.created_by
    FROM control_plane.agents agent
    JOIN control_plane.agent_runtime_environment_bindings binding ON binding.agent_id = agent.id
    JOIN control_plane.runtime_environment_versions version ON version.id = binding.environment_version_id
    WHERE agent.organization_id = $1::uuid AND agent.ref = $2
), inserted AS (
    INSERT INTO control_plane.runtime_environment_versions
        (ref, organization_id, environment_set_id, version_number, parent_version_id,
         non_secret_values, secret_descriptors, role_image_artifact_id, selected_tools,
         core_digest, resource_policy, volume_policy, network_policy, kubernetes_access_profile,
         resources_digest, volumes_digest, network_digest, rbac_digest, digest, created_by)
    SELECT $3, organization_id, environment_set_id, version_number + 1, id,
           non_secret_values, secret_descriptors, NULL, selected_tools, core_digest,
           resource_policy, volume_policy, network_policy, kubernetes_access_profile,
           resources_digest, volumes_digest, network_digest, rbac_digest, digest, created_by
    FROM original
    RETURNING id
)
UPDATE control_plane.agent_runtime_environment_bindings binding
SET environment_version_id = inserted.id
FROM inserted, control_plane.agents agent
WHERE agent.id = binding.agent_id AND agent.organization_id = $1::uuid AND agent.ref = $2;
