-- name: workers_reconcilewarmruntime_publish_system_provider_policy :one
WITH inserted_policy AS (
    INSERT INTO control_plane.provider_account_policy_versions
        (ref, organization_id, agent_id, version_number, mode, account_candidates, digest, created_by)
    VALUES (@policy_ref, @organization_id::uuid, @agent_id::uuid, @version_number,
            @policy_mode, @account_candidates::jsonb, @policy_digest, @created_by::uuid)
    RETURNING id
), inserted_config AS (
    INSERT INTO control_plane.agent_runtime_config_versions
        (ref, organization_id, agent_id, version_number, provider_account_policy_id,
         runtime_profile_key, provider, model, digest, created_by)
    SELECT @config_ref, @organization_id::uuid, @agent_id::uuid, @version_number, inserted_policy.id,
           @runtime_profile_ref, @provider, @model, @config_digest, @created_by::uuid
    FROM inserted_policy
    RETURNING id, ref
), updated_agent AS (
    UPDATE control_plane.agents agent
    SET current_runtime_config_id = inserted_config.id,
        runtime_key = @runtime_profile_ref,
        version = agent.version + 1,
        updated_at = clock_timestamp()
    FROM inserted_config
    WHERE agent.id = @agent_id::uuid
      AND agent.current_runtime_config_id = @current_config_id::uuid
    RETURNING agent.id
), recovering_runtime AS (
    UPDATE control_plane.assistant_runtime runtime
    SET runtime_state = 'RECOVERING',
        desired_runtime_revision = @next_runtime_revision,
        warm_instance_ref = NULL,
        last_heartbeat_at = NULL,
        version = runtime.version + 1,
        updated_at = clock_timestamp()
    FROM updated_agent
    WHERE runtime.organization_id = @organization_id::uuid
      AND runtime.agent_id = updated_agent.id
      AND runtime.stable_key = 'system-assistant'
    RETURNING runtime.agent_id, runtime.stable_key
), inserted_audit AS (
    INSERT INTO control_plane.audit_events(
        ref, organization_id, actor_id, assistant_agent_id, action,
        resource_kind, resource_ref, outcome, safe_summary, correlation_ref
    )
    SELECT @audit_ref, @organization_id::uuid, @created_by::uuid,
           recovering_runtime.agent_id,
           'system_assistant.provider_policy_reconciled',
           'AGENT_RUNTIME_CONFIGURATION', recovering_runtime.stable_key,
           'SUCCEEDED', 'i18n:SYSTEM_ASSISTANT_PROVIDER_POLICY_UPDATED',
           'runtime-controller'
    FROM recovering_runtime
    RETURNING ref
)
SELECT inserted_config.ref
FROM inserted_config
JOIN updated_agent ON true
JOIN recovering_runtime ON true
JOIN inserted_audit ON true;
