-- name: runtime_configuration__publish :one
WITH inserted_policy AS (
    INSERT INTO control_plane.provider_account_policy_versions
        (ref, organization_id, agent_id, version_number, mode, account_candidates, digest, created_by)
    VALUES (@policy_ref, @organization_id::uuid, @agent_id::uuid, @version_number,
            @policy_mode, @account_candidates, @policy_digest, @created_by::uuid)
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
    RETURNING agent.id
)
SELECT inserted_config.ref
FROM inserted_config
JOIN updated_agent ON true;
