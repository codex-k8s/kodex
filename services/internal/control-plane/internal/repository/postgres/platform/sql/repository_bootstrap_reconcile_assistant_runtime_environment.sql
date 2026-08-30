-- name: repository_bootstrap_reconcile_assistant_runtime_environment :one
WITH current_environment AS (
    SELECT environment.id,
           current_version.id AS current_version_id,
           current_version.organization_id,
           current_version.version_number,
           current_version.non_secret_values,
           current_version.secret_descriptors,
           current_version.selected_tools,
           current_version.resource_policy,
           current_version.volume_policy,
           current_version.network_policy,
           current_version.kubernetes_access_profile,
           current_version.resources_digest,
           current_version.volumes_digest,
           current_version.network_digest,
           current_version.rbac_digest,
           current_version.created_by
    FROM control_plane.runtime_environment_sets environment
    JOIN control_plane.runtime_environment_versions current_version
      ON current_version.id = environment.current_version_id
    WHERE environment.id = @environment_id::uuid
      AND environment.organization_id = @organization_id::uuid
      AND environment.project_id IS NULL
      AND environment.state = 'ACTIVE'
      AND environment.current_version_id = @current_version_id::uuid
      AND current_version.version_number = @current_version
      AND current_version.role_image_artifact_id IS NULL
    FOR UPDATE OF environment
), inserted_version AS (
    INSERT INTO control_plane.runtime_environment_versions
        (ref, organization_id, environment_set_id, version_number, parent_version_id,
         non_secret_values, secret_descriptors, role_image_artifact_id, selected_tools,
         core_digest, resource_policy, volume_policy, network_policy, kubernetes_access_profile,
         resources_digest, volumes_digest, network_digest, rbac_digest, digest, created_by)
    SELECT @version_ref, current_environment.organization_id, current_environment.id,
           current_environment.version_number + 1, current_environment.current_version_id,
           current_environment.non_secret_values, current_environment.secret_descriptors, NULL,
           current_environment.selected_tools, @expected_core_digest,
           current_environment.resource_policy, current_environment.volume_policy,
           current_environment.network_policy, current_environment.kubernetes_access_profile,
           current_environment.resources_digest, current_environment.volumes_digest,
           current_environment.network_digest, current_environment.rbac_digest,
           @expected_digest, current_environment.created_by
    FROM current_environment
    RETURNING id, environment_set_id
), activated_environment AS (
    UPDATE control_plane.runtime_environment_sets environment
    SET current_version_id = inserted_version.id,
        version = environment.version + 1,
        updated_at = clock_timestamp()
    FROM inserted_version
    WHERE environment.id = inserted_version.environment_set_id
      AND environment.current_version_id = @current_version_id::uuid
    RETURNING inserted_version.id
), recovering_runtime AS (
    UPDATE control_plane.assistant_runtime runtime
    SET runtime_state = 'RECOVERING',
        desired_runtime_revision = @next_runtime_revision,
        warm_instance_ref = NULL,
        last_heartbeat_at = NULL,
        version = runtime.version + 1,
        updated_at = clock_timestamp()
    FROM activated_environment
    WHERE runtime.organization_id = @organization_id::uuid
      AND runtime.agent_id = @agent_id::uuid
      AND runtime.stable_key = 'system-assistant'
    RETURNING activated_environment.id
)
SELECT id::text FROM recovering_runtime;
